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

The `lingtai-portal` binary: a single Go binary that reads the same `.lingtai/` filesystem the TUI does and serves a network visualisation, mail UI, and topology replay over HTTP. It binds to loopback by default and ships with no runtime Node dependency — the React 19 frontend is compiled in via `embed.FS`.

## Components

- **`portal/main.go:23-98`** — `main()` entry. Parses `--dir`, `--host`, `--port`, `--open`, `--lang` flags (`portal/main.go:34-44`); validates `.lingtai/` exists (`portal/main.go:52-55`); leaves project files untouched (runtime migrations are retired); creates `.portal/` directory (`portal/main.go:68-70`); constructs the `api.Server`, starts topology recording (`portal/main.go:73-74`), and serves on the requested host/port (`portal/main.go:75-82`). Blocks on SIGINT/SIGTERM, then calls `srv.Stop()` (`portal/main.go:91-97`).
- **`portal/main.go:100-130`** — `openBrowser(url)` launches the OS default browser (darwin/linux/windows/WSL).
- **`portal/main.go:130-137`** — `isWSL()` detects WSL via `/proc/version`.
- **`portal/embed.go:8-9`** — `//go:embed all:web/dist` compiles the React frontend build output into `webDist embed.FS`. No runtime Node dependency.
- **`portal/embed.go:11-17`** — `WebFS()` returns `fs.Sub(webDist, "web/dist")` so the HTTP server mounts from the `web/dist/` root.
- **`Makefile:1-24`** — Build pipeline. `web-build` runs `npm install && npm run build` in `web/`; `go-build` depends on it and stamps `main.version` via `-ldflags`. `cross-compile` targets darwin/linux × arm64/amd64.
- **`internal/api/`** — HTTP server, handlers, and the replay endpoint. See `portal/internal/api/ANATOMY.md`.
- **`internal/fs/`** — Filesystem readers: agent manifests, heartbeat, mailbox, network reconstruction (`reconstruct.go`), topology types (`types.go`). Same shape as `tui/internal/fs/` but Portal-specific.
- **`internal/migrate/`** — retained m001–m039 source/tests and registry history; production Portal does not import or execute it. See `portal/internal/migrate/ANATOMY.md`.
- **`web/`** — React 19 + TypeScript + Vite frontend. Source under `web/src/`; builds to `web/dist/`.
- **`i18n/`** — en/zh/wen JSON tables (independent of `tui/i18n/`).

## Connections

- **Portal → filesystem (read).** `internal/fs/` reads agent manifests, heartbeats, mailboxes, token ledgers, chat history, and `.notification/` payloads — the same files the TUI reads. All communication with running agents is filesystem-only: no sockets, no RPC.
- **Portal → filesystem (write).** Writes `.portal/port` (bound port), `.portal/topology.jsonl` (live recording), `.portal/replay/chunks/*.json.gz` (compressed replay caches), and `.portal/reconstruct.progress` (reconstruction progress).
- **Portal ↔ TUI integration.** The TUI launches `lingtai-portal` as a subprocess when the user opens `/viz`. The TUI reads `.portal/port` to know where to point the browser. The portal and TUI may encounter legacy `meta.json` files, but neither production binary reads, writes, or advances project migration progress. See repo-root `ANATOMY.md` Notes "Migration retirement."
- **Portal → browser.** Serves the embedded React SPA on `/` and a same-origin JSON API on `/api/*`. The API does not emit wildcard CORS headers.
- **Portal embeds frontend.** `embed.go` compiles `web/dist/` into the Go binary — `lingtai-portal` ships as a single file. (The dev build still requires `make web-build` to produce the dist.)

## Composition

- **Parent:** none — binary root under the lingtai monorepo.
- **Subpackages:** `internal/api/` (HTTP server + replay), `internal/fs/` (filesystem readers), `internal/migrate/` (retained historical registry).
- **Sibling tree:** `tui/` — the TUI binary. See `tui/ANATOMY.md` for the other half of the Go surface.
- **Build outputs:** `portal/bin/lingtai-portal` (and cross-compile variants).
- **Module name:** `github.com/anthropics/lingtai-portal`.

## State

- **`.portal/port`** — Written on server start (`portal/main.go:75-76` → `portal/internal/api/server.go:61-62`). Contains only the bound TCP port as an ASCII integer. Read by the TUI to know where to open the browser.
- **`.portal/topology.jsonl`** — JSONL tape of network snapshots. Each line is `{"t": <unix_ms>, "net": <Network>}`. Appended every 3 seconds by `StartRecording` (`portal/internal/api/server.go:96-110`); also appended by the live handlers on each request.
- **`.portal/replay/chunks/`** — Compressed hourly replay chunks (`<hourMs>.json.gz`), each containing delta-encoded frames with keyframes every 100 frames. Plus `manifest.json` indexing all chunks.
- **`.portal/reconstruct.progress`** — Temporary `"N/M"` progress file during tape reconstruction. Startup creates/deletes it in `StartRecording` (`portal/internal/api/server.go:82-93`); the shared replay writer updates it while caching reconstructed frames (`portal/internal/api/replay.go:417-446`).
- **`meta.json`** — Legacy project migration metadata may remain under `.lingtai/`; Portal production does not read, write, or advance it.

## Notes

- **Runtime/control-surface boundary:** Portal is a presentation/server process, not the agent runtime. Its SIGINT/SIGTERM path stops only the Portal HTTP server (`portal/main.go:91-97`); running agents remain kernel-owned and are observed through the filesystem described above. TUI-side Portal launch/release and agent lifecycle controls are mapped in `tui/ANATOMY.md`; exact Python lifecycle semantics belong to the separate `lingtai-kernel-anatomy` graph.
- **Loopback host and random port are the defaults.** Empty `--host` resolves to `127.0.0.1`, and `--port 0` (the default, `portal/main.go:42`) lets the OS pick an available port (`portal/internal/api/server.go:48-60`). The bound port is written to `.portal/port` so callers can discover it.
- **Explicit external hosts are unauthenticated.** `--host 0.0.0.0`, `--host ::`, or a named/non-loopback host is an opt-in for trusted-LAN use only. The display/open URL remains `http://localhost:<port>` for loopback and wildcard binds; explicit named/non-loopback hosts display directly.
- **Live recording begins at startup.** `StartRecording` (`portal/internal/api/server.go:70-114`) runs in a background goroutine. On first call it checks whether the tape needs reconstruction (`needsReconstruction`, `portal/internal/api/server.go:174-200`), rebuilds from source events if needed, then records a snapshot every 3 seconds.
- **`needsReconstruction` detects format migration.** If `topology.jsonl` is missing, empty, or uses the pre-`direct/cc/bcc` format, the recorder triggers a full rebuild (`portal/internal/api/server.go:174-200`).
- **Runtime migration retirement.** Portal startup does not run the retained registry or reject projects based on its historical version. Rebuild both binaries after ordinary code changes as usual.
