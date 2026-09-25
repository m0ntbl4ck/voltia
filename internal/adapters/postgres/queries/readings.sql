-- name: ListReadings :many
SELECT *
FROM readings
ORDER BY meter_id, ts;

-- The range is [from_ts, to_ts): the upper bound is exclusive.
-- name: ListMeterReadings :many
SELECT *
FROM readings
WHERE meter_id = @meter_id
  AND ts >= @from_ts
  AND ts < @to_ts
ORDER BY ts;
