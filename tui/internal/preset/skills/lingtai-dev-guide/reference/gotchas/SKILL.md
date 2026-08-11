---
name: dev-guide-gotchas
description: >
  Nested lingtai-dev-guide reference for known implementation footguns: Bubble Tea v2 paste, textarea theming, dev-mode rebuilds, editable installs, migrations, localization, authorization gates, config conventions, and rebuild-gate test false-passes.
version: 1.2.1
last_changed_at: "2026-08-10T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Gotchas and Known Pitfalls

Nested lingtai-dev-guide reference. Read this after the top-level router sends you here.
Collective wisdom from production incidents — each entry has caused at least one regression.

## Bubble Tea v2: paste delivery

Bubble Tea v2 splits keys (`tea.KeyPressMsg`) from clipboard pastes (`tea.PasteMsg`). **Any `Update` dispatcher handling `case tea.KeyPressMsg:` must also forward `tea.PasteMsg` to whichever text widget is focused**, or paste silently drops.

For embedded sub-models (e.g. `PresetEditorModel` inside `FirstRunModel`), the host's outer `default:` branch must forward paste msgs into the sub-model. If the host's dispatcher enumerates focused widgets per-step but misses the step hosting an embedded model, paste dies before reaching it — and a fall-through inside the sub-model's own `Update` is useless because the msg never arrives.

**Symptom:** typing into an input works, pasting does nothing.

**Fix:** trace top-down (`tea.Program` → host's outer switch → sub-model's outer switch → widget) and ensure every layer handles or forwards `tea.PasteMsg`.

## `textarea` vs `textinput`

`textinput` is single-line and drops characters on multi-byte / clipboard pastes; `textarea` handles paste cleanly. **For any paste-friendly field (API keys, base URLs, anything pasted from a browser), use `textarea` even when the content is conceptually one line:**

```go
ta := textarea.New()
ta.CharLimit = 512
ta.SetWidth(50)
ta.SetHeight(1)
ta.ShowLineNumbers = false
ta.Prompt = ""
ta.KeyMap.InsertNewline.SetKeys() // single-line: no newline insertion
ta.SetStyles(themedTextareaStyles())
```

`themedTextareaStyles()` is in the `tui` package — always apply it. A bare `textarea.New()` ships dark default cursor/focus colors that render as a black smear against the warm LingTai theme.

## Dev-mode rebuild gotcha (retired)

Project migrations are retired: production TUI/Portal no longer read, write,
advance, or gate on `.lingtai/meta.json`, so a stale dev binary against a newer
project no longer trips a `data version N is newer than this binary supports`
gate. No `meta.json` version preflight is needed before replacing a local dev
binary, and you never "fix" a project by editing `meta.json` downward — the file
is inert. Rebuilding a fresh binary after ordinary code changes is still the
right way to make `main` behaviour live; the only live migration surface left is
the TUI's per-machine `~/.lingtai-tui/` registry (`tui/internal/globalmigrate/`),
which runs at TUI startup and needs no paired portal bump.

## Auto-upgrader clobbers editable install

The TUI's auto-upgrader (`tui/internal/config/venv.go:428-437`,
`config.CheckUpgrade`; invoked by `ensureRuntimeWithOptions` at
`tui/internal/config/venv.go:462-477`) compares `lingtai.__version__` to PyPI's
latest. If your local source's `pyproject.toml` version is **lower**, it replaces
the editable install with the PyPI wheel — silently undoing dev mode.

**Symptom in the launch banner:**
```
Upgrading lingtai 0.8.0 → 0.8.2...
 - lingtai==0.8.0 (from file:///.../lingtai-kernel)   ← editable, gone
 + lingtai==0.8.2                                      ← PyPI wheel
```

**Prevention:** keep `lingtai-kernel/pyproject.toml` version `>=` PyPI's latest; after a release bump, pull the kernel repo so local source matches.

**Recovery:**
```bash
~/.local/bin/uv pip install -e ~/Documents/GitHub/lingtai-kernel \
    -p ~/.lingtai-tui/runtime/venv
```

## Runtime editable checkout can be stale after a merge

A successful PR merge does not prove running agents execute the merged code. The runtime venv may have `lingtai` installed editable from a checkout (e.g. `~/Documents/GitHub/lingtai-kernel`) that is behind `origin/main` even after the merge, and agents can be launched from older detached worktrees or legacy addon checkouts.

**Symptom:** a feature present on GitHub is missing at runtime; imports such as `lingtai.mcp_servers` fail; an MCP/addon path still points at an old standalone repo or a detached worktree.

**Fix:** probe the exact Python interpreter the agent uses and print `module.__file__` for `lingtai`, `lingtai.kernel`, and the relevant MCP/addon modules; inspect the git root/HEAD behind those paths; fast-forward or editable-reinstall the intended source; then call `system(action="refresh")` and rerun the probe. Command recipe: `reference/setup/SKILL.md` → "Verify the runtime checkout a running agent actually uses". Discipline: `reference/runtime-self-check/SKILL.md`.

## PYTHONPATH pollution shadows the runtime import

A leftover `PYTHONPATH` in the launch environment can silently replace the
runtime venv's `lingtai` with another project's source. This is a real incident:
2026-08-09, a dev agent refreshed onto a fresh frozen main, then post-refresh
probing showed `import lingtai` resolving to a *different* project's scratch
checkout (`exact-8c44d232/src`) because the live parent process carried
`PYTHONPATH=/Users/.../dev-2/.../scratch/.../src` from the session that launched
it. The intended venv was untouched — the import path just pointed elsewhere.

