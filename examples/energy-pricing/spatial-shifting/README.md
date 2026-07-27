# Spatial shifting: route to where power is cheap

The same model served from four regions costs four different amounts to run,
because the four regions buy electricity at four different prices, and which one
is cheapest changes every few minutes. This experiment routes a recorded LLM
workload to whichever region is cheapest at each moment and compares the
electricity bill against staying in one region.

Every region runs the same model, so answer quality is identical by
construction. The only thing that varies is where the tokens are served.

## Result

Replaying 1,560 questions across 13 hours of recorded prices:

| Policy | Energy cost | Saving |
|---|---|---|
| always `isone-hub` | $0.003225 | |
| always `miso-minnesota` | $0.003137 | |
| always `caiso-np15` | $0.003066 | |
| always `miso-louisiana` | $0.002610 | |
| price-aware routing | $0.002258 | **13.5%** |

The 13.5% is measured against the best region in hindsight, which is the
strongest baseline available. An operator picking a region up front does not
know which one that will be, and picking New England instead would have left 30%
on the table.

The absolute dollars are small because the workload is small and because energy
is a fraction of what inference costs. The percentage is the result, and it
scales with the fleet.

## How it works

Four Orla backends, one per region, all pointing at the same endpoint and
differing only in their `cost_source`. Each source serves that region's price,
so Orla prices every completion at the electricity cost of wherever it ran. The
totals in the table come from Orla's own cost accounting, read back through
`GET /api/v1/stages/{stage}/metrics`.

Three pieces make it runnable in minutes rather than 13 hours:

`price_service.py` replays the recorded prices against a virtual clock. It
converts dollars per megawatt-hour into dollars per million tokens through an
assumed energy draw per token, and `POST /clock` advances every region's price
together. The energy-per-token constants scale all costs linearly and cancel out
of the percentage, so they are declared parameters rather than measurements.

`sim_backend.py` stands in for inference. It reports the token usage the caller
asks for instead of generating anything, which is what makes the policies
comparable: identical work, so cost differences are routing alone.

`experiment.py` walks the price intervals, and at each one re-maps the dynamic
arm's stages to the cheapest region and dispatches that interval's share of the
workload for all five policies. Running the policies together means the wait for
Orla to refresh its prices is paid once per interval instead of once per policy.

## Run it

You need a running Orla daemon with `orlactl` on your PATH, per the
[quickstart](https://orlaserver.github.io/#/v2/quickstart). Start the two
services in their own terminals, then run the experiment.

```bash
just prices     # regional prices on port 9100
just backend    # stand-in inference on port 9200
just run        # the experiment
```

The experiment registers its own backends and stages, so there is nothing to set
up by hand. It waits until Orla is holding a fresh price for every region before
dispatching anything, because a call made before then would be priced at
nothing and would silently understate that policy.

| Variable | Default | Meaning |
|---|---|---|
| `QUESTIONS` | 1560 | How many recorded questions to replay. |
| `STRIDE` | 1 | Price intervals to step by. Raise it for a quicker, coarser run. |
| `CONCURRENCY` | 16 | Parallel dispatches. |
| `TRACE` | `../data/workload.jsonl` | The recorded workload to replay. |
| `ORLA_API` | `http://localhost:8081` | Orla's API root. |
| `PRICE_URL` | `http://127.0.0.1:9100` | Where the price service listens. |
| `SIM_URL` | `http://127.0.0.1:9200/v1` | Where the stand-in backend listens. |

## Scope

Inference is simulated and time is virtual. The routing decisions, the price
signal, and the cost accounting are real, and the claim the experiment supports
is about the routing policy.

Real multi-region routing is also constrained by latency and by data residency
rules, neither of which this model considers. A production router would weigh
those against the saving.
