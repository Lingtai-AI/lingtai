---
related_files:
  - tui/ANATOMY.md
  - tui/internal/tui/ANATOMY.md
  - tui/internal/preset/ANATOMY.md
  - tui/internal/migrate/ANATOMY.md
  - portal/internal/fs/ANATOMY.md
  - tui/internal/fs/types.go
  - tui/internal/fs/agent.go
  - tui/internal/fs/agent_test.go
  - tui/internal/fs/activity.go
  - tui/internal/fs/activity_test.go
  - tui/internal/fs/daemon_ledger.go
  - tui/internal/fs/daemon_ledger_test.go
  - tui/internal/fs/heartbeat.go
  - tui/internal/fs/heartbeat_test.go
  - tui/internal/fs/mail.go
  - tui/internal/fs/mail_test.go
  - tui/internal/fs/network.go
  - tui/internal/fs/network_test.go
  - tui/internal/fs/session.go
  - tui/internal/fs/session_rebuild_test.go
  - tui/internal/fs/session_tail_test.go
  - tui/internal/fs/signal.go
  - tui/internal/fs/signal_test.go
  - tui/internal/fs/resolve.go
  - tui/internal/fs/resolve_test.go
  - tui/internal/fs/ledger.go
  - tui/internal/fs/location.go
  - tui/internal/fs/project_hash.go
  - tui/internal/fs/contacts.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# fs

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in same-commit as code changes.

## What this is

The TUI's read-only window into an agent working directory (`<project>/.lingtai/<agent>/`). All agent state — manifest, heartbeat, mail, token ledger, location, network topology, chat history — is read through this package. The kernel writes; the TUI never writes agent-owned files (except signal files and human outbox mail).

## Components

