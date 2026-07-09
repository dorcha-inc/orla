# Example scheduling service

A tiny external scheduler for Orla. When a worker slot frees on a
backend, Orla POSTs the pending queue to this service and admits the id
it returns. This one is priority-first: the request with the highest
`tags["priority"]` wins, ties broken by the longest wait.

## Run it

```bash
uv run uvicorn app:app --host 127.0.0.1 --port 8090
```

Then point Orla at it with orlactl. This takes effect live, no restart:

```bash
orlactl scheduler policy set --url http://127.0.0.1:8090/v1/schedule/next
```

Revert with `orlactl scheduler policy disable`. With no policy set, or with
the service unreachable, Orla serves first-come-first-served. The service
only ever reorders a queue. It never gates correctness, so a slow or
failing service degrades to first-come-first-served.

## The contract

`POST /v1/schedule/next` receives:

```json
{
  "backend": {"name": "gpt4o", "capacity": 8, "inflight": 8, "queue_depth": 3},
  "pending": [
    {"id": "req-1", "stage": "reply", "model": "gpt-4o",
     "tags": {"priority": "8"}, "enqueued_at": "2026-07-09T12:00:00Z", "wait_ms": 40}
  ]
}
```

and returns the id to admit next:

```json
{"next": "req-1"}
```

A service may instead return a full ranking as `{"order": ["req-1", "req-2"]}`.
Orla uses the first element. See `docs/scheduling-service.md` in the Orla
repo for the normative contract.
