# Agent Guidelines for Orla

This file provides guidance for AI agents working on the Orla codebase.

## Project Overview

Orla is a high-performance agent execution engine written in Go. It runs as a daemon (`orla serve`) or a one-shot CLI (`orla agent`), providing a unified API for building and running agents across multiple LLM backends (SGLang, Ollama, vLLM) via OpenAI-compatible APIs.

## Repository Layout

```
cmd/orla/            CLI entry points (main, serve, agent subcommands)
internal/
  agent/             Agent loop and one-shot executor
  config/            Viper-based YAML configuration
  core/              Shared types (LLMBackend, logger, utils)
  model/             Provider interface, OpenAI provider, mock helpers
  serving/           AgenticLayer, scheduler, backend manager
  serving/access/    Access control policy store and evaluator
  serving/api/       HTTP API server and routes
  serving/memory/    Memory manager and cache control
  testing/           Shared test utilities
  tui/               Terminal UI (Charm Bubbletea)
pkg/api/             Public Go client library (frozen — see Client API Policy below)
pyorla/              Python SDK (pip-installable, full-featured client)
  src/pyorla/        Package source
  tests/             pytest test suite
examples/            Example applications and demos
deploy/              Docker Compose configs for backends
scripts/             Install/uninstall scripts
```

## Before Completing Work

Always run these commands before considering a task complete:

```bash
make lint-all
make test-all
```

- **`make lint-all`** runs Go linters (`go vet` + `golangci-lint`) and pyorla linters (`ruff` + `ty`).
- **`make test-all`** runs both Go tests and pyorla pytest.

If you only changed Go code, `make lint` and `make test` are sufficient. If you only changed pyorla, `make pyorla-lint` and `make pyorla-test` are sufficient.

Fix any failures before finishing. Do not leave lint errors or failing tests.

Other useful targets:

| Target | Purpose |
|--------|---------|
| `make lint` | Go linters only (`go vet` + `golangci-lint`) |
| `make test` | Go tests only |
| `make pyorla-lint` | pyorla linters only (`ruff check` + `ty check`) |
| `make pyorla-test` | pyorla tests only (`pytest`) |
| `make pyorla-format` | `ruff format` + `ruff check --fix` on pyorla |
| `make format` | `go fmt` + `go mod tidy` |
| `make build` | Build binary to `bin/orla` |
| `make coverage` | Generate `coverage.html` from internal and pkg |
| `make test-race` | Run Go tests with race detector |
| `make test-integration` | Go integration tests |

## Code Style and Conventions

### Go Version and Module

The module is `github.com/harvard-cns/orla` using Go 1.25+. All code lives under `internal/` (private) or `pkg/` (public API).

### Naming

- Exported identifiers use PascalCase; unexported use camelCase.
- Acronyms stay uppercase: `LLMBackend`, `APIKeyEnvVar`, `TTFTMs`.
- Files: one main type per file; mocks go in `mock_*.go`; shared types go in `types.go`.

### Error Handling

- Wrap errors with context using `fmt.Errorf("description: %w", err)`.
- Include actionable context: `fmt.Errorf("inference failed on server '%s': %w", serverName, err)`.
- Validation errors should list valid options: `fmt.Errorf("log_format must be one of: %s, got '%s'", ...)`.
- Use `core.LogDeferredError(fn)` for deferred cleanup calls that may fail.

### Logging

Uses `go.uber.org/zap` via the global `zap.L()`. Always use structured fields:

```go
zap.L().Info("backend registered", zap.String("name", backendName))
zap.L().Error("inference failed", zap.String("server", name), zap.Error(err))
```

Logs go to stderr to avoid interfering with tool stdout (MCP).

### Concurrency

Shared state is protected with `sync.RWMutex`. Follow the lock/defer-unlock pattern:

```go
s.mu.Lock()
defer s.mu.Unlock()
```

Use `RLock`/`RUnlock` for read-only access.

### Optional Fields

Use pointer types for optional values (`*int`, `*float64`). Helper constructors like `core.IntPtr(n)` exist for building these.

### Interfaces

- Define interfaces in the same package as their primary consumer.
- Keep interfaces small and focused (e.g., `Provider` has three methods: `Name`, `Chat`, `EnsureReady`).
- Use the factory-registration pattern for pluggable implementations: `RegisterProviderFactory("openai", factory)`.

## Testing Conventions

### Structure

Use **table-driven tests** with subtests for multiple cases:

```go
tests := []struct {
    name        string
    // inputs and expected outputs
}{
    {name: "descriptive case name", ...},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // ...
    })
}
```

Use standalone `Test<Function>_<Scenario>` functions for single-case tests.

### Naming

Follow `Test<FunctionOrType>_<Scenario>`:

