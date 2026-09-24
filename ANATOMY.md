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
  - .github/workflows/windows-installer-smoke.yml
  - .github/workflows/delete-merged-branch.yml
  - .github/rulesets/main.json
  - .github/rulesets/release-tags.json
  - install.sh
  - install.ps1
  - scripts/publish_bundle_to_gitee.sh
  - scripts/sync_gitee_mirror.sh
  - scripts/test-install-ps1.ps1
  - scripts/test-install-sh-hardening.sh
  - scripts/test-install-sh-architecture.sh
  - scripts/test-install-sh-desktop.sh
  - tui/main.go
  - tui/go.mod
  - tui/Makefile
  - tui/internal/preset/skills/lingtai-tui-help/SKILL.md
  - tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md
  - portal/main.go
  - portal/embed.go
  - portal/go.mod
  - portal/Makefile
  - .github/workflows/sync-hf.yml
  - .gitignore
  - LICENSE
  - NOTICE
  - assets/braille/22610_source.png
  - assets/braille/22610_source.svg
  - assets/braille/22610_source_alt.svg
  - assets/braille/22610_w29_tui.txt
  - assets/braille/22610_w44.txt
  - assets/braille/22610_w66.txt
  - discussions/cascade-skill-and-sentinel-ordering-patch.md
  - discussions/codex-credential-redesign-patch.md
  - discussions/covenant-distillation-and-per-agent-profile.md
  - discussions/firstrun-step2-builtin-default-patch.md
  - discussions/intrinsics-strict-schema-scan.md
  - discussions/lingtai-preset-swap-silent-revert-patch.md
  - discussions/lingtai-vision-capability-fallback-patch.md
  - discussions/preset-editor-codex-oauth-patch.md
  - examples/bash_policy.json
  - examples/imap.jsonc
  - examples/init.jsonc
  - examples/telegram.jsonc
  - migration/migration.md
  - prompt/archive/base_prompt.md
  - prompt/archive/base_prompt_wen.md
  - prompt/archive/base_prompt_zh.md
  - prompt/archive/covenant_base.md
  - prompt/archive/covenant_base_lzh.md
  - prompt/archive/covenant_base_zh.md
  - prompt/archive/molt_prompt_default.md
  - reports/ANATOMY.md
  - scripts/dump_tool_descriptions.py
  - scripts/img2blocks.py
  - scripts/img2braille.py
  - scripts/rename.py
  - scripts/star_tracker.py
  - scripts/test-install-sh-mirror-bundle.sh
  - scripts/test-install-sh.sh
  - scripts/test-publish-bundle-to-gitee.sh
  - scripts/test-release-workflow-publish-gating.py
  - scripts/test-sync-gitee-mirror.sh
  - scripts/test_dump_tool_descriptions.py
  - scripts/test_star_tracker.py
maintenance: |
  This file is both the repository-root anatomy and the normative
  anatomy-of-anatomy for the distributed code navigation system across the two
  binaries (lingtai-tui, lingtai-portal) and the install pipeline. Keep
  related_files repo-relative, duplicate-free, and linked to real files. Keep
  the root CONTRACT.md reciprocal and update the paired conventions together
  when their boundary changes. Code is the structural source of truth: repair
  stale navigation in the same change that moves files, symbols, connections,
  composition, or state. Preserve the child template and its maintenance rule;
  validate the distributed graph before merge. Capability mentions in any
  document require explicit navigation mapping to the implementing code: a
  related_files entry in the owning ANATOMY.md (or a markdown link to that node
  when the document lives in the anatomy graph itself), bidirectional between
  document and owner. A capability with no mapping is drift; fix the mapping in
  the same change. See dev-guide-skill/SKILL.md for the workflow.
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
> (`tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`) is the
> discoverable navigation aid into this distributed graph; this root remains
> normative, and the skill routes readers here rather than duplicating the
> convention.

## Components

The repo root holds two binary trees plus shared infrastructure. Each binary is a self-contained Go module; they communicate with running agents purely through the agent's working directory (`.lingtai/<agent>/`).