| Symbol | Citation | Purpose |
|--------|----------|---------|
| **agent.go** | | |
| `ReadAgent(dir)` | `tui/internal/fs/agent.go:26` | reads `.agent.json` → `AgentNode` (address, name, state, is_human, capabilities, location) |
| `ParseCapabilities(raw)` | `tui/internal/fs/agent.go:56` | handles `[]string` and `[["name", {}], ...]` tuple formats |
| `CapabilitiesForDisplay(manifest)` | `tui/internal/fs/agent.go:90` | prepends intrinsic caps (`system, soul, email, psyche`) to manifest caps, deduped, for operator display (kanban/props) |
| `ReadInitManifest(dir)` | `tui/internal/fs/agent.go:112` | prefers `system/manifest.resolved.json`, falls back to `init.json`, and flattens `llm.*` + `soul.delay` |
| `WritePrompt` | `tui/internal/fs/agent.go:205` | writes `.prompt` signal file (TUI→agent injection) |
| `WriteInquiry` | `tui/internal/fs/agent.go:211` | writes `.inquiry` signal file; no-op if `.inquiry` or `.inquiry.taken` exists |
| `DiscoverAgents(baseDir)` | `tui/internal/fs/agent.go:240` | scans for all subdirectories with `.agent.json` |
| `ReadStatus(dir)` | `tui/internal/fs/agent.go:303` | reads `.status.json` → `AgentStatus` (tokens, runtime) |
| `ReadContextStats(dir)` | `tui/internal/fs/agent.go:320` | summarizes retained `history/chat_history.jsonl`: entries, role counts, text input/output, tool calls/results, and per-tool distribution |
| `AggregateTokens(dirs)` | `tui/internal/fs/agent.go:452` | sums `TokenTotals` across multiple agent ledgers |
| `SumTokenLedger(path)` | `tui/internal/fs/agent.go:469` | sums a single main-agent `token_ledger.jsonl` → `TokenTotals`, skipping historical daemon-mirrored rows (`source=daemon`, `em_id`, or `run_id`) |
| `SumTokenLedgerByProvider` | `tui/internal/fs/agent.go:582` | groups main-agent ledger entries by derived provider name + recent N entries, skipping daemon-mirrored rows so `/kanban` main detail stays separate from daemon detail |
| `SumMoltSessionTokenLedger` | `tui/internal/fs/agent.go:622` | uses `logs/log.sqlite` `psyche_molt` boundaries when available (JSONL fallback), then sums cached non-daemon token-ledger windows for `/kanban` Ctrl+D current and last session API/cache stats, including Codex `codex_request_mode` counts (`ws_full` / `ws_incremental`) |
| `SumSessionTokenLedgerBetween` | `tui/internal/fs/agent.go:696` | reusable `[since, before)` ledger-window summation helper used by molt-session stats and since-cutoff callers |
| **rebuild_marker.go** | | |
| `RecentRebuildTimes(agentDir, limit)` | `tui/internal/fs/rebuild_marker.go` | best-effort newest-first `psyche_molt` (molt) timestamps for `/kanban` Ctrl+D ledger separators; prefers `logs/log.sqlite` LIMIT query (`sqlitelog.QueryRecentMoltTimes`), falls back to tailing the last `tailScanLines` (1000) lines of `logs/events.jsonl` via `tailEventTimes`; missing/malformed logs yield no markers |
| `RecentRefreshCompleteTimes(agentDir, limit)` | `tui/internal/fs/rebuild_marker.go` | same contract as `RecentRebuildTimes` but for `refresh_complete` (/refresh context reconstruction) events (`sqlitelog.QueryRecentRefreshCompleteTimes` + tail fallback); rendered as the separate `context rebuilt` separator label |
| **jsonl.go** | | |
| `forEachJSONLLine(path, fn)` | `tui/internal/fs/jsonl.go:16` | streams JSONL files one line at a time without `ReadFile`/`strings.Split`, avoiding duplicate buffers and Scanner token limits for ledger/history hot paths |
| **daemon_ledger.go** | | |
| `DaemonRecentLedger(agentDir, recentN)` | `tui/internal/fs/daemon_ledger.go:40` | aggregates recent per-call token ledgers from `daemons/<run_id>/logs/token_ledger.jsonl`, newest first, tagged with daemon run id/handle/state for kanban Ctrl+D split lanes |
| `DeriveLedgerProvider` | `tui/internal/fs/agent.go:804` | maps endpoint host / model prefix → canonical provider name |
| **heartbeat.go** | | |
| `IsAlive(dir, thresholdSec)` | `tui/internal/fs/heartbeat.go:11` | reads `.agent.heartbeat` unix timestamp, returns `age < threshold` |
| `IsAliveHuman()` | `tui/internal/fs/heartbeat.go:24` | always `true` |
| **mail.go** | | |
| `newMailboxID()` | `tui/internal/fs/mail.go:33` | builds `YYYYMMDDTHHMMSS-xxxx` short id matching the kernel's `_new_mailbox_id` |
| `prepareMailDirs` | `tui/internal/fs/mail.go:50` | allocates a short id and creates every mailbox leaf the send will write, retrying on collisions in any target folder |
| `ReadInbox(dir)` | `tui/internal/fs/mail.go:88` | reads `mailbox/inbox/` → `[]MailMessage` |
| `ReadSent(dir)` | `tui/internal/fs/mail.go:92` | reads `mailbox/sent/` → `[]MailMessage` |
| `MailCache` | `tui/internal/fs/mail.go:99` | incremental refresh cache: outbox + inbox + sent merged |
| `NewMailCache(humanDir)` | `tui/internal/fs/mail.go:109` | creates cache; `Refresh()` returns updated copy (receiver not mutated) |
| `WriteMail` | `tui/internal/fs/mail.go:237` | writes to recipient inbox + sender sent (or human outbox for pseudo-agent); allocates id via `prepareMailDirs` |
| **ledger.go** | | |
| `ReadLedger(dir)` | `tui/internal/fs/ledger.go:17` | reads `delegates/ledger.jsonl` → `[]AvatarEdge` + child dirs |
| **location.go** | | |
| `ResolveLocation()` | `tui/internal/fs/location.go:23` | queries `ipinfo.io/json` → `Location` |
| `LocationStale(loc, maxAge)` | `tui/internal/fs/location.go:52` | true if `ResolvedAt` exceeds `maxAge` |
| `UpdateHumanLocation(humanDir)` | `tui/internal/fs/location.go:65` | reads human `.agent.json`, resolves if stale, writes atomically |
| **network.go** | | |
| `BuildNetwork(baseDir)` | `tui/internal/fs/network.go:8` | full topology: nodes, avatar edges, contact edges, mail edges, stats |
| **activity.go** | | |
| `ComputeNetworkActivity(baseDir)` | `tui/internal/fs/activity.go:34` | lightweight non-human project activity badge: active, daemon-active, idle, asleep, suspend; only counts daemon runs for heartbeat-live parent agents |
| `CountDaemons(agentDir)` | `tui/internal/fs/activity.go:110` | counts parseable `daemons/<run_id>/daemon.json` files for selected-agent daemon running/total displays |
| **resolve.go** | | |
| `ParseAddress(addr)` | `tui/internal/fs/resolve.go:16` | `"localhost:/path"` or `"[ipv6]:/path"` → `(host, path, ok)` |
| `IsRemoteAddress(addr)` | `tui/internal/fs/resolve.go:62` | true if non-localhost host prefix |
| `ResolveAddress(addr, baseDir)` | `tui/internal/fs/resolve.go:81` | relative name → absolute path; host:path → as-is |
| `RelativizeAddress(addr, baseDir)` | `tui/internal/fs/resolve.go:94` | absolute → relative by stripping `baseDir/` prefix |
| **signal.go** | | |
| `Signal` type | `tui/internal/fs/signal.go:9` | `SignalSleep`, `SignalSuspend`, `SignalInterrupt` |
| `TouchSignal`, `HasSignal` | `tui/internal/fs/signal.go:17,21` | write/check `.sleep` / `.suspend` / `.interrupt` |
| `CleanSignals(dir)` | `tui/internal/fs/signal.go:32` | remove all signal + refresh handshake files |
| `SuspendAndWait` | `tui/internal/fs/signal.go:43` | touch `.suspend`, poll heartbeat until dead or timeout |
| **session.go** | | |
| `SessionCache` | `tui/internal/fs/session.go:36` | append-only cache backed by `session.jsonl`; tails mail + events + inquiries |
| `NewSessionCache` | `tui/internal/fs/session.go:99` | creates cache, calls `RebuildFromSources` after construction |
| `RebuildFromSources` | `tui/internal/fs/session.go:115` | full ingest from mail cache + SQLite/JSONL event replay + soul_inquiry.jsonl + soul_flow.jsonl |
| `Refresh` | `tui/internal/fs/session.go:1089` | incremental poll of all three sources |
| **project_hash.go** | | |
| `ProjectHash(projectPath)` | `tui/internal/fs/project_hash.go:9` | SHA-256 first 12 hex chars — used as the registry key for each project |
| **contacts.go** | | |
| `ReadContacts(dir)` | `tui/internal/fs/contacts.go:15` | reads `mailbox/contacts.json` → `[]ContactEdge` |
| **types.go** | | |
| `AgentNode` | `tui/internal/fs/types.go:15` | address, agent_name, nickname, state, alive, is_human, capabilities, location |
| `AvatarEdge`, `ContactEdge`, `MailEdge` | `tui/internal/fs/types.go:28-46` | graph edge types |
| `Network`, `NetworkStats` | `tui/internal/fs/types.go:49-66` | full topology + aggregate counts |
| `MailMessage` | `tui/internal/fs/types.go:69` | mailbox message schema; `Delivered` is transient (`json:"-"`) |
| `Location` | `tui/internal/fs/types.go:5` | city, region, country, timezone, loc, resolved_at |