- `TestNewExecutor` -- constructor happy path
- `TestExecutor_Execute_NoToolCalls` -- method + specific scenario
- `TestLayer_Execute_ServerNotFound` -- nested type + method + scenario

### Assertions

Use `github.com/stretchr/testify`:

- **`require`** for preconditions and setup that must succeed (fails the test immediately).
- **`assert`** for the actual checks under test (continues on failure).

```go
require.NoError(t, err)
assert.Equal(t, expected, actual)
assert.Contains(t, err.Error(), "expected substring")
```

### Mocks

**Hand-written mocks with fluent builders** -- do not use code generators.

`MockProvider` example:

```go
provider := model.NewMockProvider().
    WithContent("response text").
    WithToolCall("tool_name", `{"arg":"value"}`).
    Build()
```

`MockLLMServer` for HTTP-level testing:

```go
server := model.NewMockLLMServer().
    ReturnContent("hello").
    Start()
defer server.Close()
```

Mocks include inspection helpers: `ReceivedMessages()`, `LastInferenceOptions()`, `CallCount()`.

Always verify interface compliance: `var _ Provider = (*MockProvider)(nil)`.

### Test Helpers

- `internal/testing` provides `NewCapturedOutput()` for stdout/stderr capture and `GetTestModelName()`.
- Use `MockLLMServer` for OpenAI-compatible chat API tests; use `httptest.NewServer` for other HTTP tests.
- Use `t.TempDir()` for temporary files and `t.Cleanup()` for teardown.

### Integration Tests

Integration tests use `MockLLMServer` for end-to-end flows without external services. They are named `TestIntegration_*` and run via `make test-integration` (or `make test` which includes them).

## Go Linting

`make lint` runs `go vet` and `golangci-lint`. The `.golangci.yml` enables these linters: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `errorlint`, `gosec`, `copyloopvar`, `goconst`, `misspell`.

Key settings:
- `errcheck` checks type assertions and blank identifier assignments.
- `gosec` excludes G204 (subprocess with variable) since Orla intentionally uses variables for tool paths.
- `goconst` flags strings repeated 3+ times that should be constants.

## Configuration

Orla uses Viper for YAML config loading. The config struct (`config.OrlaConfig`) uses both `yaml` and `mapstructure` tags. Valid enum values are stored as `map[T]struct{}` sets with validation in `validateConfig()`.

## Model Identifiers

Models follow the `provider:model-name` format (e.g., `openai:qwen3:0.6b`). The provider prefix selects which `ProviderFactory` creates the `Provider` implementation.

## Daemon API

