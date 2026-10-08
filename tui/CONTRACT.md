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
  - tui/internal/tui/preset_editor_test.go
  - tui/internal/config/codex_public_models.go
  - tui/internal/config/codex_public_models_test.go
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
4. The startup TUI binary update verifies the installed binary from captured
   command stdout/stderr and preserves each command's exit status, even while
   that output streams live. Streamed command output is not repeated in the
   update summary; non-command diagnostics (orchestration, verification, and
   failure lines) are never suppressed.

## Provider families

The TUI ships exactly four provider families, matching the providers the
kernel accepts, with one built-in template each (`BuiltinPresets()`, in picker
order): `codex` (ChatGPT OAuth; the first-run default), `claude` (provider
`claude-code`, the local Claude Code CLI login), `openai` (any
OpenAI-compatible endpoint), and `anthropic` (any Anthropic
Messages-compatible endpoint). There are no per-vendor templates, region
tables, or vendor-named providers; another vendor, subscription, gateway, or
account pool (for example sub2api / subs-pool) is reached through `openai` or
`anthropic` with that endpoint as `base_url`.

- `base_url` is optional free text for every family. Empty means the official
  endpoint for `openai` (`https://api.openai.com/v1`) and `anthropic`
  (`https://api.anthropic.com`); `codex` keeps its `/backend-api/codex` route
  and `claude-code` has none.
- `api_key_env` defaults to `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`; saving an
  edited template stamps a fresh `<PROVIDER>_<N>_API_KEY` slot. `codex` and
  `claude-code` carry no key slot.
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
  codex/claude-code: login-owned, no network call) and reports any other
  provider as unsupported. Setup → Credentials derives an API key's endpoint
  from the openai/anthropic preset that declares its env var and never
  reports a stored key as failed merely because no endpoint is known.

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
only ever grows rots into one. Codex is the one authorized exception to this
framing below — it trades a served-today promise for account-verifiable
public suggestions — so read the Codex exception before assuming a claim
below still covers it.

**Two-generation rule.** For every STILL-STATIC model family, the TUI ships
only the **latest two generations**. This binds:

- `providerModels` (`tui/internal/tui/preset_editor.go`) — the curated,
  package-level catalog for the ←/→ picker on the editor's model row, looked
  up by `modelOptions(provider)`. Only one subscription route is a static
  catalog here now: the Claude Code CLI aliases. (Codex was the other static
  entry through 2026-09; it is now sourced per the Codex exception below and
  is deliberately absent from this map.) The `openai` and `anthropic`
  families point at arbitrary endpoints, so their model row is free text and
  their templates ship an empty model that Save requires the user to fill
  in. When the user switches provider, a curated id the new family cannot
  serve falls back to that family's first catalog entry, or is cleared for a
  free-text family; arbitrary user text is never rewritten;
- the default `model` of every built-in preset constructor
  (`tui/internal/preset/preset.go`), which for a picker-bearing provider must
  itself be the first of that provider's shipped ids — for Codex, the first
  entry of the offline static fallback (`config.DefaultCodexModelOptions()`,
  see below), not a live-fetched id.

The TUI makes no per-model vision claim: templates declare `vision: inherit`
and whether the configured model accepts images is a runtime fact.

Reading of the rule:

| Term | Meaning |
|---|---|
| family | one model line in a curated catalog — the Claude Code alias line |
| generation | the version step within the family — e.g. `gpt-6` vs `gpt-5.6` in a hypothetical hand-curated family |
| **not** a generation | a variant inside one generation — `-mini`-style suffixes. All variants of a kept generation stay. |
| exempt | catalogs with no generation ladder: CLI aliases naming concurrent tiers (`opus`/`fable`/`sonnet`/`haiku`). Free-text rows (`openai`, `anthropic`) have no catalog to curate. Codex is also exempt, but for a different reason — see below, not "no ladder." |

