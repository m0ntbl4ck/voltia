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

// CreateRun stores a new run as PENDING with the given thresholds and stages.
func (r *Repository) CreateRun(ctx context.Context, params json.RawMessage, stages []domain.StageState) (domain.AnalysisRun, error) {
	raw, err := json.Marshal(stages)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	row, err := r.q.CreateRun(ctx, sqlcgen.CreateRunParams{Params: params, Stages: raw})
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	return r.toRun(row)
}

// SaveProgress records the status, the stage in progress and every stage's state.
func (r *Repository) SaveProgress(ctx context.Context, id string, status domain.RunStatus, current string, stages []domain.StageState) error {
	raw, err := json.Marshal(stages)
	if err != nil {
		return err
	}
	return r.q.UpdateRunProgress(ctx, sqlcgen.UpdateRunProgressParams{
		ID:           id,
		Status:       string(status),
		CurrentStage: sql.NullString{String: current, Valid: current != ""},
		Stages:       raw,
	})
}

// FinishRun closes a run as COMPLETED or FAILED with its summary.
func (r *Repository) FinishRun(ctx context.Context, id string, status domain.RunStatus, stages []domain.StageState, summary domain.RunSummary) error {
	rawStages, err := json.Marshal(stages)
	if err != nil {
		return err
	}
	rawSummary, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	msg := json.RawMessage(rawSummary)
	return r.q.FinishRun(ctx, sqlcgen.FinishRunParams{
		ID: id, Status: string(status), Stages: rawStages, Summary: &msg,
	})
}

// Run returns domain.ErrNotFound when no run has that id.
func (r *Repository) Run(ctx context.Context, id string) (domain.AnalysisRun, error) {
	return r.oneRun(r.q.GetRun(ctx, id))
}

// LatestRun returns the run that started last, or domain.ErrNotFound when none has.
func (r *Repository) LatestRun(ctx context.Context) (domain.AnalysisRun, error) {
	return r.oneRun(r.q.GetLatestRun(ctx))
}

// ActiveRun returns the run still going, or domain.ErrNotFound when none is.
func (r *Repository) ActiveRun(ctx context.Context) (domain.AnalysisRun, error) {
	return r.oneRun(r.q.GetActiveRun(ctx))
}

func (r *Repository) oneRun(row sqlcgen.AnalysisRun, err error) (domain.AnalysisRun, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AnalysisRun{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	return r.toRun(row)
}

func (r *Repository) toRun(row sqlcgen.AnalysisRun) (domain.AnalysisRun, error) {
	run := domain.AnalysisRun{
		ID:           row.ID,
		Status:       domain.RunStatus(row.Status),
		CurrentStage: row.CurrentStage.String,
		Params:       row.Params,
		StartedAt:    row.StartedAt.In(r.loc),
	}
	if err := json.Unmarshal(row.Stages, &run.Stages); err != nil {
		return domain.AnalysisRun{}, fmt.Errorf("run %s stages: %w", row.ID, err)
	}
	if row.Summary != nil {
		var s domain.RunSummary
		if err := json.Unmarshal(*row.Summary, &s); err != nil {
			return domain.AnalysisRun{}, fmt.Errorf("run %s summary: %w", row.ID, err)
		}
		run.Summary = &s
	}
	if row.FinishedAt.Valid {
		t := row.FinishedAt.Time.In(r.loc)
		run.FinishedAt = &t
	}
	return run, nil
}
