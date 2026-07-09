"""A minimal external scheduling service for Orla.

Orla POSTs the pending queue for a backend to /v1/schedule/next whenever
a worker slot frees. This service answers with the id of the request to
admit next. The policy here is priority-first: the request with the
highest tags["priority"] wins, ties broken by the longest wait. Anything
without a priority tag is treated as priority 0.

Run it with `just run` (uvicorn on :8090), point Orla at it with
ORLA_SCHEDULER_POLICY_URL=http://127.0.0.1:8090/v1/schedule/next, and
send competing requests to watch the order change.
"""

from __future__ import annotations

from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI(title="orla-scheduler-example")


class BackendView(BaseModel):
    name: str
    capacity: int
    inflight: int
    queue_depth: int


class PendingRequest(BaseModel):
    id: str
    stage: str = ""
    model: str = ""
    tags: dict[str, str] = {}
    enqueued_at: str = ""
    wait_ms: int = 0


class ScheduleRequest(BaseModel):
    backend: BackendView
    pending: list[PendingRequest]


class ScheduleResponse(BaseModel):
    next: str


def _priority(req: PendingRequest) -> int:
    """Read tags["priority"] as an int, defaulting to 0 when absent or
    unparseable so a missing tag never crashes a decision."""
    raw = req.tags.get("priority", "0")
    try:
        return int(raw)
    except ValueError:
        return 0


@app.post("/v1/schedule/next", response_model=ScheduleResponse)
def schedule_next(body: ScheduleRequest) -> ScheduleResponse:
    # Highest priority first, then the longest wait. Orla only ever calls
    # this with a non-empty pending list.
    best = max(body.pending, key=lambda r: (_priority(r), r.wait_ms))
    return ScheduleResponse(next=best.id)


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}
