# Orla Refactor Plan — Two-Persona Stage-First Design

Status: **in progress**. Last updated 2026-05-13.

**Completed so far:** A (revert), A.5a (mcp.Tool removal), A.5b (openai-go SDK), B.0 (storage + batchWriter), B (stage registry + REST), B.5 (backend persistence + cleanups: stages cache removed, accuracy machinery dropped, `type` collapsed into `model_id`, dead viper defaults removed), B.7 (library swaps: google/uuid, slog over zap, goose for migrations, cenkalti/backoff for retries), C (proxy consults stage registry + `X-Orla-Backend` response header). Phase 0 coverage tooling also landed.

## Goal

Reshape Orla around two clearly separated personas:

1. **Agent logic writer (developer)** — only job is to mark each LLM call with a `stage_id`, optionally declare stage dependencies (DAG), optionally hint at reasoning effort, and optionally submit per-call feedback ("this output was good / bad"). Developers do not pick backends, scheduling policies, accuracy targets, or cache behavior.
2. **Platform engineer (or an external autonomous mapper)** — owns the mapping of stages to backends and all inference policy (scheduling, priority, cache flush, reasoning effort actually sent to the model, concurrency caps). They run their own optimizer (RL or otherwise) outside Orla and use Orla's REST API to read characteristics + feedback and update mappings.

Orla is a fast, opinionated dispatcher with a stage registry, a feedback log, and policy enforcement. It is *not* the optimizer.

## Architectural decisions

### Language: Go (no rewrite)
The new design preserves ~60–70% of existing Go code. `internal/model/`, `internal/serving/access/`, `internal/serving/memory/`, `internal/serving/backend_manager.go`, `internal/core/workflow_manager.go`, `internal/config/`, metrics/retry/rate limiting are all kept verbatim. A rewrite would discard ~5,000 lines of tested code (especially provider implementations and the scheduler) for no concrete win. The platform engineer's RL system lives outside Orla and can be any language.

### Storage: SQLite via `modernc.org/sqlite`
Pure-Go SQLite translation. Statically links, no CGo, easy cross-compilation. Mature (Datadog and others in production). SQL is the right shape for the queries the platform engineer's mapper needs (feedback aggregations, time-range scans, joins). Inspectable via `sqlite3` CLI. Switch to Postgres only if multi-process state sharing becomes a need.

### Write strategy: control plane sync, data plane async batched
Not "platform-engineer-submitted vs developer-submitted." The real split:

| Class | Examples | Write mode |
|---|---|---|
| Control plane | Stage registry CRUD, policies, workflows | Synchronous |
| Data plane | Completion records, feedback | Async batched |

Generic `batchWriter[T]` pattern, two instances (one for completion records, one for feedback). Returns `202 Accepted` for feedback. SQLite WAL + batched transactions handles 100K+ inserts/sec, vastly over-provisioned for any realistic workload.

### `completion_records` table
Orla emits one row per `/v1/chat/completions` call: `completion_id`, `stage_id`, `backend`, `workflow_run`, `prompt_tokens`, `completion_tokens`, `cost_usd`, `latency_ms`, `created_at`. Feedback joins it by `completion_id`. Without this, feedback is unattributable to a backend, and the mapper has no clean relational substrate (Prometheus is wrong shape).

### Stage as first-class entity
Auto-created on first sighting of a stage_id (defaults). Platform engineer overwrites via API. The developer never sees stage configuration.

