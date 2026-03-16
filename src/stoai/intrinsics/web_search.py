"""Web search intrinsic — web lookup via LLM."""
from __future__ import annotations

SCHEMA = {
    "type": "object",
    "properties": {
        "query": {"type": "string", "description": "Search query"},
    },
    "required": ["query"],
}
DESCRIPTION = (
    "Search the web for current information. "
    "Use for real-time data, recent events, documentation, "
    "or anything beyond your training knowledge. "
    "Returns ranked search results with titles, URLs, and snippets."
)
