# Design: Postman — IPv6 Agent Mesh Communication

**Date:** 2026-04-17
**Status:** Draft / Vision

## Summary

Introduce a unified addressing scheme for inter-agent mail and a UDP-based "postman" daemon that enables cross-machine agent communication over IPv6. Agents remain filesystem-only — the postman is an external relay that translates between local mailboxes and the network.

## Motivation

LingTai agents communicate via filesystem-based email (JSON files in mailbox/inbox and mailbox/sent directories). This works perfectly on a single machine but cannot span multiple machines. Agents should be able to mail each other across the internet — a shadow network of agents communicating peer-to-peer with no central server.

The 三千世界 (three thousand worlds) metaphor demands it: avatars (分身) scattered across machines, sending experiences home through a decentralized postal network. Agent teleportation (packing state and reconstituting on a remote machine) becomes possible as a natural extension.

## Part 1: Unified Address Scheme

### Current State

Addresses are bare names like `human`, `agent_auditor`, `minimax_cn`. Resolution is implicit:

```go
// resolve.go — current logic
func ResolveAddress(addr, baseDir string) string {
    if filepath.IsAbs(addr) { return addr }
    return filepath.Join(baseDir, addr)
}
```

This supports relative (bare name) and absolute (full path) implicitly but has no concept of "which machine."

### Proposed: Relative and Absolute Modes

### Three Explicit Modes

The email tool uses an explicit `mode` parameter to declare how the address should be interpreted. This avoids ambiguous address parsing — the agent consciously chooses the delivery mode.

**`mode="rel"` (default, can be omitted)** — relative, same `.lingtai/` dir:
```
email(to="agent_auditor")                        → .lingtai/agent_auditor
email(to="human")                                → .lingtai/human
```

**`mode="abs"` — absolute, local machine, explicit path:**
```
email(to="/Users/x/.lingtai/agent_a", mode="abs")   → direct filesystem write
```

**`mode="net"` — network, remote machine, routed via postman:**
```
email(to="[2001:db8::1]:/home/y/.lingtai/agent_b", mode="net")   → UDP via postman
```

### Address Format Per Mode

| Mode | Address format | Delivery |
|------|---------------|----------|
| `rel` | bare name (`agent_auditor`) | Resolved against current `.lingtai/`, direct filesystem |
| `abs` | absolute path (`/Users/x/.lingtai/agent_a`) | Direct filesystem write to explicit path |
| `net` | `[ipv6]:/path` or `localhost:/path` | Queued in outbox, delivered by postman over UDP |

### Mode Lives in the Email Tool (Kernel)

The `mode` parameter is part of the kernel's `email` tool definition. The kernel resolves the address according to the mode and writes to the appropriate location:
- `rel` → resolve name against `.lingtai/` baseDir, write to `mailbox/inbox/`
- `abs` → use path as-is, write to `mailbox/inbox/`
- `net` → write to sender's `mailbox/outbox/`, postman picks it up

The TUI/portal side doesn't need to know about modes — it just reads messages from inbox/sent/outbox directories. The postman daemon watches outbox and sends over UDP.

### Backward Compatibility

All existing bare-name addresses continue to work identically (implicit `mode="rel"`). The mode parameter is purely additive — omitting it preserves current behavior.

### Implementation: `ResolveAddress` Evolution

```go
func ResolveAddress(addr, baseDir string) string {
    // New: check for host prefix
    if host, path, ok := ParseAbsoluteAddress(addr); ok {
        if host == "localhost" {
            return path  // local absolute
        }
        // Remote — return as-is, caller must route via postman
        return addr
    }
    // Existing: relative resolution
    if filepath.IsAbs(addr) { return addr }
    return filepath.Join(baseDir, addr)
}

func IsRemoteAddress(addr string) bool {
    host, _, ok := ParseAbsoluteAddress(addr)
    return ok && host != "localhost"
}

func ParseAbsoluteAddress(addr string) (host, path string, ok bool) {
    // Match [ipv6]:path or localhost:path
    // ...
}
```

