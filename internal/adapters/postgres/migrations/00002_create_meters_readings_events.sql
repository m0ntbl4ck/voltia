-- +goose Up
CREATE TABLE meters (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    meter_id   text        NOT NULL UNIQUE,
    name       text        NOT NULL,
    location   text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- UNIQUE (meter_id, ts) makes the seed idempotent and doubles as the lookup index.
CREATE TABLE readings (
    id              bigint           GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    meter_id        text             NOT NULL REFERENCES meters (meter_id),
    ts              timestamptz      NOT NULL,
    consumption_kwh double precision NOT NULL,
    voltage_v       double precision NOT NULL,
    current_a       double precision NOT NULL,
    power_factor    double precision NOT NULL,
    status          text             NOT NULL,
    UNIQUE (meter_id, ts)
);

CREATE TABLE events (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    meter_id    text        NOT NULL REFERENCES meters (meter_id),
    ts          timestamptz NOT NULL,
    type        text        NOT NULL
        CHECK (type IN ('OPERATIONAL_CHANGE', 'SCHEDULED_OUTAGE', 'DATA_QUALITY', 'UNKNOWN')),
    description text        NOT NULL,
    UNIQUE (meter_id, ts, type)
);

-- +goose Down
DROP TABLE events;
DROP TABLE readings;
DROP TABLE meters;
