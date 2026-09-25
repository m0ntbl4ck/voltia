package analysis

import (
	"math"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func flatMeter(id string, days int, kwh float64) []domain.Reading {
	loc := time.UTC
	var out []domain.Reading
	for d := 0; d < days; d++ {
		for h := 0; h < 24; h++ {
			out = append(out, domain.Reading{
				MeterID: id, Timestamp: time.Date(2026, 9, 1+d, h, 0, 0, 0, loc),
				ConsumptionKWh: kwh, VoltageV: 220, CurrentA: 30, PowerFactor: 0.9,
			})
		}
	}
	return out
}

func TestMeterBaselineMatchesThePlainBaselineWithoutEvents(t *testing.T) {
	readings := flatMeter("M-1", 10, 50)
	got, err := MeterBaseline("M-1", readings, nil, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.DailyKWh()-1200) > 1e-9 {
		t.Errorf("DailyKWh = %v, want 1200", got.DailyKWh())
	}
	want, _ := baseline.Build("M-1", readings, nil, baseline.DefaultConfig())
	if got.DailyKWh() != want.DailyKWh() {
		t.Errorf("differs from baseline.Build: %v against %v", got.DailyKWh(), want.DailyKWh())
	}
}

func TestMeterBaselineLeavesOutTheHoursOfAReportedEvent(t *testing.T) {
	readings := flatMeter("M-1", 10, 50)
	// A 12 hour outage inside the reference week: those hours read 0.
	for i := range readings {
		if readings[i].Timestamp.Day() == 3 && readings[i].Timestamp.Hour() < 12 {
			readings[i].ConsumptionKWh = 0
		}
	}
	events := []domain.Event{{
		MeterID: "M-1", Type: domain.EventScheduledOutage,
		Timestamp: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), Duration: 12 * time.Hour,
	}}
	with, err := MeterBaseline("M-1", readings, events, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	// An event of another meter must not exclude anything here.
	other := []domain.Event{{MeterID: "M-2", Type: domain.EventScheduledOutage,
		Timestamp: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), Duration: 12 * time.Hour}}
	without, err := MeterBaseline("M-1", readings, other, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if with.Profiles[domain.Consumption][5].Samples != 6 {
		t.Errorf("hour 5 has %d samples with the event, want 6 (the outage day is left out)", with.Profiles[domain.Consumption][5].Samples)
	}
	if without.Profiles[domain.Consumption][5].Samples != 7 {
		t.Errorf("hour 5 has %d samples with another meter's event, want all 7", without.Profiles[domain.Consumption][5].Samples)
	}
}

func TestMeterBaselineFailsWithoutReadings(t *testing.T) {
	if _, err := MeterBaseline("M-1", nil, nil, DefaultConfig()); err == nil {
		t.Error("expected an error for a meter with no readings")
	}
}

func TestAnEventWithoutADurationExcludesTheConfiguredWindow(t *testing.T) {
	readings := flatMeter("M-1", 10, 50)
	events := []domain.Event{{
		MeterID: "M-1", Type: domain.EventOperationalChange,
		Timestamp: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
	}}
	cfg := DefaultConfig()
	got, err := MeterBaseline("M-1", readings, events, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The default window is 24 hours, so every hour of that day is out, hour 20 included.
	if n := got.Profiles[domain.Consumption][20].Samples; n != 6 {
		t.Errorf("hour 20 has %d samples, want 6", n)
	}

	cfg.EventExclusion = 6 * time.Hour
	short, err := MeterBaseline("M-1", readings, events, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if n := short.Profiles[domain.Consumption][20].Samples; n != 7 {
		t.Errorf("with a 6 hour window hour 20 has %d samples, want 7", n)
	}
	if n := short.Profiles[domain.Consumption][3].Samples; n != 6 {
		t.Errorf("with a 6 hour window hour 3 has %d samples, want 6", n)
	}
}
