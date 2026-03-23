"""LLM adapter layer — multi-provider support with kernel protocol re-exports."""

from lingtai_kernel.llm.base import ChatSession, LLMResponse, ToolCall, FunctionSchema
from lingtai_kernel.llm.interface import ChatInterface
from .service import LLMService  # concrete implementation
from .base import LLMAdapter  # now from lingtai, not kernel

__all__ = [
    "LLMAdapter",
    "ChatSession",
    "LLMResponse",
    "ToolCall",
    "FunctionSchema",
    "ChatInterface",
    "LLMService",
]

# Register built-in adapters on import
from ._register import register_all_adapters as _register_all_adapters
_register_all_adapters()
