"""Status intrinsic — agent self-inspection.

Actions:
    show — display agent identity, runtime, and resource usage

The handler lives in BaseAgent (needs access to agent state).
This module provides the schema and description.
"""
from __future__ import annotations

SCHEMA = {
    "type": "object",
    "properties": {
        "action": {
            "type": "string",
            "enum": ["show"],
            "description": (
                "show: display full agent self-inspection. Returns:\n"
                "- identity: agent_id, working_dir, mail_address (or null if no mail service)\n"
                "- runtime: started_at (UTC ISO), uptime_seconds\n"
                "- tokens.input_tokens, output_tokens, thinking_tokens, cached_tokens, "
                "total_tokens, api_calls: cumulative LLM usage since start\n"
                "- tokens.context.system_tokens, tools_tokens, history_tokens: "
                "current context window breakdown\n"
                "- tokens.context.window_size: total context window capacity\n"
                "- tokens.context.usage_pct: percentage of context window currently occupied\n"
                "Use this to monitor resource consumption, decide when to save "
                "important information to long-term memory, and identify yourself."
            ),
        },
    },
    "required": ["action"],
}
DESCRIPTION = (
    "Agent self-inspection. 'show' returns identity (agent_id, working_dir, "
    "mail address), runtime (uptime), and resource usage (cumulative tokens, "
    "context window breakdown with usage percentage). "
    "Check this to monitor your own resource consumption and decide when to "
    "save important information to long-term memory before context compaction."
)
