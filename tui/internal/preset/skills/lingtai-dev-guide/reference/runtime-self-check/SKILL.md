---
name: dev-guide-runtime-self-check
description: >
  Nested lingtai-dev-guide reference for developer/operator runtime self-checks
  after a refresh, checkout, or preset/MCP change: probe which lingtai code is
  actually running, confirm the editable source and git HEAD, verify the active
  TUI/portal binary and dev-mode symlinks, rebuild the TUI from a clean release
  worktree, inspect MCP/addon module sources and tool surface, and report
  evidence safely with secrets redacted. Includes verifying that long-lived
  runtime objects (services/adapters/caches) were actually rebuilt after a
  refresh, not just that new source is imported.
version: 1.2.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Runtime Self-Check

Nested lingtai-dev-guide reference. Read this after the top-level router sends
you here whenever you need to verify *what code is actually running* and report
it without leaking secrets. It consolidates the most frequently re-implemented
diagnostic in the network: the post-refresh "which runtime am I executing?" probe.

## Core principle

A **read-only diagnostic** — probe, confirm, report. The only writes allowed are
developer rebuilds you were asked to do (`make build`, editable reinstall). Never
paste secrets into a report: redact tokens, keys, chat IDs, and private absolute
paths, preferring `<your-lingtai-checkout>` / `~/.lingtai-tui/...` forms.

## When to use

- Right after a `refresh`, a branch/worktree switch, an editable reinstall, a
  preset swap, or any MCP/addon config change.
- A fix is merged/built but old behaviour persists ("did it actually load?").
- A fix is imported yet a long-lived service/adapter/cache still serves stale
  behaviour — source-on-disk ≠ rebuilt-at-runtime (see §6).
- An MCP boot failure needs source-of-truth checks, or a maintainer needs a safe
  evidence pack.

## Patch-to-self checklist — merged PR ≠ live runtime

To test a kernel/TUI fix on the agent you are currently speaking through, do
**all** of these. Skipping step 1 is how agents repeatedly refresh stale code and
conclude the fix failed.

1. **Find the runtime import path.** Run the §1 probe — runtime venv Python, not
   `python` on PATH.
2. **Compare HEADs.** If the agent imports `/B/lingtai-kernel` but your PR merged
   in `/A/lingtai-kernel`, update `/B` (`git fetch origin main && git pull
   --ff-only origin main`) or reinstall the intended checkout into the runtime
   venv. Do not edit protected dirty checkouts; stop and report if the
   fast-forward is not clean.
3. **Refresh only after the imported source/package is right.** `refresh` reloads
   from the configured runtime environment; it does not fetch or fast-forward a
   source tree for you.
4. **Do an in-situ probe.** Verify live behaviour or metadata changed on this
   agent — a tool result `_meta`, a token-ledger field, another direct
   observable. Source greps and import probes are necessary but not sufficient.
5. **Report evidence.** Runtime Python, import path, old/new HEAD, action taken
   (fast-forward / editable reinstall / rebuild), refresh result, live probe.

## 1. Agent runtime / kernel source probe

Which `lingtai` package the agent venv executes, whether it is editable, and the
git HEAD behind it. Use the TUI runtime venv Python, not whatever is on PATH:

```bash
VENV_PY="$HOME/.lingtai-tui/runtime/venv/bin/python"

"$VENV_PY" - <<'PY'
import importlib.util, importlib.metadata as md, json, pathlib, sys
spec = importlib.util.find_spec("lingtai")
origin = spec.origin if spec else None
pkg_dir = pathlib.Path(origin).resolve().parent if origin else None
editable = bool(pkg_dir and "site-packages" not in str(pkg_dir))
print(json.dumps({
    "python": sys.executable,
    "lingtai_file": origin,
    "package_dir": str(pkg_dir) if pkg_dir else None,
    "editable_install": editable,
}, indent=2))
try:
    import lingtai.kernel as _kernel
    _kernel_name = 'lingtai.kernel'
except ImportError:
    import lingtai_kernel as _kernel
    _kernel_name = 'lingtai_kernel'
print(f'{_kernel_name}: {_kernel.__file__}')
try:
    print('dist:', md.version('lingtai-kernel'))
    print('direct_url:', md.distribution('lingtai-kernel').read_text('direct_url.json') or '<none>')
except Exception as e:
    print('dist metadata:', repr(e))
PY
```

A path under `site-packages/` means the published wheel is in front (not dev
mode); a path under your kernel checkout
(`.../lingtai-kernel/src/lingtai/__init__.py`) means editable/dev mode is live —
usually what you want during development.

Then capture the checkout's git state, so a stale HEAD can't masquerade as fresh:

