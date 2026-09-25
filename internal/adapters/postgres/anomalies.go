package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/m0ntbl4ck/voltia/internal/adapters/postgres/sqlcgen"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// UpsertAnomalies stores the anomalies of a run in one transaction. One that
// already exists (same fingerprint) is refreshed and keeps its id, status and
// detection time.
func (r *Repository) UpsertAnomalies(ctx context.Context, anomalies []domain.Anomaly) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	q := r.q.WithTx(tx)
	for _, a := range anomalies {
		params, err := upsertParams(a)
		if err != nil {
			return fmt.Errorf("anomaly %s: %w", a.Fingerprint, err)
		}
		if err := q.UpsertAnomaly(ctx, params); err != nil {
			return fmt.Errorf("anomaly %s: %w", a.Fingerprint, err)
		}
	}
	return tx.Commit()
}

// Anomaly returns domain.ErrNotFound when no anomaly has that id.
func (r *Repository) Anomaly(ctx context.Context, id string) (domain.Anomaly, error) {
	row, err := r.q.GetAnomaly(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Anomaly{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Anomaly{}, err
	}
	return r.toAnomaly(row)
}

// Anomalies lists the ones matching the filter, most urgent first.
func (r *Repository) Anomalies(ctx context.Context, f domain.AnomalyFilter) ([]domain.Anomaly, error) {
	rows, err := r.q.ListAnomalies(ctx, sqlcgen.ListAnomaliesParams{
		Type:     nullable(string(f.Type)),
		Severity: nullable(string(f.Severity)),
		Status:   nullable(string(f.Status)),
		MeterID:  nullable(f.MeterID),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Anomaly, len(rows))
	for i, row := range rows {
		if out[i], err = r.toAnomaly(row); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func nullable(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func upsertParams(a domain.Anomaly) (sqlcgen.UpsertAnomalyParams, error) {
	evidence, err := json.Marshal(a.Evidence)
	if err != nil {
		return sqlcgen.UpsertAnomalyParams{}, err
	}
	steps := a.Explanation.InvestigationSteps
	if steps == nil {
		steps = []string{}
	}
	rawSteps, err := json.Marshal(steps)
	if err != nil {
		return sqlcgen.UpsertAnomalyParams{}, err
	}
	return sqlcgen.UpsertAnomalyParams{
		MeterID:             a.MeterID,
		Fingerprint:         a.Fingerprint,
		Type:                string(a.Type),
		Severity:            string(a.Severity),
		Confidence:          a.Confidence,
		ConfidenceBreakdown: a.ConfidenceBreakdown,
		Priority:            int32(a.Priority),
		PriorityBreakdown:   a.PriorityBreakdown,
		EpisodeStart:        a.EpisodeStart,
		EpisodeEnd:          a.EpisodeEnd,
		Ongoing:             a.Ongoing,
		Evidence:            evidence,
		Summary:             a.Explanation.Summary,
		Reason:              a.Explanation.Reason,
		RecommendedAction:   a.Explanation.RecommendedAction,
		InvestigationSteps:  rawSteps,
		ExplanationSource:   string(a.Explanation.Source),
		ExplanationModel:    nullable(a.Explanation.Model),
		LastAnalysisID:      a.LastAnalysisID,
	}, nil
}

func (r *Repository) toAnomaly(row sqlcgen.Anomaly) (domain.Anomaly, error) {
	a := domain.Anomaly{
		ID:                  row.ID,
		MeterID:             row.MeterID,
		Fingerprint:         row.Fingerprint,
		Type:                domain.AnomalyType(row.Type),
		Severity:            domain.Severity(row.Severity),
		Confidence:          row.Confidence,
		ConfidenceBreakdown: row.ConfidenceBreakdown,
		Priority:            int(row.Priority),
		PriorityBreakdown:   row.PriorityBreakdown,
		EpisodeStart:        row.EpisodeStart.In(r.loc),
		EpisodeEnd:          row.EpisodeEnd.In(r.loc),
		Ongoing:             row.Ongoing,
		Status:              domain.AnomalyStatus(row.Status),
		DetectedAt:          row.DetectedAt.In(r.loc),
		LastAnalysisID:      row.LastAnalysisID,
		Explanation: domain.Explanation{
			Summary:           row.Summary,
			Reason:            row.Reason,
			RecommendedAction: row.RecommendedAction,
			Source:            domain.ExplanationSource(row.ExplanationSource),
			Model:             row.ExplanationModel.String,
		},
	}
	if err := json.Unmarshal(row.Evidence, &a.Evidence); err != nil {
		return domain.Anomaly{}, fmt.Errorf("anomaly %s evidence: %w", row.ID, err)
	}
	if err := json.Unmarshal(row.InvestigationSteps, &a.Explanation.InvestigationSteps); err != nil {
		return domain.Anomaly{}, fmt.Errorf("anomaly %s steps: %w", row.ID, err)
	}
	return a, nil
}
