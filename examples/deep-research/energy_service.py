"""Serves a serving profile's energy draw as an Orla cost source.

A profile is one way of serving one model, covering the GPU, the inference
engine, the quantization, the batch size, and the context length. Energy per
token varies by five to ten times across those choices, so the profile carries
the joules per token figure.

    GET /price/llama-8b-a100-vllm
    {"input_cost_per_mtoken": 0.00108, "output_cost_per_mtoken": 0.0064}

Orla polls `/price` and records the electricity cost of every completion. The
harness reads `/energy` for the joules figures and checks its own arithmetic
against what Orla accounted.

Every number in `profiles.json` is a placeholder carrying `"measured": false`.
Replacing that file with measured joules per token is the whole integration.

Run it with `just energy` (uvicorn on :9300).

Environment: PROFILES (path to the profile table).
"""

from __future__ import annotations

import os
from pathlib import Path

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

PROFILES = Path(os.environ.get("PROFILES", "profiles.json"))

JOULES_PER_KWH = 3.6e6


class Profile(BaseModel):
    """One way of serving one model, and what it draws per token at the GPU."""

    model: str
    gpu: str
    gpu_count: int
    engine: str
    quantization: str
    batch_size: int
    context_tokens: int
    joules_per_prompt_token: float
    joules_per_completion_token: float
    measured: bool


class Table(BaseModel):
    usd_per_kwh: float
    pue: float
    profiles: dict[str, Profile]


class Price(BaseModel):
    """The cost-source contract Orla polls for."""

    input_cost_per_mtoken: float
    output_cost_per_mtoken: float


class Energy(BaseModel):
    """What the harness reports joules from. The two per-token figures include
    datacenter overhead, so they are the profile's values scaled by PUE."""

    profile: str
    joules_per_prompt_token: float
    joules_per_completion_token: float
    usd_per_kwh: float
    pue: float
    measured: bool
    serving: Profile


TABLE = Table.model_validate_json(PROFILES.read_text())

app = FastAPI(title="orla-token-energy-example")


def _profile(name: str) -> Profile:
    if name not in TABLE.profiles:
        known = ", ".join(sorted(TABLE.profiles))
        raise HTTPException(status_code=404, detail=f"unknown profile {name}. known: {known}")
    return TABLE.profiles[name]


def _energy(name: str) -> Energy:
    p = _profile(name)
    return Energy(
        profile=name,
        joules_per_prompt_token=p.joules_per_prompt_token * TABLE.pue,
        joules_per_completion_token=p.joules_per_completion_token * TABLE.pue,
        usd_per_kwh=TABLE.usd_per_kwh,
        pue=TABLE.pue,
        measured=p.measured,
        serving=p,
    )


@app.get("/price/{name}", response_model=Price)
def price(name: str) -> Price:
    e = _energy(name)
    per_joule = TABLE.usd_per_kwh / JOULES_PER_KWH
    return Price(
        input_cost_per_mtoken=e.joules_per_prompt_token * 1e6 * per_joule,
        output_cost_per_mtoken=e.joules_per_completion_token * 1e6 * per_joule,
    )


@app.get("/energy/{name}", response_model=Energy)
def energy(name: str) -> Energy:
    return _energy(name)


@app.get("/profiles")
def profiles() -> dict[str, list[str]]:
    return {"profiles": sorted(TABLE.profiles)}


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}
