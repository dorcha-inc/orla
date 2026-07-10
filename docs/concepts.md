# Concepts

Orla is an OpenAI-compatible proxy that sits between agent code and the LLMs or tools that serve it. Its job is to route each call to the right backend right now, record what happened, and let an external mapper change those routings as it learns.

This page explains the model. For an end-to-end walkthrough, see [`quickstart.md`](quickstart.md).

## The runtime adaptation loop

Three components participate. The agent issues OpenAI-compatible calls tagged with a stage, and posts a rating after each call completes. Orla forwards the call to the currently mapped backend and records what happened. A mapper process polls orla, sees the ratings, and updates the stage mapping when a different backend looks better.

The three are loosely coupled. The agent does not know which backend served it. The mapper does not write agent code. Orla holds the shared record between them and serves both sides through HTTP.

## Stages

A stage is a label the developer attaches to a call. It says what this call is for, not where to send it.

A reasonable set of stages for a five-step research agent:

| Stage | What the call is doing |
|---|---|
| `clarify` | Rephrase the user question into something precise |
| `plan` | Decide what evidence to look for |
| `research` | Read the corpus and extract relevant facts |
| `compute` | Do arithmetic or multi-step reasoning |
| `answer` | Synthesize the final response |

The developer chooses the names. Orla auto-creates a stage record on the first call that uses an unfamiliar name, so adding new stages does not require coordinating with whoever runs the platform.

To tag a call:

```
X-Orla-Stage: research
```

Or, for SDKs that cannot set headers easily, in the request body:

```json
{"metadata": {"orla": {"stage": "research"}}}
```

## Backends

A backend is one concrete inference endpoint. It has a name, an OpenAI-compatible URL, a model id, an API key env var, a concurrency limit, and platform-engineer-supplied priors for cost and quality.

A reasonable set of backends for a multi-model deployment:

| Name | Endpoint | What it is |
|---|---|---|
| `gpt-4o` | OpenAI | Frontier model, high cost, high quality |
| `gpt-4o-mini` | OpenAI | Mid-tier, mid-cost |
| `qwen3-next-80b` | Self-hosted | Open-weight model with wide context |
| `gemma-3-12b` | Self-hosted | Open-weight model, cheap |

Backends are registered through the API. Adding or removing one is a single HTTP call and does not require restarting the daemon.

## Mappings

A mapping is the answer to "which backend should serve stage X right now?". It lives in the stage record:

```json
{
  "id": "research",
  "backend": "qwen3-next-80b",
  "reasoning_effort": "",
  "prompt": "",
  "labels": {}
}
```

Set it with `PUT /api/v1/stages/{id}`. Change it with `PATCH /api/v1/stages/{id}`. The next call on that stage uses the new mapping immediately.

Mappings are persistent. Orla rehydrates them from Postgres on startup, so a restart does not lose the routing your mapper learned.

## Stage prompts

The stage record also carries a `prompt`. It is the second lever on how a stage behaves. The mapping decides which model serves the stage. The prompt decides the instructions that model runs under.

When a stage's prompt is non-empty, orla substitutes it for the leading instruction message, the system or developer message the request opens with, on every call tagged with that stage. The rest of the conversation is left alone. The override is opt-in, an empty prompt forwards the agent's own messages untouched. This lets a platform engineer or an optimizer manage a stage's prompt centrally, the same way they manage its backend, and change it without redeploying the agent. See [`prompts.md`](prompts.md).

## Mapping variants and shadow testing

The stage mapping above is the live mapping, the one the critical path uses. A mapping variant is a named alternative: a sparse set of per-stage backend overrides layered on the live mapping. A stage the variant does not mention resolves to its live backend, so a variant that changes one stage is a single override.

A variant lets a mapper try a candidate mapping without disturbing production. The live critical path sends no header. A shadow request sends `X-Orla-Mapping` naming the candidate, so it runs the same workload under the candidate mapping alongside the live traffic. Orla records the variant on each completion, so cost separates by mapping even when the two streams interleave. A mapper that finds a candidate at least as good for less money promotes it by writing the candidate onto the live stage mapping.