**Standing obligation.** Adding a new generation to a family still governed by
this rule (today, only Claude Code's CLI aliases can gain/lose a tier) is the
same change that removes the third-newest one — from `providerModels` and
from any provider manual under
`tui/internal/preset/skills/lingtai-preset-skill/reference/` that enumerates
the lineup. Never rewrite `presets/saved/`: a user pinned to a retired id
keeps working, the picker just stops offering it.

### Codex exception: public-directory-sourced suggestions

Human-authorized override (2026-09-30), superseding the two-generation rule
and the "served today" promise above for Codex ONLY. Every other provider's
promise in this section, and the rest of this contract (Claude Code CLI
aliases, `openai`/`anthropic` free-text policy, the explicit preset-revision
contract below), is unchanged.

Codex's model row is **per-editor-instance state**
(`PresetEditorModel.codexModels` in `tui/internal/tui/preset_editor.go`), not
a package-level static map: every open editor independently seeds, fetches,
and displays its own copy — there is no shared mutable catalog to pollute
across concurrently open editors.

- **Source and binding filter.** On preset-editor entry (`Init()`), the
  editor starts one asynchronous, bounded fetch of the fixed public directory
  `https://raw.githubusercontent.com/openai/codex/main/codex-rs/models-manager/models.json`
  (`config.RefreshCodexPublicModels`, `tui/internal/config/codex_public_models.go`).
  No OAuth/API-key request, no Codex CLI dependency, and no new kernel
  service are involved. An entry is retained if and only if its
  `display_name` string STARTS WITH the exact case-sensitive prefix `GPT`;
  every other field in the source document — including any numeric
  generation, visibility, or account-entitlement flag — is read by nothing
  in this TUI and filters nothing. This directory is public metadata, not
  proof that the current account can use a listed model; the kernel/endpoint
  response when the agent actually runs is the only real entitlement check.
- **Slug vs. label.** The retained `display_name` is shown to the user
  UNCHANGED (the picker's visible label); the entry's `slug` is what gets
  persisted into `manifest.llm.model` and is the only value ←/→ cycling
  compares. Entries are deduplicated by slug. A saved or hand-typed model not
  present in the current lineup is still rendered (hollow, by its raw slug)
  and remains fully usable — a model missing from the catalog is never
  cleared, hidden, or blocked from Save merely because it fell out of (or
  never appeared in) the public list.
- **Bounded, fail-open network behavior.** The fetch is bounded (~5s timeout,
  ~2MiB response cap). On success it overwrites a last-good cache under the
  caller's `<globalDir>/cache/`, never under the saved-preset tree. On ANY
  failure — timeout, transport error, non-200, malformed/empty body, or an
  oversized body — the editor falls back to that last-good cache, and only
  then to a small compiled-in offline static list
  (`config.DefaultCodexModelOptions()`), so the picker is never empty and a
  single bad response can never erase a previously good cache.
- **Selection- and edit-preservation.** A completed refresh replaces ONLY the
  editor's own candidate list. It never touches the working preset's selected
  or custom model id, an in-progress inline/clone-name text edit, the
  editor's mode, or its dirty-vs-clean state — whatever the editor is doing
  when the refresh result arrives. There is no network gate on Save: `commit()`
  still runs only structural `Validate()` (see "Preset editor service tier"
  and the availability-save-gate skill referenced elsewhere in this repo).
  Refreshing never auto-changes a saved preset, an agent's active preset, its
  model, or its thinking level.
- **Explicit custom entry.** Pressing `c` while focused on the Codex model
  row opens the same inline free-text editor every free-text provider already
  uses, prefilled with the current slug, so Codex is never trapped cycling
  only the public-directory suggestions — an arbitrary account-specific or
  brand-new id remains one keystroke away regardless of what the directory,
  cache, or offline fallback currently contain.

Per-provider source lists, the rest of the inclusion checklist (served on our
endpoint, GA not preview, documented vision, subscription gates) for
`claude-code`, and the removal procedure for a still-static family live in
`tui/internal/tui/SKILL.md`, which also carries the Codex exception's
pointer back here.

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

## Preset editor Codex paid credits

The native `codex` preset exposes a cursor-reachable Off/On row for paid
credits; other providers hide it. Missing, false, and malformed values display
Off. On commits JSON boolean `true` as `manifest.llm.codex_allow_credits`; Off
omits it. Saved presets preserve the explicit choice through save/load and
Agent init generation. Switching away clears it, and switching back requires a
new opt-in. The editor does not query or change billing. A supporting kernel
checks included usage with the option off and permits credit-backed requests
with it on; OpenAI decides charges. See the
[Codex preset manual](internal/preset/skills/lingtai-preset-skill/reference/codex/SKILL.md).
