package classify_test

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
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var plant = func() *time.Location {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		panic(err)
	}
	return loc
}()

func ts(day, hour int) time.Time { return time.Date(2026, 9, day, hour, 0, 0, 0, plant) }

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "..", "data", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// classifyDataset runs the whole pipeline over the real files and keeps the
// classification of each episode by meter.
func classifyDataset(t *testing.T) map[string]classify.Result {
	t.Helper()
	readings, err := seed.ParseReadings(open(t, "readings.csv"), plant)
	if err != nil {
		t.Fatal(err)
	}
	events, err := seed.ParseEvents(open(t, "events.csv"), plant)
	if err != nil {
		t.Fatal(err)
	}
	report, err := analysis.Run(context.Background(), analysis.Input{Readings: readings, Events: events}, analysis.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]classify.Result{}
	for _, a := range report.Anomalies {
		if _, dup := out[a.Episode.MeterID]; dup {
			t.Fatalf("%s has more than one episode", a.Episode.MeterID)
		}
		out[a.Episode.MeterID] = a.Result
	}
	return out
}

// The four cases of the challenge, and nothing else: the other eight meters
// produce no episode at all.
func TestDatasetFourCases(t *testing.T) {
	got := classifyDataset(t)
	if len(got) != 4 {
		t.Fatalf("got episodes for %d meters, want 4", len(got))
	}
	cases := []struct {
		meter     string
		typ       domain.AnomalyType
		rule      classify.Rule
		start     time.Time
		end       time.Time
		open      bool
		direction int
		role      classify.EventRole
	}{
		{"M-104", domain.ExplainableAnomaly, classify.RuleOperationalChange, ts(11, 0), ts(14, 23), true, 1, classify.RoleExplains},
		{"M-106", domain.FalsePositive, classify.RuleScheduledOutage, ts(8, 0), ts(8, 11), false, -1, classify.RoleExplains},
		{"M-109", domain.RealAnomaly, classify.RuleNoExplainingEvent, ts(12, 14), ts(14, 23), true, 1, classify.RoleNotExplanatory},
		{"M-112", domain.DataQuality, classify.RuleIsolatedElectricalReadings, ts(13, 0), ts(14, 21), false, 0, classify.RoleCorroborates},
	}
	for _, c := range cases {
		t.Run(c.meter, func(t *testing.T) {
			res, ok := got[c.meter]
			if !ok {
				t.Fatal("no episode")
			}
			if res.Type != c.typ || res.Rule != c.rule {
				t.Errorf("classified %s by %s, want %s by %s", res.Type, res.Rule, c.typ, c.rule)
			}
			ep := res.Episode
			if !ep.Start.Equal(c.start) || !ep.End.Equal(c.end) || ep.Open != c.open || res.Direction != c.direction {
				t.Errorf("start=%v end=%v open=%v direction=%d, want %v %v %v %d", ep.Start, ep.End, ep.Open, res.Direction, c.start, c.end, c.open, c.direction)
			}
			if len(res.Events) != 1 || res.Events[0].Role != c.role || res.Events[0].OffsetHours != 0 {
				t.Errorf("events = %+v, want one %s at offset 0", res.Events, c.role)
			}
		})
	}
}

// M-109 carries the electrical evidence that a matching event could not excuse.
func TestDatasetM109HasElectricalEvidence(t *testing.T) {
	ep := classifyDataset(t)["M-109"].Episode
	if !ep.Has(detectors.KindElectricalRelation) {
		t.Error("M-109 should carry a power factor drop")
	}
	if ep.Has(detectors.KindDataQuality) {
		t.Error("M-109 should carry no data quality signal")
	}
}