## Connections

- **Called by `tui/internal/tui/`** — every Bubble Tea screen reads agent state through this package (network home, agent detail, mail viewer, kanban, session log).
- **Reads from agent working directories** — `.agent.json`, `.agent.heartbeat`, `.status.json`, `mailbox/*/`, `logs/log.sqlite` (indexed event replay when coverage is safe), `logs/token_ledger.jsonl` (main rows only for agent totals/detail), `logs/events.jsonl`, `logs/soul_inquiry.jsonl`, `logs/soul_flow.jsonl`, `delegates/ledger.jsonl`, `mailbox/contacts.json`, `daemons/*/daemon.json`, `daemons/*/logs/token_ledger.jsonl`.
- **Writes signal files** (the only agent-owned files the TUI writes): `.sleep`, `.suspend`, `.interrupt`, `.prompt`, `.inquiry`, `.refresh`/`.refresh.taken`.
- **Writes human outbox mail** — `WriteMail` for human (pseudo-agent) writes to `human/mailbox/outbox/<mailbox-id>/`.
- **Calls `ipinfo.io`** — `ResolveLocation` makes an HTTP call; `UpdateHumanLocation` caches result in human's `.agent.json`.

## Composition

- **Parent:** `tui/internal/` (no own anatomy)
- **Subfolders:** none — flat package
- **Siblings:** `tui/internal/preset/ANATOMY.md`, `tui/internal/migrate/ANATOMY.md` — fs is a data layer, preset and migrate are logic layers

