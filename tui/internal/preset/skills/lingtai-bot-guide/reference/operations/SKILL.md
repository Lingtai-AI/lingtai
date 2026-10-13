---
name: bot-guide-operations
description: >
  Use when verifying bot readiness, maintaining a live bot, or diagnosing
  recovery, refresh, version and migration problems.
version: 1.0.0
last_changed_at: "2026-10-12T18:16:58Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Verification, maintenance and recovery

Nested lingtai-bot-guide reference. This owns the bot acceptance sequence;
focused manuals retain exact tool/lifecycle/auth semantics.

## Acceptance: prove the whole path

1. Inventory the target: `lingtai-tui list "$PROJECT_DIR"` and the published
   manifest/heartbeat. Confirm one intended agent, not a duplicate, correct
   project/owner/admin role and approved active/default/allowed presets.
2. Inspect bridge registration and live tool surface separately. Discover
   `mcp-manual` → `reference/curated-addons.md` (or third-party owner). A registered
   row, cached account identity, helper `configured`, PID or heartbeat is not
   proof that the listener/account is mounted or processing inbound events.
3. Ask the human/approved producer to send a **fresh** harmless message from an
   allowed account. Observe receipt at the host and issue one authorized reply
   on the same channel using the mounted channel's current manual/schema.
   Provider `getMe` or an outbound send alone is insufficient. Do not run a second
   Telegram long poller or send unrequested probes to users/peers.
4. Validate audience restriction with a human-approved test identity where
   available. If that test is unavailable, mark access-denial proof pending;
   do not claim it passed or open the allowlist to work around it.
5. Send a sanitized human receipt: creation vs configuration vs end-to-end status,
   intended bot/account (non-secret public identity only), test outcome, gaps,
   maintenance owner and recovery route. Never attach raw logs/config/credentials
   or private directory/user IDs. A tool result alone is not the human handoff.

## Maintenance: route by symptom

| Need | Discover owner, then lower node |
|---|---|
| Ordinary reload or model/preset swap | `system-manual` → `reference/refresh-precheck/SKILL.md`; preset operation `activation-session-refresh` in `lingtai-preset-skill` |
| Sleep/suspend/CPR or duplicate/quiet process | `system-manual` → `reference/substrate-manual/SKILL.md`; live `system` schema owns exact action authority |
| TUI binary/install provenance or upgrade | `lingtai-update` → `reference/detection/SKILL.md`, `reference/update-tui/SKILL.md` or `reference/diagnosis/SKILL.md` |
| Kernel version/nudge, source/venv cutover | `system-manual` → `reference/runtime-update-checks/SKILL.md`; update is installer-owned, refresh does not install code |
| Saved preset/provider login/model error | `lingtai-preset-skill` provider child plus `reference/operations/troubleshooting-migration/SKILL.md`; `swiss-knife` → `reference/preset-health/SKILL.md` for read-only diagnosis |
| Registered MCP missing tools or moved venv | `mcp-manual` → `reference/troubleshooting.md` and `reference/curated-addons.md` (child-runtime/venv-swap sections) |
| Source/runtime mismatch after cutover | `lingtai-dev-guide` → `reference/runtime-self-check/SKILL.md`, only for cutover/mismatch, not every normal reload |
| Durable behavior/identity, memory or restart handoff | `context-manual`, `psyche-manual`, `knowledge-manual`; selected owner, not copied memory |
| Scheduled service or shell background job | `shell-manual` root async-supervision rules or the scheduled-work reference selected by its router |

## Unique recovery hazards

- Closing a frontend does not stop the agent. Inspect current process/heartbeat
  state before launch/CPR; a process may exist before its fresh heartbeat. A
  duplicate-start refusal during that gap is not proof the process is dead.
- A main-agent refresh need not restart a **healthy MCP child**. Child code and
  interpreter come from its active activation spec, not simply the parent or
  registry row. A venv/source move needs the curated owner's explicit reconcile,
  provenance check and controlled relaunch; never claim all children moved from
  a successful main-agent refresh.
- Preserve partial creation. `created_unregistered` is a useful recovery receipt;
  never treat a nonzero exit as an empty target. Headless spawn's `plain` recipe
  selection is not proof of every optional best-effort recipe write.
- Legacy TUI/Portal project migration registries are history/test APIs, not live
  repair commands. Kernel reader compatibility and explicitly authorized edits
  own old config diagnosis. Do not advance migration stamps or reconstruct a
  project from guessed schemas. No Project concept transfer is implemented here.
- Refresh/relaunch can interrupt active collaborators or detached jobs. Record
  outstanding jobs/receipts before changes. A shell timeout or reminder is not
  terminal success; retain the job identifier, inspect completion via its owner,
  and report unknown state honestly. Do not multiply supervisors/restarts.
- Destructive recovery (`clean`, `purge`, `/nirvana`, directory deletion), token
  rotation, auth repair and upgrades need explicit authority for that operation;
  a symptom, nudge or checklist does not grant it. Preserve evidence and redact
  before sharing. Report remaining uncertainty rather than widening scope.
