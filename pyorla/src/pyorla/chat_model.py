"""ChatOrla — LangChain BaseChatModel backed by an Orla Stage."""

from __future__ import annotations

from collections.abc import Callable, Iterator, Sequence
from typing import Any, cast

from langchain_core.callbacks import CallbackManagerForLLMRun
from langchain_core.language_models.base import LanguageModelInput
from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.messages import AIMessage, AIMessageChunk, BaseMessage
from langchain_core.outputs import ChatGeneration, ChatGenerationChunk, ChatResult
from langchain_core.runnables import Runnable
from langchain_core.tools import BaseTool

from pyorla.client import OrlaClient
from pyorla.langchain_tools import tool_from_langchain
from pyorla.messages import langchain_to_orla, orla_response_to_ai_message
from pyorla.stage import Stage
from pyorla.tools import Tool
from pyorla.types import LLMBackend


class ChatOrla(BaseChatModel):
    """LangChain ``BaseChatModel`` powered by Orla.

    **Tier 1 — simple constructor** (auto-creates client + stage)::

        llm = ChatOrla(base_url="http://localhost:8081", backend="my-backend")

    **Tier 2 — from a Stage** (full control)::

        stage = Stage("triage", backend)
        stage.set_max_tokens(512)
        llm = stage.as_chat_model()
    """

    stage: Stage

    model_config = {"arbitrary_types_allowed": True}

    def __init__(
        self,
        base_url: str | None = None,
        backend: str | None = None,
        *,
        stage: Stage | None = None,
        **kwargs: Any,
    ) -> None:
        # BaseChatModel's static __init__ signature does not list subclass fields like
        # ``stage``; Pydantic still accepts them at runtime. Merge into one mapping so
        # type checkers do not report an unexpected keyword on ``super().__init__``.
        if stage is not None:
            resolved_stage = stage
        else:
            client = OrlaClient(base_url or "http://localhost:8081")
            auto_backend = LLMBackend(
                name=backend or "default",
                endpoint="",
                type="openai",
                model_id=backend or "default",
            )
            auto_stage = Stage("default", auto_backend)
            auto_stage.client = client
            if "temperature" in kwargs:
                auto_stage.set_temperature(kwargs.pop("temperature"))
            if "max_tokens" in kwargs:
                auto_stage.set_max_tokens(kwargs.pop("max_tokens"))
            resolved_stage = auto_stage

        super().__init__(**cast(Any, {"stage": resolved_stage, **kwargs}))

    @property
    def _llm_type(self) -> str:
        return "orla"

    @property
    def _identifying_params(self) -> dict[str, Any]:
        return {
            "base_url": self.stage.client.base_url if self.stage.client else "",
            "backend": self.stage.backend.name if self.stage.backend else "",
            "stage_id": self.stage.id,
        }

    # ------------------------------------------------------------------
    # bind_tools
    # ------------------------------------------------------------------

    def bind_tools(
        self,
        tools: Sequence[dict[str, Any] | type | Callable | BaseTool],
        *,
        tool_choice: str | None = None,
        **kwargs: Any,
    ) -> Runnable[LanguageModelInput, AIMessage]:
        """Bind LangChain-style tools to the underlying Orla Stage.

        ``BaseTool`` instances (including ``@tool``) are converted to executable
        ``pyorla.tools.Tool`` values so ``Stage.run_tool_call`` works. Plain dicts
        with ``name``, ``description``, and ``parameters``/``input_schema`` remain
        schema-only (no ``run``).

        ``tool_choice`` and other kwargs are accepted for API parity with
        ``BaseChatModel``; they are not yet forwarded to the Orla stage.
        """
        _ = (tool_choice, kwargs)
        new_stage = _clone_stage(self.stage)
        for t in tools:
            orla_tool = _langchain_tool_to_orla(t)
            new_stage.add_tool(orla_tool)
        return cast(Runnable[LanguageModelInput, AIMessage], ChatOrla(stage=new_stage))

    # ------------------------------------------------------------------
    # _generate (sync, non-streaming)
    # ------------------------------------------------------------------

    def _generate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> ChatResult:
        orla_msgs = langchain_to_orla(messages)
        req = self.stage.build_request(messages=orla_msgs)
        if self.stage.client is None:
            raise ValueError("ChatOrla: stage has no client attached")
        resp = self.stage.client.execute(req)
        ai_msg = orla_response_to_ai_message(resp)
        return ChatResult(generations=[ChatGeneration(message=ai_msg)])

    # ------------------------------------------------------------------
    # _stream (sync, streaming)
    # ------------------------------------------------------------------

    def _stream(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> Iterator[ChatGenerationChunk]:
        orla_msgs = langchain_to_orla(messages)
        req = self.stage.build_request(messages=orla_msgs)
        if self.stage.client is None:
            raise ValueError("ChatOrla: stage has no client attached")
        for event in self.stage.client.execute_stream(req):
            if event.type == "content":
                chunk = ChatGenerationChunk(
                    message=AIMessageChunk(content=event.content)
                )
                if run_manager:
                    run_manager.on_llm_new_token(event.content, chunk=chunk)
                yield chunk
            elif event.type == "done" and event.response is not None:
                ai_msg = orla_response_to_ai_message(event.response)
                yield ChatGenerationChunk(
                    message=AIMessageChunk(
                        content="",
                        tool_calls=ai_msg.tool_calls,
                        response_metadata=ai_msg.response_metadata,
                    )
                )


# ======================================================================
# Helpers
# ======================================================================


def _clone_stage(src: Stage) -> Stage:
    """Shallow-clone a Stage, preserving client and all settings."""
    s = Stage(src.name, src.backend)
    s.id = src.id
    s.client = src.client
    s.tools = dict(src.tools)
    s.execution_mode = src.execution_mode
    s.max_turns = src.max_turns
    s.prompt = src.prompt
    s.prompt_builder = src.prompt_builder
    s.messages_builder = src.messages_builder
    s.max_tokens = src.max_tokens
    s.temperature = src.temperature
    s.top_p = src.top_p
    s.response_format = src.response_format
    s.chat_template_kwargs = src.chat_template_kwargs
    s.reasoning_effort = src.reasoning_effort
    s.stage_scheduling_policy = src.stage_scheduling_policy
    s.request_scheduling_policy = src.request_scheduling_policy
    s.scheduling_hints = src.scheduling_hints
    s.cache_policy = src.cache_policy
    s.cache_hints = src.cache_hints
    s.stream = src.stream
    s._workflow_id = src._workflow_id
    s.tags = dict(src.tags)
    s.data_labels = list(src.data_labels)
    s.declassifies = list(src.declassifies)
    return s


def _langchain_tool_to_orla(tool: Any) -> Tool:
    """Convert a LangChain tool (BaseTool or dict) to an Orla Tool."""
    if isinstance(tool, dict):
        return Tool(
            name=tool["name"],
            description=tool.get("description", ""),
            input_schema=tool.get("parameters", tool.get("input_schema", {})),
        )
    if isinstance(tool, BaseTool):
        return tool_from_langchain(tool)
    name = getattr(tool, "name", str(tool))
    description = getattr(tool, "description", "")
    schema: dict[str, Any] = {}
    if hasattr(tool, "args_schema") and tool.args_schema is not None:
        if isinstance(tool.args_schema, dict):
            pass
        else:
            schema = tool.args_schema.model_json_schema()
    if not schema and hasattr(tool, "get_input_schema"):
        schema = tool.get_input_schema().model_json_schema()
    return Tool(
        name=name,
        description=description,
        input_schema=schema,
    )
