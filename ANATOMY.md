---
related_files:
  - CONTRACT.md
  - dev-guide-skill/SKILL.md
  - tui/architecture_documents_test.go
  - tui/ANATOMY.md
  - portal/ANATOMY.md
  - docs/ANATOMY.md
  - tui/internal/inventory/ANATOMY.md
  - README.md
  - README.zh.md
  - README.wen.md
  - RELEASING.md
  - CLAUDE.md
  - .github/workflows/release.yml
  - install.sh
  - kernel-release.json
  - scripts/publish_bundle_to_gitee.sh
  - scripts/sync_gitee_mirror.sh
  - tui/main.go
  - tui/go.mod
  - tui/Makefile
  - portal/main.go
  - portal/embed.go
  - portal/go.mod
  - portal/Makefile
maintenance: |
  This file is both the repository-root anatomy and the normative
  anatomy-of-anatomy for the distributed code navigation system across the two
  binaries (lingtai-tui, lingtai-portal) and the install pipeline. Keep
  related_files repo-relative, duplicate-free, and linked to real files. Keep
  the root CONTRACT.md reciprocal and update the paired conventions together
  when their boundary changes. Code is the structural source of truth: repair
  stale navigation in the same change that moves files, symbols, connections,
  composition, or state. Preserve the child template and its maintenance rule;
  validate the distributed graph before merge. See dev-guide-skill/SKILL.md for
  the workflow.
---

# lingtai

> **Maintenance:** this file and its `## Maintenance` section below are the
> normative convention. **Coding agents** update the relevant anatomy in the
> same commit as code changes. **LingTai agents** report drift as issues (mail
> or `discussions/<name>-patch.md`); do not silently fix.

## Purpose

**ANATOMY is the distributed code navigation system**, and this root file is
both its top-level map and its normative anatomy-of-anatomy. Each architectural
layer keeps an `ANATOMY.md` beside the code it maps, and those local maps link
into a graph an agent descends from this repository root to the exact code that
answers a structural question. Anatomy owns structure (code is the source of
truth); [`CONTRACT.md`](CONTRACT.md) is the paired system defining what each
layer promises. `## Components` below is the repository map — **start there**;
`## Anatomy convention` after it owns the schema and link rules.

This repo is the Go side of LingTai: `lingtai-tui`, `lingtai-portal`, and the
install pipeline. The Python kernel (`lingtai` on PyPI) lives in the sibling
`lingtai-kernel`. Only the TUI launches Python agents (as subprocesses); both
binaries observe them via the filesystem, and neither has a runtime Python
dependency.

> **What is an `ANATOMY.md`?** This root file defines the convention (see
> `## Anatomy convention`). The bundled `lingtai-tui-anatomy` skill
> (`tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`) is a legacy
> citation-navigation aid for the existing per-package anatomies; it predates
> this root/paired convention and awaits a separate alignment, so where the two
> disagree this file governs.

## Components

The repo root holds two binary trees plus shared infrastructure. Each binary is a self-contained Go module; they communicate with running agents purely through the agent's working directory (`.lingtai/<agent>/`).

