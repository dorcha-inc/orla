"""Stage — primary execution unit for workflows and inference."""

from __future__ import annotations

import random
import string
from collections.abc import Callable, Iterator
from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Any

from pyorla.tools import Tool, ToolCall, ToolResult, tool_call_from_raw
from pyorla.types import (
    CacheHints,
    ExecuteRequest,
    InferenceResponse,
    LLMBackend,
    Message,
    SchedulingHints,
    StreamEvent,
    StructuredOutputRequest,
)

if TYPE_CHECKING:
    from pyorla.chat_model import ChatOrla
    from pyorla.client import OrlaClient


StageResult = dict[str, Any]


StagePromptBuilder = Callable[[dict[str, "StageResultData"]], str]
StageMessagesBuilder = Callable[[dict[str, "StageResultData"]], list[Message]]
StreamHandler = Callable[[StreamEvent], None]


@dataclass
class StageResultData:
    """Wraps the output of a stage execution."""

    response: InferenceResponse | None = None
    messages: list[Message] = field(default_factory=list)


def _random_stage_id() -> str:
    return (
        "".join(random.choices(string.ascii_lowercase, k=4))
        + "-"
        + "".join(random.choices(string.ascii_lowercase, k=4))
    )


EXECUTION_MODE_SINGLE_SHOT = "single_shot"
EXECUTION_MODE_AGENT_LOOP = "agent_loop"


