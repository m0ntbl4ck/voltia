package baseline

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// madToSigma scales the MAD so it estimates a standard deviation on normal data.
const madToSigma = 1.4826

type Config struct {
	// ReferenceDays is how many days from the first reading define "normal".
	ReferenceDays int
	// MinSigmaRatio keeps very stable hours from producing huge z-scores: sigma
	// never drops below this fraction of the median. It is set per variable
	// because voltage and power factor are far steadier than consumption.
	MinSigmaRatio map[domain.Variable]float64
	// RecentDays is how many complete trailing days measure the current level.
	RecentDays int
}

func DefaultConfig() Config {
	return Config{
		ReferenceDays: 7,
		MinSigmaRatio: map[domain.Variable]float64{
			domain.Consumption: 0.05,
			domain.Voltage:     0.005,
			domain.Current:     0.05,
			domain.PowerFactor: 0.02,
		},
		RecentDays: 2,
	}
}

// HourStat summarizes one hour of the day across the reference window.
type HourStat struct {
	Median  float64
	Sigma   float64
	Samples int
}

// Profile holds one HourStat per hour of the day.
type Profile [24]HourStat

// Z returns how many robust deviations x sits from the median of that hour.
func (p Profile) Z(hour int, x float64) float64 {
	s := p[hour]
	if s.Sigma == 0 {
		return 0
	}
	return (x - s.Median) / s.Sigma
}

// Meter is the baseline of one meter: a profile per measured variable.
type Meter struct {
	MeterID  string
	Profiles map[domain.Variable]Profile
}

// Z is the z-score of one variable of a reading against its hourly profile.
func (m Meter) Z(r domain.Reading, v domain.Variable) float64 {
	return m.Profiles[v].Z(r.Timestamp.Hour(), r.Value(v))
}

// DailyKWh is the expected consumption of a full day: the sum of the hourly medians.
func (m Meter) DailyKWh() float64 {
	var total float64
	for _, s := range m.Profiles[domain.Consumption] {
		total += s.Median
	}
	return total
}

// ReferenceWindow returns the half-open interval [start, end) that defines
// normal: `days` whole days counted from midnight of the earliest reading. It
// returns zero times when there are no readings.
func ReferenceWindow(readings []domain.Reading, days int) (start, end time.Time) {
	if len(readings) == 0 {
		return time.Time{}, time.Time{}
	}
	first := readings[0].Timestamp
	for _, r := range readings {
		if r.Timestamp.Before(first) {
			first = r.Timestamp
		}
	}
	start = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, first.Location())
	return start, start.AddDate(0, 0, days)
}

// Build learns the baseline of one meter from its readings. Readings for which
// skip returns true (known events, invalid data) do not contribute.
func Build(meterID string, readings []domain.Reading, skip func(domain.Reading) bool, cfg Config) (Meter, error) {
	if len(readings) == 0 {
		return Meter{}, errors.New("no readings")
	}
	start, end := ReferenceWindow(readings, cfg.ReferenceDays)

	samples := make(map[domain.Variable]*[24][]float64, len(domain.Variables))
	for _, v := range domain.Variables {
		samples[v] = new([24][]float64)
	}
	for _, r := range readings {
		if r.Timestamp.Before(start) || !r.Timestamp.Before(end) {
			continue
		}
		if skip != nil && skip(r) {
			continue
		}
		for _, v := range domain.Variables {
			h := r.Timestamp.Hour()
			samples[v][h] = append(samples[v][h], r.Value(v))
		}
	}

	profiles := make(map[domain.Variable]Profile, len(domain.Variables))
	for _, v := range domain.Variables {
		var p Profile
		for h, values := range samples[v] {
			if len(values) == 0 {
				return Meter{}, fmt.Errorf("meter %s: no reference samples for %s at hour %d", meterID, v, h)
			}
			med := median(values)
			sigma := math.Max(madToSigma*medianAbsDeviation(values, med), cfg.MinSigmaRatio[v]*math.Abs(med))
			p[h] = HourStat{Median: med, Sigma: sigma, Samples: len(values)}
		}
		profiles[v] = p
	}
	return Meter{MeterID: meterID, Profiles: profiles}, nil
}

// RecentDailyKWh averages the consumption of the last `days` complete days.
// A day counts as complete when it has 24 readings.
func RecentDailyKWh(readings []domain.Reading, days int) (float64, error) {
	type dayTotal struct {
		count int
		kwh   float64
	}
	byDay := map[string]*dayTotal{}
	for _, r := range readings {
		key := r.Timestamp.Format("2006-01-02")
		if byDay[key] == nil {
			byDay[key] = &dayTotal{}
		}
		byDay[key].count++
		byDay[key].kwh += r.ConsumptionKWh
	}
	var complete []string
	for key, d := range byDay {
		if d.count == 24 {
			complete = append(complete, key)
		}
	}
	if len(complete) < days {
		return 0, fmt.Errorf("need %d complete days, have %d", days, len(complete))
	}
	sort.Strings(complete)
	var total float64
	for _, key := range complete[len(complete)-days:] {
		total += byDay[key].kwh
	}
	return total / float64(days), nil
}

// VariationPct is the change of recent against baseline, in percent. It returns
// 0 when the baseline is zero.
func VariationPct(recent, base float64) float64 {
	if base == 0 {
		return 0
	}
	return (recent - base) / base * 100
}
