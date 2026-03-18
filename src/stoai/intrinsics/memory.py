"""Memory intrinsic — agent long-term memory management.

Actions:
    edit — write content to system/memory.md (disk only)
    load — read system/memory.md, inject into live system prompt, git commit
"""
from __future__ import annotations

SCHEMA = {
    "type": "object",
    "properties": {
        "action": {
            "type": "string",
            "enum": ["edit", "load"],
            "description": (
                "edit: write content to system/memory.md (disk only, does not "
                "update the live system prompt or git commit). "
                "You can call edit multiple times before loading.\n"
                "load: read system/memory.md from disk, inject into the live "
                "system prompt, and git commit. Call this after editing to apply."
            ),
        },
        "content": {
            "type": "string",
            "description": "Memory content to write (for action=edit). Replaces the entire file.",
        },
    },
    "required": ["action"],
}
DESCRIPTION = (
    "Long-term memory management. Memory lives in system/memory.md. "
    "'edit' writes content to disk. 'load' applies it into the live system "
    "prompt and git commits. Workflow: edit → load."
)


def handle(agent, args: dict) -> dict:
    """Handle memory tool — edit and load."""
    action = args.get("action", "")

    system_dir = agent._working_dir / "system"
    system_dir.mkdir(exist_ok=True)
    file_path = system_dir / "memory.md"
    if not file_path.is_file():
        file_path.write_text("")

    if action == "edit":
        return _edit(agent, file_path, args)
    elif action == "load":
        return _load(agent, file_path)
    else:
        return {"status": "error", "message": f"Unknown action: {action!r}. Must be 'edit' or 'load'."}


def _edit(agent, file_path, args: dict) -> dict:
    content = args.get("content", "")
    file_path.write_text(content)
    return {"status": "ok", "path": str(file_path), "size_bytes": len(content.encode("utf-8"))}


def _load(agent, file_path) -> dict:
    content = file_path.read_text()
    size_bytes = len(content.encode("utf-8"))

    if content.strip():
        agent._prompt_manager.write_section("memory", content)
    else:
        agent._prompt_manager.delete_section("memory")
    agent._token_decomp_dirty = True

    if agent._chat is not None:
        agent._chat.update_system_prompt(agent._build_system_prompt())

    rel_path = "system/memory.md"
    git_diff, commit_hash = agent._workdir.diff_and_commit(rel_path, "memory")

    agent._log("memory_load", size_bytes=size_bytes, changed=commit_hash is not None)

    return {
        "status": "ok",
        "path": str(file_path),
        "size_bytes": size_bytes,
        "content_preview": content[:200],
        "diff": {
            "changed": commit_hash is not None,
            "git_diff": git_diff or "",
            "commit": commit_hash,
        },
    }
