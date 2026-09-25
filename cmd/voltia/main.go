package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/m0ntbl4ck/voltia/data"
	httpapi "github.com/m0ntbl4ck/voltia/internal/adapters/http"
	"github.com/m0ntbl4ck/voltia/internal/adapters/llm/template"
	"github.com/m0ntbl4ck/voltia/internal/adapters/postgres"
	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/analysis"
	"github.com/m0ntbl4ck/voltia/internal/app"
	"github.com/m0ntbl4ck/voltia/internal/config"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return err
	}
	if err := postgres.Migrate(ctx, db); err != nil {
		return err
	}
	ds, err := seed.Load(data.FS, cfg.PlantTZ)
	if err != nil {
		return err
	}
	added, err := postgres.Seed(ctx, db, ds.Meters, ds.Readings, ds.Events)
	if err != nil {
		return err
	}
	log.Printf("seed added %d meters, %d readings, %d events", added.Meters, added.Readings, added.Events)

	repo := postgres.NewRepository(db, cfg.PlantTZ)
	analyses := app.NewAnalysisService(repo, repo, repo, template.New(cfg.PlantTZ), app.AnalysisOptions{
		Config:     analysis.DefaultConfig(),
		StageDelay: cfg.StageDelay,
	})
	// Closing the service cancels a run in progress and lets it record how it
	// ended, so it must run before the database closes.
	defer analyses.Close()
	if err := analyses.Recover(ctx); err != nil {
		return err
	}

	handler := httpapi.NewRouter(httpapi.Deps{Analysis: analyses, Runs: repo})
	return serve(ctx, &http.Server{Addr: ":" + cfg.Port, Handler: handler})
}

// serve runs srv until ctx is cancelled, then gives in-flight requests
// five seconds to finish.
func serve(ctx context.Context, srv *http.Server) error {
	errc := make(chan error, 1)
	go func() {
		log.Printf("voltia listening on %s", srv.Addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
