# A dynamic stage mapper for Orla

A stage normally routes to the backend set with `orlactl stage map`, a
choice that holds until someone changes it. A dynamic stage mapper
makes the choice per request instead: Orla POSTs the stage, its tags,
and every candidate backend with live prices and queue state, and the
mapper answers with the backend to use. This service is the smallest
useful mapper, the cheapest healthy backend wins.

The wire contract is one endpoint. Orla sends:

```json
{
  "stage": "hop",
  "tags": {"tenant": "acme"},
  "current": "gpt4o",
  "candidates": [
    {"name": "gpt4o", "quality": 0.9, "input_cost_per_mtoken": 2.5,
     "output_cost_per_mtoken": 10.0, "queue_depth": 0, "in_flight": 1,
     "capacity": 8, "circuit": "closed"}
  ]
}
```

The service answers `{"backend": "gpt4o"}`, or `{"backend": ""}` to
decline, which routes the stage by its static mapping. Any timeout or
error also falls back to the static mapping, so this service can crash
without taking requests down with it.

## Run

```bash
just run
```

Point Orla at it, and every stage starts following the prices:

```bash
orlactl stage mapper set --url http://127.0.0.1:8091/v1/map --timeout-ms 50
```

`orlactl stage mapper show` prints the active mapper and
`orlactl stage mapper disable` reverts to static routing. Decisions
are counted by outcome in `orla_stage_mapper_decisions_total` on
Orla's `/metrics`.

The [spatial-shifting](../energy-pricing/spatial-shifting/README.md)
experiment uses the same contract to route a workload between grid
regions by live electricity price.
