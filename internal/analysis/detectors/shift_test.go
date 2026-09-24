package detectors

import (
	"math"
	"testing"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func TestShiftFindsALongRun(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 2, 9) {
			r.ConsumptionKWh += 20 // z = 4
		}
	})
	got := DetectPersistentShift(testMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	s := got[0]
	if s.Kind != KindPersistentShift || s.Variable != domain.Consumption || s.Hours != 8 || s.Direction != 1 {
		t.Errorf("unexpected signal: %+v", s)
	}
	if !s.Start.Equal(at(2)) || !s.End.Equal(at(9)) || s.Open {
		t.Errorf("bounds start=%v end=%v open=%v", s.Start, s.End, s.Open)
	}
	if math.Abs(s.Observed-120) > 1e-9 || math.Abs(s.Expected-100) > 1e-9 || math.Abs(s.MeanZ-4) > 1e-9 {
		t.Errorf("evidence observed=%v expected=%v z=%v", s.Observed, s.Expected, s.MeanZ)
	}
}

func TestShiftIgnoresShortRuns(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 2, 6) { // 5 hours
			r.ConsumptionKWh += 20
		}
	})
	expect(t, DetectPersistentShift(testMeter(t), r, DefaultConfig()), 0)
}

func TestShiftSplitsRunsWhenTheSideChanges(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		switch {
		case between(i, 2, 5):
			r.ConsumptionKWh += 20
		case between(i, 6, 9):
			r.ConsumptionKWh -= 20
		}
	})
	expect(t, DetectPersistentShift(testMeter(t), r, DefaultConfig()), 0)
}

func TestShiftDoesNotBridgeMissingHours(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 2, 9) {
			r.ConsumptionKWh += 20
		}
	})
	r = append(r[:5], r[6:]...) // hour 5 is missing
	expect(t, DetectPersistentShift(testMeter(t), r, DefaultConfig()), 0)
}

func TestShiftMarksRunsThatReachTheLastReading(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if i >= 12 {
			r.ConsumptionKWh += 20
		}
	})
	got := DetectPersistentShift(testMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	if !got[0].Open {
		t.Error("run reaching the last reading should be open")
	}
}

func TestShiftReportsEachVariableSeparately(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 2, 9) {
			r.CurrentA += 30      // z = 6
			r.PowerFactor -= 0.06 // z = -3.3
		}
	})
	got := DetectPersistentShift(testMeter(t), r, DefaultConfig())
	expect(t, got, 2)
	vars := map[domain.Variable]int{}
	for _, s := range got {
		vars[s.Variable] = s.Direction
	}
	if vars[domain.Current] != 1 || vars[domain.PowerFactor] != -1 {
		t.Errorf("unexpected variables: %v", vars)
	}
}

func TestSpikeFindsAShortJumpThatReturns(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 3, 5) {
			r.ConsumptionKWh += 30 // z = 6
		}
	})
	got := DetectSpikes(testMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	if got[0].Kind != KindSpike || got[0].Hours != 3 || got[0].Direction != 1 {
		t.Errorf("unexpected signal: %+v", got[0])
	}
}

func TestSpikeNeedsAReturnToNormal(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if i >= 17 {
			r.ConsumptionKWh += 30
		}
	})
	expect(t, DetectSpikes(testMeter(t), r, DefaultConfig()), 0)
}

func TestSpikeIgnoresRunsLongerThanTheLimit(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 3, 8) { // 6 hours
			r.ConsumptionKWh += 30
		}
	})
	expect(t, DetectSpikes(testMeter(t), r, DefaultConfig()), 0)
}

func TestSpikeIgnoresOtherVariables(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 3, 5) {
			r.CurrentA += 40
		}
	})
	expect(t, DetectSpikes(testMeter(t), r, DefaultConfig()), 0)
}

func TestSpikeRejectsAnUnsteadyReturn(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		switch i {
		case 3:
			r.ConsumptionKWh += 30 // z = 6
		case 4:
			r.ConsumptionKWh += 17.5 // z = 3.5, still off
		}
	})
	expect(t, DetectSpikes(testMeter(t), r, DefaultConfig()), 0)
}
