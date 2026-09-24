package classify

import (
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func classify(t *testing.T, signals []detectors.Signal, events ...domain.Event) Result {
	t.Helper()
	eps := Group(signals, DefaultConfig())
	if len(eps) != 1 {
		t.Fatalf("got %d episodes, want 1", len(eps))
	}
	return Classify(eps[0], events, DefaultConfig())
}

func wantType(t *testing.T, res Result, typ domain.AnomalyType, rule Rule) {
	t.Helper()
	if res.Type != typ || res.Rule != rule {
		t.Fatalf("got %s by %s, want %s by %s", res.Type, res.Rule, typ, rule)
	}
}

func wantRole(t *testing.T, res Result, role EventRole) {
	t.Helper()
	if len(res.Events) != 1 || res.Events[0].Role != role {
		t.Fatalf("events = %+v, want one with role %s", res.Events, role)
	}
}

func isolatedJumps(meter string) []detectors.Signal {
	var out []detectors.Signal
	for h := 0; h < 30; h += 3 {
		out = append(out, quality(meter, h))
	}
	return out
}

func TestIsolatedElectricalReadingsAreDataQuality(t *testing.T) {
	res := classify(t, isolatedJumps("M-1"))
	wantType(t, res, domain.DataQuality, RuleIsolatedElectricalReadings)
}

func TestADataQualityEventOnlyCorroborates(t *testing.T) {
	res := classify(t, isolatedJumps("M-1"), event("M-1", domain.EventDataQuality, 0, 0))
	wantType(t, res, domain.DataQuality, RuleIsolatedElectricalReadings)
	wantRole(t, res, RoleCorroborates)
}

func TestADataQualityEventAloneDoesNotMakeDataQuality(t *testing.T) {
	res := classify(t, []detectors.Signal{shift("M-1", 0, 20, 1)}, event("M-1", domain.EventDataQuality, 0, 0))
	wantType(t, res, domain.RealAnomaly, RuleNoExplainingEvent)
	wantRole(t, res, RoleNotExplanatory)
}

func TestALastingChangeWithStrayBadReadingsIsNotDataQuality(t *testing.T) {
	res := classify(t, []detectors.Signal{shift("M-1", 0, 20, 1), quality("M-1", 5)})
	wantType(t, res, domain.RealAnomaly, RuleNoExplainingEvent)
}

func TestAnUnknownEventNeverExplains(t *testing.T) {
	res := classify(t, []detectors.Signal{shift("M-1", 0, 20, 1)}, event("M-1", domain.EventUnknown, 0, 0))
	wantType(t, res, domain.RealAnomaly, RuleNoExplainingEvent)
	wantRole(t, res, RoleNotExplanatory)
}

func TestWithoutEventsAnAnomalyIsReal(t *testing.T) {
	res := classify(t, []detectors.Signal{shift("M-1", 0, 20, 1)})
	wantType(t, res, domain.RealAnomaly, RuleNoExplainingEvent)
	if len(res.Events) != 0 {
		t.Errorf("events = %+v", res.Events)
	}
}

func TestAnOperationalChangeExplainsARise(t *testing.T) {
	res := classify(t, []detectors.Signal{shift("M-1", 0, 20, 1)}, event("M-1", domain.EventOperationalChange, 0, 0))
	wantType(t, res, domain.ExplainableAnomaly, RuleOperationalChange)
	wantRole(t, res, RoleExplains)
}

func TestAnOperationalChangeDoesNotExplainADrop(t *testing.T) {
	res := classify(t, []detectors.Signal{shift("M-1", 0, 20, -1)}, event("M-1", domain.EventOperationalChange, 0, 0))
	wantType(t, res, domain.RealAnomaly, RuleNoExplainingEvent)
	wantRole(t, res, RoleIncompatible)
}

func TestAScheduledOutageExplainsADropOfTheSameLength(t *testing.T) {
	res := classify(t, []detectors.Signal{shift("M-1", 0, 12, -1)}, event("M-1", domain.EventScheduledOutage, 0, 12*time.Hour))
	wantType(t, res, domain.FalsePositive, RuleScheduledOutage)
	wantRole(t, res, RoleExplains)
}

func TestAnOutageMayOverrunByTheMarginButNotMore(t *testing.T) {
	outage := event("M-1", domain.EventScheduledOutage, 0, 12*time.Hour)
	wantType(t, classify(t, []detectors.Signal{shift("M-1", 0, 13, -1)}, outage), domain.FalsePositive, RuleScheduledOutage)
	res := classify(t, []detectors.Signal{shift("M-1", 0, 14, -1)}, outage)
	wantType(t, res, domain.RealAnomaly, RuleNoExplainingEvent)
	wantRole(t, res, RoleIncompatible)
}

func TestAnOutageWithoutADurationExplainsAnyLength(t *testing.T) {
	res := classify(t, []detectors.Signal{shift("M-1", 0, 40, -1)}, event("M-1", domain.EventScheduledOutage, 0, 0))
	wantType(t, res, domain.FalsePositive, RuleScheduledOutage)
}

func TestAnOutageDoesNotExplainARise(t *testing.T) {
	res := classify(t, []detectors.Signal{shift("M-1", 0, 12, 1)}, event("M-1", domain.EventScheduledOutage, 0, 12*time.Hour))
	wantType(t, res, domain.RealAnomaly, RuleNoExplainingEvent)
	wantRole(t, res, RoleIncompatible)
}

func TestElectricalEvidenceBeatsAnExplainingEvent(t *testing.T) {
	res := classify(t, []detectors.Signal{shift("M-1", 0, 20, 1), powerFactorDrop("M-1", 0, 20)}, event("M-1", domain.EventOperationalChange, 0, 0))
	wantType(t, res, domain.RealAnomaly, RuleElectricalEvidence)
	wantRole(t, res, RoleContradictedByElectrical)
}

func TestEventsOutsideTheWindowOrOfOtherMetersAreIgnored(t *testing.T) {
	signals := []detectors.Signal{shift("M-1", 20, 20, 1)}
	res := classify(t, signals,
		event("M-1", domain.EventOperationalChange, 27, 0),
		event("M-2", domain.EventOperationalChange, 20, 0),
	)
	wantType(t, res, domain.RealAnomaly, RuleNoExplainingEvent)
	if len(res.Events) != 0 {
		t.Errorf("events = %+v", res.Events)
	}
}

func TestTheEventWindowIncludesItsEdges(t *testing.T) {
	signals := []detectors.Signal{shift("M-1", 20, 20, 1)}
	after := classify(t, signals, event("M-1", domain.EventOperationalChange, 26, 0))
	wantType(t, after, domain.ExplainableAnomaly, RuleOperationalChange)
	before := classify(t, signals, event("M-1", domain.EventOperationalChange, 14, 0))
	wantType(t, before, domain.ExplainableAnomaly, RuleOperationalChange)
	if before.Events[0].OffsetHours != -6 || after.Events[0].OffsetHours != 6 {
		t.Errorf("offsets %v and %v", before.Events[0].OffsetHours, after.Events[0].OffsetHours)
	}
}
