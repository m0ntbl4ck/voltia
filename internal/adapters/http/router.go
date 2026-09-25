// Package httpapi serves the REST API under /api/v1.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// Starter begins an analysis run.
type Starter interface {
	Start(ctx context.Context) (domain.AnalysisRun, error)
}

// RunReader looks up runs. Lookups return domain.ErrNotFound when nothing matches.
type RunReader interface {
	Run(ctx context.Context, id string) (domain.AnalysisRun, error)
	LatestRun(ctx context.Context) (domain.AnalysisRun, error)
}

// Deps is what the handlers need.
type Deps struct {
	Analysis Starter
	Runs     RunReader
	Logger   *slog.Logger
}

// NewRouter builds the handler for the whole API.
func NewRouter(d Deps) http.Handler {
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Route("/api/v1", func(r chi.Router) {
		analysisResource{starter: d.Analysis, runs: d.Runs, log: log}.routes(r)
	})
	return r
}
