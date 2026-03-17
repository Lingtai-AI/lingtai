# Anima Capability Design

**Date:** 2026-03-16
**Status:** Draft

## Overview

Anima is a capability that upgrades the `system` intrinsic, following the same pattern as `email` upgrades `mail`. It gives agents self-knowledge management: an evolving role (covenant + character), structured long-term memory, and on-demand context compaction.

Without anima, agents have basic memory via the `system` intrinsic (diff/load). With anima, agents can evolve their character, accumulate and curate operational knowledge, and proactively manage their context window.

## Prerequisite: Kernel Changes

### 1. override_intrinsic() on BaseAgent

Capabilities that upgrade intrinsics (email → mail, anima → system) currently have no clean way to replace the intrinsic tool. Email adds itself alongside mail, leaving both visible to the agent — a bug.

Add `override_intrinsic(name)` to `BaseAgent`:

```python
def override_intrinsic(self, name: str) -> Callable[[dict], dict]:
    """Remove an intrinsic and return its handler for delegation.

    Called by capabilities that upgrade an intrinsic (email → mail, anima → system).
    Must be called before start() (tool surface sealed).

    Returns the original handler so the capability can delegate to it.
    Raises KeyError if intrinsic doesn't exist.
    Raises RuntimeError if called after start().
    """
```

- Removes the named intrinsic from `_intrinsics` dict (no longer appears as a tool).
- Returns the original handler so the capability can delegate to it.

### 2. Simplify system intrinsic

The current system intrinsic operates on `role` and `ltm` objects with `view`/`diff`/`load` actions. This changes:

- Covenant (`system/covenant.md`) is injected at construction into the system prompt as a protected section. Immutable at runtime. No tool access needed.
- The system intrinsic shrinks to operate on a single object: `memory` (`system/memory.md`), with two actions: `diff` and `load`. (`view` is removed — the agent already has the content in its system prompt.)

**New system intrinsic schema:**

| Object | Actions |
|--------|---------|
| `memory` | `diff`, `load` |

The existing `_handle_system` handler is updated accordingly. `_system_view` is removed. `role.md` is renamed to `covenant.md`. `ltm.md` is renamed to `memory.md`.

### 3. Fix email

Email's `setup()` must use `override_intrinsic` to cleanly replace mail:

```python
def setup(agent):
    mgr = EmailManager(agent)
    agent.override_intrinsic("mail")  # remove mail tool; email reimplements fully
    agent._on_normal_mail = mgr.on_normal_mail
    agent.add_tool("email", schema=SCHEMA, handler=mgr.handle, description=DESCRIPTION)
    return mgr
```

## Anima Design

### Tool Schema — 3 Objects, 8 Actions

The agent sees one tool: `anima`. Three objects.

| Object | Action | Params | Returns | Side Effects |
|--------|--------|--------|---------|-------------|
| `role` | `update` | `content: str` | Confirmation | Writes `character.md` to disk |
| `role` | `diff` | — | Git diff on `character.md` | — |
| `role` | `load` | — | Confirmation + content | Injects covenant + character into live system prompt, git commits |
| `memory` | `submit` | `content: str` | Confirmation + entry ID | Appends to `memory.json`, re-renders `memory.md` |
| `memory` | `diff` | — | Git diff on `memory.md` | — |
| `memory` | `consolidate` | `ids: list[str]`, `content: str` | Confirmation + new entry ID | Removes selected entries, adds one new entry with provided content, re-renders `memory.md` |
| `memory` | `load` | — | Confirmation + content | Injects memory into live system prompt, git commits |
| `context` | `compact` | `prompt: str` | Confirmation + new context usage % | Forces context compaction |

### JSON Schema

Flat schema following the email pattern. `object` and `action` are required; other params validated per action.

```python
SCHEMA = {
    "type": "object",
    "properties": {
        "object": {
            "type": "string",
            "enum": ["role", "memory", "context"],
            "description": (
                "role: the agent's identity (system/covenant.md + system/character.md).\n"
                "memory: the agent's long-term memory (system/memory.md, backed by system/memory.json).\n"
                "context: the agent's conversation context window."
            ),
        },
        "action": {
            "type": "string",
            "enum": ["update", "diff", "load", "submit", "consolidate", "compact"],
            "description": (
                "role: update | diff | load.\n"
                "memory: submit | diff | consolidate | load.\n"
                "context: compact."
            ),
        },
        "content": {
            "type": "string",
            "description": (
                "Text content — for role update (character), "
                "memory submit (new entry), or memory consolidate (merged text)."
            ),
        },
        "ids": {
            "type": "array",
            "items": {"type": "string"},
            "description": "Memory entry IDs — for memory consolidate.",
        },
        "prompt": {
            "type": "string",
            "description": (
                "Compaction guidance — what to preserve, what to compress. "
                "Required for context compact. Can be empty."
            ),
        },
    },
    "required": ["object", "action"],
}
```

### Schema Description Coaching

These belong in the tool schema description, not the system prompt:

- **After `role update` or `memory submit`/`consolidate`:** "Use `diff` to review pending changes, then `load` to apply them into your live system prompt."
- **For `role update`:** "Update your character — your identity, your knowledge, your experience."
- **For `memory consolidate`:** "Select memory entries by ID and provide a single consolidated text. This replaces the selected entries with one merged entry. Memory IDs are visible in your system prompt."
- **For `context compact`:** "Use when context is approaching limits and you are about to undertake a complex task. Check context usage via `status show` first. Automatic compaction at 80% is your safety net — this is for proactive management. The `prompt` parameter is required (pass empty string if no specific guidance) — use it to tell the compactor what to preserve."

