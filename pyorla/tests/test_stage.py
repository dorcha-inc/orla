"""Tests for pyorla.stage."""

from pyorla.stage import Stage
from pyorla.tools import Tool
from pyorla.types import (
    CacheHints,
    LLMBackend,
    SchedulingHints,
    StructuredOutputRequest,
)


def _backend() -> LLMBackend:
    return LLMBackend(name="test-be", endpoint="http://x", model_id="m1")


def test_stage_defaults():
    s = Stage("my-stage", _backend())
    assert s.name == "my-stage"
    assert s.id  # auto-generated
    assert s.backend is not None
    assert s.execution_mode == "single_shot"


def test_stage_setters():
    s = Stage("s1", _backend())
    s.set_max_tokens(256)
    s.set_temperature(0.7)
    s.set_top_p(0.95)
    s.set_reasoning_effort("high")
    s.set_scheduling_policy("priority")
    s.set_request_scheduling_policy("fcfs")
    s.set_scheduling_hints(SchedulingHints(priority=5))
    s.set_cache_policy("preserve")
    s.set_cache_hints(CacheHints(preserve_threshold_tokens=128))
    s.set_execution_mode("agent_loop")
    s.set_max_turns(10)
    s.set_stream(True)
    s.set_response_format(StructuredOutputRequest(name="out", schema={}))
    s.set_chat_template_kwargs({"enable_thinking": False})

    assert s.max_tokens == 256
    assert s.temperature == 0.7
    assert s.top_p == 0.95
    assert s.reasoning_effort == "high"
    assert s.stage_scheduling_policy == "priority"
    assert s.request_scheduling_policy == "fcfs"
    assert s.scheduling_hints is not None
    assert s.scheduling_hints.priority == 5
    assert s.cache_policy == "preserve"
    assert s.cache_hints is not None
    assert s.execution_mode == "agent_loop"
    assert s.max_turns == 10
    assert s.stream is True


def test_add_tool():
    s = Stage("s1", _backend())
    t = Tool(name="greet", description="says hi", input_schema={})
    s.add_tool(t)
    assert "greet" in s.tools


def test_build_request_with_prompt():
    s = Stage("s1", _backend())
    s.set_max_tokens(100)
    s.set_temperature(0.5)
    req = s.build_request(prompt="hello")
    assert req.backend == "test-be"
    assert req.stage_id == s.id
    assert req.prompt == "hello"
    assert req.max_tokens == 100
    assert req.temperature == 0.5


def test_build_request_with_messages():
    from pyorla.types import Message

    s = Stage("s1", _backend())
    msgs = [Message(role="user", content="hi")]
    req = s.build_request(messages=msgs)
    assert len(req.messages) == 1
    assert not req.prompt


def test_build_request_includes_tools():
    s = Stage("s1", _backend())
    s.add_tool(Tool(name="foo", description="foo tool", input_schema={"type": "object"}))
    req = s.build_request(prompt="test")
    assert len(req.tools) == 1
    assert req.tools[0]["name"] == "foo"


def test_build_request_no_backend_raises():
    s = Stage("s1")
    try:
        s.build_request(prompt="test")
        assert False, "should have raised"
    except ValueError as e:
        assert "backend" in str(e).lower()


def test_as_chat_model():
    s = Stage("s1", _backend())
    llm = s.as_chat_model()
    assert llm._llm_type == "orla"
    assert llm.stage is s