```bash
KERNEL_SRC="$("$VENV_PY" -c 'import lingtai,os;print(os.path.dirname(os.path.dirname(lingtai.__file__)))')"
git -C "$KERNEL_SRC" rev-parse --short HEAD 2>/dev/null
git -C "$KERNEL_SRC" status --short --branch 2>/dev/null | head
```

Editable installs are detected via PEP 610 `direct_url.json` and are *not*
auto-upgraded by the TUI (`tui/internal/config/venv.go:isEditableLingtaiInstall`),
so once dev mode is established it stays. An unexpected `site-packages` path means
the auto-upgrader or a `brew reinstall` clobbered it — re-establish dev mode per
the setup/gotchas references.

## 2. Active binary and dev-mode symlink check

The TUI binary is `lingtai-tui` (never `lingtai-agent`, which is the Python CLI).

```bash
which lingtai-tui
readlink -f "$(which lingtai-tui)"   # expect <your-lingtai-checkout>/tui/bin/lingtai-tui in dev mode
lingtai-tui --version                # -N-gSHORTSHA suffix = dev build; clean vX.Y.Z = brew install in front
```

A clean `vX.Y.Z` means the brew-installed binary wins; a `-N-gSHORTSHA` suffix
(from `git describe --tags`) means dev mode is live. Repeat for `lingtai-portal`
when the portal is in scope.

## 3. Rebuild the active TUI from a clean release worktree

To make the running binary reflect `origin/main` (or a release head), rebuild
from a clean worktree, not a dirty feature branch. After rebuilding either
binary, rebuild **both** — a stale portal against a freshly migrated project
fails with `data version N is newer than this binary supports`.

```bash
REPO=<your-lingtai-checkout>
git -C "$REPO" fetch origin main --tags --prune

# Build both so TUI and portal stay at the same meta.json version.
cd "$REPO/tui" && make build
cd "$REPO/portal" && make build
```

### Verify the rebuild actually landed on PATH

`make dev` succeeding and `--version` printing a fresh-looking `-N-gSHORTSHA` do
**not** prove your shell runs the binary you just built. On a machine with many
worktrees, `/opt/homebrew/bin/lingtai-{tui,portal}` often symlink into *another*
worktree's `tui/bin/lingtai-tui`, so your build never reaches PATH — and
`--version` can read the same string from either build. Check link target, source
commit, and mtime explicitly:

```bash
# What does PATH actually resolve to, and where does the symlink point?
which lingtai-tui
readlink "$(which lingtai-tui)"          # the immediate symlink target
readlink -f "$(which lingtai-tui)"       # fully resolved path — which worktree's bin?
readlink -f "$(which lingtai)"           # same check for the `lingtai` launcher

# Is that the worktree you just built in? Compare to your build output path:
ls -l "$REPO/tui/bin/lingtai-tui"        # mtime should be seconds-fresh after make
stat -f '%m %N' "$(readlink -f "$(which lingtai-tui)")"   # mtime of the on-PATH binary

# Source commit the on-PATH binary was built from (must match $REPO's HEAD):
lingtai-tui --version                    # vX.Y.Z-N-gSHORTSHA — compare SHA to:
git -C "$REPO" rev-parse --short HEAD
```

SHA, mtime, and resolved path must all agree before you trust the binary. If
`readlink -f` lands in a different worktree than `$REPO`, either re-link
`/opt/homebrew/bin/lingtai-{tui,portal}` to `$REPO`'s binaries or build in the
worktree the symlink already targets. If they are real binaries rather than
symlinks, re-link them (see the setup reference) before expecting rebuilds to
take effect.

**Worktree caveat — never strand the PATH symlink.** When you rebuild from a
clean worktree *because the primary checkout is dirty*, `/opt` may already point
into yet another worktree (the one currently live). Before removing or re-linking
anything: (a) `readlink -f` both `/opt/homebrew/bin/lingtai-tui` and `…/lingtai`
to learn which worktree they target; (b) ensure `/opt` points at *your* rebuilt
`tui/bin/lingtai-tui`, or clearly report that it still points elsewhere; and (c)
**do not `git worktree remove` a worktree the PATH symlink targets** — that
leaves a dangling `/opt` link and a broken `lingtai-tui` on PATH. Clean it up
only after re-linking `/opt` to a surviving build.

## 4. MCP / addon source and tool-surface check

Where MCP/addon modules resolve from, without printing any configured secret:

```bash
VENV_PY="$HOME/.lingtai-tui/runtime/venv/bin/python"

# Where do addon/MCP modules import from? (sources only, never env values)
"$VENV_PY" - <<'PY'
import importlib.util, json
mods = [
    "lingtai.mcp_servers.imap",
    "lingtai.mcp_servers.telegram",
    "lingtai.mcp_servers.feishu",
    "lingtai.mcp_servers.wechat",
    "lingtai.mcp_servers.whatsapp",
    "lingtai.mcp_servers.cloud_mail",
]
out = {}
for m in mods:
    spec = importlib.util.find_spec(m)
    out[m] = spec.origin if spec else None
print(json.dumps(out, indent=2))
PY
```

