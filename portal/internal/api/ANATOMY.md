---
related_files:
  - portal/ANATOMY.md
  - portal/internal/fs/ANATOMY.md
  - portal/internal/api/server.go
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

- **`portal/internal/api/server.go:18-24`** — `Server` struct. Wraps `http.Server` with `port`, `baseDir`, `cancel`/`done` for the recording goroutine.
- **`portal/internal/api/server.go:26-41`** — `NewServer(baseDir, staticFS)`. Registers 6 API routes (`portal/internal/api/server.go:28-33`) and mounts `staticFS` at `/` (`portal/internal/api/server.go:35`). Routes fixed before the Server is returned.
- **`portal/internal/api/server.go:43-58`** — `Start(portFile, fixedPort)`. Listens on `0.0.0.0:0` (random port) unless `fixedPort > 0` (`portal/internal/api/server.go:44-48`). Writes bound port to `portFile` (`portal/internal/api/server.go:53-54`). Serves in a goroutine (`portal/internal/api/server.go:56`).
- **`portal/internal/api/server.go:62-106`** — `StartRecording(baseDir)`. Background goroutine with a 3-second ticker (`portal/internal/api/server.go:70`). On first run: checks `needsReconstruction`, rebuilds tape from source via `agentfs.ReconstructTape`, writes replay caches through `writeReconstructedReplay` (`portal/internal/api/server.go:73-85`), then records `agentfs.BuildNetwork` snapshots on every tick via `AppendTopology` (`portal/internal/api/server.go:88-102`).
- **`portal/internal/api/server.go:116-122`** — `Stop(ctx)`. Cancels the recording goroutine, waits for `s.done`, shuts down the HTTP server.
- **`portal/internal/api/server.go:126-150`** — `needsReconstruction(path)`. Returns true if `topology.jsonl` is missing, empty, or uses the old format (lacking `direct`/`cc`/`bcc` fields on mail edges).

### Handlers (`handlers.go`)

- **`portal/internal/api/handlers.go:16`** — `TopologyMu sync.Mutex`. Global mutex guarding `topology.jsonl` writes and reads.
- **`portal/internal/api/handlers.go:18-42`** — `NewNetworkHandler(baseDir)`. `GET /api/network` — live snapshot of the agent network via `fs.BuildNetwork`. Always returns `[]` not `null` for empty slices. Sets `Lang` on the response.
- **`portal/internal/api/handlers.go:44-72`** — `NewTopologyHandler(baseDir)`. `GET /api/topology` — reads `topology.jsonl`, parses it into a JSON array of raw messages.
- **`portal/internal/api/handlers.go:93-136`** — `AppendTopology(path, network)` / `AppendTopologyAt`. Writes one JSONL line `{"t":<unix_ms>,"net":<network>}`. Normalises nil slices to `[]`. Opens the file with `O_APPEND`; creates parent dirs on first write.
- **`portal/internal/api/handlers.go:140-159`** — `NewProgressHandler(baseDir)`. `GET /api/topology/progress` — reads `reconstruct.progress` (`"N/M"` format), returns `{"current":N,"total":M}` or `{}`.

### Replay (`replay.go`, 680 lines)

