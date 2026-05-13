"""HTTP client for the Orla server."""

from __future__ import annotations

import json
import os
from collections.abc import AsyncIterator, Iterator
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from pyorla.stage import Stage

import httpx

from pyorla.types import (
    AccessPolicy,
    CostModel,
    ExecuteRequest,
    InferenceResponse,
    InferenceResponseMetrics,
    LLMBackend,
    StreamEvent,
)


class OrlaError(Exception):
    """Raised when the Orla server returns an error or an HTTP call fails."""

    def __init__(
        self,
        message: str,
        *,
        status_code: int | None = None,
        body: str | None = None,
        request_id: str | None = None,
    ) -> None:
        super().__init__(message)
        self.status_code = status_code
        self.body = body
        self.request_id = request_id


class OrlaClient:
    """Sync + async HTTP client for the Orla daemon.

    Point *base_url* at a running daemon (e.g. ``http://localhost:8081``), or use
    :meth:`from_env` to read ``ORLA_URL``. To spawn ``orla serve`` on loopback from
    Python, use :func:`pyorla.local_server.orla_runtime` as a context manager.
    """

    DEFAULT_TIMEOUT: float = 300

    def __init__(
        self,
        base_url: str = "http://localhost:8081",
        *,
        timeout: float | None = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        t = timeout if timeout is not None else self.DEFAULT_TIMEOUT
        self._sync = httpx.Client(base_url=self.base_url, timeout=t)
        self._async = httpx.AsyncClient(base_url=self.base_url, timeout=t)

    def set_timeout(self, seconds: float) -> None:
        """Change the HTTP timeout for both sync and async clients."""
        self._sync.timeout = httpx.Timeout(seconds)
        self._async.timeout = httpx.Timeout(seconds)

    @classmethod
    def from_env(
        cls, default: str = "http://localhost:8081", *, timeout: float | None = None
    ) -> OrlaClient:
        """Build a client using ``ORLA_URL`` if set, otherwise *default*."""
        return cls(os.environ.get("ORLA_URL", default), timeout=timeout)

    # ------------------------------------------------------------------
    # Health
    # ------------------------------------------------------------------

    def health(self) -> None:
        """Check Orla daemon health. Raises on failure."""
        resp = self._sync.get("/api/v1/health")
        _raise_http(resp)

    async def ahealth(self) -> None:
        resp = await self._async.get("/api/v1/health")
        await _araise_http(resp)

    # ------------------------------------------------------------------
    # Register backend
    # ------------------------------------------------------------------

    def register_backend(self, backend: LLMBackend) -> None:
        """Register an LLM backend with the daemon."""
        payload = _backend_to_dict(backend)
        resp = self._sync.post("/api/v1/backends", json=payload)
        _raise_http(resp)
        data = resp.json()
        if not data.get("success"):
            raise OrlaError(f"register backend failed: {data.get('error', 'unknown')}")

    async def aregister_backend(self, backend: LLMBackend) -> None:
        payload = _backend_to_dict(backend)
        resp = await self._async.post("/api/v1/backends", json=payload)
        await _araise_http(resp)
        data = resp.json()
        if not data.get("success"):
            raise OrlaError(f"register backend failed: {data.get('error', 'unknown')}")

    # ------------------------------------------------------------------
    # Update backend
    # ------------------------------------------------------------------

    def update_backend(
        self,
        name: str,
        *,
        cost_model: CostModel | None = None,
        quality: float | None = None,
        max_concurrency: int | None = None,
    ) -> None:
        """Live-update a registered backend's mutable fields."""
        payload: dict[str, Any] = {}
        if cost_model is not None:
            payload["cost_model"] = {
                "input_cost_per_mtoken": cost_model.input_cost_per_mtoken,
                "output_cost_per_mtoken": cost_model.output_cost_per_mtoken,
            }
        if quality is not None:
            payload["quality"] = quality
        if max_concurrency is not None:
            payload["max_concurrency"] = max_concurrency
        resp = self._sync.patch(f"/api/v1/backends/{name}", json=payload)
        _raise_http(resp)

    async def aupdate_backend(
        self,
        name: str,
        *,
        cost_model: CostModel | None = None,
        quality: float | None = None,
        max_concurrency: int | None = None,
    ) -> None:
        payload: dict[str, Any] = {}
        if cost_model is not None:
            payload["cost_model"] = {
                "input_cost_per_mtoken": cost_model.input_cost_per_mtoken,
                "output_cost_per_mtoken": cost_model.output_cost_per_mtoken,
            }
        if quality is not None:
            payload["quality"] = quality
        if max_concurrency is not None:
            payload["max_concurrency"] = max_concurrency
        resp = await self._async.patch(f"/api/v1/backends/{name}", json=payload)
        await _araise_http(resp)

    # ------------------------------------------------------------------
    # Access control policies
    # ------------------------------------------------------------------

    def add_policy(self, policy: AccessPolicy) -> None:
        """Install or replace an access control policy."""
        payload = {
            "name": policy.name,
            "subjects": policy.subjects,
            "resources": policy.resources,
            "action": policy.action,
        }
        resp = self._sync.post("/api/v1/policies", json=payload)
        _raise_http(resp)

    async def aadd_policy(self, policy: AccessPolicy) -> None:
        payload = {
            "name": policy.name,
            "subjects": policy.subjects,
            "resources": policy.resources,
            "action": policy.action,
        }
        resp = await self._async.post("/api/v1/policies", json=payload)
        await _araise_http(resp)

    def list_policies(self) -> list[AccessPolicy]:
        """List all access control policies."""
        resp = self._sync.get("/api/v1/policies")
        _raise_http(resp)
        return _parse_policies(resp.json())

    async def alist_policies(self) -> list[AccessPolicy]:
        resp = await self._async.get("/api/v1/policies")
        await _araise_http(resp)
        return _parse_policies(resp.json())

    def remove_policy(self, name: str) -> None:
        """Remove an access control policy by name."""
        resp = self._sync.delete(f"/api/v1/policies/{name}")
        _raise_http(resp)

    async def aremove_policy(self, name: str) -> None:
        resp = await self._async.delete(f"/api/v1/policies/{name}")
        await _araise_http(resp)

    # ------------------------------------------------------------------
    # Access validation
    # ------------------------------------------------------------------

    def check_access(
        self,
        tags: dict[str, str],
        *,
        backend: str = "",
        tools: list[str] | None = None,
        data_labels: list[str] | None = None,
    ) -> tuple[bool, str]:
        """Check access control policies without executing inference.

        Returns (allowed, reason). If allowed is False, reason describes
        the denial.
        """
        payload: dict[str, Any] = {"tags": tags}
        if backend:
            payload["backend"] = backend
        if tools:
            payload["tools"] = tools
        if data_labels:
            payload["data_labels"] = data_labels
        resp = self._sync.post("/api/v1/access/check", json=payload)
        _raise_http(resp)
        data = resp.json()
        return data.get("allowed", False), data.get("reason", "")

    async def acheck_access(
        self,
        tags: dict[str, str],
        *,
        backend: str = "",
        tools: list[str] | None = None,
        data_labels: list[str] | None = None,
    ) -> tuple[bool, str]:
        payload: dict[str, Any] = {"tags": tags}
        if backend:
            payload["backend"] = backend
        if tools:
            payload["tools"] = tools
        if data_labels:
            payload["data_labels"] = data_labels
        resp = await self._async.post("/api/v1/access/check", json=payload)
        await _araise_http(resp)
        data = resp.json()
        return data.get("allowed", False), data.get("reason", "")

    # ------------------------------------------------------------------
    # Workflow DAG registration (for data label propagation)
    # ------------------------------------------------------------------

    def register_workflow(
        self,
        workflow_id: str,
        edges: list[tuple[str, str]],
        declassifications: dict[str, list[str]] | None = None,
    ) -> None:
        """Register a workflow DAG so the daemon propagates data labels along edges.

        *declassifications* maps stage IDs to lists of labels that stage strips
        from propagation. A stage that declassifies a label still inherits it
        (policies apply to its own execution), but downstream stages do not.
        """
        payload: dict[str, Any] = {
            "workflow_id": workflow_id,
            "edges": [{"from": f, "to": t} for f, t in edges],
        }
        if declassifications:
            payload["declassifications"] = [
                {"stage_id": sid, "labels": labels}
                for sid, labels in declassifications.items()
            ]
        resp = self._sync.post("/api/v1/workflows", json=payload)
        _raise_http(resp)

    async def aregister_workflow(
        self,
        workflow_id: str,
        edges: list[tuple[str, str]],
        declassifications: dict[str, list[str]] | None = None,
    ) -> None:
        payload: dict[str, Any] = {
            "workflow_id": workflow_id,
            "edges": [{"from": f, "to": t} for f, t in edges],
        }
        if declassifications:
            payload["declassifications"] = [
                {"stage_id": sid, "labels": labels}
                for sid, labels in declassifications.items()
            ]
        resp = await self._async.post("/api/v1/workflows", json=payload)
        await _araise_http(resp)

    def register_workflow_from_langgraph(
        self,
        workflow_id: str,
        compiled_graph: Any,
        stages: dict[str, "Stage"],
    ) -> None:
        """Register a workflow DAG by extracting edges from a compiled LangGraph.

        *compiled_graph* is a compiled ``langgraph`` graph (the result of
        ``StateGraph(...).compile()``). *stages* maps LangGraph node names to
        Orla ``Stage`` objects. Edges between nodes not in *stages* (e.g.,
        ``__start__``, ``__end__``) are ignored.

        Sets ``workflow_id`` on each stage automatically. If any stage has
        ``declassifies`` set, those are registered with the daemon.
        """
        drawable = compiled_graph.get_graph()
        edges: list[tuple[str, str]] = []
        for e in drawable.edges:
            if e.source in stages and e.target in stages:
                edges.append((stages[e.source].id, stages[e.target].id))
        declass: dict[str, list[str]] = {}
        for stage in stages.values():
            stage.set_workflow_id(workflow_id)
            if stage.declassifies:
                declass[stage.id] = stage.declassifies
        if edges:
            self.register_workflow(
                workflow_id, edges, declassifications=declass or None,
            )

    # ------------------------------------------------------------------
    # Execute (non-streaming)
    # ------------------------------------------------------------------

    def execute(self, req: ExecuteRequest) -> InferenceResponse:
        """Run inference on a named backend."""
        payload = req.to_dict()
        resp = self._sync.post("/api/v1/execute", json=payload)
        _raise_http(resp)
        return _parse_execute_response(resp.json())

    async def aexecute(self, req: ExecuteRequest) -> InferenceResponse:
        payload = req.to_dict()
        resp = await self._async.post("/api/v1/execute", json=payload)
        await _araise_http(resp)
        return _parse_execute_response(resp.json())

    # ------------------------------------------------------------------
    # Execute (streaming)
    # ------------------------------------------------------------------

    def execute_stream(self, req: ExecuteRequest) -> Iterator[StreamEvent]:
        """Run streaming inference."""
        payload = req.to_dict()
        payload["stream"] = True
        with self._sync.stream("POST", "/api/v1/execute", json=payload) as resp:
            _raise_http(resp)
            yield from _iter_sse_events(resp.iter_lines())

    async def aexecute_stream(self, req: ExecuteRequest) -> AsyncIterator[StreamEvent]:
        payload = req.to_dict()
        payload["stream"] = True
        async with self._async.stream("POST", "/api/v1/execute", json=payload) as resp:
            await _araise_http(resp)
            async for line in resp.aiter_lines():
                ev = _parse_sse_line(line, {})
                if ev is not None:
                    yield ev

    # ------------------------------------------------------------------
    # Workflow complete
    # ------------------------------------------------------------------

    def workflow_complete(self, workflow_id: str, backends: list[str]) -> None:
        """Notify the server a workflow has finished."""
        resp = self._sync.post(
            "/api/v1/workflow/complete",
            json={"workflow_id": workflow_id, "backends": backends},
        )
        _raise_http(resp)

    async def aworkflow_complete(self, workflow_id: str, backends: list[str]) -> None:
        resp = await self._async.post(
            "/api/v1/workflow/complete",
            json={"workflow_id": workflow_id, "backends": backends},
        )
        await _araise_http(resp)

    # ------------------------------------------------------------------
    # Cleanup
    # ------------------------------------------------------------------

    def close(self) -> None:
        self._sync.close()

    async def aclose(self) -> None:
        await self._async.aclose()


# ======================================================================
# Helpers
# ======================================================================


def _error_message_from_response(r: httpx.Response) -> str:
    """Build a user-facing message from an error response.

    Tries JSON ``error`` field first (e.g., /api/v1/execute), then falls back
    to the plain-text body (e.g., access control 403s sent via http.Error).
    """
    base = f"HTTP {r.status_code} {r.reason_phrase}"
    try:
        data = r.json()
        if isinstance(data, dict):
            err = data.get("error")
            if isinstance(err, str) and err.strip():
                return f"{base}: {err.strip()}"
    except Exception:
        pass
    # Fall back to plain-text body (access control errors, validation errors).
    text = (r.text or "").strip()
    if text:
        return f"{base}: {text}"
    return base


def _raise_http(resp: httpx.Response) -> None:
    try:
        resp.raise_for_status()
    except httpx.HTTPStatusError as e:
        r = e.response
        text = (r.text or "")[:2048]
        rid = None
        try:
            data = r.json()
            if isinstance(data, dict):
                rid = data.get("request_id") or data.get("requestId")
        except Exception:
            pass
        msg = _error_message_from_response(r)
        raise OrlaError(
            msg,
            status_code=r.status_code,
            body=text or None,
            request_id=rid if isinstance(rid, str) else None,
        ) from e


async def _araise_http(resp: httpx.Response) -> None:
    try:
        resp.raise_for_status()
    except httpx.HTTPStatusError as e:
        r = e.response
        text = (r.text or "")[:2048]
        rid = None
        try:
            data = r.json()
            if isinstance(data, dict):
                rid = data.get("request_id") or data.get("requestId")
        except Exception:
            pass
        msg = _error_message_from_response(r)
        raise OrlaError(
            msg,
            status_code=r.status_code,
            body=text or None,
            request_id=rid if isinstance(rid, str) else None,
        ) from e


def _parse_policies(data: dict) -> list[AccessPolicy]:
    return [
        AccessPolicy(
            name=p["name"],
            subjects=p.get("subjects", []),
            resources=p.get("resources", []),
            action=p.get("action", ""),
        )
        for p in data.get("policies", [])
    ]


def _backend_to_dict(b: LLMBackend) -> dict[str, Any]:
    d: dict[str, Any] = {
        "name": b.name,
        "endpoint": b.endpoint,
        "type": b.type,
        "model_id": b.model_id,
    }
    if b.api_key_env_var:
        d["api_key_env_var"] = b.api_key_env_var
    if b.max_concurrency > 1:
        d["max_concurrency"] = b.max_concurrency
    if b.queue_capacity > 0:
        d["queue_capacity"] = b.queue_capacity
    if b.cost_model is not None:
        d["cost_model"] = {
            "input_cost_per_mtoken": b.cost_model.input_cost_per_mtoken,
            "output_cost_per_mtoken": b.cost_model.output_cost_per_mtoken,
        }
    if b.quality is not None:
        d["quality"] = b.quality
    return d


def _parse_execute_response(data: dict) -> InferenceResponse:
    if not data.get("success"):
        raise OrlaError(f"execution failed: {data.get('error', 'unknown')}")
    r = data.get("response", {})
    metrics = None
    m = r.get("metrics")
    if isinstance(m, dict):
        metrics = InferenceResponseMetrics(
            ttft_ms=m.get("ttft_ms"),
            tpot_ms=m.get("tpot_ms"),
            prompt_tokens=m.get("prompt_tokens", 0),
            completion_tokens=m.get("completion_tokens", 0),
            queue_wait_ms=m.get("queue_wait_ms", 0),
            scheduler_decision_ms=m.get("scheduler_decision_ms", 0),
            dispatch_ms=m.get("dispatch_ms", 0),
            backend_latency_ms=m.get("backend_latency_ms"),
            estimated_cost_usd=m.get("estimated_cost_usd"),
        )
    tool_calls = r.get("tool_calls") or []
    return InferenceResponse(
        content=r.get("content", ""),
        thinking=r.get("thinking", ""),
        tool_calls=tool_calls,
        metrics=metrics,
    )


def _iter_sse_events(lines: Iterator[str]) -> Iterator[StreamEvent]:
    """Parse SSE text/event-stream lines into StreamEvents."""
    state: dict[str, str] = {}
    for line in lines:
        ev = _parse_sse_line(line, state)
        if ev is not None:
            yield ev


def _parse_sse_line(line: str, state: dict[str, str]) -> StreamEvent | None:
    if line.startswith("event: "):
        state["event"] = line[7:]
        return None
    if line.startswith("data: "):
        state["data"] = line[6:]
        return None
    if line == "" and "event" in state and "data" in state:
        ev = _build_stream_event(state["event"], state["data"])
        state.clear()
        return ev
    return None


def _build_stream_event(event_type: str, data_str: str) -> StreamEvent | None:
    try:
        data = json.loads(data_str)
    except json.JSONDecodeError:
        return None

    if event_type == "content":
        return StreamEvent(type="content", content=data.get("content", ""))
    if event_type == "thinking":
        return StreamEvent(type="thinking", thinking=data.get("thinking", ""))
    if event_type == "tool_call":
        return StreamEvent(
            type="tool_call",
            tool_call={"name": data.get("name", ""), "arguments": data.get("arguments", {})},
        )
    if event_type == "done":
        if data.get("success") and data.get("response"):
            return StreamEvent(
                type="done",
                response=_parse_execute_response(data),
            )
    return None
