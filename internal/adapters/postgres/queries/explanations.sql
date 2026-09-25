-- name: GetCachedExplanation :one
SELECT payload, model FROM explanation_cache WHERE evidence_hash = $1;

-- name: PutCachedExplanation :exec
INSERT INTO explanation_cache (evidence_hash, provider, model, payload)
VALUES ($1, $2, $3, $4)
ON CONFLICT (evidence_hash) DO NOTHING;
