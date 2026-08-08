# TUI ↔ Agent Requirement Alignment

Status: draft · 2026-08-08 · authored from the config.json recovery incident

## Problem statement

A LingTai network can be healthy (agents running, mail flowing, kernels alive)
while the **TUI cannot start correctly**. When that happens the user is dropped
into a recovery wizard that asks for API keys the system already has — because
the TUI and the agents read credentials from **different stores**.

Goal: **if an agent can run, the TUI should run too.** Where the two sides
diverge, the doctor should diagnose the exact gap and propose the exact fix.

## What an agent needs to run

| Requirement | Source | Notes |
|---|---|---|
| `init.json` | `<net>/.lingtai/<agent>/init.json` | identity, manifest, addons, env_file |
| API keys | `.env` via `init.json` `env_file` | `~/.lingtai-tui/.env` shared by all agents |
| Kernel/runtime | installed kernel + venv | boot-time readiness check |
| Addon secrets | `<agent>/.secrets/<addon>.json` | operator-supplied, fragile |
| Heartbeat | writes on a schedule | proves liveness to peers/mail |

## What the TUI needs to run

| Requirement | Source | Notes |
|---|---|---|
| `config.json` | `~/.lingtai-tui/config.json` | UI prefs + `keys` map (API keys) |
| API keys | `config.json` → `keys` | TUI does NOT read `.env` directly |
| Kernel/runtime | same kernel + venv | `lingtai-tui doctor` checks bootstrap |
| Addon secrets | `<net>/.lingtai/.secrets/*.json` (agent dirs) | same files agents use |
| TUI binary | `~/.local/bin/lingtai-tui` | version must match kernel expectations |

## Misalignment matrix

| # | Divergence | Incident/risk | Fix direction |
|---|---|---|---|
| M1 | **API keys**: agents read `.env` (env_file); TUI reads `config.json` `keys` | 2026-08-08: reinstall wiped `config.json`, agents kept running with `.env` keys; TUI hard-blocked into API-key recovery wizard | Make `.env` the single source of truth; TUI derives/restores `config.json` from `.env` when missing (self-heal) |
| M2 | **Version skew**: TUI binary version vs kernel expectations | Wrong-fork build reported `v0.4.36-20-g873af31` while release was `v0.12.0`; string-compare bugs misreported updates | Semver compare; doctor reports TUI vs kernel versions; verify build source |
| M3 | **`.secrets/` fragility**: operator-supplied addon secrets deleted by config incidents | imap/feishu/whatsapp MCP declared but cannot boot after wipe | Doctor lists missing addon secrets explicitly; operator re-supply |
| M4 | **Recovery UX is binary**: TUI either works or hard-blocks into wizard | User trapped re-typing keys that already exist | Degraded launch: TUI starts with a persistent warning banner, disables only key-dependent features, doctor reachable |
| M5 | **Doctor coverage**: `lingtai-tui doctor` only checks update/bootstrap, not the config/env/secrets surface | Gaps like M1/M3 invisible to the existing doctor | Extend doctor with the TUI-can't-start diagnostic set |

## Design direction

1. **Single source of truth for API keys**: `.env` (already the agent boot source)
   becomes canonical; `config.json` keys are derived/restored from it, never
exclusively hand-entered. A missing `config.json` is a recoverable condition,
not a setup event.
2. **Blanket doctor** for "agents running but TUI can't": detect agents alive,
   check `config.json`/`.env`/`.secrets`/runtime/version, and for each finding
   propose the exact fix (self-heal where safe, operator action where not).
3. **Degraded launch**: when a non-fatal requirement is missing, start the TUI
   with clear signage and disable only the affected features; the doctor is one
   keystroke away instead of a hard gate.

## Follow-ups

- [ ] PR #813: self-heal `config.json` from `.env` in the recovery wizard (M1 narrow fix)
- [ ] Extend `lingtai-tui doctor` with the TUI-can't-start diagnostic set (M5, M3)
- [ ] Degraded launch banner + feature gating (M4)
- [ ] `.env` single-source refactor for TUI keys (M1 long-term)
- [ ] Semver-aware version reporting in doctor (M2)
