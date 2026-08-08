# TUI ↔ Agent Requirement Alignment

Status: draft · 2026-08-08 · authored from the config.json recovery incident

## Problem statement

A LingTai network can be healthy (agents running, mail flowing, kernels alive)
while the **TUI cannot start correctly**. When that happens the user is dropped
into a recovery wizard that asks for API keys the system already has — because
the TUI and the agents read credentials from **different stores**.

Goal: **if an agent can run, the TUI should run too.** Where the two sides
diverge, the doctor should diagnose the exact gap and propose the exact fix.

The architectural principle (see `tui/CONTRACT.md`): an agent is defined
**solely** by its `init.json`; the TUI is defined by the same project-scoped
network plus a small set of **purely additive** user-level requirements.
Anything the TUI needs beyond that is either derivable from the agent source
of truth or purely additive with a default. This document records the
misalignments that motivated the contract and how each one resolves.

## What an agent needs to run

| Requirement | Source | Notes |
|---|---|---|
| `init.json` | `<net>/.lingtai/<agent>/init.json` | identity, manifest, addons, env_file |
| API keys | `.env` via `init.json` `env_file` | `~/.lingtai-tui/.env` shared by all agents |
| Kernel/runtime | installed kernel + venv | boot-time readiness check |
| Addon secrets | `<agent>/.secrets/<addon>.json` | operator-supplied, fragile |
| Heartbeat | writes on a schedule | proves liveness to peers/mail |

## What the TUI needs to run

Classified per `tui/CONTRACT.md`:

| Class | Requirement | Source | Notes |
|---|---|---|---|
| R1 (project-scoped) | agents exist | `<net>/.lingtai/<agent>/init.json` | same source as agents |
| R1 (project-scoped) | kernel/runtime | installed kernel + venv | `lingtai-tui doctor` checks bootstrap |
| R1 (project-scoped) | addon secrets | `<net>/.lingtai/.secrets/*.json` (agent dirs) | same files agents use |
| R2 (derived) | API keys | `~/.lingtai-tui/.env` via `ResolveKeys` | `.env` is the single source of truth |
| R3 (additive) | `config.json` | `~/.lingtai-tui/config.json` | UI mirror: `keys` (regenerable from `.env`) + legacy `language`; missing → defaults + degraded banner |
| R3 (additive) | `tui_config.json` | `~/.lingtai-tui/tui_config.json` | UI prefs (language/theme/page size/insights/truncate/auto-refresh); missing → defaults loaded silently (fable F9) |

## Misalignment matrix

| # | Divergence | Incident/risk | Fix direction |
|---|---|---|---|
| M1 | **API keys**: agents read `.env` (env_file); TUI read `config.json` `keys` | 2026-08-08: reinstall wiped `config.json`, agents kept running with `.env` keys; TUI hard-blocked into API-key recovery wizard | Make `.env` the single source of truth (R2); `config.json` `keys` becomes an R3.1 regenerable mirror — self-heal is the defined response to an additive mirror loss, not a patch |
| M2 | **Version skew**: TUI binary version vs kernel expectations | Wrong-fork build reported `v0.4.36-20-g873af31` while release was `v0.12.0`; string-compare bugs misreported updates | Semver compare; doctor reports TUI vs kernel versions; verify build source |
| M3 | **`.secrets/` fragility**: operator-supplied addon secrets deleted by config incidents | imap/feishu/whatsapp MCP declared but cannot boot after wipe | Doctor lists missing addon secrets explicitly (R1); operator re-supply |
| M4 | **Recovery UX is binary**: TUI either works or hard-blocks into wizard | User trapped re-typing keys that already exist | Degraded launch: TUI starts with a persistent warning banner, disables only key-dependent features, doctor reachable — the defined degradation for R3 loss |
| M5 | **Doctor coverage**: `lingtai-tui doctor` only checks update/bootstrap, not the config/env/secrets surface | Gaps like M1/M3 invisible to the existing doctor | Extend doctor with the TUI-can't-start diagnostic set (D1–D5) validating R1/R2/R3 |

## Design direction

1. **Define requirements first, then behavior.** The contract classifies
   everything the TUI needs into R1 (project-scoped), R2 (derived from
   `.env`), R3 (purely additive with defaults). Self-heal, degraded launch,
   and doctor checks are all *derived consequences* of those classes — they
   are the defined response to a specific requirement state, never ad-hoc
   patches. (Jason: self-heal alone is a bandaid; first define what the TUI's
   additional requirements are.)
2. **Single source of truth for API keys (R2)**: `.env` (already the agent
   boot source) is canonical; `config.json` keys are a regenerable R3.1
   mirror, never the exclusive store. A missing `config.json` is a degraded
   condition, not a setup event.
3. **Blanket doctor** for "agents running but TUI can't": detect agents alive
   (R1), check `config.json`/`tui_config.json` (R3), `.env` keys (R2),
   `.secrets` (R1), runtime/version (R1); for each finding propose the exact
   fix (self-heal where safe, operator action where not).
4. **Degraded launch (M4)**: when an R3 item is missing, start the TUI with
   clear signage and disable only the affected features; the doctor is one
   keystroke away instead of a hard gate.

## Follow-ups

- [x] PR #815 (this change): contract (R1/R2/R3 + decision table) + `.env`
      single-source (`ResolveKeys`/`ReadEnvKeys`/`HasAPIKeys`) + self-heal as
      R3.1 mirror regeneration + degraded launch banner + misalignment doc
- [ ] Extend `lingtai-tui doctor` with the TUI-can't-start diagnostic set (D4
      `.secrets`, D5 runtime/version skew) — the remaining unchecked rows
- [ ] Degraded-mode key feature gating beyond the banner (which views/actions
      are limited when key-dependent features are disabled)
