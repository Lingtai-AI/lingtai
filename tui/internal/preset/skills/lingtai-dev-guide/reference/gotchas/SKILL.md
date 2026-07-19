---
name: dev-guide-gotchas
description: >
  Nested lingtai-dev-guide reference for known implementation footguns: Bubble Tea v2 paste, textarea theming, dev-mode rebuilds, editable installs, migrations, localization, authorization gates, config conventions, and rebuild-gate test false-passes.
version: 1.1.0
last_changed_at: "2026-07-18T00:00:00Z"
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

## Dev-mode rebuild gotcha

A stale TUI or portal binary against a freshly-migrated project fails with:

```
data version N is newer than this binary supports (M); upgrade lingtai-tui
data version N is newer than this binary supports (M); upgrade lingtai-portal
```

**Root cause:** TUI and portal share `.lingtai/meta.json`. Any binary whose compiled migration `CurrentVersion` is below the project's `meta.json` version must refuse to open it, or it might misread or downgrade newer state. This happens even when the feature PR you wanted is already on `main` — another local branch or newer dev binary may have already written a higher data version to that project.

**Preflight before replacing a local dev binary for an existing project:**

```bash
PROJECT=/path/to/project
CHECKOUT=/path/to/lingtai-checkout

printf 'project meta version: '
python3 - <<PY
import json, pathlib
meta = pathlib.Path('$PROJECT/.lingtai/meta.json')
print(json.loads(meta.read_text()).get('version') if meta.exists() else '<none>')
PY

printf 'tui CurrentVersion: '
grep -R 'const CurrentVersion' "$CHECKOUT/tui/internal/migrate/migrate.go"
printf 'portal CurrentVersion: '
grep -R 'const CurrentVersion' "$CHECKOUT/portal/internal/migrate/migrate.go"
```

If the project's version exceeds the checkout's `CurrentVersion`, **do not install that binary over the user's active `lingtai-tui`**. Build from a checkout/branch that includes the matching migration, or stop and explain that the requested `main` rebuild cannot safely open that project yet. Never "fix" this by editing `meta.json` downward.

**Fix after any migration bump:** rebuild both binaries from the same checkout:

```bash
cd ~/Documents/GitHub/lingtai/tui && make build
cd ~/Documents/GitHub/lingtai/portal && make build
```

The brew-installed pair normally avoids this because released TUI and portal ship together at the same version. Local dev binaries and one-off test overlays are the dangerous case — always preflight before overwriting an active binary for a real project.

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

## Migration cross-package contract

TUI and portal share `meta.json` but have separate migration registries. **When adding a TUI migration, you MUST also bump `CurrentVersion` in `portal/internal/migrate/migrate.go`.**

- Migrations touching shared state → implement in both packages with identical logic.
- TUI-only migrations → add a no-op stub in the portal registry.
- Otherwise the portal refuses to open any project the TUI has already touched.

### Migration-version / rebuild incident checklist

Follow this whenever you hit `data version N is newer than this binary supports (M)` or suspect a version collision between open branches:

1. **Check the project's stamped version:**
   ```bash
   python3 -c "import json,pathlib; print(json.loads(pathlib.Path('.lingtai/meta.json').read_text())['version'])"
   ```

2. **Check `CurrentVersion` in BOTH packages of the exact checkout being built:**
   ```bash
   grep 'const CurrentVersion' tui/internal/migrate/migrate.go portal/internal/migrate/migrate.go
   ```
   Both must match. A mismatch means an incomplete bump — the portal refuses any project the TUI has already touched.

3. **After installing or switching dev binaries, launch and smoke-test the target project — not just `--version`.** A binary printing the right version string may still refuse to open a project with a higher `meta.json` version.

4. **Do not install a feature-branch binary that bumps migrations into shared dev projects** unless that migration PR is the intended runtime path or you can roll `main` forward to include it. A single `make build` on the wrong branch contaminates every project it touches.

5. **If two open branches claim the same migration number:** stop — do not build or launch either binary into a shared project. Identify which branch (if any) already migrated real projects to that version. Renumber and combine before any further binary migrates real projects: assign the earlier repair to the lower claimed version, add a combined catch-up at the next free slot, and have the catch-up call the earlier function idempotently. See `tui/internal/migrate/ANATOMY.md` → "Collision-recovery pattern".

6. **Emergency rollback is NOT editing `meta.json` downward.** That corrupts data written by the higher-version migration. The only safe recovery is building and installing a binary whose `CurrentVersion` meets or exceeds the project's current version.

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
