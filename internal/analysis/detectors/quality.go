package detectors

import (
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

const (
	CheckElectricalJump   = "electrical_jump"
	CheckImpossibleValue  = "impossible_value"
	CheckDuplicateReading = "duplicate_timestamp"
	CheckMissingReadings  = "missing_readings"
)

var electricalVariables = []domain.Variable{domain.Voltage, domain.Current, domain.PowerFactor}

// DetectDataQuality flags readings that cannot be trusted: isolated electrical
// jumps while consumption stays normal, impossible values, duplicated hours and
// gaps. A change that lasts for hours is a real event and is left to the shift
// detector.
func DetectDataQuality(m baseline.Meter, readings []domain.Reading, cfg Config) []Signal {
	s := newSeries(m, readings)
	zs := map[domain.Variable][]float64{}
	for _, v := range domain.Variables {
		zs[v] = s.zScores(v)
	}

	var out []Signal
	for i, r := range s.readings {
		if v, ok := impossible(r); ok {
			out = append(out, pointSignal(s, i, v, CheckImpossibleValue, zs[v][i]))
		}
		if v, ok := s.electricalJump(i, zs, cfg); ok {
			out = append(out, pointSignal(s, i, v, CheckElectricalJump, zs[v][i]))
		}
		if i+1 >= len(s.readings) {
			continue
		}
		gap := s.readings[i+1].Timestamp.Sub(r.Timestamp)
		switch {
		case gap == 0:
			out = append(out, pointSignal(s, i, "", CheckDuplicateReading, 0))
		case gap > time.Hour:
			missing := int(gap/time.Hour) - 1
			out = append(out, Signal{
				Kind:    KindDataQuality,
				Check:   CheckMissingReadings,
				MeterID: m.MeterID,
				Start:   r.Timestamp.Add(time.Hour),
				End:     s.readings[i+1].Timestamp.Add(-time.Hour),
				Hours:   missing,
			})
		}
	}
	return out
}

// electricalJump reports the variable that jumps at reading i, when both
// neighbouring hours are close to normal and consumption is unaffected.
func (s series) electricalJump(i int, zs map[domain.Variable][]float64, cfg Config) (domain.Variable, bool) {
	if !s.nextHour(i-1) || !s.nextHour(i) || abs(zs[domain.Consumption][i]) > cfg.JumpMaxConsumptionZ {
		return "", false
	}
	var best domain.Variable
	var bestZ float64
	for _, v := range electricalVariables {
		z := zs[v]
		if abs(z[i]) > cfg.JumpZ && abs(z[i-1]) <= cfg.JumpNeighborZ && abs(z[i+1]) <= cfg.JumpNeighborZ && abs(z[i]) > bestZ {
			best, bestZ = v, abs(z[i])
		}
	}
	return best, best != ""
}

func impossible(r domain.Reading) (domain.Variable, bool) {
	switch {
	case r.PowerFactor <= 0 || r.PowerFactor > 1:
		return domain.PowerFactor, true
	case r.VoltageV <= 0:
		return domain.Voltage, true
	case r.CurrentA < 0:
		return domain.Current, true
	case r.ConsumptionKWh < 0:
		return domain.Consumption, true
	}
	return "", false
}

func pointSignal(s series, i int, v domain.Variable, check string, z float64) Signal {
	r := s.readings[i]
	sig := Signal{
		Kind:      KindDataQuality,
		Check:     check,
		MeterID:   s.meter.MeterID,
		Variable:  v,
		Start:     r.Timestamp,
		End:       r.Timestamp,
		Hours:     1,
		Direction: side(z, 0),
		MeanZ:     z,
		Open:      i == len(s.readings)-1,
	}
	if v != "" {
		sig.Observed = r.Value(v)
		sig.Expected = s.meter.Profiles[v][r.Timestamp.Hour()].Median
	}
	return sig
}
