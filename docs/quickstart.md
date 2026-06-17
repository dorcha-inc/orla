# Quickstart

Get from a fresh checkout to a runtime-adaptive agent in about ten minutes.

## Prerequisites

- Go 1.26+
- A running Postgres 14+ instance
- An API key for at least one OpenAI-compatible model provider

## 1. Install the daemon

```bash
go install github.com/harvard-cns/orla/cmd/orla@latest
```

Or from a clone:

```bash
git clone https://github.com/harvard-cns/orla
cd orla
go build -o bin/orla ./cmd/orla
go build -o bin/orlactl ./cmd/orlactl
```

## 2. Start the daemon

Point orla at your Postgres:

```bash
export ORLA_DATABASE_URL="postgres://user:pass@localhost:5432/orla?sslmode=disable"
orla serve
```

You should see structured logs on stderr. The HTTP API listens on `localhost:8081` by default. Override with `ORLA_LISTEN_ADDRESS`.

Health checks:

```bash
curl http://localhost:8081/healthz   # liveness
curl http://localhost:8081/readyz    # readiness, also pings Postgres
```

## 3. Register a backend

A backend is one inference endpoint. Tell orla where it is, which API key env var to use, and how concurrent it can be:

```bash
curl -X POST http://localhost:8081/api/v1/backends \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "gpt-4o",
    "endpoint": "https://api.openai.com/v1",
    "model_id": "openai:gpt-4o",
    "api_key_env_var": "OPENAI_API_KEY",
    "max_concurrency": 8,
    "quality": 0.9,
    "rate_per_second": 10
  }'
```

`api_key_env_var` is the name of the env var orla should read at dispatch time. Orla does not store the key.

Register a second backend the same way. The runtime adaptation story is more interesting with two or more options.

## 4. Map a stage to a backend

A stage is a label your agent attaches to each LLM call. Map the stage to a backend before traffic hits it:

```bash
curl -X PUT http://localhost:8081/api/v1/stages/planning \
  -H 'Content-Type: application/json' \
  -d '{"backend": "gpt-4o"}'
```

If you skip this step, the first request that tags `X-Orla-Stage: planning` will auto-create the stage with no backend, and orla will fall back to the request's `model` field for that call.

## 5. Send a chat completion through orla

Point any OpenAI-compatible client at orla and add one header:

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8081/v1",
    api_key="anything",
)

resp = client.chat.completions.create(
    model="ignored",
    messages=[{"role": "user", "content": "Summarize the second amendment."}],
    extra_headers={"X-Orla-Stage": "planning"},
)
print(resp.choices[0].message.content)
print("resolved backend:", resp.model)
```

`resp.model` reports the backend that handled the call. The request's `model` field is only a fallback for stages that have no mapping.

## 6. Submit feedback

After the agent grades its own output, tell orla how the call went. Grading can be an LLM judge, a downstream task success signal, a user thumbs-up, or anything else.

```bash
curl -X POST http://localhost:8081/v1/feedback \
  -H 'Content-Type: application/json' \
  -d '{
    "completion_id": "chatcmpl-abc",
    "stage_id": "planning",
    "rating": 0.8
  }'
```

`rating` is a number in `[0, 1]`. Higher is better. Anything outside that range is rejected.

The bandit or mapper you wire up reads feedback and re-maps stages.

## 7. Watch the adaptation

Read what orla has seen on this stage:

```bash
# Recent completions, newest first
curl 'http://localhost:8081/api/v1/stages/planning/completions?limit=50'

# Feedback joinable to completions on completion_id
curl 'http://localhost:8081/api/v1/stages/planning/feedback?limit=50'

# Per-backend aggregates ready for a reward function
curl 'http://localhost:8081/api/v1/stages/planning/metrics'
```

When your mapper decides a different backend is better, it PATCHes the stage:

```bash
curl -X PATCH http://localhost:8081/api/v1/stages/planning \
  -H 'Content-Type: application/json' \
  -d '{"backend": "gpt-4o-mini"}'
```

The next request on `planning` goes to the new backend. No restart, no agent code change.

## What to read next

- [`docs/concepts.md`](concepts.md) for the model behind stages, backends, and the feedback loop.
- [`docs/proxy.md`](proxy.md) for the full wire contract on `/v1/chat/completions`.
- [`docs/storage.md`](storage.md) for the Postgres schema your mapper reads from.
