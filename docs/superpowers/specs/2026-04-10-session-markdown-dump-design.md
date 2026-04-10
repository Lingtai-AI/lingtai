# Session.jsonl Hourly Markdown Dump

**Date:** 2026-04-10
**Status:** Draft

## Problem

The TUI now maintains `session.jsonl` as the source of truth for the chat view. The future secretary agent needs to consume this data as human-readable markdown transcripts, organized into hourly slices, stored at a well-known location keyed by project.

## Solution

Add a pure projection step to `SessionCache`: after appending a new entry, check if the entry's timestamp has crossed an hour boundary. If so, render all entries for the completed hour as markdown and write to disk — but only if the content differs from any existing file at that path.

### Data Flow

```
session.jsonl append
  → new entry's hour > previous entry's hour?
    → yes → slice entries for the completed hour
           → render to markdown (full verbose TUI view)
           → compare with existing file
           → write if different, discard if identical
    → no  → nothing
```

### Trigger

The dump is triggered naturally by the hour boundary crossing. No timer, no polling, no offset tracking. When a new entry is appended whose hour (truncated to UTC hour) differs from the previous entry's hour, the previous hour is complete.

Empty hours produce nothing. If no entries arrive during an hour, no file is created.

### File Layout

```
~/.lingtai-tui/brief/
  <project-hash>/
    history/
      2026-04-10-14.md
      2026-04-10-15.md
      ...
```

- `<project-hash>` = first 12 hex chars of SHA-256 of the absolute project path
- Filename format: `YYYY-MM-DD-HH.md` where HH is the UTC hour (00–23)

## Markdown Format

The markdown mirrors the full verbose TUI view — all entry types included (mail, thinking, diary, text_input, text_output, tool_call, tool_result, insight). This is what the secretary agent reads.

```markdown
# Session — 2026-04-10 14:00–15:00 UTC

**human** 14:02 → agent │ Re: hello
Hi there

---

[thinking] Let me consider...

[tool_call] email({action: check})

[tool_result] email → ok 250ms

---
★ insight
The user prefers...
---

/btw › What does the user think about X?
The user seems to value...
---
```

### Rendering Rules

| Entry Type | Markdown Format |
|---|---|
| `mail` | `**{from}** {HH:MM} → {to} │ Re: {subject}\n{body}` (subject omitted if empty) |
| `thinking`, `diary`, `text_input`, `text_output` | `[{type}] {body}` |
| `tool_call` | `[tool_call] {body}` |
| `tool_result` | `[tool_result] {body}` |
| `insight` (auto) | `---\n★ insight\n{body}\n---` |
| `insight` (human /btw) | `---\n/btw › {question}\n{body}\n---` |

Attachments on mail entries are listed below the body:
```
Attachments:
  [1] path/to/file.txt
```

## Implementation

### New File: `tui/internal/fs/session_dump.go`

Keeps dump logic separate from session.go's ingestion logic.

#### Functions

- `DumpHourlyMarkdown(entries []SessionEntry, projectPath string)` — public entry point. Groups entries by UTC hour, renders each hour to markdown, compares with existing file, writes if different.
- `projectHash(absPath string) string` — SHA-256 first 12 hex chars of the absolute path.
- `briefDir(projectHash string) string` — returns `~/.lingtai-tui/brief/<hash>/history/`.
- `renderHourMarkdown(entries []SessionEntry, hour time.Time) string` — renders a slice of entries to the markdown format above.
- `renderMailEntry(e SessionEntry) string` — renders a single mail entry.
- `renderEventEntry(e SessionEntry) string` — renders thinking/tool/diary entries.
- `renderInsightEntry(e SessionEntry) string` — renders insight entries.

### Modified File: `tui/internal/fs/session.go`

In the `append()` method, after appending entries, check if any new entry crosses an hour boundary. If so, call `DumpHourlyMarkdown()`.

#### Hour Boundary Detection

Add a field `lastHour time.Time` (truncated to hour) to `SessionCache`. On `loadExisting()`, set it from the last entry's timestamp. On `append()`, for each new entry, parse its timestamp and check if `entry.Hour != sc.lastHour`. If crossed, trigger dump for the completed hour.

### Modified File: `tui/internal/tui/mail.go`

Pass the project path (absolute `.lingtai/` parent directory) to `SessionCache` so it knows the project hash. The `MailModel` already has access to the lingtai directory path.

## Idempotency

The dump is idempotent by design:
1. Render markdown for the hour
2. Read existing file at the target path (if any)
3. Compare content
4. Write only if different

This means if session.jsonl is rebuilt (e.g. curated event set, or re-ingestion after clearing), the dump reruns and only overwrites files whose content actually changed.

## What is NOT in This PR

- Secretary agent
- Brief system prompt section
- Any kernel changes
- Reading/consuming the markdown files

## Verification

1. `cd tui && make build`
2. Run TUI, send messages across an hour boundary
3. Verify `~/.lingtai-tui/brief/<hash>/history/YYYY-MM-DD-HH.md` is created
4. Verify content matches the full verbose TUI view
5. Verify empty hours produce no file
6. Verify re-running with same session.jsonl doesn't rewrite identical files