For MCP config, audit references — not values: an entry should reference
`${ENV_VAR}` rather than a hardcoded key, and you report "uses env reference" vs
"hardcoded (length N)" without echoing the secret. Full audit methodology and
safe-reporting format: `reference/security-audit/SKILL.md`. MCP boot failures and
preset/path mismatches: `reference/debug-troubleshoot/SKILL.md`.

## 5. Post-refresh diagnostics checklist

- [ ] Patch-to-self: the imported source/package was updated before refresh, and
  an in-situ live probe confirms the new behaviour.
- [ ] §1 import probe: `lingtai.__file__` resolves where you expect (editable vs wheel).
- [ ] §1 git HEAD of the imported checkout matches the intended commit; tree state noted.
- [ ] §2 active binary resolves to the expected path; version string matches dev/brew expectation.
- [ ] §3 if a fix should be live, the relevant binary/kernel was actually rebuilt/reinstalled.
- [ ] §4 MCP/addon modules import from the expected source; tool surface present.
- [ ] §6 if behaviour still disagrees, the runtime object was actually rebuilt — verified via metadata/fingerprint, not just the import probe.
- [ ] No secrets, tokens, chat IDs, or private absolute paths captured for the report.

## 6. Live object/adapter lifecycle — source-on-disk ≠ rebuilt-at-runtime

The §1–§2 probes confirm the right *files* are imported. They do **not** prove
the long-lived runtime *objects* built from those files were rebuilt after a
`refresh`. A service or adapter constructed once at agent init survives refreshes
whenever the inputs gating its rebuild did not change — so new source can be on
disk and imported while the live agent still serves a stale object.

This bit the Codex prompt-cache work (PRs #406/#411): the affinity/cache source
was present and imported, but after a live `refresh` the token ledger still showed
the old stable id with no `prompt_cache_key` and no rotation. The agent rebuilt
its `LLMService` only when a coarse rebuild-gate bucket
(provider/model/base_url/provider-defaults) changed, and that bucket was stable
across refresh for this provider — so the old service and its cached adapter
outlived the refresh. The fix forced a service/adapter rebuild on the relevant
live refresh while preserving chat-history replay.

The reusable lesson: **when a fix "should be live" but behaviour disagrees,
grepping or importing the source is not evidence — verify the runtime object.**

- Identify what gates the rebuild of the object (service, adapter, client,
  cache), and confirm that gate actually changes when the fix should take effect.
  A rebuild depending on an input stable across refresh will silently never fire.
- Verify object *identity/lifecycle*, not just presence: was the adapter
  re-constructed, or is the init-time instance still alive?
- Check the observable metadata the fix should produce. For cache work that is
  the token ledger: is `codex_prompt_cache_key` (or equivalent) **non-empty**, and
  does the stable id rotate when it should?
- Where a fingerprint is computable, compare before/after concretely — an old
  `sha256(anchor)[:8]`-style id versus an epoch-stamped one — rather than trusting
  "the code looks right."

If metadata or fingerprint still reflects old behaviour after refresh, the object
was not rebuilt regardless of what the import probe says. That is the bug.

## 7. Safe evidence reporting

Report runtime state as a compact, source-labeled evidence pack:

```text
runtime self-check @ <iso-timestamp>
- lingtai source: <package_dir>  (editable=<true|false>)
- kernel HEAD:    <short-sha> [<dirty|clean>]
- active binary:  <resolved path>  version=<vX.Y.Z[-N-gSHA]>
- MCP/addons:     <module>=<source-path>, ... (env-referenced: yes/no)
- anomalies:      <none | short list>
```

Redaction rules, always: replace any token/key/password with `<REDACTED>` and
never print env *values*; generalize private absolute paths to
`<your-lingtai-checkout>` / `~/.lingtai-tui/...`; omit or redact Telegram chat
IDs, emails, and recipient lists; report "match found" / "uses env reference",
not the matched secret.

## Related references

- `reference/setup/SKILL.md` — establish or recover editable dev mode and symlinks.
- `reference/gotchas/SKILL.md` — dev-mode rebuild gotcha, editable-install behaviour.
- `reference/debug-troubleshoot/SKILL.md` — failing networks, MCP boot, preset/path mismatch.
- `reference/security-audit/SKILL.md` — full secret/permission audit and safe-reporting format.
- `reference/cache-hit-rate/SKILL.md` — the token-ledger measurement that proves a cache fix took effect.