### Impact on WriteMail

`WriteMail` currently writes directly to the recipient's inbox directory. With remote addresses:

1. If `IsRemoteAddress(toAddr)` → write to a **local outbox queue** instead of the recipient's inbox
2. The postman daemon watches this queue and delivers via UDP
3. Local delivery remains unchanged (direct filesystem write)

### Impact on MailMessage

The `From` and `To` fields in `message.json` should carry the full absolute address when crossing machines, so the recipient knows where the message came from:

```json
{
  "from": "[2001:db8::1]:/home/user/project/.lingtai/agent_auditor",
  "to": ["[2001:db8::2]:/home/other/project/.lingtai/agent_bob"],
  "message": "..."
}
```

For local messages, bare names continue to work.

## Part 2: The Postman (邮差)

### What It Is

A single long-running daemon per machine. It:

1. **Listens** on a UDP port for incoming agent mail
2. **Delivers** incoming messages to local agent inboxes by path
3. **Watches** local agent outboxes for remote-addressed mail
4. **Sends** outgoing messages as compressed UDP datagrams to remote postmen

### Design Principles

- **No registry.** The postman does not track which agents exist locally. It receives a message addressed to a path, attempts to write to that path. If the path doesn't exist, the message is silently dropped.
- **No discovery.** Peers are manually configured or addressed explicitly in messages.
- **No reliability guarantees.** UDP, fire-and-forget. If a message is lost, the agent can resend. This matches the email metaphor — letters sometimes get lost.
- **No authentication (v1).** Link-local and LAN use don't need it. Internet-facing postmen should add a shared-secret handshake in v2.
- **Stateless.** The postman can crash and restart with zero recovery needed.
- **Launchable by any LingTai CLI.** The TUI, portal, or a standalone command can start the postman. If one is already running on this machine, skip.

### Wire Protocol

```
UDP datagram:
┌──────────┬───────────┬────────────────────┐
│ magic(4) │ flags(1)  │ payload (zstd)     │
│ "LTPM"   │ 0x01=zstd │ compressed JSON    │
└──────────┴───────────┴────────────────────┘
```

- **Magic bytes:** `LTPM` (LingTai PostMan) — identifies the packet, allows filtering
- **Flags:** compression algorithm (0x01 = zstd, 0x00 = raw)
- **Payload:** The `message.json` content, optionally compressed

Max datagram size: ~1400 bytes (safe MTU). For messages exceeding this after compression, fall back to TCP on the same port (future, v2).

### Payload Format

The payload is a standard `message.json` with full absolute addresses:

```json
{
  "_mailbox_id": "uuid",
  "from": "[2001:db8::1]:/home/user/.lingtai/agent_a",
  "to": ["[2001:db8::2]:/home/other/.lingtai/agent_b"],
  "subject": "",
  "message": "Hello from across the internet",
  "type": "normal",
  "received_at": "2026-04-17T10:00:00Z",
  "identity": { "agent_name": "agent_a", ... }
}
```

### Receive Flow

```
UDP packet arrives
  → verify magic bytes "LTPM"
  → decompress payload
  → parse JSON → extract "to" field
  → for each recipient:
      → extract path from absolute address (strip host prefix)
      → attempt: os.MkdirAll(path/mailbox/inbox/<uuid>/)
      → attempt: os.WriteFile(path/mailbox/inbox/<uuid>/message.json)
      → if path doesn't exist: drop silently
```

### Send Flow (Outbox Watcher)

```
scan all known .lingtai/ dirs for mailbox/outbox/ entries
  → for each message.json:
      → parse "to" field
      → if IsRemoteAddress(to):
          → extract host from address
          → compress message
          → send UDP datagram to host:port
          → move message from outbox/ to sent/
      → if local:
          → deliver directly to recipient inbox (existing behavior)
          → move from outbox/ to sent/
```

### Launching the Postman

