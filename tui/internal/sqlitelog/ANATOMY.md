---
related_files:
  - tui/ANATOMY.md
  - tui/internal/fs/ANATOMY.md
  - tui/internal/tui/ANATOMY.md
  - tui/internal/sqlitelog/event.go
  - tui/internal/sqlitelog/molt.go
  - tui/internal/sqlitelog/molt_cache_test.go
  - tui/internal/sqlitelog/molt_recent_test.go
  - tui/internal/sqlitelog/query.go
  - tui/internal/sqlitelog/query_test.go
  - tui/internal/sqlitelog/refresh_recent_test.go
  - tui/internal/sqlitelog/tool_call_counts_test.go
  - tui/internal/fs/session.go
  - tui/internal/fs/rebuild_marker.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# sqlitelog

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in the same commit as code changes.

`sqlitelog` is the TUI's read-only, `sqlite3`-CLI-backed reader for the kernel's per-agent event database (`<agent>/logs/log.sqlite`). It exists so screens that need a *bounded* slice of a long history — the newest notification blocks, the current/previous molt window, recent tool-call counts — can ask the index for exactly that slice instead of tailing `events.jsonl`. It is an additive accelerator: canonical `logs/events.jsonl` remains the source of truth for session content and completeness, and every entry point here degrades to "no rows" so callers can fall back.

## Components

| Symbol | Citation | Purpose |
|---|---|---|
| `DBPath` / `Exists` | `tui/internal/sqlitelog/query.go:421,433` | resolves `<agentDir>/logs/log.sqlite` and reports whether the index is present at all |
| `SessionEventRow` / `ErrorEvent` | `tui/internal/sqlitelog/event.go:20,27` | the row shapes handed back to `internal/fs` session reconstruction and diagnostics |
| `sessionEventFilterSQL` / `sessionEventFieldsSQL` | `tui/internal/sqlitelog/event.go:102,109` | the allow-list of event types that may become session entries, and the projection that strips oversized `tool_result` payloads at the SQL layer. Keep this in sync with `parseEventMap`'s allow-list in `tui/internal/fs/session.go` |
| `EventsIndexCoverage` + `HasRows` / `StartsAtBeginning` / `TailNearEOF` | `tui/internal/sqlitelog/event.go:123-144`, `tui/internal/sqlitelog/event.go:193` | the honesty gate: how much of the JSONL byte range the index actually covers, so a sparse database can never be mistaken for a complete replay |
| `StreamSessionEvents` / `StreamSessionEventsWindow` / `StreamSessionEventsOffsetRange` | `tui/internal/sqlitelog/event.go:230,267,364` | callback-per-row streaming (full, newest-N window, explicit offset range) so no caller materializes the whole history |
| `QueryWindowGroupBoundary` | `tui/internal/sqlitelog/event.go:312` | finds the nearest legacy `llm_call`/`llm_response` grouping boundary for a window so a page never cuts a group in half |
| `QueryErrorEvents` / `HasTUIClearCompletionEvent` | `tui/internal/sqlitelog/event.go:386,412` | diagnostic error listing and the `/clear` handshake completion probe |
| `QueryMoltSessionWindows` | `tui/internal/sqlitelog/molt.go:99` | resolves current/previous `psyche_molt` boundaries — the time windows `/kanban` Ctrl+D session stats are summed over |
| `QueryMoltSessionToolCallCounts` | `tui/internal/sqlitelog/molt.go:244` | counts lifecycle `tool_call` events inside those same windows |
| `QueryRecentMoltTimes` / `QueryRecentRefreshCompleteTimes` | `tui/internal/sqlitelog/molt.go:334`, `tui/internal/sqlitelog/molt.go` | newest-first boundary timestamps rendered as ledger separators by `tui/internal/fs/rebuild_marker.go` |
| notification readers | `tui/internal/sqlitelog/query.go:100,350,440,449,464,482` | `QueryNotificationBlocks`, `QueryNotificationBlockSnapshots`, `QueryNotifications`, and the by-id / before / after pivot queries backing `/notification` paging |
| `PrettyFields` | `tui/internal/sqlitelog/query.go:499` | renders a notification event's JSON payload for the detail view |

## Connections

- **Called by `tui/internal/fs/`** — session reconstruction (`session.go`), molt/refresh ledger separators (`rebuild_marker.go`), and molt-window token/tool-call aggregation all consult this package first and fall back to JSONL.
- **Called by `tui/internal/tui/`** — `/notification` pages notification blocks just-in-time from the database rather than from stale `.notification/` snapshots.
- **Shells out to the host `sqlite3` binary.** There is no cgo driver and no Go SQLite dependency; rows come back as separator-delimited text (`sqliteRowSeparator`, `tui/internal/sqlitelog/event.go:16`) and are parsed here.
- **Reads kernel-owned state only.** The database is written by the Python kernel; this package never writes, migrates, or vacuums it.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`)
- **Subfolders:** none — flat package
- **Siblings:** `tui/internal/fs/` is the only substantial consumer and owns the fallback policy.

## Notes

- **Read-only, fail-soft.** A missing `sqlite3` binary, a missing/locked/corrupt database, or a query timeout must degrade to "no rows" (or a returned error the caller treats as such), never a TUI failure. Molt-window queries are additionally bounded by `moltSessionWindowQueryTimeout` / `moltSessionWindowBusyTimeoutMS` and cached for `moltSessionWindowCacheTTL` (`tui/internal/sqlitelog/molt.go:22,32,42`).
- **Coverage before content.** Never answer a completeness question from this index. `EventsIndexCoverage` exists because source identity and endpoint offsets do not prove interior continuity — see the "Session cache reconstruction" note in `tui/internal/fs/ANATOMY.md`.
- **Adding an event type is a two-file change.** Extend `sessionEventFilterSQL` here *and* the `parseEventMap` allow-list in `tui/internal/fs/session.go`, or the type will appear from only one of the two ingest paths.
