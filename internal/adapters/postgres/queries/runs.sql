-- name: CreateRun :one
INSERT INTO analysis_runs (params, stages)
VALUES ($1, $2)
RETURNING *;

-- name: UpdateRunProgress :exec
UPDATE analysis_runs
SET status = $2, current_stage = $3, stages = $4
WHERE id = $1;

-- name: FinishRun :exec
UPDATE analysis_runs
SET status = $2, current_stage = NULL, stages = $3, summary = $4, finished_at = now()
WHERE id = $1;

-- name: GetRun :one
SELECT * FROM analysis_runs WHERE id = $1;

-- name: GetLatestRun :one
SELECT * FROM analysis_runs ORDER BY started_at DESC LIMIT 1;

-- name: GetActiveRun :one
SELECT * FROM analysis_runs
WHERE status IN ('PENDING', 'RUNNING')
ORDER BY started_at DESC
LIMIT 1;
