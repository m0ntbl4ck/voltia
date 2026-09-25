package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func sampleAnomaly(runID, meter string, typ domain.AnomalyType, sev domain.Severity, priority int, start time.Time) domain.Anomaly {
	return domain.Anomaly{
		MeterID:             meter,
		Fingerprint:         meter + "|" + string(typ) + "|" + start.UTC().Format(time.RFC3339),
		Type:                typ,
		Severity:            sev,
		Confidence:          0.9,
		ConfidenceBreakdown: json.RawMessage(`{"detector_agreement":0.5}`),
		Priority:            priority,
		PriorityBreakdown:   json.RawMessage(`{"impact":10}`),
		EpisodeStart:        start.UTC(),
		EpisodeEnd:          start.Add(10 * time.Hour).UTC(),
		Ongoing:             true,
		Evidence: domain.Evidence{
			MeterID: meter, Type: typ, VariationPct: 110.5,
			EpisodeStart: start.UTC(),
			Signals:      []domain.SignalEvidence{{Kind: "PERSISTENT_SHIFT", Variable: domain.Consumption, Hours: 10}},
		},
		Explanation: domain.Explanation{
			Summary:            "Consumption doubled",
			Reason:             "Consumption is 110% above its baseline.",
			RecommendedAction:  "Send an inspector.",
			InvestigationSteps: []string{"Check the feeder", "Compare the line load"},
			Source:             domain.SourceLLM,
			Model:              "gemini-3.8-flash",
		},
		Status:         domain.StatusOpen,
		LastAnalysisID: runID,
	}
}

