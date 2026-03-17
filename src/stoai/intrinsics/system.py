"""System intrinsic — agent identity management (role + ltm).

Actions:
    view   — read the current contents of role.md or ltm.md
    diff   — show uncommitted git diff for role.md or ltm.md
    load   — read the file, inject into live system prompt, git add+commit

Objects:
    role   — system/role.md (the agent's role / persona)
    ltm    — system/ltm.md (the agent's long-term memory)

The handler lives in BaseAgent (needs access to working_dir, prompt_manager, git).
This module provides the schema and description.
"""
from __future__ import annotations

SCHEMA = {
    "type": "object",
    "properties": {
        "action": {
            "type": "string",
            "enum": ["view", "diff", "load"],
            "description": (
                "view: read the current file contents.\n"
                "diff: show uncommitted git diff (what changed since last commit).\n"
                "load: read the file, inject into the live system prompt, "
                "and git commit. This transforms the agent — changes to role "
                "alter the agent's persona, changes to ltm update its memory."
            ),
        },
        "object": {
            "type": "string",
            "enum": ["role", "ltm"],
            "description": (
                "role: the agent's role/persona (system/role.md).\n"
                "ltm: the agent's long-term memory (system/ltm.md)."
            ),
        },
    },
    "required": ["action", "object"],
}

DESCRIPTION = (
    "Agent identity management. The agent's role lives in system/role.md "
    "and long-term memory in system/ltm.md. "
    "Use 'view' to read current contents, 'diff' to see uncommitted changes, "
    "and 'load' to apply changes into the live system prompt (with git commit). "
    "Loading transforms the agent: role changes alter persona, ltm changes update memory."
)