The postman should be launchable by any LingTai tool:

```bash
# Explicit launch
lingtai-tui postman start --port 7777

# Or as part of TUI startup (if not already running)
# The TUI checks for a running postman and starts one if absent
```

**Singleton enforcement:**
- Write a PID file at `~/.lingtai/postman.pid`
- On launch: check if PID is alive → if yes, skip
- On shutdown: remove PID file

**Configuration** (`~/.lingtai/postman.json`):
```json
{
  "port": 7777,
  "watch_dirs": [
    "/home/user/project-a/.lingtai",
    "/home/user/project-b/.lingtai"
  ]
}
```

Or: the postman auto-discovers `.lingtai/` dirs by scanning `~/.lingtai/projects.json` (which the TUI already maintains).

### IPv6 Specifics

- Bind to `[::]:7777` (all IPv6 interfaces)
- Also bind to `0.0.0.0:7777` for IPv4 fallback (dual-stack)
- China residential broadband: high-numbered ports (>1024) are generally open on IPv6
- Dynamic IPv6 prefix: agents may need to update their addresses periodically (future concern)

## Part 3: Agent Teleportation (Future)

With the postman in place, teleportation is a natural extension:

1. Agent A packs its state: `tar czf - .lingtai/agent_a/ | zstd`
2. Sends the archive to the remote postman via TCP (too large for UDP)
3. Remote postman unpacks into the target `.lingtai/` directory
4. Remote machine starts the agent: `python -m lingtai run <dir>`
5. Original agent can stop (teleport) or continue (clone/分身)

This requires a separate "teleport" message type and TCP fallback — out of scope for v1.

## Implementation Plan

### Phase 1: Explicit Address Modes

**Goal:** Make address modes explicit in the codebase without changing behavior.

1. Add `ParseAbsoluteAddress()` and `IsRemoteAddress()` to `fs/resolve.go` (both TUI and portal)
2. Update `ResolveAddress()` to handle the `host:path` format
3. Add `FormatAbsoluteAddress(host, path string)` helper
4. Update address display in TUI mail view to show host when present
5. All existing bare-name addresses continue to work — zero breaking changes

### Phase 2: Outbox Queue for Remote Mail

**Goal:** When an agent addresses mail to a remote address, queue it locally instead of attempting a local filesystem write.

1. `WriteMail()` checks `IsRemoteAddress(toAddr)`:
   - Remote → write to sender's `mailbox/outbox/` (already exists as a concept)
   - Local → existing direct-write behavior
2. Outbox messages include the full absolute address in `to` field

### Phase 3: The Postman Daemon

**Goal:** A standalone Go binary (`lingtai-postman`) or subcommand that relays mail over UDP.

1. UDP listener on configurable port (default 7777)
2. Outbox scanner: watches configured `.lingtai/` dirs for outbox entries
3. Receive handler: writes incoming messages to local inboxes by path
4. PID-file singleton enforcement
5. Zstd compression for wire format
6. CLI: `lingtai-tui postman [start|stop|status]`

### Phase 4: TUI/Portal Integration

**Goal:** The TUI and portal can show remote agents and their mail.

1. TUI startup auto-launches postman if not running
2. Mail view shows remote addresses with host prefix
3. Sending mail to a remote address routes through postman
4. Status indicator showing postman connectivity

## Open Questions

1. **Authentication:** Should v1 include a shared-secret handshake for internet-facing postmen? Or leave it for v2?
2. **Dynamic IPv6:** How should agents handle changing IPv6 prefixes on residential connections?
3. **Large messages:** TCP fallback for messages exceeding UDP MTU — when to implement?
4. **Outbox scanning interval:** How frequently should the postman check outboxes? Poll vs fsnotify?
5. **Cross-project routing:** Should the postman route between `.lingtai/` dirs on the same machine too, replacing direct filesystem writes?
6. **Naming:** `lingtai-postman` vs `lingtai-relay` vs embedding in `lingtai-tui postman`?
