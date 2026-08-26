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
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/SKILL.md
  - tui/internal/preset/preset.go
  - tui/internal/tui/stream_progress.go
  - tui/internal/streamprogress/client.go
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
validates the launch-relevant subset (R1/R2/R3.1) programmatically via the
D1-D5 checks below; R3.2/R3.3 load defaults silently by design and are not
examined by the doctor.

| ID | Item | Location | Default | Degradation when missing |
|---|---|---|---|---|
| R3.1 | `keys` mirror | `~/.lingtai-tui/config.json` `keys` | none (derived from `.env`) | Keys still resolve from `.env` (R2) and every TUI key consumer reads through `ResolveKeys`, so the mirror is a cache, not a gate. Mirror is regenerable — self-heal rewrites it from `.env` on demand. |
| R3.2 | TUI preferences | `~/.lingtai-tui/tui_config.json` | `language: en`, `theme: ink-dark`, `mail_page_size: 200`, `insights: false`, `tool_call_truncate: 0` (no truncation), `auto_refresh: on`, `home_telemetry_display: absent` (the Home telemetry row's built-in default expression) | Loaded defaults replace the file silently; no banner (fable F9). `home_telemetry_display` additionally fails closed per key: any invalid value (empty, over-long, repeated, unknown name, wrong type) is discarded on load — the row renders its built-in default expression, the other preferences in the file are untouched — and is omitted on save, so an invalid value is never re-written as durable config. |
| R3.3 | Legacy `language` | `~/.lingtai-tui/config.json` `language` | n/a (deprecated) | Migrated to `tui_config.json` by `MigrateLegacyLanguage`; ignored once migrated. |

Other files under `~/.lingtai-tui/` (`utilities/`, `registry.jsonl`) are
self-healing caches regenerated or tolerated on startup — implementation
details, never requirements, never launch gates.

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
   no banner (fable F9). Within R3.2, `home_telemetry_display` is the one key
   whose own loss is scoped to itself: it is an optional, hand-editable ordered
   selection of the Home telemetry row's existing fragments (`session`, `llm`,
   `api`, `tokens`, `cache`, `context` — nothing else, and no template, format,
   color, or width), so an invalid value falls back to the row's built-in
   default expression without disturbing the neighbouring preferences.
3. The doctor validates the startup decision table programmatically
   (fable F8, D1-D5 below): check agents present (R1), `config.json`
   presence (R3.1), `.env` API keys (R2), `.secrets` for declared addons
   (R1), runtime/version (R1).

## Live stream-progress badge (additive, loopback-only)

While the visible recipient is `ACTIVE`, Mail's `Email To:` indicator may
append ` · N tok downloaded` beside `Active N s`. The value is the kernel's
consumer-neutral, read-only `lingtai.stream-progress/v1` snapshot
(`GET /v1/stream-progress` on `127.0.0.1`, discovered deterministically from the
agent's stable `agent_id`; client and arithmetic in
`tui/internal/streamprogress/ANATOMY.md`, adapter in
`tui/internal/tui/stream_progress.go`), rendered as the integer
`streamed_chars / 4`. Whether an agent streams at all is kernel-owned runtime
configuration with exactly one owner path — the kernel's System settings
resolver: a valid `LINGTAI_STREAMING_ENABLED` environment value, otherwise a
valid boolean `streaming` in the agent's `settings/system.json`, otherwise on.
`init.json` carries no `streaming` field: `GenerateInitJSON`
(`tui/internal/preset/preset.go`), `templates/init.jsonc`, and
`examples/init.jsonc` emit none, `/kanban` shows no such row, and the TUI
offers no editor or writer for the System setting.

1. **Purely additive, fail-open, strict.** A missing, refused, redirecting,
   foreign, or malformed endpoint is never a launch gate, banner, or doctor
   check — the suffix is simply absent and the next poll tries again. The
   client never follows an HTTP redirect (so nothing can send it off
   loopback) and accepts only an exact seven-field v1 body: any extra field
   (a `text` field above all), duplicate key, or trailing data is rejected.
2. **RAM only.** No snapshot, port, or progress state is written under
   `.lingtai/` or `~/.lingtai-tui/`; the shared on-disk schema read by both
   binaries is unchanged.
3. **Never on the render path.** `View()` reads one cached reading; all
   loopback I/O runs in a poll-tick `tea.Cmd` and its result is accepted only
   when the Mail generation and the visible recipient's stable identity (Main
   orchestrator, or the validated direct target) still match.
4. **Active-only, immediate clear.** The suffix shows only while the recipient
   lifecycle is `ACTIVE` and the snapshot is active; it clears at once when
   either stops holding or the visible recipient changes. Narrow terminals omit
   it rather than wrap the interaction line.
