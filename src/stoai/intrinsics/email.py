"""Email intrinsic — fire-and-forget inter-agent messaging.

The actual send logic lives in BaseAgent (it needs access to the EmailService).
This module provides the schema and description.

Replaces the old `talk` intrinsic. Key differences:
- Uses address (e.g. "localhost:8301") instead of target_id
- Fire-and-forget only — no send_and_wait (that's an upper layer concern)
- Backed by EmailService, not in-process _connections dict
"""
from __future__ import annotations

SCHEMA = {
    "type": "object",
    "properties": {
        "address": {"type": "string", "description": "Target address (e.g. localhost:8301)"},
        "message": {"type": "string", "description": "Message to send"},
    },
    "required": ["address", "message"],
}
DESCRIPTION = "Send an email (fire-and-forget message) to another agent at the given address."
