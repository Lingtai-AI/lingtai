---
name: tui-runtime-contract
contract_version: 2
root_contract: CONTRACT.md
related_files:
  - tui/ANATOMY.md
  - tui/internal/config/global.go
  - tui/internal/tui/firstrun.go
  - tui/internal/tui/app.go
  - tui/internal/tui/puffo_acp_lifecycle_test.go
  - tui/internal/processscan/check.go
  - tui/internal/processscan/check_test.go
  - tui/internal/process/launcher.go
  - tui/purge_common.go
  - tui/internal/tui/layout.go
  - tui/internal/tui/props.go
  - tui/internal/tui/preset_library.go
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/SKILL.md
  - tui/internal/preset/preset.go
  - tui/internal/preset/revision.go
  - tui/internal/preset/revision_test.go
  - tui/internal/headless/preset_revision.go
  - tui/internal/headless/preset_revision_test.go
  - tui/internal/preset/skills/lingtai-preset-skill/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/codex/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/claude/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/openai/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/anthropic/SKILL.md
  - tui/internal/tui/doctor.go
  - tui/internal/tui/login.go
  - tui/internal/tui/claude_auth.go
  - tui/main.go
  - tui/main_preset_revision_test.go
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

## Puffo-owned ACP agent visibility

The TUI's process inventory and duplicate-launch gate recognize the fixed
`lingtai-agent acp --profile puffo-v1 --runtime-id <id> --registry <path>`
launch. The ACP command does not expose its workdir, so discovery reads the
local registry to map the runtime id to its agent directory. Discovery is
best-effort: unreadable, missing, or malformed registry entries are not
reported as running processes. Registry discovery rejects non-regular files
before opening them and must not block on a FIFO; existing agent directories
are matched by filesystem identity so opening the TUI through a symlink does
not bypass Puffo lifecycle protection. The kernel workdir lease remains the
authoritative duplicate-run protection.

Opening the TUI remains filesystem observation, not a second ACP session or a
transfer of process ownership. Puffo owns the ACP process and transport:
TUI `/refresh` and `/cpr` must reject a visible Puffo-owned process before
writing lifecycle signals or removing its workdir lock, and TUI process
termination/purge must never target that process. To hand the agent over to a
LingTai-started process, pause it in Puffo, start it in LingTai, then resume it
in Puffo so Puffo attaches to that running process.

On POSIX, every TUI-started `lingtai-agent run` child receives
`LINGTAI_ACP_SOCKET_AGENT_DIR` set to that agent's canonical absolute directory.
Kernels that support the resident ACP socket may then accept a Puffo attach;
older kernels ignore the environment marker without losing the normal run
path. Windows launches omit the marker because the resident socket requires
POSIX peer credentials. The TUI itself never opens the socket or assumes an
attach succeeded.

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

A consented runtime rebuild preserves the previous venv until the replacement passes interpreter and import checks. Marker stamping is best-effort. On failure, it restores the previous venv and removes the incomplete replacement; on success, it removes the temporary backup.
| Agent env | `<project>/.lingtai/<agent>/init.json` env_file | defines where agents load `.env` |

### R2 · Derived from the agent source of truth (.env)

| Requirement | Source | Notes |
|---|---|---|
| API keys | `~/.lingtai-tui/.env` (via `ResolveKeys`) and each agent's `manifest.llm.api_key_env` | agents load this at boot; `.env` is authoritative. The declared variable name is valid even without an `_API_KEY` suffix. |

### R3 · Purely additive (user-level, loss must not block launch)

Every item below has a default and a defined degradation. The doctor
validates the launch-relevant subset (R1/R2/R3.1) programmatically via the
D1-D5 checks below; R3.2/R3.3 load defaults silently by design and are not
examined by the doctor.