- **`ANATOMY.md` / `CONTRACT.md`** — the two normative distributed-system roots. This file is the code-navigation map and anatomy-of-anatomy; `CONTRACT.md` is the code-interface/Behavior definition root and contract-of-contract. They list each other in `related_files`.
- **`dev-guide-skill/`** — the repository-local agent dev kit. Its `SKILL.md` routes agents into the Anatomy and Contract systems and the change/validation workflow, and may grow focused scripts, references, templates, or assets as real workflows recur. Distinct from the bundled `lingtai-dev-guide` skill under `tui/internal/preset/skills/`, which ships to agents and owns deeper per-topic procedures.
- **`tui/architecture_documents_test.go`** — a small real-repository smoke test in the existing TUI module (`cd tui && go test ./...`). It checks only the root Anatomy/Contract/dev-guide routing and the links from the three READMEs and `CLAUDE.md`; schema, prose, hypothetical child graphs, and defensive YAML/path edge cases stay in review rather than a bespoke test framework. The root documents belong to neither binary, so the smoke test lives in the TUI module rather than a third module.
- **`tui/`** — Terminal UI binary (`lingtai-tui`). Bubble Tea v2 + lipgloss v2. Single-binary launcher, agent monitor, first-run wizard, mail viewer, preset editor. Builds to `tui/bin/lingtai-tui`. The flat `tui/main.go` wires subcommands (`purge`, `list`, `clean`, `suspend`, `bootstrap`, `presets`, `spawn`, `self-update`, `doctor`) and the interactive entry; everything substantive is under `tui/internal/`. See the per-package summary below.
- **`portal/`** — Web portal binary (`lingtai-portal`). Go HTTP server with an embedded React frontend served from a single binary via `embed.FS`. Reads the same `.lingtai/` filesystem the TUI does, surfaces a network visualisation, mail/replay UI, and topology recorder. Builds to `portal/bin/lingtai-portal`. Per-package layout under `portal/internal/`.
- **`install.sh`** — One-shot installer (`curl -fsSL https://lingtai.ai/install.sh | bash`), Homebrew-free. `--source auto|github|gitee` (default `auto`, or `LINGTAI_SOURCE`) selects the release provider: `auto` runs a bounded, fail-open public-IP country lookup (`detect_country_cn`) and prefers Gitee (`huangzesen1997/lingtai` + `huangzesen1997/lingtai-kernel`) for mainland China, falling back to GitHub on any detection/reachability failure — always for the SAME resolved tag/bundle, never by re-querying "latest" a second time (`resolve_source_provider`, `fetch_bundle_manifest`, `fetch_kernel_manifest`). When a legacy or explicitly published release exposes `lingtai-bundle-manifest.json` (schema `lingtai.tui.bundle/v1`), it binds one exact TUI tag/commit to one exact pinned kernel tag/version/artifacts/checksums; the current source-only tag workflow does not create or mirror bundles — see `RELEASING.md`. Downloads a prebuilt per-platform tarball (`lingtai-<tag>-<os>-<arch>.tar.gz`) when the release exposes one, **verifying its `.sha256` sidecar before extraction**, otherwise falls back to building the release source tarball with Go/npm. Installs into `--bin-dir`/`--prefix`, else a writable `/usr/local/bin`, else `~/.local/bin` (never prefers Homebrew). Then one-shot-creates/updates the Python runtime venv at `~/.lingtai-tui/runtime/venv`. LingTai is installed ONLY from the pinned release bundle, never from a package index by name: on the default release-asset path a resolved bundle manifest is mandatory, and `install_kernel_from_bundle` selects a compatible platform wheel for the venv's actual interpreter (`select_kernel_wheel`, via `packaging.tags.sys_tags()`) or the pinned sdist fallback, verifies its SHA256, and installs it by **explicit local file path** (only third-party dependencies resolve via the configured index). If no bundle manifest can be resolved (either provider, same-tag fallback attempted), or the resolved bundle's kernel artifact fails to verify/install, `ensure_runtime_venv` **fails loud** — the overall install exits nonzero rather than silently reaching for PyPI. `--ref`/source-ref builds have no bundle to pin against and fail loud the same way. `--skip-python` (alias `--skip-venv`) is the explicit opt-out for a TUI/portal-only install. Verifies `import lingtai`, stamps the env marker, symlinks `lingtai-agent`. Stamps exact `vX.Y.Z` release installs as that tag and writes `install.json` (`install_method: "source"`, additive `install_kind: release-asset|source-build`, and — only on a verified bundle install — additive `kernel_source: "bundle"` + `kernel_bundle_id`/`kernel_version`/`kernel_provider`) so both the TUI source updater and `tui/internal/config/venv.go`'s bundle-provenance gate can read it. On WSL/Debian/Ubuntu it can `apt-get install` missing Go/Python/git when interactive with sudo; non-interactive mode prints the exact command instead. Independently of source policy, still auto-detects CN-restricted Go-proxy reachability for source builds and falls back to mirrors for Go modules / `npm` / Go checksum DB. Helper functions are unit-tested via `scripts/test-install-sh.sh` and `scripts/test-install-sh-gitee-bundle.sh`.
- **`kernel-release.json`** — Repo-owned compatibility pin used by explicit/manual bundle tooling. The current source-only tag workflow does not read it or publish a TUI/kernel bundle; bump it only when separately authorized bundle work needs a new kernel release.
- **`scripts/`** — Auxiliary Python utilities (image-to-blocks, tool description dumper, file-rename helper) plus release/installer test and publish infrastructure: `test-install-sh.sh` / `test-install-sh-gitee-bundle.sh` / `test-publish-bundle-to-gitee.sh` / `test-sync-gitee-mirror.sh` (source `install.sh` with `LINGTAI_INSTALL_SH_SOURCE_ONLY=1`, or exercise the standalone scripts, against a fake-curl harness or real local-git-remote fixtures), `test-release-workflow-publish-gating.py` (static assertions that `release.yml` remains source-only), `sync_gitee_mirror.sh` (non-force git push of the exact release commit/tag to the Gitee mirror — fast-forward-only, create-only tag, never `--force`), and `publish_bundle_to_gitee.sh` (the Gitee release asset publisher, `--execute`-gated). The Gitee sync/publish scripts remain explicit maintainer tools and are not invoked by the source-only tag workflow; see `RELEASING.md`. NOT the runtime — these are dev/release tools, not shipped in any TUI/portal binary.
- **`examples/`** — Reference config files (`init.jsonc`, `bash_policy.json`, `imap.jsonc`, `telegram.jsonc`) for users wiring up their own agents.
- **`docs/`** — Repo-native developer and reference docs (specs, plans, daily change log, screenshots, known limitations, graphify). The human-facing beginner guide now lives on the website tutorial (`https://lingtai.ai/{en,zh,wen}/tutorial/`), not in this repo; see `docs/ANATOMY.md`.
- **`prompt/`** — Localised prompt fragments shared across the TUI/portal.
- **`assets/`** — Static images (logos, screenshots) used by README and docs.
- **`README.md` / `README.zh.md` / `README.wen.md`** — Tri-lingual project README: concise orientation (what LingTai is, install/start, interfaces, architecture, contributing). Each links to its locale's website tutorial (`https://lingtai.ai/{en,zh,wen}/tutorial/`) for step-by-step beginner learning rather than duplicating it.
- **`RELEASING.md`** — Release process: tag, GitHub release, automated Homebrew tap update, and manual tap fallback.
- **`.github/workflows/release.yml`** — Source-only tag-push workflow (`v*` push only). `source-release` verifies the tag and creates the public GitHub Release without building or uploading binaries, checksums, bundle manifests, or Gitee assets. `update-homebrew` computes the GitHub tag source-tarball checksum, rewrites `lingtai-tui.rb`, and pushes the source-build formula update to `Lingtai-AI/homebrew-lingtai`. See `RELEASING.md`.
- **`CLAUDE.md`** — Repo-specific Claude Code instructions (build commands, gotchas, sibling repos).

