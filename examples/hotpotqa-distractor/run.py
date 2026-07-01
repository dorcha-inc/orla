"""Run the HotpotQA distractor agent on a sample, score answer F1, and feed each
score back to Orla so it can adapt the per-stage routing.

    uv run run.py            # 10 validation questions (default)
    N=200 uv run run.py      # a larger sample

Environment: ORLA_BASE_URL (default http://localhost:8081/v1), ORLA_API
(default http://localhost:8081), N (sample size).
"""

from __future__ import annotations

import collections
import json
import os
import re
import string
import sys
import urllib.request
from typing import NoReturn

from datasets import load_dataset
from openai import BadRequestError
from pydantic import ValidationError

from agent import HotpotAgent

ORLA_API = os.environ.get("ORLA_API", "http://localhost:8081")


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
        print(f"  feedback failed for {stage}: {type(e).__name__}")


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


def main() -> None:
    n = int(os.environ.get("N", "10"))
    ds = load_dataset("hotpotqa/hotpot_qa", "distractor", split=f"validation[:{n}]")
    agent = HotpotAgent()

    total, em = 0.0, 0
    for ex in ds:
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
        total += score
        em += int(normalize(pred) == normalize(ex["answer"]))
        # Broadcast the task reward to every stage that produced this answer, a
        # simple credit assignment that lets Orla score each stage's backend.
        for stage, cid in calls:
            post_feedback(cid, stage, score)
        print(f"F1 {score:.2f}  pred={pred!r:32.32}  gold={ex['answer']!r}")

    k = max(len(ds), 1)
    print(f"\n{len(ds)} questions  |  EM {em / k:.0%}  |  answer F1 {total / k:.3f}")


if __name__ == "__main__":
    main()
