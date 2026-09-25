package domain

import (
	"errors"
	"slices"
	"testing"
)

func TestNextStatusFollowsTheLifeCycle(t *testing.T) {
	allowed := map[AnomalyStatus]map[Action]AnomalyStatus{
		StatusOpen: {
			ActionCreateInspectionOrder:  StatusAcknowledged,
			ActionRequestMeterValidation: StatusAcknowledged,
			ActionConfirmOperation:       StatusResolved,
			ActionDismiss:                StatusDismissed,
		},
		StatusAcknowledged: {ActionResolve: StatusResolved},
	}
	statuses := []AnomalyStatus{StatusOpen, StatusAcknowledged, StatusResolved, StatusDismissed}
	actions := []Action{ActionCreateInspectionOrder, ActionRequestMeterValidation, ActionConfirmOperation, ActionDismiss, ActionResolve}
	for _, from := range statuses {
		for _, a := range actions {
			got, err := NextStatus(from, a)
			want, ok := allowed[from][a]
			switch {
			case ok && (err != nil || got != want):
				t.Errorf("%s + %s = %q, %v; want %s", from, a, got, err, want)
			case !ok && !errors.Is(err, ErrInvalidTransition):
				t.Errorf("%s + %s = %q, %v; want ErrInvalidTransition", from, a, got, err)
			}
		}
	}
}

func TestFinalStatesAllowNothing(t *testing.T) {
	for _, s := range []AnomalyStatus{StatusResolved, StatusDismissed} {
		if got := AvailableActions(s); len(got) != 0 {
			t.Errorf("%s allows %v, want nothing", s, got)
		}
	}
}

func TestAvailableActions(t *testing.T) {
	open := AvailableActions(StatusOpen)
	want := []Action{ActionConfirmOperation, ActionCreateInspectionOrder, ActionDismiss, ActionRequestMeterValidation}
	if !slices.Equal(open, want) {
		t.Errorf("open allows %v, want %v", open, want)
	}
	if got := AvailableActions(StatusAcknowledged); !slices.Equal(got, []Action{ActionResolve}) {
		t.Errorf("acknowledged allows %v", got)
	}
	if got := AvailableActions("SOMETHING"); len(got) != 0 {
		t.Errorf("an unknown status allows %v", got)
	}
}

func TestParseAction(t *testing.T) {
	for _, ok := range []string{"CREATE_INSPECTION_ORDER", "REQUEST_METER_VALIDATION", "CONFIRM_OPERATION", "DISMISS", "RESOLVE"} {
		if a, err := ParseAction(ok); err != nil || string(a) != ok {
			t.Errorf("ParseAction(%q) = %q, %v", ok, a, err)
		}
	}
	for _, bad := range []string{"", "dismiss", "DELETE", "RESOLVE ", "OPEN"} {
		if _, err := ParseAction(bad); !errors.Is(err, ErrInvalidAction) {
			t.Errorf("ParseAction(%q) error = %v, want ErrInvalidAction", bad, err)
		}
	}
}

func TestMainActionPerType(t *testing.T) {
	want := map[AnomalyType]Action{
		RealAnomaly:        ActionCreateInspectionOrder,
		DataQuality:        ActionRequestMeterValidation,
		ExplainableAnomaly: ActionConfirmOperation,
		FalsePositive:      ActionDismiss,
	}
	for typ, action := range want {
		if got := MainAction(typ); got != action {
			t.Errorf("MainAction(%s) = %s, want %s", typ, got, action)
		}
		if _, err := NextStatus(StatusOpen, MainAction(typ)); err != nil {
			t.Errorf("the main action of %s cannot be applied to an open anomaly: %v", typ, err)
		}
	}
}
