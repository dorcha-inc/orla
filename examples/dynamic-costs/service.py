"""A toy cost service that Orla polls for a backend's current price.

The service answers every GET with the cost contract Orla expects:

    {"input_cost_per_mtoken": 0.1, "output_cost_per_mtoken": 0.4}

The price alternates between an off-peak and a peak level so a
watcher can see Orla's recorded costs move. The first half of each
period serves the base price and the second half serves the base
price times the peak multiplier, like time-of-use electricity
pricing.

Run it with `just run` (uvicorn on :9090) and point a backend at it
with `orlactl backend create --cost-source http://127.0.0.1:9090/`.

Environment: INPUT_COST and OUTPUT_COST (base dollars per million
tokens, defaults 0.10 and 0.40), PERIOD (seconds per full off-peak and
peak cycle, default 120), PEAK_MULTIPLIER (default 4.0).
"""

from __future__ import annotations

import os
import time

from fastapi import FastAPI
from pydantic import BaseModel

INPUT_COST = float(os.environ.get("INPUT_COST", "0.10"))
OUTPUT_COST = float(os.environ.get("OUTPUT_COST", "0.40"))
PERIOD = float(os.environ.get("PERIOD", "120"))
PEAK_MULTIPLIER = float(os.environ.get("PEAK_MULTIPLIER", "4.0"))

app = FastAPI(title="orla-cost-source-example")


class Price(BaseModel):
    """The cost-source contract Orla polls for."""

    input_cost_per_mtoken: float
    output_cost_per_mtoken: float


@app.get("/", response_model=Price)
def price() -> Price:
    peak = (time.time() % PERIOD) >= PERIOD / 2
    factor = PEAK_MULTIPLIER if peak else 1.0
    return Price(
        input_cost_per_mtoken=round(INPUT_COST * factor, 6),
        output_cost_per_mtoken=round(OUTPUT_COST * factor, 6),
    )


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}
