"""A stand-in inference backend that replays recorded token counts.

The experiment measures routing, not answer quality, so running a real model
would cost money and add noise without changing what is being measured. This
server speaks enough of the OpenAI chat completions protocol for Orla to
dispatch to it, and reports the token usage the caller asks for rather than
whatever it would have generated.

The caller states the usage to report in the system message, as
`sim:<prompt_tokens>:<completion_tokens>`. Reporting exactly the recorded
counts is what makes the policy arms comparable: every arm does identical
work, so any cost difference between them comes from routing alone.

    uv run sim_backend.py

Environment: PORT (default 9200).
"""

from __future__ import annotations

import os
import time
import uuid

import uvicorn
from fastapi import FastAPI
from pydantic import BaseModel

PORT = int(os.environ.get("PORT", "9200"))


class Message(BaseModel):
    role: str
    content: str | list | None = None


class CompletionRequest(BaseModel):
    model: str = "sim"
    messages: list[Message] = []


def requested_usage(messages: list[Message]) -> tuple[int, int]:
    """Read the `sim:<prompt>:<completion>` directive out of the messages."""
    for message in messages:
        if isinstance(message.content, str) and message.content.startswith("sim:"):
            _, prompt, completion = message.content.split(":")
            return int(prompt), int(completion)
    return 0, 0


app = FastAPI()


@app.post("/v1/chat/completions")
def completions(request: CompletionRequest) -> dict:
    prompt_tokens, completion_tokens = requested_usage(request.messages)
    return {
        "id": f"chatcmpl-{uuid.uuid4().hex[:24]}",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": request.model,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": "{}"},
                "finish_reason": "stop",
            }
        ],
        "usage": {
            "prompt_tokens": prompt_tokens,
            "completion_tokens": completion_tokens,
            "total_tokens": prompt_tokens + completion_tokens,
        },
    }


def main() -> None:
    print(f"simulated backend on http://127.0.0.1:{PORT}")
    uvicorn.run(app, host="127.0.0.1", port=PORT, log_level="warning")


if __name__ == "__main__":
    main()
