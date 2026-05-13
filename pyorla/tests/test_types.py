"""Tests for pyorla.types."""

from pyorla.types import (
    CacheHints,
    ExecuteRequest,
    LLMBackend,
    Message,
    SchedulingHints,
    StructuredOutputRequest,
)


def test_llm_backend_defaults():
    b = LLMBackend(name="b1", endpoint="http://x", model_id="m1")
    assert b.max_concurrency == 1
    assert b.queue_capacity == 0
    b.set_max_concurrency(4)
    assert b.max_concurrency == 4
    b.set_queue_capacity(1000)
    assert b.queue_capacity == 1000


def test_execute_request_to_dict_minimal():
    req = ExecuteRequest(backend="b1")
    d = req.to_dict()
    assert d == {"backend": "b1"}


def test_execute_request_to_dict_full():
    req = ExecuteRequest(
        backend="b1",
        stage_id="s1",
        prompt="hello",
        messages=[Message(role="user", content="hi")],
        max_tokens=100,
        temperature=0.5,
        top_p=0.9,
        stream=True,
        scheduling_policy="priority",
        scheduling_hints=SchedulingHints(priority=5),
        workflow_id="wf-123",
        cache_policy="preserve",
        cache_hints=CacheHints(preserve_threshold_tokens=256),
        reasoning_effort="high",
        response_format=StructuredOutputRequest(name="test", schema={"type": "object"}),
    )
    d = req.to_dict()
    assert d["backend"] == "b1"
    assert d["stage_id"] == "s1"
    assert d["prompt"] == "hello"
    assert len(d["messages"]) == 1
    assert d["messages"][0]["role"] == "user"
    assert d["max_tokens"] == 100
    assert d["temperature"] == 0.5
    assert d["top_p"] == 0.9
    assert d["stream"] is True
    assert d["scheduling_policy"] == "priority"
    assert d["scheduling_hints"]["priority"] == 5
    assert d["workflow_id"] == "wf-123"
    assert d["cache_policy"] == "preserve"
    assert d["cache_hints"]["preserve_threshold_tokens"] == 256
    assert d["reasoning_effort"] == "high"
    assert d["response_format"]["name"] == "test"


def test_execute_request_omits_empty():
    req = ExecuteRequest(
        backend="b1",
        scheduling_hints=SchedulingHints(),
        cache_hints=CacheHints(),
    )
    d = req.to_dict()
    assert "scheduling_hints" not in d
    assert "cache_hints" not in d


def test_message_with_tool_calls():
    m = Message(
        role="assistant",
        content="",
        tool_calls=[{"id": "tc1", "name": "foo"}],
    )
    assert m.tool_calls[0]["name"] == "foo"
