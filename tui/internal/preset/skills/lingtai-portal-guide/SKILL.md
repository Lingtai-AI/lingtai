---
name: lingtai-portal-guide
description: Reference for the LingTai portal (lingtai-portal) internals. Read this to understand the portal's API endpoints, topology recording, replay system, and .portal/ directory structure. Useful when the human asks about visualization, network history, or the portal.
version: 1.0.0
---

# Portal Guide

This is a reference document. It describes how the LingTai portal works internally. Do not take action based on this skill — use it to understand the portal's architecture.

## What Is the Portal

`lingtai-portal` is a Go binary that serves a web-based visualization of the agent network. It:

1. Reads the same `.lingtai/` directory as the TUI (filesystem-native, no separate database)
2. Snapshots the network topology every 3 seconds into a JSONL tape
3. Serves a web UI for live network visualization and historical replay
4. Listens on a random port, stored in `.lingtai/.port`

## Opening the Portal

Read `.lingtai/.port` to get the port number. Open `http://localhost:<port>` in a browser. If the file doesn't exist, the portal is not running — the user can start it with `lingtai-portal`.

## .portal/ Directory

```
.lingtai/.portal/
├── topology.jsonl              # Live tape — one JSON frame per line
├── replay/
│   ├── manifest.json           # Chunk metadata (start/end timestamps, frame counts)
│   └── chunks/
│       ├── <hour-bucket>.json.gz   # Delta-encoded, gzipped hourly chunks
│       └── ...
└── reconstruct.progress        # Transient: "current/total" during reconstruction
```

### topology.jsonl

Each line is a `TapeFrame`:

```json
{
  "t": 1744567890123,
  "net": {
    "nodes": [...],
    "avatar_edges": [...],
    "contact_edges": [...],
    "mail_edges": [...],
    "stats": {
      "active": 2,
      "idle": 1,
      "stuck": 0,
      "asleep": 0,
      "suspended": 3,
      "total_mails": 42
    }
  }
}
```

`t` is milliseconds since epoch.

### Replay Chunks

Historical data is stored as delta-encoded, keyframed, gzipped JSON chunks bucketed by hour. The manifest lists all available chunks with their time ranges and frame counts.

## API Endpoints

All endpoints are served at `http://localhost:<port>`.

| Endpoint | Method | Response | Description |
|----------|--------|----------|-------------|
| `/api/network` | GET | `Network` JSON | Live network state (nodes, edges, stats) |
| `/api/topology` | GET | JSON array of `TapeFrame` | Full topology tape (can be large) |
| `/api/topology/manifest` | GET | `ReplayManifest` JSON | Chunk metadata for replay |
| `/api/topology/chunk?start=<ms>&end=<ms>` | GET | Gzipped chunk | Compressed frames for a time range |
| `/api/topology/progress` | GET | `"current/total"` | Reconstruction progress |
| `/api/topology/rebuild` | GET | `"ok"` | Trigger tape reconstruction |

### Network Response Shape

```json
{
  "nodes": [
    {
      "address": "orchestrator",
      "agent_name": "orchestrator",
      "nickname": "小灵",
      "state": "ACTIVE",
      "alive": true,
      "is_human": false,
      "capabilities": ["avatar", "search"]
    }
  ],
  "avatar_edges": [
    {"parent": "orchestrator", "child": "avatar-1", "child_name": "avatar-1"}
  ],
  "contact_edges": [
    {"owner": "orchestrator", "target": "human", "name": "human"}
  ],
  "mail_edges": [
    {"sender": "orchestrator", "recipient": "human", "count": 5}
  ],
  "stats": {
    "active": 2, "idle": 1, "stuck": 0,
    "asleep": 0, "suspended": 3, "total_mails": 42
  }
}
```

### 5-State Lifecycle

| State | Meaning |
|-------|---------|
| `ACTIVE` | Agent is running and processing |
| `IDLE` | Agent is running but waiting for input |
| `STUCK` | Agent encountered an error or is unresponsive |
| `ASLEEP` | Agent is in sleep mode (`.sleep` signal or stamina exhausted) |
| `SUSPENDED` | Agent process is not running (no heartbeat) |

Note: if an agent has a state in `.agent.json` but its heartbeat is stale (> 3s old), it is effectively `SUSPENDED` regardless of the stored state.

## Recording

The portal starts recording immediately on launch. A background goroutine calls `BuildNetwork()` every 3 seconds and appends the result as a JSONL line to `topology.jsonl`. This tape grows indefinitely. On startup, if the tape needs reconstruction (missing, empty, or old format), the portal rebuilds it from replay chunks.
