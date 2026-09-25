package baseline

import (
	"math"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var day0 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// series builds one reading per hour for the given number of days. kwh
// receives the day index and the hour of day.
func series(start time.Time, days int, kwh func(day, hour int) float64) []domain.Reading {
	var out []domain.Reading
	for d := 0; d < days; d++ {
		for h := 0; h < 24; h++ {
			out = append(out, domain.Reading{
				MeterID:        "M-TEST",
				Timestamp:      start.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour),
				ConsumptionKWh: kwh(d, h),
				VoltageV:       220,
				CurrentA:       100,
				PowerFactor:    0.9,
			})
		}
	}
	return out
}

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v (+-%v)", name, got, want, tol)
	}
}

func TestBuildComputesMedianAndSigma(t *testing.T) {
	readings := series(day0, 7, func(d, h int) float64 {
		if h == 0 {
			return float64(10 + d) // 10..16: median 13, MAD 2
		}
		return 5
	})
	m, err := Build("M-TEST", readings, nil, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := m.Profiles[domain.Consumption]
	near(t, "hour 0 median", p[0].Median, 13, 1e-9)
	near(t, "hour 0 sigma", p[0].Sigma, 2*madToSigma, 1e-9)
	if p[0].Samples != 7 {
		t.Errorf("hour 0 samples = %d, want 7", p[0].Samples)
	}
}

func TestBuildAppliesSigmaFloor(t *testing.T) {
	readings := series(day0, 7, func(_, _ int) float64 { return 20 })
	m, err := Build("M-TEST", readings, nil, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	near(t, "sigma", m.Profiles[domain.Consumption][5].Sigma, 0.05*20, 1e-9)
}

func TestBuildIgnoresReadingsAfterReferenceWindow(t *testing.T) {
	readings := series(day0, 10, func(d, _ int) float64 {
		if d >= 7 {
			return 1000
		}
		return 10
	})
	m, err := Build("M-TEST", readings, nil, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := m.Profiles[domain.Consumption]
	near(t, "median", p[12].Median, 10, 1e-9)
	if p[12].Samples != 7 {
		t.Errorf("samples = %d, want 7", p[12].Samples)
	}
}

func TestBuildSkipsExcludedReadings(t *testing.T) {
	readings := series(day0, 7, func(_, _ int) float64 { return 10 })
	skip := func(r domain.Reading) bool {
		return r.Timestamp.Day() == 1 && r.Timestamp.Hour() == 0
	}
	m, err := Build("M-TEST", readings, skip, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := m.Profiles[domain.Consumption]
	if p[0].Samples != 6 || p[1].Samples != 7 {
		t.Errorf("samples hour0=%d hour1=%d, want 6 and 7", p[0].Samples, p[1].Samples)
	}
}

func TestBuildFailsWhenAnHourHasNoSamples(t *testing.T) {
	readings := series(day0, 7, func(_, _ int) float64 { return 10 })[:12]
	if _, err := Build("M-TEST", readings, nil, DefaultConfig()); err == nil {
		t.Fatal("expected an error for missing hours")
	}
}

func TestBuildFailsWithoutReadings(t *testing.T) {
	if _, err := Build("M-TEST", nil, nil, DefaultConfig()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestBuildUsesTheReadingsOwnHourOfDay(t *testing.T) {
	cot := time.FixedZone("COT", -5*3600)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, cot)
	readings := series(start, 7, func(_, h int) float64 { return float64(h) })
	m, err := Build("M-TEST", readings, nil, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	near(t, "hour 3 median", m.Profiles[domain.Consumption][3].Median, 3, 1e-9)
}

func TestProfileZ(t *testing.T) {
	var p Profile
	p[0] = HourStat{Median: 13, Sigma: 2, Samples: 7}
	near(t, "z above", p.Z(0, 17), 2, 1e-9)
	near(t, "z below", p.Z(0, 9), -2, 1e-9)
	near(t, "z without sigma", p.Z(1, 50), 0, 1e-9)
}

func TestDailyKWhSumsTheHourlyMedians(t *testing.T) {
	readings := series(day0, 7, func(_, _ int) float64 { return 5 })
	m, err := Build("M-TEST", readings, nil, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	near(t, "daily", m.DailyKWh(), 120, 1e-9)
}

func TestRecentDailyKWhUsesTheLastCompleteDays(t *testing.T) {
	readings := series(day0, 4, func(d, _ int) float64 { return float64(d + 1) }) // days total 24, 48, 72, 96
	partial := series(day0.AddDate(0, 0, 4), 1, func(_, _ int) float64 { return 1000 })[:10]
	got, err := RecentDailyKWh(append(readings, partial...), 2)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "recent", got, 84, 1e-9)
}

func TestRecentDailyKWhFailsWithTooFewCompleteDays(t *testing.T) {
	readings := series(day0, 1, func(_, _ int) float64 { return 1 })
	if _, err := RecentDailyKWh(readings, 2); err == nil {
		t.Fatal("expected an error")
	}
}

func TestVariationPct(t *testing.T) {
	near(t, "increase", VariationPct(110, 100), 10, 1e-9)
	near(t, "decrease", VariationPct(90, 100), -10, 1e-9)
	near(t, "zero base", VariationPct(5, 0), 0, 1e-9)
}

func TestBuildAppliesTheSigmaFloorPerVariable(t *testing.T) {
	readings := series(day0, 7, func(_, _ int) float64 { return 20 })
	m, err := Build("M-TEST", readings, nil, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	near(t, "voltage sigma", m.Profiles[domain.Voltage][0].Sigma, 0.005*220, 1e-9)
	near(t, "current sigma", m.Profiles[domain.Current][0].Sigma, 0.05*100, 1e-9)
	near(t, "power factor sigma", m.Profiles[domain.PowerFactor][0].Sigma, 0.02*0.9, 1e-9)
}

func TestMeterZUsesTheReadingsHour(t *testing.T) {
	readings := series(day0, 7, func(_, h int) float64 { return float64(100 + h) })
	m, err := Build("M-TEST", readings, nil, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	r := domain.Reading{Timestamp: day0.AddDate(0, 0, 8).Add(3 * time.Hour), ConsumptionKWh: 103 + 2*0.05*103}
	near(t, "z", m.Z(r, domain.Consumption), 2, 1e-9)
}

func TestReferenceWindowStartsAtMidnightOfTheEarliestReading(t *testing.T) {
	readings := series(day0, 2, func(d, h int) float64 { return 1 })
	// The earliest reading comes last and falls on the previous day.
	readings = append(readings, domain.Reading{Timestamp: day0.Add(-5 * time.Hour)})

	start, end := ReferenceWindow(readings, 7)
	if want := day0.AddDate(0, 0, -1); !start.Equal(want) {
		t.Errorf("start = %v, want %v", start, want)
	}
	if want := day0.AddDate(0, 0, 6); !end.Equal(want) {
		t.Errorf("end = %v, want %v", end, want)
	}
}

func TestReferenceWindowWithoutReadingsIsEmpty(t *testing.T) {
	start, end := ReferenceWindow(nil, 7)
	if !start.IsZero() || !end.IsZero() {
		t.Errorf("window = [%v, %v), want zero times", start, end)
	}
}
