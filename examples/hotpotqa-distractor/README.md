# HotpotQA (distractor) on Orla

A multi-hop question-answering agent for [HotpotQA](https://hotpotqa.github.io/),
distractor setting, built as a fixed pipeline of Orla stages. Every model call
goes through Orla's OpenAI-compatible endpoint tagged with a stage, so Orla
routes each stage to a backend and adapts that routing from feedback. The agent
code never changes when the routing does.

## Why a fixed pipeline

In the distractor setting each question ships with ten passages, two relevant and
eight distractors, so there is nothing to retrieve. HotpotQA questions are also
two-hop by construction. The workflow is therefore fixed:

    select  ->  hop  ->  answer

- **select** picks the relevant passages from the ten. Mechanical, so a cheap model fits.
- **hop** reasons across the two hops, returning the reasoning and a draft answer. The hard step.
- **answer** distills the final short answer or yes or no. Cheap again.

Every stage returns a Pydantic-typed structured output (`response_format`), so
parsing is just attribute access. Each stage is independent, so Orla can route and
price them separately and learn which backend serves each one best. The open-domain
`fullwiki` setting, where the agent must retrieve before it can read, is the dynamic
counterpart and a natural second example.

## Prerequisites

A running Orla daemon with at least one backend, and the three stages mapped to
backends. See the [quickstart](../../docs/quickstart.md) to register a backend,
then point each stage at one with `orlactl`:

```bash
for stage in select hop answer; do
  orlactl stage map "$stage" <your-backend-name>
done
```

## Run

```bash
uv run run.py            # the default 10 validation questions
N=200 uv run run.py      # a larger sample
```

The run reads these environment variables.

| Variable | Default | Meaning |
|---|---|---|
| `ORLA_BASE_URL` | `http://localhost:8081/v1` | Orla's OpenAI-compatible endpoint. |
| `ORLA_API` | `http://localhost:8081` | Orla's API root, where feedback is posted. |
| `N` | 10 | How many validation questions to run. |
| `ORLA_MAX_TOKENS` | 2048 | Per-call output cap. Raise it if a reasoning backend comes back empty. |

The script scores each answer's token F1 against the gold answer and posts that
score back to Orla as feedback for every stage that produced the answer. F1 is
already in `[0, 1]`, so it maps straight onto Orla's rating.

## Scope and fidelity

- Answer scoring matches the official HotpotQA scorer: the same normalization,
  token F1, and the yes/no/no-answer exact-match rule, so the EM and F1 here line
  up with `hotpot_evaluate_v1.py`.
- This example reports answer EM/F1 only. The full task also scores
  supporting-fact selection and a joint answer-and-support metric. Predicting the
  supporting sentences would be a natural extra stage, and one more thing for
  Orla to route.
- The select, hop, answer split is a decomposition chosen to give Orla distinct
  stages to route and price. A plain baseline often answers the distractor
  setting in a single chain-of-thought call over all ten passages. The staging
  is for the demo, not a requirement of the benchmark.

## Watch Orla adapt

```bash
# per-backend reward aggregates for a stage (a data-plane read, so use the API)
curl 'http://localhost:8081/api/v1/stages/hop/metrics'

# point a stage at a different backend, no restart and no code change
orlactl stage map hop <other-backend>
```

Re-run and compare the F1 to see what a routing change buys. A mapper that reads
`/metrics` and re-maps the best backend per stage closes the loop automatically.
