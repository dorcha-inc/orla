"""Core request/response and wire types for the Orla HTTP API."""

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class CostModel:
    """Per-backend token pricing in USD per million tokens."""

    input_cost_per_mtoken: float = 0.0
    output_cost_per_mtoken: float = 0.0


@dataclass
class LLMBackend:
    """Registered LLM backend (OpenAI-compatible or SGLang)."""

    name: str
    endpoint: str
    type: str  # "openai" | "sglang"
    model_id: str
    api_key_env_var: str = ""
    max_concurrency: int = 1
    queue_capacity: int = 0
    cost_model: CostModel | None = None
    quality: float | None = None

    def set_max_concurrency(self, n: int) -> None:
        self.max_concurrency = n

    def set_queue_capacity(self, n: int) -> None:
        self.queue_capacity = n

    def set_cost_model(self, cm: CostModel) -> None:
        self.cost_model = cm

    def set_quality(self, q: float) -> None:
        self.quality = q


@dataclass
class AccessPolicy:
    """An access control policy rule."""

    name: str
    subjects: list[str]
    resources: list[str]
    action: str  # "allow" | "deny"


@dataclass
class SchedulingHints:
    """Optional hints for stage/request scheduling."""

    priority: int | None = None
    request_priority: int | None = None


@dataclass
class CacheHints:
    """Optional KV-cache behavior hints."""

    preserve_threshold_tokens: int | None = None
    flush_on_complete: bool | None = None


@dataclass
class Message:
    """Chat message for execute requests."""

    role: str
    content: str
    tool_call_id: str = ""
    tool_name: str = ""
    tool_calls: list[dict] = field(default_factory=list)


@dataclass
class StructuredOutputRequest:
    """Structured JSON output (response_format) for the model."""

    name: str
    schema: dict
    strict: bool = False


@dataclass
class ExecuteRequest:
    """Non-streaming or streaming execute request payload."""

    backend: str
    stage_id: str = ""
    prompt: str = ""
    messages: list[Message] = field(default_factory=list)
    tools: list[dict] = field(default_factory=list)
    max_tokens: int | None = None
    temperature: float | None = None
    top_p: float | None = None
    stream: bool = False
    response_format: StructuredOutputRequest | None = None
    chat_template_kwargs: dict | None = None
    scheduling_policy: str = ""
    request_scheduling_policy: str = ""
    scheduling_hints: SchedulingHints | None = None
    workflow_id: str = ""
    cache_policy: str = ""
    cache_hints: CacheHints | None = None
    reasoning_effort: str = ""
    accuracy: float | None = None
    accuracy_policy: str = ""
    tags: dict[str, str] = field(default_factory=dict)
    data_labels: list[str] = field(default_factory=list)

    def to_dict(self) -> dict:
        """Serialize to JSON-compatible dict, omitting None/empty values."""
        d: dict = {"backend": self.backend}
        if self.stage_id:
            d["stage_id"] = self.stage_id
        if self.prompt:
            d["prompt"] = self.prompt
        if self.messages:
            d["messages"] = [_message_to_dict(m) for m in self.messages]
        if self.tools:
            d["tools"] = self.tools
        if self.max_tokens is not None:
            d["max_tokens"] = self.max_tokens
        if self.temperature is not None:
            d["temperature"] = self.temperature
        if self.top_p is not None:
            d["top_p"] = self.top_p
        if self.stream:
            d["stream"] = True
        if self.response_format is not None:
            d["response_format"] = {
                "name": self.response_format.name,
                "schema": self.response_format.schema,
                "strict": self.response_format.strict,
            }
        if self.chat_template_kwargs:
            d["chat_template_kwargs"] = self.chat_template_kwargs
        if self.scheduling_policy:
            d["scheduling_policy"] = self.scheduling_policy
        if self.request_scheduling_policy:
            d["request_scheduling_policy"] = self.request_scheduling_policy
        if self.scheduling_hints is not None:
            hints: dict[str, int] = {}
            if self.scheduling_hints.priority is not None:
                hints["priority"] = self.scheduling_hints.priority
            if self.scheduling_hints.request_priority is not None:
                hints["request_priority"] = self.scheduling_hints.request_priority
            if hints:
                d["scheduling_hints"] = hints
        if self.workflow_id:
            d["workflow_id"] = self.workflow_id
        if self.cache_policy:
            d["cache_policy"] = self.cache_policy
        if self.cache_hints is not None:
            ch: dict = {}
            if self.cache_hints.preserve_threshold_tokens is not None:
                ch["preserve_threshold_tokens"] = self.cache_hints.preserve_threshold_tokens
            if self.cache_hints.flush_on_complete is not None:
                ch["flush_on_complete"] = self.cache_hints.flush_on_complete
            if ch:
                d["cache_hints"] = ch
        if self.reasoning_effort:
            d["reasoning_effort"] = self.reasoning_effort
        if self.accuracy is not None:
            d["accuracy"] = self.accuracy
        if self.accuracy_policy:
            d["accuracy_policy"] = self.accuracy_policy
        if self.tags:
            d["tags"] = self.tags
        if self.data_labels:
            d["data_labels"] = self.data_labels
        return d


def _message_to_dict(m: Message) -> dict:
    d: dict = {"role": m.role, "content": m.content}
    if m.tool_call_id:
        d["tool_call_id"] = m.tool_call_id
    if m.tool_name:
        d["tool_name"] = m.tool_name
    if m.tool_calls:
        d["tool_calls"] = m.tool_calls
    return d


@dataclass
class InferenceResponseMetrics:
    """Latency and token metrics from a completed inference."""

    ttft_ms: int | None = None
    tpot_ms: int | None = None
    prompt_tokens: int = 0
    completion_tokens: int = 0
    queue_wait_ms: int = 0
    scheduler_decision_ms: int = 0
    dispatch_ms: int = 0
    backend_latency_ms: int | None = None
    estimated_cost_usd: float | None = None


@dataclass
class InferenceResponse:
    """Assistant output from a completed inference."""

    content: str
    thinking: str = ""
    tool_calls: list[dict] = field(default_factory=list)
    metrics: InferenceResponseMetrics | None = None


@dataclass
class StreamEvent:
    """One event from a streaming execute response."""

    type: str  # "content" | "thinking" | "tool_call" | "done"
    content: str = ""
    thinking: str = ""
    tool_call: dict | None = None
    response: InferenceResponse | None = None


# Scheduling policy constants
SCHEDULING_POLICY_FCFS = "fcfs"
SCHEDULING_POLICY_PRIORITY = "priority"
REQUEST_SCHEDULING_POLICY_FCFS = "fcfs"
REQUEST_SCHEDULING_POLICY_PRIORITY = "priority"

# Cache policy constants
CACHE_POLICY_PRESERVE = "preserve"
CACHE_POLICY_FLUSH = "flush"
CACHE_POLICY_AUTO = "auto"

# Accuracy policy constants
ACCURACY_POLICY_PREFER = "prefer"
ACCURACY_POLICY_STRICT = "strict"

# Execution mode constants
EXECUTION_MODE_SINGLE_SHOT = "single_shot"
EXECUTION_MODE_AGENT_LOOP = "agent_loop"

# Access control action constants
ACCESS_ACTION_ALLOW = "allow"
ACCESS_ACTION_DENY = "deny"
