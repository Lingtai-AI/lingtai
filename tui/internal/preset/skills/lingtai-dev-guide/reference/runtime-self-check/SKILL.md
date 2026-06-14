---
name: dev-guide-runtime-self-check
description: >
  Nested lingtai-dev-guide reference for developer/operator runtime self-checks
  after a refresh, checkout, or preset/MCP change: probe which lingtai code is
  actually running, confirm the editable source and git HEAD, verify the active
  TUI/portal binary and dev-mode symlinks, rebuild the TUI from a clean release
  worktree, inspect MCP/addon module sources and tool surface, and report
  evidence safely with secrets redacted.
version: 1.0.0
---

# Runtime Self-Check

Nested lingtai-dev-guide reference. Read this after the top-level router sends
you here whenever you need to verify *what code is actually running* and report
it without leaking secrets.

This consolidates the most frequently re-implemented diagnostic in the network:
the post-refresh "which runtime am I executing?" probe. Use it after a
`refresh`, a checkout/branch switch, an editable reinstall, a preset swap, or any
MCP/addon config change — and any time a fix "should be live" but behaviour
disagrees.

## Core principle

This is a **read-only diagnostic**. Probe, confirm, and report. The only write
actions explicitly allowed here are developer rebuilds you were asked to do
(`make build`, editable reinstall). Never paste secrets into a report: redact
tokens, keys, chat IDs, and private absolute paths, and prefer repo-relative or
parameterized forms (`<your-lingtai-checkout>`, `~/.lingtai-tui/...`).

## When to use

- Right after a `refresh` — verify the runtime picked up the intended code.
- After switching branches/worktrees or reinstalling the kernel editable.
- A code fix is merged/built but old behaviour persists ("did it actually load?").
- A preset swap, MCP boot failure, or addon change needs source-of-truth checks.
- You must report runtime state to a maintainer and want a safe evidence pack.

## 1. Agent runtime / kernel source probe

Confirm which `lingtai` package the agent venv is executing, whether it is an
editable checkout, and the git HEAD of that checkout. Use the TUI runtime venv
Python (not whatever `python` happens to be on PATH):

```bash
VENV_PY="$HOME/.lingtai-tui/runtime/venv/bin/python"

# Which lingtai is imported, and is it an editable checkout?
"$VENV_PY" - <<'PY'
import importlib.util, json, os, pathlib
spec = importlib.util.find_spec("lingtai")
origin = spec.origin if spec else None
pkg_dir = pathlib.Path(origin).resolve().parent if origin else None
editable = bool(pkg_dir and "site-packages" not in str(pkg_dir))
print(json.dumps({
    "lingtai_file": origin,
    "package_dir": str(pkg_dir) if pkg_dir else None,
    "editable_install": editable,
}, indent=2))
PY
```

Interpretation:

- A path under `site-packages/` → the published wheel is in front (not dev mode).
- A path under your kernel checkout (`.../lingtai-kernel/src/lingtai/__init__.py`)
  → editable/dev mode is live. This is what you usually want during development.

Then capture the checkout's git state (so a stale HEAD can't masquerade as fresh):

```bash
KERNEL_SRC="$("$VENV_PY" -c 'import lingtai,os;print(os.path.dirname(os.path.dirname(lingtai.__file__)))')"
git -C "$KERNEL_SRC" rev-parse --short HEAD 2>/dev/null
git -C "$KERNEL_SRC" status --short --branch 2>/dev/null | head
```

Editable installs are detected via PEP 610 `direct_url.json` and are *not*
auto-upgraded by the TUI (`tui/internal/config/venv.go:isEditableLingtaiInstall`),
so once dev mode is established it stays. If the import path resolves into
`site-packages` unexpectedly, the auto-upgrader or a `brew reinstall` likely
clobbered it; re-establish dev mode per the dev-guide setup/gotchas references.

## 2. Active binary and dev-mode symlink check

The TUI binary is `lingtai-tui` (never `lingtai-agent`, which is the Python CLI).
Confirm which binary the shell actually resolves and whether dev-mode symlinks
are in place:

```bash
which lingtai-tui
readlink -f "$(which lingtai-tui)"   # expect <your-lingtai-checkout>/tui/bin/lingtai-tui in dev mode
lingtai-tui --version                # -N-gSHORTSHA suffix = dev build; clean vX.Y.Z = brew install in front
```