- **`ANATOMY.md` / `CONTRACT.md`** — the two normative distributed-system roots. This file is the code-navigation map and anatomy-of-anatomy; `CONTRACT.md` is the code-interface/Behavior definition root and contract-of-contract. They list each other in `related_files`.
- **`dev-guide-skill/`** — the repository-local agent dev kit. Its `SKILL.md` routes agents into the Anatomy and Contract systems and the change/validation workflow, and may grow focused scripts, references, templates, or assets as real workflows recur. Distinct from the bundled `lingtai-dev-guide` skill under `tui/internal/preset/skills/`, which ships to agents and owns deeper per-topic procedures.
- **`tui/architecture_documents_test.go`** — the real-repository architecture check in the existing TUI module (`cd tui && go test ./...`). It covers three things: the root Anatomy/Contract/dev-guide routing plus the links from the three READMEs and `CLAUDE.md`; the runtime/control-surface route anchors; and — `TestArchitectureDocumentsCoverEveryTrackedFile` — the graph-coverage rule, walking `related_files` from this root against `git ls-files` so an orphan tracked file, an unreachable `ANATOMY.md`, an empty/duplicated list, a self-link, or an entry that no longer resolves fails the build. It deliberately reads frontmatter with a narrow line-based reader rather than pulling in a YAML dependency; prose accuracy and semantic misdescription stay in review. The root documents belong to neither binary, so the check lives in the TUI module rather than a third module.
- **`tui/`** — Terminal UI binary (`lingtai-tui`). Bubble Tea v2 + lipgloss v2. Single-binary launcher, agent monitor, first-run wizard, mail viewer, preset editor. Builds to `tui/bin/lingtai-tui`. The flat `tui/main.go` wires subcommands (`purge`, `list`, `clean`, `suspend`, `bootstrap`, `presets`, `spawn`, `self-update`, `doctor`) and the interactive entry; everything substantive is under `tui/internal/`. See the per-package summary below.
- **`portal/`** — Retained, deprecated web portal codebase (`lingtai-portal`). Go HTTP server with an embedded React frontend served from a single binary via `embed.FS`. Reads the same `.lingtai/` filesystem the TUI does, surfaces a network visualisation, mail/replay UI, and topology recorder. It remains separately buildable for repository development, but is not built or installed by the installers, release workflow, or Homebrew. Builds to `portal/bin/lingtai-portal`. Per-package layout under `portal/internal/`.
- **`install.sh`** — One-shot Homebrew-free installer (`curl -fsSL https://lingtai.ai/install.sh | bash`). On the ordinary no-version stable path, `--source auto|github|mirror` (default `auto`, or `LINGTAI_SOURCE`) selects the TUI source transport: the normal route resolves and verifies producer-owned TUI source metadata and archive through `lingtai.ai`, then always builds `lingtai-tui` locally; a TUI source failure falls back only to the latest GitHub TUI source release. The kernel independently resolves and verifies its latest release manifest and declared source archive through `lingtai.ai`, with its own GitHub fallback; there is no shared provider switch, release bundle, or repository-owned kernel pin (`install.sh:5-38,546-818,1397-1850`). The verified kernel source archive is built/installed by local path and only its third-party dependencies use a package index (`install.sh:1760-1850`). Explicit `--version`, `--ref`/`--from-source`, `--update`, and `--latest` preserve their distinct source/ref/latest/update behavior; every stable TUI route builds locally from verified source, and the GitHub fallback uses a source archive or exact peeled-tag source checkout (`install.sh:1926-2065,2196-2343`). `--skip-python`/`--skip-venv` is the runtime opt-out; installation metadata records the independently selected TUI/kernel provenance (`install.sh:94-123,978-1065`). After those prerequisites succeed, it removes only other exact `lingtai-tui` files/symlinks from normalized current PATH directories while preserving the canonical target and fails loudly on removal failure (`install.sh:203-272,2331-2343`). Portal is not built, installed, flagged, or recorded by this installer.
  Registration preserves a complete official Desktop launcher byte-for-byte when its managed current App executable is also a regular executable. An ordinary stable install likewise preserves a regular executable non-symlink target bearing this installer's lazy-bootstrap marker; an eligible stable update atomically refreshes that owned lazy command from the current generator and trust pins. Non-executable files, symlinks, foreign targets, and incomplete official launchers remain loud no-overwrite failures. This covers Desktop-first installs and retained Desktop/lazy state outside the TUI receipt.
  An ordinary re-run that detects an existing TUI receipt also retains its prior update/reinstall behavior and does not re-register the lazy Desktop command.
