package detectors

import (
	"testing"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func TestOutlierFlagsAnIsolatedElectricalReading(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if i == 5 {
			r.VoltageV = 260
		}
	})
	got := DetectOutliers(testMeter(t), r, DefaultConfig())
	expect(t, got, 1)
	if got[0].Kind != KindOutlier || got[0].Variable != domain.Voltage || !got[0].Start.Equal(at(5)) || got[0].Direction != 1 {
		t.Errorf("unexpected signal: %+v", got[0])
	}
}

func TestOutlierSkipsWhatAShiftExplains(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if between(i, 2, 9) {
			r.ConsumptionKWh += 30
		}
	})
	expect(t, DetectOutliers(testMeter(t), r, DefaultConfig()), 0)
}

func TestOutlierSkipsConsumptionSpikes(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if i == 5 {
			r.ConsumptionKWh += 30
		}
	})
	expect(t, DetectOutliers(testMeter(t), r, DefaultConfig()), 0)
}

func TestOutlierKeepsWhatHasNoReturnYet(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if i >= 17 {
			r.ConsumptionKWh += 30
		}
	})
	expect(t, DetectOutliers(testMeter(t), r, DefaultConfig()), 3)
}

func TestOutlierIgnoresReadingsWithinFiveDeviations(t *testing.T) {
	r := window(20, func(i int, r *domain.Reading) {
		if i == 5 {
			r.VoltageV = 220 + 4*1.1
		}
	})
	expect(t, DetectOutliers(testMeter(t), r, DefaultConfig()), 0)
}
