-- name: GetSchedulerPolicy :one
SELECT url, timeout_ms, updated_at
FROM scheduler_policy
WHERE id = true;

-- name: UpsertSchedulerPolicy :exec
INSERT INTO scheduler_policy (id, url, timeout_ms, updated_at)
VALUES (true, $1, $2, now())
ON CONFLICT (id) DO UPDATE
SET url = EXCLUDED.url,
    timeout_ms = EXCLUDED.timeout_ms,
    updated_at = now();
