# Dynamic costs on Orla

A toy cost service for Orla's `cost_source` feature. A backend
usually carries static per-million-token costs. A backend whose price
changes over time can instead carry a `cost_source` URL that Orla
polls, and this service is the smallest thing that URL can point at.

The service answers every GET with the contract Orla expects:

```json
{"input_cost_per_mtoken": 0.1, "output_cost_per_mtoken": 0.4}
```

The price alternates between an off-peak and a peak level, like
time-of-use electricity pricing. The first half of each period serves
the base price and the second half serves the base price times the
peak multiplier, so Orla's recorded costs visibly move within a couple
of polling rounds.

## Run

```bash
uv run service.py
```

The service reads these environment variables.

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | 9090 | Port the service listens on. |
| `INPUT_COST` | 0.10 | Base input price in dollars per million tokens. |
| `OUTPUT_COST` | 0.40 | Base output price in dollars per million tokens. |
| `PERIOD` | 120 | Seconds in one full off-peak and peak cycle. |
| `PEAK_MULTIPLIER` | 4.0 | How far the peak price rises above the base. |

## Point a backend at it

Register a backend with `--cost-source`, or patch an existing one.
Orla polls the URL and prices every completion with the latest fetched
value. The static cost columns are untouched and take over again if
the source is cleared.

```bash
orlactl backend create --name my-llm \
  --endpoint http://localhost:11434/v1 \
  --model ollama:llama3.2:1b --api-key-env OLLAMA_API_KEY \
  --max-concurrency 2 --cost-source http://127.0.0.1:9090/
```

The polling cadence defaults to 60s and is itself control-plane state,
so you can shorten it while the daemon runs.

```bash
orlactl costs policy set --refresh-interval 15s
```

The [Dynamic Costs tutorial](https://orlaserver.github.io/#/v2/dynamic-costs)
walks through the full loop, including watching the recorded cost of
one stage change as the price cycles.
