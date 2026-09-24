package detectors

import (
	"math"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// profile is the shape every reference day has: 100 kWh on average, a peak of
// 140 at 14:00 and a trough of 60 at 02:00. The seven identical days leave the
// median on the shape and the sigma at its 5% floor.
func profile(h int) float64 { return 100 + 40*math.Sin(2*math.Pi*float64(h-8)/24) }

func shapedMeter(t *testing.T) baseline.Meter {
	t.Helper()
	var ref []domain.Reading
	for d := 0; d < 7; d++ {
		for h := 0; h < 24; h++ {
			r := flatReading(day0.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour))
			r.ConsumptionKWh = profile(h)
			ref = append(ref, r)
		}
	}
	m, err := baseline.Build("M-TEST", ref, nil, baseline.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// days lays out consecutive days after the reference week. kwh gives the
// consumption of each day and hour.
func days(n int, kwh func(day, hour int) float64) []domain.Reading {
	return window(24*n, func(i int, r *domain.Reading) { r.ConsumptionKWh = kwh(i/24, i%24) })
}

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v (+-%v)", name, got, want, tol)
	}
}

func night(h int) bool { return h >= 22 || h < 6 }

// All expected values below come from an independent numpy computation of the
// same day against the same profile.
func TestPatternIgnoresALevelShiftThatKeepsTheShape(t *testing.T) {
	r := days(2, func(d, h int) float64 { return profile(h) * 1.3 })
	expect(t, DetectHourlyPattern(shapedMeter(t), r, DefaultConfig()), 0)
}

