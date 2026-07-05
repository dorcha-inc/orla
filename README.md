<p align="center">
  <img src="share/orla_banner_no_caption.png" alt="Orla" width="500">
</p>

<p align="center">
  <a href="https://goreportcard.com/report/github.com/harvard-cns/orla"><img src="https://img.shields.io/badge/go%20report-A+-brightgreen.svg?style=flat" alt="Go Report Card"></a>
  <a href="https://www.bestpractices.dev/projects/6573"><img src="https://www.bestpractices.dev/projects/6573/badge" alt="OpenSSF Best Practices"></a>
  <a href="https://github.com/harvard-cns/orla/actions/workflows/ci.yml"><img src="https://github.com/harvard-cns/orla/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://codecov.io/gh/harvard-cns/orla"><img src="https://codecov.io/gh/harvard-cns/orla/branch/main/graph/badge.svg" alt="Codecov"></a>
</p>

Orla is a runtime-adaptive execution layer for agentic workflows. Modern agentic applications combine many LLM calls, tool invocations, and heterogeneous backends, and developers usually wire these together by hand.

You point your agent at Orla as an OpenAI-compatible endpoint and tag each call with a stage, what the call is for rather than where to send it. Orla serves the call through the stage's current policy, records what happened, and lets an external optimizer update that policy as it learns. This loop tunes each stage's policy for cost and accuracy from production data, with no changes to your agent's code.

Three components participate. The agent issues stage-tagged calls and rates each result. Orla, the execution layer in the middle, applies each stage's policy to incoming calls and holds the shared record. The optimizer reads that record and revises a stage's policy whenever the data points to a better one, from a simple backend swap to a cheap-first rule that escalates to a stronger model only when a task fails.

<p align="center">
  <a href="https://seas.harvard.edu/"><img src="share/harvard_university_logo.svg" alt="Harvard University" width="360"></a>
</p>

<p align="center">
  Orla is a project of Dr. <a href="https://seas.harvard.edu/person/minlan-yu">Minlan Yu</a>'s lab at <a href="https://seas.harvard.edu/">Harvard SEAS</a>.
</p>

## Contributing

We welcome any and all open-source contributions to orla. Orla is designed to be a community-focused project and runs on individual contributions from amazing people around the world. This [document](CONTRIBUTING.md) provides guidelines and instructions for contributing to the project.

## Getting Started

Install the orla daemon from source:

```bash
go install github.com/harvard-cns/orla/cmd/orla@latest
```

Or build it locally:

```bash
git clone https://github.com/harvard-cns/orla
cd orla
go build -o bin/orla ./cmd/orla
```

For your first stage and a tour of the runtime adaptation loop, see [`docs/quickstart.md`](docs/quickstart.md).

## Citation

If you use Orla for your research, we would greatly appreciate it if you cite our demo [paper](https://arxiv.org/abs/2603.13605).


```
@misc{shahout2026orlalibraryservingllmbased,
      title={Orla: A Library for Serving LLM-Based Multi-Agent Systems}, 
      author={Rana Shahout and Hayder Tirmazi and Minlan Yu and Michael Mitzenmacher},
      year={2026},
      eprint={2603.13605},
      archivePrefix={arXiv},
      primaryClass={cs.AI},
      url={https://arxiv.org/abs/2603.13605}, 
}
```

## Contacting Us

- For technical questions and feature requests, please use [GitHub Issues](https://github.com/harvard-cns/orla/issues)
- For security disclosures, please see [SECURITY.md](SECURITY.md).
