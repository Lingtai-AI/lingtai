---
name: tui-runtime-contract
contract_version: 2
root_contract: CONTRACT.md
related_files:
  - tui/ANATOMY.md
  - tui/internal/config/global.go
  - tui/internal/tui/firstrun.go
  - tui/internal/tui/app.go
  - tui/internal/tui/layout.go
  - tui/internal/tui/props.go
  - tui/internal/tui/setup.go
  - tui/internal/tui/preset_library.go
  - tui/main.go
  - tui/internal/config/global_test.go
  - docs/tui-agent-alignment.md
maintenance: |
  This contract defines what the TUI needs to launch and run, classified by
  whether each requirement is project-scoped (R1), derived from the agent
  source of truth (R2), or purely additive user-level state (R3). The startup
  decision table, doctor checks, and degraded-launch behavior must all be
  derivable from these three classes — never added as one-off patches.
  Keep it reciprocal with docs/tui-agent-alignment.md and with the config
  resolution code (ResolveKeys / ReadEnvKeys / HasAPIKeys). When a
  requirement class or a concrete additive item changes, update this contract
  and the doctor checks that validate against it together. Bump
  contract_version for a breaking change to the requirement classes.
---
# TUI Runtime Contract

## Definition principle

An agent is defined **solely** by `<project>/.lingtai/<agent>/init.json`
(identity, manifest, addons, env_file). Everything it needs at runtime derives
from that file, its env_file, and the installed kernel.

The TUI is defined the same way: it manages the project-scoped network
(`<project>/.lingtai/`) and therefore depends on the same R1/R2 requirements
as the agents it runs. On top of that it has a small, **purely additive** set
of user-level requirements under `~/.lingtai-tui/` (R3).

**Additive invariant:** a missing R3 item must **never** block launch. Every
R3 item has a default value and a defined degradation; the startup decision
table gates only on R1/R2. This is what makes the system robust: losing a
preferences file is a degraded condition with a defined response, not a setup
event and not a surprise.

## Requirement classes

### R1 · Project-scoped (required, sourced from the network)

| Requirement | Source | Notes |
|---|---|---|
| Agents exist | `<project>/.lingtai/<agent>/init.json` | zero agents → real first-run setup |
| Runtime/kernel | installed kernel + venv | readiness check; bootstrap gated on human consent |
| Agent env | `<project>/.lingtai/<agent>/init.json` env_file | defines where agents load `.env` |

### R2 · Derived from the agent source of truth (.env)

| Requirement | Source | Notes |
|---|---|---|
| API keys | `~/.lingtai-tui/.env` (via `ResolveKeys`) | agents load this at boot; `.env` is authoritative |

### R3 · Purely additive (user-level, loss must not block launch)

Every item below has a default and a defined degradation. The doctor
validates each item; the TUI launches regardless of its state.

| ID | Item | Location | Default | Degradation when missing |
|---|---|---|---|---|
| R3.1 | `keys` mirror | `~/.lingtai-tui/config.json` `keys` | none (derived from `.env`) | Keys still resolve from `.env` (R2) and every TUI key consumer reads through `ResolveKeys`, so the mirror is a cache, not a gate. Mirror is regenerable — self-heal rewrites it from `.env` on demand. |
| R3.2 | TUI preferences | `~/.lingtai-tui/tui_config.json` | `language: en`, `theme: ink-dark`, `mail_page_size: 200`, `insights: false`, `tool_call_truncate: 0` (no truncation), `auto_refresh: on` | Loaded defaults replace the file silently; no banner (fable F9). |
| R3.3 | Legacy `language` | `~/.lingtai-tui/config.json` `language` | n/a (deprecated) | Migrated to `tui_config.json` by `MigrateLegacyLanguage`; ignored once migrated. |

## Startup decision table

The table gates **only** on R1/R2. R3 loss never appears as a launch gate; it
appears as the degraded state below.

| State | Decision |
|---|---|
| No agents in `.lingtai/` (R1 fail) | first-run wizard (create first agent) |
| Agents exist, `config.json` missing or its keys mirror empty (R3.1 loss), `.env` has API keys (R2 ok) | **degraded launch** — derive keys from `.env`, show persistent banner, key-dependent features limited; self-heal offered to regenerate the R3.1 mirror; recovery wizard not forced. Content-based (fable F7): a present-but-keyless mirror degrades exactly like an absent file. |
| Agents exist, `.env` has no API keys (R2 fail) | recovery wizard (real missing key → setup) |
| Agents exist, everything present | normal launch |

## Alignment rules

1. `.env` is the single source of truth for API keys (R2). `config.json` keys
   are an R3.1 mirror; `ResolveKeys` prefers `.env` and fills gaps from the
   mirror for legacy setups.
2. Losing `~/.lingtai-tui/config.json` (or its keys mirror) is an R3 degraded
   condition, not a setup event — the TUI launches with a banner and offers
   self-heal for the regenerable mirror (R3.1). Losing `tui_config.json`
   (R3.2) is the same class but degrades silently: defaults are loaded with
   no banner (fable F9).
3. The doctor *will* validate the startup decision table programmatically
   (fable F8: D2/D3 are currently unbuilt — see the checks below): check
   agents present (R1), `config.json`/`tui_config.json` presence (R3),
   `.env` API keys (R2), `.secrets` for declared addons (R1), runtime/version
   (R1).

## Doctor checks (TUI-can't-start diagnostic set)

- [x] D1 agents running / orchestrators detected (R1)
- [ ] D2 config.json present — `ResolveKeys` configOK (R3.1, not yet built)
- [ ] D3 .env API keys present — `HasAPIKeys` (R2, not yet built)
- [ ] D4 addon `.secrets` present for declared addons (R1 follow-up)
- [ ] D5 runtime/version skew reported (R1 follow-up, extends existing doctor)
