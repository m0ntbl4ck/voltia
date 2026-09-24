package detectors

import (
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var day0 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func flatReading(ts time.Time) domain.Reading {
	return domain.Reading{MeterID: "M-TEST", Timestamp: ts, ConsumptionKWh: 100, VoltageV: 220, CurrentA: 100, PowerFactor: 0.9}
}

// testMeter learns a baseline from seven flat days: 100 kWh, 220 V, 100 A and
// 0.9 power factor. The sigma floors then give sigma 5, 1.1, 5 and 0.018.
func testMeter(t *testing.T) baseline.Meter {
	t.Helper()
	var ref []domain.Reading
	for h := 0; h < 7*24; h++ {
		ref = append(ref, flatReading(day0.Add(time.Duration(h)*time.Hour)))
	}
	m, err := baseline.Build("M-TEST", ref, nil, baseline.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// window builds n hourly readings that start right after the reference week.
// mod may change any reading; index i is the hour offset within the window.
func window(n int, mod func(i int, r *domain.Reading)) []domain.Reading {
	start := day0.AddDate(0, 0, 7)
	out := make([]domain.Reading, n)
	for i := range out {
		out[i] = flatReading(start.Add(time.Duration(i) * time.Hour))
		if mod != nil {
			mod(i, &out[i])
		}
	}
	return out
}

func at(i int) time.Time { return day0.AddDate(0, 0, 7).Add(time.Duration(i) * time.Hour) }

func between(i, from, to int) bool { return i >= from && i <= to }

func expect(t *testing.T, got []Signal, n int) {
	t.Helper()
	if len(got) != n {
		t.Fatalf("got %d signals, want %d: %+v", len(got), n, got)
	}
}
