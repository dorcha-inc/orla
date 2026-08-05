# AGENTS.md

Guidance for AI agents working on the orla codebase. The CLAUDE.md
symlink resolves to this file. Read top-to-bottom on first session.

## Project context

Orla is a Go daemon that sits between agent code and the LLM or tool
backends that serve it. It is an OpenAI-compatible proxy with two
side channels: a registry the platform engineer writes to, and a
telemetry stream the mapper reads from. See [`docs/concepts.md`](docs/concepts.md)
for the model, [`docs/quickstart.md`](docs/quickstart.md) for the
hands-on tour, and [`docs/proxy.md`](docs/proxy.md) plus
[`docs/storage.md`](docs/storage.md) for the wire and schema details.

The codebase is small and uniformly Go. There is no Python SDK in
this repo. Frontend tooling, CI helpers, and demos live in separate
repositories.

## Quality gate

Before declaring any change complete, run:

```bash
just check
```

That runs `go build ./...`, the test suite with the race detector, a
coverage profile, `golangci-lint`, and an offline markdown link
check. The pipeline is the same one CI runs. If it does not pass
locally, the work is not done.

Individual recipes:

```bash
just build        # compile every package
just test         # tests only (Docker required for testcontainers)
just test-cover   # tests with coverage.out (used by CI)
just lint         # golangci-lint v2
just links        # offline markdown link check
just fmt          # go fmt + go mod tidy
just sqlc         # regenerate internal/storage/db
just binary       # build bin/orla
just modernize    # gopls modernize report (no changes)
just              # list recipes
```