## State

- **Reads (never writes)**: `.agent.json`, `.agent.heartbeat`, `.status.json`, `mailbox/inbox/*`, `mailbox/sent/*`, `logs/log.sqlite` (additive index), `logs/token_ledger.jsonl` (main rows only for agent totals/detail), `logs/events.jsonl`, `logs/soul_inquiry.jsonl`, `logs/soul_flow.jsonl`, `delegates/ledger.jsonl`, `mailbox/contacts.json`, `daemons/*/daemon.json`, `daemons/*/logs/token_ledger.jsonl`.
- **Writes**: signal files (`.sleep`, `.suspend`, `.interrupt`, `.prompt`, `.inquiry`), human `mailbox/outbox/*`, human `.agent.json` location field.

## Notes

- **Read-only for agent state.** This package is the TUI's window — it never writes agent-owned files except signal files. The kernel owns `.agent.json`, heartbeats, mailboxes, ledgers, logs. Do not add write paths for kernel-owned state.
- **Mailbox id shape.** `WriteMail` allocates short, human-scannable ids of the form `YYYYMMDDTHHMMSS-xxxx` (20 chars, UTC, 4 hex chars of UUID4 entropy) via `newMailboxID`. This matches the kernel's `_new_mailbox_id` in `lingtai-kernel/src/lingtai_kernel/intrinsics/email/primitives.py` and the portal's mirror in `portal/internal/fs/mail.go`, so directory names, `id`, and `_mailbox_id` look identical regardless of which side wrote the message. The directory name IS the id — `prepareMailDirs` uses `os.Mkdir` (not `MkdirAll`) on each leaf so collisions in any target folder surface as `fs.ErrExist` and trigger up to 8 regenerations without overwriting existing mail.
- **`Delivered` is transient.** `MailMessage.Delivered` is `json:"-"` — set by `MailCache.Refresh()` based on which folder the message was found in. Outbox → false; inbox/sent → true.
- **`MailCache` is copy-on-refresh.** `Refresh()` returns a new `MailCache`; the receiver is not mutated. Safe for goroutine use.
- **Session cache reconstruction.** `RebuildFromSources` is idempotent — it re-ingests all mail + events + inquiries from offset 0, sorts by timestamp, and rewrites `session.jsonl`. It prefers `logs/log.sqlite` for session-relevant event rows when the index covers the whole JSONL, or uses a hybrid prefix-JSONL + SQLite tail when the index starts mid-file but reaches near EOF; otherwise it falls back to authoritative `logs/events.jsonl`. JSONL remains the source of truth; the SQLite/hybrid path is a best-effort acceleration and may omit a few rows if the additive index missed individual JSONL entries. Incremental `Refresh` still tails JSONL from EOF offsets.
- **`parseEvent` event-type allow-list.** Only certain `events.jsonl` types become `SessionEntry`s: `thinking`, `diary`, `text_input`, `text_output`, `tool_call`, `tool_result`, `insight`, `soul_flow`, `notification`, `aed`. Three kernel-side rename rules at ingest: `consultation_fire → soul_flow` (carries `fire_id` for voice-index inflation against `logs/soul_flow.jsonl`); `notification_pair_injected → notification` (carries `sources []string` and prefers the kernel-logged `summary` string for body, **plus an optional `meta *NotificationMeta`** with `current_time`, `context.{system_tokens,history_tokens,usage}`, `stamina_left_seconds`, and `injection_seq` — the kernel's `build_meta` snapshot at injection time, rendered as a faint footer line by `mail.go`; nil for events written before issue #40); `aed_attempt`/`aed_exhausted`/`aed_timeout → aed` (subtype written to `Source`, body recovered from raw `type` plus per-subtype fields — `attempt`/`error`, `attempts`/`error`, `seconds`). To surface a new event type in the chat replay: extend the rename map (if needed), the allow-list in `parseEvent` (`tui/internal/fs/session.go:598`) and `sqlitelog` session-event filter (`tui/internal/sqlitelog/event.go:102`), `extractSessionEventText`, and the renderer in `tui/internal/tui/mail.go`.
- **Provider derivation.** `DeriveLedgerProvider` uses endpoint host substring matching first, then model prefix fallback. Unknown endpoints surface the hostname so the UI still shows a breakdown.
- **Location is cached for 1 hour.** `UpdateHumanLocation` checks `LocationStale` with a 1-hour maxAge before calling `ipinfo.io`.
