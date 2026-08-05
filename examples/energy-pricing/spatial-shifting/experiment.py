"""Route a recorded workload between grid regions and compare the energy bill.

Four regions serve the same model at the same moment for different electricity
prices, and which one is cheapest changes through the day. This replays a
recorded HotpotQA workload through Orla five times over: once pinned to each
region, and once following whichever region is cheapest right now. Orla prices
every call from the live regional price and reports the totals, so the
comparison comes out of Orla's own cost accounting.

Each policy arm gets its own stages, so all five advance through simulated time
together and the price poll is waited on once per interval rather than once per
arm.

    uv run experiment.py

Environment: ORLA_API (default http://localhost:8081), ORLA_BASE_URL,
PRICE_URL, SIM_URL, MAPPER_URL, TRACE (recorded workload), QUESTIONS
(workload size), STRIDE (price intervals to skip), CONCURRENCY.
"""

from __future__ import annotations

import json
import os
import re
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from pathlib import Path

from openai import OpenAI

ORLA_API = os.environ.get("ORLA_API", "http://localhost:8081")
ORLA_BASE_URL = os.environ.get("ORLA_BASE_URL", "http://localhost:8081/v1")
PRICE_URL = os.environ.get("PRICE_URL", "http://127.0.0.1:9100")
SIM_URL = os.environ.get("SIM_URL", "http://127.0.0.1:9200/v1")
MAPPER_URL = os.environ.get("MAPPER_URL", "http://127.0.0.1:8092/v1/map")
TRACE = Path(os.environ.get("TRACE", "../data/workload.jsonl"))
QUESTIONS = int(os.environ.get("QUESTIONS", "1560"))
STRIDE = int(os.environ.get("STRIDE", "1"))
CONCURRENCY = int(os.environ.get("CONCURRENCY", "16"))

REGIONS = ["caiso-np15", "isone-hub", "miso-louisiana", "miso-minnesota"]
DYNAMIC = "dynamic"
ARMS = [*REGIONS, DYNAMIC]
STAGES = ["select", "hop", "answer"]

# One poll interval plus margin, so the daemon has refreshed every region's
# price before the interval's requests are dispatched.
REFRESH = 1.0
SETTLE = 1.4


