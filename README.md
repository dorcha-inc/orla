<p align="center">
  <img src="share/orla_banner_no_caption.png" alt="Orla" width="500">
</p>

<p align="center">
  <a href="https://goreportcard.com/report/github.com/harvard-cns/orla"><img src="https://img.shields.io/badge/go%20report-A+-brightgreen.svg?style=flat" alt="Go Report Card"></a>
  <a href="https://www.bestpractices.dev/projects/6573"><img src="https://www.bestpractices.dev/projects/6573/badge" alt="OpenSSF Best Practices"></a>
  <a href="https://github.com/harvard-cns/orla/actions/workflows/build.yml"><img src="https://github.com/harvard-cns/orla/actions/workflows/build.yml/badge.svg" alt="Build"></a>
  <a href="https://github.com/harvard-cns/orla/actions/workflows/pyorla-ci.yml"><img src="https://github.com/harvard-cns/orla/actions/workflows/pyorla-ci.yml/badge.svg" alt="pyorla CI"></a>
  <a href="https://codecov.io/gh/harvard-cns/orla"><img src="https://codecov.io/gh/harvard-cns/orla/branch/main/graph/badge.svg" alt="Codecov"></a>
  <a href="https://pypi.org/project/pyorla/"><img src="https://img.shields.io/pypi/v/pyorla" alt="pyorla on PyPI"></a>
</p>

Please see our website for the most up to date information.

[The Orla Project Website](https://orlaserver.github.io/)

Orla is a library for building and running LLM-based agentic systems. Modern agentic applications are workflows that combine multiple LLM calls, tool invocations, and heterogeneous infrastructure. Today, developers often stitch these pieces together manually using orchestration code, LLM serving engines, and tool execution logic.

Orla simplifies this process by separating workflow-level decisions from request execution. Developers define workflows as stages, while Orla handles how those stages are mapped to models and backends, scheduled and executed, and coordinated through shared inference state.


The system exposes three core components: a Stage Mapper for heterogeneous model routing, a Workflow Orchestrator for executing and scheduling stages, and a Memory Manager that manages KV cache across workflow stages.

<p align="center">
  <a href="https://seas.harvard.edu/"><img src="share/harvard_university_logo.svg" alt="Harvard University" width="360"></a>
</p>

<p align="center">
  Orla is a project of Dr. <a href="https://seas.harvard.edu/person/minlan-yu">Minlan Yu</a>'s lab at <a href="https://seas.harvard.edu/">Harvard SEAS</a>.
</p>

## Contributing

We welcome any and all open-source contributions to orla. Orla is designed to be a community-focused project and runs on individual contributions from amazing people around the world. This [document](CONTRIBUTING.md) provides guidelines and instructions for contributing to the project.

## Getting Started

Installing the orla daemon:

```bash
brew install --cask harvard-cns/orla/orla
```

Installing the orla client SDK:

```bash
pip install pyorla
```

Visit our [website](https://orlaserver.github.io) to learn more.

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

