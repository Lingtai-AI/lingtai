---
name: portal-guide-overview
description: Nested lingtai-portal-guide reference for portal purpose, opening the browser view, and the `.portal/` directory layout.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Portal overview

This is a nested `lingtai-portal-guide` reference. It covers what the portal is, how to open it, and which files it writes under `.lingtai/.portal/`.

## What is the portal

`lingtai-portal` is a Go binary that:

1. Reads the same `.lingtai/` directory as the TUI (filesystem-native, no separate database).
2. Snapshots the network topology every 3 seconds into a JSONL tape.
3. Serves a web UI for live network visualization and historical replay.
4. Listens on a random port, stored in `.lingtai/.portal/port`.

## Opening the portal

Read `.lingtai/.portal/port` to get the port number, then open `http://localhost:<port>` in a browser. If that file does not exist, the portal is not running — the user can start it with `lingtai-portal`.

## `.portal/` directory

```text
.lingtai/.portal/
├── topology.jsonl              # Live tape — one JSON frame per line
├── replay/
│   └── chunks/
│       ├── manifest.json           # Chunk metadata (start/end timestamps, frame counts)
│       ├── <hour-bucket>.json.gz   # Delta-encoded, gzipped hourly chunks
│       └── ...
└── reconstruct.progress        # Transient: "current/total" during reconstruction
```

Use the `topology-and-api` reference for the data formats inside those files.
