---
name: dev-guide-gotchas
description: >
  Nested lingtai-dev-guide reference for known implementation footguns: Bubble Tea v2 paste, textarea theming, dev-mode rebuilds, editable installs, migrations, localization, authorization gates, config conventions, and rebuild-gate test false-passes.
version: 1.1.0
---

# Gotchas and Known Pitfalls


Nested lingtai-dev-guide reference. Read this after the top-level router sends you here.
Collective wisdom from production incidents. Each entry has caused at least one regression.

## Bubble Tea v2: paste delivery

Bubble Tea v2 splits keys (`tea.KeyPressMsg`) from clipboard pastes (`tea.PasteMsg`). **Any `Update` dispatcher that handles `case tea.KeyPressMsg:` must also forward `tea.PasteMsg` to whichever text widget is focused.** Otherwise paste silently drops.

For embedded sub-models (e.g. `PresetEditorModel` inside `FirstRunModel`), the host's outer `default:` branch must forward paste msgs into the sub-model. If the host's dispatcher enumerates focused widgets per-step but misses the step that hosts an embedded model, paste dies before reaching the sub-model — and adding a fall-through inside the sub-model's own `Update` is useless because the msg never arrives.

**Symptom:** typing into an input works, pasting does nothing.

**Fix:** Trace top-down (`tea.Program` → host's outer switch → sub-model's outer switch → widget) and ensure every layer either handles or forwards `tea.PasteMsg`.

## `textarea` vs `textinput`

`textinput` is single-line and drops characters on multi-byte / clipboard pastes. `textarea` handles paste cleanly. **For any paste-friendly field (API keys, base URLs, anything the user might paste from a browser), use `textarea` even when the content is conceptually one line:**

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

When running symlinked dev binaries, a stale TUI or portal binary against a
freshly-migrated project fails with:

```
data version N is newer than this binary supports (M); upgrade lingtai-tui
data version N is newer than this binary supports (M); upgrade lingtai-portal
```

**Root cause:** The TUI and portal share `.lingtai/meta.json`. Any binary whose
compiled migration `CurrentVersion` is lower than the project's `meta.json`
version must refuse to open the project; otherwise it might misread or downgrade
newer state. This can happen even when the feature PR you wanted is already on
`main`: another local branch or newer dev binary may have already written a
higher data version to the target project.

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

If the target project's version is higher than the checkout's `CurrentVersion`,
**do not install that binary over the user's active `lingtai-tui`**. Either build
from a checkout/branch that includes the matching migration, or stop and explain
that the requested `main` rebuild cannot safely open that project yet. Do not
"fix" this by editing `meta.json` downward.

**Fix after any migration bump:** rebuild both binaries from the same checkout:

```bash
cd ~/Documents/GitHub/lingtai/tui && make build
cd ~/Documents/GitHub/lingtai/portal && make build
```

The brew-installed pair normally avoids this because the released TUI and portal
ship together at the same version. Local dev binaries and one-off test overlays
are the dangerous case; always do the preflight above before overwriting an
active binary for a real project.

## Auto-upgrader clobbers editable install

The TUI's auto-upgrader (`config.CheckUpgrade`) compares `lingtai.__version__` to PyPI's latest. If your local source's version is lower, it replaces the editable install with the PyPI wheel.

**Symptom in the launch banner:**
```
Upgrading lingtai 0.8.0 → 0.8.2...
 - lingtai==0.8.0 (from file:///.../lingtai-kernel)   ← editable, gone
 + lingtai==0.8.2                                      ← PyPI wheel
```

**Prevention:** Keep `lingtai-kernel/pyproject.toml` version >= PyPI's latest.

**Recovery:**
```bash
~/.local/bin/uv pip install -e ~/Documents/GitHub/lingtai-kernel \
    -p ~/.lingtai-tui/runtime/venv
```

Use `uv`, not `pip` — the venv is uv-managed and has no `pip` symlink.

## Runtime editable checkout can be stale after a merge

A successful PR merge does not prove running agents are executing the merged
code. The TUI-created runtime venv may have `lingtai` installed editable from a
checkout such as `~/Documents/GitHub/lingtai-kernel`, and that checkout may be
behind `origin/main` even after the PR merged. Agents can also be launched from
older detached worktrees or legacy addon checkouts.

