-- +goose Up
CREATE TABLE analysis_runs (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    status        text        NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'RUNNING', 'COMPLETED', 'FAILED')),
    current_stage text,
    stages        jsonb       NOT NULL DEFAULT '[]',
    summary       jsonb,
    params        jsonb       NOT NULL,
    started_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz
);

-- The fingerprint (meter, type, episode start) lets a re-run update an anomaly
-- without losing its status or history.
CREATE TABLE anomalies (
    id                    uuid             PRIMARY KEY DEFAULT gen_random_uuid(),
    meter_id              text             NOT NULL REFERENCES meters (meter_id),
    fingerprint           text             NOT NULL UNIQUE,
    type                  text             NOT NULL
        CHECK (type IN ('REAL_ANOMALY', 'EXPLAINABLE_ANOMALY', 'FALSE_POSITIVE', 'DATA_QUALITY')),
    severity              text             NOT NULL
        CHECK (severity IN ('HIGH', 'MEDIUM', 'LOW')),
    confidence            double precision NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    confidence_breakdown  jsonb            NOT NULL,
    priority              integer          NOT NULL CHECK (priority BETWEEN 0 AND 100),
    priority_breakdown    jsonb            NOT NULL,
    episode_start         timestamptz      NOT NULL,
    episode_end           timestamptz      NOT NULL,
    ongoing               boolean          NOT NULL DEFAULT false,
    evidence              jsonb            NOT NULL,
    reason                text             NOT NULL,
    recommended_action    text             NOT NULL,
    investigation_steps   jsonb            NOT NULL DEFAULT '[]',
    explanation_source    text             NOT NULL DEFAULT 'TEMPLATE',
    explanation_model     text,
    status                text             NOT NULL DEFAULT 'OPEN'
        CHECK (status IN ('OPEN', 'ACKNOWLEDGED', 'RESOLVED', 'DISMISSED')),
    detected_at           timestamptz      NOT NULL DEFAULT now(),
    last_analysis_id      uuid             NOT NULL REFERENCES analysis_runs (id)
);

CREATE INDEX anomalies_meter_id_idx ON anomalies (meter_id);
CREATE INDEX anomalies_status_priority_idx ON anomalies (status, priority DESC);

CREATE TABLE anomaly_actions (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    anomaly_id  uuid        NOT NULL REFERENCES anomalies (id) ON DELETE CASCADE,
    user_id     uuid        NOT NULL REFERENCES users (id),
    action      text        NOT NULL
        CHECK (action IN ('CREATE_INSPECTION_ORDER', 'REQUEST_METER_VALIDATION', 'CONFIRM_OPERATION', 'DISMISS', 'RESOLVE')),
    note        text,
    from_status text        NOT NULL,
    to_status   text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX anomaly_actions_anomaly_id_idx ON anomaly_actions (anomaly_id, created_at);

-- +goose Down
DROP TABLE anomaly_actions;
DROP TABLE anomalies;
DROP TABLE analysis_runs;
