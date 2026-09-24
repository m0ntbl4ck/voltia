-- +goose Up
CREATE TABLE explanation_cache (
    evidence_hash text        PRIMARY KEY,
    provider      text        NOT NULL,
    model         text        NOT NULL,
    payload       jsonb       NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE explanation_cache;
