"""Run the HotpotQA distractor agent, score answer F1, and feed each score back
to Orla so it can adapt the per-stage routing.

Every question is appended to a JSONL trace holding its score and the token
counts of all three calls. The trace doubles as a resume log, so re-running
after an interruption picks up where it stopped, and as the workload input for
the energy-pricing examples.

    uv run run.py                  # 10 validation questions
    N=all CONCURRENCY=12 uv run run.py   # the full validation split

Environment: ORLA_BASE_URL (default http://localhost:8081/v1), ORLA_API
(default http://localhost:8081), N (sample size or "all"), CONCURRENCY
(parallel questions), TRACE (output path).
"""

from __future__ import annotations

import collections
import json
import os
import re
import string
import sys
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import NoReturn

from datasets import load_dataset
from openai import BadRequestError
from pydantic import BaseModel, ValidationError

from agent import Call, HotpotAgent

ORLA_API = os.environ.get("ORLA_API", "http://localhost:8081")
TRACE_PATH = Path(os.environ.get("TRACE", "trace.jsonl"))


class TraceRecord(BaseModel):
    """One answered question. The file of these is the benchmark result, the
    resume log, and the workload the energy-pricing examples replay."""

    id: str
    question: str
    gold: str
    pred: str
    f1: float
    em: int
    calls: list[Call]


def normalize(s: str) -> str:
    s = s.lower()
    s = "".join(ch for ch in s if ch not in string.punctuation)
    s = re.sub(r"\b(a|an|the)\b", " ", s)
    return " ".join(s.split())


# Answer F1 from the official HotpotQA scorer (hotpot_evaluate_v1.py): SQuAD-style
# token overlap, with no partial credit for yes/no/no-answer labels.
def f1(pred: str, gold: str) -> float:
    np_, ng = normalize(pred), normalize(gold)
    if np_ in {"yes", "no", "noanswer"} or ng in {"yes", "no", "noanswer"}:
        return float(np_ == ng)
    p, g = np_.split(), ng.split()
    if not p or not g:
        return float(p == g)
    same = sum((collections.Counter(p) & collections.Counter(g)).values())
    if same == 0:
        return 0.0
    precision, recall = same / len(p), same / len(g)
    return 2 * precision * recall / (precision + recall)


def post_feedback(completion_id: str, stage: str, rating: float) -> None:
    body = json.dumps(
        {"completion_id": completion_id, "stage_id": stage, "rating": rating}
    ).encode()
    req = urllib.request.Request(
        f"{ORLA_API}/v1/feedback",
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        urllib.request.urlopen(req, timeout=10).close()
    except Exception as e:
        print(f"  feedback failed for {stage}: {type(e).__name__}", file=sys.stderr)


def _structured_output_error() -> NoReturn:
    print(
        "\nstructured output is not reaching the backend, so the response_format "
        "schema is not being enforced.\n"
        "  - update and restart the Orla daemon. an older daemon drops "
        "response_format before it reaches the backend.\n"
        "  - confirm the mapped backend supports structured outputs "
        "(orlactl stage get select).",
        file=sys.stderr,
    )
    raise SystemExit(1)


def read_trace(path: Path) -> list[TraceRecord]:
    """Every valid record in the trace. A partial last line from an interrupted
    run is skipped rather than failing the read."""
    if not path.exists():
        return []
    records = []
    for line in path.read_text().splitlines():
        try:
            records.append(TraceRecord.model_validate_json(line))
        except ValidationError:
            continue
    return records


def answer_one(agent: HotpotAgent, ex: dict) -> TraceRecord:
    paragraphs = list(zip(ex["context"]["title"], ex["context"]["sentences"], strict=True))
    try:
        pred, calls = agent.answer(ex["question"], paragraphs)
    except ValidationError:
        _structured_output_error()
    except BadRequestError as e:
        if "response_format" in str(e) or "json_schema" in str(e):
            _structured_output_error()
        raise

    score = f1(pred, ex["answer"])
    # Broadcast the task reward to every stage that produced this answer, a
    # simple credit assignment that lets Orla score each stage's backend.
    for call in calls:
        post_feedback(call.completion_id, call.stage, score)

    return TraceRecord(
        id=ex["id"],
        question=ex["question"],
        gold=ex["answer"],
        pred=pred,
        f1=score,
        em=int(normalize(pred) == normalize(ex["answer"])),
        calls=calls,
    )


def main() -> None:
    n = os.environ.get("N", "10")
    split = "validation" if n == "all" else f"validation[:{int(n)}]"
    concurrency = int(os.environ.get("CONCURRENCY", "4"))

    ds = load_dataset("hotpotqa/hotpot_qa", "distractor", split=split)
    done = {r.id for r in read_trace(TRACE_PATH)}
    todo = [ex for ex in ds if ex["id"] not in done]
    if done:
        print(f"resuming: {len(done)} already in {TRACE_PATH}, {len(todo)} to go")

    agent = HotpotAgent()
    failures: collections.Counter[str] = collections.Counter()
    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = [pool.submit(answer_one, agent, ex) for ex in todo]
        with TRACE_PATH.open("a") as f:
            for i, future in enumerate(as_completed(futures), 1):
                # One question failing upstream must not end the run. The
                # question stays out of the trace, so a later resume retries it.
                try:
                    record = future.result()
                except Exception as e:
                    failures[type(e).__name__] += 1
                    continue
                f.write(record.model_dump_json() + "\n")
                f.flush()
                if i % 50 == 0:
                    print(f"  {i}/{len(todo)} answered", flush=True)

    if failures:
        detail = ", ".join(f"{n}x {name}" for name, n in failures.most_common())
        print(f"\n{sum(failures.values())} questions failed ({detail}); re-run to retry them")
    report(TRACE_PATH)


def report(path: Path) -> None:
    """Score and token totals over everything in the trace."""
    records = read_trace(path)
    if not records:
        print("no results")
        return
    n = len(records)
    em = sum(r.em for r in records) / n
    answer_f1 = sum(r.f1 for r in records) / n
    calls = [c for r in records for c in r.calls]
    prompt = sum(c.prompt_tokens for c in calls)
    completion = sum(c.completion_tokens for c in calls)
    print(f"\n{n} questions  |  EM {em:.1%}  |  answer F1 {answer_f1:.3f}")
    print(f"{len(calls)} calls  |  {prompt:,} prompt tokens  |  {completion:,} completion tokens")


if __name__ == "__main__":
    main()
