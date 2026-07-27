-- name: GetStageMapper :one
SELECT url, timeout_ms, updated_at
FROM stage_mapper
WHERE id = true;

-- name: UpsertStageMapper :exec
INSERT INTO stage_mapper (id, url, timeout_ms, updated_at)
VALUES (true, $1, $2, now())
ON CONFLICT (id) DO UPDATE
SET url = EXCLUDED.url,
    timeout_ms = EXCLUDED.timeout_ms,
    updated_at = now();
