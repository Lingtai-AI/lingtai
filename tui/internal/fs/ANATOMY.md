---
related_files:
  - tui/ANATOMY.md
  - tui/internal/tui/ANATOMY.md
  - tui/internal/inventory/ANATOMY.md
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
  - tui/internal/fs/direct_mail.go
  - tui/internal/fs/direct_mail_test.go
  - tui/internal/fs/network.go
  - tui/internal/fs/network_test.go
  - tui/internal/fs/session.go
  - tui/internal/fs/session_persistence_role_test.go
  - tui/internal/fs/session_rebuild_test.go
  - tui/internal/fs/session_rebuild_offsets_test.go
  - tui/internal/fs/session_tail_test.go
  - tui/internal/fs/session_window_test.go
  - tui/internal/sqlitelog/event.go
  - tui/internal/sqlitelog/query_test.go
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

The TUI's filesystem window into an agent working directory (`<project>/.lingtai/<agent>/`). Agent state — manifest, heartbeat, mail, token ledger, location, network topology, chat history — is read through this package. The kernel owns agent state; the TUI's narrow writes are signal files, human outbox/location, and its derived human `logs/session.jsonl` replay cache.

## Components

| Symbol | Citation | Purpose |
|--------|----------|---------|
| **agent.go** | | |
| `ReadAgent(dir)` | `tui/internal/fs/agent.go:33` | reads `.agent.json` → `AgentNode` (durable agent_id, current address, name, state, is_human, capabilities, location) |
| `ParseCapabilities(raw)` | `tui/internal/fs/agent.go:63` | handles `[]string` and `[["name", {}], ...]` tuple formats |
| `CapabilitiesForDisplay(manifest)` | `tui/internal/fs/agent.go:100` | prepends intrinsic caps (`system, soul, email, psyche`) to manifest caps, deduped, for operator display (kanban/props) |
| `ReadInitManifest(dir)` | `tui/internal/fs/agent.go:124` | prefers `system/manifest.resolved.json`, falls back to `init.json`, and flattens `llm.*` + `soul.delay` |
| `WritePrompt` | `tui/internal/fs/agent.go:213` | writes `.prompt` signal file (TUI→agent injection) |
| `WriteInquiry` | `tui/internal/fs/agent.go:220` | writes `.inquiry` signal file; no-op if `.inquiry` or `.inquiry.taken` exists |
| `IsOrchestratorManifest(manifest)` | `tui/internal/fs/agent.go:249` | lower-level orchestrator role detector shared by TUI display logic and running-agent inventory |
| `DiscoverAgents(baseDir)` | `tui/internal/fs/agent.go:267` | scans for all subdirectories with `.agent.json` |
| `ReadStatus(dir)` | `tui/internal/fs/agent.go:332` | reads `.status.json` → `AgentStatus` (tokens, runtime) |
| `ReadContextStats(dir)` | `tui/internal/fs/agent.go:345` | summarizes retained `history/chat_history.jsonl`: entries, role counts, text input/output, tool calls/results, and per-tool distribution |
| `AggregateTokens(dirs)` | `tui/internal/fs/agent.go:474` | sums `TokenTotals` across multiple agent ledgers |
| `SumTokenLedger(path)` | `tui/internal/fs/agent.go:491` | sums a single main-agent `token_ledger.jsonl` → `TokenTotals`, skipping historical daemon-mirrored rows (`source=daemon`, `em_id`, or `run_id`) |
| `SumTokenLedgerByProvider` | `tui/internal/fs/agent.go:605` | groups main-agent ledger entries by derived provider name + recent N entries, skipping daemon-mirrored rows so `/kanban` main detail stays separate from daemon detail |
| `SumMoltSessionTokenLedger` | `tui/internal/fs/agent.go:645` | uses `logs/log.sqlite` `psyche_molt` boundaries when available (JSONL fallback), then sums cached non-daemon token-ledger windows for `/kanban` Ctrl+D current and last session API/cache stats, including Codex `codex_request_mode` counts (`ws_full` / `ws_incremental`) |
| `SumMoltSessionToolCalls` | `tui/internal/fs/agent.go:728` | counts lifecycle `tool_call` events in the SAME current/previous molt windows as `SumMoltSessionTokenLedger` (via `sqlitelog.QueryMoltSessionToolCallCounts`, JSONL fallback) for the `/kanban` Ctrl+D `tool_calls` + `tool_calls/api_call` rows; tool results are not counted. Freshness is keyed on authoritative `events.jsonl` (derived `log.sqlite` only when JSONL is absent), NOT the token-ledger cache, so event-only changes invalidate the count and SQLite fallback cannot pin a stale result |
| `SumSessionTokenLedgerBetween` | `tui/internal/fs/agent.go:889` | reusable `[since, before)` ledger-window summation helper used by molt-session stats and since-cutoff callers |
| **rebuild_marker.go** | | |
| `RecentRebuildTimes(agentDir, limit)` | `tui/internal/fs/rebuild_marker.go` | best-effort newest-first `psyche_molt` (molt) timestamps for `/kanban` Ctrl+D ledger separators; prefers `logs/log.sqlite` LIMIT query (`sqlitelog.QueryRecentMoltTimes`), falls back to tailing the last `tailScanLines` (1000) lines of `logs/events.jsonl` via `tailEventTimes`; missing/malformed logs yield no markers |
| `RecentRefreshCompleteTimes(agentDir, limit)` | `tui/internal/fs/rebuild_marker.go` | same contract as `RecentRebuildTimes` but for `refresh_complete` (/refresh context reconstruction) events (`sqlitelog.QueryRecentRefreshCompleteTimes` + tail fallback); rendered as the separate `context rebuilt` separator label |
| **jsonl.go** | | |
| `forEachJSONLLine(path, fn)` | `tui/internal/fs/jsonl.go:16` | streams JSONL files one line at a time without `ReadFile`/`strings.Split`, avoiding duplicate buffers and Scanner token limits for ledger/history hot paths |
| **daemon_ledger.go** | | |
| `DaemonLedgerSummary(agentDir, recentN)` | `tui/internal/fs/daemon_ledger.go:70` | single traversal returning both provider/backend totals (`map[string]TokenTotals`) and most-recent tagged per-call rows (`[]DaemonLedgerEntry`); one daemon.json read per run (typed `daemonCard` includes `backend` plus `cli_tokens`/`tokens` sub-structs), valid ledger rows retain backend in memory, CLI/legacy snapshots remain totals-only and use `daemonFallbackProvider` attribution |
| `DaemonRecentLedger(agentDir, recentN)` | `tui/internal/fs/daemon_ledger.go:165` | convenience wrapper — returns only the recent-rows half of `DaemonLedgerSummary` |
| `daemonFallbackProvider` | `tui/internal/fs/daemon_ledger.go:207` | derives a provider/backend label for runs with no per-call ledger: preset_provider → non-lingtai backend → model derivation → raw backend/model → "daemon" |
| `DeriveLedgerProvider` | `tui/internal/fs/agent.go:993` | maps endpoint host / model prefix → canonical provider name |
| **heartbeat.go** | | |
| `IsAlive(dir, thresholdSec)` | `tui/internal/fs/heartbeat.go:11` | reads `.agent.heartbeat` unix timestamp, returns `age < threshold` |
| `IsAliveHuman()` | `tui/internal/fs/heartbeat.go:24` | always `true` |
| **mail.go** | | |
| `newMailboxID()` | `tui/internal/fs/mail.go:33` | builds `YYYYMMDDTHHMMSS-xxxx` short id matching the kernel's `_new_mailbox_id` |
| `prepareMailDirs` | `tui/internal/fs/mail.go:55` | allocates a short id and creates every mailbox leaf the send will write, retrying on collisions in any target folder |
| `ReadInbox(dir)` | `tui/internal/fs/mail.go:93` | reads `mailbox/inbox/` → `[]MailMessage` |
| `ReadSent(dir)` | `tui/internal/fs/mail.go:97` | reads `mailbox/sent/` → `[]MailMessage` |
| `MailCache` | `tui/internal/fs/mail.go:104` | incremental refresh cache: outbox + inbox + sent merged |
| `NewMailCache(humanDir)` | `tui/internal/fs/mail.go:114` | creates cache; `Refresh()` returns updated copy (receiver not mutated) |
| `MailCache.Clone()` | `tui/internal/fs/mail.go:127` | deep-clones seen sets, message slices, recipients, attachments, and identity while preserving nil versus non-nil-empty shapes for accepted-snapshot publication |
| `WriteMail` | `tui/internal/fs/mail.go:328-391` | writes local mail to recipient inbox + sender sent (or human outbox for pseudo-agent); returns `ErrRemoteMailUnsupported` before mailbox allocation for remote addresses |
| **direct_mail.go** | | |
| `DirectTarget` / `DirectThreadKey` / `AddressFingerprint` | `tui/internal/fs/direct_mail.go:9-38` | target carries canonical project + target directories, durable manifest AgentID, and current route; thread identity hashes `(project, agent_id)`, while the address fingerprint is route-only |
| `NormalizeMailEndpoints` / `IsDirectMail` | `tui/internal/fs/direct_mail.go:40-149` | keeps lenient deduplication for topology, but direct membership requires one valid raw recipient, empty CC, distinct endpoints, exact current addresses, and matching supplied inbound `identity.agent_id` |
| **ledger.go** | | |
| `ReadLedger(dir)` | `tui/internal/fs/ledger.go:17` | reads `delegates/ledger.jsonl` → `[]AvatarEdge` + child dirs |
| **location.go** | | |
| `ResolveLocation()` | `tui/internal/fs/location.go:23` | queries `ipinfo.io/json` → `Location` |
| `LocationStale(loc, maxAge)` | `tui/internal/fs/location.go:52` | true if `ResolvedAt` exceeds `maxAge` |
| `UpdateHumanLocation(humanDir)` | `tui/internal/fs/location.go:65` | reads human `.agent.json`, resolves if stale, writes atomically |
| **network.go** | | |
| `BuildNetwork(baseDir)` | `tui/internal/fs/network.go:8` | full topology: nodes, avatar edges, contact edges, mail edges, stats |
| **activity.go** | | |
| `ComputeNetworkActivity(baseDir)` | `tui/internal/fs/activity.go:42` | lightweight non-human project activity badge: folds agent state, heartbeat liveness, `.status.json` activity evidence, and running daemons into active, daemon-active, idle, asleep, suspend |
| `hasStatusActivity(agentDir, now)` | `tui/internal/fs/activity.go:174` | treats heartbeat-live agents as active when status-snapshot evidence is fresh: `active_turn` via mtime/started_at/last_progress_at within 600s, or `last_progress_at` within 90s |
| `CountDaemons(agentDir)` | `tui/internal/fs/activity.go:238` | counts parseable `daemons/<run_id>/daemon.json` files; running daemons feed project daemon-active status and selected-agent running/total displays |
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
| `SessionPersistenceRole` / `SessionCache` | `tui/internal/fs/session.go:125-165` | separates the sole `MainAggregateWriter` from zero-safe `NoPersist`, independently of mutex-protected replay-window completeness and offsets |
| `NewSessionCache` | `tui/internal/fs/session.go:176-190` | pure in-memory construction with an explicit persistence role; creates no file or directory |
| `RebuildFromSources` / `RebuildFromSourcesInMemory` | `tui/internal/fs/session.go:195-213` | authoritative full ingest; write-through requests still pass through the cache's persistence role |
| `RebuildFromSourcesWindowedInMemory` / `Complete` / `ExactHistoryStats` | `tui/internal/fs/session.go:219-232`, `tui/internal/fs/session.go:417-428` | bounded newest-content ingest plus a separately invoked exact metadata count for the captured canonical JSONL source/horizon; completeness prevents partial-file truncation but does not grant write authority |
| `Persist` / `rewriteFile` / `append` | `tui/internal/fs/session.go:315-382` | both write primitives enforce `MainAggregateWriter`; bounded caches independently remain memory-only until complete |
| `Refresh` | `tui/internal/fs/session.go:2244-2251` | incremental poll from each source's last complete consumed record; `NoPersist` caches update memory without appending the shared aggregate |
| **project_hash.go** | | |
| `ProjectHash(projectPath)` | `tui/internal/fs/project_hash.go:9` | SHA-256 first 12 hex chars — used as the registry key for each project |
| **contacts.go** | | |
| `ReadContacts(dir)` | `tui/internal/fs/contacts.go:15` | reads `mailbox/contacts.json` → `[]ContactEdge` |
| **types.go** | | |
| `AgentNode` | `tui/internal/fs/types.go:15` | durable agent_id, current address, agent_name, nickname, state, alive, is_human, capabilities, location |
| `AvatarEdge`, `ContactEdge`, `MailEdge` | `tui/internal/fs/types.go:29-47` | graph edge types |
| `Network`, `NetworkStats` | `tui/internal/fs/types.go:50-67` | full topology + aggregate counts |
| `MailMessage` | `tui/internal/fs/types.go:70` | mailbox message schema; `Delivered` is transient (`json:"-"`) |
| `Location` | `tui/internal/fs/types.go:5` | city, region, country, timezone, loc, resolved_at |

