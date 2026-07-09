# Custom scheduling service

By default the scheduler serves each backend's queue first-come-first-served. You can instead delegate the ordering decision to an external service. When a worker slot frees on a backend and requests are queued, Orla asks the service which queued request to admit next.

The service only reorders a queue. It never decides routing, never admits or rejects a request, and never gates correctness. If the service is slow, unreachable, or returns something Orla cannot use, the request falls back to first-come-first-served. This keeps a broken scheduler from turning into stalled or dropped traffic.

## Enabling it

The scheduling policy is control-plane state, managed live through `orlactl` the same way backends and stages are. It is stored in Postgres, survives restarts, and takes effect on every backend the moment you set it. No daemon restart is needed.

```bash
orlactl scheduler policy set --url http://scheduler:8090/v1/schedule/next --timeout-ms 50
orlactl scheduler policy show
orlactl scheduler policy disable
```

- `set --url` points the scheduler at a service. An empty or invalid URL is rejected. `--timeout-ms` bounds a single decision and defaults to 50.
- `show` prints the active policy.
- `disable` reverts to first-come-first-served.

A fresh deployment starts first-come-first-served, so the feature is opt-in. The `set` and `disable` commands map to `PUT /api/v1/scheduler/policy`, and `show` to `GET`, if you prefer to drive the API directly.

The timeout is small on purpose. An LLM dispatch runs for seconds, so a scheduling decision that takes longer than a few tens of milliseconds is not worth waiting for.

## The request

Orla sends `POST` with a JSON body each time it needs a decision for one backend.

```json
{
  "backend": {
    "name": "gpt4o",
    "capacity": 8,
    "inflight": 8,
    "queue_depth": 3
  },
  "pending": [
    {
      "id": "9f1c...",
      "stage": "reply",
      "model": "gpt-4o",
      "tags": {"tenant": "acme", "priority": "8"},
      "enqueued_at": "2026-07-09T12:00:00.123Z",
      "wait_ms": 42
    }
  ]
}
```

- `backend` is the live state of the backend whose queue is being ordered. `capacity` is its max concurrency, `inflight` is the number of requests currently dispatched, and `queue_depth` is the number waiting.
- `pending` is the set of queued requests, oldest first. `id` is assigned by Orla and is unique within this request. `stage`, `model`, and `tags` are the request metadata Orla threads through from the caller. `enqueued_at` is an RFC 3339 timestamp and `wait_ms` is how long the request has already waited.

## The response

Return the `id` of the request to admit next.

```json
{"next": "9f1c..."}
```

A service that prefers to return a full ranking can send `order` instead. Orla admits the first element and asks again when the next slot frees.

```json
{"order": ["9f1c...", "3ab8...", "7de2..."]}
```

If both are present, `next` wins. If `next` is empty, the first element of `order` is used.

## Fallback

Orla falls back to first-come-first-served, admitting the oldest queued request, when any of the following happens. Each is counted in `orla_scheduler_policy_decisions_total{backend, outcome}` under the matching outcome.

- `fallback_timeout`: the decision exceeded the configured decision timeout.
- `fallback_error`: the service was unreachable, returned a non-200 status, or sent a body Orla could not decode.
- `fallback_invalid`: the returned id was not one of the pending requests.

A successful decision is counted as `ok`. Decision latency is recorded in `orla_scheduler_policy_decision_seconds`.

## Cardinality note

`tags` is caller-supplied and can be high cardinality. A service is free to read it, but should not echo raw tag values back into its own metric labels. Orla does not put request metadata into Prometheus labels for the same reason.

## Example

A runnable priority-first service lives in `examples/scheduler_service`. Point Orla at it with `orlactl scheduler policy set --url http://127.0.0.1:8090/v1/schedule/next`.
