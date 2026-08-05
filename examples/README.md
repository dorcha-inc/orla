# Examples

Self-contained agents built on Orla. Each one runs through Orla's
OpenAI-compatible endpoint and tags every call with a stage, so Orla routes each
stage to a backend and optimizes that routing from feedback. The agent code does
not change when the routing does.

- [hotpotqa-distractor](hotpotqa-distractor/README.md): multi-hop QA on HotpotQA,
  a fixed select-hop-answer pipeline.
- [dynamic-costs](dynamic-costs/README.md): a toy cost service Orla polls for
  time-varying backend prices.
- [capture-io](capture-io/README.md): capture each stage's request and response
  and read them back, a retrieve-answer pipeline.
- [mapper_service](mapper_service/README.md): a dynamic stage mapper that
  routes every stage to the cheapest healthy backend.
- [energy-pricing](energy-pricing/README.md): serving an LLM workload for less
  electricity, starting with routing between grid regions by live price.
