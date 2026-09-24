package classify

import (
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// Rule names the branch of the decision that produced the type.
type Rule string

const (
	RuleIsolatedElectricalReadings Rule = "isolated_electrical_readings"
	RuleElectricalEvidence         Rule = "electrical_evidence_overrides_event"
	RuleScheduledOutage            Rule = "scheduled_outage"
	RuleOperationalChange          Rule = "operational_change"
	RuleNoExplainingEvent          Rule = "no_explaining_event"
)

// EventRole is what an event did for the episode it sits next to.
type EventRole string

const (
	RoleExplains                 EventRole = "EXPLAINS"
	RoleCorroborates             EventRole = "CORROBORATES"
	RoleNotExplanatory           EventRole = "NOT_EXPLANATORY"
	RoleIncompatible             EventRole = "INCOMPATIBLE"
	RoleContradictedByElectrical EventRole = "CONTRADICTED_BY_ELECTRICAL"
)

// EventLink records how one event related to the episode.
type EventLink struct {
	Event domain.Event
	Role  EventRole
	// OffsetHours is the event time minus the episode start; negative when the
	// event came first.
	OffsetHours float64
}

// Result is an episode with the type decided for it.
type Result struct {
	Episode   Episode
	Type      domain.AnomalyType
	Rule      Rule
	Direction int
	Events    []EventLink
}

// Classify decides what an episode is, in this order: isolated electrical
// readings with no lasting change are data quality, an event that fits explains
// the episode unless the electrical evidence disagrees, and anything else is
// real. An UNKNOWN event never explains, and a DATA_QUALITY event only corroborates.
func Classify(ep Episode, events []domain.Event, cfg Config) Result {
	res := Result{Episode: ep, Direction: ep.Direction()}
	quality := ep.Has(detectors.KindDataQuality)
	lasting := ep.Has(detectors.KindPersistentShift, detectors.KindElectricalRelation)
	res.Events = links(ep, events, res.Direction, quality, cfg)

	if quality && !lasting {
		res.Type, res.Rule = domain.DataQuality, RuleIsolatedElectricalReadings
		return res
	}
	for i, l := range res.Events {
		if l.Role != RoleExplains {
			continue
		}
		if ep.Has(detectors.KindElectricalRelation) {
			res.Events[i].Role = RoleContradictedByElectrical
			res.Type, res.Rule = domain.RealAnomaly, RuleElectricalEvidence
			return res
		}
		if l.Event.Type == domain.EventScheduledOutage {
			res.Type, res.Rule = domain.FalsePositive, RuleScheduledOutage
		} else {
			res.Type, res.Rule = domain.ExplainableAnomaly, RuleOperationalChange
		}
		return res
	}
	res.Type, res.Rule = domain.RealAnomaly, RuleNoExplainingEvent
	return res
}

func links(ep Episode, events []domain.Event, direction int, quality bool, cfg Config) []EventLink {
	var out []EventLink
	for _, e := range events {
		offset := e.Timestamp.Sub(ep.Start)
		if e.MeterID != ep.MeterID || offset > cfg.EventWindow || offset < -cfg.EventWindow {
			continue
		}
		out = append(out, EventLink{Event: e, Role: roleOf(e, ep, direction, quality, cfg), OffsetHours: offset.Hours()})
	}
	return out
}

func roleOf(e domain.Event, ep Episode, direction int, quality bool, cfg Config) EventRole {
	switch e.Type {
	case domain.EventDataQuality:
		if quality {
			return RoleCorroborates
		}
		return RoleNotExplanatory
	case domain.EventScheduledOutage:
		if direction == -1 && fitsDuration(ep.Duration(), e.Duration, cfg.OutageMargin) {
			return RoleExplains
		}
		return RoleIncompatible
	case domain.EventOperationalChange:
		if direction == 1 {
			return RoleExplains
		}
		return RoleIncompatible
	}
	return RoleNotExplanatory
}

// fitsDuration accepts an outage that lasted no longer than the scheduled one,
// or any outage when the event states no duration.
func fitsDuration(episode, scheduled, margin time.Duration) bool {
	return scheduled == 0 || episode <= scheduled+margin
}
