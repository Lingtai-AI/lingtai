---
related_files:
  - ANATOMY.md
  - tui/ANATOMY.md
  - tui/internal/tui/ANATOMY.md
  - tui/internal/processscan/ANATOMY.md
  - tui/internal/fs/ANATOMY.md
  - tui/internal/inventory/inventory.go
  - tui/internal/inventory/inventory_test.go
  - tui/list_common.go
  - tui/list_common_test.go
  - tui/list_unix.go
  - tui/list_windows.go
  - tui/purge_common.go
  - tui/internal/tui/projects.go
  - tui/internal/tui/projects_test.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# inventory

> **Maintenance:** see `lingtai-tui-anatomy` (at `~/.lingtai-tui/utilities/lingtai-tui-anatomy/SKILL.md`). Update this file in the same commit as code changes.

`inventory` is the typed running-agent inventory shared by the `lingtai-tui list` CLI and the interactive `/projects` switcher. It sits below both callers: process visibility comes from `internal/processscan`, while filesystem enrichment comes from `internal/fs`. It writes nothing.

## Components

| Component | File | Purpose |
|---|---|---|
| `Options`, `Snapshot`, `Group`, `Record`, `EnterabilityReason`, `AgentIdentity` | `tui/internal/inventory/inventory.go:31` | Public typed API for process-visible running agents, grouped by project and carrying role/admin/heartbeat/lock/typed enterability data plus stable agent identity. |
| `Scan` / `FromProcesses` | `tui/internal/inventory/inventory.go:96` | Scan the process table or convert supplied process rows into enriched, duplicate-collapsed, sorted inventory. `FromProcesses` is the deterministic test seam. |
| `NormalizePath`, `IdentityFor`, `AgentDirInFilter`, `ProjectFromAgentDir` | `tui/internal/inventory/inventory.go:160` | Shared lexical path normalization, stable identity, filtering, and process-to-project derivation. Agent directories with spaces are treated as normal paths. |
| `enrichRecord`, `RoleFor`, `enterability` | `tui/internal/inventory/inventory.go:211` | Read `.agent.json`, derive role/admin/IM/heartbeat/lock state, and mark unreadable, human, phantom, pathless, or non-admin records as non-enterable with a typed reason and optional detail; only valid orchestrator/admin records are enterable. |
| `collapseByAgentDir` | `tui/internal/inventory/inventory.go:276` | Collapses duplicate visible processes for the same agent dir, preferring the PID currently advertised in `.status.json` when available. |
| `sortRecords` / `groupRecords` | `tui/internal/inventory/inventory.go:325` | Deterministic project, role, display-name, path, PID sorting and project grouping. |
| `SummarizeIMIdentities`, `SummarizeAdmin`, `HumanUptimeFromEtime`, `HeartbeatLabel` | `tui/internal/inventory/inventory.go:410` | Small rendering helpers shared by CLI and TUI callers without importing either package. |

## Connections

- **Called from:** `lingtai-tui list` after platform process scanning (`tui/list_unix.go:19-27`, `tui/list_windows.go:19-24`), `/projects` via `ProjectsModel.loadDataMsg` (`tui/internal/tui/projects.go:194-221`), and purge filtering through `AgentDirInFilter`.
- **Calls out:** `internal/processscan` for visible process rows when using `Scan`, and `internal/fs` for `.agent.json`, `.agent.heartbeat`, `.status.json`, `.agent.lock`, and MCP identity metadata.
- **Does not call:** package `main`, `internal/tui`, or any root Bubble Tea model. Keep this package below both the CLI and TUI to avoid import cycles.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`)
- **Siblings:** `tui/internal/processscan/` (process-table parser), `tui/internal/fs/` (agent filesystem readers), `tui/internal/tui/` (interactive caller).
- **Consumers:** `tui/list_common.go` owns CLI table/JSON rendering from `Record`, while `tui/internal/tui/projects.go` owns the grouped interactive switcher and selection UX from the same `Snapshot`.

## State

- **Reads:** process table via `processscan` when scanning; per-agent `.agent.json`, `.agent.heartbeat`, `.status.json`, `.agent.lock`, and `system/mcp_identities/*.json`.
- **Writes:** none.
- **Errors:** scan-command errors are returned to callers so empty inventory and failed inventory scan stay distinct. Per-record manifest/read errors stay on `Record.ReadError` and make the row non-enterable rather than disappearing.

## Invariants

1. Process-table visibility is the inclusion boundary. The default inventory excludes human pseudo-agents, but it must not hide stale-heartbeat agents.
2. Phantom projects and unreadable manifests are rendered honestly by callers: they remain visible records/groups with explicit non-enterable errors.
3. Duplicate process rows for one agent directory collapse to one record, preferring `.status.json` runtime PID when it identifies the current process.
4. Sorting and grouping are deterministic so CLI output, JSON, and `/projects` tests can compare behavior without frame snapshots.
5. Role detection is owned by lower-level filesystem metadata (`fs.IsOrchestratorManifest`), not duplicated in CLI or TUI packages.
