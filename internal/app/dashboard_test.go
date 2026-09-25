package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// dashboardFor builds the service over the shipped dataset, with the anomalies
// the engine finds and, when analysed is set, one completed run with 0.989 confidence.
func dashboardFor(t *testing.T, analysed bool) (*DashboardService, *memoryMeters, *memory) {
	t.Helper()
	data := newMemoryMeters(t)
	runs := newMemory(t)
	if !analysed {
		data.anomalies = nil
		return NewDashboardService(data.service(), data, runs), data, runs
	}
	run, err := runs.CreateRun(context.Background(), []byte(`{}`), initialStages())
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.FinishRun(context.Background(), run.ID, domain.RunCompleted, run.Stages, domain.RunSummary{Anomalies: 4, Confidence: 0.989}); err != nil {
		t.Fatal(err)
	}
	return NewDashboardService(data.service(), data, runs), data, runs
}

func attentionMeters(d Dashboard) []string {
	out := make([]string, len(d.Attention))
	for i, a := range d.Attention {
		out[i] = a.MeterID
	}
	return out
}

func TestDashboardBeforeAnyAnalysisInvitesToRunOne(t *testing.T) {
	svc, _, _ := dashboardFor(t, false)
	d, err := svc.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.HasAnalysis || d.LastAnalysis != nil || d.Confidence != nil {
		t.Errorf("analysis %v, last %v, confidence %v; want none", d.HasAnalysis, d.LastAnalysis, d.Confidence)
	}
	if d.Meters != (MeterCounts{Total: 12, OK: 12}) {
		t.Errorf("meters = %+v, want 12 all ok", d.Meters)
	}
	if d.Anomalies != (AnomalyCounts{}) || len(d.Attention) != 0 {
		t.Errorf("anomalies %+v, attention %v", d.Anomalies, attentionMeters(d))
	}
	if d.ConsumptionKWh <= 0 || d.PeriodFrom != "2026-09-01" || d.PeriodTo != "2026-09-14" || len(d.Daily) != 14 {
		t.Errorf("period %s to %s, %.0f kWh, %d days", d.PeriodFrom, d.PeriodTo, d.ConsumptionKWh, len(d.Daily))
	}
}

func TestDashboardAfterTheAnalysis(t *testing.T) {
	svc, _, _ := dashboardFor(t, true)
	d, err := svc.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !d.HasAnalysis || d.LastAnalysis == nil || d.LastAnalysis.Status != domain.RunCompleted {
		t.Fatalf("analysis %v, last %+v", d.HasAnalysis, d.LastAnalysis)
	}
	if d.Confidence == nil || *d.Confidence != 0.989 {
		t.Errorf("confidence = %v, want 0.989", d.Confidence)
	}
	if d.Meters != (MeterCounts{Total: 12, OK: 9, Alert: 2, Critical: 1}) {
		t.Errorf("meters = %+v, want 9 ok, 2 alert, 1 critical", d.Meters)
	}
	if d.Anomalies != (AnomalyCounts{Unresolved: 4, PendingHighPriority: 2}) {
		t.Errorf("anomalies = %+v, want 4 unresolved and 2 pending of high priority", d.Anomalies)
	}
	if got := attentionMeters(d); !slices.Equal(got, []string{"M-109", "M-112", "M-104"}) {
		t.Errorf("attention = %v, want the three most urgent by priority", got)
	}
}

func TestActingOnAnAnomalyMovesItOutOfThePending(t *testing.T) {
	svc, data, _ := dashboardFor(t, true)
	ctx := context.Background()

	data.setStatus("M-109", domain.StatusAcknowledged)
	d, _ := svc.Summary(ctx)
	if d.Anomalies != (AnomalyCounts{Unresolved: 4, PendingHighPriority: 1}) {
		t.Errorf("after the inspection order: %+v, want 4 unresolved and 1 pending (the demo goes from 2 to 1)", d.Anomalies)
	}
	if got := attentionMeters(d); !slices.Equal(got, []string{"M-112", "M-104", "M-106"}) {
		t.Errorf("attention = %v; an acknowledged anomaly should leave the card and the next one enter", got)
	}
	if d.Meters.Critical != 1 {
		t.Errorf("an acknowledged real anomaly still makes the meter critical, got %d critical", d.Meters.Critical)
	}

	data.setStatus("M-109", domain.StatusResolved)
	d, _ = svc.Summary(ctx)
	if d.Anomalies.Unresolved != 3 || d.Meters.Critical != 0 {
		t.Errorf("after resolving: %d unresolved, %d critical", d.Anomalies.Unresolved, d.Meters.Critical)
	}
}

