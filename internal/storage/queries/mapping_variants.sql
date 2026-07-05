-- name: GetMappingOverride :one
-- The backend override for one (variant, stage). No row means the
-- request falls through to the stage's live backend.
SELECT backend
FROM mapping_variants
WHERE name = $1 AND stage_id = $2;

-- name: ListVariantOverrides :many
-- All overrides for one variant, ordered by stage id.
SELECT name, stage_id, backend, created_at, updated_at
FROM mapping_variants
WHERE name = $1
ORDER BY stage_id;

-- name: ListAllVariantOverrides :many
-- Every override across all variants, ordered so a caller can group
-- consecutive rows by name.
SELECT name, stage_id, backend, created_at, updated_at
FROM mapping_variants
ORDER BY name, stage_id;

-- name: UpsertMappingOverride :exec
INSERT INTO mapping_variants (name, stage_id, backend)
VALUES ($1, $2, $3)
ON CONFLICT (name, stage_id) DO UPDATE
SET backend = EXCLUDED.backend,
    updated_at = now();

-- name: DeleteVariant :execrows
DELETE FROM mapping_variants WHERE name = $1;