class Stage:
    """Primary execution unit in Orla.

    Each Stage has a globally unique ID and can execute LLM inference
    calls directly through its attached ``OrlaClient``.
    """

    def __init__(self, name: str, backend: LLMBackend | None = None) -> None:
        self.id: str = _random_stage_id()
        self.name: str = name
        self.client: OrlaClient | None = None
        self.backend: LLMBackend | None = backend

        self.tools: dict[str, Tool] = {}
        self.execution_mode: str = EXECUTION_MODE_SINGLE_SHOT
        self.max_turns: int = 0

        self.prompt: str = ""
        self.prompt_builder: StagePromptBuilder | None = None
        self.messages_builder: StageMessagesBuilder | None = None

        self.max_tokens: int | None = None
        self.temperature: float | None = None
        self.top_p: float | None = None
        self.response_format: StructuredOutputRequest | None = None
        self.chat_template_kwargs: dict[str, Any] | None = None
        self.reasoning_effort: str = ""

        self.stage_scheduling_policy: str = ""
        self.request_scheduling_policy: str = ""
        self.scheduling_hints: SchedulingHints | None = None

        self.cache_policy: str = ""
        self.cache_hints: CacheHints | None = None

        self.stream: bool = False
        self.accuracy: float | None = None
        self.accuracy_policy: str = ""

        self._workflow_id: str = ""
        self.tags: dict[str, str] = {}
        self.data_labels: list[str] = []
        self.declassifies: list[str] = []

    # ---- Setters ----

    def set_max_tokens(self, n: int) -> None:
        self.max_tokens = n

    def set_temperature(self, f: float) -> None:
        self.temperature = f

    def set_top_p(self, f: float) -> None:
        self.top_p = f

    def set_response_format(self, r: StructuredOutputRequest) -> None:
        self.response_format = r

    def set_chat_template_kwargs(self, kwargs: dict[str, Any]) -> None:
        self.chat_template_kwargs = kwargs

    def set_reasoning_effort(self, effort: str) -> None:
        self.reasoning_effort = effort

    def set_scheduling_policy(self, policy: str) -> None:
        self.stage_scheduling_policy = policy

    def set_request_scheduling_policy(self, policy: str) -> None:
        self.request_scheduling_policy = policy

    def set_scheduling_hints(self, hints: SchedulingHints) -> None:
        self.scheduling_hints = hints

    def set_execution_mode(self, mode: str) -> None:
        self.execution_mode = mode

    def set_max_turns(self, n: int) -> None:
        self.max_turns = n

    def set_prompt_builder(self, builder: StagePromptBuilder) -> None:
        self.prompt_builder = builder

    def set_messages_builder(self, builder: StageMessagesBuilder) -> None:
        self.messages_builder = builder

    def set_cache_policy(self, policy: str) -> None:
        self.cache_policy = policy

    def set_cache_hints(self, hints: CacheHints) -> None:
        self.cache_hints = hints

    def set_stream(self, enabled: bool) -> None:
        self.stream = enabled

    def set_accuracy(self, score: float) -> None:
        self.accuracy = score

    def set_accuracy_policy(self, policy: str) -> None:
        self.accuracy_policy = policy

    def set_workflow_id(self, wf_id: str) -> None:
        self._workflow_id = wf_id

    def set_tags(self, tags: dict[str, str]) -> None:
        self.tags = tags

    def set_data_labels(self, labels: list[str]) -> None:
        self.data_labels = labels

    def set_declassifies(self, labels: list[str]) -> None:
        self.declassifies = labels

    # ---- Tools ----

    def add_tool(self, tool: Tool) -> None:
        """Add a tool to this stage."""
        self.tools[tool.name] = tool

    # ---- Request building ----

    def build_request(self, prompt: str = "", messages: list[Message] | None = None) -> ExecuteRequest:
        """Build an ExecuteRequest from this stage's configuration."""
        if self.backend is None:
            raise ValueError(f"stage {self.name!r}: backend is nil")
        req = ExecuteRequest(
            backend=self.backend.name,
            stage_id=self.id,
        )
        if messages:
            req.messages = messages
        elif prompt:
            req.prompt = prompt

        req.max_tokens = self.max_tokens
        req.temperature = self.temperature
        req.top_p = self.top_p
        req.response_format = self.response_format
        req.chat_template_kwargs = self.chat_template_kwargs
        req.reasoning_effort = self.reasoning_effort
        req.scheduling_policy = self.stage_scheduling_policy
        req.request_scheduling_policy = self.request_scheduling_policy
        req.scheduling_hints = self.scheduling_hints
        req.cache_policy = self.cache_policy
        req.cache_hints = self.cache_hints
        req.workflow_id = self._workflow_id
        req.accuracy = self.accuracy
        req.accuracy_policy = self.accuracy_policy
        req.tags = self.tags
        req.data_labels = self.data_labels

        if self.tools:
            req.tools = [t.to_mcp() for t in self.tools.values()]

        return req

    # ---- Execution methods ----

    def execute(self, prompt: str) -> InferenceResponse:
        """Run non-streaming inference with a prompt string."""
        if self.client is None:
            raise ValueError(f"stage {self.name!r}: client is nil")
        return self.client.execute(self.build_request(prompt=prompt))

    def execute_with_messages(self, messages: list[Message]) -> InferenceResponse:
        """Run non-streaming inference with a message list."""
        if self.client is None:
            raise ValueError(f"stage {self.name!r}: client is nil")
        return self.client.execute(self.build_request(messages=messages))

    def execute_stream(self, prompt: str) -> Iterator[StreamEvent]:
        """Run streaming inference with a prompt string."""
        if self.client is None:
            raise ValueError(f"stage {self.name!r}: client is nil")
        req = self.build_request(prompt=prompt)
        return self.client.execute_stream(req)

    def execute_stream_with_messages(self, messages: list[Message]) -> Iterator[StreamEvent]:
        """Run streaming inference with a message list."""
        if self.client is None:
            raise ValueError(f"stage {self.name!r}: client is nil")
        req = self.build_request(messages=messages)
        return self.client.execute_stream(req)

    def consume_stream(
        self, stream: Iterator[StreamEvent], handler: StreamHandler | None = None
    ) -> InferenceResponse:
        """Read a stream until 'done', accumulate content/thinking/metrics."""
        response = InferenceResponse(content="")
        for event in stream:
            if handler is not None:
                handler(event)
            if event.type == "content":
                response.content += event.content
            elif event.type == "thinking":
                response.thinking += event.thinking
            elif event.type == "done" and event.response is not None:
                return event.response
        return response

    # ---- Tool execution ----

    def run_tool_call(self, tool_call: ToolCall) -> ToolResult:
        """Run a single tool call against this stage's tools."""
        tool = self.tools.get(tool_call.name)
        if tool is None:
            raise ValueError(f"unknown tool {tool_call.name!r}")
        if tool.run is None:
            raise ValueError(f"tool {tool_call.name!r} has no runner")
        result = tool.run(tool_call.input_arguments)
        result.id = tool_call.id
        result.name = tool_call.name
        return result

    def run_tool_calls_in_response(self, response: InferenceResponse) -> list[ToolResult]:
        """Parse and run all tool calls in an inference response."""
        results: list[ToolResult] = []
        for raw_call in response.tool_calls:
            tc = tool_call_from_raw(raw_call)
            results.append(self.run_tool_call(tc))
        return results

    # ---- LangChain integration ----

    def as_chat_model(self) -> ChatOrla:
        """Create a ``ChatOrla`` (LangChain BaseChatModel) backed by this stage."""
        from pyorla.chat_model import ChatOrla as _ChatOrla

        return _ChatOrla(stage=self)
