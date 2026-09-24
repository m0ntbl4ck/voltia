package scoring

import (
	"math"

	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// evidence is what the scoring reads from an episode besides its type.
type evidence struct {
	// variationPct is the absolute consumption change of the longest
	// consumption shift, or 0 when consumption did not shift.
	variationPct float64
	// excessKWh is the consumption above the baseline, summed over the upward shifts.
	excessKWh float64
	// invalidReadings counts the hours flagged by the data quality detector.
	invalidReadings int
}

func gather(ep classify.Episode) evidence {
	var ev evidence
	longest := 0
	for _, s := range ep.Signals {
		switch {
		case s.Kind == detectors.KindDataQuality:
			ev.invalidReadings += s.Hours
		case s.Variable == domain.Consumption && (s.Kind == detectors.KindPersistentShift || s.Kind == detectors.KindSpike):
			if s.Hours > longest && s.Expected != 0 {
				longest = s.Hours
				ev.variationPct = math.Abs(s.Observed-s.Expected) / s.Expected * 100
			}
			if s.Direction > 0 {
				ev.excessKWh += (s.Observed - s.Expected) * float64(s.Hours)
			}
		}
	}
	return ev
}
