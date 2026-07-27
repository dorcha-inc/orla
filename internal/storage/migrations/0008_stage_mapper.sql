-- +goose Up

-- The active dynamic stage mapper. The proxy asks this URL which backend
-- should serve a stage, once per request. Singleton in the same shape
-- as scheduler_policy. An empty url means stages route by their static
-- mapping, the default, so a fresh deployment behaves as if no mapper
-- is set.
CREATE TABLE stage_mapper (
    id          BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    url         TEXT NOT NULL DEFAULT '',
    timeout_ms  INTEGER NOT NULL DEFAULT 50,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO stage_mapper (id) VALUES (true);

-- +goose Down
DROP TABLE stage_mapper;
