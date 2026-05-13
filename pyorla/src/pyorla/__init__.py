"""pyorla — Python SDK for Orla inference scheduling and orchestration.

**Start here**

- **HTTP client:** `OrlaClient`, `OrlaClient.from_env()` (`ORLA_URL`), `OrlaError`
- **Local daemon (subprocess):** `orla_runtime`, `OrlaBinaryNotFoundError`
- **Stages / workflows:** `Stage`, `Workflow`, types and backend helpers
- **LangChain:** `ChatOrla`
- **Tools:** `orla_tool`, `tool_from_langchain`, `Tool`, `new_tool`
"""

from pyorla.backend import new_ollama_backend, new_sglang_backend, new_vllm_backend
from pyorla.chat_model import ChatOrla
from pyorla.client import OrlaClient, OrlaError
from pyorla.langchain_tools import tool_from_langchain
from pyorla.local_server import OrlaBinaryNotFoundError, orla_runtime, resolve_orla_binary
from pyorla.memory import (
    CacheEvent,
    DefaultMemoryPolicy,
    FlushAtBoundaryPolicy,
    MemoryPolicy,
    PreserveOnSmallIncrementPolicy,
)
from pyorla.messages import (
    langchain_to_orla,
    orla_response_to_ai_message,
    orla_to_langchain,
)
from pyorla.stage import Stage, StageResultData
from pyorla.stage_mapping import (
    ExplicitStageMapping,
    StageAssignment,
    StageMapping,
    StageMappingInput,
    StageMappingOutput,
    apply_stage_mapping_output,
)
from pyorla.tool_decorators import orla_tool
from pyorla.tools import (
    Tool,
    ToolCall,
    ToolResult,
    new_tool,
    tool_call_from_raw,
    tool_runner_from_schema,
)
from pyorla.types import (
    ACCESS_ACTION_ALLOW,
    ACCESS_ACTION_DENY,
    ACCURACY_POLICY_PREFER,
    ACCURACY_POLICY_STRICT,
    CACHE_POLICY_AUTO,
    CACHE_POLICY_FLUSH,
    CACHE_POLICY_PRESERVE,
    EXECUTION_MODE_AGENT_LOOP,
    EXECUTION_MODE_SINGLE_SHOT,
    REQUEST_SCHEDULING_POLICY_FCFS,
    REQUEST_SCHEDULING_POLICY_PRIORITY,
    SCHEDULING_POLICY_FCFS,
    SCHEDULING_POLICY_PRIORITY,
    AccessPolicy,
    CacheHints,
    CostModel,
    ExecuteRequest,
    InferenceResponse,
    InferenceResponseMetrics,
    LLMBackend,
    Message,
    SchedulingHints,
    StreamEvent,
    StructuredOutputRequest,
)
from pyorla.workflow import Workflow

__all__ = [
    # Client
    "OrlaClient",
    "OrlaError",
    "OrlaBinaryNotFoundError",
    "resolve_orla_binary",
    "orla_runtime",
    # Chat model (LangChain)
    "ChatOrla",
    # Stage
    "Stage",
    "StageResultData",
    # Workflow
    "Workflow",
    # Backend constructors
    "new_vllm_backend",
    "new_sglang_backend",
    "new_ollama_backend",
    # Stage mapping
    "StageMapping",
    "ExplicitStageMapping",
    "StageAssignment",
    "StageMappingInput",
    "StageMappingOutput",
    "apply_stage_mapping_output",
    # Tools
    "Tool",
    "ToolCall",
    "ToolResult",
    "new_tool",
    "orla_tool",
    "tool_call_from_raw",
    "tool_from_langchain",
    "tool_runner_from_schema",
    # Memory
    "MemoryPolicy",
    "DefaultMemoryPolicy",
    "PreserveOnSmallIncrementPolicy",
    "FlushAtBoundaryPolicy",
    "CacheEvent",
    # Messages
    "langchain_to_orla",
    "orla_response_to_ai_message",
    "orla_to_langchain",
    # Access control
    "AccessPolicy",
    "ACCESS_ACTION_ALLOW",
    "ACCESS_ACTION_DENY",
    # Types
    "CostModel",
    "LLMBackend",
    "ExecuteRequest",
    "Message",
    "InferenceResponse",
    "InferenceResponseMetrics",
    "StreamEvent",
    "SchedulingHints",
    "CacheHints",
    "StructuredOutputRequest",
    # Constants
    "SCHEDULING_POLICY_FCFS",
    "SCHEDULING_POLICY_PRIORITY",
    "REQUEST_SCHEDULING_POLICY_FCFS",
    "REQUEST_SCHEDULING_POLICY_PRIORITY",
    "CACHE_POLICY_PRESERVE",
    "CACHE_POLICY_FLUSH",
    "CACHE_POLICY_AUTO",
    "EXECUTION_MODE_SINGLE_SHOT",
    "EXECUTION_MODE_AGENT_LOOP",
    "ACCURACY_POLICY_PREFER",
    "ACCURACY_POLICY_STRICT",
]
