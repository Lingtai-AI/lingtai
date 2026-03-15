"""Core types for stoai."""
from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Callable


@dataclass
class MCPTool:
    """A domain tool provided to an agent via MCP-compatible interface."""
    name: str
    schema: dict
    description: str
    handler: Callable[[dict], dict]


class UnknownToolError(Exception):
    """Raised when a tool name cannot be resolved."""
    def __init__(self, tool_name: str):
        self.tool_name = tool_name
        super().__init__(f"Unknown tool: {tool_name}")

