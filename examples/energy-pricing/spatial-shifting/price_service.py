"""Serves each region's electricity price as an Orla cost source.

One endpoint per grid hub, each answering the contract Orla polls for:

    GET /price/caiso-np15
    {"input_cost_per_mtoken": 0.0021, "output_cost_per_mtoken": 0.021}

Prices come from recorded five-minute locational marginal prices, converted
from dollars per megawatt-hour into dollars per million tokens through an
assumed energy cost per token. The service holds a virtual clock so a run can
replay thirteen hours of price movement in a couple of minutes: POST /clock
advances it to a given interval and every /price response moves with it.

    uv run price_service.py

Environment: PORT (default 9100), DATA (path to the hub CSV),
JOULES_PER_PROMPT_TOKEN, JOULES_PER_COMPLETION_TOKEN.
"""

from __future__ import annotations

import csv
import os
from collections import defaultdict
from pathlib import Path

import uvicorn
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

PORT = int(os.environ.get("PORT", "9100"))
DATA = Path(os.environ.get("DATA", "../data/hub_lmps_2026-07-17.csv"))

# Energy drawn per token, including datacenter overhead. Prefill is far cheaper
# per token than decode because it batches. Both values scale every cost
# linearly and cancel out of the percentage savings, so treat them as declared
# parameters rather than measurements.
JOULES_PER_PROMPT_TOKEN = float(os.environ.get("JOULES_PER_PROMPT_TOKEN", "0.05"))
JOULES_PER_COMPLETION_TOKEN = float(os.environ.get("JOULES_PER_COMPLETION_TOKEN", "0.5"))

JOULES_PER_MWH = 3.6e9

# The hub identifiers in the data are grid operator codes. These are the names
# the experiment registers as Orla backends.
REGIONS = {
    "caiso-np15": "TH_NP15_GEN-APND",
    "isone-hub": ".H.INTERNAL_HUB",
    "miso-louisiana": "LOUISIANA.HUB",
    "miso-minnesota": "MINN.HUB",
}


class Price(BaseModel):
    """The cost-source contract Orla polls for."""

    input_cost_per_mtoken: float
    output_cost_per_mtoken: float


class ClockRequest(BaseModel):
    index: int


class ClockState(BaseModel):
    index: int
    time: str
    size: int


def dollars_per_mtoken(lmp_per_mwh: float, joules_per_token: float) -> float:
    return joules_per_token * 1e6 * lmp_per_mwh / JOULES_PER_MWH


def load_series(path: Path) -> tuple[list[str], dict[str, list[Price]]]:
    """Read the CSV into one price series per region, aligned on the timestamps
    where every region reported."""
    by_time: dict[str, dict[str, float]] = defaultdict(dict)
    with path.open() as f:
        for row in csv.DictReader(f):
            by_time[row["Time"]][row["Location"]] = float(row["LMP"])

    hubs = set(REGIONS.values())
    times = sorted(t for t, prices in by_time.items() if hubs <= prices.keys())
    series = {
        region: [
            Price(
                input_cost_per_mtoken=dollars_per_mtoken(by_time[t][hub], JOULES_PER_PROMPT_TOKEN),
                output_cost_per_mtoken=dollars_per_mtoken(
                    by_time[t][hub], JOULES_PER_COMPLETION_TOKEN
                ),
            )
            for t in times
        ]
        for region, hub in REGIONS.items()
    }
    return times, series


TIMES, SERIES = load_series(DATA)
current_index = 0

app = FastAPI()


@app.get("/price/{region}")
def price(region: str) -> Price:
    if region not in SERIES:
        raise HTTPException(status_code=404, detail=f"unknown region {region}")
    return SERIES[region][current_index]


@app.get("/clock")
def get_clock() -> ClockState:
    return ClockState(index=current_index, time=TIMES[current_index], size=len(TIMES))


@app.post("/clock")
def set_clock(request: ClockRequest) -> ClockState:
    global current_index
    current_index = max(0, min(request.index, len(TIMES) - 1))
    return get_clock()


def main() -> None:
    print(f"price service on http://127.0.0.1:{PORT}, {len(TIMES)} intervals")
    print(f"regions: {', '.join(SERIES)}")
    uvicorn.run(app, host="127.0.0.1", port=PORT, log_level="warning")


if __name__ == "__main__":
    main()