- **`install.sh --latest`** — Explicit POSIX current-main mode: resolves `refs/heads/main` independently in `Lingtai-AI/lingtai` and `Lingtai-AI/lingtai-kernel` to full SHAs before shallow checkouts, verifies both checkouts against those pins, builds only the TUI, installs the kernel from the checked-out local source (never by package name), and writes/shows both commits (`install.sh:2033-2065,2196-2343`). It is explicit and conflicts with release/ref/update/source/python-skip modes; the no-argument path remains the independently resolved latest stable TUI/kernel release flow.
- **`install.ps1`** — Native-Windows PowerShell 5.1/7 counterpart to `install.sh`. The public default resolves and verifies the latest producer-owned TUI source through `lingtai.ai`, always builds `lingtai-tui.exe` locally, and falls back only that TUI source route to the latest GitHub source release. It independently resolves and verifies the latest kernel manifest and declared source archive through `lingtai.ai`, with its own GitHub fallback, then builds/installs that source archive by explicit local path into `%USERPROFILE%\.lingtai-tui\runtime\venv` unless `-SkipVenv` (`install.ps1:1-61,664-932,1404-1455`). `-ArchivePath`+`-ChecksumPath` is an explicit local TUI-artifact mode; `-Version`, `-Ref`/`-FromSource`, `-Update`, and `-Latest` retain their explicit source/ref/update/current-main distinctions (`install.ps1:1915-2069,2074-2345`). There is no coupled bundle input or repository-owned kernel pin, and Portal is not built, installed, flagged, or recorded by this installer. `-DryRun` is a mode-specific, read-only plan: source and current-main paths check prerequisites and report the relevant TUI selection, without downloading or verifying source archives or resolving/installing a kernel release or artifact; local-artifact mode validates its supplied checksum. No writes occur. Real branches remove only other exact `lingtai-tui.exe` files from normalized current PATH directories after successful state publication (`install.ps1:162-216,390-498,2127-2344`).
- **`install.ps1 -Latest`** — Explicit native-Windows current-main development mode: amd64-only, with WSL2 guidance for ARM64; inventories and, when authorized, repairs supported prerequisites, resolves full `refs/heads/main` SHAs for both repositories before shallow checkout, verifies both exact checkouts, builds `lingtai-tui.exe`, installs the checked-out kernel as a non-editable local build, and records `source_mode: latest-main` plus full `tui_commit`/`kernel_commit` provenance (`install.ps1:1531-1851,1799-1993,2149-2182`). Portal is not built in this mode. `-Latest -DryRun` reports the exact prerequisite repair plan, invokes no winget, and makes no destination, PATH, or config writes.
- **Lifecycle source ownership.** `install.sh` and `install.ps1` are the only agent/user-facing install, update, and repair entrypoints. A receipt-less runtime is not migrated or adopted; the platform root installer ignores an old caller's pinned release and reruns the normal latest independently verified TUI and kernel source-release path. There is no standalone lifecycle-script or legacy-migration surface. The internal version-pinned `install.sh --update` contract remains available only to the TUI self-update backend.
- **`scripts/`** — Auxiliary developer/release utilities and installer test infrastructure. Unrelated build, test, release, and mirror automation remains.
- **`examples/`** — Reference config files (`init.jsonc`, `bash_policy.json`, `imap.jsonc`, `telegram.jsonc`) for users wiring up their own agents.
- **`docs/`** — Repo-native developer and reference docs (specs, plans, daily change log, screenshots, known limitations, graphify). The human-facing beginner guide now lives on the website tutorial (`https://lingtai.ai/{en,zh,wen}/tutorial/`), not in this repo; see `docs/ANATOMY.md`.
- **`reports/`** — Local-only by default. The tracked files here are the deliberate exceptions: one evidence bundle per shipped release plus a few promoted explainers. See `reports/ANATOMY.md` for the tracked/untracked boundary and `CLAUDE.md` for the working rule.
- **`prompt/`** — Localised prompt fragments. Everything tracked here now lives under `prompt/archive/` — the superseded base-prompt, covenant, and molt-prompt originals, kept for provenance after the live copies moved into the kernel and `tui/internal/preset/`. Nothing in either binary reads this directory.
- **`assets/`** — Static images (logos, screenshots) used by README and docs, plus `assets/braille/` — the source art and the pre-rendered 29/44/66-column braille variants of the 𢘐 (U+22610) glyph behind the first-run splash (`tui/internal/tui/firstrun.go:3436`).
- **`discussions/`** — Patch proposals and design discussions written by LingTai agents. This is where the maintenance banner's "report drift as issues, do not silently fix" rule lands: `discussions/<name>-patch.md`.
- **`migration/`** — `migration/migration.md`, a release-scoped migration note. Its versioned TUI/Portal and kernel-tag sections are preserved history, not current installer or release inputs.
- **`README.md` / `README.zh.md` / `README.wen.md`** — Tri-lingual project README: concise orientation (what LingTai is, install/start, interfaces, architecture, contributing). Each links to its locale's website tutorial (`https://lingtai.ai/{en,zh,wen}/tutorial/`) for step-by-step beginner learning rather than duplicating it.
- **`RELEASING.md`** — Release process: tag, peeled-commit TUI source archive/checksum publication, source-only `lingtai-web` relay, TUI-only Homebrew update, manual tap fallback, and the independent PowerShell TUI/kernel install path.
- **`.github/workflows/release.yml`** — Tag-push workflow (`v*` push only) with two jobs. `source-release` validates the exact `vX.Y.Z` tag, peels it to a commit SHA, creates the deterministic `lingtai-<tag>-source.tar.gz` and `.sha256` assets, publishes them, and sends source-only generic asset metadata to `Lingtai-AI/lingtai-web`. `update-homebrew` consumes the producer checksum, fails closed for non-exact tags before tap-token use, and writes a TUI-only source-build formula. The workflow does not build or publish Portal, Windows bundles, or a TUI-coupled kernel pin; `lingtai-web` relays verified producer assets and does not build or reinterpret source. See `RELEASING.md`.
- **`.github/rulesets/`** — Intended GitHub branch/tag ruleset state as reviewable JSON (`main.json` targets `~DEFAULT_BRANCH`; `release-tags.json` makes `refs/tags/v*` immutable so the ref that triggers `release.yml` cannot be moved or deleted after publication). Rulesets are repository *settings*, so nothing in this tree applies them — an admin POSTs these files. `docs/repository-rulesets.md` owns the evidence for why they exist, the apply commands, and the read-back checks that prove a ruleset actually targets a ref. Not read by any binary.

