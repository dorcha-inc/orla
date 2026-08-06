-- The SELECT column order matches the canonical backends table order.
-- Keeping that order lets sqlc reuse the db.Backend row type instead of
-- emitting a per-query row type.

-- name: GetBackend :one
SELECT name, endpoint, model_id, api_key_env_var, max_concurrency,
       rate_per_second, quality, kind, tool_kind,
       input_cost_per_mtoken, output_cost_per_mtoken, rates,
       created_at, updated_at, cost_source, cache_read_cost_per_mtoken
FROM backends
WHERE name = $1;

-- name: ListBackends :many
SELECT name, endpoint, model_id, api_key_env_var, max_concurrency,
       rate_per_second, quality, kind, tool_kind,
       input_cost_per_mtoken, output_cost_per_mtoken, rates,
       created_at, updated_at, cost_source, cache_read_cost_per_mtoken
FROM backends
ORDER BY name;

-- name: InsertBackend :one
INSERT INTO backends (
    name, endpoint, model_id, api_key_env_var, max_concurrency,
    rate_per_second, quality, kind, tool_kind,
    input_cost_per_mtoken, output_cost_per_mtoken, rates, cost_source,
    cache_read_cost_per_mtoken
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING name, endpoint, model_id, api_key_env_var, max_concurrency,
          rate_per_second, quality, kind, tool_kind,
          input_cost_per_mtoken, output_cost_per_mtoken, rates,
          created_at, updated_at, cost_source, cache_read_cost_per_mtoken;

-- name: UpdateBackend :one
UPDATE backends
SET endpoint = $2,
    model_id = $3,
    api_key_env_var = $4,
    max_concurrency = $5,
    rate_per_second = $6,
    quality = $7,
    kind = $8,
    tool_kind = $9,
    input_cost_per_mtoken = $10,
    output_cost_per_mtoken = $11,
    rates = $12,
    cost_source = $13,
    cache_read_cost_per_mtoken = $14,
    updated_at = now()
WHERE name = $1
RETURNING name, endpoint, model_id, api_key_env_var, max_concurrency,
          rate_per_second, quality, kind, tool_kind,
          input_cost_per_mtoken, output_cost_per_mtoken, rates,
          created_at, updated_at, cost_source, cache_read_cost_per_mtoken;

-- name: DeleteBackend :execrows
DELETE FROM backends WHERE name = $1;
