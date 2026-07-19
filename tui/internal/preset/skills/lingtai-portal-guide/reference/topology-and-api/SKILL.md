---
name: portal-guide-topology-and-api
description: Nested lingtai-portal-guide reference for topology tape frames, replay chunks, portal API endpoints, and the Network JSON response.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Topology and API

This is a nested `lingtai-portal-guide` reference. It covers the on-disk topology tape, replay chunks, HTTP endpoints, and live network JSON shape.

## `topology.jsonl`

Each line is a `TapeFrame`:

```json
{
  "t": 1744567890123,
  "net": { "nodes": [...], "avatar_edges": [...], "contact_edges": [...], "mail_edges": [...], "stats": {...} }
}
```

`t` is milliseconds since epoch. `net` is the same `Network` object `/api/network` serves; *Network response shape* below is a representative example of its core fields.

## Replay chunks

Historical data is stored as delta-encoded, keyframed, gzipped JSON chunks bucketed by hour under `.lingtai/.portal/replay/chunks/`. The `manifest.json` in that chunks directory lists all available chunks with their time ranges and frame counts.

## API endpoints

All endpoints are served at `http://localhost:<port>`.

| Endpoint | Method | Response | Description |
|----------|--------|----------|-------------|
| `/api/network` | GET | `Network` JSON | Live network state (nodes, edges, stats) |
| `/api/topology` | GET | JSON array of `TapeFrame` | Full topology tape (can be large) |
| `/api/topology/manifest` | GET | `ReplayManifest` JSON | Chunk metadata for replay |
| `/api/topology/chunk?start=<hourMs>` | GET | Chunk JSON (optionally gzip-encoded when requested) | Frames for one hour-bucket start time |
| `/api/topology/progress` | GET | JSON object such as `{ "current": N, "total": M }` (or `{}`) | Reconstruction progress parsed from `reconstruct.progress` |
| `/api/topology/rebuild` | POST | `ReplayManifest` JSON | Trigger tape reconstruction from source data and rewrite replay chunks |

## Network response shape

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
    "active": 2,
    "idle": 1,
    "stuck": 0,
    "asleep": 0,
    "suspended": 3,
    "total_mails": 42
  }
}
```
