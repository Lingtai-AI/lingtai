"""LLM adapter layer — multi-provider support with kernel protocol re-exports."""
import sys

import stoai_kernel.llm.base
import stoai_kernel.llm.service
import stoai_kernel.llm.interface
import stoai_kernel.llm.streaming

# Alias kernel modules so that ``from stoai.llm.service import X`` and
# ``from stoai_kernel.llm.service import X`` return the same objects.
# Without this, the old local files (not yet deleted) would be loaded as
# separate modules, creating duplicate classes and split registries.
sys.modules.setdefault("stoai.llm.base", stoai_kernel.llm.base)
sys.modules.setdefault("stoai.llm.service", stoai_kernel.llm.service)
sys.modules.setdefault("stoai.llm.interface", stoai_kernel.llm.interface)
sys.modules.setdefault("stoai.llm.streaming", stoai_kernel.llm.streaming)

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
