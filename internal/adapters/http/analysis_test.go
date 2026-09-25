package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

const validID = "3f2b8c1e-5a4d-4e6f-9b7a-1c2d3e4f5a6b"

type fakeStarter struct {
	run   domain.AnalysisRun
	err   error
	calls int
}

func (f *fakeStarter) Start(context.Context) (domain.AnalysisRun, error) {
	f.calls++
	return f.run, f.err
}

type fakeRuns struct {
	run       domain.AnalysisRun
	err       error
	gotID     string
	runCalls  int
	lastCalls int
}

func (f *fakeRuns) Run(_ context.Context, id string) (domain.AnalysisRun, error) {
	f.runCalls++
	f.gotID = id
	return f.run, f.err
}

func (f *fakeRuns) LatestRun(context.Context) (domain.AnalysisRun, error) {
	f.lastCalls++
	return f.run, f.err
}

func newServer(starter *fakeStarter, runs *fakeRuns) http.Handler {
	return NewRouter(Deps{Analysis: starter, Runs: runs, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func bogota(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestHealthz(t *testing.T) {
	rec := do(newServer(&fakeStarter{}, &fakeRuns{}), http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("healthz = %d %q", rec.Code, rec.Body)
	}
}

func TestAnalyzeAnswers202WithTheRunID(t *testing.T) {
	starter := &fakeStarter{run: domain.AnalysisRun{ID: validID}}
	rec := do(newServer(starter, &fakeRuns{}), http.MethodPost, "/api/v1/ai/analyze")

	if rec.Code != http.StatusAccepted || starter.calls != 1 {
		t.Fatalf("status %d after %d starts", rec.Code, starter.calls)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type = %q", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["analysis_id"] != validID {
		t.Errorf("body = %s (%v)", rec.Body, err)
	}
}

func TestAnalyzeFailureIsAProblemWithoutInternalDetail(t *testing.T) {
	starter := &fakeStarter{err: errors.New("connection to 10.0.0.5 refused")}
	rec := do(newServer(starter, &fakeRuns{}), http.MethodPost, "/api/v1/ai/analyze")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content type = %q", ct)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("the body leaks the cause: %s", rec.Body)
	}
	var p problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Status != 500 || p.Title != "Internal Server Error" || p.Type != "about:blank" {
		t.Errorf("problem = %+v (%v)", p, err)
	}
}

func TestGetRunShapesTheResponse(t *testing.T) {
	loc := bogota(t)
	start := time.Date(2026, 9, 24, 15, 0, 0, 0, loc)
	end := start.Add(3 * time.Second)
	run := domain.AnalysisRun{
		ID: validID, Status: domain.RunRunning, CurrentStage: "EVENTS", StartedAt: start,
		Stages: []domain.StageState{
			{Name: "READINGS", Status: domain.StageDone, StartedAt: &start, FinishedAt: &end},
			{Name: "BASELINE", Status: domain.StageDone, StartedAt: &start, FinishedAt: &end},
			{Name: "DETECTION", Status: domain.StageRunning, StartedAt: &start},
			{Name: "CORRELATION", Status: domain.StagePending},
		},
	}
	runs := &fakeRuns{run: run}
	rec := do(newServer(&fakeStarter{}, runs), http.MethodGet, "/api/v1/ai/analysis/"+validID)

	if rec.Code != http.StatusOK || runs.gotID != validID {
		t.Fatalf("status %d, looked up %q", rec.Code, runs.gotID)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "status", "current_stage", "progress", "stages", "summary", "started_at", "finished_at"} {
		if _, ok := body[key]; !ok {
			t.Errorf("response lacks %q: %s", key, rec.Body)
		}
	}
	if body["progress"] != 0.5 || body["status"] != "RUNNING" || body["current_stage"] != "EVENTS" {
		t.Errorf("progress %v, status %v, stage %v", body["progress"], body["status"], body["current_stage"])
	}
	if body["started_at"] != "2026-09-24T20:00:00Z" {
		t.Errorf("started_at = %v, want UTC 20:00Z for 15:00 in Bogota", body["started_at"])
	}
	first := body["stages"].([]any)[0].(map[string]any)
	if first["name"] != "READINGS" || first["finished_at"] != "2026-09-24T20:00:03Z" {
		t.Errorf("first stage = %v", first)
	}
	if body["summary"] != nil || body["finished_at"] != nil {
		t.Errorf("a running run has no summary or end: %v, %v", body["summary"], body["finished_at"])
	}
}

func TestGetFinishedRunIncludesTheSummary(t *testing.T) {
	done := time.Date(2026, 9, 24, 15, 0, 5, 0, bogota(t))
	run := domain.AnalysisRun{
		ID: validID, Status: domain.RunCompleted, StartedAt: done.Add(-5 * time.Second), FinishedAt: &done,
		Stages:  []domain.StageState{{Name: "READINGS", Status: domain.StageDone}},
		Summary: &domain.RunSummary{Anomalies: 4, ByType: map[string]int{"REAL_ANOMALY": 1}, Confidence: 0.989, Failures: []domain.MeterFailure{}},
	}
	rec := do(newServer(&fakeStarter{}, &fakeRuns{run: run}), http.MethodGet, "/api/v1/ai/analysis/"+validID)
	var body struct {
		Progress   float64 `json:"progress"`
		FinishedAt string  `json:"finished_at"`
		Summary    struct {
			Anomalies  int            `json:"anomalies"`
			ByType     map[string]int `json:"by_type"`
			Confidence float64        `json:"confidence"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Progress != 1 || body.FinishedAt != "2026-09-24T20:00:05Z" || body.Summary.Anomalies != 4 ||
		body.Summary.ByType["REAL_ANOMALY"] != 1 || body.Summary.Confidence != 0.989 {
		t.Errorf("body = %+v", body)
	}
}

func TestLatestIsNotTakenForAnID(t *testing.T) {
	runs := &fakeRuns{run: domain.AnalysisRun{ID: validID, Status: domain.RunCompleted}}
	rec := do(newServer(&fakeStarter{}, runs), http.MethodGet, "/api/v1/ai/analysis/latest")
	if rec.Code != http.StatusOK || runs.lastCalls != 1 || runs.runCalls != 0 {
		t.Errorf("status %d, latest %d, by id %d", rec.Code, runs.lastCalls, runs.runCalls)
	}
}

func TestMissingRunsAreProblems404(t *testing.T) {
	tests := []struct {
		name, path string
		wantLookup bool
	}{
		{"nothing has run yet", "/api/v1/ai/analysis/latest", false},
		{"unknown id", "/api/v1/ai/analysis/" + validID, true},
		{"not an id at all", "/api/v1/ai/analysis/abc", false},
		{"sql-looking id", "/api/v1/ai/analysis/1%27%20OR%20%271%27=%271", false},
	}
	for _, tt := range tests {
		runs := &fakeRuns{err: domain.ErrNotFound}
		rec := do(newServer(&fakeStarter{}, runs), http.MethodGet, tt.path)
		if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: status %d, type %q", tt.name, rec.Code, rec.Header().Get("Content-Type"))
		}
		if looked := runs.runCalls > 0; looked != tt.wantLookup {
			t.Errorf("%s: repository consulted = %v, want %v", tt.name, looked, tt.wantLookup)
		}
	}
}

func TestReadFailureIsAProblem500(t *testing.T) {
	runs := &fakeRuns{err: errors.New("pq: relation does not exist")}
	rec := do(newServer(&fakeStarter{}, runs), http.MethodGet, "/api/v1/ai/analysis/"+validID)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "relation") {
		t.Errorf("status %d, body %s", rec.Code, rec.Body)
	}
}