### `tui/` packages

| Package | LOC | Role |
|---------|-----|------|
| `tui/internal/tui/` | ~22k | Bubble Tea models for every screen — first-run wizard, network home (`app.go`), agent detail, mail composer, preset editor, knowledge/skills, doctor, addon installer. The biggest module by far; the `tui/` package is itself decomposable but the boundaries match Bubble Tea's screen-per-file convention. |
| `tui/internal/preset/` | — | Atomic `{llm, capabilities}` bundle layer. `preset.go` (~1900 lines) handles load/save/list, `recipe_apply.go` handles recipe import, `state.go` tracks user preset state. Embeds the canonical preset templates, covenant text, principles, soul fragments, procedures, skills, and recipe assets via `//go:embed`. |
| `tui/internal/migrate/` | — | Retained m001–m039 historical source/tests and registry API; production startup, project creation, launcher, and diagnostics do not execute it or advance `.lingtai/meta.json`. See `tui/internal/migrate/ANATOMY.md`. |
| `tui/internal/globalmigrate/` | — | Per-machine analogue under `~/.lingtai-tui/`. Same conventions, separate version space (`~/.lingtai-tui/meta.json`). For things like Homebrew tap renames and runtime venv relocations. Currently at v2; v2 (`split-presets-dir`) is a neutralized no-op tombstone — it once moved/deleted flat `presets/*.json` files and caused the preset-loss incident, so its destructive body was removed while the version entry is retained for advancement semantics. |
| `tui/internal/fs/` | — | Filesystem accessors: agent manifest, heartbeat, mail (read/list/write outbox), token ledger, location, network discovery, signal files, session JSONL load. The TUI's read-only window into a running agent's working directory. |
| `tui/internal/sqlitelog/` | — | Small sqlite3 CLI-backed readers for kernel `logs/log.sqlite`; currently used by `/notification` to page notification events just-in-time instead of relying on stale `.notification/` snapshots. |
| `tui/internal/config/` | — | Global TUI config under `~/.lingtai-tui/`: `tui_config.json`, runtime venv resolution, addon registry. |
| `tui/internal/process/` | — | Subprocess launcher (`launcher.go`). Spawns `python -m lingtai run <dir>` with the right venv, log redirection, and PID tracking. |
| `tui/internal/inventory/` | — | Typed running-agent inventory shared by `lingtai-tui list` and `/projects`: processscan rows plus `.agent.json`/heartbeat/status/admin/IM enrichment, duplicate collapse, deterministic grouping, and admin-only enterability. |
| `tui/internal/headless/` | — | JSON-emitting non-interactive CLI surface. Backs the `bootstrap`, `presets`, and `spawn` subcommands wired from `tui/main.go` (`bootstrapMain`, `presetsMain`, `spawnMain`). The adjacent `doctorMain` and `selfUpdateMain` subcommands use `config` update routines directly because they repair the local install rather than emitting headless JSON. Exposes `RunPresets`, `RunSpawn`, `ExitError` for structured agent-consumable output. |
| `tui/i18n/` | — | en/zh/wen JSON tables. **Three locales always** — adding a key requires updating all three. Missing keys render as the raw key string. |
| `tui/scripts/` | — | Build helper scripts (cross-compile, asset bundling). |
| `tui/packages/` | — | Vendored or generated dependency artefacts. |
| Per-OS `*_unix.go` / `*_windows.go` | — | Platform-specific shims for `agent_count`, `exec`, `list`, `purge`, `suspend` subcommands. |

