package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// openAnomaly stores one open anomaly and a user to act on it.
func openAnomaly(t *testing.T) (*Repository, domain.Anomaly, domain.User) {
	t.Helper()
	repo, _, loc := seededRepository(t)
	ctx := context.Background()
	user, err := repo.SaveUser(ctx, "ana@example.com", "Ana", "hash")
	if err != nil {
		t.Fatal(err)
	}
	a := sampleAnomaly(newRun(t, repo), "M-109", domain.RealAnomaly, domain.SeverityHigh, 100, time.Date(2026, 9, 12, 14, 0, 0, 0, loc))
	if err := repo.UpsertAnomalies(ctx, []domain.Anomaly{a}); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Anomalies(ctx, domain.AnomalyFilter{})
	if err != nil || len(stored) != 1 {
		t.Fatalf("Anomalies = %v, %v", stored, err)
	}
	return repo, stored[0], user
}

func TestActionsWalkTheLifeCycleAndLeaveAHistory(t *testing.T) {
	repo, a, user := openAnomaly(t)
	ctx := context.Background()

	got, err := repo.ApplyAction(ctx, a.ID, user.ID, domain.ActionCreateInspectionOrder, "Send Luis to the mill")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusAcknowledged || got.ID != a.ID {
		t.Errorf("after the order: %s %s", got.ID, got.Status)
	}
	if got, err = repo.ApplyAction(ctx, a.ID, user.ID, domain.ActionResolve, ""); err != nil || got.Status != domain.StatusResolved {
		t.Fatalf("after resolving: %+v, %v", got.Status, err)
	}

	stored, err := repo.Anomaly(ctx, a.ID)
	if err != nil || stored.Status != domain.StatusResolved {
		t.Errorf("stored status = %s, %v", stored.Status, err)
	}
	history, err := repo.AnomalyActions(ctx, a.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history = %+v, %v", history, err)
	}
	first, second := history[0], history[1]
	if first.Action != domain.ActionCreateInspectionOrder || first.From != domain.StatusOpen || first.To != domain.StatusAcknowledged ||
		first.Note != "Send Luis to the mill" || first.UserName != "Ana" || first.UserID != user.ID || first.CreatedAt.IsZero() {
		t.Errorf("first entry = %+v", first)
	}
	if second.Action != domain.ActionResolve || second.From != domain.StatusAcknowledged || second.To != domain.StatusResolved || second.Note != "" {
		t.Errorf("second entry = %+v", second)
	}
	if second.CreatedAt.Before(first.CreatedAt) {
		t.Error("the history is not oldest first")
	}
}

func TestAnInvalidActionChangesNothing(t *testing.T) {
	repo, a, user := openAnomaly(t)
	ctx := context.Background()
	if _, err := repo.ApplyAction(ctx, a.ID, user.ID, domain.ActionCreateInspectionOrder, ""); err != nil {
		t.Fatal(err)
	}

	_, err := repo.ApplyAction(ctx, a.ID, user.ID, domain.ActionDismiss, "too late")
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("error = %v, want ErrInvalidTransition", err)
	}
	stored, _ := repo.Anomaly(ctx, a.ID)
	history, _ := repo.AnomalyActions(ctx, a.ID)
	if stored.Status != domain.StatusAcknowledged || len(history) != 1 {
		t.Errorf("status %s with %d history entries, want ACKNOWLEDGED with 1", stored.Status, len(history))
	}
}

func TestActionOnAnUnknownAnomalyIsNotFound(t *testing.T) {
	repo, _, user := openAnomaly(t)
	if _, err := repo.ApplyAction(context.Background(), missingID, user.ID, domain.ActionDismiss, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestTwoOperatorsActingAtOnceCannotBothWin(t *testing.T) {
	repo, a, user := openAnomaly(t)
	ctx := context.Background()
	actions := []domain.Action{domain.ActionConfirmOperation, domain.ActionDismiss, domain.ActionCreateInspectionOrder, domain.ActionRequestMeterValidation}

	// Every goroutine waits at the gate, so they all read the anomaly while it is still open.
	var wg sync.WaitGroup
	gate := make(chan struct{})
	results := make([]error, 32)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, results[i] = repo.ApplyAction(ctx, a.ID, user.ID, actions[i%len(actions)], "")
		}()
	}
	close(gate)
	wg.Wait()

	won := 0
	for _, err := range results {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, domain.ErrInvalidTransition) && !errors.Is(err, domain.ErrNotFound):
			t.Errorf("unexpected error: %v", err)
		}
	}
	history, _ := repo.AnomalyActions(ctx, a.ID)
	if won != 1 || len(history) != 1 {
		t.Errorf("%d actions succeeded and %d were recorded, want exactly 1 of each", won, len(history))
	}
}

func TestHistoryOfAnUntouchedAnomalyIsEmpty(t *testing.T) {
	repo, a, _ := openAnomaly(t)
	history, err := repo.AnomalyActions(context.Background(), a.ID)
	if err != nil || len(history) != 0 {
		t.Errorf("history = %+v, %v", history, err)
	}
}
