package detectors

import (
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func jump(r *domain.Reading) {
	r.VoltageV = 250 // z = 27
	r.CurrentA = 200 // z = 20
	r.PowerFactor = 0.6
}

func TestDataQualityFlagsAnIsolatedElectricalJump(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if i == 4 {
			jump(r)
		}
	})
	got := DetectDataQuality(testMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	s := got[0]
	if s.Kind != KindDataQuality || s.Check != CheckElectricalJump || s.Variable != domain.Voltage || s.Hours != 1 || !s.Start.Equal(at(4)) || s.Open {
		t.Errorf("unexpected signal: %+v", s)
	}
}

func TestDataQualityIgnoresAJumpWhenConsumptionMoves(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if i == 4 {
			jump(r)
			r.ConsumptionKWh += 30
		}
	})
	expect(t, DetectDataQuality(testMeter(t), r, DefaultConfig()), 0)
}

func TestDataQualityIgnoresAChangeThatLastsHours(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 4, 13) {
			r.CurrentA = 200
		}
	})
	expect(t, DetectDataQuality(testMeter(t), r, DefaultConfig()), 0)
}

func TestDataQualityNeedsBothNeighbours(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if i == 0 || i == 19 {
			jump(r)
		}
	})
	expect(t, DetectDataQuality(testMeter(t), r, DefaultConfig()), 0)
}

func TestDataQualityFlagsImpossibleValues(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if i == 3 {
			r.PowerFactor = 1.2
		}
	})
	got := DetectDataQuality(testMeter(t), r, DefaultConfig())
	var found bool
	for _, s := range got {
		if s.Check == CheckImpossibleValue && s.Variable == domain.PowerFactor && s.Start.Equal(at(3)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("impossible power factor not flagged: %+v", got)
	}
}

func TestDataQualityFlagsDuplicatedHours(t *testing.T) {
	r := window(20, nil)
	r = append(r[:4], append([]domain.Reading{r[3]}, r[4:]...)...)
	got := DetectDataQuality(testMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	if got[0].Check != CheckDuplicateReading || !got[0].Start.Equal(at(3)) {
		t.Errorf("unexpected signal: %+v", got[0])
	}
}

func TestDataQualityFlagsMissingHours(t *testing.T) {
	r := window(20, nil)
	r = append(r[:5], r[8:]...) // hours 5, 6 and 7 are missing
	got := DetectDataQuality(testMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	s := got[0]
	if s.Check != CheckMissingReadings || s.Hours != 3 || !s.Start.Equal(at(5)) || !s.End.Equal(at(7)) {
		t.Errorf("unexpected signal: %+v", s)
	}
}

func TestDataQualityLeavesCleanReadingsAlone(t *testing.T) {
	expect(t, DetectDataQuality(testMeter(t), window(48, nil), DefaultConfig()), 0)
}

func TestDataQualityUsesTheReadingsOwnClock(t *testing.T) {
	cot := time.FixedZone("COT", -5*3600)
	r := window(20, func(i int, r *domain.Reading) {
		r.Timestamp = r.Timestamp.In(cot)
	})
	expect(t, DetectDataQuality(testMeter(t), r, DefaultConfig()), 0)
}
