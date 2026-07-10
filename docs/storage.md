# Storage

Orla persists everything in a single Postgres database. The schema is the contract between the daemon and the platform engineer's mapper.

## Driver and connection

- `github.com/jackc/pgx/v5` with the `pgx/v5/stdlib` adapter, so `database/sql` works for both application queries and `goose` migrations. Native pgx is available for hot paths if profiling later warrants it.
- Connection URL via `ORLA_DATABASE_URL` env var, or `database_url` in YAML config. Conventional Postgres URL: `postgres://user:pass@host:5432/orla?sslmode=...`.
- `*sql.DB` defaults are sufficient. `MaxOpenConns` is set to `2 × backend.max_concurrency_sum`, an upper bound on simultaneous dispatch goroutines plus headroom for the API and the BatchWriter. `MaxIdleConns` is equal. `ConnMaxLifetime` is 30 minutes so managed Postgres rotations do not surprise us.
- `sslmode=disable` is for local dev only. Managed deployments use `sslmode=require` or `sslmode=verify-full` per the provider's guidance.

## Migrations

`github.com/pressly/goose/v3` over embedded `.sql` files under `internal/storage/migrations/`. Files are named `NNNN_description.sql` with `-- +goose Up` and `-- +goose Down` sections. The Postgres dialect is set via `goose.SetDialect("postgres")`. Migrations run on every `storage.Open` so a fresh database comes up ready to use.

## Write strategy

Two write classes with different durability needs:

| Class | Examples | Write mode |
|---|---|---|
| Control plane | Stage records, backend records | Synchronous |
| Data plane | Completion records, feedback | Async batched via `BatchWriter[T]` |

Control-plane writes return only after the row is durable. The caller needs the confirmation.

Data-plane writes go to a buffered channel and are flushed in batches of about 100 rows, or every 100ms, whichever comes first. Buffer-full drops are counted in a Prometheus metric and never block the producer. Flushes use Postgres `COPY` via `pgx.CopyFrom` for throughput. That is the meaningful win over per-row `INSERT`.

## Schema

### `stages`

The platform engineer's action surface.

