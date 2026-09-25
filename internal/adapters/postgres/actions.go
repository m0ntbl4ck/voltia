package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/m0ntbl4ck/voltia/internal/adapters/postgres/sqlcgen"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// ApplyAction moves an anomaly along its life cycle and records who did it.
// It locks the row first, so two operators acting at once cannot both succeed:
// the second sees the new status and gets domain.ErrInvalidTransition. It
// returns domain.ErrNotFound for an unknown anomaly.
func (r *Repository) ApplyAction(ctx context.Context, anomalyID, userID string, action domain.Action, note string) (domain.Anomaly, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Anomaly{}, err
	}
	defer tx.Rollback()
	q := r.q.WithTx(tx)

	row, err := q.GetAnomalyForUpdate(ctx, anomalyID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Anomaly{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Anomaly{}, err
	}
	from := domain.AnomalyStatus(row.Status)
	to, err := domain.NextStatus(from, action)
	if err != nil {
		return domain.Anomaly{}, err
	}
	if err := q.SetAnomalyStatus(ctx, sqlcgen.SetAnomalyStatusParams{ID: anomalyID, Status: string(to)}); err != nil {
		return domain.Anomaly{}, err
	}
	if _, err := q.InsertAnomalyAction(ctx, sqlcgen.InsertAnomalyActionParams{
		AnomalyID: anomalyID, UserID: userID, Action: string(action), Note: nullable(note),
		FromStatus: string(from), ToStatus: string(to),
	}); err != nil {
		return domain.Anomaly{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Anomaly{}, err
	}
	row.Status = string(to)
	return r.toAnomaly(row)
}

// AnomalyActions lists the history of an anomaly, oldest first.
func (r *Repository) AnomalyActions(ctx context.Context, anomalyID string) ([]domain.AnomalyAction, error) {
	rows, err := r.q.ListAnomalyActions(ctx, anomalyID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.AnomalyAction, len(rows))
	for i, row := range rows {
		out[i] = domain.AnomalyAction{
			ID: row.ID, AnomalyID: row.AnomalyID, UserID: row.UserID, UserName: row.UserName,
			Action: domain.Action(row.Action), Note: row.Note.String,
			From: domain.AnomalyStatus(row.FromStatus), To: domain.AnomalyStatus(row.ToStatus),
			CreatedAt: row.CreatedAt.In(r.loc),
		}
	}
	return out, nil
}