- **`.github/workflows/windows-installer-smoke.yml`** — Runs `scripts/test-install-ps1.ps1` under both Windows PowerShell 5.1 and PowerShell 7 on `windows-latest` (PR/push, no live-release dependency). Its local-artifact dry-run and source assertions are deterministic and do not perform a real install or create a current Windows release-asset contract; historical release assets are not installer inputs.
- **`.github/workflows/delete-merged-branch.yml`** — Deletes a pull request's head branch when that PR merges, so merged refs stop accumulating on `origin`. Runs on `pull_request_target: closed` (base-branch definition, writable token, no checkout and no PR-authored code) and acts only when the PR actually merged and its head lives in this repository — fork heads and closed-without-merge branches are left alone, as are the default/`develop`/`gh-pages`/`release/*` branches and any branch that is still the head of another open PR. An already-deleted ref is a success, not a failure, so it composes with the repository's own "automatically delete head branches" setting rather than fighting it.
- **`CLAUDE.md`** — Repo-specific Claude Code instructions (build commands, gotchas, sibling repos).

### `tui/` packages

| Package | LOC | Role |
|---------|-----|------|
| `tui/internal/tui/` | ~22k | Bubble Tea models for every screen — first-run wizard, network home (`app.go`), agent detail, mail composer, preset editor, knowledge/skills, doctor, addon installer. The biggest module by far; the `tui/` package is itself decomposable but the boundaries match Bubble Tea's screen-per-file convention. |
| `tui/internal/preset/` | — | Atomic `{llm, capabilities}` bundle layer. `preset.go` (~1900 lines) handles load/save/list, `recipe_apply.go` handles recipe import, `state.go` tracks user preset state. Embeds the canonical preset templates, covenant text, principles, procedures, skills, and recipe assets via `//go:embed`. |
| `tui/internal/migrate/` | — | Retained m001–m039 historical source/tests and registry API; production startup, project creation, launcher, and diagnostics do not execute it or advance `.lingtai/meta.json`. See `tui/internal/migrate/ANATOMY.md`. |
| `tui/internal/globalmigrate/` | — | Per-machine analogue under `~/.lingtai-tui/`. Same conventions, separate version space (`~/.lingtai-tui/meta.json`). For things like Homebrew tap renames and runtime venv relocations. Currently at v2; v2 (`split-presets-dir`) is a neutralized no-op tombstone — it once moved/deleted flat `presets/*.json` files and caused the preset-loss incident, so its destructive body was removed while the version entry is retained for advancement semantics. See `tui/internal/globalmigrate/ANATOMY.md`. |
| `tui/internal/fs/` | — | Filesystem accessors: agent manifest, heartbeat, mail (read/list/write outbox), token ledger, location, network discovery, signal files, session JSONL load. The TUI's read-only window into a running agent's working directory. |
| `tui/internal/sqlitelog/` | — | Small sqlite3 CLI-backed readers for kernel `logs/log.sqlite`; currently used by `/notification` to page notification events just-in-time instead of relying on stale `.notification/` snapshots. See `tui/internal/sqlitelog/ANATOMY.md`. |
| `tui/internal/config/` | — | Global TUI config under `~/.lingtai-tui/`: `tui_config.json`, runtime venv resolution, addon registry. |
| `tui/internal/process/` | — | Subprocess launcher (`launcher.go`). Spawns `python -m lingtai run <dir>` with the right venv, log redirection, and PID tracking; also the terminate path. See `tui/internal/process/ANATOMY.md`. |
| `tui/internal/inventory/` | — | Typed running-agent inventory shared by `lingtai-tui list` and `/projects`: processscan rows plus `.agent.json`/heartbeat/status/admin/IM enrichment, duplicate collapse, deterministic grouping, and admin-only enterability. |
| `tui/internal/headless/` | — | JSON-emitting non-interactive CLI surface. Backs the `bootstrap`, `presets`, and `spawn` subcommands wired from `tui/main.go` (`bootstrapMain`, `presetsMain`, `spawnMain`). The adjacent `doctorMain` and `selfUpdateMain` subcommands use `config` update routines directly because they repair the local install rather than emitting headless JSON. Exposes `RunPresets`, `RunSpawn`, `ExitError` for structured agent-consumable output. See `tui/internal/headless/ANATOMY.md`. |
| `tui/i18n/` | — | en/zh/wen JSON tables. **Three locales always** — adding a key requires updating all three; a key missing everywhere renders as the raw key string. See `tui/i18n/ANATOMY.md`. |
| `tui/internal/doctorreport/` | — | Writer-only serializer for a finished `/doctor` run: redacts the captured draft and emits the private `report.md`/`metadata.json`/`redaction.json` bundle. Runs no diagnostics of its own. See `tui/internal/doctorreport/ANATOMY.md`. |
| `tui/scripts/` | — | Build helper scripts (cross-compile, asset bundling). |
| `tui/packages/` | — | Vendored or generated dependency artefacts. |
| Per-OS `*_unix.go` / `*_windows.go` | — | Platform-specific shims for `agent_count`, `exec`, `list`, `purge`, `suspend` subcommands. |

