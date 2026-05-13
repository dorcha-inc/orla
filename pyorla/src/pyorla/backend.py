"""Convenience constructors for :class:`~pyorla.types.LLMBackend`.

The provider implementation is selected by the prefix on ``model_id``
(e.g. ``openai:gpt-4o``, ``sglang:Qwen/Qwen3-4B``).
"""

from __future__ import annotations

import random
import string

from pyorla.types import LLMBackend


def _random_backend_name() -> str:
    return "".join(random.choices(string.ascii_lowercase, k=4)) + "-" + "".join(
        random.choices(string.ascii_lowercase, k=4)
    )


def new_vllm_backend(model_id: str, endpoint: str) -> LLMBackend:
    """Create a vLLM backend (OpenAI-compatible API)."""
    return LLMBackend(
        name=_random_backend_name(),
        endpoint=endpoint,
        model_id=f"openai:{model_id}",
    )


def new_sglang_backend(model_id: str, endpoint: str) -> LLMBackend:
    """Create an SGLang backend. The ``sglang:`` prefix triggers the daemon's
    SGLang-specific cache flush wiring."""
    return LLMBackend(
        name=_random_backend_name(),
        endpoint=endpoint,
        model_id=f"sglang:{model_id}",
    )


def new_ollama_backend(model_id: str, endpoint: str) -> LLMBackend:
    """Create an Ollama backend.

    endpoint should be the base Ollama URL (e.g. "http://ollama:11434");
    "/v1" is appended automatically.
    """
    return LLMBackend(
        name=_random_backend_name(),
        endpoint=endpoint.rstrip("/") + "/v1",
        model_id=f"openai:{model_id}",
    )
