package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// analysisResource serves the analysis endpoints.
type analysisResource struct {
	starter Starter
	runs    RunReader
	log     *slog.Logger
}

func (a analysisResource) routes(r chi.Router) {
	r.Post("/ai/analyze", a.start)
	r.Get("/ai/analysis/latest", a.latest)
	r.Get("/ai/analysis/{id}", a.get)
}

// start answers 202 with the id of the run to follow, which is the one already
// going when there is one.
func (a analysisResource) start(w http.ResponseWriter, r *http.Request) {
	run, err := a.starter.Start(r.Context())
	if err != nil {
		serverError(w, a.log, "start analysis", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"analysis_id": run.ID})
}

func (a analysisResource) latest(w http.ResponseWriter, r *http.Request) {
	run, err := a.runs.LatestRun(r.Context())
	a.respond(w, run, err)
}

func (a analysisResource) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !uuidPattern.MatchString(id) {
		writeProblem(w, http.StatusNotFound, "no analysis has that id")
		return
	}
	run, err := a.runs.Run(r.Context(), id)
	a.respond(w, run, err)
}

func (a analysisResource) respond(w http.ResponseWriter, run domain.AnalysisRun, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "no analysis has run yet or none has that id")
		return
	}
	if err != nil {
		serverError(w, a.log, "read analysis", err)
		return
	}
	writeJSON(w, http.StatusOK, toRunResponse(run))
}

type runResponse struct {
	ID           string              `json:"id"`
	Status       domain.RunStatus    `json:"status"`
	CurrentStage string              `json:"current_stage"`
	Progress     float64             `json:"progress"`
	Stages       []domain.StageState `json:"stages"`
	Summary      *domain.RunSummary  `json:"summary"`
	StartedAt    time.Time           `json:"started_at"`
	FinishedAt   *time.Time          `json:"finished_at"`
}

// toRunResponse shapes a run for the API: dates in UTC and the share of
// stages done, which the interface draws as a progress bar.
func toRunResponse(run domain.AnalysisRun) runResponse {
	done := 0
	stages := make([]domain.StageState, len(run.Stages))
	for i, s := range run.Stages {
		if s.Status == domain.StageDone {
			done++
		}
		stages[i] = s
		stages[i].StartedAt, stages[i].FinishedAt = utc(s.StartedAt), utc(s.FinishedAt)
	}
	resp := runResponse{
		ID: run.ID, Status: run.Status, CurrentStage: run.CurrentStage, Stages: stages,
		Summary: run.Summary, StartedAt: run.StartedAt.UTC(), FinishedAt: utc(run.FinishedAt),
	}
	if len(run.Stages) > 0 {
		resp.Progress = float64(done) / float64(len(run.Stages))
	}
	return resp
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