Manage variants with `POST /api/v1/mappings` to create or replace one, and `GET` and `DELETE /api/v1/mappings/{name}` to read and remove it. Read cost grouped by variant with `GET /api/v1/costs`.

## Feedback

After a call completes, the agent can rate it:

```json
{
  "completion_id": "chatcmpl-abc",
  "stage_id": "research",
  "rating": 0.85
}
```

`rating` is in `[0, 1]`. What it means is up to you. Common choices:

- LLM-as-judge score against a gold answer
- Downstream task success such as whether the code compiled or the workflow finished
- User explicit thumbs-up or thumbs-down
- A composite score that blends correctness and latency

Multiple feedback rows per completion are allowed. The mapper decides how to aggregate them.

## The mapper

The mapper is your code, not orla's. It reads feedback and metrics through orla's HTTP API and decides what to PATCH. Some choices for the algorithm:

- **Epsilon-greedy bandit.** Cheapest to operate. Works well when the stage's reward surface is roughly stationary.
- **Thompson sampling.** Smoother exploration than epsilon-greedy, useful when reward signal is noisy.
- **Contextual bandit.** Uses request features such as prompt length or tenant.
- **RL with delayed reward.** When grading happens long after the call, off-policy RL is the right shape.

Orla does not ship a mapper. It ships the API surface a mapper needs:

```
GET    /api/v1/stages/{id}/completions    # raw records
GET    /api/v1/stages/{id}/feedback       # raw ratings
GET    /api/v1/stages/{id}/metrics        # per-backend aggregates
PATCH  /api/v1/stages/{id}                # update the mapping
```

A simple loop:

```python
while True:
    for stage_id in stages_to_manage:
        completions = orla.get_completions(stage_id)
        feedback   = orla.get_feedback(stage_id)
        chosen     = bandit.decide(stage_id, completions, feedback)
        if chosen != orla.get_stage(stage_id).backend:
            orla.patch_stage(stage_id, backend=chosen)
    time.sleep(30)
```

That is the entire control loop. The bandit can be twenty lines of Python or a serious RL agent. Orla treats them the same way.

## The two roles

Orla is built around a strict split between two roles.

The developer writes agent logic. They tag each call with a stage and optionally with workflow and tenant tags. They never pick a backend.

The platform engineer registers backends, sets the initial mappings, and runs the mapper that re-maps stages over time. They never edit agent code.

Each role has its own API surface. The developer uses the OpenAI-compatible proxy at `/v1/chat/completions` and submits feedback at `/v1/feedback`. The platform engineer uses the REST CRUD endpoints under `/api/v1/`.

This separation is what makes runtime adaptation safe. Routing changes do not require an agent redeploy. Agent changes do not require a platform-engineer review. The mapping in Postgres is the shared boundary.

## Identity tags

Every call can carry arbitrary tags via headers:

```
X-Orla-Stage: research
X-Orla-Workflow-Run: run-2026-06-04-abc123
X-Orla-Tag-Tenant: acme-corp
X-Orla-Tag-User: alice@acme
```

Tags land in `completion_records.tags_json` and are available to the mapper. They are how you wire up multi-tenant fair-share scheduling, per-customer dashboards, and per-workflow analysis.

## What orla is not

- **Not an agent framework.** Orla does not parse tool calls, run loops, or do orchestration. It serves OpenAI-compatible requests, full stop. Your agent framework stays exactly as it is, whether that is LangGraph, LangChain, the raw OpenAI SDK, or anything custom.
- **Not a model gateway.** Orla does not pick backends based on prompt content, cost thresholds, or token counts. The mapping is the platform engineer's lever. Orla just executes it.
- **Not a retry/fallback chain.** If the resolved backend fails after retries, the call fails. Fallback chains are policy, and policy belongs to the mapper.
- **Not an auth gateway.** Run orla behind nginx, Cloudflare, or a service mesh that handles auth. See [`SECURITY.md`](../SECURITY.md).

## What to read next

- [`quickstart.md`](quickstart.md) for hands-on setup.
- [`proxy.md`](proxy.md) for the exact wire contract.
- [`storage.md`](storage.md) for the Postgres schema your mapper reads.
