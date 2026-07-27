# Energy pricing on Orla

Experiments in serving an LLM workload for less electricity. Electricity is
priced by region and by the minute, so the same tokens cost different amounts
depending on where and when they are served. Orla decides where a stage runs, so
it is the layer that can act on that.

The energy literature calls moving work to cheaper power **load shifting**, in
two forms. Moving it in space means routing to a region where power is cheap
right now. Moving it in time means deferring until power is cheap later.

- [spatial-shifting](spatial-shifting/README.md) routes between grid regions at
  the same instant. Orla already does this, so the experiment needs no new
  daemon feature.

Temporal shifting is not here yet. Deferring work by hours cannot be expressed
over a synchronous request that a caller is waiting on, so it needs an
asynchronous submission path that Orla does not have.

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
