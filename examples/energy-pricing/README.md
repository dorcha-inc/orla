# Energy pricing on Orla

Experiments in serving an LLM workload for less electricity. Electricity is
priced by region and by the minute, so the same tokens cost different amounts
depending on where and when they are served. Orla decides where a stage runs, so
it is the layer that can act on that.

The energy literature calls moving work to cheaper power **load shifting**.
Moving it in space means routing to a region where power is cheap right now,
which is a choice Orla's stage mapper already makes on every request.

- [spatial-shifting](spatial-shifting/README.md) replays a recorded workload
  across four grid regions and compares the electricity bill against staying in
  one region.

## Current results

Replaying 1,560 questions across 13 hours of recorded prices, routing to
whichever region is cheapest at each moment cuts the electricity bill by 13.5%
against Louisiana, the cheapest single region over the window, and by 30%
against New England, the most expensive. Louisiana is only the cheapest in
hindsight. An operator picking one region up front does not know which it will
be, so 13.5% is the saving against a baseline nobody can actually choose. Orla's
own cost accounting produces those totals, and the
[experiment](spatial-shifting/README.md#result) carries the per-region
breakdown.

## The data

`data/hub_lmps_2026-07-17.csv` holds five-minute locational marginal prices for
four trading hubs across three grid operators, covering 13 hours of
2026-07-17. The columns come from [gridstatus](https://github.com/kmax12/gridstatus).

| Region | Hub | Operator |
|---|---|---|
| `caiso-np15` | `TH_NP15_GEN-APND` | CAISO, northern California |
| `isone-hub` | `.H.INTERNAL_HUB` | ISO New England |
| `miso-louisiana` | `LOUISIANA.HUB` | MISO, Louisiana |
| `miso-minnesota` | `MINN.HUB` | MISO, Minnesota |

Two properties of this window are what make the experiments work. Prices differ
across regions at the same instant, by a median of 1.66x and up to 6.15x. And no
region is cheapest for long: Louisiana wins 38% of intervals, California 34%,
Minnesota 28%, and New England never. A single region chosen up front cannot
capture the difference.

The window runs 07:00 to 19:55 UTC, which is midnight to 1pm Pacific. It
captures the overnight trough and the morning but misses the evening peak, when
regional prices usually diverge most, so it likely understates the effect. The
CSV also carries a `GHG` column, so the same machinery can route on carbon
intensity rather than price.

## The workload

`data/workload.jsonl` is the per-call token count of every question in the
HotpotQA distractor validation split, answered through Orla by the
[hotpotqa-distractor](../hotpotqa-distractor/README.md) agent: 7,405 questions,
22,215 calls, 19.2M prompt and 1.24M completion tokens.

Recording it once and replaying it keeps the experiments honest. Every policy
does identical work, so any cost difference between policies comes from routing
and nothing else. It also means the experiments cost nothing to run and need no
model provider.
