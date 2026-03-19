"""LLM adapter layer — multi-provider support with kernel protocol re-exports."""

from stoai_kernel.llm.base import LLMAdapter, ChatSession, LLMResponse, ToolCall, FunctionSchema
from stoai_kernel.llm.service import LLMService

__all__ = [
    "LLMAdapter",
    "ChatSession",
    "LLMResponse",
    "ToolCall",
    "FunctionSchema",
    "LLMService",
]

# Register built-in adapters on import
from ._register import register_all_adapters as _register_all_adapters
_register_all_adapters()