**Root cause:** debug sessions commonly `export PYTHONPATH=...` to test one
scratch/worktree/temp repo. LingTai's refresh watcher copies the parent
environment verbatim (`os.environ`), so pollution survives refresh — a plain
`system(action="refresh")` re-inherits it. The wrong import can persist across
refreshes until the *process* is relaunched from a clean environment.

**Symptoms:** post-refresh probes disagree with the configured venv
(`lingtai.__file__` points at another project's `src/`), `direct_url`/git HEAD
resolve to the wrong checkout, or behavior matches old code that is no longer in
`init.json`/the venv.

**Prevention (launch-session discipline):** never launch the TUI or an agent
from a shell session that exports `PYTHONPATH`. If you must test scratch source,
use `env -u PYTHONPATH` or an isolated venv/preset rather than exporting it into
the shared launch session.

**Detection:** inspect the live process environment, not just config:

```bash
PID=<agent-parent-pid>
ps eww -p "$PID" | tr ' ' '\n' | grep '^PYTHONPATH=' || echo 'PYTHONPATH absent'
```

Then confirm the import resolves from the intended venv with the same probe
under a clean environment:

```bash
VENV_PY=<configured-venv>/bin/python
env -u PYTHONPATH "$VENV_PY" -c 'import lingtai; print(lingtai.__file__)'
```

**Recovery when pollution is already live:** a plain refresh re-inherits the
pollution, so use an identity-verified clean relaunch of the agent process (with
`PYTHONPATH` unset in the new parent and children) instead of relying on
refresh. See `reference/runtime-self-check/SKILL.md` §1 for the full probe and
relaunch recipe.

## Migration cross-package contract (retired)

TUI and portal historically shared `meta.json` and were required to bump
`CurrentVersion` in lockstep across `tui/internal/migrate/` and
`portal/internal/migrate/`. That contract is retired: both registries are
retained as historical/test APIs (m001–m039), and no production binary consults
`meta.json` or refuses a project on version grounds. Do not reintroduce paired
project migrations, mirrored `CurrentVersion` bumps, or the `data version N is
newer than this binary supports` recovery checklist — none of it runs today.

The only live migration surface is the TUI's per-machine registry
(`tui/internal/globalmigrate/` under `~/.lingtai-tui/meta.json`), run by the TUI
alone at startup; Portal has no equivalent and needs no mirror.

## Three-locale rule

A new i18n key means updating all three of `en.json`, `zh.json`, `wen.json` in **both** `tui/i18n/` and (where applicable) `portal/i18n/`. Missing translations show as the raw key on screen — they don't fall back. If a key is genuinely English-only (procedural notes, error stacks), document why; otherwise translate.

## Binary naming

The TUI binary is `lingtai-tui`, **never** `lingtai`. `lingtai` is the Python agent CLI (installed by the kernel into the runtime venv at `~/.lingtai-tui/runtime/venv/bin/lingtai`). Build the TUI to `tui/bin/lingtai-tui`; never to `tui/bin/lingtai`.

## Preset directory structure

- `presets/templates/` — TUI-owned. Rewritten on every Bootstrap from embedded data. Never hand-edit.
- `presets/saved/` — user-owned. Bootstrap never touches it.

The directory IS the answer to "is this a template?" — there's no in-band marker. Each loaded `Preset` carries a `Source` field (`SourceTemplate` / `SourceSaved`); prefer `IsTemplate(p)` over the legacy `IsBuiltin(p.Name)`. When writing `manifest.preset.*` paths from Go code, always use `preset.RefFor(p)` to pick the right subdirectory based on `Source`.

## Authorization gate

`manifest.preset.allowed` is the explicit list of preset paths the agent may swap to at runtime. The kernel refuses any swap not in `allowed`. `default` and `active` MUST both appear in `allowed`; `init_schema.validate_init` enforces this.

## Module naming

The Go module names are `github.com/anthropics/lingtai-tui` and `github.com/anthropics/lingtai-portal`. Historical naming — they are NOT moving to a `Lingtai-AI/` import path even though the GitHub org renamed.

## Notifications — single-slot wire invariant

At most ONE `system(action="notification")` pair lives in the wire history at any time. When the kernel detects a fingerprint change it strips the prior pair and reinjects a fresh one. Agents observe the **current** notification state, not a history of arrivals.

## Rebuild-gate test can false-pass on incidental bucket changes

A service/adapter that rebuilds only when a coarse "bucket" of inputs changes (provider / model / base_url / provider-defaults) is hard to test honestly. A regression test asserting "refresh rebuilds the service" can **pass for the wrong reason**: if the fixture also perturbs an unrelated field feeding the same bucket (`max_rpm`, some provider-default), the rebuild fires off that incidental change rather than the condition under test — green test, still-broken code path.

This surfaced while verifying the Codex prompt-cache refresh fix (PRs #406/#411).

**Discipline:** when testing a bucket-gated rebuild, vary only the field under test and pin everything else in the bucket. Assert on the *effect* the rebuild should produce (the live object was reconstructed; the expected metadata/fingerprint changed), not merely that "a rebuild happened". See `reference/runtime-self-check/SKILL.md` §6 for the live-object verification side of the same problem.

## `uv` vs `pip`

The TUI's runtime venv is uv-managed. There is no `pip` symlink — only `pip3`. Always use `uv pip` for package operations in this venv.