### `portal/` packages

| Package | LOC | Role |
|---------|-----|------|
| `portal/internal/api/` | ~1.5k | HTTP server (`server.go`), handlers (`handlers.go`), and replay endpoint (`replay.go` — 680 lines, the largest single API surface). Listens on a randomly-chosen port (or `--port`), writes the bound port to `.portal/port` so the TUI can find it. |
| `portal/internal/fs/` | ~2.2k | Same shape as `tui/internal/fs/` but tailored to portal's needs: agent reading, heartbeat, mail, network/topology reconstruction (`reconstruct.go`, 326 lines), location resolution. |
| `portal/internal/migrate/` | — | Retained m001–m039 historical source/tests and registry API; Portal production startup does not execute it or advance `.lingtai/meta.json`. See `portal/internal/migrate/ANATOMY.md`. |
| `portal/web/` | — | React 19 + TypeScript + Vite frontend. Source under `portal/web/src/` (`App.tsx`, `Graph.tsx`, `BottomBar.tsx`, `FilterPanel.tsx`, etc.). Builds to `portal/web/dist/` then `embed.go` (`//go:embed all:web/dist`) compiles it into the Go binary. |
| `portal/i18n/` | — | en/zh/wen JSON tables. Independent of the TUI's i18n — same three-locale rule. |
| `portal/docs/` | — | Portal-specific docs and screenshots. |

## Connections

- **TUI → kernel.** The TUI launches the kernel as a subprocess: `python -m lingtai run <agent-dir>` via `process/launcher.go`. The kernel is installed into `~/.lingtai-tui/runtime/venv/` (an isolated venv set up on first run via `pip install lingtai`). After spawn, the TUI never talks to the agent process directly — only via the agent's working directory.
- **TUI → filesystem (read).** `internal/fs/` reads `.lingtai/<agent>/.agent.json`, `.agent.heartbeat`, `mailbox/`, `logs/token_ledger.jsonl`, `history/chat_history.jsonl`, `system/*.md`. The kernel writes these; the TUI never writes them.
- **TUI → filesystem (write).** Signal files only: `.lingtai/<agent>/{.sleep, .suspend, .interrupt, .clear, .prompt, .refresh, .inquiry, .forget}`. The kernel polls these on each heartbeat tick. `init.json` is also writeable but only via explicit user actions in the wizard / preset editor.
- **TUI → human pseudo-mailbox.** The TUI is the user's MUA: it writes outbound messages into `.lingtai/human/mailbox/outbox/<uuid>/message.json`; agents poll this folder and claim deliveries.
- **Portal → filesystem.** Same read pattern as the TUI; additionally writes `.lingtai/.portal/port`, recordings under `.lingtai/.portal/recordings/`, and topology snapshots that feed the replay timeline.
- **Portal ↔ TUI integration.** `lingtai-tui` discovers an installed `lingtai-portal` to launch on `/viz`; otherwise the binaries are independent.
- **TUI ↔ Homebrew tap.** Pushing a release tag runs `.github/workflows/release.yml`, which updates `Lingtai-AI/homebrew-lingtai/lingtai-tui.rb`. Users running `brew upgrade lingtai-ai/lingtai/lingtai-tui` pull from there. Manual tap edits are fallback/debug steps only. See `RELEASING.md`.
- **Portal embeds web frontend.** `embed.go` at the portal root compiles `portal/web/dist/` into the Go binary so `lingtai-portal` ships with no runtime dependency on Node.

