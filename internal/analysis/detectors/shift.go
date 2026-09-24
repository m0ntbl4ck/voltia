package detectors

import (
	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func (s series) shiftSpans(z []float64, cfg Config) []span {
	var out []span
	for _, sp := range s.runs(z, cfg.ShiftZ) {
		if sp.hours() >= cfg.ShiftMinHours {
			out = append(out, sp)
		}
	}
	return out
}

// spikeSpans returns short runs that end back inside the normal band. A run
// that reaches the last reading is not a spike because its return is unknown.
func (s series) spikeSpans(z []float64, cfg Config) []span {
	var out []span
	for _, sp := range s.runs(z, cfg.SpikeZ) {
		if sp.hours() > cfg.SpikeMaxHours || !s.nextHour(sp.to) {
			continue
		}
		if abs(z[sp.to+1]) <= cfg.ShiftZ {
			out = append(out, sp)
		}
	}
	return out
}

// DetectPersistentShift finds levels that stay beyond the baseline for
// ShiftMinHours or more, in any variable.
func DetectPersistentShift(m baseline.Meter, readings []domain.Reading, cfg Config) []Signal {
	s := newSeries(m, readings)
	var out []Signal
	for _, v := range domain.Variables {
		z := s.zScores(v)
		for _, sp := range s.shiftSpans(z, cfg) {
			out = append(out, s.signal(KindPersistentShift, v, sp, z))
		}
	}
	return out
}

// DetectSpikes finds short jumps of consumption that return to normal.
func DetectSpikes(m baseline.Meter, readings []domain.Reading, cfg Config) []Signal {
	s := newSeries(m, readings)
	z := s.zScores(domain.Consumption)
	var out []Signal
	for _, sp := range s.spikeSpans(z, cfg) {
		out = append(out, s.signal(KindSpike, domain.Consumption, sp, z))
	}
	return out
}
