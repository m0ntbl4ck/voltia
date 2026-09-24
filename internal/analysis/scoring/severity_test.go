package scoring

import (
	"testing"

	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func severityOf(res classify.Result) domain.Severity {
	return Evaluate(res, DefaultConfig()).Severity
}

func TestSeverityOfARealAnomalyFollowsItsVariation(t *testing.T) {
	cases := []struct {
		name string
		sig  float64
		want domain.Severity
	}{
		{"above half", 110, domain.SeverityHigh},
		{"exactly half", 50, domain.SeverityHigh},
		{"between", 30, domain.SeverityMedium},
		{"exactly a fifth", 20, domain.SeverityMedium},
		{"small", 10, domain.SeverityLow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil, consumption(20, 1, c.sig))
			if got := severityOf(res); got != c.want {
				t.Errorf("severity = %s, want %s", got, c.want)
			}
		})
	}
}

func TestSeverityCountsADropAsMuchAsARise(t *testing.T) {
	res := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, false, nil, consumption(20, -1, 60))
	if got := severityOf(res); got != domain.SeverityHigh {
		t.Errorf("severity = %s, want HIGH", got)
	}
}

func TestSeverityOfARealAnomalyIsHighWithElectricalEvidence(t *testing.T) {
	res := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 20, true, nil, consumption(20, 1, 10), powerFactorDrop(20))
	if got := severityOf(res); got != domain.SeverityHigh {
		t.Errorf("severity = %s, want HIGH", got)
	}
}

func TestSeverityOfARealAnomalyWithoutAShiftIsLow(t *testing.T) {
	res := result(domain.RealAnomaly, classify.RuleNoExplainingEvent, 1, true, nil)
	if got := severityOf(res); got != domain.SeverityLow {
		t.Errorf("severity = %s, want LOW", got)
	}
}

func TestAnExplainableAnomalyIsNeverHigh(t *testing.T) {
	big := result(domain.ExplainableAnomaly, classify.RuleOperationalChange, 96, true, nil, consumption(96, 1, 300))
	if got := severityOf(big); got != domain.SeverityMedium {
		t.Errorf("large explainable severity = %s, want MEDIUM", got)
	}
	small := result(domain.ExplainableAnomaly, classify.RuleOperationalChange, 96, true, nil, consumption(96, 1, 10))
	if got := severityOf(small); got != domain.SeverityLow {
		t.Errorf("small explainable severity = %s, want LOW", got)
	}
}

func TestAFalsePositiveIsAlwaysLow(t *testing.T) {
	res := result(domain.FalsePositive, classify.RuleScheduledOutage, 12, false, nil, consumption(12, -1, 80))
	if got := severityOf(res); got != domain.SeverityLow {
		t.Errorf("severity = %s, want LOW", got)
	}
}

func TestSeverityOfDataQualityFollowsTheInvalidReadings(t *testing.T) {
	cases := []struct {
		readings int
		open     bool
		want     domain.Severity
	}{
		{16, false, domain.SeverityHigh},
		{5, false, domain.SeverityHigh},
		{4, false, domain.SeverityMedium},
		{2, false, domain.SeverityMedium},
		{1, false, domain.SeverityLow},
		{1, true, domain.SeverityHigh},
	}
	for _, c := range cases {
		res := result(domain.DataQuality, classify.RuleIsolatedElectricalReadings, 30, c.open, nil, invalid(c.readings))
		if got := severityOf(res); got != c.want {
			t.Errorf("%d readings open=%v: severity = %s, want %s", c.readings, c.open, got, c.want)
		}
	}
}
