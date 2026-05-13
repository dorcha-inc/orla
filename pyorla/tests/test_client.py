"""Tests for pyorla.client."""

import pytest
import httpx

from pyorla.client import OrlaClient, OrlaError, _parse_execute_response, _raise_http
from pyorla.types import InferenceResponseMetrics


def test_orla_client_from_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("ORLA_URL", "http://example:9090")
    c = OrlaClient.from_env()
    assert c.base_url == "http://example:9090"
    c.close()


def test_orla_client_from_env_default(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("ORLA_URL", raising=False)
    c = OrlaClient.from_env("http://localhost:9999")
    assert c.base_url == "http://localhost:9999"
    c.close()


def test_orla_client_default_timeout() -> None:
    c = OrlaClient()
    assert c._sync.timeout == httpx.Timeout(OrlaClient.DEFAULT_TIMEOUT)
    assert c._async.timeout == httpx.Timeout(OrlaClient.DEFAULT_TIMEOUT)
    c.close()


def test_orla_client_custom_timeout() -> None:
    c = OrlaClient(timeout=600)
    assert c._sync.timeout == httpx.Timeout(600)
    assert c._async.timeout == httpx.Timeout(600)
    c.close()


def test_orla_client_set_timeout() -> None:
    c = OrlaClient()
    c.set_timeout(1800)
    assert c._sync.timeout == httpx.Timeout(1800)
    assert c._async.timeout == httpx.Timeout(1800)
    c.close()


def test_orla_client_from_env_timeout(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("ORLA_URL", raising=False)
    c = OrlaClient.from_env(timeout=900)
    assert c._sync.timeout == httpx.Timeout(900)
    c.close()


def test_raise_http_maps_status() -> None:
    req = httpx.Request("GET", "http://test/api/v1/health")
    resp = httpx.Response(502, request=req, text="bad gateway")
    with pytest.raises(OrlaError) as exc_info:
        _raise_http(resp)
    err = exc_info.value
    assert err.status_code == 502
    assert "bad gateway" in (err.body or "")


def test_raise_http_includes_json_error_field() -> None:
    req = httpx.Request("POST", "http://test/api/v1/execute")
    payload = '{"success":false,"error":"inference failed: model not found"}'
    resp = httpx.Response(500, request=req, text=payload)
    with pytest.raises(OrlaError) as exc_info:
        _raise_http(resp)
    err = exc_info.value
    assert err.status_code == 500
    assert "model not found" in str(err)


def test_orla_error_message_str() -> None:
    e = OrlaError("x", status_code=418)
    assert str(e) == "x"
    assert e.status_code == 418


def test_parse_execute_response_metrics_null() -> None:
    """Orla may send ``metrics: null``; client must not assume a dict."""
    data = {
        "success": True,
        "response": {"content": "ok", "thinking": "", "metrics": None},
    }
    r = _parse_execute_response(data)
    assert r.content == "ok"
    assert r.metrics is None


def test_parse_execute_response_metrics_dict() -> None:
    data = {
        "success": True,
        "response": {
            "content": "",
            "metrics": {"ttft_ms": 12, "prompt_tokens": 3, "completion_tokens": 1},
        },
    }
    r = _parse_execute_response(data)
    assert r.metrics == InferenceResponseMetrics(
        ttft_ms=12,
        prompt_tokens=3,
        completion_tokens=1,
    )


def test_parse_execute_response_estimated_cost_usd() -> None:
    data = {
        "success": True,
        "response": {
            "content": "ok",
            "metrics": {"prompt_tokens": 100, "completion_tokens": 50, "estimated_cost_usd": 0.0012},
        },
    }
    r = _parse_execute_response(data)
    assert r.metrics is not None
    assert r.metrics.estimated_cost_usd == 0.0012


def test_parse_execute_response_estimated_cost_null() -> None:
    data = {
        "success": True,
        "response": {
            "content": "ok",
            "metrics": {"prompt_tokens": 100, "completion_tokens": 50},
        },
    }
    r = _parse_execute_response(data)
    assert r.metrics is not None
    assert r.metrics.estimated_cost_usd is None


def test_backend_to_dict_cost_model_and_quality() -> None:
    from pyorla.client import _backend_to_dict
    from pyorla.types import CostModel, LLMBackend

    b = LLMBackend(
        name="test", endpoint="http://x", model_id="openai:m",
        cost_model=CostModel(input_cost_per_mtoken=0.25, output_cost_per_mtoken=1.25),
        quality=0.8,
    )
    d = _backend_to_dict(b)
    assert d["cost_model"] == {"input_cost_per_mtoken": 0.25, "output_cost_per_mtoken": 1.25}
    assert d["quality"] == 0.8


def test_backend_to_dict_no_cost_model() -> None:
    from pyorla.client import _backend_to_dict
    from pyorla.types import LLMBackend

    b = LLMBackend(name="test", endpoint="http://x", model_id="openai:m")
    d = _backend_to_dict(b)
    assert "cost_model" not in d
    assert "quality" not in d