**Symptom:** a feature that is present on GitHub is missing at runtime; imports
such as `lingtai.mcp_servers` fail; an MCP/addon path still points at an old
standalone repo or a detached worktree.

**Fix:** probe the exact Python interpreter named by the agent/runtime, print
`module.__file__` for `lingtai`, `lingtai_kernel`, and relevant MCP/addon
modules, inspect the git root/HEAD behind those paths, fast-forward the checkout
if it is the intended editable source, then call `system(action="refresh")` and
rerun the probe. See `reference/setup/SKILL.md` → “Verify the runtime checkout a
running agent actually uses” for the full command recipe.

## Migration cross-package contract

TUI and portal share `meta.json` but have separate migration registries. **When adding a TUI migration, you MUST also bump `CurrentVersion` in `portal/internal/migrate/migrate.go`.**

- Migrations touching shared state → implement in both packages with identical logic.
- TUI-only migrations → add a no-op stub in the portal registry.
- Otherwise the portal refuses to open any project the TUI has already touched.

## Three-locale rule

Adding an i18n key means updating all three of `en.json`, `zh.json`, `wen.json` in **both** `tui/i18n/` and (where applicable) `portal/i18n/`. Missing translations show as the raw key on screen — they don't fall back.

If a key is genuinely English-only (procedural notes, error stacks), document why. Otherwise translate.

## Binary naming

The TUI binary is `lingtai-tui`, **never** `lingtai`. `lingtai` is the Python agent CLI (installed by the kernel into the TUI's runtime venv at `~/.lingtai-tui/runtime/venv/bin/lingtai`). Build the TUI to `tui/bin/lingtai-tui`; never to `tui/bin/lingtai`.

## Preset directory structure

Presets are split into two directories:

- `presets/templates/` — TUI-owned. Rewritten on every Bootstrap from embedded data. Never hand-edit.
- `presets/saved/` — User-owned. Bootstrap never touches it.

The directory IS the answer to "is this a template?" — there's no in-band marker. Each loaded `Preset` carries a `Source` field (`SourceTemplate` / `SourceSaved`). Prefer `IsTemplate(p)` over the legacy `IsBuiltin(p.Name)`.

When writing `manifest.preset.*` paths from Go code, always use `preset.RefFor(p)` to pick the right subdirectory based on `Source`.

## Authorization gate

`manifest.preset.allowed` is the explicit list of preset paths the agent may swap to at runtime. The kernel refuses any swap not in `allowed`. `default` and `active` MUST both appear in `allowed`; `init_schema.validate_init` enforces this.

## Module naming

The Go module names are `github.com/anthropics/lingtai-tui` and `github.com/anthropics/lingtai-portal`. This is historical naming — they are NOT moving to a `Lingtai-AI/` import path even though the GitHub org renamed.

## Notifications — single-slot wire invariant

At most ONE `system(action="notification")` pair lives in the wire history at any time. When the kernel detects a fingerprint change, it strips the prior pair and reinjects a fresh one. Agents observe the **current** notification state, not a history of arrivals.

## Rebuild-gate test can false-pass on incidental bucket changes

A service/adapter that rebuilds only when a coarse "bucket" of inputs changes
(e.g. provider / model / base_url / provider-defaults) is hard to test honestly.
A regression test that asserts "refresh rebuilds the service" can **pass for the
wrong reason**: if the test fixture also perturbs an unrelated field that feeds
the same bucket (`max_rpm`, some provider-default), the rebuild fires off that
incidental change — not the condition under test. The test goes green while the
real code path it was meant to protect is still broken.

This surfaced while verifying the Codex prompt-cache refresh fix (PRs #406/#411):
the rebuild gate is bucket-based, so a test must hold the bucket fixed except for
the *one* condition it is exercising.

**Discipline:** when testing a bucket-gated rebuild, vary only the field under
test and pin everything else in the bucket. Assert on the *effect* the rebuild is
supposed to produce (the live object was reconstructed; the expected metadata/
fingerprint changed), not merely that "a rebuild happened" — otherwise an
incidental bucket delta gives you a false-pass. See
`reference/runtime-self-check/SKILL.md` §6 for the live-object verification side
of the same problem.

## `uv` vs `pip`

The TUI's runtime venv is uv-managed. There is no `pip` symlink — only `pip3`. Always use `uv pip` for package operations in this venv.
