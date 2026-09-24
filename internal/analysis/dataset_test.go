package analysis_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/analysis"
	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/analysis/iforest"
	"github.com/m0ntbl4ck/voltia/internal/analysis/scoring"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var plant = func() *time.Location {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		panic(err)
	}
	return loc
}()

func loadInput(t *testing.T) analysis.Input {
	t.Helper()
	open := func(name string) *os.File {
		f, err := os.Open(filepath.Join("..", "..", "data", name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	readings, err := seed.ParseReadings(open("readings.csv"), plant)
	if err != nil {
		t.Fatal(err)
	}
	events, err := seed.ParseEvents(open("events.csv"), plant)
	if err != nil {
		t.Fatal(err)
	}
	return analysis.Input{Readings: readings, Events: events}
}

func runDataset(t *testing.T) analysis.Report {
	t.Helper()
	report, err := analysis.Run(context.Background(), loadInput(t), analysis.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// Expected values are the ones computed by hand for each piece: M-109 rises
// 110.5% with 2825 kWh of excess and no event that explains it, M-104 rises
// 46.5% after a new production line, M-112 has 16 invalid readings and M-106
// lost 80% of its consumption during a scheduled outage. The isolation forest
// backs all four up, which gives M-104, M-106 and M-112 three independent
// sources and takes their confidence to the 0.99 ceiling; M-109 had that many
// already and stays at 0.9875.
func TestDatasetRegression(t *testing.T) {
	report := runDataset(t)
	if len(report.Failures) != 0 {
		t.Fatalf("failures: %v", report.Failures)
	}
	want := []struct {
		meter      string
		typ        domain.AnomalyType
		rule       classify.Rule
		severity   domain.Severity
		priority   int
		confidence float64
	}{
		{"M-109", domain.RealAnomaly, classify.RuleNoExplainingEvent, domain.SeverityHigh, 100, 0.9875},
		{"M-112", domain.DataQuality, classify.RuleIsolatedElectricalReadings, domain.SeverityHigh, 65, 0.99},
		{"M-104", domain.ExplainableAnomaly, classify.RuleOperationalChange, domain.SeverityMedium, 53, 0.99},
		{"M-106", domain.FalsePositive, classify.RuleScheduledOutage, domain.SeverityLow, 5, 0.99},
	}
	if len(report.Anomalies) != len(want) {
		t.Fatalf("got %d anomalies, want %d", len(report.Anomalies), len(want))
	}
	for i, w := range want {
		a := report.Anomalies[i]
		if a.Episode.MeterID != w.meter || a.Type != w.typ || a.Rule != w.rule ||
			a.Score.Severity != w.severity || a.Score.Priority != w.priority {
			t.Errorf("#%d = %s %s %s %s %d, want %s %s %s %s %d", i,
				a.Episode.MeterID, a.Type, a.Rule, a.Score.Severity, a.Score.Priority,
				w.meter, w.typ, w.rule, w.severity, w.priority)
		}
		if d := a.Score.Confidence - w.confidence; d > 0.001 || d < -0.001 {
			t.Errorf("%s confidence = %.4f, want %.4f", w.meter, a.Score.Confidence, w.confidence)
		}
		if a.Score.ConfidenceBand != scoring.BandHigh {
			t.Errorf("%s band = %s, want %s", w.meter, a.Score.ConfidenceBand, scoring.BandHigh)
		}
	}
}

func TestDatasetAggregateConfidence(t *testing.T) {
	// Weights 3, 3, 2 and 1 by severity: (3*0.9875 + 3*0.99 + 2*0.99 + 0.99) / 9.
	if got := runDataset(t).Confidence; got < 0.9891 || got > 0.9893 {
		t.Errorf("aggregate confidence = %.4f, want about 0.9892", got)
	}
}

// The eight healthy meters produce no episode, so nothing is raised for them.
func TestDatasetHealthyMetersStaySilent(t *testing.T) {
	flagged := map[string]bool{"M-109": true, "M-112": true, "M-104": true, "M-106": true}
	for _, a := range runDataset(t).Anomalies {
		if !flagged[a.Episode.MeterID] {
			t.Errorf("unexpected anomaly on %s: %s", a.Episode.MeterID, a.Type)
		}
	}
}

func TestDatasetReportsEveryStageInOrder(t *testing.T) {
	var got []analysis.Progress
	_, err := analysis.Run(context.Background(), loadInput(t), analysis.DefaultConfig(), func(p analysis.Progress) {
		got = append(got, p)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2*len(analysis.Stages) {
		t.Fatalf("got %d progress calls, want %d: %v", len(got), 2*len(analysis.Stages), got)
	}
	for i, stage := range analysis.Stages {
		if got[2*i] != (analysis.Progress{Stage: stage}) || got[2*i+1] != (analysis.Progress{Stage: stage, Done: true}) {
			t.Errorf("calls %d and %d = %v %v, want %s start then done", 2*i, 2*i+1, got[2*i], got[2*i+1], stage)
		}
	}
}

// What the forest sees in each case. The exact number of unusual hours still
// moves a little with the seed (across 20 seeds M-104 runs from 93 to 96 of 96
// hours, M-106 from 7 to 12 of 12 and M-112 from 23 to 26 of 46), so only what
// holds for every one of them is asserted: a minimum share of unusual hours, a
// high top score, voltage left out where it did not move, current on top for
// M-104 and the load taking most of M-106. An independent numpy run on the
// same files (2000 trees) gives the same picture.
func TestDatasetIsolationForestBacksUpTheFourCases(t *testing.T) {
	report := runDataset(t)
	cases := []struct {
		meter    string
		minShare float64
	}{
		{"M-109", 0.9},
		{"M-104", 0.9},
		{"M-106", 0.5},
		{"M-112", 0.45},
	}
	for _, c := range cases {
		t.Run(c.meter, func(t *testing.T) {
			var ep classify.Episode
			var found []detectors.Signal
			for _, a := range report.Anomalies {
				if a.Episode.MeterID != c.meter {
					continue
				}
				ep = a.Episode
				for _, s := range a.Episode.Signals {
					if s.Kind == detectors.KindIsolationForest {
						found = append(found, s)
					}
				}
			}
			if len(found) != 1 {
				t.Fatalf("got %d forest signals, want 1", len(found))
			}
			sig := found[0]
			if want := c.minShare * ep.Duration().Hours(); float64(sig.Hours) < want {
				t.Errorf("%d unusual hours of %.0f, want at least %.0f%%", sig.Hours, ep.Duration().Hours(), c.minShare*100)
			}
			if sig.Observed < 0.66 || sig.Observed > 0.8 {
				t.Errorf("highest score = %.3f, want between 0.66 and 0.8", sig.Observed)
			}
			var total float64
			for _, v := range iforest.Features {
				total += sig.Attribution[v]
			}
			if d := total - 1; d > 1e-9 || d < -1e-9 {
				t.Errorf("attribution adds up to %v, want 1", total)
			}
			if c.meter != "M-112" && sig.Attribution[domain.Voltage] > 0.2 {
				t.Errorf("voltage share = %.2f, want at most 0.2: voltage did not move", sig.Attribution[domain.Voltage])
			}
			switch c.meter {
			case "M-104":
				if sig.Variable != domain.Current {
					t.Errorf("top variable = %s, want current_a", sig.Variable)
				}
			case "M-106":
				if load := sig.Attribution[domain.Consumption] + sig.Attribution[domain.Current]; load < 0.5 {
					t.Errorf("consumption and current share %.2f together, want at least 0.5: the load is what dropped", load)
				}
			}
		})
	}
}
