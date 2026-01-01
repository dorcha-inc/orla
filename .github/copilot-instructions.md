<!-- Short, focused guidance for AI coding agents working on the Orla repo -->
# Orla — Copilot Guidance

This file contains concise, repository-specific guidance to help an AI coding agent be immediately productive.

**Quick Orientation**
- **Purpose:** Orla is a Go (1.25) runtime for Model Context Protocol (MCP) servers that discovers executable tools from a `tools/` directory and exposes them as MCP tools. See [README](../README.md) for high-level examples.
- **Binaries:** Primary entry points live under `cmd/` — in particular `cmd/orla` (server) and `cmd/orla-test`. Build via `make build` which produces `bin/orla`.
- **Packages:** Core implementation is in `internal/` (e.g., `internal/agent`, `internal/registry`, `internal/tool`, `internal/core`). Keep package APIs stable; prefer internal package changes that preserve external CLI behavior.
- **Examples & Registry:** Examples are in `examples/` (each example has its own test/run flow). Tool registry is in the sibling repo `orla-registry` and used by `orla install`.

**Build / Test / Run workflows**
- **Build:** `make build` (sets `main.version` and `main.buildDate` via ldflags). `make install` installs binary via `go install`.
- **Run server:** `./bin/orla serve` or after `go install` run `orla serve`. Use `--stdio` for stdio transport. Config defaults check `orla.yaml` in CWD.
- **Unit tests:** `make test` runs unit tests for all packages (excludes integration tests).
- **Integration tests:** `make test-integration` runs tests with the `integration` build tag — these require external services (e.g., Ollama) and should not be run in minimal CI without dependencies.
- **E2E:** `make test-e2e` runs `scripts/e2e-test.sh` which builds binaries, starts the server for each example, runs example tests, and tears down the server. Use this for full-stack verification.
- **Lint/Format:** `make lint` (uses `golangci-lint`), `make format` runs `go fmt` and `go mod tidy`.

**Project-specific conventions and patterns**
- **Tools discovery:** Orla treats any executable under `tools/` (configurable via `tools_dir` in `orla.yaml`) as an MCP tool. Tool behavior is intentionally generic—tests and examples often use small shell scripts or Python binaries in `examples/` or `tools/`.
- **Hot reload:** The server supports hot reload via SIGHUP: `kill -HUP $(pgrep orla)` to refresh tools/config without restarting.
- **Config layering:** CLI flags override `orla.yaml`. Default port is `8080` unless `--stdio` is specified.
- **Versioning in builds:** `Makefile` injects `main.version` and `main.buildDate` via `-ldflags` — tests or tooling that assert binary version should account for these flags.
- **Testing tags:** Integration tests use `-tags=integration` and often use `-run Integration` in CI; keep those tests guarded and clearly annotated.

- **Key files to inspect (start here)**
- `README.md` — repo overview and examples
- `Makefile` — canonical build/test/lint commands
- `scripts/e2e-test.sh` — how examples are exercised end-to-end
- `cmd/orla` — server entrypoint, CLI flags
- `internal/tool` and `internal/registry` — tool discovery and registry integration
- `examples/` — runnable minimal integrations and their tests

**Integration points & external dependencies**
- Uses MCP Go SDK: `github.com/modelcontextprotocol/go-sdk` (see `go.mod`).
- Some integration tests require Ollama or other services; these are the reason integration tests are gated by tags.
- The Orla Tool Registry is a separate repo (`orla-registry`) referenced by `orla install` — changes to registry behavior can affect examples and CI.

**Examples of guidance to follow**
- When adding features that affect tool discovery, update tests under `internal/tool` and add an example under `examples/` demonstrating the new behavior.
- For CLI changes, update `cmd/orla` and ensure `Makefile`/build flags are preserved (version/buildDate injection).
- For tests that interact with external services, prefer adding a CI-specific guard or build tag rather than making them run in default `make test`.

If anything here looks incomplete or you need more detail (e.g., recent CI behavior or author intent for a subsystem), tell me which area and I'll expand or merge with existing docs.
