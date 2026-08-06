-- +goose Up
-- A provider that caches a prompt prefix bills a repeat read at a
-- discount and reports the hit as prompt_tokens_details.cached_tokens,
-- a subset of prompt_tokens. NULL cache_read_cost_per_mtoken prices a
-- cached token at input_cost_per_mtoken. Only kind=llm may set it.
ALTER TABLE backends ADD COLUMN cache_read_cost_per_mtoken DOUBLE PRECISION;

-- cached_tokens records how much of prompt_tokens the provider served
-- from its cache. NULL follows the same rule as the other token
-- columns, meaning the upstream reported no usage at all.
ALTER TABLE completion_records ADD COLUMN cached_tokens INTEGER;

-- +goose Down
ALTER TABLE backends DROP COLUMN cache_read_cost_per_mtoken;
ALTER TABLE completion_records DROP COLUMN cached_tokens;