## Connections

- **Called by `tui/internal/tui/`** — every Bubble Tea screen reads agent state through this package (network home, agent detail, mail viewer, kanban, session log).
- **Called by `tui/internal/inventory/`** — running-agent inventory enriches process rows with `.agent.json`, heartbeat, status PID, lock, admin, IM identity, and orchestrator-role metadata.
- **Reads from agent working directories** — `.agent.json`, `.agent.heartbeat`, `.status.json`, `mailbox/*/`, `logs/log.sqlite` (molt/session-boundary and diagnostic indexes, never canonical session replay authority), `logs/token_ledger.jsonl` (main rows only for agent totals/detail), `logs/events.jsonl`, `logs/soul_inquiry.jsonl`, `logs/soul_flow.jsonl`, `delegates/ledger.jsonl`, `mailbox/contacts.json`, `daemons/*/daemon.json`, `daemons/*/logs/token_ledger.jsonl`.
- **Writes signal files** (the only agent-owned files the TUI writes): `.sleep`, `.suspend`, `.interrupt`, `.prompt`, `.inquiry`, `.refresh`/`.refresh.taken`.
- **Writes human-owned/derived state** — local `WriteMail` writes recipient inbox + sender sent, or `human/mailbox/outbox/<mailbox-id>/` for pseudo-agent sends; remote addresses fail before any mailbox write. Only a complete `MainAggregateWriter` changes `human/logs/session.jsonl` (`tui/internal/fs/session.go:315-382`).
- **Calls `ipinfo.io`** — `ResolveLocation` makes an HTTP call; `UpdateHumanLocation` caches result in human's `.agent.json`.