### `portal/` packages

| Package | LOC | Role |
|---------|-----|------|
| `portal/internal/api/` | ~1.5k | HTTP server (`server.go`), handlers (`handlers.go`), and replay endpoint (`replay.go` — 784 lines, the largest single API surface). Listens on a randomly-chosen port (or `--port`), writes the bound port to `.portal/port` so the TUI can find it. |
| `portal/internal/fs/` | ~2.2k | Same shape as `tui/internal/fs/` but tailored to portal's needs: agent reading, heartbeat, mail, network/topology reconstruction (`reconstruct.go`, 326 lines), location resolution. |
| `portal/internal/migrate/` | — | Retained m001–m039 historical source/tests and registry API; Portal production startup does not execute it or advance `.lingtai/meta.json`. See `portal/internal/migrate/ANATOMY.md`. |
| `portal/web/` | — | React 19 + TypeScript + Vite frontend. Source under `portal/web/src/` (`App.tsx`, `Graph.tsx`, `BottomBar.tsx`, `FilterPanel.tsx`, etc.). Builds to `portal/web/dist/` then `embed.go` (`//go:embed all:web/dist`) compiles it into the Go binary. |
| `portal/i18n/` | — | en/zh/wen JSON tables. Independent of the TUI's i18n — same three-locale rule. |
| `portal/docs/` | — | Portal-specific docs and screenshots. |

## Connections