### Cross-repo dependencies

This repo depends on `lingtai-kernel` only at runtime (the Python agent it launches), not at build time. Other sibling repos:

- **`lingtai-kernel`** — Python kernel + `lingtai` PyPI package. Owns the canonical agent runtime.
- **`lingtai-skill`** — Single-source-of-truth for the mailbox-protocol `SKILL.md`. Vendored into plugin repos via `lingtai-claude-code/scripts/sync-from-canonical.sh`.
- **`lingtai-claude-code`** — Claude Code plugin (SessionStart hook, marketplace manifest).
- **`codex-plugin`** — OpenAI Codex CLI plugin.
- **`lingtai-imap` / `lingtai-telegram` / `lingtai-feishu` / `lingtai-wechat`** — MCP server addons. Each ships as a separate PyPI package.
- **`Lingtai-AI/homebrew-lingtai`** — Homebrew tap for `lingtai-tui`.

## Composition

- **Parent:** none — this is a top-level repo.
- **Subfolders:** `tui/`, `portal/`, `docs/`, `examples/`, `prompt/`, `scripts/`, `assets/`. The TUI and portal each have full per-package internal trees with their own `internal/` boundaries.
- **Build outputs:** `tui/bin/lingtai-tui`, `portal/bin/lingtai-portal`. Cross-compile via `make cross-compile` in either directory (darwin/linux/windows × amd64/arm64).
- **Module names:** `github.com/anthropics/lingtai-tui` and `github.com/anthropics/lingtai-portal`. Note the historical naming — these are NOT moving to a `Lingtai-AI/` import path even though the GitHub org renamed.

## State

- **Per-project state** under `<project>/.lingtai/`:
  - `meta.json` — legacy project migration metadata may remain on disk, but TUI and Portal production do not read, write, or advance it.
  - `<agent>/init.json` — the agent's preset manifest (LLM + capabilities + allowed presets list).
  - `<agent>/.agent.json` / `.agent.heartbeat` / `.status.json` — written by the agent, read by the TUI/portal.
  - `<agent>/mailbox/{inbox,outbox,sent,archive}/<uuid>/message.json` — filesystem mailbox.
  - `<agent>/logs/log.sqlite` — kernel event trace; `/notification` reads notification events from this database just-in-time so the view reflects current log history rather than a sidecar snapshot.
  - `<agent>/.notification/<channel>.json` — `.notification/` filesystem-as-protocol sidecar signals (email, soul, system events). The TUI no longer renders these directly in `/notification`; `/goal` remains the narrow write exception that appends a `goal.request` event to `<agent>/.notification/system.json` so the running agent can guide goal setup.
  - `human/` — the user's pseudo-agent (no admin, no heartbeat). Mailbox layout identical to a real agent.
  - `.tui-asset/` — TUI-owned per-project caches (remotes list, etc.).
  - `.portal/port` / `.portal/recordings/` — portal-owned files when running.
- **Per-machine state** under `~/.lingtai-tui/`:
  - `meta.json` — global migration version stamp.
  - `tui_config.json` — global TUI preferences (default language, model selection, etc.).
  - `runtime/venv/` — Python venv with `lingtai` installed; agents launch from here.
  - `presets/templates/` — TUI-owned, rewritten on every Bootstrap from embedded data. Don't hand-edit.
  - `presets/saved/` — User-owned preset clones; the wizard's auto-clone-on-edit lands new presets here as `<template>-<N>.json`.
  - `utilities/` — Optional skills paths surfaced to agents.

## Notes

