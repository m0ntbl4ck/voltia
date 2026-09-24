package iforest

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var day1 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func hourAt(day, hour int) time.Time {
	return day1.AddDate(0, 0, day-1).Add(time.Duration(hour) * time.Hour)
}

// breakdown changes the readings of day 8, hours 10 to 15.
type breakdown func(kwh, v, i, pf *float64)

// loadSurge more than doubles consumption and current while the power factor drops.
func loadSurge(kwh, v, i, pf *float64) { *kwh, *i, *pf = *kwh*2.2, *i*2.2, *pf*0.8 }

// meterFault sends the voltage up 15 V and the power factor down by a third.
func meterFault(kwh, v, i, pf *float64) { *v, *pf = *v+15, *pf*0.66 }

// noisyMeter builds 10 days of coherent readings with a little noise (the
// forest cannot cut a feature that never varies). Days 1 to 7 are the
// reference and the hours of day 8 from 10 to 15 break down as told.
func noisyMeter(t *testing.T, hit breakdown) (baseline.Meter, []domain.Reading, []domain.Reading) {
	t.Helper()
	rng := rand.New(rand.NewPCG(42, 1))
	var reference, analysis []domain.Reading
	for day := 1; day <= 10; day++ {
		for hour := 0; hour < 24; hour++ {
			v := 220 + rng.NormFloat64()
			i := 100 + 3*rng.NormFloat64()
			pf := 0.9 + 0.01*rng.NormFloat64()
			kwh := v * i * pf / 1000 * (1 + 0.03*rng.NormFloat64())
			if day == 8 && hour >= 10 && hour <= 15 {
				hit(&kwh, &v, &i, &pf)
			}
			r := domain.Reading{MeterID: "M-T", Timestamp: hourAt(day, hour), ConsumptionKWh: kwh, VoltageV: v, CurrentA: i, PowerFactor: pf}
			if day <= 7 {
				reference = append(reference, r)
			} else {
				analysis = append(analysis, r)
			}
		}
	}
	m, err := baseline.Build("M-T", reference, nil, baseline.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	return m, reference, analysis
}

func trained(t *testing.T) (Model, []domain.Reading) {
	t.Helper()
	return trainedOn(t, loadSurge)
}

func trainedOn(t *testing.T, hit breakdown) (Model, []domain.Reading) {
	t.Helper()
	m, reference, analysis := noisyMeter(t, hit)
	model, err := Train(m, reference, DefaultModelConfig())
	if err != nil {
		t.Fatal(err)
	}
	return model, analysis
}

// episodeOn covers the hours from..to (inclusive) of day 8.
func episodeOn(from, to int, open bool) classify.Episode {
	return classify.Episode{MeterID: "M-T", Start: hourAt(8, from), End: hourAt(8, to), Open: open}
}

// A flat profile with median 100 and sigma 10 for consumption, 220 and 2 for
// voltage, 100 and 5 for current and 0.9 and 0.02 for power factor.
func TestFeaturesAreBaselineZScoresAndTheLogRatio(t *testing.T) {
	flat := func(median, sigma float64) baseline.Profile {
		var p baseline.Profile
		for h := range p {
			p[h] = baseline.HourStat{Median: median, Sigma: sigma, Samples: 7}
		}
		return p
	}
	m := baseline.Meter{MeterID: "M-T", Profiles: map[domain.Variable]baseline.Profile{
		domain.Consumption: flat(100, 10),
		domain.Voltage:     flat(220, 2),
		domain.Current:     flat(100, 5),
		domain.PowerFactor: flat(0.9, 0.02),
	}}
	r := domain.Reading{Timestamp: hourAt(1, 3), ConsumptionKWh: 130, VoltageV: 223, CurrentA: 95, PowerFactor: 0.88}

	got := features(m, r)
	// 130/(223*95*0.88/1000) = 6.9732, whose log is 1.94207.
	want := []float64{3, 1.5, -1, -1, 1.9420744378869448}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Errorf("feature %s = %v, want %v", Features[i], got[i], want[i])
		}
	}

	r.ConsumptionKWh = 0
	if got := features(m, r); math.Abs(got[4]-math.Log(0.01)) > 1e-9 {
		t.Errorf("ratio feature without consumption = %v, want log(0.01) = %v", got[4], math.Log(0.01))
	}
}

