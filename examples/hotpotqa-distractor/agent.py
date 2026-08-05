"""A fixed three-stage HotpotQA agent that runs entirely through Orla.

Every model call goes to Orla's OpenAI-compatible endpoint tagged with a stage,
so Orla routes each stage to a backend and can re-map it from feedback. The
pipeline is fixed because the distractor setting hands you the passages and the
questions are two-hop by construction, so there is nothing to retrieve and no
need for a variable-length loop.

    select -> hop -> answer

Each stage returns a Pydantic-typed structured output.
"""

from __future__ import annotations

import os
from dataclasses import dataclass
from typing import TypeVar

from openai import OpenAI
from pydantic import BaseModel

T = TypeVar("T", bound=BaseModel)

Paragraph = tuple[str, list[str]]


@dataclass(frozen=True)
class Call:
    """One model call: which stage made it, what Orla called it, and what it cost
    in tokens. The token counts are what the energy-pricing examples replay."""

    stage: str
    completion_id: str
    prompt_tokens: int
    completion_tokens: int


BASE_URL = os.environ.get("ORLA_BASE_URL", "http://localhost:8081/v1")
# Generous, not tight: reasoning backends spend output tokens before answering, so
# a low cap truncates them to empty. Raise it if a reasoning model comes back empty.
MAX_TOKENS = int(os.environ.get("ORLA_MAX_TOKENS", "2048"))

SELECT_SYSTEM = "You pick the passages needed to answer a multi-hop question."
HOP_SYSTEM = "You answer multi-hop questions by reasoning over the passages one hop at a time."
ANSWER_SYSTEM = "You give the final answer in as few words as possible, or yes or no."


class Selection(BaseModel):
    passages: list[int]


class Hop(BaseModel):
    reasoning: str
    answer: str


class Answer(BaseModel):
    answer: str


def _format(paragraphs: list[Paragraph]) -> str:
    return "\n".join(
        f"[{i}] {title}: {' '.join(sentences)}"
        for i, (title, sentences) in enumerate(paragraphs, 1)
    )


def _selected_text(nums: list[int], paragraphs: list[Paragraph]) -> str:
    nums = [n for n in nums if 1 <= n <= len(paragraphs)]
    chosen = [paragraphs[n - 1] for n in dict.fromkeys(nums)] or paragraphs
    return _format(chosen)


class HotpotAgent:
    """Runs one question through select -> hop -> answer. Holds no per-question
    state, so one instance is safe to share across threads."""

    def __init__(self, base_url: str = BASE_URL) -> None:
        self._client = OpenAI(base_url=base_url, api_key="orla")

    def _ask(self, stage: str, system: str, user: str, schema: type[T]) -> tuple[T | None, Call]:
        resp = self._client.chat.completions.parse(
            model="orla",
            messages=[
                {"role": "system", "content": system},
                {"role": "user", "content": user},
            ],
            extra_headers={"X-Orla-Stage": stage},
            temperature=0,
            max_tokens=MAX_TOKENS,
            response_format=schema,
        )
        usage = resp.usage
        call = Call(
            stage=stage,
            completion_id=resp.id,
            prompt_tokens=usage.prompt_tokens if usage else 0,
            completion_tokens=usage.completion_tokens if usage else 0,
        )
        return resp.choices[0].message.parsed, call

    def answer(self, question: str, paragraphs: list[Paragraph]) -> tuple[str, list[Call]]:
        passages = _format(paragraphs)
        sel, sel_call = self._ask(
            "select",
            SELECT_SYSTEM,
            f"Question: {question}\n\nPassages:\n{passages}\n\nWhich passages are needed?",
            Selection,
        )
        text = _selected_text(sel.passages if sel else [], paragraphs)
        hop, hop_call = self._ask(
            "hop",
            HOP_SYSTEM,
            f"Question: {question}\n\nPassages:\n{text}\n\nReason hop by hop, then answer.",
            Hop,
        )
        final, answer_call = self._ask(
            "answer",
            ANSWER_SYSTEM,
            f"Question: {question}\n\nReasoning:\n{hop.reasoning if hop else ''}\n"
            f"Draft answer: {hop.answer if hop else ''}\n\nGive the final answer.",
            Answer,
        )
        answer = final.answer.strip() if final else ""
        return answer, [sel_call, hop_call, answer_call]