- **Human-facing docs ownership:** the step-by-step beginner guide lives on the website tutorial (`https://lingtai.ai/{en,zh,wen}/tutorial/`), maintained outside this repo. In-repo, the human-facing surfaces are the three READMEs (concise orientation) and the bundled help assets (`tui/internal/preset/skills/lingtai-tui-help/assets/`, the canonical slash-command catalog). Any change that adds/removes/renames user-visible capabilities, slash commands, setup/install flows, channel/addon surfaces, memory/molt behavior, daemon/avatar behavior, or safety boundaries must keep the README orientation and help assets accurate and flag the website tutorial for a matching update (tracked in the separate website repo).
- **Binary naming.** The TUI binary is `lingtai-tui`, never `lingtai`. `lingtai` is the Python agent CLI inside the runtime venv (`~/.lingtai-tui/runtime/venv/bin/lingtai`). Build to `tui/bin/lingtai-tui`; never `tui/bin/lingtai`.
- **Bubble Tea v2 paste delivery.** Bubble Tea v2 splits keys (`tea.KeyPressMsg`) from clipboard pastes (`tea.PasteMsg`). Any `Update` dispatcher gating on `case tea.KeyPressMsg:` must also forward `tea.PasteMsg` to whichever text widget is focused — otherwise paste silently drops. For embedded sub-models hosted inside another model (e.g. `PresetEditorModel` inside `FirstRunModel`), the host's outer `default:` branch must forward paste msgs into the sub-model. Trace top-down to find missing layers; the symptom is "typing works, paste does nothing."
- **`textarea` vs `textinput`.** For any paste-friendly field (API keys, base URLs), use `textarea` even when the content is conceptually one line. `textinput` drops characters on multi-byte / clipboard pastes. Always apply `themedTextareaStyles()` from the `tui` package — bare `textarea.New()` ships dark default cursor/focus colors that render as a black smear against the warm theme.
- **Migration retirement.** TUI and Portal retain the shared migration registries as non-executing history/test APIs. Production does not consult or stamp project migration progress; compatibility diagnosis/repair belongs to the kernel reader and explicit Agent edits.
- **Dev-mode rebuild gotcha.** Rebuild both binaries after code changes as usual; runtime project migration bumps are retired, so a stale migration registry is not a startup compatibility gate.
- **Preset architecture.** Presets are atomic `{llm, capabilities}` bundles. `templates/` is TUI-owned (rewritten every Bootstrap from embedded data, prunes retired entries — never hand-edit). `saved/` is user-owned (Bootstrap never touches it). The directory IS the answer to "is this a template?" — there's no in-band marker. Each loaded `Preset` carries a `Source` field (`SourceTemplate` / `SourceSaved`); prefer `IsTemplate(p)` over the legacy `IsBuiltin(p.Name)`. When writing `manifest.preset.*` paths from Go, always use `preset.RefFor(p)` to pick the right subdirectory based on `Source`.
- **Authorization gate.** `manifest.preset.allowed` is the explicit list of preset paths the agent may swap to at runtime. The kernel refuses any swap not in `allowed`. `default` and `active` MUST both appear in `allowed`; `init_schema.validate_init` enforces this. m029 was the migration that introduced this declarative form.
- **Three-locale rule.** Adding an i18n key means updating en.json, zh.json, AND wen.json in BOTH `tui/i18n/` and (where applicable) `portal/i18n/`. Missing translations show as the raw key on screen — they don't fall back. Procedural / dev-only strings can stay English-only with a comment noting why.
- **Filesystem-only IPC.** The TUI and portal never open a socket or RPC channel to a running agent. All communication is via files: agent manifests, heartbeats, signal files, mailbox folders, `.notification/` sidecars, and read-only `logs/log.sqlite` event traces. This is the same boundary the kernel-side documents in `lingtai-kernel/src/lingtai/kernel/ANATOMY.md` "Notifications". Anything you'd want to add here that needs cross-process communication should follow the same pattern: write a file, let the other side poll or read the persisted event log.

## Anatomy convention

This root is the normative anatomy-of-anatomy; the map above is the payload,
these rules are how the navigation graph is shaped. Governance of *behavior*
lives in [`CONTRACT.md`](CONTRACT.md); the change/validation *workflow* lives in
[`dev-guide-skill/SKILL.md`](dev-guide-skill/SKILL.md). This section owns only
the structural schema and link rules, and does not restate either.

