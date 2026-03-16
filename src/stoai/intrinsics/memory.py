"""Memory intrinsic — long-term memory management.

Actions:
    load — read ltm/ltm.md from disk, reload into live system prompt, git commit

The handler lives in BaseAgent (needs access to working_dir, prompt_manager, git).
This module provides the schema and description.
"""
from __future__ import annotations

SCHEMA = {
    "type": "object",
    "properties": {
        "action": {
            "type": "string",
            "enum": ["load"],
            "description": (
                "load: read the long-term memory file (ltm/ltm.md in working directory) "
                "and reload its contents into the live system prompt. "
                "Call this after editing ltm/ltm.md with the write/edit intrinsics "
                "to make your changes take effect in the current conversation.\n"
                "Returns: status, path (absolute path to the ltm file), "
                "size_bytes (file size), content_preview (first 200 chars), "
                "diff (git diff of changes with commit hash).\n"
                "If the file does not exist, it is created empty and loaded.\n"
                "Workflow: write/edit ltm/ltm.md → call memory load → "
                "changes are now part of your system prompt and committed to git."
            ),
        },
    },
    "required": ["action"],
}
DESCRIPTION = (
    "Long-term memory management. The agent's persistent memory lives in "
    "ltm/ltm.md (markdown file in working directory). "
    "Edit that file with read/write/edit intrinsics, then call 'load' to "
    "reload it into the live system prompt. "
    "Use this to persist important information across context compactions "
    "and agent restarts."
)
