-- name: GetAnomalyForUpdate :one
SELECT * FROM anomalies WHERE id = $1 FOR UPDATE;

-- name: SetAnomalyStatus :exec
UPDATE anomalies SET status = $2 WHERE id = $1;

-- name: InsertAnomalyAction :one
INSERT INTO anomaly_actions (anomaly_id, user_id, action, note, from_status, to_status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAnomalyActions :many
SELECT a.id, a.anomaly_id, a.user_id, u.name AS user_name, a.action, a.note,
       a.from_status, a.to_status, a.created_at
FROM anomaly_actions a
JOIN users u ON u.id = a.user_id
WHERE a.anomaly_id = $1
ORDER BY a.created_at, a.id;
