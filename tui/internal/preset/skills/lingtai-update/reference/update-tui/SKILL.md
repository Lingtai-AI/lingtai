---
name: lingtai-update-command
description: Use when operating /update-tui or lingtai-tui self-update.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `/update-tui`

Nested `lingtai-update` reference. `/update-tui` compares the running TUI with
the latest GitHub release, detects the install method, and requires explicit
confirmation before changing the TUI binary; the selected distribution may also
refresh the co-installed portal binary. It never updates the Python kernel,
presets, or utility library, and never auto-restarts the current TUI — relaunch
after a successful update.

- Homebrew: migrates to LingTai's native installer instead of running brew.
  Runs `install.sh --version <tag> --non-interactive` (no `--update`, no
  `--prefix`, since no source install exists yet) to a fresh native prefix,
  then verifies the new `install.json` source metadata, the binary version at
  the new `bin_dir`, and the Python runtime. On any migration failure the old
  Homebrew formula/keg remains exactly as usable as before the attempt — the
  migration step itself never runs `brew`.
  Before reporting success, the migration also resolves `lingtai-tui` on PATH
  and checks it against the freshly installed binary — on a standard
  Apple-Silicon Homebrew host, `/opt/homebrew/bin` is earlier on PATH than
  install.sh's default native bin dir, so a fresh native install alone does
  **not** make the shell start running it. When PATH still resolves to
  Homebrew, the migration reports "native installed, migration not complete"
  instead of "Migrated!", and every update entry point (startup prompt,
  `self-update`, `doctor`) re-detects and re-reports that same truthful state
  on every subsequent run — it does not re-run the installer and does not
  claim success until PATH actually resolves the native binary. Only once
  that is confirmed does `detectTUIInstallMethod` report the install as
  source/user-local going forward, so subsequent updates use the source path
  below instead of brew.
- Source/user-local: runs the versioned `install.sh --update --prefix ...
  --version <tag> --non-interactive` path and verifies the result.
- Unknown/other or non-comparable/dev versions: reports guidance and does not
  guess a package manager.

**Homebrew removal is a separate, interactive-only decision.** Once a native
install is verified — either right after a fresh migration's PATH takeover
succeeds, or on a later launch that redetects an existing verified-but-shadowed
install — the interactive **startup prompt** (not `/update-tui`'s Bubble Tea
view, not `self-update`, not `doctor`) asks a concrete second question:
"Remove the old Homebrew installation now? [y/N]" (default No). Only an
explicit "y" runs the single injected uninstall
(`brew uninstall lingtai-ai/lingtai/lingtai-tui`, exactly this formula), then
re-resolves `lingtai-tui` on ordinary PATH and requires it to match the
verified native binary before reporting cleanup complete — the currently
running Homebrew process is never treated as proof. A declined prompt, a
failed uninstall, or PATH still not resolving to the native binary afterward
all leave removal reported as pending, and the same question is asked again
on the next interactive launch. `self-update`, `doctor`, and `/update-tui`
never ask this question and never run `brew uninstall` — they only report
both the native and Homebrew paths/versions and the exact manual command.

`lingtai-tui self-update` is the shell command for the same manual update
surface when the interactive TUI is unavailable. `lingtai-tui doctor` is a
broader repair/report path that can also run the detected TUI backend; use this
skill only for its TUI/portal side and defer Python-runtime decisions to the
kernel `system-manual` runtime/kernel update manual.
