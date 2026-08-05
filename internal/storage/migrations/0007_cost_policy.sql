-- +goose Up

-- How often the daemon refreshes prices from backend cost sources.
-- Singleton in the same shape as scheduler_policy: a boolean primary
-- key fixed to true with a CHECK allows exactly one row. The default
-- matches the poller's own default so a fresh deployment and an
-- unconfigured store agree.
CREATE TABLE cost_policy (
    id                   BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    refresh_interval_ms  INTEGER NOT NULL DEFAULT 60000
                           CHECK (refresh_interval_ms > 0),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO cost_policy (id) VALUES (true);

-- +goose Down
DROP TABLE cost_policy;
