---
related_files:
  - ANATOMY.md
  - tui/ANATOMY.md
  - portal/internal/api/ANATOMY.md
  - portal/internal/fs/ANATOMY.md
  - portal/internal/migrate/ANATOMY.md
  - portal/main.go
  - portal/embed.go
  - portal/Makefile
  - portal/go.mod
  - portal/i18n/i18n.go
  - portal/i18n/en.json
  - portal/i18n/zh.json
  - portal/i18n/wen.json
  - portal/web/package.json
  - portal/web/src/App.tsx
  - portal/web/src/Graph.tsx
  - portal/web/src/BottomBar.tsx
  - portal/web/src/FilterPanel.tsx
  - portal/web/src/api.ts
  - portal/web/src/types.ts
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# portal

> **Maintenance:** see the `lingtai-tui-anatomy` skill. **Coding agents** update this file in the same commit as code changes. **LingTai agents** report drift as issues; do not silently fix.

The `lingtai-portal` binary: a single Go binary that reads the same `.lingtai/` filesystem the TUI does and serves a network visualisation, mail UI, and topology replay over HTTP. Ships with no runtime Node dependency — the React 19 frontend is compiled in via `embed.FS`.

## Components

- **`portal/main.go:23-93`** — `main()` entry. Parses `--dir`, `--port`, `--open`, `--lang` flags (`portal/main.go:34-43`); validates `.lingtai/` exists (`portal/main.go:52-55`); runs migrations (`portal/main.go:61`); creates `.portal/` directory (`portal/main.go:67-68`); constructs the `api.Server`, starts topology recording (`portal/main.go:72`), and serves on a random port (`portal/main.go:74`). Blocks on SIGINT/SIGTERM, then calls `srv.Stop()` (`portal/main.go:87-92`).
- **`portal/main.go:95-125`** — `openBrowser(url)` launches the OS default browser (darwin/linux/windows/WSL).
- **`portal/main.go:127-134`** — `isWSL()` detects WSL via `/proc/version`.
- **`portal/embed.go:8-9`** — `//go:embed all:web/dist` compiles the React frontend build output into `webDist embed.FS`. No runtime Node dependency.
- **`portal/embed.go:11-17`** — `WebFS()` returns `fs.Sub(webDist, "web/dist")` so the HTTP server mounts from the `web/dist/` root.
- **`Makefile:1-24`** — Build pipeline. `web-build` runs `npm install && npm run build` in `web/`; `go-build` depends on it and stamps `main.version` via `-ldflags`. `cross-compile` targets darwin/linux × arm64/amd64.
- **`internal/api/`** — HTTP server, handlers, and the 655-line replay endpoint. See `portal/internal/api/ANATOMY.md`.
- **`internal/fs/`** — Filesystem readers: agent manifests, heartbeat, mailbox, network reconstruction (`reconstruct.go`), topology types (`types.go`). Same shape as `tui/internal/fs/` but Portal-specific.
- **`internal/migrate/`** — Migration registry sharing the `meta.json` version space with the TUI. Each migration mirrors its TUI counterpart (or is a no-op stub). See `portal/internal/migrate/migrate.go`.
- **`web/`** — React 19 + TypeScript + Vite frontend. Source under `web/src/`; builds to `web/dist/`.
- **`i18n/`** — en/zh/wen JSON tables (independent of `tui/i18n/`).

## Connections

- **Portal → filesystem (read).** `internal/fs/` reads agent manifests, heartbeats, mailboxes, token ledgers, chat history, and `.notification/` payloads — the same files the TUI reads. All communication with running agents is filesystem-only: no sockets, no RPC.
- **Portal → filesystem (write).** Writes `.portal/port` (bound port), `.portal/topology.jsonl` (live recording), `.portal/replay/chunks/*.json.gz` (compressed replay caches), and `.portal/reconstruct.progress` (reconstruction progress).
- **Portal ↔ TUI integration.** The TUI launches `lingtai-portal` as a subprocess when the user opens `/viz`. The TUI reads `.portal/port` to know where to point the browser. The portal and TUI share `meta.json` version space — when one bumps `CurrentVersion`, the other must also bump. See repo-root `ANATOMY.md` Notes "Migration cross-package contract."
- **Portal → browser.** Serves the embedded React SPA on `/` and a JSON API on `/api/*`. All endpoints set `Access-Control-Allow-Origin: *`.
- **Portal embeds frontend.** `embed.go` compiles `web/dist/` into the Go binary — `lingtai-portal` ships as a single file. (The dev build still requires `make web-build` to produce the dist.)

## Composition

- **Parent:** none — binary root under the lingtai monorepo.
- **Subpackages:** `internal/api/` (HTTP server + replay), `internal/fs/` (filesystem readers), `internal/migrate/` (migration registry).
- **Sibling tree:** `tui/` — the TUI binary. See `tui/ANATOMY.md` for the other half of the Go surface.
- **Build outputs:** `portal/bin/lingtai-portal` (and cross-compile variants).
- **Module name:** `github.com/anthropics/lingtai-portal`.

## State

- **`.portal/port`** — Written on server start (`portal/main.go:73` → `portal/internal/api/server.go:54`). Contains the bound TCP port as an ASCII integer. Read by the TUI to know where to open the browser.
- **`.portal/topology.jsonl`** — JSONL tape of network snapshots. Each line is `{"t": <unix_ms>, "net": <Network>}`. Appended every 3 seconds by `StartRecording` (`portal/internal/api/server.go:152-157`); also appended by the live handlers on each request.
- **`.portal/replay/chunks/`** — Compressed hourly replay chunks (`<hourMs>.json.gz`), each containing delta-encoded frames with keyframes every 100 frames. Plus `manifest.json` indexing all chunks.
- **`.portal/reconstruct.progress`** — Temporary `"N/M"` progress file during tape reconstruction (`portal/internal/api/server.go:116`). Deleted when reconstruction completes (`portal/internal/api/server.go:139`).
- **`meta.json`** — Migration version stamp under `.lingtai/`. Shared with the TUI; portal bumps its own `CurrentVersion` in lockstep.

## Notes

- **Random port is the default.** `--port 0` (the default, `portal/main.go:40`) lets the OS pick an available port (`portal/internal/api/server.go:44-48`). The bound port is written to `.portal/port` so callers can discover it.
- **Live recording begins at startup.** `StartRecording` (`portal/internal/api/server.go:62-159`) runs in a background goroutine. On first call it checks whether the tape needs reconstruction (`needsReconstruction`, `portal/internal/api/server.go:180-203`), rebuilds from source events if needed, then records a snapshot every 3 seconds.
- **`needsReconstruction` detects format migration.** If `topology.jsonl` is missing, empty, or uses the pre-`direct/cc/bcc` format, the recorder triggers a full rebuild (`portal/internal/api/server.go:179-203`).
- **Dev-mode rebuild gotcha.** After ANY migration bump, rebuild both binaries: `cd tui && make build && cd ../portal && make build`. A stale portal against a migrated project fails with "data version N is newer than this binary supports."
