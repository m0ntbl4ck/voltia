package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/data"
	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/analysis"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// memory is an in-memory Source, Runs and Anomalies.
type memory struct {
	ds seed.Dataset

	mu        sync.Mutex
	runs      []domain.AnalysisRun
	saves     []progressSave
	anomalies []domain.Anomaly
	upserts   int
}

type progressSave struct {
	status  domain.RunStatus
	current string
}

func newMemory(t *testing.T) *memory {
	t.Helper()
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := seed.Load(data.FS, loc)
	if err != nil {
		t.Fatal(err)
	}
	return &memory{ds: ds}
}

func (m *memory) Meters(context.Context) ([]domain.Meter, error)     { return m.ds.Meters, nil }
func (m *memory) Readings(context.Context) ([]domain.Reading, error) { return m.ds.Readings, nil }
func (m *memory) Events(context.Context) ([]domain.Event, error)     { return m.ds.Events, nil }

func (m *memory) CreateRun(_ context.Context, params json.RawMessage, stages []domain.StageState) (domain.AnalysisRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run := domain.AnalysisRun{
		ID: fmt.Sprintf("run-%d", len(m.runs)+1), Status: domain.RunPending,
		Stages: slices.Clone(stages), Params: params, StartedAt: time.Now(),
	}
	m.runs = append(m.runs, run)
	return run, nil
}

func (m *memory) SaveProgress(ctx context.Context, id string, status domain.RunStatus, current string, stages []domain.StageState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves = append(m.saves, progressSave{status, current})
	r := m.find(id)
	r.Status, r.CurrentStage, r.Stages = status, current, slices.Clone(stages)
	return nil
}

func (m *memory) FinishRun(ctx context.Context, id string, status domain.RunStatus, stages []domain.StageState, summary domain.RunSummary) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	now := time.Now()
	r.Status, r.CurrentStage, r.Stages, r.Summary, r.FinishedAt = status, "", slices.Clone(stages), &summary, &now
	return nil
}

func (m *memory) find(id string) *domain.AnalysisRun {
	for i := range m.runs {
		if m.runs[i].ID == id {
			return &m.runs[i]
		}
	}
	panic("unknown run " + id)
}

func (m *memory) Run(_ context.Context, id string) (domain.AnalysisRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.ID == id {
			return r, nil
		}
	}
	return domain.AnalysisRun{}, domain.ErrNotFound
}

func (m *memory) LatestRun(context.Context) (domain.AnalysisRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.runs) == 0 {
		return domain.AnalysisRun{}, domain.ErrNotFound
	}
	return m.runs[len(m.runs)-1], nil
}

func (m *memory) LatestCompletedRun(context.Context) (domain.AnalysisRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.runs) - 1; i >= 0; i-- {
		if m.runs[i].Status == domain.RunCompleted {
			return m.runs[i], nil
		}
	}
	return domain.AnalysisRun{}, domain.ErrNotFound
}

func (m *memory) ActiveRun(context.Context) (domain.AnalysisRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.runs) - 1; i >= 0; i-- {
		if m.runs[i].Status.Active() {
			return m.runs[i], nil
		}
	}
	return domain.AnalysisRun{}, domain.ErrNotFound
}

func (m *memory) UpsertAnomalies(_ context.Context, as []domain.Anomaly) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upserts++
	m.anomalies = append(m.anomalies, as...)
	return nil
}

// scriptedExplainer answers from a function so each test decides how.
type scriptedExplainer func(context.Context, domain.Evidence) (domain.Explanation, error)

func (f scriptedExplainer) Explain(ctx context.Context, ev domain.Evidence) (domain.Explanation, error) {
	return f(ctx, ev)
}

func plainExplainer(_ context.Context, ev domain.Evidence) (domain.Explanation, error) {
	return domain.Explanation{
		Summary:            "summary of " + ev.MeterID,
		Reason:             "reason for " + ev.MeterName,
		RecommendedAction:  "act on " + ev.MeterID,
		InvestigationSteps: []string{"step"},
		Source:             domain.SourceTemplate,
	}, nil
}

