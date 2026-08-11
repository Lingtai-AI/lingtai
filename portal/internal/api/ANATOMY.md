---
related_files:
  - portal/ANATOMY.md
  - portal/internal/fs/ANATOMY.md
  - portal/internal/api/server.go
  - portal/internal/api/server_test.go
  - portal/internal/api/handlers.go
  - portal/internal/api/handlers_test.go
  - portal/internal/api/replay.go
  - portal/internal/api/replay_test.go
  - portal/main.go
  - portal/embed.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# portal/internal/api

> **Maintenance:** see the `lingtai-tui-anatomy` skill. **Coding agents** update this file in the same commit as code changes.

HTTP server for `lingtai-portal`: serves the embedded React SPA on `/` and a JSON REST API on `/api/*`. Manages a live topology tape (3-second snapshots appended as JSONL during recording; reconstructed tapes use activity-driven sampling on the same 3s grid), compressed hourly replay caches, and on-the-fly reconstruction from source events.

## Components

### Server (`server.go`)

- **`portal/internal/api/server.go:20-28`** — `Server` struct. Wraps `http.Server` with bound `host`/`port`, `baseDir`, `cancel`/`done` for the recording goroutine.
- **`portal/internal/api/server.go:35-52`** — `NewServer(baseDir, staticFS)`. Registers 6 API routes (`portal/internal/api/server.go:37-42`) and mounts `staticFS` at `/` (`portal/internal/api/server.go:43`). Routes fixed before the Server is returned. `NewServer` also resolves the rebuild authorization token (see `rebuildTokenFor`) and passes it to `NewRebuildHandler`.
- **`portal/internal/api/server.go:52-72`** — `Start(portFile, host, fixedPort)`. Resolves an empty host to `127.0.0.1`, listens with `net.JoinHostPort`, stores the effective host, writes only the bound port to `portFile`, and serves in a goroutine.
- **`portal/internal/api/server.go:74-121`** — `StartRecording(baseDir)`. Background goroutine with a 3-second ticker (`portal/internal/api/server.go:82`). On first run: checks `needsReconstruction`, rebuilds tape from source via `agentfs.ReconstructTape` **while holding `TopologyMu`** (the lock is acquired before source scanning or frame allocation begins), writes replay caches through `writeReconstructedReplay` (`portal/internal/api/server.go:86-97`), then records complete `agentfs.BuildNetwork` snapshots immediately and on every tick via `AppendTopology` (`portal/internal/api/server.go:100-114`).
- **`portal/internal/api/server.go:123-173`** — `Port`, `Host`, `URL`, and host helpers. `URL()` keeps `http://localhost:<port>` for loopback and wildcard binds, but renders explicit named/non-loopback hosts directly. `ExternalAccessWarning()` reports unauthenticated non-loopback/wildcard binds for the CLI.
- **`portal/internal/api/server.go:175-186`** — `Stop(ctx)`. Cancels the recording goroutine, waits for `s.done`, shuts down the HTTP server.
- **`portal/internal/api/server.go:188-213`** — `needsReconstruction(path)`. Returns true if `topology.jsonl` is missing, empty, or uses the old format (lacking `direct`/`cc`/`bcc` fields on mail edges). Runs under `TopologyMu` and inspects only the **final line** via `lastLine` (a bounded backward tail read) so startup does not scale with all retained history.
- **`portal/internal/api/server.go:225-310`** — `lastLine(path, maxTail)` / `scanLastLine(f)`. Reads only the last `maxTapeTailBytes` of a tape to locate its final non-empty line (memory-bounded forward fallback for pathologically long final lines).
- **`portal/internal/api/server.go:312-341`** — `rebuildTokenFor(baseDir)` / `generateRebuildToken()`. Resolves the rebuild token from `LINGTAI_PORTAL_REBUILD_TOKEN` or generates a fresh 128-bit random token persisted to `.portal/rebuild-token` (mode 0600) for operators/automation on non-loopback binds.

### Handlers (`handlers.go`)