- **`portal/internal/api/replay.go:18-56`** — Wire types: `ReplayChunk` (delta-encoded hour range), `ReplayFrame` (keyframe or delta), `FrameDelta` (only-changed fields), `ChunkInfo` (manifest entry), `ReplayManifest` (tape bounds + chunk list).
- **`portal/internal/api/replay.go:60-87`** — `deltaEncode(frames, keyframeInterval)`. Converts `[]TapeFrame` into a `ReplayChunk` with full keyframes every N frames and `FrameDelta` in between.
- **`portal/internal/api/replay.go:91-178`** — `computeDelta(prev, curr)`. Field-by-field diff of two `Network` values: nodes (with `__REMOVED__` tombstones), avatar/contact/mail edges (keyed by identifier pairs), and stats. Returns nil if nothing changed.
- **`portal/internal/api/replay.go:217-335`** — `buildManifest(topologyPath, replayDir)`. Fast path: reads cached `manifest.json`, scans only new JSONL frames after the last completed chunk, caches newly-completed hours as `.json.gz`. O(new_frames). `manifest.TapeStart` is the first real frame timestamp (preferred from the cached manifest, then `firstFrameForChunk`, then the bucket floor as last-resort) — NOT `chunks[0].Start`, which is the hour-bucket floor and would render as ~55min of empty scrubber padding.
- **`portal/internal/api/replay.go:337-372`** — `firstFrameForChunk(info, replayDir, topologyPath)`. Returns the earliest frame timestamp in a chunk, reading `<hourMs>.json.gz` first and falling back to a JSONL scan within the chunk's hour window. Used by `buildManifest` when no cached `TapeStart` is available.
- **`portal/internal/api/replay.go:405-489`** — `writeReconstructedReplay(topologyPath, replayDir, progressPath, frames)`. Shared writer for already-reconstructed tapes: replaces replay chunks, writes `manifest.json` with `TapeStart = frames[0].T`, truncates `topology.jsonl` to the last reconstructed frame, and updates optional `"N/M"` progress.
- **`portal/internal/api/replay.go:491-524`** — `writeChunkCache` / `readChunkCache`. Gzip-compressed JSON encode/decode of `ReplayChunk` to/from `<hourMs>.json.gz`.
- **`portal/internal/api/replay.go:526-566`** — `loadChunk(replayDir, topologyPath, hourStart)`. Tries cached `.json.gz` first; falls back to scanning JSONL for that hour's frames if cache missing.
- **`portal/internal/api/replay.go:568-593`** — `NewManifestHandler`. `GET /api/topology/manifest` — calls `buildManifest`, returns chunk index.
- **`portal/internal/api/replay.go:595-639`** — `NewRebuildHandler`. `POST /api/topology/rebuild` — reconstructs the full tape from `fs.ReconstructTape`, then calls `writeReconstructedReplay` while holding `TopologyMu`.
- **`portal/internal/api/replay.go:641-680`** — `NewChunkHandler`. `GET /api/topology/chunk?start=<hourMs>` — serves one delta-encoded chunk. Supports `Accept-Encoding: gzip` for transparent compression.

## Connections

- **Called by `portal/main.go:71-74`.** `NewServer` + `srv.StartRecording` + `srv.Start` — the HTTP server is the portal's only runtime component.
- **Calls `portal/internal/fs/`:** `BuildNetwork` (live snapshot), `ReconstructTape` (full rebuild from events + mailbox), and all types (`TapeFrame`, `Network`, `AgentNode`, `MailEdge`, etc.).
- **Calls `portal/i18n/`:** `i18n.Lang()` for the language field on `/api/network` responses.
- **Port file consumed by the TUI.** `main.go` writes `.portal/port` via `srv.Start`; the TUI reads it to discover the portal URL. See `tui/ANATOMY.md`.

## Composition

- **Parent:** `portal/internal/`. Sibling packages: `fs/`, `migrate/`.
- **Files:** `server.go` (~150 lines), `handlers.go` (~175 lines), `replay.go` (~680 lines), plus `*_test.go` files.
- **No sub-packages.** All API logic is in this flat package.

## State

- **`.portal/topology.jsonl`** — Always written with `TopologyMu` held. Appended by `AppendTopology` (live recording) and overwritten by `NewRebuildHandler` (reconstruction).
- **`.portal/replay/chunks/<hourMs>.json.gz`** — Gzip-compressed delta-encoded hourly chunks. Written by `writeReconstructedReplay` for reconstructed tapes (`portal/internal/api/replay.go:454-466`) and by `buildManifest` when a new live-recorded hour completes (`portal/internal/api/replay.go:282-298`).
- **`.portal/replay/chunks/manifest.json`** — Chunk index (`ReplayManifest`). Written by `writeReconstructedReplay` (`portal/internal/api/replay.go:480-485`) and `buildManifest` (`portal/internal/api/replay.go:329-331`).
- **`.portal/reconstruct.progress`** — Temporary `"N/M"` file. Written by `StartRecording` / `writeReconstructedReplay` during reconstruction (`portal/internal/api/server.go:76-85`, `portal/internal/api/replay.go:417-446`) and read by `/api/topology/progress`.

## Notes

- **TopologyMu is coarse-grained.** One global lock gates all topology I/O — both live append (3s ticker) and rebuild (POST handler). The rebuild endpoint replaces the file entirely while holding the lock.
- **Delta encoding is the memory-replay strategy.** Instead of replaying every 3-second frame, the frontend requests hourly chunks and interpolates. Keyframes every 100 frames anchor the interpolation; deltas carry only changed fields.
- **Old-format detection is structural.** `needsReconstruction` checks whether the most recent JSONL line's mail edges carry `direct` fields. Missing fields → rebuild. This is format migration driven by data inspection, not version stamps.
