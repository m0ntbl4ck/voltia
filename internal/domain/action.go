package domain

import (
	"errors"
	"slices"
	"time"
)

// Action is what an operator does about an anomaly.
type Action string

const (
	ActionCreateInspectionOrder  Action = "CREATE_INSPECTION_ORDER"
	ActionRequestMeterValidation Action = "REQUEST_METER_VALIDATION"
	ActionConfirmOperation       Action = "CONFIRM_OPERATION"
	ActionDismiss                Action = "DISMISS"
	ActionResolve                Action = "RESOLVE"
)

// ErrInvalidAction is returned for an action name that does not exist.
var ErrInvalidAction = errors.New("unknown action")

// ErrInvalidTransition is returned when an action cannot be applied to an
// anomaly in its current status.
var ErrInvalidTransition = errors.New("the action is not allowed in the current status")

// transitions is the life cycle of an anomaly: an open one is acknowledged,
// resolved or dismissed, an acknowledged one can then be resolved, and the
// other two states are final.
var transitions = map[AnomalyStatus]map[Action]AnomalyStatus{
	StatusOpen: {
		ActionCreateInspectionOrder:  StatusAcknowledged,
		ActionRequestMeterValidation: StatusAcknowledged,
		ActionConfirmOperation:       StatusResolved,
		ActionDismiss:                StatusDismissed,
	},
	StatusAcknowledged: {
		ActionResolve: StatusResolved,
	},
}

// ParseAction validates an action name.
func ParseAction(s string) (Action, error) {
	a := Action(s)
	for _, from := range transitions {
		if _, ok := from[a]; ok {
			return a, nil
		}
	}
	return "", ErrInvalidAction
}

// NextStatus returns the status an anomaly moves to when action is applied in
// status from, or ErrInvalidTransition.
func NextStatus(from AnomalyStatus, action Action) (AnomalyStatus, error) {
	if to, ok := transitions[from][action]; ok {
		return to, nil
	}
	return "", ErrInvalidTransition
}

// AvailableActions lists what can be done in status, in a stable order.
func AvailableActions(status AnomalyStatus) []Action {
	out := make([]Action, 0, len(transitions[status]))
	for a := range transitions[status] {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// MainAction is the action the platform recommends for an open anomaly of the type.
func MainAction(t AnomalyType) Action {
	switch t {
	case RealAnomaly:
		return ActionCreateInspectionOrder
	case DataQuality:
		return ActionRequestMeterValidation
	case ExplainableAnomaly:
		return ActionConfirmOperation
	}
	return ActionDismiss
}

// AnomalyAction is one entry in the history of an anomaly.
type AnomalyAction struct {
	ID        string
	AnomalyID string
	UserID    string
	UserName  string
	Action    Action
	Note      string
	From      AnomalyStatus
	To        AnomalyStatus
	CreatedAt time.Time
}
