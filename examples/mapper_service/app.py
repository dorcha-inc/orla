"""A minimal dynamic stage mapper for Orla.

Orla POSTs the routing context for a stage to /v1/map on every
request. This service answers with the backend to serve it. The rule
here is the cheapest healthy backend: among candidates whose
circuit is closed, the one with the lowest input price wins, and
candidates without a price are skipped. When nothing qualifies the
mapper declines with an empty backend, and the stage routes by its
static mapping.

Run it with `just run` (uvicorn on :8091), point Orla at it with
`orlactl stage mapper set --url http://127.0.0.1:8091/v1/map`,
and watch stages follow the prices.
"""

from __future__ import annotations

from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI(title="orla-stage-mapper-example")


class Candidate(BaseModel):
    name: str
    quality: float | None = None
    input_cost_per_mtoken: float | None = None
    output_cost_per_mtoken: float | None = None
    queue_depth: int = 0
    in_flight: int = 0
    capacity: int = 0
    circuit: str = "closed"


class DecideRequest(BaseModel):
    stage: str
    tags: dict[str, str] = {}
    current: str = ""
    candidates: list[Candidate]


class DecideResponse(BaseModel):
    backend: str


@app.post("/v1/map", response_model=DecideResponse)
def decide(body: DecideRequest) -> DecideResponse:
    priced = [
        c for c in body.candidates if c.circuit == "closed" and c.input_cost_per_mtoken is not None
    ]
    if not priced:
        return DecideResponse(backend="")
    cheapest = min(priced, key=lambda c: c.input_cost_per_mtoken or 0.0)
    return DecideResponse(backend=cheapest.name)


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}
