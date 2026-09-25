package analysis

import (
	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// MeterBaseline learns the baseline of one meter the way Run does: from the
// reference window of its readings, leaving out the hours covered by a
// reported event. readings must all belong to the meter; events may include
// other meters', which are ignored.
func MeterBaseline(meterID string, readings []domain.Reading, events []domain.Event, cfg Config) (baseline.Meter, error) {
	skip := referenceSkip(meterID, events, cfg.EventExclusion)
	return baseline.Build(meterID, readings, skip, cfg.Baseline)
}
