-- name: ListEvents :many
SELECT *
FROM events
ORDER BY ts, meter_id;

-- name: ListMeterEvents :many
SELECT *
FROM events
WHERE meter_id = $1
ORDER BY ts;
