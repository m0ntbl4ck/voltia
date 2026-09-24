package scoring

import (
	"testing"

	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// excess builds a rise of the given size for 10 hours, so the excess is
// hours times the difference: 10 hours at +125 kWh is 1250 kWh.
func excess(kwhPerHour float64) classify.Result {
	sig := shift(domain.Consumption, 10, 1, 100+kwhPerHour, 100, 10)
	return result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 10, false, nil, sig)
}

func TestPriorityAddsSeverityTypeImpactAndRecency(t *testing.T) {
	res := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 58, true, nil, shift(domain.Consumption, 58, 1, 240, 100, 20), powerFactorDrop(58))
	s := Evaluate(res, DefaultConfig())
	b := s.PriorityBreakdown
	if b.Severity != 50 || b.Type != 25 || b.Impact != 15 || b.Recency != 10 || s.Priority != 100 {
		t.Errorf("breakdown %+v priority %d, want 50 + 25 + 15 + 10 = 100", b, s.Priority)
	}
}

func TestImpactIsProportionalToTheExcessAndCapped(t *testing.T) {
	near(t, "half of the full excess", Evaluate(excess(125), DefaultConfig()).PriorityBreakdown.Impact, 7.5, 1e-9)
	near(t, "beyond the full excess", Evaluate(excess(1000), DefaultConfig()).PriorityBreakdown.Impact, 15, 1e-9)
}

func TestADropEarnsNoImpact(t *testing.T) {
	res := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 12, false, nil, consumption(12, -1, 80))
	if got := Evaluate(res, DefaultConfig()).PriorityBreakdown.Impact; got != 0 {
		t.Errorf("impact = %v, want 0", got)
	}
}

func TestRecencyOnlyCountsForEpisodesStillOpen(t *testing.T) {
	closed := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 10, false, nil, consumption(10, 1, 60))
	open := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 10, true, nil, consumption(10, 1, 60))
	near(t, "closed", Evaluate(closed, DefaultConfig()).PriorityBreakdown.Recency, 0, 1e-9)
	near(t, "open", Evaluate(open, DefaultConfig()).PriorityBreakdown.Recency, 10, 1e-9)
}

func TestPriorityIsRoundedAndCappedAtOneHundred(t *testing.T) {
	near(t, "rounded", float64(PriorityBreakdown{Severity: 25, Type: 5, Impact: 13.07, Recency: 10}.Total()), 53, 0)
	near(t, "capped", float64(PriorityBreakdown{Severity: 50, Type: 25, Impact: 15, Recency: 10}.Total()), 100, 0)
	near(t, "over the cap", float64(PriorityBreakdown{Severity: 90, Type: 25, Impact: 15, Recency: 10}.Total()), 100, 0)
}

func TestARealHighOpenAnomalyOutranksTheRest(t *testing.T) {
	cfg := DefaultConfig()
	real := Evaluate(result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 58, true, nil, shift(domain.Consumption, 58, 1, 240, 100, 20)), cfg)
	quality := Evaluate(result(domain.DataQuality, classify.RuleIsolatedElectricalReadings, 46, false, nil, invalid(16)), cfg)
	explainable := Evaluate(result(domain.ExplainableAnomaly, classify.RuleOperationalChange, 96, true, nil, consumption(96, 1, 47)), cfg)
	falsePositive := Evaluate(result(domain.FalsePositive, classify.RuleScheduledOutage, 12, false, nil, consumption(12, -1, 80)), cfg)
	if !(real.Priority > quality.Priority && quality.Priority > explainable.Priority && explainable.Priority > falsePositive.Priority) {
		t.Errorf("priorities real=%d quality=%d explainable=%d falsePositive=%d", real.Priority, quality.Priority, explainable.Priority, falsePositive.Priority)
	}
}

func TestExcessOnlyCountsTheRisesOfAMixedEpisode(t *testing.T) {
	rise := shift(domain.Consumption, 10, 1, 225, 100, 10) // 10 hours at +125 kWh: 1250 kWh
	drop := shift(domain.Consumption, 10, -1, 50, 100, -10)
	s := Evaluate(result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, false, nil, rise, drop), DefaultConfig())
	near(t, "excess", s.ExcessKWh, 1250, 1e-9)
	near(t, "impact", s.PriorityBreakdown.Impact, 7.5, 1e-9)
}