### Error Handling

- Unknown object/action combination: return `{"error": "..."}` listing valid actions for that specific object.
- `memory consolidate` with invalid IDs: return error listing which IDs were not found.
- `role update` with empty content: allowed (clears character).
- `memory submit` with empty content: return error.
- `context compact` without prompt: return error (prompt is required, can be empty string).

### Setup & Override Pattern

```python
def setup(agent: "BaseAgent") -> AnimaManager:
    mgr = AnimaManager(agent)
    original_system = agent.override_intrinsic("system")
    mgr._original_system = original_system  # for delegating memory diff/load
    agent.add_tool("anima", schema=SCHEMA, handler=mgr.handle, description=DESCRIPTION)
    return mgr
```

Anima replaces the `system` intrinsic. The agent sees `anima`, not `system`.

**Delegation strategy:** Anima delegates to the original system handler for `memory diff` and `memory load` by calling it with synthetic args: `self._original_system({"action": "diff", "object": "memory"})`. This works because the original handler operates on `system/memory.md` which is unchanged. This delegation depends on the kernel changes in step 1 (system intrinsic accepts `memory` as a valid object). **Role** operations are reimplemented in anima because the two-file split (`covenant.md` + `character.md`) doesn't match the original handler's single-file model.

### AnimaManager

```python
class AnimaManager:
    def __init__(self, agent: "BaseAgent"):
        self._agent = agent
        self._working_dir = agent._working_dir
        self._original_system = None  # set by setup()

        # Paths
        self._covenant_path = self._working_dir / "system" / "covenant.md"
        self._character_path = self._working_dir / "system" / "character.md"
        self._memory_md = self._working_dir / "system" / "memory.md"
        self._memory_json = self._working_dir / "system" / "memory.json"

        # In-memory cache of entries (loaded from memory.json)
        self._entries: list[dict] = self._load_entries()

    def _load_entries(self) -> list[dict]:
        """Load entries from memory.json, or return empty list if missing."""
        if not self._memory_json.is_file():
            return []
        data = json.loads(self._memory_json.read_text())
        return data.get("entries", [])

    def handle(self, args: dict) -> dict:
        """Main dispatch — routes by object + action."""
        ...
```

### File Layout

```
system/
  covenant.md     — shared behavioral contract, written at construction
                    immutable at runtime, every agent gets the same one
  character.md    — agent-mutable identity, starts empty
                    what experience has made this agent
  memory.md       — rendered from memory.json, ID-prefixed bullet list
                    injected into system prompt via load
  memory.json     — structured backing store for memory.md
```

### memory.json Format

```json
{
  "version": 1,
  "entries": [
    {
      "id": "a1b2c3d4",
      "content": "Agent bob has expertise in CDF format parsing",
      "created_at": "2026-03-16T10:30:00.123456Z"
    }
  ]
}
```

- **ID**: `sha256(content + created_at)[:8]` — deterministic. `created_at` uses ISO 8601 with sub-second precision.
- **Version field**: For future schema migrations.

### memory.md Rendering

```markdown
- [a1b2c3d4] Agent bob has expertise in CDF format parsing
- [e5f6g7h8] Dataset X uses HDF5 format, needs h5py
- [f9a0b1c2] The /data/raw/ directory contains unprocessed CSVs
```

Simple bullet list. Each entry prefixed by its ID. This is what the agent sees in its system prompt after `memory load`.

## Role Architecture

### Two-Part Role

- **`covenant.md`** — The behavioral contract. Written by host at construction or by delegating parent. Defines how agents work, communicate, and collaborate in this network. Immutable at runtime. Every agent in a population gets the same covenant.
- **`character.md`** — Starts empty. The agent's evolving self: identity, knowledge, experience — what it has become through its work. Without this, all agents with the same covenant are interchangeable.

### Role in Communication

The covenant is where behavioral norms live. Its content is defined by the host application, not by this spec. Personality evolution is emergent from the covenant's norms and the agent's experience.

### Delegation

When a parent delegates, it writes `covenant.md` (the behavioral contract) and provides an empty `character.md`. The child agent develops its own character through experience.

## Key Behaviors

### Commit Model

Only `load` triggers a git commit. All other write operations (`role update`, `memory submit`, `memory consolidate`) write to disk but do not commit. The agent controls when changes are committed by explicitly calling `role load` or `memory load`.

Encouraged workflow: make changes → `diff` to review → `load` to apply and commit.

### Context Compaction

- **Automatic** (80% threshold) — unchanged, safety net.
- **On-demand** (`context compact`) — rare, deliberate. Agent checks `status show`, sees context filling up, proactively compacts before complex work. Unlike automatic compaction, `context compact` **forces** compaction regardless of current usage threshold. The `prompt` parameter (required, can be empty) is prepended to the compaction prompt to guide what gets preserved. After compaction, returns new context usage percentage via `get_token_usage()`.

### Consolidation

Agent-driven, no LLM call. The agent already has all memories (with IDs) in its system prompt. Workflow:
1. Read memory IDs from system prompt
2. Call `memory consolidate` with selected IDs + merged text → removes old entries, adds one new entry
3. `memory diff` to review
4. `memory load` to apply and commit

Many-to-one: multiple entries in, one entry out. The agent runs consolidate multiple times for different groupings.

## Implementation Order

1. **Simplify system intrinsic** — rename files (covenant/memory), remove view, remove role object
2. **Add `override_intrinsic()` to BaseAgent** — kernel mechanism
3. **Fix email** — use `override_intrinsic("mail")` to cleanly replace mail
4. **Build anima capability** — `AnimaManager`, tool schema, setup
