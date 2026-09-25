package app

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/data"
	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/analysis"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var (
	datasetOnce   sync.Once
	datasetReport analysis.Report
	datasetMeters map[string]domain.Meter
	datasetErr    error
)

// datasetRun runs the engine over the shipped dataset once for all the tests.
func datasetRun(t *testing.T) (analysis.Report, map[string]domain.Meter) {
	t.Helper()
	datasetOnce.Do(func() { datasetReport, datasetMeters, datasetErr = runDataset() })
	if datasetErr != nil {
		t.Fatal(datasetErr)
	}
	return datasetReport, datasetMeters
}

func runDataset() (analysis.Report, map[string]domain.Meter, error) {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		return analysis.Report{}, nil, err
	}
	ds, err := seed.Load(data.FS, loc)
	if err != nil {
		return analysis.Report{}, nil, err
	}
	report, err := analysis.Run(context.Background(),
		analysis.Input{Readings: ds.Readings, Events: ds.Events}, analysis.DefaultConfig(), nil)
	if err != nil {
		return analysis.Report{}, nil, err
	}
	meters := map[string]domain.Meter{}
	for _, m := range ds.Meters {
		meters[m.MeterID] = m
	}
	return report, meters, nil
}

func TestBuildEvidenceForTheUnexplainedSurge(t *testing.T) {
	report, meters := datasetRun(t)
	a := report.Anomalies[0]
	if a.Episode.MeterID != "M-109" {
		t.Fatalf("first anomaly is %s, want M-109", a.Episode.MeterID)
	}

	ev := BuildEvidence(a, meters["M-109"])
	if ev.MeterName != meters["M-109"].Name || ev.Location != meters["M-109"].Location {
		t.Errorf("meter name and location not carried: %q, %q", ev.MeterName, ev.Location)
	}
	if ev.Type != domain.RealAnomaly || ev.Severity != domain.SeverityHigh || ev.Direction != "UP" {
		t.Errorf("type %s, severity %s, direction %s", ev.Type, ev.Severity, ev.Direction)
	}
	if math.Abs(ev.VariationPct-110) > 5 {
		t.Errorf("variation = %.1f%%, want about 110%%", ev.VariationPct)
	}
	if ev.EpisodeStart.Location() != time.UTC || !ev.EpisodeStart.Equal(a.Episode.Start) {
		t.Errorf("episode start = %v, want %v in UTC", ev.EpisodeStart, a.Episode.Start)
	}
	if want := int(a.Episode.Duration().Hours()); ev.DurationH != want {
		t.Errorf("duration = %d h, want %d", ev.DurationH, want)
	}
	if len(ev.Signals) == 0 || len(ev.Signals) != len(a.Episode.Signals) {
		t.Errorf("%d signals for %d in the episode", len(ev.Signals), len(a.Episode.Signals))
	}
	forest := false
	for _, sig := range ev.Signals {
		if sig.Kind == "ISOLATION_FOREST" {
			forest = len(sig.Attribution) > 0
		}
	}
	if !forest {
		t.Error("the isolation forest signal lost its attribution")
	}
	if len(ev.Events) != 1 || ev.Events[0].Type != domain.EventUnknown || ev.Events[0].Role != "NOT_EXPLANATORY" {
		t.Errorf("events = %+v, want the one UNKNOWN event that explains nothing", ev.Events)
	}
}

func TestBuildEvidenceForTheScheduledOutage(t *testing.T) {
	report, meters := datasetRun(t)
	var a analysis.Anomaly
	for _, x := range report.Anomalies {
		if x.Episode.MeterID == "M-106" {
			a = x
		}
	}
	ev := BuildEvidence(a, meters["M-106"])
	if ev.Direction != "DOWN" || ev.Type != domain.FalsePositive {
		t.Errorf("direction %s, type %s", ev.Direction, ev.Type)
	}
	if len(ev.Events) != 1 || ev.Events[0].DurationH != 12 || ev.Events[0].Role != "EXPLAINS" {
		t.Errorf("events = %+v, want the 12 h outage that explains it", ev.Events)
	}
}

func TestEvidenceJSONIsSnakeCase(t *testing.T) {
	report, meters := datasetRun(t)
	raw, err := json.Marshal(BuildEvidence(report.Anomalies[0], meters["M-109"]))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"meter_id"`, `"episode_start"`, `"variation_pct"`, `"duration_hours"`, `"mean_z"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("evidence JSON lacks %s", key)
		}
	}
	var back domain.Evidence
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.MeterID != "M-109" || len(back.Signals) == 0 {
		t.Errorf("round trip lost data: %+v", back)
	}
}

func TestBuildAnomalyCarriesTheScoreAndAStableFingerprint(t *testing.T) {
	report, meters := datasetRun(t)
	exp := domain.Explanation{Reason: "r", RecommendedAction: "a", Source: domain.SourceTemplate}
	seen := map[string]bool{}
	for _, a := range report.Anomalies {
		got, err := BuildAnomaly(a, meters[a.Episode.MeterID], exp, "run-1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Priority != a.Score.Priority || got.Confidence != a.Score.Confidence || got.Severity != a.Score.Severity {
			t.Errorf("%s: score not carried", got.MeterID)
		}
		if !got.EpisodeStart.Equal(a.Episode.Start) || !got.EpisodeEnd.Equal(a.Episode.End) || got.Ongoing != a.Episode.Open {
			t.Errorf("%s: episode %v to %v ongoing %v", got.MeterID, got.EpisodeStart, got.EpisodeEnd, got.Ongoing)
		}
		if got.Status != domain.StatusOpen || got.LastAnalysisID != "run-1" {
			t.Errorf("%s: status %s, run %s", got.MeterID, got.Status, got.LastAnalysisID)
		}
		var parts map[string]any
		if err := json.Unmarshal(got.ConfidenceBreakdown, &parts); err != nil || parts["detector_agreement"] == nil {
			t.Errorf("%s: confidence breakdown = %s (%v)", got.MeterID, got.ConfidenceBreakdown, err)
		}
		var pb map[string]any
		if err := json.Unmarshal(got.PriorityBreakdown, &pb); err != nil || pb["impact"] == nil {
			t.Errorf("%s: priority breakdown = %s (%v)", got.MeterID, got.PriorityBreakdown, err)
		}
		if seen[got.Fingerprint] {
			t.Errorf("fingerprint %q repeats", got.Fingerprint)
		}
		seen[got.Fingerprint] = true
	}
	if len(seen) != 4 {
		t.Errorf("%d anomalies, want 4", len(seen))
	}
}

func TestFingerprintChangesWithEachPart(t *testing.T) {
	start := time.Date(2026, 9, 12, 14, 0, 0, 0, time.UTC)
	base := Fingerprint("M-109", domain.RealAnomaly, start)
	if base != Fingerprint("M-109", domain.RealAnomaly, start.In(time.FixedZone("x", -5*3600))) {
		t.Error("the same instant in another zone gives another fingerprint")
	}
	for name, other := range map[string]string{
		"meter": Fingerprint("M-104", domain.RealAnomaly, start),
		"type":  Fingerprint("M-109", domain.DataQuality, start),
		"start": Fingerprint("M-109", domain.RealAnomaly, start.Add(time.Hour)),
	} {
		if other == base {
			t.Errorf("changing the %s keeps the fingerprint", name)
		}
	}
}