func newRun(t *testing.T, repo *Repository) string {
	t.Helper()
	run, err := repo.CreateRun(context.Background(), json.RawMessage(`{}`), pendingStages())
	if err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func TestAnomalyRoundTrip(t *testing.T) {
	repo, _, loc := seededRepository(t)
	ctx := context.Background()
	start := time.Date(2026, 9, 12, 14, 0, 0, 0, loc)
	want := sampleAnomaly(newRun(t, repo), "M-109", domain.RealAnomaly, domain.SeverityHigh, 100, start)

	if err := repo.UpsertAnomalies(ctx, []domain.Anomaly{want}); err != nil {
		t.Fatal(err)
	}
	list, err := repo.Anomalies(ctx, domain.AnomalyFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("Anomalies = %v, %v", list, err)
	}
	got, err := repo.Anomaly(ctx, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID == "" || got.Status != domain.StatusOpen || got.DetectedAt.IsZero() || got.DetectedAt.Location() != loc {
		t.Errorf("id %q, status %s, detected %v", got.ID, got.Status, got.DetectedAt)
	}
	if got.MeterID != "M-109" || got.Type != want.Type || got.Severity != want.Severity ||
		got.Priority != 100 || got.Confidence != 0.9 || !got.Ongoing || got.LastAnalysisID != want.LastAnalysisID {
		t.Errorf("scalar fields differ: %+v", got)
	}
	if !got.EpisodeStart.Equal(start) || got.EpisodeStart.Location() != loc || !got.EpisodeEnd.Equal(want.EpisodeEnd) {
		t.Errorf("episode %v to %v", got.EpisodeStart, got.EpisodeEnd)
	}
	if got.Explanation.Summary != want.Explanation.Summary || got.Explanation.Reason != want.Explanation.Reason ||
		got.Explanation.RecommendedAction != want.Explanation.RecommendedAction ||
		got.Explanation.Source != domain.SourceLLM || got.Explanation.Model != "gemini-3.8-flash" ||
		len(got.Explanation.InvestigationSteps) != 2 || got.Explanation.InvestigationSteps[1] != "Compare the line load" {
		t.Errorf("explanation = %+v", got.Explanation)
	}
	gotEv, _ := json.Marshal(got.Evidence)
	wantEv, _ := json.Marshal(want.Evidence)
	if string(gotEv) != string(wantEv) {
		t.Errorf("evidence:\n got %s\nwant %s", gotEv, wantEv)
	}
	if string(got.ConfidenceBreakdown) == "" || string(got.PriorityBreakdown) == "" {
		t.Error("breakdowns were not stored")
	}
	if _, err := repo.Anomaly(ctx, missingID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Anomaly(missing) = %v, want ErrNotFound", err)
	}
}

func TestUpsertKeepsIdentityStatusAndDetectionTime(t *testing.T) {
	repo, _, loc := seededRepository(t)
	ctx := context.Background()
	start := time.Date(2026, 9, 12, 14, 0, 0, 0, loc)
	first := sampleAnomaly(newRun(t, repo), "M-109", domain.RealAnomaly, domain.SeverityHigh, 90, start)
	if err := repo.UpsertAnomalies(ctx, []domain.Anomaly{first}); err != nil {
		t.Fatal(err)
	}
	before, err := repo.Anomalies(ctx, domain.AnomalyFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE anomalies SET status = 'ACKNOWLEDGED'`); err != nil {
		t.Fatal(err)
	}

	secondRun := newRun(t, repo)
	again := sampleAnomaly(secondRun, "M-109", domain.RealAnomaly, domain.SeverityMedium, 70, start)
	again.Explanation.Reason = "A newer reason."
	again.Explanation.Model = ""
	again.Explanation.Source = domain.SourceTemplate
	if err := repo.UpsertAnomalies(ctx, []domain.Anomaly{again}); err != nil {
		t.Fatal(err)
	}

	after, err := repo.Anomalies(ctx, domain.AnomalyFilter{})
	if err != nil || len(after) != 1 {
		t.Fatalf("Anomalies = %v, %v; want the same single anomaly", after, err)
	}
	a := after[0]
	if a.ID != before[0].ID || a.Status != domain.StatusAcknowledged || !a.DetectedAt.Equal(before[0].DetectedAt) {
		t.Errorf("identity lost: id %s to %s, status %s, detected %v to %v",
			before[0].ID, a.ID, a.Status, before[0].DetectedAt, a.DetectedAt)
	}
	if a.Priority != 70 || a.Severity != domain.SeverityMedium || a.Explanation.Reason != "A newer reason." ||
		a.LastAnalysisID != secondRun || a.Explanation.Source != domain.SourceTemplate || a.Explanation.Model != "" {
		t.Errorf("refresh missing: %+v", a)
	}
}

func TestAnomaliesFilterAndOrder(t *testing.T) {
	repo, _, loc := seededRepository(t)
	ctx := context.Background()
	run := newRun(t, repo)
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, loc) }
	all := []domain.Anomaly{
		sampleAnomaly(run, "M-104", domain.ExplainableAnomaly, domain.SeverityMedium, 53, day(11)),
		sampleAnomaly(run, "M-109", domain.RealAnomaly, domain.SeverityHigh, 100, day(12)),
		sampleAnomaly(run, "M-112", domain.DataQuality, domain.SeverityHigh, 65, day(13)),
		sampleAnomaly(run, "M-106", domain.FalsePositive, domain.SeverityLow, 5, day(8)),
	}
	if err := repo.UpsertAnomalies(ctx, all); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE anomalies SET status = 'DISMISSED' WHERE meter_id = 'M-106'`); err != nil {
		t.Fatal(err)
	}

	meters := func(f domain.AnomalyFilter) []string {
		list, err := repo.Anomalies(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(list))
		for i, a := range list {
			out[i] = a.MeterID
		}
		return out
	}
	tests := []struct {
		name   string
		filter domain.AnomalyFilter
		want   []string
	}{
		{"all, most urgent first", domain.AnomalyFilter{}, []string{"M-109", "M-112", "M-104", "M-106"}},
		{"by type", domain.AnomalyFilter{Type: domain.DataQuality}, []string{"M-112"}},
		{"by severity", domain.AnomalyFilter{Severity: domain.SeverityHigh}, []string{"M-109", "M-112"}},
		{"by status", domain.AnomalyFilter{Status: domain.StatusDismissed}, []string{"M-106"}},
		{"by meter", domain.AnomalyFilter{MeterID: "M-104"}, []string{"M-104"}},
		{"combined", domain.AnomalyFilter{Severity: domain.SeverityHigh, Status: domain.StatusOpen}, []string{"M-109", "M-112"}},
		{"nothing matches", domain.AnomalyFilter{Type: domain.RealAnomaly, MeterID: "M-104"}, []string{}},
	}
	for _, tt := range tests {
		got := meters(tt.filter)
		if len(got) != len(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

func TestUpsertIsAllOrNothing(t *testing.T) {
	repo, _, loc := seededRepository(t)
	ctx := context.Background()
	run := newRun(t, repo)
	start := time.Date(2026, 9, 12, 14, 0, 0, 0, loc)
	good := sampleAnomaly(run, "M-109", domain.RealAnomaly, domain.SeverityHigh, 100, start)
	bad := sampleAnomaly(run, "M-999", domain.RealAnomaly, domain.SeverityHigh, 50, start)

	if err := repo.UpsertAnomalies(ctx, []domain.Anomaly{good, bad}); err == nil {
		t.Fatal("expected an error for an unknown meter")
	}
	if n := count(t, repo.db, "anomalies"); n != 0 {
		t.Errorf("%d anomalies stored after a failed batch, want 0", n)
	}
}

func TestNoInvestigationStepsAreStoredAsAnEmptyList(t *testing.T) {
	repo, _, loc := seededRepository(t)
	ctx := context.Background()
	a := sampleAnomaly(newRun(t, repo), "M-106", domain.FalsePositive, domain.SeverityLow, 5, time.Date(2026, 9, 8, 0, 0, 0, 0, loc))
	a.Explanation.InvestigationSteps = nil
	if err := repo.UpsertAnomalies(ctx, []domain.Anomaly{a}); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := repo.db.QueryRowContext(ctx, `SELECT jsonb_typeof(investigation_steps) FROM anomalies`).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "array" {
		t.Errorf("investigation_steps is JSON %s, want array", kind)
	}
}
