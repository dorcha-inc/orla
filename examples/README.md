# Examples

Self-contained agents built on Orla. Each one runs through Orla's
OpenAI-compatible endpoint and tags every call with a stage, so Orla routes each
stage to a backend and optimizes that routing from feedback. The agent code does
not change when the routing does.

- [hotpotqa-distractor](hotpotqa-distractor/README.md): multi-hop QA on HotpotQA,
  a fixed select-hop-answer pipeline.
- [capture-io](capture-io/README.md): capture each stage's request and response
  and read them back, a retrieve-answer pipeline.
