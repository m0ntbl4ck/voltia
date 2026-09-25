package detectors_test

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
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

// run applies a detector to every meter over the days after the reference week
// and returns the signals by meter.
func run(t *testing.T, detect func(baseline.Meter, []domain.Reading, detectors.Config) []detectors.Signal) map[string][]detectors.Signal {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "..", "data", "readings.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	all, err := seed.ParseReadings(f, plant)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string][]domain.Reading{}
	for _, r := range all {
		by[r.MeterID] = append(by[r.MeterID], r)
	}
	out := map[string][]detectors.Signal{}
	for meter, readings := range by {
		b, err := baseline.Build(meter, readings, nil, baseline.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		var analysis []domain.Reading
		for _, r := range readings {
			if !r.Timestamp.Before(ts(8, 0)) {
				analysis = append(analysis, r)
			}
		}
		if sigs := detect(b, analysis, detectors.DefaultConfig()); len(sigs) > 0 {
			out[meter] = sigs
		}
	}
	return out
}

func meters(got map[string][]detectors.Signal) []string {
	var names []string
	for m := range got {
		names = append(names, m)
	}
	sort.Strings(names)
	return names
}

func onlyMeters(t *testing.T, got map[string][]detectors.Signal, want ...string) {
	t.Helper()
	names := meters(got)
	if len(names) != len(want) {
		t.Fatalf("signals on meters %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("signals on meters %v, want %v", names, want)
		}
	}
}

// The three meters with a real level change, and how long each one lasts.
func TestDatasetPersistentShifts(t *testing.T) {
	got := run(t, detectors.DetectPersistentShift)
	onlyMeters(t, got, "M-104", "M-106", "M-109")
	type shift struct {
		start      time.Time
		hours, dir int
		open       bool
	}
	want := map[string]map[domain.Variable]shift{
		"M-104": {domain.Consumption: {ts(11, 0), 96, 1, true}, domain.Current: {ts(11, 0), 96, 1, true}},
		"M-106": {domain.Consumption: {ts(8, 0), 12, -1, false}, domain.Current: {ts(8, 0), 12, -1, false}},
		"M-109": {
			domain.Consumption: {ts(12, 14), 58, 1, true},
			domain.Current:     {ts(12, 14), 58, 1, true},
			domain.PowerFactor: {ts(12, 14), 58, -1, true},
		},
	}
	for meter, sigs := range got {
		if len(sigs) != len(want[meter]) {
			t.Errorf("%s: %d signals, want %d", meter, len(sigs), len(want[meter]))
		}
		for _, s := range sigs {
			w, ok := want[meter][s.Variable]
			if !ok {
				t.Errorf("%s: unexpected signal on %s", meter, s.Variable)
				continue
			}
			if !s.Start.Equal(w.start) || s.Hours != w.hours || s.Direction != w.dir || s.Open != w.open {
				t.Errorf("%s %s: got start=%v hours=%d dir=%d open=%v, want %+v", meter, s.Variable, s.Start, s.Hours, s.Direction, s.Open, w)
			}
		}
	}
}

func TestDatasetHasNoSpikes(t *testing.T) {
	if got := run(t, detectors.DetectSpikes); len(got) != 0 {
		t.Errorf("unexpected spikes: %v", meters(got))
	}
}

func TestDatasetPowerFactorDropBelongsToM109(t *testing.T) {
	got := run(t, detectors.DetectElectricalRelation)
	onlyMeters(t, got, "M-109")
	if len(got["M-109"]) != 1 {
		t.Fatalf("got %d signals, want 1", len(got["M-109"]))
	}
	s := got["M-109"][0]
	if !s.Start.Equal(ts(12, 14)) || s.Hours != 58 || s.Direction != -1 {
		t.Errorf("unexpected signal: %+v", s)
	}
}

// The meter whose electrical readings jump while its consumption stays flat.
var m112Hours = func() []time.Time {
	var out []time.Time
	for _, day := range []int{13, 14} {
		for hour := 0; hour < 24; hour += 3 {
			out = append(out, ts(day, hour))
		}
	}
	return out
}()

func TestDatasetDataQualityFindsEveryOddReadingOfM112(t *testing.T) {
	got := run(t, detectors.DetectDataQuality)
	onlyMeters(t, got, "M-112")
	sigs := got["M-112"]
	if len(sigs) != len(m112Hours) {
		t.Fatalf("got %d signals, want %d", len(sigs), len(m112Hours))
	}
	sort.Slice(sigs, func(i, j int) bool { return sigs[i].Start.Before(sigs[j].Start) })
	for i, s := range sigs {
		if s.Check != detectors.CheckElectricalJump || !s.Start.Equal(m112Hours[i]) {
			t.Errorf("signal %d: check=%s start=%v, want %s at %v", i, s.Check, s.Start, detectors.CheckElectricalJump, m112Hours[i])
		}
	}
}

// Outliers are only expected where the readings really are off: M-112's odd
// hours, plus one voltage dip of M-109 under its heavy load.
func TestDatasetOutliers(t *testing.T) {
	got := run(t, detectors.DetectOutliers)
	onlyMeters(t, got, "M-109", "M-112")
	if len(got["M-109"]) != 1 || got["M-109"][0].Variable != domain.Voltage || !got["M-109"][0].Start.Equal(ts(14, 13)) {
		t.Errorf("M-109 outliers: %+v", got["M-109"])
	}
	odd := map[time.Time]bool{}
	for _, h := range m112Hours {
		odd[h] = true
	}
	for _, s := range got["M-112"] {
		if !odd[s.Start] {
			t.Errorf("M-112 outlier outside the odd hours: %v", s.Start)
		}
	}
	if len(got["M-112"]) != 44 {
		t.Errorf("M-112 outliers = %d, want 44", len(got["M-112"]))
	}
}

// Only the two days whose shape broke: the outage of M-106 on the 8th, which
// left the first twelve hours near zero, and the day M-109 started to climb at
// 14:00. The steady rises of M-104 and the rest of M-109 keep the daily shape
// (correlation 0.97 to 0.99), and the healthy days never fall under 0.93.
// Expected values come from an independent numpy computation on the same files.
func TestDatasetHourlyPattern(t *testing.T) {
	got := run(t, detectors.DetectHourlyPattern)
	onlyMeters(t, got, "M-106", "M-109")
	cases := []struct {
		meter              string
		start, end         time.Time
		hours              int
		corr, ratio, base  float64
		observed, expected float64
		meanZ              float64
	}{
		{"M-106", ts(8, 0), ts(8, 11), 12, 0.54066, 0.46396, 0.73907, 10.5333, 52.1917, -15.8408},
		{"M-109", ts(12, 14), ts(12, 23), 10, 0.53411, 0.63421, 0.73590, 97.9600, 46.0370, 21.8155},
	}
	for _, c := range cases {
		t.Run(c.meter, func(t *testing.T) {
			if len(got[c.meter]) != 1 {
				t.Fatalf("got %d signals, want 1", len(got[c.meter]))
			}
			s := got[c.meter][0]
			if s.Check != detectors.CheckDailyCorrelation || !s.Start.Equal(c.start) || !s.End.Equal(c.end) || s.Hours != c.hours {
				t.Errorf("check=%s start=%v end=%v hours=%d, want %s %v to %v (%d hours)", s.Check, s.Start, s.End, s.Hours, detectors.CheckDailyCorrelation, c.start, c.end, c.hours)
			}
			for name, want := range map[string]float64{
				"correlation": c.corr, "night_day_ratio": c.ratio, "baseline_night_day_ratio": c.base,
			} {
				if got := s.Metrics[name]; math.Abs(got-want) > 1e-4 {
					t.Errorf("%s = %.5f, want %.5f", name, got, want)
				}
			}
			for name, pair := range map[string][2]float64{
				"observed": {s.Observed, c.observed}, "expected": {s.Expected, c.expected}, "mean z": {s.MeanZ, c.meanZ},
			} {
				if math.Abs(pair[0]-pair[1]) > 1e-3 {
					t.Errorf("%s = %.4f, want %.4f", name, pair[0], pair[1])
				}
			}
		})
	}
}