5. **No text.** The kernel publishes character counts only; the TUI reads and
   shows no output preview.

## Model list curation

The model ids the TUI offers are a contract with the user: everything the
picker shows must be something the chosen endpoint actually serves today. A
retired id in the list is a 4xx the user did not ask for, and a list that
only ever grows rots into one.

**Two-generation rule.** For every model family, the TUI ships only the
**latest two generations**. This binds:

- `providerModels` (`tui/internal/tui/preset_editor.go`) — the ←/→ picker
  on the editor's model row;
- the default `model` of every built-in preset constructor
  (`tui/internal/preset/preset.go`), which for a picker-bearing provider must
  itself be one of that provider's shipped ids;
- `modelHasVision` (`tui/internal/tui/preset_editor.go`), which carries one
  entry per id in `providerModels` and no entry for a retired one. The
  bijection is what `TestModelHasVisionDeclaresEveryShippedModel` enforces,
  minus two deliberate exemptions it names: `nvidia` (a gateway catalog whose
  per-vendor vision facts we do not verify) and the `claude*` CLI-alias
  spellings. The exemptions apply only to the *requirement* to carry an
  entry — the reverse direction is not exempt: a `modelHasVision` entry for
  an id no picker ships still fails the test. Adding an exemption is a change
  to that test, not a silent omission;
- **not** the providers whose model row is free text. `kimi`, `gemini`,
  `openrouter`, and `custom` have no `providerModels` entry, so they have no
  `modelHasVision` entries either and the bijection test never sees them.
  Their default model is still expected to be a current generation
  (`kimi-for-coding`, `gemini-3-flash-preview`, `z-ai/glm-5.1`, and `custom`'s
  empty model), but the maps carry nothing for them and must not be given
  entries to "complete" the table — for `kimi` in particular, adding a
  `providerModels` entry flips its row from free text to picker-only and one
  `→` silently overwrites a typed Moonshot id. That deletion is deliberate and
  pinned by a negative assertion in `preset_editor_test.go`.

Reading of the rule:

| Term | Meaning |
|---|---|
| family | one vendor's model line — `MiniMax-M*`, `GLM-*`, `mimo-v*`, `deepseek-v*`, `gpt-5.*`, `kimi-k*` |
| generation | the version step within the family — `M3` vs `M2.7`; `GLM-5.2` vs `GLM-5.1`; `gpt-5.6` vs `gpt-5.5` |
| **not** a generation | a variant inside one generation — `-highspeed`, `-pro`, `-flash`, `-mini`, `-Air`, the `gpt-5.6-sol/-terra/-luna` routes, or the same generation respelled for another endpoint (`GLM-5.2` / `glm-5.2`). All variants of a kept generation stay. |
| exempt | catalogs with no generation ladder: CLI aliases naming concurrent tiers (`opus`/`fable`/`sonnet`/`haiku`), and gateway catalogs that list one current id per vendor (`nvidia`). The rule still applies per family inside such a list. Free-text model rows (`kimi`, `gemini`, `openrouter`, `custom`) are outside the `providerModels`/`modelHasVision` bindings entirely — see the fourth clause above. |

**Standing obligation.** Adding a new generation is the same change that
removes the third-newest one — from `providerModels`, from `modelHasVision`,
and from any provider manual under
`tui/internal/preset/skills/lingtai-preset-skill/reference/` that enumerates
the lineup. Never rewrite `presets/saved/`: a user pinned to a retired id
keeps working, the picker just stops offering it.

Per-provider source lists, the rest of the inclusion checklist (served on our
endpoint, GA not preview, documented vision, subscription gates), and the
removal procedure live in `tui/internal/tui/SKILL.md`.

## Doctor checks (TUI-can't-start diagnostic set)

- [x] D1 agents running / orchestrators detected (R1)
- [x] D2 config.json present, readable, and keys mirror non-empty — `ResolveKeys` configOK + `HasAPIKeys(mirror)` (R3.1, content-based fable F7)
- [x] D3 effective API keys present — `HasAPIKeys(resolved)` (.env + mirror gap-fill, matching the gate)
- [x] D4 addon `.secrets`/config present for declared addons (R1; honors declared `mcp.<addon>.env` / legacy `addons.<name>.config` paths)
- [x] D5 runtime/version skew reported (R1, extends existing doctor; plain-release stamps only)

The set is reachable both from the interactive `/doctor` view and the
`lingtai-tui doctor` CLI (fable F5) so the checks that force the first-run /
recovery wizards can be surfaced when the TUI itself cannot start.
