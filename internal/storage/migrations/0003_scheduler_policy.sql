-- +goose Up

-- The active scheduling policy. This is a singleton: a boolean primary
-- key fixed to true with a CHECK allows exactly one row, so upserts
-- always target it. An empty url means first-come-first-served, the
-- default, so a fresh deployment behaves as if no policy is set.
CREATE TABLE scheduler_policy (
    id          BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    url         TEXT NOT NULL DEFAULT '',
    timeout_ms  INTEGER NOT NULL DEFAULT 50,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO scheduler_policy (id) VALUES (true);

-- +goose Down
DROP TABLE scheduler_policy;
