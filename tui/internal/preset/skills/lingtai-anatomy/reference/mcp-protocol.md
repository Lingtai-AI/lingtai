# MCP Protocol — LingTai Anatomy Reference

> **Scope:** Canonical specification for the LingTai MCP capability and the
> LingTai Inbox Callback Contract (LICC). Covers the catalog → registry →
> activation chain, subprocess lifecycle, environment injection, and the
> file-based inbound callback channel that lets out-of-process MCP servers
> push events into the host agent's inbox.

---

## 概述 / Overview

LingTai 不再以"in-process addon"形式集成第三方协议（IMAP / Telegram / Feishu / WeChat 等）。所有此类集成都以独立的 MCP（Model Context Protocol）服务器进程实现，并通过一组明确的契约与 kernel 协作：

1. **MCP capability** — agent 端的轻量调度层，拥有"目录(catalog) → 注册表(registry) → 激活(activation)"的三层模型。
2. **MCP subprocess loader** — 在 agent 启动 / refresh 时根据 `init.json` 的 `mcp:` 字段拉起 MCP 子进程。
3. **LICC (LingTai Inbox Callback Contract)** — MCP 子进程向宿主 agent 收件箱推送事件的文件协议。

The kernel itself owns none of the protocol-specific code (no httpx / lark_oapi / imapclient lives inside lingtai-kernel). The four first-party addons live in sibling repositories — see [Reference Implementations](#reference-implementations).

---

## 1. Three-Layer Model

```
┌──────────────────────────────────────────────────────────┐
│ Layer 1 — Catalog (kernel-shipped, editorial)            │
│   src/lingtai/mcp_catalog.json                            │
│   {name → {summary, transport, command, args, source,    │
│            homepage}}                                     │
│   Known entries: imap, telegram, feishu, wechat           │
│   Substitution: "{python}" → sys.executable               │
├──────────────────────────────────────────────────────────┤
│ Layer 2 — Registry (per-agent, JSONL, gating)            │
│   <agent_workdir>/mcp_registry.jsonl                      │
│   One validated record per line. Append-only.             │
│   Sources: (a) auto-decompressed from catalog when an     │
│            addon name appears in init.json `addons:`,     │
│            (b) hand-written by the agent for third-party  │
│            MCPs.                                           │
├──────────────────────────────────────────────────────────┤
│ Layer 3 — Activation (per-agent, in init.json)           │
│   init.json.mcp = {name → subprocess spec}                │
│   Loader cross-references against the registry; entries   │
│   without a matching record are skipped with a warning.   │
└──────────────────────────────────────────────────────────┘
```

Promotion path: **catalog → registry → activation**. Each transition is explicit and auditable. The agent never spawns an MCP just because it appears in the catalog; the human (or the agent) must opt in via the registry, and activate via init.json.

---

## 2. MCP Capability

The `mcp` capability is intentionally a **lighthouse** — pure presentation. It does not write to the registry, does not spawn subprocesses, and does not own any tool-call execution. Its only job is to render the registry into the agent's system prompt as `<registered_mcp>` so the agent knows what's available.

### Tool surface

One action: `mcp(action="show")`. Returns `{status, mcp_manual, registry_path, registered_count, registered, problems}`. All other registry operations (register, deregister, update) happen via **file operations** — the agent uses `write` / `edit` / `bash` to mutate `mcp_registry.jsonl` directly, then runs `system(action="refresh")`.

### Boot-time decompression

When the kernel sets up the `mcp` capability (during `Agent.__init__` or `_setup_from_init`), if `init.json` contains an `addons: ["imap", ...]` list, the capability calls `decompress_addons()`:

```python
for name in addons:
    if name not in registry:        # skip if already present (idempotent)
        record = catalog[name]      # lookup; warn if missing
        substitute("{python}", sys.executable)  # template substitution
        validate(record)
        append_to_registry(record)
```

**Properties:**
- **Append-only**: never modifies existing registry records.
- **Idempotent**: running multiple times produces the same registry as once.
- **Catalog-version stable**: a new kernel version with an updated catalog entry does NOT touch already-registered records — the agent's contract with that MCP is frozen at registration time. To pick up catalog updates, deregister + re-decompress (or use the migration story).

### Validator

Every catalog record and every JSONL line is validated. Schema (defined in `lingtai/core/mcp/__init__.py:validate_record`):

| Field | Type | Required | Notes |
|---|---|---|---|
| `name` | str | yes | Matches `^[a-z][a-z0-9_-]{0,30}$` |
| `summary` | str | yes | Max 200 chars |
| `transport` | `"stdio"` \| `"http"` | yes | |
| `source` | str | yes | E.g., `"lingtai-curated"`, `"user"`, or a URL |
| `command` | str | conditional | Required for stdio |
| `args` | list[str] | conditional | Required for stdio (may be `[]`) |
| `url` | str | conditional | Required for http |
| `env` | dict | optional | Subprocess env vars |
| `headers` | dict | optional | http-only |
| `homepage` | str | optional | Canonical setup-doc URL |
| `env_required` | list[str] | optional | Hint to humans/agents |

Invalid lines are dropped with a warning at parse time; malformed catalog records are rejected at decompression time.

---

## 3. Subprocess Loader

`Agent._load_mcp_from_workdir` reads two sources in order:

1. **`<workdir>/mcp/servers.json`** — legacy direct MCP wiring, ungated. Loaded as-is. Same dict shape as `init.json.mcp` (see below).
2. **`init.json.mcp`** — gated by registry membership. Each entry:

```json
{
  "mcp": {
    "imap": {
      "type": "stdio",
      "command": "/Users/.../runtime/venv/bin/python",
      "args": ["-m", "lingtai_imap"],
      "env": {
        "LINGTAI_IMAP_CONFIG": ".secrets/imap.json"
      }
    }
  }
}
```

For `init.json.mcp` entries, the loader cross-references the name against `mcp_registry.jsonl`. **An MCP cannot be activated unless it has a matching registry record.** Unregistered entries are skipped with a logged warning.

### Environment injection

The kernel **automatically injects two env vars** into every spawned MCP subprocess (whether from `mcp/servers.json` or `init.json.mcp`):

- `LINGTAI_AGENT_DIR` — absolute path to the host agent's working directory.
- `LINGTAI_MCP_NAME` — the MCP's registry name (e.g., `"imap"`).

User-supplied `env:` keys override these only by name collision (rare). These two vars are LICC's foundation: they let the MCP subprocess find the agent's filesystem inbox without any IPC handshake.

### Lifecycle

- **Spawn** during `Agent.__init__` (and after `system(refresh)`) — `connect_mcp()` opens stdio, calls `tools/list`, registers each tool into the agent's tool surface.
- **Stop** during `Agent.stop()` — `client.close()` on each `_mcp_clients` entry. The LICC poller is stopped first to drain any in-flight events.
- **Crash isolation** — an MCP crashing does not crash the host agent. Tool calls return error JSON; the LICC poller continues unaffected (it only reads the filesystem).

---

## 4. LICC v1 — LingTai Inbox Callback Contract

LICC is a **filesystem-based protocol** that lets out-of-process MCP servers push events into the host agent's inbox. It is the inbound counterpart of MCP tool calls (which are MCP → kernel → tool result, agent-initiated). LICC is MCP → kernel → agent inbox, MCP-initiated.

> **Implementation:** kernel side at `lingtai-kernel/src/lingtai/core/mcp/inbox.py`. Reference client at `lingtai-imap/src/lingtai_imap/licc.py` (vendored verbatim into each first-party MCP repo).

### 4.1 Path convention

MCPs write events to:

```
<agent_workdir>/.mcp_inbox/<mcp_name>/<event_id>.json
```

- `<agent_workdir>` comes from the `LINGTAI_AGENT_DIR` env var.
- `<mcp_name>` comes from the `LINGTAI_MCP_NAME` env var.
- `<event_id>` is any unique string the MCP picks. The reference client uses `f"{millis}-{uuid_hex8}"`.

The kernel polls `<workdir>/.mcp_inbox/*/*.json` at the same cadence as the mailbox listener (0.5s default). Subdirs starting with `.` (notably `.dead/`) are skipped.

### 4.2 Atomicity

MCPs MUST write events atomically:

1. Write to `<event_id>.json.tmp`.
2. `fsync()` the file descriptor.
3. `os.rename()` the `.tmp` → `.json`.

The kernel ignores any file ending in `.tmp`. This guarantees the poller never reads a half-written event. Reference client uses `tempfile.NamedTemporaryFile` semantics to ensure the rename is observed atomically.

### 4.3 Event schema (v1)

```json
{
  "licc_version": 1,
  "from": "human-readable sender identity",
  "subject": "one-line summary",
  "body": "full message body or preview",
  "metadata": {
    "key": "value"
  },
  "wake": true,
  "received_at": "2026-04-29T15:42:00Z"
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `licc_version` | int | optional (default 1) | Hard-rejected if not 1 |
| `from` | str | yes | Non-empty |
| `subject` | str | yes | Non-empty, max 200 chars |
| `body` | str | yes | May be empty string |
| `metadata` | dict | optional | Arbitrary, MCP-defined |
| `wake` | bool | optional (default `true`) | When `false`, kernel delivers to inbox but does NOT call `_wake_nap` |
| `received_at` | ISO-8601 str | optional | Kernel fills in if absent |

**Body convention:** for high-volume events (e.g., real email bodies), pass a preview (~300 chars) in `body` and put routing keys in `metadata.<id>` so the agent can call back into the MCP via tool calls (`imap(action="read", email_id=...)`) for the full content. This keeps the inbox readable and avoids ballooning the agent's prompt.

### 4.4 Validation and dead-letter

Every event is validated before dispatch (`validate_event` in `inbox.py`). If validation fails (parse error, missing required field, unknown version, bad type), the event is **dead-lettered**:

```
<agent_workdir>/.mcp_inbox/<mcp_name>/.dead/<event_id>.json
<agent_workdir>/.mcp_inbox/<mcp_name>/.dead/<event_id>.error.json
```

Dead-letters are **never auto-deleted**. Humans / agents can inspect `.error.json` to debug, then choose to delete the pair. The poller skips the `.dead/` subdir on subsequent passes.

### 4.5 Dispatch

A valid event lands in the agent's inbox via:

```python
notification = (
    f"[system] New event from MCP '<mcp_name>'.\n"
    f"  From: <from>\n"
    f"  Subject: <subject>\n"
    f"  <body[:200]>{'...' if len(body) > 200 else ''}"
)
agent.inbox.put(_make_message(MSG_REQUEST, "system", notification))
if event["wake"]:
    agent._wake_nap("mcp_event")
```

The agent sees a `[system]` notification with sender + subject + body preview. The full event payload (including metadata) is logged to `events.jsonl` under the `mcp_inbox_event` event type but is **not** included in the inbox message. This is intentional: the inbox holds short notifications; agents call back into the MCP for full data.

After successful dispatch, the kernel deletes the event file. Failed dispatches (e.g., transient inbox.put errors) leave the file in place for the next poll cycle.

### 4.6 Versioning policy

LICC v1 is **stable and supported indefinitely**. New fields that may land in v2 will be additive — a v1 event without those fields will continue to work after a kernel that supports v2 ships. The version field exists primarily as a hard barrier: events whose `licc_version` is greater than what the kernel knows are dead-lettered with a clear error, so MCPs cannot silently shadow contract changes.

If a v2 ships, MCPs may opt in by writing `"licc_version": 2`; until they do, they continue to write v1.

### 4.7 Backpressure

The kernel processes up to 100 events per poll cycle per MCP (`MAX_EVENTS_PER_CYCLE` in `inbox.py`). Beyond that, events queue on disk until the next cycle. This caps ingestion rate and prevents one chatty MCP from monopolizing the agent's inbox in a single tick. MCPs that need to throttle their own production should do so at source (e.g., debounce identical events).

---

## 5. Reference Client — `licc.py`

Each first-party MCP repo vendors a copy of the LICC client helper. The complete implementation is ~80 lines:

```python
def push_inbox_event(
    sender: str,
    subject: str,
    body: str,
    *,
    metadata: dict | None = None,
    wake: bool = True,
) -> bool:
    agent_dir = os.environ.get("LINGTAI_AGENT_DIR")
    mcp_name = os.environ.get("LINGTAI_MCP_NAME")
    if not agent_dir or not mcp_name:
        log.warning("LICC: env vars missing; event dropped")
        return False
    event = {
        "licc_version": 1,
        "from": sender, "subject": subject, "body": body,
        "metadata": metadata or {}, "wake": wake,
        "received_at": datetime.now(timezone.utc).isoformat(),
    }
    target_dir = Path(agent_dir) / ".mcp_inbox" / mcp_name
    target_dir.mkdir(parents=True, exist_ok=True)
    event_id = f"{int(time.time() * 1000)}-{uuid.uuid4().hex[:8]}"
    tmp = target_dir / f"{event_id}.json.tmp"
    final = target_dir / f"{event_id}.json"
    with tmp.open("w", encoding="utf-8") as f:
        f.write(json.dumps(event, ensure_ascii=False))
        f.flush()
        os.fsync(f.fileno())
    tmp.rename(final)
    return True
```

The helper is **never raises** — it is designed to be called from listener callback threads where exceptions would silently kill the listener. Failures log a warning and return `False`.

### Implementing LICC in another language

The contract is purely filesystem-based. To implement LICC in Go, Node, Rust, or anything else:

1. Read `LINGTAI_AGENT_DIR` and `LINGTAI_MCP_NAME` from the process environment.
2. Construct the target directory `<LINGTAI_AGENT_DIR>/.mcp_inbox/<LINGTAI_MCP_NAME>/`. Create it (mkdir -p) if absent.
3. Compose a JSON event matching the §4.3 schema. Set `licc_version: 1`.
4. Write to `<event_id>.json.tmp`, fsync, then rename to `<event_id>.json`. The rename must be atomic on the host filesystem (it is on POSIX and on NTFS).
5. Return / continue. The kernel poller will pick the file up on its next 0.5s tick.

That's the entire MCP-side implementation surface. There is no IPC, no protocol handshake, no version negotiation at runtime.

---

## 6. Activation Workflows

### Curated MCP (lingtai-imap, lingtai-telegram, lingtai-feishu, lingtai-wechat)

```jsonc
// init.json
{
  "addons": ["imap"],                         // ← decompression key
  "mcp": {
    "imap": {                                 // ← activation entry
      "type": "stdio",
      "command": "/path/to/python",
      "args": ["-m", "lingtai_imap"],
      "env": { "LINGTAI_IMAP_CONFIG": ".secrets/imap.json" }
    }
  }
}
```

On boot:
1. `addons: ["imap"]` triggers decompression → `mcp_registry.jsonl` gains an `imap` line from the kernel catalog.
2. `mcp.imap` activation → loader looks up `imap` in the registry (found), spawns `python -m lingtai_imap`, injects env vars.
3. MCP starts, opens stdio, advertises tools via `tools/list`. Kernel registers each tool into `_tool_schemas`.
4. If the MCP has a listener thread, it begins watching for inbound (e.g., IMAP IDLE) and pushes via LICC when events arrive.

### Custom / third-party MCP

The agent (or human via init.json edit) writes a full registry record by hand, then adds a matching activation entry. The catalog never enters the picture for third-party MCPs.

```bash
# From an agent's bash tool
echo '{"name":"my-mcp","summary":"Custom helper.","transport":"stdio","command":"my-mcp-binary","args":[],"source":"user"}' \
  >> mcp_registry.jsonl
```

Then add to init.json's `mcp:` field and `system(action="refresh")`.

---

## 7. Reference Implementations

The four first-party MCP repos. Each is a thin Python package wrapping a manager + service + LICC client.

| MCP | Repo | Tool surface | Listener |
|---|---|---|---|
| `imap` | [Lingtai-AI/lingtai-imap](https://github.com/Lingtai-AI/lingtai-imap) | omnibus `imap(action=...)` 14 actions | IMAP IDLE per-account |
| `telegram` | [Lingtai-AI/lingtai-telegram](https://github.com/Lingtai-AI/lingtai-telegram) | omnibus `telegram(action=...)` 11 actions | Bot API getUpdates per-account |
| `feishu` | [Lingtai-AI/lingtai-feishu](https://github.com/Lingtai-AI/lingtai-feishu) | omnibus `feishu(action=...)` 9 actions | Open API WebSocket per-account |
| `wechat` | [Lingtai-AI/lingtai-wechat](https://github.com/Lingtai-AI/lingtai-wechat) | omnibus `wechat(action=...)` 8 actions | iLink long-poll (single-account) |

Each repo's `README.md` is the canonical setup doc for that MCP. The `licc.py` helper is vendored verbatim into each — when the contract evolves, all four are updated together.

The kernel-side `mcp` capability never imports any MCP-specific code. There is no in-process integration anywhere; every protocol handler lives in its own subprocess.

---

## Migration Notes

In v0.7.3 the kernel's in-process `addons/` tree was removed entirely (~3000 LOC), replaced by the four sibling MCP repos plus the catalog/registry/LICC infrastructure described above. Existing users with legacy `init.json` `addons: {imap: {config: "..."}}` dict-shape declarations are migrated automatically by TUI/portal migration **m028**, which:

- Rewrites `addons:` to the new list-of-names shape.
- Adds matching `mcp:` activation entries with the resolved venv-python command.
- Resolves any `*_env` indirection in the addon's config file at migration time (the new MCPs require plaintext config; the kernel's `_resolve_addons` helper is gone).

See [`changelog.md`](changelog.md) for the full migration history.