- **`portal/internal/api/handlers.go:18`** — `TopologyMu sync.Mutex`. Global mutex guarding all `topology.jsonl` and replay-cache I/O — appends, rebuilds (reconstruction included), and every read path (`/api/topology`, manifest, chunk, and `needsReconstruction`).
- **`portal/internal/api/handlers.go:20-45`** — `NewNetworkHandler(baseDir)`. `GET /api/network` — historical full network by default; only explicit `?mail=0` selects the incomplete fast shape, while `?mail=1` is explicit full behavior. Consumers of `mail=0` must gate mail totals with live availability rather than treating zero as factual. Always returns `[]` not `null` for empty slices. Sets `Lang` on the response.
- **`portal/internal/api/handlers.go:58-83`** — `NewTopologyHandler(baseDir)`. `GET /api/topology` — serves the **most recent `maxTopologyResponseFrames` (1000) frames** of `topology.jsonl` as a JSON array. **Bounded retrieval contract:** the response is capped at 1000 frames (the replay timeline uses the manifest/chunk API for full history), the tape is streamed line-by-line by `readTopologyTail` (never fully materialized), and the read runs under `TopologyMu` so readers see a consistent snapshot relative to append/rebuild writers. Missing tapes still fall back to `[]`.
- **`portal/internal/api/handlers.go:85-121`** — `readTopologyTail(path, n)`. Streams `topology.jsonl` and returns the last `n` non-empty lines as raw JSON, memory-bounded by `n` and the per-line token limit.
- **`portal/internal/api/handlers.go:116-160`** — `AppendTopology(path, network)` / `AppendTopologyAt`. Writes one JSONL line `{"t":<unix_ms>,"net":<network>}`. Normalises nil slices to `[]`. Opens the file with `O_APPEND`; creates parent dirs on first write.
- **`portal/internal/api/handlers.go:163-183`** — `NewProgressHandler(baseDir)`. `GET /api/topology/progress` — reads `reconstruct.progress` (`"N/M"` format), returns `{"current":N,"total":M}` or `{}`.
- **`portal/internal/api/handlers_test.go:70-99`** — `TestNetworkHandlerMailMode` covers default full and explicit fast/full mail query behavior.
- **`portal/internal/api/handlers_test.go:101-171`** — CORS regression coverage for live network, topology, and progress handlers, including success and error/fallback responses.

### Replay (`replay.go`, 864 lines)

