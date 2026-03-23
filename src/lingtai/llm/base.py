"""LLMAdapter ABC — abstract interface for LLM provider adapters.

Moved from lingtai-kernel to lingtai: adapters are an implementation concern,
not a kernel protocol type.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from collections.abc import Callable
from typing import Any

from lingtai_kernel.llm.base import ChatSession, FunctionSchema, LLMResponse
from lingtai_kernel.llm.interface import ChatInterface, ToolResultBlock

from .api_gate import APICallGate


class LLMAdapter(ABC):
    """Abstract interface that every LLM provider adapter must implement."""

    _gate: APICallGate | None = None

    def _setup_gate(self, max_rpm: int) -> None:
        """Set up rate-limiting gate for this adapter.

        Args:
            max_rpm: Maximum requests per minute. 0 disables.
        """
        if max_rpm > 0:
            self._gate = APICallGate(max_rpm)

    def _gated_call(self, fn: Callable[[], Any]) -> Any:
        """Run fn through the gate if configured, otherwise call directly."""
        if self._gate is not None:
            return self._gate.submit(fn)
        return fn()

    @abstractmethod
    def create_chat(
        self,
        model: str,
        system_prompt: str,
        tools: list[FunctionSchema] | None = None,
        *,
        json_schema: dict | None = None,
        force_tool_call: bool = False,
        interface: ChatInterface | None = None,
        thinking: str = "default",
        interaction_id: str | None = None,
        context_window: int = 0,
    ) -> ChatSession:
        """Create a new multi-turn chat session.

        Args:
            model: Model identifier (e.g. ``"gemini-3-flash-preview"``).
            system_prompt: System instruction for the session.
            tools: Tool/function schemas available to the model.
            json_schema: If set, enforce JSON output conforming to this schema.
            force_tool_call: If True, force the model to call a tool (Gemini
                ``mode="ANY"``).
            interface: Previously saved ChatInterface to restore.
                The session inherits this interface instance and converts
                it to provider format for the initial API state.
            thinking: Thinking level — ``"low"``, ``"high"``, or ``"default"``
                (adapter decides).
            interaction_id: Gemini Interactions API session ID for server-side
                history resume.  Ignored by providers that don't support it.
            context_window: Total context window in tokens for this model.
                0 = unknown.  Provided by LLMService.
        """

    @abstractmethod
    def generate(
        self,
        model: str,
        contents: str | list,
        *,
        system_prompt: str | None = None,
        temperature: float | None = None,
        json_schema: dict | None = None,
        max_output_tokens: int | None = None,
    ) -> LLMResponse:
        """One-shot generation (no chat history).

        Used for memory analysis, follow-up suggestions, document extraction,
        and other single-turn calls.
        """

    @abstractmethod
    def make_tool_result_message(
        self, tool_name: str, result: dict, *, tool_call_id: str | None = None
    ) -> ToolResultBlock:
        """Build a canonical ToolResultBlock.

        Args:
            tool_name: The name of the tool that was called.
            result: The result dict returned by the tool executor.
            tool_call_id: Provider-assigned tool-call ID from ToolCall.id.
        """

    @abstractmethod
    def is_quota_error(self, exc: Exception) -> bool:
        """Return True if ``exc`` represents a quota/rate-limit error (429)."""
