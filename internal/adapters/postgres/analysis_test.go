package postgres

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis"
	"github.com/m0ntbl4ck/voltia/internal/app"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

type plainExplainer struct{}

func (plainExplainer) Explain(_ context.Context, ev domain.Evidence) (domain.Explanation, error) {
	return domain.Explanation{
		Summary: "summary " + ev.MeterID, Reason: "reason " + ev.MeterID,
		RecommendedAction: "act " + ev.MeterID, InvestigationSteps: []string{"look"},
		Source: domain.SourceTemplate,
	}, nil
}

func runToCompletion(t *testing.T, repo *Repository, svc *app.AnalysisService) domain.AnalysisRun {
	t.Helper()
	ctx := context.Background()
	started, err := svc.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		run, err := repo.Run(ctx, started.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !run.Status.Active() {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the run did not finish")
	return domain.AnalysisRun{}
}

func TestAnalysisAgainstPostgresIsRepeatable(t *testing.T) {
	repo, _, _ := seededRepository(t)
	ctx := context.Background()
	svc := app.NewAnalysisService(repo, repo, repo, plainExplainer{}, app.AnalysisOptions{
		Config: analysis.DefaultConfig(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer svc.Close()

	first := runToCompletion(t, repo, svc)
	if first.Status != domain.RunCompleted || first.Summary == nil || first.Summary.Anomalies != 4 {
		t.Fatalf("first run = %+v, summary %+v", first.Status, first.Summary)
	}
	if len(first.Stages) != 7 || first.Stages[6].Status != domain.StageDone || first.FinishedAt == nil {
		t.Errorf("stages = %+v", first.Stages)
	}
	stored, err := repo.Anomalies(ctx, domain.AnomalyFilter{})
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"M-109", "M-112", "M-104", "M-106"}
	if len(stored) != 4 {
		t.Fatalf("%d anomalies stored, want 4", len(stored))
	}
	for i, a := range stored {
		if a.MeterID != wantOrder[i] || a.Status != domain.StatusOpen || a.LastAnalysisID != first.ID ||
			a.Explanation.Reason != "reason "+a.MeterID || a.Evidence.MeterName == "" || len(a.Evidence.Signals) == 0 {
			t.Errorf("anomaly %d = %s status %s run %s reason %q evidence %q", i, a.MeterID, a.Status, a.LastAnalysisID,
				a.Explanation.Reason, a.Evidence.MeterName)
		}
	}
	if stored[0].Priority != 100 || stored[0].Type != domain.RealAnomaly {
		t.Errorf("top anomaly = %s priority %d", stored[0].Type, stored[0].Priority)
	}

	if _, err := repo.db.ExecContext(ctx, `UPDATE anomalies SET status = 'ACKNOWLEDGED' WHERE meter_id = 'M-109'`); err != nil {
		t.Fatal(err)
	}
	second := runToCompletion(t, repo, svc)
	if second.ID == first.ID {
		t.Fatal("the second run reused the first")
	}
	again, err := repo.Anomalies(ctx, domain.AnomalyFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 4 {
		t.Fatalf("%d anomalies after the second run, want the same 4", len(again))
	}
	for i, a := range again {
		if a.ID != stored[i].ID || a.LastAnalysisID != second.ID {
			t.Errorf("%s: id %s to %s, run %s; want the same id under run %s", a.MeterID, stored[i].ID, a.ID, a.LastAnalysisID, second.ID)
		}
	}
	if again[0].Status != domain.StatusAcknowledged {
		t.Errorf("M-109 status = %s, want ACKNOWLEDGED kept across runs", again[0].Status)
	}
	latest, err := repo.LatestRun(ctx)
	if err != nil || latest.ID != second.ID {
		t.Errorf("LatestRun = %v, %v; want %s", latest.ID, err, second.ID)
	}
}
