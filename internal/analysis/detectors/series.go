package detectors

import (
	"math"
	"slices"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// series is the time-ordered readings of one meter with its baseline.
type series struct {
	meter    baseline.Meter
	readings []domain.Reading
}

func newSeries(m baseline.Meter, readings []domain.Reading) series {
	sorted := slices.Clone(readings)
	slices.SortStableFunc(sorted, func(a, b domain.Reading) int { return a.Timestamp.Compare(b.Timestamp) })
	return series{meter: m, readings: sorted}
}

func (s series) zScores(v domain.Variable) []float64 {
	z := make([]float64, len(s.readings))
	for i, r := range s.readings {
		z[i] = s.meter.Z(r, v)
	}
	return z
}

// nextHour reports whether reading i+1 is exactly one hour after reading i.
func (s series) nextHour(i int) bool {
	return i >= 0 && i+1 < len(s.readings) && s.readings[i+1].Timestamp.Sub(s.readings[i].Timestamp) == time.Hour
}

// span is a stretch of readings, inclusive on both ends, on one side of the baseline.
type span struct{ from, to, sign int }

func (sp span) hours() int { return sp.to - sp.from + 1 }

func side(z, threshold float64) int {
	switch {
	case z > threshold:
		return 1
	case z < -threshold:
		return -1
	}
	return 0
}

// runs returns the maximal stretches of consecutive hours whose value stays
// beyond the threshold on the same side.
func (s series) runs(values []float64, threshold float64) []span {
	var out []span
	for i := 0; i < len(values); {
		sg := side(values[i], threshold)
		if sg == 0 {
			i++
			continue
		}
		j := i
		for s.nextHour(j) && side(values[j+1], threshold) == sg {
			j++
		}
		out = append(out, span{from: i, to: j, sign: sg})
		i = j + 1
	}
	return out
}

func (s series) signal(kind Kind, v domain.Variable, sp span, z []float64) Signal {
	var observed, expected, zSum float64
	for i := sp.from; i <= sp.to; i++ {
		r := s.readings[i]
		observed += r.Value(v)
		expected += s.meter.Profiles[v][r.Timestamp.Hour()].Median
		zSum += z[i]
	}
	n := float64(sp.hours())
	return Signal{
		Kind:      kind,
		MeterID:   s.meter.MeterID,
		Variable:  v,
		Start:     s.readings[sp.from].Timestamp,
		End:       s.readings[sp.to].Timestamp,
		Hours:     sp.hours(),
		Direction: sp.sign,
		Observed:  observed / n,
		Expected:  expected / n,
		MeanZ:     zSum / n,
		Open:      sp.to == len(s.readings)-1,
	}
}

func abs(x float64) float64 { return math.Abs(x) }
