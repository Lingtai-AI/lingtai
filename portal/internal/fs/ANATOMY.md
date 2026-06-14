# portal/internal/fs — Filesystem Reader (Portal)

> **Maintenance:** see `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`.

## What this is

The portal's read-focused window into a `.lingtai/` project directory. Same shape as `tui/internal/fs/` — agent manifests, heartbeats, mail, token ledgers, contacts, network discovery — but tailored to the portal's needs: it builds full `Network` payloads for the API, holds a `MailCache` for incremental mailbox polling, and — the single biggest difference from the TUI — reconstructs the **network topology tape** from historical `events.jsonl` and mailbox data for the replay timeline. The TUI doesn't do tape reconstruction; the portal exists to provide it.

## Components

### Types (`types.go`)
- `Location` struct (`types.go:5-12`) — cached ipinfo.io geolocation.
- `AgentNode` (`types.go:15-25`) — discovered agent: address, name, state, alive, capabilities, location. `WorkingDir` is internal-only (`json:"-"`).
- `AvatarEdge`, `ContactEdge`, `MailEdge` (`types.go:28-49`) — topology edge types.
- `NetworkStats` (`types.go:52-59`) — aggregate counts by state.
- `Network` (`types.go:62-69`) — full topology payload: nodes + edges + stats + lang.
- `MailMessage` (`types.go:72-89`) — inbox/archive/sent message schema, with identity card.

### Agent Reading (`agent.go`)
- `agentManifest` struct (`agent.go:13-23`) — raw `.agent.json` JSON shape.
- `ReadAgent(dir)` (`agent.go:26-53`) — reads `.agent.json` → `AgentNode`. Derives `IsHuman` from `admin: null`.
- `ParseCapabilities(raw)` (`agent.go:56-81`) — handles `[]string` (TUI-generated) and `[[name, {}], ...]` (live agent) formats.
- `ReadInitManifest(dir)` (`agent.go:90-100`) — returns the agent's manifest, preferring the kernel-published resolved artifact `system/manifest.resolved.json` (`readResolvedManifest`, `agent.go:104`; kernel issue #259) and falling back to raw `init.json` (`readRawInitManifest`, `agent.go:121`) when the artifact is absent/malformed. Either way `flattenManifest` (`agent.go:138`) hoists `llm.{model, provider, base_url}` and `soul.delay` to top-level.
- `WritePrompt(agentDir, content)` (`agent.go:157-159`) — writes `.prompt` signal file.
- `DiscoverAgents(baseDir)` (`agent.go:175-195`) — scans for subdirectories with `.agent.json`.
- `AgentStatus` struct + `ReadStatus(dir)` (`agent.go:197-224`) — runtime context/runtime from `.status.json`.
- `TokenTotals` + `AggregateTokens(dirs)` (`agent.go:227-248`) — sums token usage across agents.
- `SumTokenLedger(path)` (`agent.go:252-279`) — reads and sums a single `token_ledger.jsonl`.

### Heartbeat (`heartbeat.go`)
- `IsAlive(dir, thresholdSec)` (`heartbeat.go:11-21`) — reads `.agent.heartbeat`, returns true if fresher than threshold (2.0s for the portal).
- `IsAliveHuman()` (`heartbeat.go:24-26`) — always true.

### Mail (`mail.go`)
- `newMailboxID()` (`mail.go:32`) — builds `YYYYMMDDTHHMMSS-xxxx` short id matching the kernel's `_new_mailbox_id`.
- `ReadInbox(dir)`, `ReadArchive(dir)` (`mail.go:36-40`) — full-folder reads.
- `MailCache` struct + `NewMailCache(humanDir)` (`mail.go:46-55`) — incremental mailbox cache tracking inbox+seen mailbox-id directories.
- `MailCache.Refresh()` (`mail.go:66-91`) — immutable refresh: scans for new messages, merges, sorts by `ReceivedAt`. Safe for goroutine use.
- `prepareMailDirs(primaryParent, sentParent)` (`mail.go:176`) — allocates a short id and creates every mailbox leaf the send will write, retrying on collisions in any target folder.
- `WriteMail(recipientDir, senderDir, ...)` (`mail.go:214`) — writes message to inbox (local) or outbox (pseudo-agent/remote), plus sent copy. Allocates id via `prepareMailDirs`. Respects the pseudo-agent contract: if sender has `admin: null`, writes to outbox only.
- `isPseudoAgent(identity)` (`mail.go:279`) — `admin` nil or absent → true.