Storage tests use [testcontainers](https://testcontainers.com/) and
need Docker running.

## Repository layout

```
cmd/orla/                CLI entry point and cobra subcommands
internal/
  api/                   HTTP server, middleware, route handlers, proxy
  backends/              Backend registry (PostgresRegistry + FakeRegistry)
  config/                envconfig-based daemon configuration
  metrics/               Prometheus collectors and registration
  provider/              LLMProvider + ToolProvider interfaces
  scheduler/             Per-backend FCFS executor with concurrency caps
  stages/                Stage registry (PostgresRegistry + FakeRegistry)
  storage/               pgx pool, goose migrations, BatchWriter
  storage/db/            sqlc-generated query code (regenerate with `just sqlc`)
  storage/queries/       sqlc query files
  storage/migrations/    goose .sql files
  telemetry/             Completion and feedback writers + readers
docs/                    User-facing documentation
share/                   README banner assets
```

There is no `pkg/`. Public clients consume orla over HTTP, not by
importing Go packages.

## Writing prose

The following style rules apply to all prose in the repo: README,
docs, design notes, commit message bodies, code comments. They were
established by the maintainer's stated preference and must be
honored.

### Hard rules

- **No em-dashes.** The character `—` does not appear in prose. If
  you would normally use an em-dash, split into two sentences or use
  a comma. The same goes for en-dashes (`–`) in prose.
- **No semicolons in prose.** Use a period and start a new sentence.
- **No unnecessary parentheses.** Parenthetical asides that pause the
  reader for a thought you could have put in its own sentence should
  go in its own sentence. A one-word gloss such as an abbreviation on
  first use is fine. Anything longer is a sentence you have not
  written yet, and parens are never a substitute for a comma or a
  period.
- **Don't pack settings into parentheses.** A run of
  `NAME (default X), OTHER (default Y)` is unreadable, and a second
  clause tacked on after a semicolon makes it worse. Put environment
  variables, flags, and their defaults in a table with a column for
  the name, the default, and the meaning.
- **No ASCII diagrams.** Describe relationships in prose. ASCII boxes
  and arrows are hard to maintain and rarely earn their space.
- **No emoji** unless the user explicitly asks for them.
- **Don't define a thing by what it is not.** "X, not Y" and its
  variants, "X rather than Y", "X and never Y", tell the reader what
  to erase instead of what to hold. Say what the thing does and stop.
  "The audit is a visibility aid, not an access gate" becomes "The
  audit records every write and lets all of them through." State a
  limit outright when it is load-bearing, as its own sentence, in
  terms of what happens: "Orla still serves the request."
- **No vague back-references.** Do not open a sentence with "This",
  "That", "These", "Those", "Their", or "It" pointing at a noun from
  an earlier sentence. Name the noun again. "This is the wrong
  baseline" becomes "The best region in hindsight is the wrong
  baseline." The reader should never have to look backward to resolve
  what a pronoun stands for.

### Soft rules

- Write short, direct sentences. If a sentence has more than one
  comma, consider whether it should be two sentences.
- Lead with the noun, not the qualifier. "The proxy looks up the
  stage" beats "When a request arrives, the proxy looks up the
  stage."
- Define jargon on first use, even if you think the reader knows it.
- Do not write in fragments or in a punchy, aphoristic style. Short
  clipped clauses strung together read like a parable, not like
  documentation. "No key, no cost, runs on a laptop" is wrong.
  "Ollama runs models locally, so it needs no API key and costs
  nothing to call" is right.
- Don't write multi-paragraph code docstrings. One short paragraph
  per exported identifier is the ceiling.

### Examples

Wrong:

> The proxy auto-creates a default stage record (empty backend, empty
> everything) on first sighting; the request then falls back to
> `req.Model` for that one call.

Right:

> The proxy auto-creates a default stage record on first sighting.
> The request falls back to `req.Model` for that one call.

Wrong:

> Cost comes from the generic Rates map, with the tool reporting
> matching usage at dispatch time. ToolKind is required, ModelID is
> unused.

Right:

> Cost comes from the generic Rates map, with the tool reporting
> matching usage at dispatch time. ToolKind is required. ModelID is
> unused.

## Writing comments

The rules under "Writing prose" apply, plus:

- **Default to writing no comment.** A well-named identifier and a
  short function explain themselves. Only comment when the WHY is
  non-obvious: a hidden constraint, a subtle invariant, a workaround
  for a specific upstream bug, behavior that would surprise a reader.
- **Don't describe what the code does.** The code does that.
  "Increments the counter" above `c++` is noise.
- **Describe what the code does do, not what it avoids.** A comment
  that lists rejected alternatives or absent behavior goes stale the
  moment the code changes. State the behavior that exists and the
  reason for it. Name an absence only when it is a load-bearing
  constraint a maintainer would otherwise violate, such as a retry
  that must not happen, and give the why. Write that absence as
  behavior, not as a contrast. "The mapper falls back to the static
  mapping on any error" beats "the mapper is a hint, not a gate."
- **Don't reference the past.** "Renamed from X", "formerly Y",
  "was previously fooBar" all rot. Comments describe the present
  state. If the reader needs migration history they can `git log`.
- **Don't reference callers or PRs.** "Called by X", "added for
  the Y flow", "see issue #123" all rot as the codebase evolves.
  Caller context belongs in the PR description.
- **Don't write multi-line comment banners.** Use one short comment
  per declaration, not a docblock with `@param`/`@returns`/`@example`
  decoration. Go doc tooling reads the comment line directly above
  the symbol.
- **Write comments as prose a human reads top to bottom.** Make them
  correct, clear, and concise. No unnecessary parentheses, semicolons,
  colons, or dashes. When a parenthetical or a clause after a colon
  carries real weight, give it its own sentence instead. The em-dash
  and the semicolon are banned in comments as they are in all prose.
- Package docs go in one file per package and explain the package's
  purpose in a paragraph or two. They are an exception to the
  "default to no comment" rule.

### Doc comment form

When a comment is warranted, for an exported identifier, a package
doc, or a non-obvious why, write it the way `gofmt` and `go doc`
expect. The full guide is [Go Doc Comments](https://go.dev/doc/comment).
The conventions that matter here:

- Put the comment on the line directly above the declaration with no
  blank line between them.
- Begin a doc comment with the name of the thing it documents so it
  reads as a sentence. `// Acquire blocks until a slot frees.` reads
  better than `// blocks until a slot frees.`
- Use complete sentences and end them with a period. A package comment
  starts with `Package <name>`.
- Say what a function returns or does for the caller, not how it works
  inside. Use "reports whether" for a boolean result, never "returns
  true if ... or not".
- Name parameters and results directly in the text. No backticks.
- State a type's concurrency guarantees and any useful zero value when
  they are not obvious.
- Mark a removal with a `Deprecated:` paragraph so tooling can flag it.

## Writing documentation

The docs under `docs/`, the tutorials on the site, and every example
README are read top to bottom by someone following along. The prose
rules above apply, plus a few that matter for a page with commands in
it.

- **Explain every code block.** Never drop a command or snippet
  without saying what it does and what every meaningful flag means.
  Show output too, and say what its columns or fields mean.
- **Headings name content, and are not narration.** "Dynamic stage
  mapper" and "Cost, latency, and feedback" are good. "Now we register
  the backends" narrates the act instead of naming the subject. "What
  makes a workflow static" poses a question the prose should just
  answer.
- **Do not over-chunk.** A heading breaks the reader's flow. Add one
  only where a genuinely new section begins. Merge two short sections
  that are really one idea and let a sentence carry the transition.
- **Cut filler.** Remove words that earn nothing. If "static" already
  carries the meaning, do not also write "fixed". Do not lean on one
  adjective across a passage.

## Writing tests

### Structure

Use table-driven tests with subtests when there are multiple cases of
the same shape. Use standalone `Test<X>_<Scenario>` functions when
there is one case or each case is shaped differently.

```go
tests := []struct {
    name string
    in   T
    want U
}{
    {name: "descriptive case", in: ..., want: ...},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // ...
    })
}
```

Always write struct literals inside table cases with field names.
Positional literals break silently when the struct grows a field,
and the failure shows up as a test passing on the wrong input
rather than as a compile error. The same rule applies to any
struct literal where the field types do not uniquely identify the
field by position.

### Naming

`Test<Type>_<Scenario>` or `Test<FunctionName>_<Scenario>`. The
scenario reads as a clause: `TestProxy_ComputesLLMCostUSD`,
`TestFakeRegistry_GetReturnsIndependentRatesCopy`,
`TestBackendHandlers_CreateRejectsRatesOnLLM`.

### Assertions

`github.com/stretchr/testify` is the assertion library.

- `require` for preconditions and setup that must succeed before the
  rest of the test makes sense. Failure halts the test.
- `assert` for the actual checks under test. Failure records the
  failure and continues so the test surfaces every problem in one
  run.

### Mocks and fakes

Use hand-written fakes with the suffix `Fake*` (e.g. `FakeRegistry`).
Do not introduce a mocking code generator.

Share one fake per interface across the package's tests. If a fake
already exists for an interface, extend it rather than hand-rolling a
second.

For HTTP-shaped dependencies, prefer `httptest.NewServer` or a
hand-written fake server function that returns a *httptest.Server.
Both are easier to debug than generated mocks.

Every Fake should pass the same contract test as the real
implementation (see `internal/backends/fake_registry.go` and the
contract test that exercises both `PostgresRegistry` and
`FakeRegistry`).

### What to test

- The happy path for every exported function.
- Every validation branch that returns an error.
- Boundary cases for numeric inputs (zero, max, NaN, Inf, negative).
- One end-to-end integration test per HTTP route that wires storage,
  scheduler, and the handler together.
- The fakes themselves. A silently broken fake hides regressions.

## Commit messages

[Conventional commits](https://www.conventionalcommits.org/en/v1.0.0/),
one sentence each, no body unless absolutely necessary.

```
feat: compute cost_usd for LLM dispatches from per-million-token rates
fix: reject non-finite tool costs and log misconfig
docs: refresh storage.md schema and cost semantics for v2
refactor: squash migrations into a single fresh init.sql
chore: drop demo-* justfile recipes since demos live in a separate repo
style: replace em-dashes with commas in Go comments
test: add coverage for batch writer drop policy
```

Rules:

- One sentence subject. No imperative-vs-past-tense pedantry, just
  pick one and be consistent. The examples above use present-tense
  imperative.
- Lowercase the type and the first word after the colon, unless that
  first word is a proper noun or acronym.
- No commit body unless the change is non-obvious and the reason
  cannot fit in the subject. Don't pad with a "Test plan" or
  "Summary" boilerplate.
- **No `Co-Authored-By: Claude` trailer.** Ever. Even when the user
  has authorized commits in advance.
- Do not amend or rewrite published commits without explicit user
  consent. Force-push only with `--force-with-lease`, only on a
  feature branch, and only after confirming with the user.

## Git practices

- **Never commit without asking. Never push without asking.** Both are
  separate acts and each needs its own approval. Propose the commit,
  name the files and the message, then wait for the user to say yes.
  Leave the work uncommitted until they do.
- **Approval never carries forward.** A yes for one commit is not a
  yes for the next one, and a yes to commit is not a yes to push. Ask
  again every time, however routine the change looks and however many
  times the user has already agreed in the session.
- **Approval to do work is not approval to commit it.** "Sounds good",
  "go ahead", and "yes" in reply to a plan mean write the code. They
  do not mean commit it, and they never mean push it. When the user
  approves an implementation, finish it, run the quality gate, and
  stop with a clean summary and a commit proposal.
- Use whatever git identity the user has configured. Never pass
  `-c user.email` or `-c user.name`.
- For PR merges, prefer `gh pr merge <num> --squash --delete-branch`.
- Before any destructive operation (`git reset --hard`, force-push,
  `git rm -r .`, branch delete), confirm with the user. Use
  `--force-with-lease` not `--force` when force-pushing is
  authorized.

## Go style

### Principles

Follow the priorities of [Google's Go style guide](https://google.github.io/styleguide/go/):
code should be correct first, clear second, and concise third. Never
trade an earlier property for a later one. Judge clarity from the
reader's seat, not the writer's.

Aim for the simplest design that solves the problem. Simple sometimes
means clever. A well-chosen invariant can delete a page of special
cases. Simple never means convoluted. Code that a reader must
simulate in their head before trusting it is not simple yet.

When two designs are otherwise equal, pick the one with less
mechanism. Fewer moving parts, fewer knobs, less code, and less for
an operator to run and monitor.

Avoid grab-bag packages. Names like util, common, and helpers say
nothing about what a package contains, and both the
[Go blog](https://go.dev/blog/package-names) and Google's style guide
reject them. Shared code belongs in the package that owns its
concept.

### Naming

- Exported: PascalCase. Unexported: camelCase. Acronyms stay
  uppercase: `LLMBackend`, `APIKeyEnvVar`, `CostUSD`.
- One main type per file. Mocks/fakes in `fake_*.go` or `mock_*.go`.
  Package-shared types in `types.go` if there is no obvious home.
- Receivers are one-letter abbreviations for the type name (`r` for
  `*PostgresRegistry`, `s` for `*Scheduler`).

### Errors

- Wrap with `fmt.Errorf("operation: %w", err)`. Include enough
  context that the caller can tell which operation failed without
  reading the trace.
- Error strings start lowercase and end without punctuation. They
  read as the leaf of a wrapped chain. `fmt.Errorf("decode body:
  %w", err)` chains cleanly into `"telemetry: decode body: ..."`,
  whereas a capitalized or period-terminated leaf produces ugly
  joins.
- Return sentinel errors (`var ErrNotFound = errors.New(...)`) for
  conditions callers branch on. Compare with `errors.Is`, never with
  string matching. Document the sentinel in the package doc.
- Use `errors.As` for typed-error inspection. For multiple errors
  from concurrent work, `errors.Join` is the stdlib aggregator. Do
  not write hand-rolled multi-error types.
- Validation errors should describe both the constraint and the bad
  input: `"max_concurrency must be >= 1, got 0"`.
- Don't swallow errors. If an operation can fail and you choose to
  proceed anyway, log at warn level with enough context that an
  operator can investigate.
- Don't encode failure as a sentinel value of the result type. A
  function that returns `-1` or `""` or `NaN` to mean "not found"
  forces every caller to remember the rule. Return `(T, bool)` for
  lookups and `(T, error)` for fallible work.

### Context

Pass `context.Context` as the first parameter on every function that
touches the database, the network, or a goroutine. Plumb the request
context from the HTTP handler all the way down to the pgx call so
cancellation and deadlines propagate.

Never store a context on a struct field. A struct that needs
cancellation gets a `Shutdown(ctx context.Context)` method instead.

`context.Background()` is reserved for top-level main and tests.
Anywhere else it indicates a missing plumb.

### Logging

`log/slog` everywhere. Use structured key-value attrs, not
formatted strings.

```go
slog.Default().Warn("tool: dropping non-finite reported cost",
    "backend", backendName,
    "completion_id", completionID,
    "cost_usd", c,
)
```

Logs go to stderr so stdout stays clean for tool wrappers and child
processes.

### Concurrency

- Protect shared state with `sync.RWMutex`. Use `RLock`/`RUnlock` for
  read-only access.
- `defer mu.Unlock()` immediately after `mu.Lock()`. Keep the locked
  region small.
- Never write to a map under `RLock`. If you find yourself wanting
  to, restructure or upgrade to `Lock`.
- Channels for ownership transfer, mutexes for protecting fields.
  Don't use channels as locks.
- When passing maps across goroutine boundaries, copy them at the
  boundary unless the producer documents the no-mutate contract.
  `maps.Clone` is the one-line stdlib helper.
- Do not copy a struct that contains a `sync.Mutex`, a
  `sync.RWMutex`, or an `atomic.*` value. Pass it by pointer through
  arguments and return values. `go vet` catches most cases, and the
  resulting bug under load looks like phantom unlock failures.

### Goroutines

Every goroutine has a documented exit. Either it returns when a
context is cancelled, it reads from a channel the owner closes, or
the caller calls a `Stop` or `Shutdown` method that joins it. Never
start a goroutine from `init`, from a constructor that has no
`Stop`, or from an HTTP handler without a way to wait on it.

The `BatchWriter` and the scheduler executors are the canonical
patterns. Match them.

### Type assertions

Always use the comma-ok form: `v, ok := x.(T)`. A bare `x.(T)`
panics on mismatch and crashes the daemon. The same rule applies to
map reads where the absent case is meaningful: `v, ok := m[k]`.

### Interfaces

Define an interface in the package that consumes it, not the package
that implements it. Add an interface only when there is a real
second implementation or a real fake. Producers return concrete
types so callers see the full surface.

`backends.Registry` is the right pattern: defined in `backends`
because both `PostgresRegistry` and `FakeRegistry` live there and
serve consumers in `internal/api`.

### Optional fields

Use pointer types for optional values: `*int`, `*float64`,
`*string`. nil means "not set".

For maps in patch requests, use `*map[K]V` so the caller can
distinguish three states:

- absent field in JSON: the pointer is nil. Don't modify.
- JSON null or pointer to nil map: clear.
- pointer to populated map: overwrite.

See `backends.PatchRequest.Rates` for the canonical example.

### Deferred Close

On anything writable, do not write `defer x.Close()` without
inspecting the error. A `*sql.Tx`, a buffered writer, or a flushable
sink can fail at `Close` and lose writes. Use a deferred closure
that captures a named return so the error surfaces:

```go
func write(...) (err error) {
    f, err := os.Create(path)
    if err != nil { return err }
    defer func() {
        if cerr := f.Close(); cerr != nil && err == nil {
            err = cerr
        }
    }()
    ...
}
```

Read-only handles are exempt. A `defer body.Close()` on an HTTP
response body is fine.

### Validation

Validate at four boundaries:

1. The HTTP handler before any business logic runs.
2. The storage layer when reading an operator-managed JSONB or
   nullable column.
3. The proxy when consuming a tool wrapper's reported usage or
   self-reported cost.
4. The scheduler when accepting a new request that could exceed
   capacity or rate limits.

Past these boundaries values are trusted and not re-checked. A
helper like `isFiniteNonNegative(float64) bool` keeps the boundary
checks readable. Don't sprinkle finite-checks throughout the call
stack.

### Failure handling

Orla is critical infrastructure. Fail loud at startup and config time.
Degrade gracefully at request time. These are not in tension. They
apply at different layers.

At construction and wiring time a missing or invalid required
dependency should fail fast, before the listener serves traffic. A
readiness probe catches it, the deploy rolls back, and no live request
is affected. Crashlooping on a bad config beats coming up healthy and
serving blind.

On the request hot path, degrade instead of crashing. One bad input or
one unavailable backend must not take down in-flight work. Return an
error envelope, shed load with a 503, or no-op an optional side effect.
The scheduler shedding to 503 with a Retry-After header is the pattern.

Never split the difference by tolerating a required dependency at
construction and then panicking on the first request that needs it.
That turns a config mistake into a delayed outage under load. Validate
it at the wiring boundary or make it a genuinely optional dependency
that no-ops when absent, the way the proxy treats its metrics sink.

### Don't reinvent the wheel

Before writing any non-trivial logic, look for something already
built. Search the standard library first, then the deps already in
go.mod, then the wider module ecosystem. A mature package is almost
always more correct and better tested than a version written under
deadline, and every line not written is a line nobody has to review,
test, or maintain. Hand-rolling what a library already solves does
not just cost the lines, it adds a design of our own that we now own
forever.

Judge a candidate dependency by its maintenance and adoption. Recent
releases, responsive maintainers, wide use in serious projects, and a
focused scope are the signals that matter. Stars and download counts
are hints, not verdicts. A good dependency is welcome. Reinvent only
when nothing fits or the dependency would weigh far more than the
problem it solves.

Go 1.26's standard library covers most of the helpers a new contributor
would otherwise hand-roll. Reach for these before writing your own:

- `maps.Clone(m)` for shallow map copy. `maps.Keys(m)` returns an
  iterator. `slices.Sorted(maps.Keys(m))` gives a sorted slice.
- `slices.Sort`, `slices.Contains`, `slices.Concat`, `slices.Collect`
  are the modern replacements for hand-rolled loops.
- `cmp.Or(a, b, c)` picks the first non-zero value. The common
  `if x == "" { x = fallback }` pattern collapses to one line.
- `errors.Is`, `errors.As`, `errors.Join` for error introspection
  and aggregation.
- `sync.OnceFunc`, `sync.OnceValue` for one-shot initialization.
- `context.WithTimeout`, `context.AfterFunc` for cancellation.
- `golang.org/x/sync/errgroup` for parallel goroutines with shared
  cancellation. Use it instead of hand-rolling
  done-channels-plus-select-on-context.

Already-imported deps to use rather than re-implementing:

- `github.com/cenkalti/backoff/v4` for retries with exponential
  backoff.
- `github.com/google/uuid` for ids.
- `golang.org/x/time/rate` for token-bucket rate limiting.
- `github.com/jackc/pgx/v5` types for nullable database columns
  (`pgtype.Text`, `pgtype.Timestamptz`).

## Python style

The daemon is Go. Python appears only under `examples/`, where each
example is a small runnable agent that drives Orla over its
OpenAI-compatible endpoint. These rules keep those examples
consistent with each other and with the Go side. They lean on the
same instincts: type everything, fail loudly, prefer the standard
library, and let the tooling enforce the rest.

### Tooling

The toolchain is Astral's, and it is not optional.

- **uv** for environments and dependencies. Not pip, not poetry, not
  a bare `requirements.txt`. Use `uv add` to add a dependency,
  `uv lock` to resolve, `uv run` to execute inside the project
  environment.
- **ruff** for both linting and formatting. It replaces black,
  isort, and flake8. There is one formatter and one linter, and they
  are the same tool.
- **ty** for type checking. It is Astral's checker and still young,
  so expect rough edges, but it is the house checker. Do not reach
  for mypy or pyright instead.
- **just** for task running, the same as the Go side. Every example
  ships a `justfile` with the same recipe names so muscle memory
  carries across examples: `run`, `fmt`, `lint`, `typecheck`, and a
  `check` that runs the read-only trio the way CI would.

### Project layout and dependencies

Each example is a self-contained uv project under
`examples/<name>/` with its own `pyproject.toml` and `uv.lock`.

- Runtime dependencies go in `[project].dependencies`. Development
  tools like ruff and ty go in `[dependency-groups].dev` per
  PEP 735. `uv run` installs the dev group by default, so a
  contributor gets the linters without a second command.
- Pin every direct dependency to an exact version with `==` and commit
  `uv.lock`. The lockfile resolves the full tree with hashes, so
  installs are both reproducible and verified against supply-chain
  tampering. An example is a thing you run, not a library someone
  imports, so pinning beats flexibility. A library would use floors
  with `>=` instead.
- The lockfile is committed and never hand-edited. uv owns it.
  `.gitignore` covers `.venv/`, `__pycache__/`, `.ruff_cache/`, and
  `.ty_cache/`.

### Types

Type every function signature, both parameters and return. `ty check`
runs in `just check` and in CI, so an untyped surface is a failing
build.

- Put `from __future__ import annotations` at the top of every
  module. Annotations become lazy strings, and a 3.10 target can
  write `X | None` and `list[int]` without importing from `typing`.
- Use the built-in generics, `list[int]` and `dict[str, T]`, not
  `typing.List`. Use `X | None`, not `Optional[X]`.
- Reach for a `TypeVar` when a function's return type depends on a
  type handed in. `_ask(..., schema: type[T]) -> T | None` in the
  HotpotQA example returns the exact model it was asked for. A
  function that returns the base type instead forces every caller to
  narrow it back.
- Model structured data that crosses a boundary with a Pydantic
  model or a dataclass. Do not pass bare dicts whose shape lives
  only in your head. This is the Python form of the Go rule about
  field-named struct literals.

### Imports

Every import goes at the top of the module. A function-level import
hides a dependency from both the reader and the tooling, and it defers
an ImportError from startup to call time. Import inside a function
only to break a genuine circular import or to keep an optional
dependency optional, and name the reason in a comment.

### Naming

- `snake_case` for functions and variables, `PascalCase` for
  classes, `UPPER_SNAKE` for module constants. A single leading
  underscore marks a name module-private.
- Do not uppercase acronyms the way Go does. PEP 8 wins here.
  `HTTPClient` as a class is fine, but `url` and `id` stay
  lowercase.

### Errors

- Raise exceptions. Do not return a sentinel value to signal
  failure. A function that returns `None` or `""` or `-1` to mean
  "it did not work" forces every caller to remember the rule, the
  same failure mode the Go side bans.
- Catch narrowly. A broad `except Exception` belongs only at a
  top-level boundary where you log and carry on, the way the
  feedback post in the example swallows a network error so one bad
  call does not abort the run. A bare `except:` is never correct.
- Let an exception propagate to the layer that can act on it. Do not
  wrap and re-raise just to attach a string the traceback already
  carries. When you do re-raise with new context, use
  `raise ... from err` so the cause survives.

### Comments and prose

The "Writing prose" and "Writing comments" rules apply to Python as
well. No em-dashes, no semicolons, default to no comment, and comment
only the non-obvious why.

- Open each module with a one-paragraph docstring. The run scripts
  put the invocation and the environment variables there so the file
  is its own usage message.
- A class or function docstring earns its place only when the
  contract is non-obvious. The bar is the same as a Go doc comment.

### Don't reinvent the wheel

Before writing any non-trivial logic, look for something already
built. Search the standard library first, then the deps already in
`pyproject.toml`, then PyPI. A mature package is almost always more
correct and better tested than a version written under deadline, and
every line not written is a line nobody has to review, test, or
maintain. Hand-rolling what a library already solves does not just
cost the lines, it adds a design of our own that we now own forever.

Judge a candidate dependency by its maintenance and adoption. Recent
releases, responsive maintainers, wide use in serious projects, and a
focused scope are the signals that matter. Stars and download counts
are hints, not verdicts. A good dependency is welcome. Reinvent only
when nothing fits or the dependency would weigh far more than the
problem it solves.

### Talking to Orla

Orla speaks the OpenAI wire protocol, so an example talks to it with
the plain `openai` client. Point `base_url` at Orla, tag every call
with the `X-Orla-Stage` header, and keep the returned completion id
so the script can post feedback against it.

## Database and storage

### Migrations

Goose with embedded `.sql` files under `internal/storage/migrations`.
Files are `NNNN_description.sql` with `-- +goose Up` and
`-- +goose Down` sections.

The v2 init lives in `0001_init.sql`. Add new changes as new files
(`0002_*.sql`, `0003_*.sql`, …). Do not edit prior migrations once
they have been deployed.

### sqlc

`sqlc.yaml` configures generation. Query files live in
`internal/storage/queries/`. Generated code lives in
`internal/storage/db/`.

Workflow:

1. Edit the migration file with the schema change.
2. Edit the query file in `internal/storage/queries/`.
3. Run `just sqlc` to regenerate.
4. Update Go callers to use the new generated types.

Never edit `internal/storage/db/*.go` by hand. They are regenerated
from the queries and will lose your changes.

### Write strategy

- **Control plane** (stage records, backend records): synchronous,
  return after the row is durable.
- **Data plane** (completion records, feedback): async via
  `storage.BatchWriter[T]`. Buffer drops are counted in a Prometheus
  metric. The producer must not block.

### JSONB columns

Hard rules:

1. **Never write SQL NULL into a JSONB column we own.** The default
   for every JSONB column we declare is `'{}'::jsonb` or
   `'[]'::jsonb`. Go's `json.Marshal(nil)` returns the bytes
   `"null"`, which the column will accept and then downstream
   queries like `tags->>'tenant'` silently return NULL. Use
   `encodeJSONBObject` in `internal/telemetry/completion.go` which
   substitutes `"{}"` for nil and empty maps.
2. **Surface unmarshal errors on the read path.** A malformed JSONB
   cell means schema drift or hand-editing, not "no data". Returning
   nil with no error silently zeroes downstream calculations.
   `internal/backends/registry.go:unmarshalRates` is the pattern.
3. **Distinguish absent, null, and empty for patch requests.** Use
   `*map[K]V`. nil pointer means no change. Pointer to nil or empty
   map means clear. Pointer to populated map means overwrite. See
   `backends.PatchRequest.Rates`.

## Cost reporting

Two channels exist on backends:

- LLM backends price through `input_cost_per_mtoken` and
  `output_cost_per_mtoken` (per million tokens). The proxy computes
  `cost_usd = (prompt_tokens × input + completion_tokens × output) /
  1_000_000` and records it on every completion. An LLM backend whose
  price changes over time can set `cost_source`, a URL the daemon
  polls for the current per-mtoken costs. A live polled price
  overrides the static columns for cost accounting without rewriting
  them. See `internal/costs` and [`docs/proxy.md`](docs/proxy.md).
- Tool backends price through the `rates` JSONB map. Each key is a
  resource name (`gpu_seconds`, `cpu_seconds`, `calls`, …) and the
  value is USD per unit. Tool wrappers report a parallel `usage` map
  on their response. The proxy computes the dot product.

Rules:

- `rates` is rejected at registration time for LLM backends. The API
  returns 400.
- All numeric rate and cost values are validated as finite and
  non-negative. NaN, Inf, and negative values are rejected with 400
  at the API boundary and dropped with a log line at the proxy if a
  tool returns one in a response.
- A tool can short-circuit cost computation by setting
  `cost_usd` on its response. The proxy uses that value verbatim
  after the same finite-non-negative sanity check.
- A tool that reports usage keys that do not match any backend rate
  emits a warning log naming both key sets. Cost is recorded as null,
  not zero, so an operator can tell misconfiguration from a free
  tool.

## Working with the user

### Risk and reversibility

Carefully consider the blast radius of every action. Local, reversible
actions (edit a file, run a test) need no preamble. Hard-to-reverse
actions (force-push, drop database, delete a branch, modify a shared
configuration) need explicit user confirmation each time.

Authorization for a single action does not extend to similar actions.
A `git push` approved once is not blanket approval for every future
push.

### Confirmation patterns

- Lay out a plan before doing destructive multi-step work. Get a
  green light, then execute.
- After every destructive step, summarize the state. The user often
  wants to verify before authorizing the next step.
- When you spot a side-effect the user didn't ask for (cleanup,
  refactor, lint fix), name it and ask before doing it. Do not slip
  it into a commit silently.

### Communication style

- Default to terse. The user reads diffs and can see what changed.
- Lead with the result, then the details if asked. Don't bury the
  headline under a recap of the process.
- One-sentence end-of-turn summary: what shipped and what is next.
  Never longer than two sentences.
- Don't restate the user's request back to them. Don't say "Great
  question" or "Let me help with that". Just answer.
- When you've already done a task, don't describe it in the past
  tense; the diff already documents it.

### Scope

Match the scope of your changes to what the user asked. A bug fix
does not get a free refactor of the surrounding code. A one-shot
script does not need a helper module.

If a side-improvement is genuinely small and obvious (one line,
zero behavior change), do it without ceremony. If it is more than
that, surface it as a separate option for the user to opt into.

## Adding a new feature

A rough order:

1. **Schema first** if the feature touches storage. Add the
   migration. Update `internal/storage/queries/*.sql`. Run `just sqlc`.
2. **Types and interfaces** next. Define the wire types, the patch
   request, the response shape. Keep them in the package that owns
   the domain (`backends`, `telemetry`, `stages`).
3. **Implementation** with both `PostgresRegistry` and `FakeRegistry`
   paths if applicable. They must pass the same contract test.
4. **HTTP handler** wiring with validation at the boundary.
5. **Tests** for each: happy path, every validation branch, one
   integration test that ties storage and handler together.
6. **Documentation** if the feature changes the wire contract or the
   schema. Update `docs/concepts.md`, `docs/proxy.md`, and
   `docs/storage.md` as relevant.
7. **`just check`** until green. Fix everything it surfaces.

## When in doubt

Re-read this document, then the most recent code changes that touched
the same area. The patterns are intentionally consistent across the
codebase. Match them rather than introducing a new variation.
