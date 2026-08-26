---
related_files:
  - tui/ANATOMY.md
  - tui/internal/tui/ANATOMY.md
  - tui/internal/streamprogress/client.go
  - tui/internal/streamprogress/client_test.go
  - tui/internal/tui/stream_progress.go
  - tui/internal/tui/stream_progress_test.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. The discovery arithmetic, schema string, and path here are
  byte-for-byte the kernel's (lingtai-kernel
  src/lingtai/kernel/stream_progress/CONTRACT.md); change them together and
  keep the shared known vectors in client_test.go and the kernel's
  tests/test_stream_progress.py identical.
---

# streamprogress

> **Maintenance:** update this file in the same commit as code changes.

`streamprogress` is the TUI's read-only loopback client for the kernel-owned,
consumer-neutral stream-progress API (`lingtai.stream-progress/v1`). While an
agent streams an LLM response the kernel keeps a RAM-only counter — how many
Unicode characters have arrived, never the text — and serves it as one JSON
snapshot on `GET /v1/stream-progress` bound only to `127.0.0.1`. No port is
written anywhere: this package derives the same ordered candidate list the
publisher binds from the agent's stable `agent_id`, so a restarted TUI reattaches
to a living agent with no shared file. It is the first consumer of that API;
it owns no rendering.

## Components

| Component | File | Purpose |
|---|---|---|
| `Schema`, `Path`, `DefaultTimeout` | `tui/internal/streamprogress/client.go` | the v1 schema string, the one resource path, and the per-probe timeout |
| `Seed` / `CandidatePorts` | `tui/internal/streamprogress/client.go` | `uint16_be(SHA256(schema+"\0"+agent_id)[0:2])` and candidate `i = 41000 + ((seed + i*7919) mod 20000)` for `i = 0..7` — byte-for-byte the kernel's `candidate_ports` |
| `Snapshot` / `EstimatedTokens` | `tui/internal/streamprogress/client.go` | the seven documented fields (no text) and the documented integer `streamed_chars / 4` estimate |
| `ParseSnapshot` / `rejectDuplicateKeys` | `tui/internal/streamprogress/client.go` | strict frozen-v1 body validation: exactly one JSON object with exactly the seven documented fields — unknown fields (including a forbidden `text`), duplicate keys, and trailing data are rejected — exact schema, exact `agent_id`, unsigned `generation`, non-negative `streamed_chars`/`updated_unix_ms`, positive `pid` |
| `Client` (`NewClient`, `Fetch`, `CachedPort`, `Forget`) | `tui/internal/streamprogress/client.go` | loopback-only `http.Client` that never follows redirects (`ErrRedirect`) and keeps no connections alive; probes the cached port first, otherwise the candidates in order (skipping the port that just failed as the cache); accepts only a valid matching body of at most `maxBodyBytes` (64 KiB) — an oversized body is rejected whole, never truncated and parsed; caches the port in RAM; forgets and rescans after any failure |

## Composition

- **Upstream caller:** `tui/internal/tui/stream_progress.go` — Mail's
  poll-path adapter. It calls `Client.Fetch` only inside a `tea.Cmd` scheduled
  from the poll tick (never from `View()`), gates the result by Mail generation
  and visible-recipient identity, and renders `· N tok downloaded` beside
  `Active N s`. See `tui/internal/tui/ANATOMY.md`.
- **External dependency:** the kernel agent process (`lingtai-kernel`
  `src/lingtai/adapters/stream_progress.py`) serving loopback only. A missing,
  refused, foreign, or malformed endpoint is an ordinary `ok=false` — the
  consumer shows nothing and retries on the next poll.

## Invariants

1. Loopback only: the client dials `127.0.0.1` and nothing else, and never
   follows a redirect — a foreign service on a candidate port cannot send it
   anywhere; there is no remote, authenticated, or write operation.
2. Identity and shape are exact: a body whose `agent_id` differs from the
   requested id is rejected even when the schema is valid, so two agents'
   candidate ranges can overlap safely; a body with any field beyond the
   frozen seven (a `text` field above all), a duplicate key, or trailing data
   is not a v1 snapshot and is rejected rather than partially read; a body
   larger than 64 KiB is rejected whole, so trailing bytes past the read cap
   can never hide behind a valid prefix.
3. Memory only: the accepted port lives in the `Client` map for the process
   lifetime; no snapshot or port is ever persisted.
4. Never on the render path: `Fetch` is I/O and is only ever invoked from a
   Bubble Tea command.
5. Shared vectors: `client_test.go` pins the same known agent-id → ports
   vectors as the kernel's `tests/test_stream_progress.py`; a change to the
   arithmetic must update both.