```sql
CREATE TABLE stages (
    id                TEXT PRIMARY KEY,
    backend           TEXT NOT NULL DEFAULT '',
    reasoning_effort  TEXT NOT NULL DEFAULT '',
    prompt            TEXT NOT NULL DEFAULT '',
    labels            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Auto-created on first sighting with empty fields. The platform engineer fills in `backend` and optionally `reasoning_effort`, `prompt`, and `labels` via `PUT /api/v1/stages/{id}`.

`prompt` is the stage's system-prompt override. Empty is the "not set" state and the client's own prompt passes through untouched. When non-empty the proxy substitutes it for the leading system message. See [`prompts.md`](prompts.md).

`labels` is intentionally free-form JSONB. The mapper encodes its own state there, such as last action timestamp, exploration flag, or arm-pull counters, without schema migrations. JSONB lets the mapper query directly:

```sql
SELECT id FROM stages WHERE labels @> '{"exploring":true}'
```

### `backends`

```sql
CREATE TABLE backends (
    name                    TEXT PRIMARY KEY,
    endpoint                TEXT NOT NULL,
    model_id                TEXT,
    api_key_env_var         TEXT NOT NULL DEFAULT '',
    max_concurrency         INTEGER NOT NULL DEFAULT 1
                              CHECK (max_concurrency >= 1),
    rate_per_second         DOUBLE PRECISION,
    quality                 DOUBLE PRECISION,
    kind                    TEXT NOT NULL DEFAULT 'llm'
                              CHECK (kind IN ('llm', 'tool')),
    tool_kind               TEXT,
    input_cost_per_mtoken   DOUBLE PRECISION,
    output_cost_per_mtoken  DOUBLE PRECISION,
    rates                   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`kind` discriminates the two backend flavors.

- `'llm'` backends speak OpenAI-compatible chat completions. `model_id` is required. Cost comes from `input_cost_per_mtoken` and `output_cost_per_mtoken`. The orla proxy computes `cost_usd` at write time as `(prompt_tokens × input_cost + completion_tokens × output_cost) / 1_000_000`. The `rates` column is unused for LLM backends and rejected at registration time.

- `'tool'` backends speak a kind-specific JSON RPC over HTTP. `tool_kind` identifies the family (`'structure-prediction'`, `'docking'`, and so on). `model_id` is unused. Cost comes from the `rates` JSONB, a map of `resource_name` to USD-per-unit. The tool wrapper reports a parallel `usage` map on each response, and orla computes `cost_usd` as the dot product of the two maps. A tool can also short-circuit by setting `cost_usd` directly on its response, which is recorded verbatim after a non-negative-finite sanity check.

`quality` is a platform-engineer-supplied prior. Orla does not act on it directly. The mapper does. It is persisted so the mapper can read it as part of its state.

`max_concurrency` is the only operational cap orla enforces directly. `rate_per_second` is enforced per orla process.

### `mapping_variants`

Named, sparse per-stage backend overrides layered on the live stage mapping. One row per (variant, stage). The optimizer creates a variant to shadow-test a candidate, and a request selects it with `X-Orla-Mapping`.

```sql
CREATE TABLE mapping_variants (
    name        TEXT NOT NULL,
    stage_id    TEXT NOT NULL,
    backend     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (name, stage_id)
);
```

A stage absent from a variant resolves to the stage's live backend, so a candidate that differs from live on one stage is a single row. `POST /api/v1/mappings` replaces a variant atomically, `GET` and `DELETE /api/v1/mappings/{name}` read and remove it.

### `completion_records`

The mapper's primary observation channel.

```sql
CREATE TABLE completion_records (
    completion_id     TEXT PRIMARY KEY,
    stage_id          TEXT NOT NULL,
    workflow_run      TEXT,
    backend           TEXT NOT NULL,
    status            TEXT NOT NULL,
    prompt_tokens     INTEGER,
    completion_tokens INTEGER,
    usage             JSONB NOT NULL DEFAULT '{}'::jsonb,
    tool_kind         TEXT,
    latency_ms        INTEGER,
    cost_usd          DOUBLE PRECISION,
    tags              JSONB NOT NULL DEFAULT '{}'::jsonb,
    mapping           TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_completion_stage_time ON completion_records(stage_id, created_at DESC);
CREATE INDEX idx_completion_workflow ON completion_records(workflow_run) WHERE workflow_run IS NOT NULL;
CREATE INDEX idx_completion_mapping_time ON completion_records(mapping, created_at DESC);
```

`status` is either `"success"` or `"error"`. One row per `/v1/chat/completions` or `/v1/tools/{kind}` dispatch, written async via `BatchWriter`. `tags` carries the `X-Orla-Tag-*` map verbatim. `mapping` is the variant that served the request, empty for the live critical path, which lets `GET /api/v1/costs` sum cost per variant even when critical and shadow traffic interleave.

LLM rows populate `prompt_tokens` and `completion_tokens` and leave `usage` as the empty object. Tool rows leave the token columns NULL and populate `usage` with the resources the wrapper reported, with `tool_kind` set to the backend's tool family. To distinguish tool rows in a query, filter on `tool_kind IS NOT NULL` rather than `prompt_tokens IS NULL`. `cost_usd` is the final dollar amount in both cases, computed by the proxy at write time.

A GIN index on `tags` is not added by default. Most mapper queries filter on `stage_id` first, which the b-tree already covers. Add `CREATE INDEX idx_completion_tags ON completion_records USING gin (tags)` if profiling shows tag-filtered queries are hot.

### `feedback`

```sql
CREATE TABLE feedback (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    completion_id TEXT NOT NULL,
    stage_id      TEXT NOT NULL,
    workflow_run  TEXT,
    rating        DOUBLE PRECISION,
    labels        JSONB NOT NULL DEFAULT '[]'::jsonb,
    notes         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_feedback_completion ON feedback(completion_id);
CREATE INDEX idx_feedback_stage_time ON feedback(stage_id, created_at DESC);
```

`rating` is in `[0, 1]` or `NULL`. Developer-submitted, written async. The endpoint returns 202. The mapper joins against `completion_records` by `completion_id` to attribute feedback to backends.

## How the mapper consumes this

Three access patterns the schema is optimized for.

**Recent observations per stage.**

```sql
SELECT * FROM completion_records
WHERE stage_id = $1 AND created_at > $2
ORDER BY created_at DESC LIMIT $3;
```

Backed by `idx_completion_stage_time`.

**Feedback joined to completion.**

```sql
SELECT f.rating, c.backend, c.cost_usd, c.latency_ms
FROM feedback f
JOIN completion_records c USING (completion_id)
WHERE c.stage_id = $1 AND f.created_at > $2;
```

**Aggregates by (stage, backend).**

```sql
SELECT backend,
       COUNT(*),
       AVG(latency_ms),
       SUM(cost_usd),
       PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY latency_ms) AS p50,
       PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY latency_ms) AS p95
FROM completion_records
WHERE stage_id = $1 AND created_at > $2
GROUP BY backend;
```

This is what the `/metrics` endpoint surfaces.

## Read-only access for the mapper

A second Postgres role is provisioned with `SELECT` only on the four tables, intended for mappers that want to query Postgres directly when the REST endpoints are not expressive enough:

```sql
CREATE ROLE orla_reader LOGIN PASSWORD '...';
GRANT CONNECT ON DATABASE orla TO orla_reader;
GRANT USAGE ON SCHEMA public TO orla_reader;
GRANT SELECT ON stages, backends, mapping_variants, completion_records, feedback TO orla_reader;
```

The REST API stays authoritative for common patterns. Direct SQL is the escape hatch for heavy analytical queries that do not deserve a new endpoint.

## Deployment

Local dev needs any Postgres 14+. Install via your system package manager, then `createdb orla`. The default URL for local work is `postgres://$(whoami)@localhost:5432/orla?sslmode=disable`.

Production uses any Postgres 14+ deployment. Tested against managed Postgres providers including RDS, Cloud SQL, and Neon. Orla does not require any extensions beyond the default contrib set.

## Multiple orla instances

The schema supports HA. Multiple orla processes can point at the same database.

- Synchronous control-plane writes use single-row upserts. Concurrent writers are serialized by row locks naturally.
- BatchWriter batches are append-only inserts. Conflicting completion ids, which are vanishingly unlikely with UUIDs, become a single `ON CONFLICT DO NOTHING`.
- Scheduler queues live in-process per instance. A request hits whichever instance the load balancer routes to and is dispatched from that instance's worker pool. Per-backend `max_concurrency` is therefore enforced per instance, not globally. A future change can add a Postgres-advisory-lock-based global cap.

## Retention

There is no automatic pruning. The mapper deletes what it does not need, or runs a periodic vacuum job. Orla does not opinionatedly delete observation data because the mapper may want long history.

If `completion_records` growth becomes a concern, Postgres native partitioning by `created_at` is the planned mitigation. Adding partitioning later is a non-breaking schema change.
