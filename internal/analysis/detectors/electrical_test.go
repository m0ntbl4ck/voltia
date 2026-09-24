package detectors

import (
	"math"
	"testing"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func TestElectricalRelationFindsAPowerFactorDrop(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 4, 11) {
			r.PowerFactor = 0.75
		}
	})
	got := DetectElectricalRelation(testMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	s := got[0]
	if s.Kind != KindElectricalRelation || s.Check != checkPowerFactorDrop || s.Hours != 8 || s.Direction != -1 {
		t.Errorf("unexpected signal: %+v", s)
	}
	if math.Abs(s.Observed-0.75) > 1e-9 || math.Abs(s.Expected-0.9) > 1e-9 {
		t.Errorf("evidence observed=%v expected=%v", s.Observed, s.Expected)
	}
}

func TestElectricalRelationIgnoresASmallDrop(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 4, 11) {
			r.PowerFactor = 0.85
		}
	})
	expect(t, DetectElectricalRelation(testMeter(t), r, DefaultConfig()), 0)
}

func TestElectricalRelationIgnoresAShortDrop(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 4, 8) {
			r.PowerFactor = 0.75
		}
	})
	expect(t, DetectElectricalRelation(testMeter(t), r, DefaultConfig()), 0)
}

func TestElectricalRelationIgnoresAHigherPowerFactor(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 4, 11) {
			r.PowerFactor = 1
		}
	})
	expect(t, DetectElectricalRelation(testMeter(t), r, DefaultConfig()), 0)
}

func TestElectricalRelationIgnoresALoadChangeThatKeepsThePowerFactor(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 4, 11) {
			r.ConsumptionKWh += 40
			r.CurrentA += 40
		}
	})
	expect(t, DetectElectricalRelation(testMeter(t), r, DefaultConfig()), 0)
}
