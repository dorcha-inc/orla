-- +goose Up
-- cost_source is an optional URL the daemon polls for the backend's
-- current per-million-token costs. NULL means the static
-- input_cost_per_mtoken and output_cost_per_mtoken columns price the
-- backend. Only kind=llm backends may set it.
ALTER TABLE backends ADD COLUMN cost_source TEXT;

-- +goose Down
ALTER TABLE backends DROP COLUMN cost_source;