## Composition

- **Parent:** `tui/internal/` (no own anatomy)
- **Subfolders:** none — flat package
- **Siblings:** `tui/internal/preset/ANATOMY.md`, `tui/internal/migrate/ANATOMY.md` — fs is a data layer, preset and migrate are logic layers

## State

- **Reads**: `.agent.json`, `.agent.heartbeat`, `.status.json`, `mailbox/inbox/*`, `mailbox/sent/*`, `logs/log.sqlite` (additive index), `logs/token_ledger.jsonl` (main rows only for agent totals/detail), `logs/events.jsonl`, `logs/soul_inquiry.jsonl`, `logs/soul_flow.jsonl`, `delegates/ledger.jsonl`, `mailbox/contacts.json`, `daemons/*/daemon.json`, `daemons/*/logs/token_ledger.jsonl`.
- **Writes**: signal files (`.sleep`, `.suspend`, `.interrupt`, `.prompt`, `.inquiry`), human `mailbox/outbox/*`, human `.agent.json` location field, and the TUI-derived human `logs/session.jsonl` replay cache only from `MainAggregateWriter` persist/append paths.

## Notes

- **Read-only for agent state.** This package is the TUI's window — it never writes agent-owned files except signal files. The kernel owns `.agent.json`, heartbeats, mailboxes, ledgers, logs. Do not add write paths for kernel-owned state.
- **Mailbox id shape.** `WriteMail` allocates short, human-scannable ids of the form `YYYYMMDDTHHMMSS-xxxx` (20 chars, UTC, 4 hex chars of UUID4 entropy) via `newMailboxID`. This matches the kernel's `_new_mailbox_id` in `lingtai-kernel/src/lingtai/kernel/intrinsics/email/primitives.py` and the portal's mirror in `portal/internal/fs/mail.go`, so directory names, `id`, and `_mailbox_id` look identical regardless of which side wrote the message. The directory name IS the id — `prepareMailDirs` uses `os.Mkdir` (not `MkdirAll`) on each leaf so collisions in any target folder surface as `fs.ErrExist` and trigger up to 8 regenerations without overwriting existing mail.
- **`Delivered` is transient.** `MailMessage.Delivered` is `json:"-"` — set by `MailCache.Refresh()` based on which folder the message was found in. Outbox → false; inbox/sent → true.
- **`MailCache` refresh and snapshot boundaries differ.** `Refresh()` returns a new cache without mutating the receiver, while `Clone()` is the explicit deep-copy boundary for accepted publication: nested recipient, attachment, and identity graphs cannot alias the live producer, and nil shapes are preserved.
- **Direct mail identity boundary.** `DirectTarget` separates stable identity (canonical project directory + manifest `agent_id`) from current routing (target directory + address); `DirectThreadKey` hashes only the stable pair, and `AddressFingerprint` is route-only. `NormalizeMailEndpoints` remains deliberately lenient for topology edges. `IsDirectMail` instead validates one raw recipient, rejects any CC, malformed/multi-entry envelope, empty/equal endpoints, or cross-address record, and on incoming mail requires any supplied nonblank `identity.agent_id` to match literally while allowing exact-address fallback for legacy mail without that field.
- **Session persistence role.** `MainAggregateWriter` is the only role authorized to mutate the compatibility aggregate `human/logs/session.jsonl`; zero-safe `NoPersist` is enforced inside both rewrite and append primitives. `complete` describes whether the in-memory history window can safely replace or extend a complete derived file—it is not write authorization.
- **Session cache reconstruction.** `RebuildFromSources` is idempotent — it re-ingests all mail + events + inquiries from offset 0, sorts by timestamp, and requests a role-gated `session.jsonl` rewrite; `RebuildFromSourcesInMemory` performs the same read/merge without filesystem writes for detached generation-gated work. Canonical `logs/events.jsonl` owns session content and completeness: the additive SQLite log's source identity and endpoint offsets do not prove interior continuity, so they are not used to declare a replay complete. Every path retains the last complete-record boundary it actually consumed, so trailing partial records and concurrent appends are retried by `Refresh` rather than leaked, duplicated, or skipped.
- **Windowed reconstruction and count metadata.** `RebuildFromSourcesWindowedInMemory` retains only the newest requested parser-produced session-event content window while loading mail/inquiries in full. `mail_page_size` directly owns that initial window and every later Ctrl+U increment. Empty/missing/wrong-type text rows do not spend content slots, while hidden `llm_call` and zero-token `llm_response` grouping carriers still do. The content path captures the canonical JSONL source/horizon but never runs a full-history aggregate. `ExactHistoryStats` is one async metadata task per activation/source/horizon: same-horizon Ctrl+U caches reuse it, while a genuinely newer horizon supersedes the old task. Accepted stats are cache/identity/generation/current-horizon-gated, reused by older-page caches, and incremented for parser-proven EOF refresh rows. JSONL content is read backward from EOF; top-level count/window metadata uses a structural fixed-buffer fast path across arbitrarily long string/nested payloads, enforces the same 10,000-container limit as `encoding/json`, and falls back to canonical one-record decoding whenever a bounded key/type/number lexeme or parser edge is declined. A cut legacy group retains only its nearest hidden `llm_response` marker. Increasing windows rescan the same canonical horizon, include every session row regardless of SQLite sparsity, and become complete only after reaching byte offset zero; parser-proven offsets, stable sort, and the shared completeness gate on both persistence and incremental disk append keep that convergence honest.
- **`parseEvent` event-type allow-list.** Only certain `events.jsonl` / `log.sqlite` types become `SessionEntry`s: `thinking`, `diary`, `text_input`, `text_output`, `tool_call`, `tool_result`, `insight`, `soul_flow`, `notification`, `aed`, and `apriori_summary`. Four kernel-side rename/promotion rules at ingest: `consultation_fire → soul_flow` (carries `fire_id` for voice-index inflation against `logs/soul_flow.jsonl`); `notification_pair_injected → notification` (carries `sources []string` and prefers the kernel-logged `summary` string for body, **plus an optional `meta *NotificationMeta`** with `current_time`, `context.{system_tokens,history_tokens,usage}`, and `injection_seq` — the kernel's `build_meta` snapshot at injection time, rendered as a faint footer line by `mail.go`; nil for events written before issue #40); `aed_attempt`/`aed_exhausted`/`aed_timeout → aed` (subtype written to `Source`, body recovered from raw `type` plus per-subtype fields — `attempt`/`error`, `attempts`/`error`, `seconds`); and `apriori_summary_generated`/`apriori_summary_cap_refused`/`apriori_summary_failed`/`apriori_summary_empty`/`apriori_summary_no_summarizer → apriori_summary` (summary metadata and generated text preserved for Ctrl+O rendering). To surface a new event type in the chat replay: extend the rename map (if needed), the allow-list in `parseEventMap` (the `switch eventType` in `tui/internal/fs/session.go`) and the `sqlitelog` session-event filter (`sessionEventFilterSQL` in `tui/internal/sqlitelog/event.go`), `extractSessionEventText`, and the renderer in `tui/internal/tui/mail.go`.
- **Provider derivation.** `DeriveLedgerProvider` uses endpoint host substring matching first, then model prefix fallback. Unknown endpoints surface the hostname so the UI still shows a breakdown.
- **Location is cached for 1 hour.** `UpdateHumanLocation` checks `LocationStale` with a 1-hour maxAge before calling `ipinfo.io`.
