---
name: dev-guide-runtime-self-check
description: >
  Nested lingtai-dev-guide reference for deep, trigger-gated provenance and
  lifecycle diagnosis during a source/venv/interpreter or TUI-binary cutover,
  for changed or failing MCP source, or when a runtime mismatch is suspected.
  Use only the matching import/distribution/editable/HEAD, binary, MCP-source,
  live-object, redaction, or PYTHONPATH-recovery material. Ordinary same-runtime
  reload sequencing belongs to kernel system-manual -> refresh-precheck.
version: 2.0.0
last_changed_at: "2026-09-09T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Runtime Self-Check

Nested lingtai-dev-guide reference. This is a deep diagnostic library selected
only when a cutover, observed failure, or suspected mismatch raises a provenance
or lifecycle question. Kernel `system-manual` →
`reference/refresh-precheck/SKILL.md` is the single owner of ordinary agent
refresh sequencing; this reference neither repeats nor initiates that
transaction.

## Core principle

Probe, confirm, and report only the assertion named by the trigger. Diagnosis is
read-only. A rebuild, editable reinstall, relink, source update, process stop, or
clean relaunch needs separate exact authorization and its owning procedure. Never
paste secrets into a report: redact tokens, keys, chat IDs, and private absolute
paths, preferring `<your-lingtai-checkout>` / `~/.lingtai-tui/...` forms.

## When to use

- A source/venv/interpreter-selector or editable-install cutover needs one exact
  runtime provenance assertion.
- A TUI-binary cutover needs binary, symlink, build, or PATH provenance.
- Changed or failing MCP source needs its specific module-source assertion, or
  MCP boot/tool behaviour suggests a source mismatch.
- On-disk source and live behaviour disagree, including a suspected long-lived
  service/adapter/cache mismatch.
- A proven inherited `PYTHONPATH` mismatch needs source diagnosis or the
  separately authorized clean-relaunch recovery.

**Ordinary same-runtime reload does not use this deep checklist.** A routine
reload or preset swap with no provenance question routes directly to kernel
`system-manual` → `reference/refresh-precheck/SKILL.md`.

## Source/venv cutover diagnostic handoff

A merged change or correct-looking checkout is not proof of what the live agent
imports. Keep the owner boundary explicit:

1. The update/build owner uses the relevant §1 probe to identify the exact
   runtime interpreter, imported source/package, HEAD/version, and intended
   selectors. Any source update or reinstall remains that owner's separately
   authorized action; stop rather than modify a protected dirty checkout.
2. That owner freezes and hands off the cutover target evidence. Kernel
   `system-manual` → `reference/refresh-precheck/SKILL.md` owns the transaction
   and its targeted receipt; this diagnostic does not restate either.
3. Use only the assertion the trigger needs: §1 for import provenance, §6 when
   source is present but live behaviour still disagrees, §2–§3 for a TUI-binary
   cutover, or §4 for changed/failing MCP source.
4. Report only the selected evidence under §7's redaction rules.

## 1. Agent runtime / kernel source probe (cutover or mismatch only)

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

### PYTHONPATH shadowing check

Before trusting an import probe, confirm the live agent process is not carrying a
stale `PYTHONPATH` that overrides the venv. Debug sessions often `export
PYTHONPATH=...` to test a scratch/worktree/temp repo, and because LingTai's
refresh watcher copies the parent environment verbatim, that pollution survives
refresh — a plain refresh re-inherits it and the wrong import can persist until
the process is relaunched from a clean environment. See `reference/gotchas/SKILL.md`
("PYTHONPATH pollution") for the full incident and rules.

```bash
PID=<agent-parent-pid>
ps eww -p "$PID" | tr ' ' '\n' | grep '^PYTHONPATH=' && echo 'POLLUTED' || echo 'clean'
```

Then confirm the import resolves from the intended venv under a clean
environment (this is the source of truth, not whatever `python` resolves on
PATH):