**Navigation model.** Navigation is distributed: the root defines the system and
enumerates the two binary trees; each component's anatomy maps only the layer it
owns; parent/child and `related_files` links connect them. Do not copy local
facts into this root. For a structural question, descend the graph (this file →
the relevant tree or component anatomy → cited code); for enumeration (every
callsite, every matching file), use search. A folder earns an anatomy when an
agent can reason about it as an architectural unit without reading all its
siblings; pure helpers and trivial leaves do not. Legacy per-package anatomies
keep their current shape until they migrate; a component enters the paired
governed system only when its co-located `CONTRACT.md` is linked from the root
contract, and from that point the schema and link rules here apply.

The governed-child frontmatter, body, and link/pairing rules below now govern
the first concrete vertical slice: TUI agent-process lifecycle and advisory
process observation. The focused smoke test validates that pair, its root index,
and reciprocal Anatomy links; broader future children remain review-owned until
their own concrete graph earns equally narrow validation.

**Frontmatter.** A root-governed component anatomy has exactly two YAML keys, in
order: `related_files` (a non-empty, duplicate-free list of repo-relative
regular files — the paired `CONTRACT.md`, the parent and direct-child anatomies,
and the code files it maps) and `maintenance` (a non-empty statement; use the
Template's text, or a root-specific one here because this file also governs the
system). Paths MUST be repo-relative, resolve to files, use `/`, and contain no
`.`/`..` segments.

**Body.** A root-governed component anatomy opens with one paragraph naming the
layer, then uses these five `##` sections once, in order: `## Components` (files,
symbols, or child components with verified `file:line` citations),
`## Connections`, `## Composition`, `## State`, `## Notes`. It SHOULD stay near
80 lines — a larger map suggests smaller components — with no empty stubs. This
root file is the sole exception to that body/size shape: it also carries this
meta-convention and the repository-wide map above.

**Link and pairing.**

1. This root anatomy and root contract list each other in `related_files`.
2. A root-governed component's co-located `ANATOMY.md` and `CONTRACT.md` list
   each other exactly once.
3. Parent/child anatomy links are reciprocal so navigation can descend and
   return. Cross-binary references are narrative, not enumerated call-graph
   edges.
4. The contract owns interface behavior; the anatomy owns structure. Cross-link
   instead of copying a rule into both.
5. Orphans, missing targets, duplicate links, one-way pair links, and unpaired
   governed components are defects and MUST fail validation.

## Maintenance

Maintenance is part of reading:

- If code and anatomy disagree structurally, code is normally the current fact;
  repair the anatomy before leaving the change. If the code move itself is a
  defect, report or fix the code and keep the mismatch visible until resolved.
- If code and contract disagree behaviorally, do **not** rewrite the contract to
  match accidental behavior. Treat the implementation as defective unless an
  authorized contract change updates the Port, adapters, version, and tests.
- Verify every touched citation after moves, renames, splits, or ownership
  changes. The anatomy drift checker catches missing/out-of-range citation
  targets, not semantic misdescription.
- Keep parent/child and Anatomy/Contract pair links reciprocal, and keep the
  two-binary facts compatible across `tui/ANATOMY.md` and `portal/ANATOMY.md`.
  When this system's convention itself changes, update this root, its smoke test
  (`tui/architecture_documents_test.go`), the repository-local dev guide, and the
  README entries together. The bundled `lingtai-tui-anatomy` skill is a legacy
  citation-navigation aid that predates this convention; aligning it is separate,
  owner-gated work, not part of every change here.

## Template

```markdown
---
related_files:
  - <repo-relative paired CONTRACT.md>
  - <repo-relative parent ANATOMY.md>
  - <repo-relative direct-child ANATOMY.md, when any>
  - <repo-relative mapped code file>
maintenance: |
  Keep related_files repo-relative, duplicate-free, and linked to real files.
  Keep this component's ANATOMY.md and CONTRACT.md reciprocal and keep
  parent/child anatomy links bidirectional. Code is the structural source of
  truth: update this anatomy in the same change that moves files, symbols,
  connections, composition, or state. Verify every changed citation and run the
  architecture-document validation before merge.
---
# <Component Name> Anatomy

<One paragraph defining the architectural layer this folder embodies.>

## Components

- `<symbol>` — purpose (`repo/relative/file.go:line-line`).

## Connections

## Composition

## State

## Notes
```
