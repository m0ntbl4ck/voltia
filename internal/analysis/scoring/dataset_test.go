package scoring_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/analysis"
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

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "..", "data", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// scoreDataset runs the whole pipeline over the real files and keeps the score
// of each meter's episode.
func scoreDataset(t *testing.T) map[string]scoring.Score {
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
	out := map[string]scoring.Score{}
	for _, a := range report.Anomalies {
		out[a.Episode.MeterID] = a.Score
	}
	return out
}

// Hand-computed from the episode evidence: M-109 rises 110.5% with 2825 kWh of
// excess, M-104 rises 46.5% with 2178 kWh, M-112 has 16 invalid readings and
// M-106 lost 80% of its consumption for 12 hours.
func TestDatasetScores(t *testing.T) {
	got := scoreDataset(t)
	if len(got) != 4 {
		t.Fatalf("got scores for %d meters, want 4", len(got))
	}
	cases := []struct {
		meter      string
		severity   domain.Severity
		priority   int
		confidence float64
		band       scoring.Band
	}{
		{"M-109", domain.SeverityHigh, 100, 0.9875, scoring.BandHigh},
		{"M-112", domain.SeverityHigh, 65, 0.9028, scoring.BandHigh},
		{"M-104", domain.SeverityMedium, 53, 0.9125, scoring.BandHigh},
		{"M-106", domain.SeverityLow, 5, 0.9125, scoring.BandHigh},
	}
	for _, c := range cases {
		t.Run(c.meter, func(t *testing.T) {
			s := got[c.meter]
			if s.Severity != c.severity || s.Priority != c.priority || s.ConfidenceBand != c.band {
				t.Errorf("severity=%s priority=%d band=%s, want %s %d %s", s.Severity, s.Priority, s.ConfidenceBand, c.severity, c.priority, c.band)
			}
			if d := s.Confidence - c.confidence; d > 0.001 || d < -0.001 {
				t.Errorf("confidence = %.4f, want %.4f", s.Confidence, c.confidence)
			}
		})
	}
}

// The meter that matters most has to come out first.
func TestDatasetM109IsTheFirstToInvestigate(t *testing.T) {
	got := scoreDataset(t)
	meters := make([]string, 0, len(got))
	for m := range got {
		meters = append(meters, m)
	}
	sort.Slice(meters, func(i, j int) bool { return got[meters[i]].Priority > got[meters[j]].Priority })
	want := []string{"M-109", "M-112", "M-104", "M-106"}
	for i := range want {
		if meters[i] != want[i] {
			t.Fatalf("priority order %v, want %v", meters, want)
		}
	}
}

func TestDatasetAggregateConfidence(t *testing.T) {
	got := scoreDataset(t)
	var scores []scoring.Score
	for _, s := range got {
		scores = append(scores, s)
	}
	agg := scoring.AggregateConfidence(scores, scoring.DefaultConfidenceConfig())
	if agg < 0.93 || agg > 0.94 {
		t.Errorf("aggregate confidence = %.4f, want about 0.934", agg)
	}
}
