-- name: ListStageCompletions :many
-- Returns completion records for a stage, optionally filtered by
-- created_at > since. Pass a zero timestamptz to skip the filter.
SELECT completion_id, stage_id, workflow_run, backend, status,
       prompt_tokens, completion_tokens, latency_ms, cost_usd,
       tags, created_at, usage, tool_kind, mapping, cached_tokens
FROM completion_records
WHERE stage_id = $1
  AND created_at > COALESCE(@since::timestamptz, '-infinity'::timestamptz)
ORDER BY created_at DESC
LIMIT @limit_count;

-- name: CostByMapping :many
-- Total cost and dispatch count grouped by mapping variant, optionally
-- filtered by created_at > since. The live critical path aggregates
-- under the empty mapping, each shadow variant under its own name.
SELECT mapping,
       COUNT(*)::bigint                            AS count,
       COALESCE(SUM(cost_usd), 0)::double precision AS total_cost_usd
FROM completion_records
WHERE created_at > COALESCE(@since::timestamptz, '-infinity'::timestamptz)
GROUP BY mapping
ORDER BY mapping;

-- name: StageMetricsByBackend :many
-- Per-backend aggregates for a stage. Empty AVG/PERCENTILE results
-- (e.g., when no rows in the window) are coalesced to 0 so callers
-- don't have to deal with NULLs.
SELECT backend,
       COUNT(*)::bigint                                                                AS count,
       COALESCE(AVG(latency_ms), 0)::double precision                                  AS avg_latency_ms,
       COALESCE(PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY latency_ms), 0)::double precision AS p50_latency_ms,
       COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY latency_ms), 0)::double precision AS p95_latency_ms,
       COALESCE(SUM(cost_usd), 0)::double precision                                    AS total_cost_usd,
       COUNT(*) FILTER (WHERE status = 'error')::bigint                                AS error_count
FROM completion_records
WHERE stage_id = $1
  AND created_at > COALESCE(@since::timestamptz, '-infinity'::timestamptz)
GROUP BY backend
ORDER BY backend;