| ID | Item | Location | Default | Degradation when missing |
|---|---|---|---|---|
| R3.1 | `keys` mirror | `~/.lingtai-tui/config.json` `keys` | none (derived from `.env`) | Keys still resolve from `.env` (R2) and every TUI key consumer reads through `ResolveKeys`, so the mirror is a cache, not a gate. Mirror is regenerable — self-heal rewrites it from `.env` on demand. |
| R3.2 | TUI preferences | `~/.lingtai-tui/tui_config.json` | `language: en`, `theme: ink-dark`, `mail_page_size: 200`, `tool_call_truncate: 0` (no truncation), `auto_refresh: on`, `home_telemetry_display: absent` (the Home telemetry row's built-in default expression) | Loaded defaults replace the file silently; no banner (fable F9). `home_telemetry_display` additionally fails closed per key: any invalid value (empty, over-long, repeated, unknown name, wrong type) is discarded on load — the row renders its built-in default expression, the other preferences in the file are untouched — and is omitted on save, so an invalid value is never re-written as durable config. |
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
| Agents exist, resolved keys have neither a conventional `_API_KEY` nor a nonempty agent-declared `api_key_env` (R2 fail) | recovery wizard (real missing key → setup) |
| Agents exist, everything present | normal launch |

## Alignment rules

1. `.env` is the single source of truth for API keys (R2). `config.json` keys
   are an R3.1 mirror; `ResolveKeys` prefers `.env` and fills gaps from the
   mirror for legacy setups. Startup and doctor also accept the exact
   `manifest.llm.api_key_env` named by an orchestrator, even if that name does
   not use the conventional `_API_KEY` suffix; unrelated env variables do not
   satisfy R2.
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

## Provider families

The TUI ships exactly four provider families, matching the providers the
kernel accepts, with one built-in template each (`BuiltinPresets()`, in picker
order): `codex` (ChatGPT OAuth; the first-run default), `claude` (provider
`claude-code`, shown as `claude-p`: the local Claude login, or a
`claude setup-token` OAuth token), `openai` (any OpenAI-compatible
endpoint), and `anthropic` (any Anthropic Messages-compatible endpoint). There are no per-vendor templates, region
tables, or vendor-named providers; another vendor, subscription, gateway, or
account pool (for example sub2api / subs-pool) is reached through `openai` or
`anthropic` with that endpoint as `base_url`.

- `base_url` is optional free text for every family. Empty means the official
  endpoint for `openai` (`https://api.openai.com/v1`) and `anthropic`
  (`https://api.anthropic.com`); `codex` keeps its `/backend-api/codex` route
  and `claude-code` has none.
- `api_key_env` defaults to `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`; saving an
  edited template stamps a fresh `<PROVIDER>_<N>_API_KEY` slot. `codex`
  carries no key slot. `claude-code` uses the one shared
  `CLAUDE_CODE_OAUTH_TOKEN` slot (a setup-token belongs to the Claude
  account, so it is never numbered; a legacy Claude preset with no
  `api_key_env` reads that slot too).
- `claude-code` credential order: a stored `claude setup-token` token (in
  `~/.lingtai-tui/.env` under the preset's `api_key_env`) takes precedence;
  otherwise the local `claude` CLI login is used. The TUI detects the login
  with the non-billed `claude auth status --json` and never spends a model
  call to verify either path. It runs that probe only where a Claude
  credential is shown or decided — the Claude preset editor, `/presets`,
  Setup → Credentials and `/doctor` when Claude is in use, and Next on a
  Claude preset in first-run — never while merely constructing the
  first-run wizard, so a first-run that picks another family never execs
  `claude`. The first-run paste step asks for a token (hint: run
  `claude setup-token` and paste it) only when neither is present.
- The `claude` template carries no `model` and no `thinking`: Claude Code
  runs its own default model and effort. `Validate` accepts a model-less
  `claude-code` preset (and only that family), and saving a Claude preset
  drops `model`, `thinking`, `base_url`, `service_tier`, and `wire_api`.
- The preset editor shows only the chosen family's fields: openai — model,
  service tier, reasoning, wire format (+ transport on Responses), base_url,
  key; anthropic — model, reasoning, base_url, key; codex — model, service
  tier, reasoning, base_url, account; claude-code — the auth row only. The
  family is named in the LLM section header; the four-family provider
  choice is offered only to convert a legacy saved provider.
- `wire_api` exists only on `openai`: `chat_completions` (the default, always
  written explicitly) or `responses`; `responses_transport` (`http` omitted
  default, or `websocket`) only on the Responses wire.
- The TUI never writes the retired `manifest.llm.api_compat` field: the
  editor drops it on commit and `init.json` generation drops it from the
  copied llm block. Existing files are not rewritten just to remove it.
- Template capabilities never name a vendor: `web_search` uses DuckDuckGo and
  `vision` inherits the agent's own provider (`codex`, `openai`,
  `anthropic`); the `claude` template declares neither.
- `/doctor` probes by family (openai: Bearer `GET {base_url}/models`;
  anthropic: `x-api-key` + `anthropic-version` `GET {base_url}/v1/models`;
  codex: login-owned, no network call; claude-code: presence only, no
  network or model call — reports the active path: setup-token present,
  local Claude login, or neither with the `claude setup-token` hint) and
  reports any other provider as unsupported. Setup → Credentials derives an
  API key's endpoint from the openai/anthropic preset that declares its env
  var and never reports a stored key as failed merely because no endpoint is
  known; a Claude setup-token is listed by presence only, and a Claude line
  shows which Claude path is active when a Claude preset, agent, or token is
  in use.

## Preset editor service tier

The preset editor exposes the service-tier row for the two families that
accept it — `openai` and `codex` — and hides it for `anthropic` and
`claude-code`. Its vocabulary is exactly `normal | fast` (the lower layer sends
`fast` as `priority`); where shown, the row is cyclable and cursor-reachable,
and the TUI does not probe endpoint support.

Missing or `normal` `manifest.llm.service_tier` displays as `normal` and is
omitted from the committed manifest. Selecting `fast` commits the string
`"fast"`. Unknown legacy values display as `normal` and are omitted on commit;
a stale value on a family without service tiers is dropped on commit (and on a
provider switch into such a family).

## Model list curation

The model ids the TUI offers are a contract with the user: everything the
picker shows must be something the chosen endpoint actually serves today. A
retired id in the list is a 4xx the user did not ask for, and a list that
only ever grows rots into one.

**Two-generation rule.** For every model family, the TUI ships only the
**latest two generations**. This binds:

- `providerModels` (`tui/internal/tui/preset_editor.go`) — the curated
  catalog for the ←/→ picker on the editor's model row, looked up by
  `modelOptions(provider)`. Only the Codex OAuth route carries one. The
  `openai` and `anthropic` families point at arbitrary endpoints, so their
  model row is free text and their templates ship an empty model that Save
  requires the user to fill in; `claude-code` has no model row at all.
  When a legacy provider is converted, a curated id the new family cannot
  serve falls back to that family's first catalog entry, or is cleared for a
  free-text family; arbitrary user text is never rewritten;
- the default `model` of every built-in preset constructor
  (`tui/internal/preset/preset.go`), which for a picker-bearing provider must
  itself be the first of that provider's shipped ids.

The TUI makes no per-model vision claim: templates declare `vision: inherit`
and whether the configured model accepts images is a runtime fact.

Reading of the rule:

| Term | Meaning |
|---|---|
| family | one model line in a curated catalog — the Codex `gpt-*` line |
| generation | the version step within the family — `gpt-6` vs `gpt-5.6` |
| **not** a generation | a variant inside one generation — `-mini`, the `gpt-5.6-sol/-terra/-luna` routes. All variants of a kept generation stay. |
| exempt | Free-text rows (`openai`, `anthropic`) and `claude-code` (no model row; Claude Code picks its default) have no catalog to curate. |

**Standing obligation.** Adding a new generation is the same change that
removes the third-newest one — from `providerModels` and from any provider
manual under
`tui/internal/preset/skills/lingtai-preset-skill/reference/` that enumerates
the lineup. Never rewrite `presets/saved/`: a user pinned to a retired id
keeps working, the picker just stops offering it.

Per-provider source lists, the rest of the inclusion checklist (served on our
endpoint, GA not preview, documented vision, subscription gates), and the
removal procedure live in `tui/internal/tui/SKILL.md`.

## Explicit preset revision contract

`lingtai-tui presets revise` is a separate headless operation. It consumes only
the explicit manifest and input paths, and dispatches before preset bootstrap
or any global/provider/runtime access. The manifest pins both the input bytes
and the exact expected post-image SHA-256. A pinned input is planned and
verified against that post-image; a different input is accepted as already
materialized only when its exact bytes match the declared post-image and its
name, provider, and represented route bindings still validate.

Targets use the typed `revise`, `unsupported`, or `no-op` state. Non-revision
targets require a deterministic reason, declare no model data or changes, and
report an explicit unchanged result. Two evidenced generations and explicit
retirements are required only for revision targets whose owned changes concern
a model or model list. A revision route is established either by exact direct
input bindings for API, transport, and scope, or by a typed `provider_child`
binding of the real input provider to provider-specific route facts; arbitrary
markers do not bind a route. Capability changes name their exact model records,
promotions require supported facts for those records, and a same-plan
retirement cannot remove a referenced model.

Named built-in preset revision guidance is one direct child per
`BuiltinPresets()` name under
`tui/internal/preset/skills/lingtai-preset-skill/reference/<name>/SKILL.md`.
Those 4 family children own family-specific endpoint and credential facts,
the Codex/CLI catalog distinctions, exact TUI surfaces, and the reviewed
revision procedure. Multi-account pooling is not built into the TUI (there is
no `codex-pool` template, provider, or pool file): the `openai` child owns the
recipe for reaching an external pool such as sub2api / subs-pool as an
`openai` Responses preset. The operation axis remains the five shared children:
saved-presets, endpoint-capabilities, availability-save-gate,
activation-session-refresh, and troubleshooting-migration. The deterministic
production CLI adapter and pure engine remain shared at
`tui/internal/headless/preset_revision.go` and
`tui/internal/preset/revision.go`; they are not a sixth operation child.
`wire_api` / `responses_transport` revisions are eligible only for `openai`
Responses targets.

Requested and observed Responses service-tier vocabularies are distinct;
ordinary `service_tier` paths are request-side, and service-tier and reasoning
replacements must be strings. Codex keeps its four-level reasoning
vocabulary. Owned and change JSON-pointer paths are disjoint by ancestry before
dry-run/check/apply can emit a plan; apply repeats the overlap check as defense
in depth. The splice engine unconditionally preserves unowned JSON bytes and
retains the existing 0/1/2/3/4 exit mapping.

Apply stages a document or bundle and refuses an output path that exists when
inspected, then attempts one rename. The no-replace guarantee is not
synchronized against a concurrent creator between inspection and publication,
so that race can have platform-dependent behavior.

## Doctor checks (TUI-can't-start diagnostic set)

- [x] D1 agents running / orchestrators detected (R1)
- [x] D2 config.json present, readable, and keys mirror non-empty — `ResolveKeys` configOK + `HasAPIKeys(mirror, declaredKeyEnvs...)` using all detected orchestrators' declarations (R3.1, content-based fable F7)
- [x] D3 effective API keys present — `HasAPIKeys(resolved, declaredKeyEnvs...)` using the same network-wide declarations (.env + mirror gap-fill, matching the gate)
- [x] D4 addon `.secrets`/config present for declared addons (R1; honors declared `mcp.<addon>.env` / legacy `addons.<name>.config` paths)
- [x] D5 runtime/version skew reported (R1, extends existing doctor; plain-release stamps only)

The set is reachable both from the interactive `/doctor` view and the
`lingtai-tui doctor` CLI (fable F5) so the checks that force the first-run /
recovery wizards can be surfaced when the TUI itself cannot start.
