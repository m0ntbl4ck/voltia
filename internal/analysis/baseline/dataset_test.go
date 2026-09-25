package baseline_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func loadByMeter(t *testing.T) map[string][]domain.Reading {
	t.Helper()
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join("..", "..", "..", "data", "readings.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	all, err := seed.ParseReadings(f, loc)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string][]domain.Reading{}
	for _, r := range all {
		by[r.MeterID] = append(by[r.MeterID], r)
	}
	return by
}

// Expected values come from an independent computation of the same rule:
// hourly medians over days 1 to 7, and the mean of days 13 and 14.
func TestDatasetDailyBaselineAndVariation(t *testing.T) {
	by := loadByMeter(t)
	cfg := baseline.DefaultConfig()
	cases := []struct {
		meter               string
		base, recent, delta float64
	}{
		{"M-109", 1047.74, 2200.60, 110.033},
		{"M-104", 1170.32, 1722.11, 47.149},
		{"M-106", 1346.98, 1343.98, -0.223},
		{"M-112", 659.53, 662.535, 0.456},
		{"M-101", 731.15, 725.745, -0.739},
	}
	for _, c := range cases {
		t.Run(c.meter, func(t *testing.T) {
			m, err := baseline.Build(c.meter, by[c.meter], nil, cfg)
			if err != nil {
				t.Fatal(err)
			}
			recent, err := baseline.RecentDailyKWh(by[c.meter], cfg.RecentDays)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(m.DailyKWh()-c.base) > 0.01 {
				t.Errorf("baseline = %.3f, want %.2f", m.DailyKWh(), c.base)
			}
			if math.Abs(recent-c.recent) > 0.01 {
				t.Errorf("recent = %.3f, want %.3f", recent, c.recent)
			}
			if got := baseline.VariationPct(recent, m.DailyKWh()); math.Abs(got-c.delta) > 0.01 {
				t.Errorf("variation = %.3f%%, want %.3f%%", got, c.delta)
			}
		})
	}
}

// After the reference week, hours beyond 3 robust deviations mark the shifted
// meters and leave the rest alone.
func TestDatasetHoursOutsideTheBaseline(t *testing.T) {
	by := loadByMeter(t)
	cfg := baseline.DefaultConfig()
	want := map[string]int{"M-109": 58, "M-104": 96, "M-106": 12}
	for meter, readings := range by {
		m, err := baseline.Build(meter, readings, nil, cfg)
		if err != nil {
			t.Fatal(err)
		}
		p := m.Profiles[domain.Consumption]
		outside, maxZ := 0, 0.0
		for _, r := range readings {
			if r.Timestamp.Day() <= 7 {
				continue
			}
			z := math.Abs(p.Z(r.Timestamp.Hour(), r.ConsumptionKWh))
			maxZ = math.Max(maxZ, z)
			if z > 3 {
				outside++
			}
		}
		if expected, shifted := want[meter]; shifted {
			if outside != expected {
				t.Errorf("%s: %d hours outside, want %d", meter, outside, expected)
			}
		} else if maxZ >= 4 {
			t.Errorf("%s: max |z| = %.2f, a steady meter should stay under 4", meter, maxZ)
		}
	}
}

func TestDatasetHourlyZScoreOfTheAnomalousMeter(t *testing.T) {
	by := loadByMeter(t)
	m, err := baseline.Build("M-109", by["M-109"], nil, baseline.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := m.Profiles[domain.Consumption]
	if math.Abs(p[15].Median-51.44) > 1e-9 {
		t.Errorf("hour 15 median = %v, want 51.44", p[15].Median)
	}
	if z := p.Z(15, 105.37); math.Abs(z-20.968) > 0.001 {
		t.Errorf("z = %.4f, want 20.968", z)
	}
}
