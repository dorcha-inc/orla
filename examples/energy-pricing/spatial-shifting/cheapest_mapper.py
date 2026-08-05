"""The stage mapper that routes the dynamic arm to the cheapest region.

Orla POSTs every routing decision here. Stages belonging to the dynamic
arm are answered with the cheapest healthy candidate, priced by the
live regional electricity price Orla itself is holding. Every other
stage belongs to a pinned arm, so the mapper declines and the stage
routes by its static mapping. The decline path is what lets one global
mapper drive the experiment's five arms at once.

Run it with `just mapper` (uvicorn on :8092).
"""

from __future__ import annotations

from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI(title="orla-cheapest-region-mapper")

DYNAMIC_PREFIX = "dynamic-"


class Candidate(BaseModel):
    name: str
    input_cost_per_mtoken: float | None = None
    circuit: str = "closed"


class DecideRequest(BaseModel):
    stage: str
    current: str = ""
    candidates: list[Candidate]


class DecideResponse(BaseModel):
    backend: str


@app.post("/v1/map", response_model=DecideResponse)
def decide(body: DecideRequest) -> DecideResponse:
    if not body.stage.startswith(DYNAMIC_PREFIX):
        return DecideResponse(backend="")
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