```bash
VENV_PY=<configured-venv>/bin/python   # e.g. ~/.lingtai-tui/runtime/venv/bin/python
env -u PYTHONPATH "$VENV_PY" - <<'PY'
import importlib.util, json
spec = importlib.util.find_spec("lingtai")
print(json.dumps({"lingtai_file": spec.origin if spec else None}))
PY
```

If `ps eww` shows a `PYTHONPATH` and the venv probe differs, an ordinary
refresh will **not** fix it because the watcher re-inherits the pollution; use
the clean-relaunch recipe in §8 instead.

## 2. Active binary and dev-mode symlink check (TUI cutover only)

Use this section only for a TUI/portal binary cutover or suspected PATH/build
mismatch. The TUI binary is `lingtai-tui` (never `lingtai-agent`, which is
the Python CLI).

```bash
which lingtai-tui
readlink -f "$(which lingtai-tui)"   # expect <your-lingtai-checkout>/tui/bin/lingtai-tui in dev mode
lingtai-tui --version                # -N-gSHORTSHA suffix = dev build; clean vX.Y.Z = brew install in front
```

A clean `vX.Y.Z` means the brew-installed binary wins; a `-N-gSHORTSHA` suffix
(from `git describe --tags`) means dev mode is live. Repeat for `lingtai-portal`
when the portal is in scope.

## 3. Rebuild the active TUI from a clean release worktree

Use this section only when a TUI/portal cutover actually requires a rebuild and
that mutation has separate exact authorization. To make the running binary
reflect `origin/main` (or a release head), rebuild
from a clean worktree, not a dirty feature branch. Rebuild both binaries when
both are in scope — project migrations are retired, so there is no `.lingtai/meta.json`
version gate to keep in lockstep (see `reference/architecture/SKILL.md`).

```bash
REPO=<your-lingtai-checkout>
git -C "$REPO" fetch origin main --tags --prune

# Build both when the change touches either binary (no meta.json version lockstep).
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

## 4. MCP / addon source check (changed or failing module only)

Use this only when a named MCP package/config changed, an MCP boot/tool failure
occurred, or a module-source mismatch is suspected. Probe only the module named
by that trigger; never inventory unrelated addons or print configured secrets:

```bash
VENV_PY="$HOME/.lingtai-tui/runtime/venv/bin/python"
MOD="lingtai.mcp_servers.telegram"  # replace with the one changed/failing module