- **`portal/internal/api/replay.go:19-64`** — Wire types: versioned `ReplayChunk` (delta-encoded hour range), `ReplayFrame` (keyframe or delta), `FrameDelta` (only-changed fields plus removed avatar/contact/mail key pairs), `ChunkInfo` (manifest entry), `ReplayManifest` (tape bounds + chunk list).
- **`portal/internal/api/replay.go:68-98`** — `deltaEncode(frames, keyframeInterval)`. Converts `[]TapeFrame` into a V2 `ReplayChunk` with full keyframes every N frames and `FrameDelta` in between.
- **`portal/internal/api/replay.go:100-215`** — `computeDelta(prev, curr)`. Field-by-field diff of two `Network` values: nodes (with `__REMOVED__` tombstones), avatar/contact/mail edge additions/changes and removals (keyed by identifier pairs), and stats. Returns nil if nothing changed.
- **`portal/internal/api/replay.go:256-374`** — `buildManifest(topologyPath, replayDir)`. Fast path: reads cached `manifest.json`, scans only new JSONL frames after the last completed chunk, caches newly-completed hours as `.json.gz`. O(new_frames). `manifest.TapeStart` is the first real frame timestamp (preferred from the cached manifest, then `firstFrameForChunk`, then the bucket floor as last-resort) — NOT `chunks[0].Start`, which is the hour-bucket floor and would render as ~55min of empty scrubber padding.
- **`portal/internal/api/replay.go:376-408`** — `firstFrameForChunk(info, replayDir, topologyPath)`. Returns the earliest frame timestamp in a chunk, reading `<hourMs>.json.gz` first, accepting readable stale V0 chunks for timestamp lookup, and falling back to a JSONL scan within the chunk's hour window. Used by `buildManifest` when no cached `TapeStart` is available.
- **`portal/internal/api/replay.go:413-439`** — `scanJSONLFrom(topologyPath, fromMs)`. Streams `topology.jsonl` and returns every frame with `T >= fromMs`, skipping blank and unparseable lines. Used by `buildManifest` for the tail scan.
- **`portal/internal/api/replay.go:443-525`** — `writeReconstructedReplay(topologyPath, replayDir, progressPath, frames)`. Shared writer for already-reconstructed tapes: replaces replay chunks, writes `manifest.json` with `TapeStart = frames[0].T`, truncates `topology.jsonl` to the last reconstructed frame, and updates optional `"N/M"` progress.
- **`portal/internal/api/replay.go:527-567`** — `writeChunkCache` / `readChunkCache`. Gzip-compressed JSON encode/decode of `ReplayChunk` to/from `<hourMs>.json.gz`; readable missing-version chunks return a stale-format sentinel, while corrupt/unreadable chunks remain ordinary errors.
- **`portal/internal/api/replay.go:568-678`** — `loadChunk(replayDir, topologyPath, hourStart)` plus JSONL coverage helpers (`scanJSONLChunk`, `jsonlCoversStaleChunk`). Tries cached V2 first; for stale V0 caches, reconstructs a V2 chunk from JSONL **in memory only** when JSONL covers the stale chunk, otherwise serves the readable stale chunk as compatibility fallback. `loadChunk` is a read path and never writes, replaces, or truncates a cache file — a stale V0 chunk may be the only surviving copy of that hour. Coverage is **sequence-aware**: the stale timestamp sequence must be a subsequence of the JSONL timestamps, matching relative order and every duplicate occurrence, while extra JSONL frames anywhere around it are permitted. Frame count plus endpoints is not sufficient (JSONL `[0, 6, 9]` does not cover stale `[0, 3, 6]`), and neither is distinct-timestamp set membership (JSONL `[0, 3, 6, 9]` covers neither stale `[0, 3, 3, 6]` — one occurrence of T=3 lost — nor the reordered stale `[0, 6, 3]`).
- **`portal/internal/api/replay.go:679-704`** — `NewManifestHandler`. `GET /api/topology/manifest` — calls `buildManifest`, returns chunk index.
- **`portal/internal/api/replay.go:714-768`** — `NewRebuildHandler(baseDir, token)`. `POST /api/topology/rebuild` — reconstructs the full tape from `fs.ReconstructTape`, then calls `writeReconstructedReplay`. Reconstruction runs **entirely under `TopologyMu`** (lock acquired before source scanning or frame allocation), and the request must first cross the explicit trust boundary implemented by `rebuildRequestAllowed` (403 otherwise). Reconstruction errors — including the span/frame budgets enforced in `fs.ReconstructTape` — surface as 500 responses.
- **`portal/internal/api/replay.go:775-795`** — `rebuildRequestAllowed(r, token)`. Explicit rebuild trust boundary: accepts loopback clients addressed via a loopback Host (same-machine TUI/browser), same-origin browser requests on non-loopback binds (the SPA), and requests carrying the configured `X-Portal-Rebuild-Token`. Rejects cross-origin browser requests, DNS-rebinding-shaped requests (loopback connection with a non-loopback Host), and token-less non-loopback clients. Helpers `clientIsLoopback`/`hostOnly`/`originHost` at `replay.go:797-824`.
- **`portal/internal/api/replay.go:827-866`** — `NewChunkHandler`. `GET /api/topology/chunk?start=<hourMs>` — serves one delta-encoded chunk. Supports `Accept-Encoding: gzip` for transparent compression.

## Connections

- **Called by `portal/main.go:73-88`.** `NewServer` + `srv.StartRecording` + `srv.Start` + `srv.URL` — the HTTP server is the portal's only runtime component.
- **Calls `portal/internal/fs/`:** `BuildNetworkWithOptions` (typed fast/full live snapshot), `BuildNetwork`'s full default for explicit callers, `ReconstructTape` (full rebuild from events + mailbox), and all types (`TapeFrame`, `Network`, `AgentNode`, `MailEdge`, etc.).
- **Calls `portal/i18n/`:** `i18n.Lang()` for the language field on `/api/network` responses.
- **Port file consumed by the TUI.** `main.go` writes `.portal/port` via `srv.Start`; the TUI reads it to discover the portal URL. See `tui/ANATOMY.md`.

## Composition