The `orla serve` daemon exposes an HTTP API for inference and backend management. Base path: `/api/v1`.

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/health` | GET | Health check |
| `/v1/chat/completions` | POST | OpenAI-compatible chat completions proxy (streaming or non-streaming) |
| `/api/v1/execute` | POST | Run inference (streaming or non-streaming) |
| `/api/v1/backends` | GET | List registered backends |
| `/api/v1/backends` | POST | Register an LLM backend |
| `/api/v1/backends/{name}` | PATCH | Live-update backend cost_model, quality, or max_concurrency |
| `/api/v1/backends/{name}` | DELETE | Remove a registered backend and tear down its runtime state |
| `/api/v1/workflows` | POST | Register a workflow DAG for data label propagation |
| `/api/v1/policies` | POST | Add an access control policy |
| `/api/v1/policies` | GET | List all access control policies |
| `/api/v1/policies/{name}` | DELETE | Remove an access control policy |
| `/api/v1/stages/{id}` | PUT | Create or replace a stage record |
| `/api/v1/stages/{id}` | PATCH | Partially update a stage record |
| `/api/v1/stages/{id}` | GET | Read a stage record |
| `/api/v1/stages` | GET | List all stage records |
| `/api/v1/stages/{id}` | DELETE | Remove a stage record |
| `/api/v1/access/check` | POST | Validate access control policies without executing |
| `/api/v1/workflow/complete` | POST | Notify workflow completion for memory tracking |
| `/metrics` | GET | Prometheus metrics |

See `docs/openapi.yaml` for the full OpenAPI 3.0 spec.

### OpenAI-compatible proxy

`POST /v1/chat/completions` lets developers use any OpenAI-compatible client (raw OpenAI SDK, LangChain, LangGraph, AutoGen, CrewAI, etc.) by pointing `base_url` at the Orla daemon. The contract is intentionally minimal for the developer:

- **Routing is owned by the platform engineer's stage configuration.** Before dispatching, the proxy calls `StageRegistry.GetOrCreate(stage)` and reads `stage.Backend`. If the stage has a backend mapping, that wins. The request's `model` field is only consulted as a *fallback* when the stage has no mapping, and a stage with no mapping plus an empty `model` returns 400. New stages are auto-created with empty config on first sighting so the platform engineer can fill them in later via `PUT /api/v1/stages/{id}`.
- **The response's `model` field reports the resolved backend** — divergence from the request's `model` is by design under the two-persona contract, and the response body is the canonical source of truth for "which backend ran this".
- **Inference policy** is sourced from the stage record too. Currently `reasoning_effort` is applied if set on the stage; later phases will add scheduling policy, priority, and cache-flush behavior.
- **Headers** for Orla identity metadata (with `metadata.orla` in the request body as a fallback for SDKs that can't easily set headers):
  - `X-Orla-Stage` (required) — names the stage this call belongs to. Becomes the `stage` tag for access control.
  - `X-Orla-Workflow-Run` (optional) — workflow run id, becomes the `workflow_run` tag.
  - `X-Orla-Data-Labels` (optional) — comma-separated sensitivity labels, checked against data access policies.
  - `X-Orla-Tag-<Key>` (optional, repeatable) — adds an arbitrary tag (`tenant`, `group`, etc.) for policy matching. Header names are lowercased.
- Requests/responses are standard OpenAI chat completion shapes (including streaming SSE that terminates with `data: [DONE]`). Tool calls flow through `tool_calls` and `role: "tool"` messages — Orla observes tool names for policy enforcement but does not execute tools.

The original `POST /api/v1/execute` endpoint still works and uses Orla's bespoke request shape. New integrations should prefer the proxy.

## Production Features

The daemon includes production hardening:

- **Request body limit**: 10MB max for JSON bodies (execute, register backend, workflow complete).
- **Panic recovery**: Handlers are wrapped; panics are logged and return 500.
- **HTTP timeouts**: ReadTimeout, ReadHeaderTimeout, WriteTimeout (30 min for inference), IdleTimeout, MaxHeaderBytes.
- **Graceful shutdown**: On server error or SIGTERM/SIGINT, the daemon calls `Shutdown` before exiting.
- **Prometheus metrics**: `orla_requests_total`, `orla_queue_wait_seconds`, `orla_backend_latency_seconds`, `orla_queue_depth`, `orla_estimated_cost_usd_total`, `orla_estimated_cost_usd`, `orla_accuracy_routing_total` at `GET /metrics`.
- **Retry with backoff**: Provider calls retry on 5xx, 429, and network errors (up to 3 attempts, exponential backoff).
- **Rate limiting**: Optional `rate_limit_rps` in config limits execute and backends endpoints; 0 disables.

Config options for `orla serve` (in `OrlaConfig`): `listen_address`, `rate_limit_rps`, `log_format`, `log_level`, `storage_path`.

## Storage

The daemon persists backend configuration, stage configuration (and later, completion records and feedback) in an embedded SQLite database via the `internal/storage` package.

- **Driver**: `modernc.org/sqlite` (pure-Go, statically linkable, no CGo).
- **Path**: `storage_path` config option. Defaults to `$XDG_DATA_HOME/orla/orla.db` or `~/.orla/orla.db`. Use `:memory:` for ephemeral in-process databases (tests).
- **Pragmas**: `journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`, `busy_timeout=5000`.
- **Migrations**: Embedded `.sql` files under `internal/storage/migrations/`, applied in numeric order, tracked in the `schema_migrations` table.
- **Helpers**: `Store.Tx(ctx, fn)` runs `fn` in a transaction; `storage.BatchWriter[T]` is a generic async batched writer used for high-volume telemetry (Phase F onwards).

## Backends

LLM backend definitions are platform-engineer-owned state, persisted alongside stages.

- **Backend record**: `name`, `endpoint`, `model_id` (provider-prefixed, e.g. `openai:gpt-4o` or `sglang:Qwen/Qwen3-4B`), `api_key_env_var`, `max_concurrency`, `queue_capacity`, `cost_model`, `quality`.
- **Provider selection**: the prefix on `model_id` picks the provider factory. Both `openai:` and `sglang:` route to the OpenAI-compatible provider; the `sglang:` prefix additionally wires up the SGLang `/flush_cache` cache controller.
- **In-memory state**: provider clients, scheduler queues, and concurrency semaphores live in `LLMBackendManager` and are reconstructed lazily on first use. The records themselves come from SQLite.
- **Rehydration**: the daemon calls `AgenticLayer.LoadPersistedBackends` on startup so all previously-registered backends come back without re-pushing.
- **REST API**: see the Daemon API table above. `POST /api/v1/backends` creates; `PATCH /api/v1/backends/{name}` updates cost_model/quality/max_concurrency in place; `DELETE /api/v1/backends/{name}` removes both the record and runtime state.

## Stages

Stage configuration is owned by the platform engineer (or an external mapper). The `internal/stages` package persists stage records in SQLite with an in-memory read cache.

- **Stage record**: `id`, `backend`, scheduling policy + priority knobs, `max_concurrency`, `reasoning_effort`, `flush_on_complete`, `reasoning_hint` (developer-supplied), free-form `labels`.
- **Auto-create**: `GetOrCreate(ctx, id)` inserts a default row on miss, so a stage exists as soon as a developer first uses its id.
- **REST API** is listed in the Daemon API table above. PUT replaces the whole record; PATCH updates only the fields present in the request body (clear nullable fields by re-PUTting without them).
- **Phase B scope**: registry + CRUD only. Phase C wires the proxy to consult `StageRegistry.Get(stage)` for backend selection and inference options; Phase D applies stage-scoped scheduling and per-stage concurrency.

## Access Control

The `serving/access` package implements a deny-overrides policy engine. Policies are installed at runtime via the HTTP API or pyorla SDK.

- **Policy model**: Each policy has subjects (glob patterns matching request tags like `"tenant:interns"`), resources (glob patterns matching `"backend:gpt4o"`, `"tool:shell_*"`, or `"data:pii"`), and an action (`"allow"` or `"deny"`).
- **Evaluation**: Deny overrides allow. If no policies match, access is allowed by default (open policy).
- **Enforcement**: Checked in `handleExecute` before backend selection. Backends, tools, and data labels are all deny-on-match (requests are rejected with 403, not silently filtered).
- **Subject identity**: Request tags (`map[string]string`) carried in the `tags` field of execute requests. Workflow-level tags propagate to all stages.

## pyorla (Python SDK)

`pyorla/` is the full-featured Python SDK. All new features target pyorla first.

### Development

```bash
cd pyorla
uv pip install -e ".[dev]"    # editable install with dev deps
make pyorla-test               # run tests (from repo root)
make pyorla-lint               # run ruff + ty (from repo root)
```

### Linting

pyorla uses **ruff** for linting/formatting and **ty** for type checking. Both are configured in `pyorla/pyproject.toml`.

- `make pyorla-lint` runs `ruff check` and `ty check`.
- `make pyorla-format` runs `ruff format` and `ruff check --fix`.
- Always use `uv` to run Python tools (e.g., `uv run ruff`, `uv run ty`, `uv run pytest`).

### Code Style

- **Dataclasses** for all wire types (`types.py`). Avoid Pydantic unless schema validation is needed.
- **Sync + async pairs**: Every client method has a sync version and an `a`-prefixed async version (e.g., `execute` / `aexecute`).
- **Private helpers**: Prefix with `_` and place at the bottom of the file (e.g., `_parse_execute_response`, `_backend_to_dict`).
- **Type annotations**: Use `from __future__ import annotations` and modern union syntax (`str | None`).
- **Exports**: All public types go in `__init__.py` `__all__` list. Keep imports organized: constants, then classes, then functions.

### Testing

- Tests live in `pyorla/tests/`, one file per source module (e.g., `test_client.py` tests `client.py`).
- Use **pytest** with plain functions (`def test_*`), not unittest classes.
- Use `monkeypatch` for environment variables, `httpx` mock responses for HTTP tests.
- Run with `make pyorla-test` (from repo root) or `uv run pytest tests/ -v` (from `pyorla/`).

### Key Modules

| Module | Purpose |
|--------|---------|
| `client.py` | `OrlaClient` — sync/async HTTP client for all daemon endpoints |
| `types.py` | Wire types: `ExecuteRequest`, `LLMBackend`, `AccessPolicy`, constants |
| `stage.py` | `Stage` — primary execution unit, builds requests, runs tools |
| `workflow.py` | `Workflow` — DAG of stages with dependency-aware parallel execution |
| `chat_model.py` | `ChatOrla` — LangChain `BaseChatModel` adapter backed by a Stage |
| `tools.py` | `Tool`, `ToolCall`, `ToolResult` types and MCP conversion |
| `langchain_tools.py` | `tool_from_langchain()` — converts LangChain tools to Orla tools |
| `messages.py` | Bidirectional LangChain/Orla message conversion |

## Client API Policy

- **`pkg/api/` (Go client)**: Frozen at its current feature set. Supports register, execute, streaming, and workflows. New features (cost-based routing, accuracy, fallback policies) are **not** added here. The existing Go examples (`swe_bench_lite`, `dag_math_eval`, etc.) continue to use it.
- **`pyorla/` (Python client)**: The full-featured client. All new features target the HTTP API and pyorla first.
- **HTTP API**: The source of truth. Go consumers needing new features should use the HTTP API directly.