"$VENV_PY" - "$MOD" <<'PY'
import importlib.util, json, sys
module = sys.argv[1]
spec = importlib.util.find_spec(module)
print(json.dumps({module: spec.origin if spec else None}, indent=2))
PY
```

For the relevant MCP config, audit references — not values: an entry should
reference `${ENV_VAR}` rather than a hardcoded key, and report "uses env
reference" vs "hardcoded (length N)" without echoing the secret. Full audit
methodology and safe-reporting format: `reference/security-audit/SKILL.md`.
MCP boot failures and preset/path mismatches:
`reference/debug-troubleshoot/SKILL.md`.

## 5. Trigger-to-section index

| Current trigger | Load only |
|---|---|
| Source/venv/interpreter or editable-install cutover; suspected import/distribution/HEAD mismatch | §1 |
| TUI/portal binary cutover or suspected PATH/symlink/build mismatch | §2; §3 only when an authorized rebuild is required |
| Named MCP source/config change, boot failure, or module-source mismatch | §4 for that module only |
| On-disk source disagrees with live service/adapter/cache behaviour | §6 |
| Need to disclose selected evidence safely | §7 |
| Proven inherited `PYTHONPATH` pollution requires owner-authorized process recovery | §1 for diagnosis, then §8 |

If no row matches, stop rather than manufacture a cross-subsystem audit. For an
ordinary same-runtime reload or preset swap, use kernel `system-manual` →
`reference/refresh-precheck/SKILL.md`, not this index.

## 6. Live object/adapter lifecycle — source-on-disk ≠ rebuilt-at-runtime

Use this only when the expected source is present but observed live behaviour
still disagrees. The selected provenance probe confirms the right *files* are
imported; it does **not** prove the long-lived runtime *objects* built from those
files were rebuilt. A service or adapter constructed once at agent init can
survive lifecycle reloads whenever the inputs gating its rebuild did not change —
so new source can be on disk and imported while the live agent still serves a
stale object.

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

Report a compact, trigger-dependent evidence pack. Omit every subsystem not
selected by the trigger:

```text
runtime self-check @ <iso-timestamp>
- trigger:            <cutover or suspected mismatch>
- selected assertion: <import | binary | MCP source | live object | PYTHONPATH>
- result:             <expected vs observed>
- source evidence:    <only paths/version/HEAD/fingerprint relevant to assertion>
- anomalies:          <none | short list>
```

For example, source/venv evidence may name `lingtai` source and kernel HEAD;
binary evidence may name the resolved binary and version; MCP evidence may name
only the changed/failing module's source. Do not report all three by default.

Redaction rules, always: replace any token/key/password with `<REDACTED>` and
never print env *values*; generalize private absolute paths to
`<your-lingtai-checkout>` / `~/.lingtai-tui/...`; omit or redact Telegram chat
IDs, emails, and recipient lists; report "match found" / "uses env reference",
not the matched secret.

## 8. Clean relaunch of a polluted agent process

When a live agent carries a `PYTHONPATH` that points at another project's source
and a plain refresh re-inherits it, the only correct recovery is an
identity-verified clean relaunch of the process with `PYTHONPATH` unset in the
new parent and children. This is an **authorized owner/operator action** — get
explicit permission for the exact PID/root before doing it.

1. **Identity-verify the old parent.** Confirm the PID, full command, parent
   lineage, working directory, and start identity match the agent root you think
   you are restarting. Never kill on a `ps | grep | xargs kill` hunch.
2. **SIGTERM the old parent gracefully** and confirm its children exit.
3. **Launch the new parent from a clean environment**, explicitly unsetting the
   pollution the session would otherwise inherit:

   ```bash
   ROOT=<agent-root-dir>                 # e.g. .../.lingtai/<agent>
   VENV_PY=<configured-venv>/bin/python  # exact venv from init.json
   env -u PYTHONPATH \
       -u LINGTAI_RUNTIME_PYTHON \
       -u LINGTAI_RUNTIME_VENV \
       -u LINGTAI_REFRESH_ENV_OVERWRITE \
       "$VENV_PY" -m lingtai run "$ROOT"
   ```
4. **Verify the relaunch, not just that a process exists:** new parent/children
   PIDs and PPID 1; `ps eww` shows `PYTHONPATH` absent; `sys.executable`,
   `lingtai.__file__`, `kernel.find_spec`, and `direct_url` all resolve from the
   intended venv/source; heartbeat fresh; a producer-channel read (e.g. Telegram)
   and a self-email canary both PASS; the old PID is absent. Preserve any old
   rollback/evidence and do not do a second refresh.

This recipe was proven on 2026-08-09 when a dev agent's live parent inherited
`PYTHONPATH` from its launch session (pointing at another project's scratch
`src`), survived the first refresh, and required exactly this clean relaunch
before `import lingtai` resolved from the intended frozen source.

## Related references

- Kernel `system-manual` → `reference/refresh-precheck/SKILL.md` — own the
  ordinary refresh transaction and targeted receipt.
- `reference/setup/SKILL.md` — establish or recover editable dev mode and
  supply update/build target evidence.
- `reference/gotchas/SKILL.md` — dev-mode rebuild gotcha, editable-install behaviour.
- `reference/debug-troubleshoot/SKILL.md` — failing networks, MCP boot, preset/path mismatch.
- `reference/security-audit/SKILL.md` — full secret/permission audit and safe-reporting format.
- `reference/cache-hit-rate/SKILL.md` — the token-ledger measurement that proves a cache fix took effect.