- **TUI → kernel.** The TUI launches the kernel as a subprocess: `python -m lingtai run <agent-dir>` via `process/launcher.go`. Installers place a separately resolved and verified kernel source archive or checked-out kernel source into `~/.lingtai-tui/runtime/venv/`; the TUI then observes agents through the filesystem. After spawn, the TUI never talks to the agent process directly — only via the agent's working directory.
- **TUI → filesystem (read).** `internal/fs/` reads `.lingtai/<agent>/.agent.json`, `.agent.heartbeat`, `mailbox/`, `logs/token_ledger.jsonl`, `history/chat_history.jsonl`, `system/*.md`. The kernel writes these; the TUI never writes them.
- **TUI → filesystem (write).** Signal files only: `.lingtai/<agent>/{.sleep, .suspend, .interrupt, .clear, .prompt, .refresh}`. The kernel polls these on each heartbeat tick. `init.json` is also writeable but only via explicit user actions in the wizard / preset editor.
- **TUI → human pseudo-mailbox.** The TUI is the user's MUA: it writes outbound messages into `.lingtai/human/mailbox/outbox/<uuid>/message.json`; agents poll this folder and claim deliveries.
- **Portal → filesystem.** Same read pattern as the TUI; additionally writes `.lingtai/.portal/port`, recordings under `.lingtai/.portal/recordings/`, and topology snapshots that feed the replay timeline.
- **Portal ↔ TUI integration.** `lingtai-tui` can discover a separately installed `lingtai-portal` to launch on `/viz`; otherwise the binaries are independent. The installers, release workflow, and Homebrew do not provide or record Portal.
- **TUI ↔ Homebrew tap.** Pushing an exact release tag runs `.github/workflows/release.yml`, which updates the TUI-only source-build formula at `Lingtai-AI/homebrew-lingtai/lingtai-tui.rb` from the deterministic producer archive/checksum. `brew install`/manual `brew upgrade lingtai-ai/lingtai/lingtai-tui` pull the TUI source from there; Portal and the kernel are not formula inputs. Manual tap edits are fallback/debug steps only. See `RELEASING.md`. LingTai's own update paths (`/update-tui`, `self-update`, `doctor`) no longer run `brew upgrade` for a detected Homebrew install — they migrate it to the native installer instead (`tui/internal/config/tui_updater.go`'s `homebrewTUIUpdater`), leaving the old formula/keg installed but no longer the update target.
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
  - `<agent>/.notification/<channel>.json` — `.notification/` filesystem-as-protocol sidecar signals (email, system events). The TUI no longer renders these directly in `/notification`; `/goal` remains the narrow write exception that appends a `goal.request` event to `<agent>/.notification/system.json` so the running agent can guide goal setup.
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

- **Runtime/control-surface boundary:** TUI and Portal are control/presentation processes; the independently running kernel process owns the agent heartbeat, listeners, and lifecycle. Closing a frontend is not an agent lifecycle operation, and ordinary persistence does not require a second `launchd` supervisor. Use explicit lifecycle commands and inspect current state instead. `tui/ANATOMY.md` carries the same-repo quit/launch/attach/signal/inventory file-and-symbol routes; `portal/ANATOMY.md` carries the Portal shutdown boundary; exact Python runtime semantics remain in the separate `lingtai-kernel-anatomy` graph.
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

**Coverage (no orphans).** Every tracked file in this repository MUST be
reachable from this root anatomy by descending `related_files` — the whole file
tree climbs the anatomy graph, and no tracked file is an orphan. Each file is
owned by the anatomy of the layer it belongs to (a package's own `ANATOMY.md`
when it has one, otherwise its parent's), and an `ANATOMY.md` counts only once
its parent lists it. This is enforced, not aspirational:
`tui/architecture_documents_test.go`'s
`TestArchitectureDocumentsCoverEveryTrackedFile` walks the graph from this file
against `git ls-files` and fails on any orphan, unreachable anatomy, empty or
duplicated list, self-link, or entry that no longer resolves to a tracked file.
Adding a file to the repo therefore means adding it to exactly the
`related_files` list that owns it, in the same change. `related_files` is the
complete inventory of a layer; the anatomy *body* stays the curated architectural
map of that layer and is deliberately not a per-file listing.

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

The governed-child frontmatter, body, and link/pairing rules below are the
**normative target** for that first governed child, not machinery the smoke test
runs today. The repository has zero governed children, so there is no mechanical
child gate. A first governed-child PR must justify and add only the focused
validation its concrete graph needs; until then these rules remain review-owned.

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
- Adding, moving, or deleting a tracked file is an anatomy change. Update the
  owning `related_files` list in the same commit, or
  `TestArchitectureDocumentsCoverEveryTrackedFile` fails with the exact paths to
  add. Deleting a file means deleting its entry; a new package directory means a
  new `ANATOMY.md` linked from its parent.
- **Capability mentions require explicit bidirectional mapping to implementing
  code.** Any document (README, docs, skill, issue/PR body, proposal) that names
  a user-visible or agent-visible capability must resolve to the code that
  implements it: either the owning ANATOMY.md lists that document in its
  `related_files` and the implementing files, or the document links to the
  owning anatomy node. A capability with no mapping is navigation drift and
  must be repaired in the same change, not deferred.
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