- **Parent:** `portal/internal/`. Sibling packages: `fs/`, `migrate/`.
- **Files:** `server.go` (~336 lines), `handlers.go` (~197 lines), `replay.go` (~864 lines), plus `*_test.go` files.
- **No sub-packages.** All API logic is in this flat package.

## State

- **`.portal/topology.jsonl`** — Always written with `TopologyMu` held. Appended by `AppendTopology` (live recording) and overwritten by `NewRebuildHandler` (reconstruction).
- **`.portal/replay/chunks/<hourMs>.json.gz`** — Gzip-compressed delta-encoded hourly chunks. Written by `writeReconstructedReplay` for reconstructed tapes (`portal/internal/api/replay.go:490-502`) and by `buildManifest` when a new live-recorded hour completes (`portal/internal/api/replay.go:327-332`).
- **`.portal/replay/chunks/manifest.json`** — Chunk index (`ReplayManifest`). Written by `writeReconstructedReplay` (`portal/internal/api/replay.go:516-520`) and `buildManifest` (`portal/internal/api/replay.go:365-367`).
- **`.portal/reconstruct.progress`** — Temporary `"N/M"` file. Written by `StartRecording` / `writeReconstructedReplay` during reconstruction (`portal/internal/api/server.go:86-97`, `portal/internal/api/replay.go:453-482`) and read by `/api/topology/progress`.
- **`.portal/rebuild-token`** — Per-project rebuild authorization token (mode 0600, one hex line). Generated by `NewServer` (or overridden via `LINGTAI_PORTAL_REBUILD_TOKEN`) and required for rebuild requests from non-loopback, non-browser clients (`portal/internal/api/server.go:312-341`).

## Notes

- **Loopback by default.** Empty host input resolves to `127.0.0.1`. Explicit `--host 0.0.0.0`, `--host ::`, or named/non-loopback hosts are allowed but treated as unauthenticated trusted-LAN exposure and warned about by the CLI.
- **No wildcard CORS.** API handlers intentionally do not set `Access-Control-Allow-Origin: *`; same-origin browser requests from the embedded portal UI still work.
- **TopologyMu is coarse-grained and covers reads.** One global lock gates all topology I/O — live append (3s ticker), rebuild (POST handler), and every read path (`/api/topology`, manifest, chunk, `needsReconstruction`). The rebuild endpoint acquires the lock **before** `fs.ReconstructTape` runs and holds it through `writeReconstructedReplay`, so concurrent rebuilds cannot stack expensive pre-lock work and readers always see a consistent snapshot. This is the synchronization semantics documented here and implemented in `handlers.go`/`replay.go`.
- **Rebuild has an explicit trust boundary.** `POST /api/topology/rebuild` is rejected with 403 unless the request is a loopback client addressed via a loopback Host (same-machine TUI/browser), a same-origin browser request on an intentionally non-loopback bind (the SPA served by this server), or carries the project's rebuild token (`X-Portal-Rebuild-Token`; persisted at `.portal/rebuild-token`, overridable via `LINGTAI_PORTAL_REBUILD_TOKEN`). Cross-origin browser requests and DNS-rebinding-shaped requests (loopback connection with a non-loopback Host) are rejected.
- **Reconstruction is budgeted in `fs.ReconstructTape`.** Timestamps must be finite and within `[1e9, 1e12]` unix seconds; the derived range is capped at 5 years; and a worst-case frame-count estimate (span/minute-gap plus activity samples) is capped at 3,000,000 frames. Violations reject the rebuild with a 500 before any frame allocation. Event-scanner failures (including oversized `events.jsonl` lines) surface as reconstruction errors.
- **Tape reads are bounded.** Startup inspection (`needsReconstruction`) reads only the tape's final line; `/api/topology` returns the most recent 1000 frames streamed line-by-line. Full-history access is via the bounded manifest/chunk API.
- **Delta encoding is the memory-replay strategy.** Instead of replaying every 3-second frame, the frontend requests hourly chunks and interpolates. Keyframes every 100 frames anchor the interpolation; deltas carry only changed fields.
- **Old-format detection is structural.** `needsReconstruction` checks whether the most recent JSONL line's mail edges carry `direct` fields. Missing fields → rebuild. This is format migration driven by data inspection, not version stamps.
