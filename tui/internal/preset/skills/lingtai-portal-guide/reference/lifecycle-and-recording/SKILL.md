---
name: portal-guide-lifecycle-and-recording
description: Nested lingtai-portal-guide reference for portal lifecycle-state interpretation, heartbeat staleness, recording, and tape reconstruction behavior.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Lifecycle and recording

This is a nested `lingtai-portal-guide` reference. It covers how the portal interprets agent lifecycle states and how it records/rebuilds topology history.

## 5-state lifecycle

| State | Meaning |
|-------|---------|
| `ACTIVE` | Agent is running and processing |
| `IDLE` | Agent is running but waiting for input |
| `STUCK` | Agent encountered an error or is unresponsive |
| `ASLEEP` | Agent is in sleep mode (`.sleep` signal or explicit lifecycle transition) |
| `SUSPENDED` | Agent process is not running (kernel/manifest reports it as such) |

Heartbeat is the portal's liveness ground truth, but it is observed independently from the manifest lifecycle state. `BuildNetwork()` calls `IsAlive(..., AgentAliveThresholdSec())`, which resolves the liveness window from the `LINGTAI_AGENT_ALIVE_THRESHOLD_SEC` env var (default 5 seconds; missing, blank, malformed, non-finite, zero, or negative values fall back to the default). A stale or missing heartbeat only clears `AgentNode.Alive` — it does not rewrite `.agent.json`'s stored `state`, so a genuinely `SUSPENDED` manifest stays `SUSPENDED` while an `ACTIVE`/`IDLE`/etc. agent with a stale heartbeat keeps its real state and is simply reported not alive.

## Recording

The portal starts recording immediately on launch: a background goroutine calls `BuildNetwork()` every 3 seconds and appends the result as a JSONL line to `topology.jsonl`. This tape grows indefinitely.

## Reconstruction

On startup, if the tape needs reconstruction (missing, empty, or old format), the portal rebuilds it from replay chunks. While that runs, `.lingtai/.portal/reconstruct.progress` may contain a transient `current/total` progress string, which the `/api/topology/progress` endpoint parses and exposes to the browser UI.

A manual `POST /api/topology/rebuild` instead reconstructs from source data and rewrites the replay chunks. The `topology-and-api` reference documents both endpoints' exact response shapes.