func TestAttentionShowsAtMostThreeAndOnlyOpenOnes(t *testing.T) {
	svc, data, _ := dashboardFor(t, true)
	data.setStatus("M-112", domain.StatusDismissed)
	d, _ := svc.Summary(context.Background())
	if got := attentionMeters(d); !slices.Equal(got, []string{"M-109", "M-104", "M-106"}) {
		t.Errorf("attention = %v, want a dismissed anomaly skipped and the list capped at three", got)
	}
}

func TestOnlyACompletedRunCounts(t *testing.T) {
	svc, _, runs := dashboardFor(t, true)
	ctx := context.Background()
	failed, _ := runs.CreateRun(ctx, []byte(`{}`), initialStages())
	_ = runs.FinishRun(ctx, failed.ID, domain.RunFailed, failed.Stages, domain.RunSummary{Error: "boom", Confidence: 0.1})
	if _, err := runs.CreateRun(ctx, []byte(`{}`), initialStages()); err != nil {
		t.Fatal(err)
	}

	d, err := svc.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !d.HasAnalysis || d.Confidence == nil || *d.Confidence != 0.989 || d.LastAnalysis.ID != "run-1" {
		t.Errorf("last analysis %+v, confidence %v; a failed and a pending run must not replace the completed one", d.LastAnalysis, d.Confidence)
	}
}

func TestPlantTotalsAddUpTheMeters(t *testing.T) {
	svc, data, _ := dashboardFor(t, false)
	ctx := context.Background()
	d, _ := svc.Summary(ctx)
	list, _ := data.service().Meters(ctx, MeterFilter{})

	var all float64
	for _, r := range data.ds.Readings {
		all += r.ConsumptionKWh
	}
	if !near(d.ConsumptionKWh, all, 1e-6) {
		t.Errorf("period consumption %.3f, the readings add up to %.3f", d.ConsumptionKWh, all)
	}
	var recent, base float64
	for _, m := range list {
		recent += m.RecentDailyKWh
		base += m.BaselineDailyKWh
	}
	if want := (recent - base) / base * 100; !near(d.VariationPct, want, 1e-9) {
		t.Errorf("variation %.4f, want %.4f", d.VariationPct, want)
	}
	if !(d.VariationPct > 3 && d.VariationPct < 20) {
		t.Errorf("plant variation %.1f%%: M-109 and M-104 push it up but it must stay far below M-109's 110%%", d.VariationPct)
	}
	if !slices.IsSortedFunc(d.Daily, func(a, b DailyKWh) int {
		switch {
		case a.Date < b.Date:
			return -1
		case a.Date > b.Date:
			return 1
		}
		return 0
	}) {
		t.Error("the daily series is not oldest first")
	}
	var last float64
	for _, m := range list {
		last += m.Daily[13].KWh
	}
	if !near(d.Daily[13].KWh, last, 1e-6) {
		t.Errorf("last day %.3f, the meters add up to %.3f", d.Daily[13].KWh, last)
	}
}

func TestAMeterWithoutABaselineDoesNotSkewTheVariation(t *testing.T) {
	svc, data, _ := dashboardFor(t, false)
	before, _ := svc.Summary(context.Background())

	loc := data.ds.Readings[0].Timestamp.Location()
	data.ds.Meters = append(data.ds.Meters, domain.Meter{MeterID: "M-200", Name: "Nuevo", Location: "Bodega"})
	for h := 0; h < 5; h++ {
		data.ds.Readings = append(data.ds.Readings, domain.Reading{
			MeterID: "M-200", Timestamp: time.Date(2026, 9, 14, h, 0, 0, 0, loc), ConsumptionKWh: 900, Status: "OK",
		})
	}
	after, _ := NewDashboardService(data.service(), data, newMemory(t)).Summary(context.Background())
	if after.Meters.Total != 13 {
		t.Fatalf("total = %d, want 13", after.Meters.Total)
	}
	if !near(after.VariationPct, before.VariationPct, 1e-9) {
		t.Errorf("variation moved from %.4f to %.4f because of a meter with no baseline", before.VariationPct, after.VariationPct)
	}
}

type failingRuns struct {
	*memory
	err error
}

func (f failingRuns) LatestCompletedRun(context.Context) (domain.AnalysisRun, error) {
	return domain.AnalysisRun{}, f.err
}

type failingAnomalies struct{ err error }

func (f failingAnomalies) Anomalies(context.Context, domain.AnomalyFilter) ([]domain.Anomaly, error) {
	return nil, f.err
}

func TestDashboardPassesStoreFailuresUp(t *testing.T) {
	_, data, runs := dashboardFor(t, false)
	boom := errors.New("database is down")

	if _, err := NewDashboardService(data.service(), data, failingRuns{runs, boom}).Summary(context.Background()); !errors.Is(err, boom) {
		t.Errorf("runs failure: %v", err)
	}
	if _, err := NewDashboardService(data.service(), failingAnomalies{boom}, runs).Summary(context.Background()); !errors.Is(err, boom) {
		t.Errorf("anomalies failure: %v", err)
	}
}
