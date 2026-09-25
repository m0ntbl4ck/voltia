-- +goose Up
-- The one-line description the challenge asks for in the "anomaly" field.
ALTER TABLE anomalies ADD COLUMN summary text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE anomalies DROP COLUMN summary;
