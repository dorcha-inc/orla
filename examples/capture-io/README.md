# Capturing stage I/O on Orla

A two-stage agent that shows Orla's per-stage input and output capture. The agent
answers a question with `retrieve -> answer`, where `retrieve` lists the facts a
question needs and `answer` writes the answer from those facts. Every call goes
through Orla's OpenAI-compatible endpoint tagged with its stage, and both calls
share one workflow run id so Orla groups them.

## What capture is for

The metadata Orla records on every call tells you a stage was slow or expensive.
It does not tell you what the stage saw or produced. Capture fills that gap. When
you turn `capture_io` on for a stage, Orla stores the request and response content
of every call tagged with that stage, keyed by completion and grouped by workflow
run. Reading a run back shows exactly what `retrieve` produced and what `answer`
received, which is how you tell a retrieval miss from a composer mistake.

Capture is off by default and stored in a separate table with its own access
control and retention, so turning it on is a deliberate, reversible step.

## Prerequisites

A running Orla daemon with the two stages mapped to a backend. See the
[quickstart](../../docs/quickstart.md) to register a backend, then point each
stage at one:

```bash
for stage in retrieve answer; do
  orlactl stage map "$stage" <your-backend-name>
done
```

## Run

```bash
uv run run.py
```

The script turns `capture_io` on for both stages, runs one question, then reads
the captured content back and prints the request and response for each stage. The
output shows the facts `retrieve` produced and the same facts arriving in the
`answer` stage's request.

Environment variables: `ORLA_BASE_URL` (default `http://localhost:8081/v1`),
`ORLA_API` (default `http://localhost:8081`), and `ORLA_MAX_TOKENS` (per-call
output cap, default 1024).

## Read a run yourself

The script reads the run through the same endpoint you can call directly. Pass the
workflow run id the script printed.

```bash
curl 'http://localhost:8081/api/v1/workflows/<run-id>/completions'
```

The response is one object per captured stage, each with `stage_id`,
`completion_id`, `request_content`, and `response_content`. A stage with capture
off contributes no row, so the endpoint returns only the stages you opted in.

## Turn capture off

Capture stays on until you turn it off, so content keeps accruing for every call
on those stages. Turn it off when you are done inspecting.

```bash
for stage in retrieve answer; do
  orlactl stage capture "$stage" off
done
```

`orlactl stage list` shows which stages currently capture, under the `CAPTURE`
column, so you can confirm nothing is left on.
