package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

const missingID = "00000000-0000-0000-0000-000000000000"

func pendingStages() []domain.StageState {
	return []domain.StageState{
		{Name: "READINGS", Status: domain.StagePending},
		{Name: "BASELINE", Status: domain.StagePending},
	}
}

func TestRunLifecycle(t *testing.T) {
	repo, _, loc := seededRepository(t)
	ctx := context.Background()

	if _, err := repo.LatestRun(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("LatestRun with no runs: %v, want ErrNotFound", err)
	}

	created, err := repo.CreateRun(ctx, json.RawMessage(`{"shift_z":3}`), pendingStages())
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != domain.RunPending || created.FinishedAt != nil || created.Summary != nil {
		t.Errorf("created run = %+v", created)
	}
	if created.StartedAt.Location() != loc {
		t.Errorf("StartedAt in %v, want %v", created.StartedAt.Location(), loc)
	}
	if len(created.Stages) != 2 || created.Stages[0].Name != "READINGS" {
		t.Errorf("stages = %+v", created.Stages)
	}
	if string(created.Params) != `{"shift_z": 3}` && string(created.Params) != `{"shift_z":3}` {
		t.Errorf("params = %s", created.Params)
	}

	stages := pendingStages()
	stages[0].Status = domain.StageRunning
	if err := repo.SaveProgress(ctx, created.ID, domain.RunRunning, "READINGS", stages); err != nil {
		t.Fatal(err)
	}
	active, err := repo.ActiveRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != created.ID || active.Status != domain.RunRunning || active.CurrentStage != "READINGS" ||
		active.Stages[0].Status != domain.StageRunning {
		t.Errorf("active run = %+v", active)
	}

	stages[0].Status, stages[1].Status = domain.StageDone, domain.StageDone
	summary := domain.RunSummary{
		Anomalies:  4,
		ByType:     map[string]int{"REAL_ANOMALY": 1},
		Confidence: 0.989,
		Failures:   []domain.MeterFailure{{MeterID: "M-101", Error: "no baseline"}},
	}
	if err := repo.FinishRun(ctx, created.ID, domain.RunCompleted, stages, summary); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ActiveRun(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ActiveRun after finishing: %v, want ErrNotFound", err)
	}
	got, err := repo.LatestRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.Status != domain.RunCompleted || got.CurrentStage != "" || got.FinishedAt == nil {
		t.Errorf("finished run = %+v", got)
	}
	if got.Summary == nil || got.Summary.Anomalies != 4 || got.Summary.Confidence != 0.989 ||
		got.Summary.ByType["REAL_ANOMALY"] != 1 || len(got.Summary.Failures) != 1 {
		t.Errorf("summary = %+v", got.Summary)
	}
	if got.Stages[1].Status != domain.StageDone {
		t.Errorf("stages = %+v", got.Stages)
	}
}

func TestRunFailureKeepsTheError(t *testing.T) {
	repo, _, _ := seededRepository(t)
	ctx := context.Background()
	run, err := repo.CreateRun(ctx, json.RawMessage(`{}`), pendingStages())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishRun(ctx, run.ID, domain.RunFailed, pendingStages(), domain.RunSummary{Error: "no readings"}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.RunFailed || got.Summary == nil || got.Summary.Error != "no readings" {
		t.Errorf("failed run = %+v", got)
	}
	if _, err := repo.Run(ctx, missingID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Run(missing) = %v, want ErrNotFound", err)
	}
}
