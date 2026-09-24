package detectors

import (
	"math"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

const (
	CheckDailyCorrelation = "daily_correlation"
	CheckNightDayRatio    = "night_day_ratio"
)

// DetectHourlyPattern compares the shape of each complete day with the
// baseline profile. A day is off when its consumption correlates with the
// profile below PatternMinCorrelation, or when its night to day ratio strays
// from the baseline's by more than NightDayTolerance. The correlation only
// counts when the profile has a shape of its own, above its noise. A level
// that moves with the same shape leaves both untouched, and is left to the
// shift detector.
// The signal covers the hours of the day that left the baseline, or the whole
// day when no single hour did.
func DetectHourlyPattern(m baseline.Meter, readings []domain.Reading, cfg Config) []Signal {
	s := newSeries(m, readings)
	z := s.zScores(domain.Consumption)
	var profile [24]float64
	for h, stat := range m.Profiles[domain.Consumption] {
		profile[h] = stat.Median
	}
	baseRatio, baseRatioOK := nightDayRatio(profile, cfg)
	shaped := shapeStrength(m.Profiles[domain.Consumption]) >= cfg.PatternMinShape

	var out []Signal
	for from := 0; from < len(s.readings); {
		to := from
		for to+1 < len(s.readings) && sameDay(s.readings[to+1].Timestamp, s.readings[from].Timestamp) {
			to++
		}
		start := from
		from = to + 1
		if !s.completeDay(start, to) {
			continue
		}

		var day [24]float64
		for i := start; i <= to; i++ {
			day[s.readings[i].Timestamp.Hour()] = s.readings[i].ConsumptionKWh
		}
		metrics := map[string]float64{}
		corr, corrOK := pearson(day[:], profile[:])
		corrOK = corrOK && shaped
		off := corrOK && corr < cfg.PatternMinCorrelation
		if corrOK {
			metrics["correlation"] = corr
		}
		ratio, ratioOK := nightDayRatio(day, cfg)
		if ratioOK && baseRatioOK {
			metrics["night_day_ratio"] = ratio
			metrics["baseline_night_day_ratio"] = baseRatio
		}
		strayed := ratioOK && baseRatioOK && math.Abs(ratio/baseRatio-1) > cfg.NightDayTolerance
		if !off && !strayed {
			continue
		}

		sp := span{from: start, to: to}
		if first, last, found := offPattern(z, start, to, cfg.ShiftZ); found {
			sp = span{from: first, to: last}
		}
		sig := s.signal(KindHourlyPattern, domain.Consumption, sp, z)
		sig.Check = CheckNightDayRatio
		if off {
			sig.Check = CheckDailyCorrelation
		}
		sig.Metrics = metrics
		out = append(out, sig)
	}
	return out
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// completeDay reports whether readings from..to are the 24 hours of one day
// with no gap, which is what the shape of a day needs.
func (s series) completeDay(from, to int) bool {
	if to-from+1 != 24 {
		return false
	}
	for i := from; i < to; i++ {
		if !s.nextHour(i) {
			return false
		}
	}
	return true
}

// offPattern finds the first and last reading in from..to whose consumption sits
// beyond the threshold on either side.
func offPattern(z []float64, from, to int, threshold float64) (first, last int, found bool) {
	for i := from; i <= to; i++ {
		if abs(z[i]) > threshold {
			if !found {
				first, found = i, true
			}
			last = i
		}
	}
	return first, last, found
}

// isNight reports whether an hour of the day falls in the night, which wraps
// midnight when it starts after it ends.
func (c Config) isNight(h int) bool {
	if c.NightStartHour <= c.NightEndHour {
		return h >= c.NightStartHour && h < c.NightEndHour
	}
	return h >= c.NightStartHour || h < c.NightEndHour
}

// shapeStrength is the spread of the hourly medians over the typical sigma:
// how far the profile's shape stands above its noise. It is 0 for a flat
// profile and infinite for a shaped one without any noise.
func shapeStrength(p baseline.Profile) float64 {
	var mean, sigma float64
	for _, h := range p {
		mean += h.Median
		sigma += h.Sigma
	}
	mean, sigma = mean/24, sigma/24
	var variance float64
	for _, h := range p {
		variance += (h.Median - mean) * (h.Median - mean)
	}
	spread := math.Sqrt(variance / 24)
	switch {
	case spread <= 1e-12*math.Abs(mean):
		return 0 // rounding noise around a flat profile
	case sigma == 0:
		return math.Inf(1)
	}
	return spread / sigma
}

// nightDayRatio is the mean consumption of the night hours over that of the
// day hours. It reports false when the day mean is zero.
func nightDayRatio(hours [24]float64, cfg Config) (float64, bool) {
	var night, day float64
	var nights, days int
	for h, v := range hours {
		if cfg.isNight(h) {
			night += v
			nights++
		} else {
			day += v
			days++
		}
	}
	if nights == 0 || days == 0 || day == 0 {
		return 0, false
	}
	return (night / float64(nights)) / (day / float64(days)), true
}

// pearson is the correlation of two series of the same length. It reports
// false when either has no variation, which leaves the correlation undefined.
func pearson(a, b []float64) (float64, bool) {
	var ma, mb float64
	for i := range a {
		ma += a[i]
		mb += b[i]
	}
	n := float64(len(a))
	ma, mb = ma/n, mb/n
	var cov, va, vb float64
	for i := range a {
		cov += (a[i] - ma) * (b[i] - mb)
		va += (a[i] - ma) * (a[i] - ma)
		vb += (b[i] - mb) * (b[i] - mb)
	}
	if va == 0 || vb == 0 {
		return 0, false
	}
	return cov / math.Sqrt(va*vb), true
}
