"""Tests for pyorla.chat_model."""

from langchain_core.tools import tool

from pyorla.chat_model import ChatOrla, _langchain_tool_to_orla
from pyorla.stage import Stage
from pyorla.types import LLMBackend


@tool
def _bound_lc_tool(x: str) -> str:
    """Echo."""
    return x


def _backend() -> LLMBackend:
    return LLMBackend(name="be", endpoint="http://x", model_id="m1")


def test_chat_orla_from_stage():
    s = Stage("s1", _backend())
    llm = ChatOrla(stage=s)
    assert llm._llm_type == "orla"
    assert llm.stage is s


def test_chat_orla_simple_constructor():
    llm = ChatOrla(base_url="http://localhost:8081", backend="my-model")
    assert llm._llm_type == "orla"
    assert llm.stage.backend is not None
    assert llm.stage.backend.name == "my-model"
    assert llm.stage.client is not None


def test_chat_orla_bind_tools():
    s = Stage("s1", _backend())
    llm = ChatOrla(stage=s)
    bound = llm.bind_tools([
        {"name": "search", "description": "Search", "parameters": {"type": "object"}},
    ])
    assert isinstance(bound, ChatOrla)
    assert "search" in bound.stage.tools


def test_chat_orla_bind_tools_langchain_has_run():
    s = Stage("s1", _backend())
    llm = ChatOrla(stage=s)
    bound = llm.bind_tools([_bound_lc_tool])
    assert isinstance(bound, ChatOrla)
    t = bound.stage.tools[_bound_lc_tool.name]
    assert t.run is not None
    r = t.run({"x": "hi"})
    assert not r.is_error
    assert r.output_values == {"result": "hi"}


def test_langchain_tool_dict_to_orla():
    t = _langchain_tool_to_orla({
        "name": "foo",
        "description": "does foo",
        "parameters": {"type": "object", "properties": {"x": {"type": "integer"}}},
    })
    assert t.name == "foo"
    assert t.description == "does foo"
    assert t.input_schema["type"] == "object"


def test_identifying_params():
    from pyorla.client import OrlaClient

    s = Stage("s1", _backend())
    s.client = OrlaClient("http://localhost:9999")
    llm = ChatOrla(stage=s)
    params = llm._identifying_params
    assert "localhost:9999" in params["base_url"]
    assert params["backend"] == "be"