def api(method: str, path: str, body: dict | None = None) -> dict:
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(
        f"{ORLA_API}{path}",
        data=data,
        headers={"Content-Type": "application/json"},
        method=method,
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        text = resp.read().decode()
    return json.loads(text) if text else {}


def get_json(url: str) -> dict:
    with urllib.request.urlopen(url, timeout=30) as resp:
        return json.loads(resp.read().decode())


def stage_name(arm: str, stage: str) -> str:
    return f"{arm}-{stage}"


def setup() -> None:
    """Register one backend per region and point every arm's stages at it."""
    for region in REGIONS:
        body = {
            "name": region,
            "endpoint": SIM_URL,
            "model_id": "openai:sim",
            "api_key_env_var": "SIM_API_KEY",
            "max_concurrency": 64,
            "cost_source": f"{PRICE_URL}/price/{region}",
        }
        try:
            api("POST", "/api/v1/backends", body)
        except urllib.error.HTTPError as e:
            if e.code != 409:
                raise
            api("PATCH", f"/api/v1/backends/{region}", {"cost_source": body["cost_source"]})

    for region in REGIONS:
        for stage in STAGES:
            api("PUT", f"/api/v1/stages/{stage_name(region, stage)}", {"backend": region})
    for stage in STAGES:
        api("PUT", f"/api/v1/stages/{stage_name(DYNAMIC, stage)}", {"backend": REGIONS[0]})

    api("PUT", "/api/v1/costs/policy", {"refresh_interval_ms": int(REFRESH * 1000)})
    api("PUT", "/api/v1/stage-mapper", {"url": MAPPER_URL, "timeout_ms": 250})


def load_workload(path: Path, limit: int) -> list[list[dict]]:
    """The recorded per-call token counts, one list of calls per question."""
    questions = []
    for line in path.read_text().splitlines():
        record = json.loads(line)
        questions.append(record["calls"])
        if len(questions) >= limit:
            break
    return questions


AGE_METRIC = re.compile(r'^orla_cost_price_age_seconds\{backend="([^"]+)"\}\s+(\S+)', re.MULTILINE)


def wait_for_live_prices(timeout: float = 120.0) -> None:
    """Block until Orla holds a fresh price for every region.

    The daemon re-reads its refresh interval only when the current one
    elapses, so the first poll after registering backends can be a full
    old interval away. Dispatching before then prices calls at nothing.
    """
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        with urllib.request.urlopen(f"{ORLA_API}/metrics", timeout=30) as resp:
            text = resp.read().decode()
        ages = {m.group(1): float(m.group(2)) for m in AGE_METRIC.finditer(text)}
        if all(ages.get(region, timeout) < REFRESH * 3 for region in REGIONS):
            return
        time.sleep(1.0)
    raise SystemExit(
        "Orla is not holding fresh prices for every region. Check that the price "
        "service is reachable at the cost_source URLs."
    )


def set_clock(index: int) -> None:
    req = urllib.request.Request(
        f"{PRICE_URL}/clock",
        data=json.dumps({"index": index}).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    urllib.request.urlopen(req, timeout=30).close()


def dispatch(client: OpenAI, arm: str, call: dict) -> None:
    client.chat.completions.create(
        model="orla",
        messages=[
            {
                "role": "system",
                "content": f"sim:{call['prompt_tokens']}:{call['completion_tokens']}",
            },
            {"role": "user", "content": "replay"},
        ],
        max_tokens=16,
        extra_headers={"X-Orla-Stage": stage_name(arm, call["stage"])},
    )


def arm_cost(arm: str, since: str) -> float:
    """What this run's calls for one arm cost, from Orla's own accounting.

    A null cost means Orla priced a call with no live price and no static
    columns to fall back on, which would silently understate the arm. Fail
    instead of treating it as free.
    """
    total = 0.0
    query = urllib.parse.urlencode({"since": since})
    for stage in STAGES:
        metrics = api("GET", f"/api/v1/stages/{stage_name(arm, stage)}/metrics?{query}")
        for row in metrics["metrics"]:
            if row["total_cost_usd"] is None:
                raise SystemExit(
                    f"{stage_name(arm, stage)} on {row['backend']}: no cost recorded, "
                    "so Orla held no price for that backend when the calls ran"
                )
            total += row["total_cost_usd"]
    return total


def main() -> None:
    if not TRACE.exists():
        raise SystemExit(
            f"no workload at {TRACE}. Point TRACE at a recorded trace, or run the "
            "hotpotqa-distractor benchmark to record a fresh one."
        )

    setup()
    wait_for_live_prices()
    started = datetime.now(timezone.utc).isoformat()
    intervals = get_json(f"{PRICE_URL}/clock")["size"]
    slots = list(range(0, intervals, STRIDE))
    workload = load_workload(TRACE, QUESTIONS)
    print(f"{len(workload)} questions over {len(slots)} price intervals, {len(ARMS)} arms")

    # Spread the workload evenly across the replayed intervals.
    per_slot: dict[int, list[list[dict]]] = defaultdict(list)
    for i, calls in enumerate(workload):
        per_slot[slots[i % len(slots)]].append(calls)

    client = OpenAI(base_url=ORLA_BASE_URL, api_key="orla", max_retries=3)

    with ThreadPoolExecutor(max_workers=CONCURRENCY) as pool:
        for n, slot in enumerate(slots, 1):
            set_clock(slot)
            time.sleep(SETTLE)

            jobs = [(arm, call) for calls in per_slot[slot] for arm in ARMS for call in calls]
            list(pool.map(lambda job: dispatch(client, *job), jobs))
            if n % 20 == 0:
                print(f"  interval {n}/{len(slots)}", flush=True)

    report(started)


def report(since: str) -> None:
    # Cost records are written asynchronously, so let the batch writer drain.
    time.sleep(3)
    costs = {arm: arm_cost(arm, since) for arm in ARMS}
    baseline = min(costs[r] for r in REGIONS)

    print("\nenergy cost to serve the same workload\n")
    print(f"  {'policy':<24}{'cost (USD)':>14}{'vs best static':>18}")
    for region in REGIONS:
        print(f"  {'always ' + region:<24}{costs[region]:>14.6f}{'':>18}")
    saving = (baseline - costs[DYNAMIC]) / baseline * 100 if baseline else 0.0
    print(f"  {'price-aware routing':<24}{costs[DYNAMIC]:>14.6f}{saving:>17.1f}%")

    print("\n  dynamic arm served by:")
    served: dict[str, int] = defaultdict(int)
    query = urllib.parse.urlencode({"since": since})
    for stage in STAGES:
        metrics = api("GET", f"/api/v1/stages/{stage_name(DYNAMIC, stage)}/metrics?{query}")
        for row in metrics["metrics"]:
            served[row["backend"]] += row["count"]
    total = sum(served.values()) or 1
    for region, count in sorted(served.items(), key=lambda kv: -kv[1]):
        print(f"    {region:<20}{count:>6} calls ({count / total:.0%})")


if __name__ == "__main__":
    main()
