-- +goose Up

-- Mapping variants. A variant is a named, sparse set of per-stage
-- backend overrides layered on the live stage mapping. The optimizer
-- creates a variant, then a shadow request selects it with the
-- X-Orla-Mapping header. A stage absent from a variant falls through
-- to the stage's live backend, so a candidate that differs from live
-- on one stage is a single-row variant.
CREATE TABLE mapping_variants (
    name        TEXT NOT NULL,
    stage_id    TEXT NOT NULL,
    backend     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (name, stage_id)
);

-- Which variant served a dispatched request. The live critical path
-- leaves this empty, a shadow request records the variant name so cost
-- separates by mapping when critical and shadow stream concurrently.
ALTER TABLE completion_records
    ADD COLUMN mapping TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_completion_mapping_time
    ON completion_records(mapping, created_at DESC);

-- +goose Down
DROP INDEX idx_completion_mapping_time;
ALTER TABLE completion_records DROP COLUMN mapping;
DROP TABLE mapping_variants;
