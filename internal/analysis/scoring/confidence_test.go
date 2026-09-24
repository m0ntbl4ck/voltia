package scoring

import (
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func partsOf(res classify.Result) ConfidenceBreakdown {
	return Evaluate(res, DefaultConfig()).ConfidenceParts
}

func TestAgreementCountsIndependentSources(t *testing.T) {
	one := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil, consumption(20, 1, 60))
	twoVariables := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil,
		consumption(20, 1, 60), shift(domain.Current, 20, 1, 200, 100, 10))
	three := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil,
		consumption(20, 1, 60), powerFactorDrop(20), invalid(1))
	near(t, "one source", partsOf(one).DetectorAgreement, 0.5, 1e-9)
	near(t, "one kind on two variables", partsOf(twoVariables).DetectorAgreement, 0.75, 1e-9)
	near(t, "three kinds", partsOf(three).DetectorAgreement, 1, 1e-9)
}

// The forest backs up the episode as one more independent source.
func TestAgreementCountsTheIsolationForest(t *testing.T) {
	alone := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil, consumption(20, 1, 60))
	backed := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil, consumption(20, 1, 60), isolation(15, 0.9))
	near(t, "alone", partsOf(alone).DetectorAgreement, 0.5, 1e-9)
	near(t, "backed up", partsOf(backed).DetectorAgreement, 0.75, 1e-9)
}

// Its score is not a z-score and its hours lie inside the other signals', so
// it must not raise the strength: the 12 hours below would otherwise turn
// half persistence into full persistence.
func TestStrengthIgnoresTheIsolationForest(t *testing.T) {
	sig := shift(domain.Consumption, 6, 1, 200, 100, 4)
	alone := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 12, true, nil, sig)
	backed := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 12, true, nil, sig, isolation(12, 0.99))
	near(t, "alone", partsOf(alone).SignalStrength, 0.5, 1e-9)
	near(t, "backed up", partsOf(backed).SignalStrength, partsOf(alone).SignalStrength, 1e-9)
}

// Corroboration feeds the confidence only: type, severity and priority stay.
func TestIsolationForestDoesNotChangeSeverityOrPriority(t *testing.T) {
	plain := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil, consumption(20, 1, 60))
	backed := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil, consumption(20, 1, 60), isolation(20, 0.95))
	a, b := Evaluate(plain, DefaultConfig()), Evaluate(backed, DefaultConfig())
	if a.Severity != b.Severity || a.Priority != b.Priority || a.VariationPct != b.VariationPct || a.ExcessKWh != b.ExcessKWh {
		t.Errorf("without: %+v, with: %+v", a, b)
	}
	if b.Confidence <= a.Confidence {
		t.Errorf("confidence %.4f with the forest, %.4f without: the forest should raise it", b.Confidence, a.Confidence)
	}
}

func TestStrengthCombinesDeviationAndPersistence(t *testing.T) {
	full := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 12, true, nil, shift(domain.Consumption, 12, 1, 200, 100, 8))
	half := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 6, true, nil, shift(domain.Consumption, 6, 1, 200, 100, 4))
	near(t, "full marks", partsOf(full).SignalStrength, 1, 1e-9)
	near(t, "half marks", partsOf(half).SignalStrength, 0.5, 1e-9)
}

func TestIntegrityFallsWithInvalidReadings(t *testing.T) {
	clean := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil, consumption(20, 1, 60))
	dirty := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil, consumption(20, 1, 60), invalid(5))
	near(t, "clean", partsOf(clean).DataIntegrity, 1, 1e-9)
	near(t, "a quarter invalid", partsOf(dirty).DataIntegrity, 0.75, 1e-9)
}

func TestIntegrityDoesNotApplyToDataQuality(t *testing.T) {
	res := result(domain.DataQuality, classify.RuleIsolatedElectricalReadings, 46, false,
		[]classify.EventLink{link(domain.EventDataQuality, classify.RoleCorroborates, 0, 0)}, invalid(16))
	s := Evaluate(res, DefaultConfig())
	if s.ConfidenceParts.IntegrityApplies {
		t.Fatal("integrity should not apply to a data quality episode")
	}
	// Agreement 0.5, strength 1, clarity 1, weights renormalised without integrity.
	near(t, "confidence", s.Confidence, (0.35*0.5+0.30*1+0.25*1)/0.90, 1e-9)
}