func TestTrainNeedsAtLeastTwoReadings(t *testing.T) {
	m, reference, _ := noisyMeter(t, loadSurge)
	for _, n := range []int{0, 1} {
		if _, err := Train(m, reference[:n], DefaultModelConfig()); err == nil {
			t.Errorf("%d reference readings: expected an error", n)
		}
	}
	bad := DefaultModelConfig()
	bad.Forest.Trees = 0
	if _, err := Train(m, reference, bad); err == nil {
		t.Error("expected the forest error to come through")
	}
}

func TestTrainingIsDeterministic(t *testing.T) {
	m, reference, analysis := noisyMeter(t, loadSurge)
	a, _ := Train(m, reference, DefaultModelConfig())
	b, _ := Train(m, reference, DefaultModelConfig())
	for _, r := range analysis[:48] {
		if a.Score(r) != b.Score(r) {
			t.Fatalf("scores differ at %v", r.Timestamp)
		}
	}
}

// Ordinary hours can brush the threshold (one reaches 0.606 here), so what the
// model has to guarantee is the gap between them and the broken hours.
func TestBrokenHoursScoreWellAboveOrdinaryOnes(t *testing.T) {
	model, analysis := trained(t)
	brokenLow, ordinaryHigh := 1.0, 0.0
	for _, r := range analysis {
		s := model.Score(r)
		if r.Timestamp.Day() == 8 && r.Timestamp.Hour() >= 10 && r.Timestamp.Hour() <= 15 {
			brokenLow = math.Min(brokenLow, s)
		} else {
			ordinaryHigh = math.Max(ordinaryHigh, s)
		}
	}
	if brokenLow < model.cfg.Threshold || brokenLow < ordinaryHigh+0.05 {
		t.Errorf("lowest broken score %.3f, highest ordinary score %.3f: want the broken hours over the threshold and 0.05 above the rest", brokenLow, ordinaryHigh)
	}
}

func TestCorroborateFlagsTheUnusualHours(t *testing.T) {
	model, analysis := trained(t)
	sig, ok := model.Corroborate(episodeOn(8, 17, false), analysis)
	if !ok {
		t.Fatal("expected the broken hours to corroborate the episode")
	}
	if sig.Kind != detectors.KindIsolationForest || sig.MeterID != "M-T" {
		t.Errorf("kind=%s meter=%s", sig.Kind, sig.MeterID)
	}
	if !sig.Start.Equal(hourAt(8, 10)) || !sig.End.Equal(hourAt(8, 15)) || sig.Hours != 6 {
		t.Errorf("span %v to %v (%d hours), want 10:00 to 15:00 (6 hours)", sig.Start, sig.End, sig.Hours)
	}
	var top float64
	for _, r := range analysis {
		if !r.Timestamp.Before(hourAt(8, 10)) && !r.Timestamp.After(hourAt(8, 15)) {
			top = math.Max(top, model.Score(r))
		}
	}
	if sig.Observed != top || sig.Expected != 0.6 {
		t.Errorf("observed=%.4f expected=%.4f, want the highest score %.4f against 0.6", sig.Observed, sig.Expected, top)
	}
	if sig.Open {
		t.Error("an episode that is not open cannot give an open signal")
	}
}

// Consumption, current and power factor moved and voltage did not, so voltage
// must not take the credit.
func TestCorroborateSaysWhichVariablesIsolatedTheHours(t *testing.T) {
	model, analysis := trained(t)
	sig, _ := model.Corroborate(episodeOn(8, 17, false), analysis)

	var total float64
	for _, v := range Features {
		share, ok := sig.Attribution[v]
		if !ok {
			t.Errorf("no share for %s", v)
		}
		total += share
	}
	if math.Abs(total-1) > 1e-9 {
		t.Errorf("shares add up to %v, want 1", total)
	}
	if sig.Attribution[domain.Voltage] > 0.15 {
		t.Errorf("voltage share = %.2f, want at most 0.15 (it did not move)", sig.Attribution[domain.Voltage])
	}
	if sig.Variable == domain.Voltage || sig.Attribution[sig.Variable] < sig.Attribution[domain.Voltage] {
		t.Errorf("top variable = %s with %.2f", sig.Variable, sig.Attribution[sig.Variable])
	}
	for _, v := range Features {
		if sig.Attribution[v] > sig.Attribution[sig.Variable] {
			t.Errorf("%s has %.2f, more than the top variable %s with %.2f", v, sig.Attribution[v], sig.Variable, sig.Attribution[sig.Variable])
		}
	}
}

