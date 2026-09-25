package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/m0ntbl4ck/voltia/internal/adapters/postgres/sqlcgen"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// cachedExplanation is the JSON kept in explanation_cache.payload.
type cachedExplanation struct {
	Summary            string   `json:"summary"`
	Reason             string   `json:"reason"`
	RecommendedAction  string   `json:"recommended_action"`
	InvestigationSteps []string `json:"investigation_steps"`
}

// CachedExplanation returns the text stored under key. Only language model
// text is cached, so what comes back is always marked as LLM.
func (r *Repository) CachedExplanation(ctx context.Context, key string) (domain.Explanation, bool, error) {
	row, err := r.q.GetCachedExplanation(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Explanation{}, false, nil
	}
	if err != nil {
		return domain.Explanation{}, false, err
	}
	var c cachedExplanation
	if err := json.Unmarshal(row.Payload, &c); err != nil {
		return domain.Explanation{}, false, err
	}
	return domain.Explanation{
		Summary: c.Summary, Reason: c.Reason, RecommendedAction: c.RecommendedAction,
		InvestigationSteps: c.InvestigationSteps, Source: domain.SourceLLM, Model: row.Model,
	}, true, nil
}

// CacheExplanation stores exp under key and keeps the first text stored there.
func (r *Repository) CacheExplanation(ctx context.Context, key, provider string, exp domain.Explanation) error {
	payload, err := json.Marshal(cachedExplanation{
		Summary: exp.Summary, Reason: exp.Reason, RecommendedAction: exp.RecommendedAction,
		InvestigationSteps: exp.InvestigationSteps,
	})
	if err != nil {
		return err
	}
	return r.q.PutCachedExplanation(ctx, sqlcgen.PutCachedExplanationParams{
		EvidenceHash: key, Provider: provider, Model: exp.Model, Payload: payload,
	})
}
