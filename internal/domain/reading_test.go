package domain

import (
	"math"
	"testing"
)

func TestReadingValue(t *testing.T) {
	r := Reading{ConsumptionKWh: 23.5, VoltageV: 221.9, CurrentA: 101.28, PowerFactor: 0.954}
	cases := []struct {
		v    Variable
		want float64
	}{
		{Consumption, 23.5},
		{Voltage, 221.9},
		{Current, 101.28},
		{PowerFactor, 0.954},
		// 221.9 * 101.28 * 0.954 / 1000 = 21.4402 kW, and 23.5 / 21.4402 = 1.0961.
		{PhysicalRatio, 1.0961},
		{Variable("unknown"), 0},
	}
	for _, c := range cases {
		if got := r.Value(c.v); math.Abs(got-c.want) > 0.0001 {
			t.Errorf("Value(%s) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestPhysicalRatioIsZeroWithoutPower(t *testing.T) {
	for name, r := range map[string]Reading{
		"no current":      {ConsumptionKWh: 20, VoltageV: 220, PowerFactor: 0.9},
		"no voltage":      {ConsumptionKWh: 20, CurrentA: 100, PowerFactor: 0.9},
		"no power factor": {ConsumptionKWh: 20, VoltageV: 220, CurrentA: 100},
	} {
		if got := r.Value(PhysicalRatio); got != 0 {
			t.Errorf("%s: ratio = %v, want 0", name, got)
		}
	}
}

func TestPhysicalRatioIsNotAMeasuredVariable(t *testing.T) {
	for _, v := range Variables {
		if v == PhysicalRatio {
			t.Error("a baseline profiles Variables, and the ratio has no profile")
		}
	}
}
