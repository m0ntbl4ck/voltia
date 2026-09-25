package detectors

import (
	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

const checkPowerFactorDrop = "power_factor_drop"

// DetectElectricalRelation finds a power factor that stays well below its
// hourly median for ShiftMinHours or more. A load change that keeps the power
// factor steady, like a new production line, does not trigger it.
func DetectElectricalRelation(m baseline.Meter, readings []domain.Reading, cfg Config) []Signal {
	s := newSeries(m, readings)
	deficit := make([]float64, len(s.readings))
	for i, r := range s.readings {
		deficit[i] = m.Profiles[domain.PowerFactor][r.Timestamp.Hour()].Median - r.PowerFactor
	}
	z := s.zScores(domain.PowerFactor)
	var out []Signal
	for _, sp := range s.runs(deficit, cfg.PowerFactorDrop) {
		if sp.sign != 1 || sp.hours() < cfg.ShiftMinHours {
			continue
		}
		sig := s.signal(KindElectricalRelation, domain.PowerFactor, sp, z)
		sig.Check = checkPowerFactorDrop
		sig.Direction = -1 // the span sign is the sign of the deficit, not of the value
		out = append(out, sig)
	}
	return out
}