func TestClarityOfARealAnomaly(t *testing.T) {
	sig := consumption(20, 1, 60)
	cases := []struct {
		name   string
		rule   classify.Rule
		events []classify.EventLink
		want   float64
	}{
		{"no event nearby", classify.RuleNoExplainingEvent, nil, 1},
		{"an unknown event", classify.RuleNoExplainingEvent, []classify.EventLink{link(domain.EventUnknown, classify.RoleNotExplanatory, 0, 0)}, 0.95},
		{"an event that does not fit", classify.RuleNoExplainingEvent, []classify.EventLink{link(domain.EventScheduledOutage, classify.RoleIncompatible, 0, 0)}, 0.9},
		{"an event the electrical evidence contradicts", classify.RuleElectricalEvidence, []classify.EventLink{link(domain.EventOperationalChange, classify.RoleContradictedByElectrical, 0, 0)}, 0.8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			near(t, "clarity", partsOf(result(domain.RealAnomaly, c.rule, 20, true, c.events, sig)).ClassificationClarity, c.want, 1e-9)
		})
	}
}

func TestClarityOfAFalsePositiveDependsOnHowTheOutageMatches(t *testing.T) {
	sig := consumption(12, -1, 80)
	exact := []classify.EventLink{link(domain.EventScheduledOutage, classify.RoleExplains, 0, 12*time.Hour)}
	late := []classify.EventLink{link(domain.EventScheduledOutage, classify.RoleExplains, 3, 12*time.Hour)}
	unstated := []classify.EventLink{link(domain.EventScheduledOutage, classify.RoleExplains, 0, 0)}
	near(t, "exact match", partsOf(result(domain.FalsePositive, classify.RuleScheduledOutage, 12, false, exact, sig)).ClassificationClarity, 1, 1e-9)
	near(t, "three hours late", partsOf(result(domain.FalsePositive, classify.RuleScheduledOutage, 12, false, late, sig)).ClassificationClarity, 0.75, 1e-9)
	near(t, "no stated duration", partsOf(result(domain.FalsePositive, classify.RuleScheduledOutage, 12, false, unstated, sig)).ClassificationClarity, 0.85, 1e-9)
}

func TestClarityOfAnExplainableAnomalyDependsOnTheAlignment(t *testing.T) {
	sig := consumption(96, 1, 47)
	at := func(offset float64) []classify.EventLink {
		return []classify.EventLink{link(domain.EventOperationalChange, classify.RoleExplains, offset, 0)}
	}
	near(t, "aligned", partsOf(result(domain.ExplainableAnomaly, classify.RuleOperationalChange, 96, true, at(0), sig)).ClassificationClarity, 1, 1e-9)
	near(t, "at the edge", partsOf(result(domain.ExplainableAnomaly, classify.RuleOperationalChange, 96, true, at(-6), sig)).ClassificationClarity, 0.4, 1e-9)
}

func TestClarityOfADataQualityEpisodeGrowsWhenAnEventCorroborates(t *testing.T) {
	alone := result(domain.DataQuality, classify.RuleIsolatedElectricalReadings, 46, false, nil, invalid(16))
	near(t, "alone", partsOf(alone).ClassificationClarity, 0.75, 1e-9)
}

func TestConfidenceStaysWithinTheFloorAndTheCeiling(t *testing.T) {
	weak := result(domain.RealAnomaly, classify.RuleElectricalEvidence, 1, true,
		[]classify.EventLink{link(domain.EventOperationalChange, classify.RoleContradictedByElectrical, 0, 0)},
		detectors.Signal{Kind: detectors.KindOutlier, Variable: domain.Voltage, Hours: 1, MeanZ: 0.1, Start: t0, End: t0})
	near(t, "floor", Evaluate(weak, DefaultConfig()).Confidence, 0.5, 1e-9)

	strong := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 58, true, nil,
		shift(domain.Consumption, 58, 1, 240, 100, 20), shift(domain.Current, 58, 1, 400, 200, 22), powerFactorDrop(58))
	near(t, "ceiling", Evaluate(strong, DefaultConfig()).Confidence, 0.99, 1e-9)
}

func TestBandsFollowTheThresholds(t *testing.T) {
	cfg := DefaultConfidenceConfig()
	cases := map[float64]Band{0.99: BandHigh, 0.85: BandHigh, 0.849: BandMediumHigh, 0.75: BandMediumHigh, 0.749: BandMedium, 0.60: BandMedium, 0.599: BandLow}
	for c, want := range cases {
		if got := band(c, cfg); got != want {
			t.Errorf("band(%v) = %s, want %s", c, got, want)
		}
	}
}

func TestAggregateConfidenceWeighsBySeverity(t *testing.T) {
	cfg := DefaultConfidenceConfig()
	scores := []Score{
		{Severity: domain.SeverityHigh, Confidence: 0.9},
		{Severity: domain.SeverityLow, Confidence: 0.5},
	}
	near(t, "weighted", AggregateConfidence(scores, cfg), (3*0.9+1*0.5)/4, 1e-9)
	near(t, "empty", AggregateConfidence(nil, cfg), 0, 1e-9)
}
