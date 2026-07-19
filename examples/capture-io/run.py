"""Capture the request and response of every stage of one workflow run, then
read the captured content back from Orla and print it per stage.

    uv run run.py

The example is a two-stage agent, retrieve -> answer. The retrieve stage lists
the facts needed to answer a question, and the answer stage writes the answer
from those facts. Both calls carry the same workflow run id, so Orla groups them,
and both are tagged with their stage.

The script turns capture_io on for both stages, runs one question, then reads the
captured input and output back through GET /api/v1/workflows/{run}/completions.
Capture shows exactly what the retrieve stage produced and what the answer stage
received, which is how you tell a retrieval miss from a composer mistake.

Prerequisite: a running Orla daemon with the retrieve and answer stages mapped to
a backend (orlactl stage map retrieve <backend>). Capture is left on afterward,
turn it off with `orlactl stage capture <stage> off`. Environment: ORLA_BASE_URL
(default http://localhost:8081/v1), ORLA_API (default http://localhost:8081),
ORLA_MAX_TOKENS (per-call output cap, default 1024).
"""

from __future__ import annotations

import json
import os
import time
import urllib.error
import urllib.request
import uuid

from openai import OpenAI
from pydantic import BaseModel

BASE_URL = os.environ.get("ORLA_BASE_URL", "http://localhost:8081/v1")
ORLA_API = os.environ.get("ORLA_API", "http://localhost:8081")
MAX_TOKENS = int(os.environ.get("ORLA_MAX_TOKENS", "1024"))

STAGES = ("retrieve", "answer")
QUESTION = "Which planet in our solar system has the most known moons?"

RETRIEVE_SYSTEM = "List the facts needed to answer the question, one per line. Do not answer it."
ANSWER_SYSTEM = "Answer the question in one sentence, using only the facts provided."


class CapturedIO(BaseModel):
    completion_id: str
    stage_id: str
    request_content: str | None = None
    response_content: str | None = None


class Agent:
    """Runs one question through retrieve -> answer. Both calls share a workflow
    run id so Orla groups them, and each is tagged with its stage."""

    def __init__(self, run_id: str, base_url: str = BASE_URL) -> None:
        self._client = OpenAI(base_url=base_url, api_key="orla")
        self._run_id = run_id

    def _ask(self, stage: str, system: str, user: str) -> str:
        resp = self._client.chat.completions.create(
            model="orla",
            messages=[
                {"role": "system", "content": system},
                {"role": "user", "content": user},
            ],
            extra_headers={"X-Orla-Stage": stage, "X-Orla-Workflow-Run": self._run_id},
            temperature=0,
            max_tokens=MAX_TOKENS,
        )
        return resp.choices[0].message.content or ""

    def answer(self, question: str) -> str:
        facts = self._ask("retrieve", RETRIEVE_SYSTEM, question)
        return self._ask("answer", ANSWER_SYSTEM, f"Question: {question}\n\nFacts:\n{facts}")


def _api(method: str, path: str, body: dict[str, object] | None = None) -> bytes:
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(
        f"{ORLA_API}{path}",
        data=data,
        headers={"Content-Type": "application/json"},
        method=method,
    )
    with urllib.request.urlopen(req, timeout=10) as resp:
        return resp.read()


def set_capture(stage: str, on: bool) -> None:
    _api("PATCH", f"/api/v1/stages/{stage}", {"capture_io": on})


def read_captured(run_id: str) -> list[CapturedIO]:
    payload = json.loads(_api("GET", f"/api/v1/workflows/{run_id}/completions"))
    return [CapturedIO.model_validate(c) for c in payload["completions"]]


def _shorten(s: str | None, limit: int = 700) -> str:
    if s is None:
        return "(not captured)"
    s = s.strip()
    if not s:
        return "(empty)"
    return s if len(s) <= limit else s[:limit] + f"... ({len(s)} chars total)"


def main() -> None:
    try:
        for stage in STAGES:
            set_capture(stage, True)
    except urllib.error.HTTPError as e:
        if e.code == 404:
            print("stages not found. map them to a backend first:")
            print("  for s in retrieve answer; do orlactl stage map $s <backend>; done")
            return
        raise
    print(f"capture on for {', '.join(STAGES)}\n")

    run_id = uuid.uuid4().hex
    final = Agent(run_id).answer(QUESTION)
    print(f"question:     {QUESTION}")
    print(f"answer:       {final}")
    print(f"workflow run: {run_id}\n")

    # The capture write is async, so give the batch writer time to flush before
    # reading the run back.
    rows: list[CapturedIO] = []
    for _ in range(20):
        rows = read_captured(run_id)
        if len(rows) >= len(STAGES):
            break
        time.sleep(0.25)

    if not rows:
        print("no captured rows yet. confirm capture is on with `orlactl stage list`.")
        return

    for row in rows:
        print(f"=== {row.stage_id}  (completion {row.completion_id}) ===")
        print(f"  request:  {_shorten(row.request_content)}")
        print(f"  response: {_shorten(row.response_content)}\n")

    print("capture is still on. turn it off with: orlactl stage capture <stage> off")


if __name__ == "__main__":
    main()