func TestCorroborateStaysSilentOnOrdinaryHours(t *testing.T) {
	model, analysis := trained(t)
	if _, ok := model.Corroborate(episodeOn(0, 9, false), analysis); ok {
		t.Error("hours before the breakdown should not be corroborated")
	}
	if _, ok := model.Corroborate(episodeOn(16, 23, false), analysis); ok {
		t.Error("hours after the breakdown should not be corroborated")
	}
}

// Only the hours of the episode count, inclusive at both ends.
func TestCorroborateOnlyLooksInsideTheEpisode(t *testing.T) {
	model, analysis := trained(t)
	sig, ok := model.Corroborate(episodeOn(12, 13, false), analysis)
	if !ok || sig.Hours != 2 || !sig.Start.Equal(hourAt(8, 12)) || !sig.End.Equal(hourAt(8, 13)) {
		t.Errorf("got %+v (found %v), want the two hours 12:00 and 13:00", sig, ok)
	}
	sig, ok = model.Corroborate(episodeOn(15, 20, false), analysis)
	if !ok || sig.Hours != 1 || !sig.Start.Equal(hourAt(8, 15)) {
		t.Errorf("got %+v (found %v), want only the 15:00 hour", sig, ok)
	}
}

func TestCorroborateOpenOnlyWhenTheLastHourIsUnusual(t *testing.T) {
	model, analysis := trained(t)
	if sig, _ := model.Corroborate(episodeOn(10, 15, true), analysis); !sig.Open {
		t.Error("the last unusual hour is the last of an open episode: the signal is open")
	}
	if sig, _ := model.Corroborate(episodeOn(10, 17, true), analysis); sig.Open {
		t.Error("two ordinary hours follow: the signal is closed")
	}
	if sig, _ := model.Corroborate(episodeOn(10, 15, false), analysis); sig.Open {
		t.Error("the episode is closed, so the signal is too")
	}
}

// A reading whose score equals the threshold counts.
func TestCorroborateThresholdIsInclusive(t *testing.T) {
	model, analysis := trained(t)
	var target domain.Reading
	for _, r := range analysis {
		if r.Timestamp.Equal(hourAt(8, 12)) {
			target = r
		}
	}
	model.cfg.Threshold = model.Score(target)
	if _, ok := model.Corroborate(episodeOn(12, 12, false), analysis); !ok {
		t.Error("a score equal to the threshold should corroborate")
	}
}

// The readings may arrive in any order.
func TestCorroborateDoesNotNeedSortedReadings(t *testing.T) {
	model, analysis := trained(t)
	slices.Reverse(analysis)
	sig, ok := model.Corroborate(episodeOn(8, 17, false), analysis)
	if !ok || !sig.Start.Equal(hourAt(8, 10)) || !sig.End.Equal(hourAt(8, 15)) {
		t.Errorf("got %v to %v (found %v), want 10:00 to 15:00", sig.Start, sig.End, ok)
	}
}

// Voltage and power factor are the ones that broke, so consumption must not
// come out on top.
func TestCorroborateNamesTheVariablesThatBroke(t *testing.T) {
	model, analysis := trainedOn(t, meterFault)
	sig, ok := model.Corroborate(episodeOn(8, 17, false), analysis)
	if !ok {
		t.Fatal("expected the faulty hours to corroborate the episode")
	}
	if sig.Variable != domain.Voltage && sig.Variable != domain.PowerFactor {
		t.Errorf("top variable = %s, want voltage or power factor", sig.Variable)
	}
	if sig.Attribution[domain.Consumption] > 0.15 {
		t.Errorf("consumption share = %.2f, want at most 0.15 (it did not move)", sig.Attribution[domain.Consumption])
	}
}
