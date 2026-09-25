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

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/m0ntbl4ck/voltia/data"
	"github.com/m0ntbl4ck/voltia/internal/adapters/postgres"
	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
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

	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return serve(ctx, &http.Server{Addr: ":" + cfg.Port, Handler: r})
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