### Ledger (`ledger.go`)
- `ReadLedger(dir)` (`ledger.go:17-47`) — scans `delegates/ledger.jsonl` for `event: "avatar"` records, returns `AvatarEdge` pairs and resolved child directories.

### Location (`location.go`)
- `ResolveLocation()` (`location.go:23-48`) — queries `ipinfo.io/json`, returns cached `Location`.
- `LocationStale(loc, maxAge)` (`location.go:50-61`) — true if `ResolvedAt` is empty/unparseable or older than `maxAge`.
- `UpdateHumanLocation(humanDir)` (`location.go:63-103`) — resolves location if stale (>1h), writes atomically via temp+rename.

### Network (`network.go`)
- `BuildNetwork(baseDir)` (`network.go:8-76`) — discovers agents, reads ledgers+contacts+mail, builds the full `Network` payload (nodes, all edge types, stats). Heartbeat overrides state to SUSPENDED when agent isn't alive.
- `buildMailEdges(nodes, baseDir)` (`network.go:78-133`) — aggregates inbox+archive into `MailEdge` counts (direct/cc/bcc).
- `computeStats(nodes, mailEdges)` (`network.go:151-171`) — counts agents by state; sums total mails.

### Contacts (`contacts.go`)
- `ReadContacts(dir)` (`contacts.go:15-35`) — reads `mailbox/contacts.json`, resolves target addresses to absolute paths.

### Topology Reconstruction (`reconstruct.go`) — **portal-specific**
- `TapeFrame` struct (`reconstruct.go:14-18`) — timestamped `Network` snapshot.
- `ReconstructTape(baseDir)` (`reconstruct.go:39-354`) — the portal's defining capability: reads all agents' `logs/events.jsonl` and mailbox contents, replays them chronologically, and produces a sequence of `TapeFrame` snapshots using activity-driven sampling on a 3s grid. Frames are emitted at each `agent_state` event, each mail timestamp, each agent's first-seen time, and at least once per 60s during quiet stretches. `maxTs` clamps to the latest mutation-causing event (state change or mail) — heartbeats during idle tails don't extend the tape. Agents appear when their first event fires; mail accumulates frame-by-frame; avatar/contact edges are static pre-read. This is what drives the replay timeline — the TUI has no equivalent.
- `readEventsJSONL(agentDir)` (`reconstruct.go:358-383`) — parses `events.jsonl`, filters to `agent_state`, `heartbeat_start`, `refresh_start`.
- `mailTimestamp(msg)` (`reconstruct.go:385-401`) — extracts best timestamp (SentAt → ReceivedAt) as unix seconds.

### Signal (`signal.go`)
- Signal constants (`signal.go:11-15`) — `.sleep`, `.suspend`, `.interrupt`.
- `TouchSignal(dir, sig)`, `HasSignal(dir, sig)`, `CleanSignals(dir)` (`signal.go:17-30`) — signal file lifecycle.
- `SuspendAndWait(dir, timeout)` (`signal.go:34-46`) — touches `.suspend`, polls heartbeat until agent dies or timeout.

