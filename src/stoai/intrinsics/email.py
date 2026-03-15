"""Email intrinsic — structured inter-agent messaging with inbox.

Actions:
    send    — fire-and-forget message to an address
    check   — list emails in inbox (from, time, subject, preview)
    read    — read a specific email by ID

The actual handlers live in BaseAgent (needs access to EmailService and mailbox).
This module provides the schema and description.
"""
from __future__ import annotations

SCHEMA = {
    "type": "object",
    "properties": {
        "action": {
            "type": "string",
            "enum": ["send", "check", "read"],
            "description": "Action: send an email, check inbox, or read a specific email",
        },
        "address": {"type": "string", "description": "Target address for send (e.g. 127.0.0.1:8301)"},
        "subject": {"type": "string", "description": "Email subject line (for send)"},
        "message": {"type": "string", "description": "Email body (for send)"},
        "email_id": {"type": "string", "description": "Email ID to read (for read)"},
        "n": {"type": "integer", "description": "Number of recent emails to list (for check)", "default": 10},
    },
    "required": ["action"],
}
DESCRIPTION = (
    "Email tool for inter-agent communication. "
    "Use 'send' to email another agent, 'check' to list your inbox, "
    "'read' to read a specific email by ID."
)