### Persona-respecting hint vs policy split
`X-Orla-Reasoning-Hint` (developer-supplied) writes to a separate field on the stage record than `ReasoningEffort` (platform engineer's actual policy). The hint is an *input signal* the external mapper reads; it does not affect inference directly. This split is what makes the personas contract enforceable.

### OpenAI-compatible proxy is the only inference entry point
`POST /v1/chat/completions` is the canonical entry point. After Phase H, `/api/v1/execute` and `pkg/api/` are deleted entirely. `model` field in the proxy is a fallback default; if the stage is mapped, it's ignored entirely.

## Phases

### Phase A — Revert wrong inference knobs *(~1h)* — NEXT
Strip the per-call knobs that were added under the previous (incorrect) design and belong on the stage record instead:
- Remove from `metadata.orla` body: `accuracy`, `accuracy_policy`, `scheduling_policy`, `request_scheduling_policy`, `scheduling_hints`, `cache_policy`, `reasoning_effort`, `chat_template_kwargs`
- Remove `applyOrlaInferenceKnobs`, `validateInferencePolicies`, accuracy routing branch in `handleChatCompletions`
- Drop the 6 tests that exercise these
- **Keep**: workflow label propagation, `X-Orla-Workflow-Run`, `X-Orla-Data-Labels`, `X-Orla-Tag-*`, the basic proxy plumbing

### Phase B.0 — DB foundation *(~2h)*
- Add `modernc.org/sqlite` dependency
- New `internal/storage/` package: connection lifecycle (open, migrate, close); WAL pragmas (`journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`, `busy_timeout=5000`); embedded `.sql` migration runner; `Tx(fn func(*sql.Tx) error) error` helper
- **Generic `batchWriter[T]`** for async batched writes (used by Phases B/F)
- Config: `storage_path` (default `~/.orla/orla.db` or `$XDG_DATA_HOME/orla/orla.db`; `:memory:` for tests); writer batch sizes / buffer / period knobs
- Tests: open + migrate, WAL confirmed, basic CRUD, batchWriter happy path + buffer-full drop

### Phase B — Stage registry *(~4h)*
- New `internal/stages/` package
- `Stage` type: `ID`, `Backend`, `SchedulingPolicy`, `RequestSchedulingPolicy`, `Priority`, `RequestPriority`, `MaxConcurrency`, `ReasoningEffort`, `FlushOnComplete`, `ReasoningHint`, `Labels`, `CreatedAt`, `UpdatedAt`
- `Registry` backed by SQLite with an in-memory read cache (`RWMutex` + `map`). Cache invalidates on write. Auto-create on first sighting via `GetOrCreate`.
- **Control-plane writes are synchronous.**
- REST: `PUT /api/v1/stages/{id}`, `PATCH /api/v1/stages/{id}`, `GET /api/v1/stages/{id}`, `GET /api/v1/stages`, `DELETE /api/v1/stages/{id}`
- Schema:
  ```sql
  CREATE TABLE stages (
    id                          TEXT PRIMARY KEY,
    backend                     TEXT,
    scheduling_policy           TEXT,
    request_scheduling_policy   TEXT,
    priority                    INTEGER,
    request_priority            INTEGER,
    max_concurrency             INTEGER,
    reasoning_effort            TEXT,
    flush_on_complete           INTEGER NOT NULL DEFAULT 0,
    reasoning_hint              TEXT,
    labels_json                 TEXT,
    created_at                  INTEGER NOT NULL,
    updated_at                  INTEGER NOT NULL
  );
  ```
- Tests: CRUD happy paths, default-on-create, concurrent reads, cache invalidation on write
- Update OpenAPI spec + AGENTS.md

### Phase C — Proxy consults the stage registry *(~3h)*
- In `handleChatCompletions`: after extracting stage, `registry.GetOrCreate(stage)`. Resolve backend from the stage record. Fallback to request's `model` field if the stage has no backend mapped; 400 if neither.
- Apply stage-configured `ReasoningEffort` to `InferenceOptions`.
- If the stage is mapped and `model` differs, log mismatch once (drift detection).
- Tests: mapped stage ignores `model`, unmapped stage falls back, mismatch logged.

### Phase D — Stage-scoped scheduling + per-stage concurrency *(~4-5h)*
- Populate `SchedulingPolicy`, `RequestSchedulingPolicy`, scheduling-hints priorities in `InferenceOptions` from the stage record (not the request)
- **Per-stage `MaxConcurrency` enforcement** in the backend manager — when two stages share a backend, each stage's queue gets its own semaphore. Today only the backend has a concurrency cap; this prevents stage A from starving stage B.
- Tests including the starvation scenario.
- New metrics: `orla_stage_queue_depth`, `orla_stage_in_flight`.

### Phase E — Cache flush on stage completion *(~2-3h)*
- New `TransitionStageComplete` signal in the memory manager.
- Triggers:
  1. On `POST /api/v1/workflow/complete`, walk the run's stages and fire `TransitionStageComplete` for any stage with `FlushOnComplete=true`
  2. Optional per-stage idle timeout (background goroutine in stage registry tracks last-activity-timestamp; configurable, default off)
- No explicit "stage done" dev API — keeps developer surface narrow.
- Tests: flush fires on workflow complete; idle timeout fires; non-flushing stages unaffected.

### Phase F — Completion records + feedback + metrics *(~4h)*
- Two SQLite tables: `completion_records` (one row per inference call) + `feedback` (developer-submitted, joins by `completion_id`)
- **Both writes go through async `batchWriter` instances.** Feedback endpoint returns `202 Accepted`.
- Schema:
  ```sql
  CREATE TABLE completion_records (
    completion_id     TEXT PRIMARY KEY,
    stage_id          TEXT NOT NULL,
    backend           TEXT NOT NULL,
    workflow_run      TEXT,
    prompt_tokens     INTEGER,
    completion_tokens INTEGER,
    cost_usd          REAL,
    latency_ms        INTEGER,
    created_at        INTEGER NOT NULL
  );
  CREATE INDEX idx_completion_stage_time ON completion_records(stage_id, created_at DESC);

  CREATE TABLE feedback (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    stage_id      TEXT NOT NULL,
    completion_id TEXT NOT NULL,
    workflow_run  TEXT,
    rating        REAL,
    labels_json   TEXT,
    notes         TEXT,
    created_at    INTEGER NOT NULL
  );
  CREATE INDEX idx_feedback_stage_time ON feedback(stage_id, created_at DESC);
  CREATE INDEX idx_feedback_completion ON feedback(completion_id);
  ```
- REST:
  - `POST /api/v1/stages/{id}/feedback` (202)
  - `GET /api/v1/stages/{id}/feedback?limit=&since=`
  - `GET /api/v1/stages/{id}/metrics` — aggregates (count, avg rating, label freq, p50/p95 latency, total cost, etc.)
- Retention: configurable per-stage `feedback_retention_days`, background vacuum. Defaults: 30 days feedback, no completion-record cap by default.
- Tests: submission, retrieval, aggregation, eviction.

### Phase G — Developer-side reasoning hint *(~1h)*
- New header `X-Orla-Reasoning-Hint: high|medium|low`
- On every proxy call with header present, update stage record's `ReasoningHint` field (last-write-wins).
- **Does not affect inference directly.** Only `ReasoningEffort` (platform-engineer-set) does. The split is what enables persona separation.
- Test: hint header populates the right field; doesn't leak into `InferenceOptions`.

### Phase H — Delete old paths *(~3-4h)*
- Delete `handleExecute`, `handleExecuteStream`, `ExecuteRequest`, `ExecuteResponse`, route registration, related tests
- Delete `pkg/api/` entirely
- Delete `examples/` Go directory entirely (per user direction — pyorla covers examples now)
- **Delete `orla agent` (the one-shot CLI):**
  - `cmd/orla/agent_cmd.go`
  - `internal/agent/` (agent loop + one-shot executor)
  - `internal/tui/` (Bubbletea terminal UI)
  - Agent-only config fields: `Streaming`, `OutputFormat`, `ShowThinking`, `ShowProgress`, `LLMBackend`, `Model` from `OrlaConfig`
  - Drop the `OrlaConfig` "service vs agent" TODO — config becomes daemon-only
  - Drop `github.com/charmbracelet/*` (bubbletea, lipgloss, termenv) deps from `go.mod`
- Update AGENTS.md and openapi.yaml to remove `/api/v1/execute`
- Update root CLI in `cmd/orla/main.go` to drop the `agent` subcommand
- Verify daemon still serves only `/v1/chat/completions` + control plane

### Phase I — Slim pyorla to control-plane SDK *(~4h)*
- **Delete**: `stage.py`, `workflow.py` (executor), `chat_model.py`, `messages.py`, `langchain_tools.py`, `tools.py`, `tool_decorators.py`, `tool_output.py`, `stage_mapping.py`, related tests
- **Add**:
  - `OrlaClient.openai_client(stage: str, workflow_run: str | None = None, **headers)` → configured `openai.OpenAI` (sync + async) pointed at the Orla proxy
  - Stage CRUD: `upsert_stage`, `patch_stage`, `get_stage`, `list_stages`, `delete_stage`
  - Feedback: `submit_feedback(stage, completion_id, rating=None, labels=None, notes=None)`, `get_feedback`, `get_metrics`
- **Trim** `types.py` to control-plane types only
- **New** `pyorla/examples/` with a few canonical demos showing the dev → platform engineer split

### Phase J — Documentation pass *(~1-2h)*
- README leads with the two-persona model
- AGENTS.md describes stages as first-class
- Architecture diagram or section: what's in Orla vs. external (the mapper)
- "For platform engineers" section in pyorla docs

## What's already done (pre-plan)

- Skill machinery fully removed from Go daemon and pyorla.
- `/v1/chat/completions` proxy endpoint exists in `internal/serving/api/openai_proxy.go` with: `model` = backend name, identity headers, message + tool conversion, streaming SSE, access control via existing `ValidateAccess`. 25 tests passing.
- The proxy *currently* also accepts the per-call inference knobs we've since decided belong on the stage record. **Phase A removes those.**

## Open questions

1. **Stage ID namespace** — global or scoped per tenant/workflow? Default global, add scoping later if needed.
2. **Default backend when stage is unmapped** — daemon-level config option `default_backend`; 400 if both missing.
3. **Naming**: `completion_records` is literal; alternatives `call_log`, `inference_log` worth considering at Phase F time.
4. **Stage idle timeout for cache flush** — exact default value TBD; start opt-in per stage.

## Estimated total

Roughly **2.5–3.5 days** of focused engineering. Phases A → B.0 → B → C are the critical path (everything after depends on the stage registry being live); D–G are independent additions; H–J are cleanup.