func newService(m *memory, ex scriptedExplainer, delay time.Duration) *AnalysisService {
	return NewAnalysisService(m, m, m, ex, AnalysisOptions{
		Config:     analysis.DefaultConfig(),
		StageDelay: delay,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// waitFinished polls until the run leaves the active states.
func waitFinished(t *testing.T, m *memory, id string) domain.AnalysisRun {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		run, err := m.Run(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if !run.Status.Active() {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish", id)
	return domain.AnalysisRun{}
}

func TestRunAnalysisEndToEnd(t *testing.T) {
	m := newMemory(t)
	svc := newService(m, plainExplainer, 0)
	defer svc.Close()

	started, err := svc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != domain.RunPending || len(started.Stages) != 7 {
		t.Fatalf("started run = %+v", started)
	}
	run := waitFinished(t, m, started.ID)

	if run.Status != domain.RunCompleted || run.FinishedAt == nil || run.CurrentStage != "" {
		t.Fatalf("run = %+v", run)
	}
	wantStages := []string{"READINGS", "BASELINE", "DETECTION", "CORRELATION", "EVENTS", "EXPLANATION", "RECOMMENDATION"}
	for i, st := range run.Stages {
		if st.Name != wantStages[i] || st.Status != domain.StageDone || st.StartedAt == nil || st.FinishedAt == nil {
			t.Errorf("stage %d = %+v, want %s done", i, st, wantStages[i])
		}
	}
	if run.Summary == nil || run.Summary.Anomalies != 4 || run.Summary.ByType["REAL_ANOMALY"] != 1 ||
		run.Summary.BySeverity["HIGH"] != 2 || run.Summary.Confidence < 0.98 || len(run.Summary.Failures) != 0 {
		t.Errorf("summary = %+v", run.Summary)
	}
	var params analysis.Config
	if err := json.Unmarshal(run.Params, &params); err != nil || params.Detectors.ShiftZ != 3 {
		t.Errorf("params do not carry the thresholds: %s (%v)", run.Params, err)
	}

	if len(m.anomalies) != 4 || m.upserts != 1 {
		t.Fatalf("%d anomalies in %d batches, want 4 in 1", len(m.anomalies), m.upserts)
	}
	top := m.anomalies[0]
	if top.MeterID != "M-109" || top.LastAnalysisID != run.ID || top.Status != domain.StatusOpen {
		t.Errorf("top anomaly = %s, run %s, status %s", top.MeterID, top.LastAnalysisID, top.Status)
	}
	if top.Explanation.Reason != "reason for "+top.Evidence.MeterName || top.Evidence.MeterName == "" {
		t.Errorf("explanation was written from other evidence: %+v", top.Explanation)
	}
}

func TestProgressIsSavedStageByStage(t *testing.T) {
	m := newMemory(t)
	svc := newService(m, plainExplainer, 0)
	defer svc.Close()
	started, err := svc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitFinished(t, m, started.ID)

	var running []string
	for _, s := range m.saves {
		if s.status != domain.RunRunning {
			t.Errorf("saved status %s, want RUNNING while going", s.status)
		}
		if s.current != "" {
			running = append(running, s.current)
		}
	}
	want := []string{"READINGS", "BASELINE", "DETECTION", "CORRELATION", "EVENTS", "EXPLANATION", "RECOMMENDATION"}
	if !slices.Equal(running, want) {
		t.Errorf("stages announced as %v, want %v", running, want)
	}
}

func TestStageDelayPacesTheRun(t *testing.T) {
	const delay = 50 * time.Millisecond
	m := newMemory(t)
	svc := newService(m, plainExplainer, delay)
	defer svc.Close()

	started, err := svc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run := waitFinished(t, m, started.ID)
	for i := 1; i < len(run.Stages); i++ {
		gap := run.Stages[i].StartedAt.Sub(*run.Stages[i-1].FinishedAt)
		if gap < delay {
			t.Errorf("%s started %v after %s finished, want at least %v",
				run.Stages[i].Name, gap, run.Stages[i-1].Name, delay)
		}
	}
}

func TestAMeterWithoutABaselineIsReportedAndTheRestGoOn(t *testing.T) {
	m := newMemory(t)
	loc := m.ds.Readings[0].Timestamp.Location()
	for h := 0; h < 5; h++ {
		m.ds.Readings = append(m.ds.Readings, domain.Reading{
			MeterID: "M-999", Timestamp: time.Date(2026, 9, 1, h, 0, 0, 0, loc),
			ConsumptionKWh: 10, VoltageV: 220, CurrentA: 30, PowerFactor: 0.9,
		})
	}
	svc := newService(m, plainExplainer, 0)
	defer svc.Close()

	started, err := svc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run := waitFinished(t, m, started.ID)
	if run.Status != domain.RunCompleted || run.Summary.Anomalies != 4 {
		t.Fatalf("run = %+v, summary %+v", run.Status, run.Summary)
	}
	if len(run.Summary.Failures) != 1 || run.Summary.Failures[0].MeterID != "M-999" || run.Summary.Failures[0].Error == "" {
		t.Errorf("failures = %+v", run.Summary.Failures)
	}
}

func TestStartWhileAnotherRunIsActiveReturnsIt(t *testing.T) {
	m := newMemory(t)
	release := make(chan struct{})
	svc := newService(m, func(ctx context.Context, ev domain.Evidence) (domain.Explanation, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return plainExplainer(ctx, ev)
	}, 0)
	defer svc.Close()

	first, err := svc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || len(m.runs) != 1 {
		t.Errorf("second Start gave %s and there are %d runs; want %s and 1", second.ID, len(m.runs), first.ID)
	}

	close(release)
	waitFinished(t, m, first.ID)
	third, err := svc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if third.ID == first.ID {
		t.Error("a finished run was returned instead of starting a new one")
	}
	waitFinished(t, m, third.ID)
}

func TestConcurrentStartsCreateOneRun(t *testing.T) {
	m := newMemory(t)
	release := make(chan struct{})
	svc := newService(m, func(ctx context.Context, ev domain.Evidence) (domain.Explanation, error) {
		<-release
		return plainExplainer(ctx, ev)
	}, 0)
	defer svc.Close()

	var wg sync.WaitGroup
	ids := make([]string, 8)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, err := svc.Start(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = run.ID
		}()
	}
	wg.Wait()
	close(release)
	waitFinished(t, m, ids[0])
	if len(m.runs) != 1 {
		t.Errorf("%d runs created by 8 simultaneous starts, want 1", len(m.runs))
	}
}

func TestExplainerFailureFailsTheRunAndStoresNothing(t *testing.T) {
	m := newMemory(t)
	svc := newService(m, func(_ context.Context, ev domain.Evidence) (domain.Explanation, error) {
		if ev.MeterID == "M-104" {
			return domain.Explanation{}, errors.New("quota exhausted")
		}
		return plainExplainer(context.Background(), ev)
	}, 0)
	defer svc.Close()

	started, err := svc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run := waitFinished(t, m, started.ID)
	if run.Status != domain.RunFailed || run.Summary == nil || run.Summary.Error == "" {
		t.Fatalf("run = %+v", run)
	}
	if got := run.Summary.Error; got != "explain M-104: quota exhausted" {
		t.Errorf("error = %q", got)
	}
	if len(m.anomalies) != 0 {
		t.Errorf("%d anomalies stored by a failed run", len(m.anomalies))
	}
	if run.Stages[5].Status != domain.StageRunning {
		t.Errorf("the explanation stage should show where it stopped: %+v", run.Stages[5])
	}
	if _, err := m.ActiveRun(context.Background()); !errors.Is(err, domain.ErrNotFound) {
		t.Error("a failed run still counts as active")
	}
}

func TestCloseCancelsARunAndRecordsIt(t *testing.T) {
	m := newMemory(t)
	entered := make(chan struct{}, 8)
	svc := newService(m, func(ctx context.Context, _ domain.Evidence) (domain.Explanation, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return domain.Explanation{}, ctx.Err()
	}, 0)

	started, err := svc.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	svc.Close()

	run, err := m.Run(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != domain.RunFailed || run.FinishedAt == nil {
		t.Errorf("cancelled run = %+v", run)
	}
	if run.Summary == nil || run.Summary.Error != "interrupted because the server was shutting down" {
		t.Errorf("summary = %+v, want the shutdown to be named, not a bare context error", run.Summary)
	}
	if _, err := svc.Start(context.Background()); err == nil {
		t.Error("Start after Close should fail")
	}
}

func TestRecoverClosesRunsLeftActive(t *testing.T) {
	m := newMemory(t)
	svc := newService(m, plainExplainer, 0)
	defer svc.Close()
	stale, err := m.CreateRun(context.Background(), json.RawMessage(`{}`), initialStages())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SaveProgress(context.Background(), stale.ID, domain.RunRunning, "BASELINE", stale.Stages); err != nil {
		t.Fatal(err)
	}

	if err := svc.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Run(context.Background(), stale.ID)
	if got.Status != domain.RunFailed || got.Summary == nil || got.Summary.Error != "interrupted by a restart" {
		t.Errorf("recovered run = %+v", got)
	}
	if _, err := m.ActiveRun(context.Background()); !errors.Is(err, domain.ErrNotFound) {
		t.Error("a run is still active after Recover")
	}
}