A clean `vX.Y.Z` version means the brew-installed binary wins; a `-N-gSHORTSHA`
suffix (from `git describe --tags`) means dev mode is live. Do the same for
`lingtai-portal` when the portal is in scope.

## 3. Rebuild the active TUI from a clean release worktree

When you need the running binary to reflect `origin/main` (or a release head),
rebuild from a clean worktree rather than a dirty feature branch. After ANY
rebuild of one binary, rebuild **both** — a stale portal against a freshly
migrated project fails with `data version N is newer than this binary supports`.

```bash
REPO=<your-lingtai-checkout>
git -C "$REPO" fetch origin main --tags --prune

# Build both so TUI and portal stay at the same meta.json version.
cd "$REPO/tui" && make build
cd "$REPO/portal" && make build

# Verify the freshly built binary is what the shell now runs.
readlink -f "$(which lingtai-tui)"
lingtai-tui --version
```

If `/opt/homebrew/bin/lingtai-{tui,portal}` are dev-mode symlinks into the
checkout, each `make build` is picked up immediately — no `brew reinstall`. If
they are real binaries, re-link them (see the setup reference) before expecting
rebuilds to take effect.

## 4. MCP / addon source and tool-surface check

Confirm where MCP/addon modules resolve from and what tool surface is exposed,
without printing any configured secret values:

```bash
VENV_PY="$HOME/.lingtai-tui/runtime/venv/bin/python"

# Where do addon/MCP modules import from? (sources only, never env values)
"$VENV_PY" - <<'PY'
import importlib.util, json
mods = ["lingtai_imap", "lingtai_telegram", "lingtai_feishu", "lingtai_wechat"]
out = {}
for m in mods:
    spec = importlib.util.find_spec(m)
    out[m] = spec.origin if spec else None
print(json.dumps(out, indent=2))
PY
```

For MCP config, audit references — not values. An MCP entry should reference
`${ENV_VAR}` rather than a hardcoded key; report "uses env reference" vs
"hardcoded (length N)" and never echo the secret. For the full secret/permission
audit methodology and the safe-reporting format, use the
`reference/security-audit/SKILL.md` reference; for MCP boot failures and
preset/path mismatches, see `reference/debug-troubleshoot/SKILL.md`.

## 5. Post-refresh diagnostics checklist

After a `refresh`, walk this list before trusting new behaviour:

- [ ] §1 import probe: `lingtai.__file__` resolves where you expect (editable vs wheel).
- [ ] §1 git HEAD of the imported checkout matches the intended commit; tree state noted.
- [ ] §2 active binary resolves to the expected path; version string matches dev/brew expectation.
- [ ] §3 if a fix should be live, the relevant binary/kernel was actually rebuilt/reinstalled.
- [ ] §4 MCP/addon modules import from the expected source; tool surface present.
- [ ] No secrets, tokens, chat IDs, or private absolute paths captured for the report.

## 6. Safe evidence reporting

When reporting runtime state to a maintainer, produce a compact, source-labeled
evidence pack. Recommended shape:

```text
runtime self-check @ <iso-timestamp>
- lingtai source: <package_dir>  (editable=<true|false>)
- kernel HEAD:    <short-sha> [<dirty|clean>]
- active binary:  <resolved path>  version=<vX.Y.Z[-N-gSHA]>
- MCP/addons:     <module>=<source-path>, ... (env-referenced: yes/no)
- anomalies:      <none | short list>
```

Redaction rules, always:

- Replace any token/key/password with `<REDACTED>`; never print env *values*.
- Generalize private absolute paths to `<your-lingtai-checkout>` / `~/.lingtai-tui/...`.
- Telegram chat IDs, emails, and recipient lists are private — omit or redact.
- Report "match found" / "uses env reference", not the matched secret itself.

## Related references

- `reference/setup/SKILL.md` — establish or recover editable dev mode and symlinks.
- `reference/gotchas/SKILL.md` — dev-mode rebuild gotcha, editable-install behaviour.
- `reference/debug-troubleshoot/SKILL.md` — failing networks, MCP boot, preset/path mismatch.
- `reference/security-audit/SKILL.md` — full secret/permission audit and safe-reporting format.
