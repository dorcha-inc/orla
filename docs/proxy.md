# Proxy: `POST /v1/chat/completions`

The OpenAI-compatible inference entry point. Point any OpenAI-compatible client at orla and add a stage header.

## Wire shape

Request: standard OpenAI chat completion plus identity metadata. See [`concepts.md`](concepts.md#identity-tags) for the full tag list.

Response: standard OpenAI chat completion. The response's `model` field reports the **resolved backend name**, not necessarily what the client sent in its `model` field. Divergence is by design under the developer and platform engineer split documented in [`concepts.md`](concepts.md).

Streaming follows OpenAI's data-only SSE format, terminated by `data: [DONE]`.

## Request handling order

The handler runs these checks in order. Each is a 400 unless noted.

1. **Decode body.** Body too large, over 10 MB, returns 400.
2. **`messages` non-empty.**
3. **Stage extracted.** From `X-Orla-Stage` header, falling back to `metadata.orla.stage` in the body. Missing returns 400.
4. **Resolve backend.** `registry.GetOrCreate(stage)` auto-creates a default stage record on first sighting. If the request carries `X-Orla-Mapping` and that variant overrides this stage, use the variant's backend. Otherwise use `stage.Backend`. If that is empty, fall back to `req.Model`. If neither is set, return 400.
5. **Apply inference policy** from the stage record. This sets `reasoning_effort` and, when the stage has a `prompt`, substitutes it for the leading system message.
6. **Convert messages and tools** to the internal model types.
7. **Dispatch** via `LayerExecute`, then `BackendManager.ScheduleChat`, into the per-backend queue and a worker that calls the openai-go provider.
8. **Encode response** as OpenAI chat completion or stream chunks.

## Auto-create on first sighting

If a developer uses a stage id that orla has never seen, the daemon inserts a default stage record with no backend and the request falls back to `req.Model` for that one call. The platform engineer can later `PUT /api/v1/stages/{id}` to map it.

This means a developer can deploy new agent code without coordinating with the platform engineer. Their requests still flow, and the mapper picks them up on the next pass.

## Mapping variants and shadow testing

A mapping variant is a named, sparse set of per-stage backend overrides layered on the live stage mapping. A request selects a variant with the `X-Orla-Mapping` header, falling back to `metadata.orla.mapping`. When the variant overrides the request's stage, its backend wins. A stage absent from the variant falls through to the stage's live backend, so a candidate that differs from live on one stage is a single-entry variant.

The optimizer uses variants for shadow testing. The live critical path sends no header and resolves the live mapping. A shadow request sends `X-Orla-Mapping` naming a candidate variant, so it runs the same workload under a different mapping without disturbing the live one. Both can stream concurrently because nothing mutates the live mapping. The variant name is recorded on each completion, so cost separates by mapping even when critical and shadow traffic interleave. Manage variants with `POST/GET/DELETE /api/v1/mappings`. See [`storage.md`](storage.md).

## Stage prompt override

A stage can carry a `prompt`. When it is non-empty the proxy substitutes it for the request's leading system message before forwarding. If the first message is a system message its content is replaced. Otherwise a system message is prepended. The rest of the conversation is untouched, so a tool-calling loop keeps its accumulated scratchpad and only the instructions change.

The override is opt-in. A stage with an empty prompt forwards the client's messages verbatim, so an agent that does not want orla managing its prompt is unaffected. The contract is "the stage's prompt is the leading system message," which holds for a single-shot stage like a composer and for a multi-step loop whose every call repeats the same system message. An agent that hides its instructions in a user turn or a tool description should leave the stage prompt empty and apply the prompt itself. See [`prompts.md`](prompts.md).

## Identity tags become completion-record dimensions

Every dispatched request results in one row in `completion_records` with the following columns:

- `completion_id`: UUID assigned by orla
- `stage_id`: from the request
- `workflow_run`: from the request, nullable
- `backend`: the resolved name
- `mapping`: the variant that served the request, empty for the live critical path
- `tags_json`: the full `X-Orla-Tag-*` map
- `prompt_tokens`, `completion_tokens`, `latency_ms`, `cost_usd`, `status`, `created_at`

This is the mapper's primary observation channel. See [`storage.md`](storage.md).

## Streaming semantics

For `stream: true`:

- Orla opens an upstream stream and proxies chunks. Each chunk is rewritten to include the resolved backend in the `model` field.
- The worker holds its concurrency slot until the upstream stream finishes draining, not just until the first chunk arrives. This is load-bearing for the `max_concurrency` invariant.
- On client disconnect, orla drains the upstream stream silently and the worker releases its slot.

## Error shape

All non-200 responses use the OpenAI error envelope:

```json
{
  "error": {
    "message": "...",
    "type": "invalid_request_error" | "permission_denied" | "rate_limit_exceeded" | "server_error" | "api_error"
  }
}
```

Status code drives the `type` field. Clients that already handle OpenAI errors handle these correctly.

Some requests fail before dispatch and never reach `completion_records`. These are counted in `orla_scheduler_rejections_total{backend, reason}`. The `reason` label is one of:

- `unknown_backend`: the resolved backend is not registered. Returns 502.
- `wrong_kind`: the backend exists but is the wrong kind for the route, an LLM asked to serve a tool or the reverse. Returns 500.
- `circuit_open`: the backend's circuit breaker is open and is failing fast. Returns 503 with `Retry-After`.
- `canceled`: the client canceled while the request was queued. Returns 408.
- `deadline_exceeded`: the request deadline passed while the request was queued. Returns 408.
- `internal_error`: any other acquire failure. Returns 500.

The `chat/completions` route falls back to the client-supplied `model` field when a stage has no backend mapping, so an unknown backend name can be arbitrary client input. To keep the metric from minting unbounded label series, the `unknown_backend` case records a fixed `backend="unregistered"` label. The real name still appears in the error body and logs.

These rejections do not increment `orla_requests_total`, which counts only requests that reach dispatch, so a total error rate must union both counters.
