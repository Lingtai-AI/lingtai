---
name: tui-runtime-contract
contract_version: 1
root_contract: CONTRACT.md
related_files:
  - internal/config/global.go
  - internal/tui/firstrun.go
  - internal/tui/app.go
  - docs/tui-agent-alignment.md
maintenance: |
  This contract defines the TUI runtime requirements and their alignment with
  agent requirements. Keep it reciprocal with docs/tui-agent-alignment.md and
  with the config resolution code (ResolveKeys / ReadEnvKeys / HasAPIKeys).
  When a startup-path requirement changes, update this contract and the doctor
  checks that validate against it together. Bump contract_version for a
  breaking change to the requirement classes.
---
# TUI Runtime Contract

## Purpose

Define what the TUI needs to launch and run, and how those needs align with
what an agent needs to run. The invariant: **if an agent can run, the TUI
should be able to launch too.** Requirements are classified so a missing item
has a defined, programmatic response instead of an ambiguous hard gate.

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
| API keys | `~/.lingtai-tui/.env` (via `ResolveKeys`) | agents load this at boot; `.env` is authoritative, `config.json` keys fill gaps |

### R3 · Purely additive (user-level, loss must not block launch)

| Requirement | Source | Notes |
|---|---|---|
| `config.json` | `~/.lingtai-tui/config.json` | UI prefs + mirrored keys; missing → defaults + degraded banner |
| Theme/language | `config.json` | default when absent |

## Startup decision table

| State | Decision |
|---|---|
| No agents in `.lingtai/` | first-run wizard (create first agent) |
| Agents exist, `config.json` missing, `.env` has API keys | **degraded launch** — derive keys from `.env`, show persistent banner, key-dependent features limited; recovery wizard not forced |
| Agents exist, `config.json` missing, `.env` has no keys | recovery wizard (real missing key → setup) |
| Agents exist, everything present | normal launch |

## Alignment rules

1. `.env` is the single source of truth for API keys. `config.json` keys are a
   mirror; `ResolveKeys` prefers `.env` and fills gaps from `config.json`.
2. Losing `~/.lingtai-tui/config.json` is a degraded condition, not a setup
   event — the TUI launches with a banner and offers self-heal.
3. The doctor validates the startup decision table programmatically: check
   agents present, `config.json` presence, `.env` API keys, `.secrets` for
   declared addons, runtime/version.

## Doctor checks (TUI-can't-start diagnostic set)

- [x] D1 agents running (orchestrators detected)
- [x] D2 config.json present (`ResolveKeys` configOK)
- [x] D3 .env API keys present (`HasAPIKeys`)
- [ ] D4 addon `.secrets` present for declared addons (follow-up)
- [ ] D5 runtime/version skew reported (extends existing doctor)