func TestPatternFlagsAScheduleShiftedBySixHours(t *testing.T) {
	r := days(1, func(d, h int) float64 { return profile(h - 6) })
	got := DetectHourlyPattern(shapedMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	s := got[0]
	if s.Kind != KindHourlyPattern || s.Check != CheckDailyCorrelation || s.Variable != domain.Consumption || s.Direction != 0 {
		t.Errorf("unexpected signal: %+v", s)
	}
	if !s.Start.Equal(at(0)) || !s.End.Equal(at(23)) || s.Hours != 24 || !s.Open {
		t.Errorf("start=%v end=%v hours=%d open=%v, want the whole last day", s.Start, s.End, s.Hours, s.Open)
	}
	near(t, "observed", s.Observed, 100, 1e-6)
	near(t, "expected", s.Expected, 100, 1e-6)
	near(t, "mean z", s.MeanZ, 1.821789, 1e-5)
	near(t, "correlation", s.Metrics["correlation"], 0, 1e-9)
	near(t, "night/day ratio", s.Metrics["night_day_ratio"], 1.066389, 1e-5)
	near(t, "baseline ratio", s.Metrics["baseline_night_day_ratio"], 0.576317, 1e-5)
}

// The night runs 30% above its normal level: the shape still correlates at
// 0.9637, but the night to day ratio goes from 0.5763 to 0.7492, 30% away.
func TestPatternFlagsAChangedNightDayRatio(t *testing.T) {
	r := days(1, func(d, h int) float64 {
		if night(h) {
			return profile(h) * 1.3
		}
		return profile(h)
	})
	got := DetectHourlyPattern(shapedMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	s := got[0]
	if s.Check != CheckNightDayRatio || s.Hours != 24 {
		t.Errorf("check=%s hours=%d, want the ratio check over the whole day", s.Check, s.Hours)
	}
	near(t, "correlation", s.Metrics["correlation"], 0.963675, 1e-5)
	near(t, "night/day ratio", s.Metrics["night_day_ratio"], 0.749212, 1e-5)
	near(t, "observed", s.Observed, 106.710942, 1e-5)
	near(t, "mean z", s.MeanZ, 2, 1e-9)
}

// A quiet night is as much a change of pattern as a busy one: at 70% the
// correlation is still 0.9876, and the ratio falls from 0.5763 to 0.4034, 30% below.
func TestPatternFlagsANightThatFellBelowItsRatio(t *testing.T) {
	r := days(1, func(d, h int) float64 {
		if night(h) {
			return profile(h) * 0.7
		}
		return profile(h)
	})
	got := DetectHourlyPattern(shapedMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	if got[0].Check != CheckNightDayRatio {
		t.Errorf("check = %s, want %s", got[0].Check, CheckNightDayRatio)
	}
	near(t, "correlation", got[0].Metrics["correlation"], 0.987586, 1e-5)
	near(t, "night/day ratio", got[0].Metrics["night_day_ratio"], 0.403422, 1e-5)
}

// At 1.6 the correlation falls to 0.7501, under 0.8, so that check names the signal.
func TestPatternNamesTheCorrelationWhenBothChecksFail(t *testing.T) {
	r := days(1, func(d, h int) float64 {
		if night(h) {
			return profile(h) * 1.6
		}
		return profile(h)
	})
	got := DetectHourlyPattern(shapedMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	if got[0].Check != CheckDailyCorrelation {
		t.Errorf("check = %s, want %s", got[0].Check, CheckDailyCorrelation)
	}
	near(t, "correlation", got[0].Metrics["correlation"], 0.750136, 1e-5)
}

// Only the first twelve hours drop to 20%, like a meter stopped for half a day.
// The signal covers those hours and not the rest of the day.
func TestPatternCoversOnlyTheHoursThatLeftTheBaseline(t *testing.T) {
	r := days(1, func(d, h int) float64 {
		if h < 12 {
			return profile(h) * 0.2
		}
		return profile(h)
	})
	got := DetectHourlyPattern(shapedMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	s := got[0]
	if !s.Start.Equal(at(0)) || !s.End.Equal(at(11)) || s.Hours != 12 || s.Open {
		t.Errorf("start=%v end=%v hours=%d open=%v, want 00:00 to 11:00 and closed", s.Start, s.End, s.Hours, s.Open)
	}
	near(t, "observed", s.Observed, 16.890732, 1e-5)
	near(t, "expected", s.Expected, 84.453658, 1e-5)
	near(t, "mean z", s.MeanZ, -16, 1e-9)
	near(t, "correlation", s.Metrics["correlation"], 0.746468, 1e-5)
}

// When no hour is far from the baseline on its own, the whole day is reported.
// A sigma of 1000 keeps every z near zero while the day is upside down.
func TestPatternCoversTheWholeDayWhenNoSingleHourStandsOut(t *testing.T) {
	var p baseline.Profile
	for h := range p {
		p[h] = baseline.HourStat{Median: profile(h), Sigma: 1000, Samples: 7}
	}
	m := baseline.Meter{MeterID: "M-TEST", Profiles: map[domain.Variable]baseline.Profile{domain.Consumption: p}}
	r := days(1, func(d, h int) float64 { return 200 - profile(h) })
	got := DetectHourlyPattern(m, r, DefaultConfig())
	expect(t, got, 1)
	if got[0].Hours != 24 || !got[0].Start.Equal(at(0)) || !got[0].End.Equal(at(23)) {
		t.Errorf("hours=%d start=%v end=%v, want the whole day", got[0].Hours, got[0].Start, got[0].End)
	}
	near(t, "correlation", got[0].Metrics["correlation"], -1, 1e-9)
}

func TestPatternFlagsOnlyTheDayThatChanged(t *testing.T) {
	r := days(3, func(d, h int) float64 {
		if d == 1 {
			return profile(h - 6)
		}
		return profile(h)
	})
	got := DetectHourlyPattern(shapedMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	if !got[0].Start.Equal(at(24)) || !got[0].End.Equal(at(47)) || got[0].Open {
		t.Errorf("start=%v end=%v open=%v, want the second day, closed", got[0].Start, got[0].End, got[0].Open)
	}
}

func TestPatternSkipsIncompleteDays(t *testing.T) {
	shifted := days(1, func(d, h int) float64 { return profile(h - 6) })
	expect(t, DetectHourlyPattern(shapedMeter(t), shifted[:23], DefaultConfig()), 0)

	gap := append(append([]domain.Reading{}, shifted[:10]...), shifted[11:]...)
	expect(t, DetectHourlyPattern(shapedMeter(t), gap, DefaultConfig()), 0)

	// 24 readings on the day, but 05:00 twice and no 10:00: still not a day.
	duplicated := append([]domain.Reading{}, gap...)
	duplicated = append(duplicated, shifted[5])
	expect(t, DetectHourlyPattern(shapedMeter(t), duplicated, DefaultConfig()), 0)
}

// A flat baseline has no shape to correlate with, so only the ratio can speak.
func TestPatternWithAFlatBaselineUsesTheRatioOnly(t *testing.T) {
	m := testMeter(t)
	ordinary := days(1, func(d, h int) float64 { return 100 })
	expect(t, DetectHourlyPattern(m, ordinary, DefaultConfig()), 0)

	tripledNights := days(1, func(d, h int) float64 {
		if night(h) {
			return 300
		}
		return 100
	})
	got := DetectHourlyPattern(m, tripledNights, DefaultConfig())
	expect(t, got, 1)
	if _, ok := got[0].Metrics["correlation"]; ok {
		t.Error("the correlation with a flat profile is undefined and must not be reported")
	}
	near(t, "night/day ratio", got[0].Metrics["night_day_ratio"], 3, 1e-9)
	if got[0].Check != CheckNightDayRatio {
		t.Errorf("check = %s, want %s", got[0].Check, CheckNightDayRatio)
	}
}

// An hour exactly three sigmas away is not off the pattern: with a flat
// baseline of 100 and sigma 5, hours 10 to 15 at 115 sit on the line. A tight
// ratio tolerance still flags the day, and with no hour beyond the line the
// whole day is reported.
func TestPatternHourThresholdIsStrict(t *testing.T) {
	r := days(1, func(d, h int) float64 {
		if h >= 10 && h <= 15 {
			return 115
		}
		return 100
	})
	cfg := DefaultConfig()
	cfg.NightDayTolerance = 0.01
	got := DetectHourlyPattern(testMeter(t), r, cfg)
	expect(t, got, 1)
	if got[0].Hours != 24 {
		t.Errorf("hours = %d, want the whole day: no hour is beyond 3 sigma", got[0].Hours)
	}
}

// A day without variation has no correlation and a dead meter has no ratio:
// the level detectors own that.
func TestPatternStaysSilentOnADayWithoutConsumption(t *testing.T) {
	r := days(1, func(d, h int) float64 { return 0 })
	expect(t, DetectHourlyPattern(shapedMeter(t), r, DefaultConfig()), 0)
}

// The thresholds are strict: a day exactly on the line is not flagged.
func TestPatternThresholdsAreStrict(t *testing.T) {
	m := shapedMeter(t)
	r := days(1, func(d, h int) float64 {
		if night(h) {
			return profile(h) * 1.3
		}
		return profile(h)
	})
	var day, prof [24]float64
	for h := 0; h < 24; h++ {
		day[h], prof[h] = r[h].ConsumptionKWh, profile(h)
	}

	corr, _ := pearson(day[:], prof[:])
	cfg := DefaultConfig()
	cfg.NightDayTolerance = 10 // silence the ratio
	cfg.PatternMinCorrelation = corr
	expect(t, DetectHourlyPattern(m, r, cfg), 0)
	cfg.PatternMinCorrelation = math.Nextafter(corr, 2)
	expect(t, DetectHourlyPattern(m, r, cfg), 1)

	ratio, _ := nightDayRatio(day, DefaultConfig())
	base, _ := nightDayRatio(prof, DefaultConfig())
	cfg = DefaultConfig()
	cfg.PatternMinCorrelation = -2 // silence the correlation
	cfg.NightDayTolerance = math.Abs(ratio/base - 1)
	expect(t, DetectHourlyPattern(m, r, cfg), 0)
	cfg.NightDayTolerance = math.Nextafter(cfg.NightDayTolerance, 0)
	expect(t, DetectHourlyPattern(m, r, cfg), 1)
}

func TestPearson(t *testing.T) {
	got, ok := pearson([]float64{1, 2, 3, 4}, []float64{2, 4, 5, 9})
	if !ok {
		t.Fatal("expected a correlation")
	}
	near(t, "correlation", got, 0.9647638212377321, 1e-12)
	got, _ = pearson([]float64{1, 2, 3}, []float64{2, 4, 6})
	near(t, "same shape", got, 1, 1e-12)
	got, _ = pearson([]float64{1, 2, 3}, []float64{6, 4, 2})
	near(t, "opposite shape", got, -1, 1e-12)
	if _, ok := pearson([]float64{5, 5, 5}, []float64{1, 2, 3}); ok {
		t.Error("a series without variation has no correlation")
	}
	if _, ok := pearson([]float64{1, 2, 3}, []float64{7, 7, 7}); ok {
		t.Error("a series without variation has no correlation")
	}
}

func TestNightDayRatioWrapsMidnight(t *testing.T) {
	var hours [24]float64
	for h := range hours {
		hours[h] = 100
		if night(h) {
			hours[h] = 50
		}
	}
	got, ok := nightDayRatio(hours, DefaultConfig())
	if !ok {
		t.Fatal("expected a ratio")
	}
	near(t, "ratio", got, 0.5, 1e-12)

	// A window that does not wrap: the night is the morning, hours 0 to 11.
	// It averages (6*50 + 6*100)/12 = 75 against (10*100 + 2*50)/12 = 91.667 for
	// hours 12 to 23.
	cfg := DefaultConfig()
	cfg.NightStartHour, cfg.NightEndHour = 0, 12
	got, _ = nightDayRatio(hours, cfg)
	near(t, "custom night", got, 75/(1100.0/12), 1e-12)

	if _, ok := nightDayRatio([24]float64{}, DefaultConfig()); ok {
		t.Error("a day without consumption has no ratio")
	}
}