### Address Resolution (`resolve.go`)
- `ParseAddress(addr)` (`resolve.go:16-55`) — parses `localhost:/path` and `[host]:/path` formats.
- `IsRemoteAddress(addr)` (`resolve.go:57-61`) — true if host is non-localhost.
- `FormatAbsoluteAddress(host, path)` (`resolve.go:63-70`) — inverse of ParseAddress.
- `ResolveAddress(addr, baseDir)` (`resolve.go:76-84`) — host:path addresses returned as-is; relative names joined with `baseDir`.
- `RelativizeAddress(addr, baseDir)` (`resolve.go:86-98`) — strips `baseDir/` prefix from absolute paths.

## Connections

- **Called by** `portal/internal/api/` (handlers build `Network` payloads; replay calls `ReconstructTape`; mail composer calls `WriteMail`).
- **Reads** `.lingtai/<agent>/.agent.json`, `.agent.heartbeat`, `.status.json`, `system/manifest.resolved.json` (preferred over `init.json` when present), `init.json`, `logs/events.jsonl`, `logs/token_ledger.jsonl`, `delegates/ledger.jsonl`, `mailbox/contacts.json`, `mailbox/inbox/*/message.json`, `mailbox/archive/*/message.json`, `mailbox/sent/*/message.json`.
- **Writes** signal files (`.sleep`, `.suspend`, `.interrupt`, `.prompt`) and atomically updates human location in `.agent.json`. Writes outbound mail to inbox/outbox/sent directories.
- **Cross-reference** `tui/internal/fs/` shares the same read pattern for agent manifests, heartbeats, mail, and ledgers. The portal adds `reconstruct.go` (tape reconstruction — the TUI doesn't do this), `MailCache` for incremental polling (the TUI reads all mail fresh each time), and `WriteMail` for the portal's mail composer. Address resolution and signal writing are identical across both binaries.

## Composition

- **Parent:** `portal/internal/` (portal binary packages).
- **Siblings:** `api/`, `migrate/` — fs is the data layer they both consume.
- **Repo-root path:** `portal/internal/fs/`. Mirror of `tui/internal/fs/` under the same monorepo.

## State

All state is read from the project's `.lingtai/` directory. The portal only writes signal files and human location updates — it does not own the agent data it reads, which belongs to the kernel runtime.

## Notes

- **`reconstruct.go` is the portal-specific addition.** The TUI shows live topology; the portal reconstructs historical topology tapes from `events.jsonl` + mailbox data. The reconstruction replays agent states chronologically using activity-driven sampling on a 3s grid (a frame per `agent_state` event, per mail message, per agent's first-seen time, plus one heartbeat per 60s during quiet stretches) — not a dense uniform grid, so reconstruction stays O(events) rather than O(duration/3s). Agents appear when their first event fires and mail accumulates cumulatively. This is the data source for the `/replay` endpoint.
- **Mailbox id shape.** `WriteMail` allocates short, human-scannable ids of the form `YYYYMMDDTHHMMSS-xxxx` (20 chars, UTC, 4 hex chars of UUID4 entropy) via `newMailboxID`. This matches the kernel's `_new_mailbox_id` in `lingtai-kernel/src/lingtai_kernel/intrinsics/email/primitives.py` and the TUI's mirror in `tui/internal/fs/mail.go`, so directory names, `id`, and `_mailbox_id` look identical regardless of which side wrote the message. `prepareMailDirs` uses `os.Mkdir` (not `MkdirAll`) on each leaf so same-second collisions in any target folder surface as `fs.ErrExist` and trigger up to 8 regenerations without overwriting existing mail.
- **`MailCache`** tracks already-loaded messages via mailbox-id directory names, enabling incremental refreshes without re-reading the entire mailbox. It is immutable by convention — `Refresh()` returns a new cache rather than mutating the receiver, so it's safe to call from a goroutine.
- **`IsAlive`** uses a 2-second threshold. A stale heartbeat forces state to `SUSPENDED` in `BuildNetwork`.
- **Atomic writes** (location, signals) use temp-file + rename, matching the kernel's filesystem contract.
- **Capability parsing** handles both `[]string` (from TUI-generated presets) and tuple format (from live agents), so the portal works with projects the TUI hasn't touched.
