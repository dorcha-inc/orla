-- name: GetCostPolicy :one
SELECT refresh_interval_ms, updated_at
FROM cost_policy
WHERE id = true;

-- name: UpsertCostPolicy :exec
INSERT INTO cost_policy (id, refresh_interval_ms, updated_at)
VALUES (true, $1, now())
ON CONFLICT (id) DO UPDATE
SET refresh_interval_ms = EXCLUDED.refresh_interval_ms,
    updated_at = now();
