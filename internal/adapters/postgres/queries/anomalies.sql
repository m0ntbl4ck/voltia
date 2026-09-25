-- A re-run finds the same anomaly by fingerprint. It refreshes what the engine
-- measured and wrote, and leaves id, status and detected_at as they were.
-- name: UpsertAnomaly :exec
INSERT INTO anomalies (
    meter_id, fingerprint, type, severity, confidence, confidence_breakdown,
    priority, priority_breakdown, episode_start, episode_end, ongoing, evidence,
    summary, reason, recommended_action, investigation_steps,
    explanation_source, explanation_model, last_analysis_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
)
ON CONFLICT (fingerprint) DO UPDATE SET
    severity             = EXCLUDED.severity,
    confidence           = EXCLUDED.confidence,
    confidence_breakdown = EXCLUDED.confidence_breakdown,
    priority             = EXCLUDED.priority,
    priority_breakdown   = EXCLUDED.priority_breakdown,
    episode_end          = EXCLUDED.episode_end,
    ongoing              = EXCLUDED.ongoing,
    evidence             = EXCLUDED.evidence,
    summary              = EXCLUDED.summary,
    reason               = EXCLUDED.reason,
    recommended_action   = EXCLUDED.recommended_action,
    investigation_steps  = EXCLUDED.investigation_steps,
    explanation_source   = EXCLUDED.explanation_source,
    explanation_model    = EXCLUDED.explanation_model,
    last_analysis_id     = EXCLUDED.last_analysis_id;

-- name: GetAnomaly :one
SELECT * FROM anomalies WHERE id = $1;

-- Every filter is optional: a NULL one matches everything.
-- name: ListAnomalies :many
SELECT * FROM anomalies
WHERE (sqlc.narg('type')::text     IS NULL OR type     = sqlc.narg('type'))
  AND (sqlc.narg('severity')::text IS NULL OR severity = sqlc.narg('severity'))
  AND (sqlc.narg('status')::text   IS NULL OR status   = sqlc.narg('status'))
  AND (sqlc.narg('meter_id')::text IS NULL OR meter_id = sqlc.narg('meter_id'))
ORDER BY priority DESC, confidence DESC, meter_id, episode_start;
