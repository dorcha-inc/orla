# Prompt management

A stage carries two levers on how it behaves. The mapping decides which
backend serves the stage. The prompt decides the instructions that
backend runs under. This document covers the second lever.

## The stage prompt

Every stage record has a `prompt` field. It is the stage's system-prompt
override. Empty is the "not set" state. When it is non-empty, the proxy
substitutes it for the request's leading system message before
forwarding the call.

The point is the same as the mapping. A platform engineer or an
optimizer manages the prompt centrally, in orla, and changes it without
redeploying the agent. The agent ships a sensible default prompt in its
own code. Orla overrides that default when a stage prompt is set, and
leaves it alone when one is not.

## How the override applies

The contract is "the stage's prompt is the leading system message." On a
call tagged with a stage that has a non-empty prompt, orla does one of
two things.

- If the first message is a system message, orla replaces its content
  with the stage prompt.
- If the first message is not a system message, orla prepends a system
  message carrying the prompt.

Everything after the leading system message is untouched. A single-shot
stage like a composer sends one system message and one user message, and
orla swaps the first. A multi-step loop sends the same system message on
every step alongside a growing scratchpad of user, assistant, and tool
messages, and orla swaps that same system message on each step while the
scratchpad survives. The instructions change. The accumulated reasoning
does not.

## Opt in per stage

The override is opt-in and additive. A stage with an empty prompt
forwards the client's messages verbatim, exactly as orla behaved before
the field existed. Nothing changes for an agent that does not want orla
managing its prompt.

Set a stage prompt only when the stage's instructions arrive as a clean
leading system message. That holds for most single-instruction stages
and for tool-calling loops whose framework puts the agent instructions
in the system slot. It does not hold for an agent that folds its
instructions into a user turn or into tool descriptions. For those, leave
the stage prompt empty and let the agent apply the prompt itself. Routing
a backend needs no cooperation from the agent, but replacing a prompt
needs orla to know which message is the prompt, and only the agent knows
that when the layout is unusual.

## Set a prompt

With the `orlactl` CLI, pass the text inline, read it from a file, or
clear it.

```bash
orlactl stage prompt answer "You are a careful medical assistant. Cite sources."
orlactl stage prompt answer --file composer-prompt.md
orlactl stage prompt answer --clear
```

`orlactl stage list` shows which stages carry a prompt.

```bash
orlactl stage list
```

```
STAGE      BACKEND         PROMPT
answer     claude-sonnet-5 set
plan       qwen-05b        -
```

Over the REST API, `PATCH` sets the prompt and leaves the backend and
other fields alone.

```bash
curl -X PATCH localhost:8081/api/v1/stages/answer \
  -H 'content-type: application/json' \
  -d '{"prompt": "You are a careful medical assistant. Cite sources."}'
```

Clear it by patching an empty string.

```bash
curl -X PATCH localhost:8081/api/v1/stages/answer \
  -H 'content-type: application/json' \
  -d '{"prompt": ""}'
```

`PUT /api/v1/stages/{id}` replaces the whole record, so a `PUT` that
omits `prompt` resets it to empty. Use `PATCH` to change the prompt
without disturbing the mapping.

A prompt is capped at 10 MB, a sanity bound well above any model's
context window. A longer one is rejected with a 400 at the API boundary.

## Optimizing a prompt

Prompt optimization follows the same promote-on-the-live-record pattern
as mapping optimization. An optimizer measures a stage's accuracy and
cost from the telemetry stream, proposes a better prompt, and writes it
onto the stage with a `PATCH`. The next call on that stage runs the new
prompt. Because the prompt lives in orla rather than in agent code, an
optimizer can evolve a novel prompt and put it live without a redeploy,
which is what makes the loop dynamic.

## See also

- [`concepts.md`](concepts.md) for stages and mappings.
- [`proxy.md`](proxy.md) for where the override applies in request
  handling.
- [`storage.md`](storage.md) for the `prompt` column.
