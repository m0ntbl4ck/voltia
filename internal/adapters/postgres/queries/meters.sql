-- name: ListMeters :many
SELECT *
FROM meters
ORDER BY meter_id;

-- name: GetMeter :one
SELECT *
FROM meters
WHERE meter_id = $1;
