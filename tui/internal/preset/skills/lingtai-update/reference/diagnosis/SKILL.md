---
name: lingtai-update-diagnosis
description: Use when a TUI or portal install, update, build, or restart fails.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Failure diagnosis

Nested `lingtai-update` reference. Start with read-only identity checks:

```bash
lingtai-tui doctor
lingtai-tui version
command -v lingtai-tui lingtai-portal
```

If `/update-tui` says unknown, inspect the reported executable and the
TUI-owned `~/.lingtai-tui/install.json`; do not substitute Homebrew. If a
source update fails, confirm its prefix/bin directory still exists and that the
requested tag is a strict `vX.Y.Z` release. If a Homebrew-to-native migration
fails, the reported failure line names the exact step (installer command,
`install.json` verification, binary version, or Python runtime) and confirms
the original Homebrew install is untouched and still usable; fix the reported
native-installer error, then retry with `lingtai-tui self-update`. Do not
substitute `brew update` or `brew upgrade` in this migration flow. The migration
step itself never uninstalls, unlinks, or deletes the Homebrew
formula/keg — do not suggest `brew uninstall` as part of recovering from a
migration failure; a failed migration has nothing to clean up. A successful
binary update still requires a fresh process: quit and relaunch the TUI.

If `/update-tui` or a startup prompt reports the native install as ready but
the migration as not complete, this is not a failure to diagnose: install.sh
and every artifact check succeeded, but the resolved `lingtai-tui` on PATH is
still the Homebrew one — typically because Homebrew's bin dir (for example
`/opt/homebrew/bin` on Apple Silicon) comes before install.sh's default native
bin dir on PATH. This state is expected to repeat on every subsequent
interactive launch until the human answers "y" to the concrete
"Remove the old Homebrew installation now? [y/N]" prompt (default No, so a
declined or non-interactive launch leaves it pending), or reorders PATH so the
native bin dir resolves first. `self-update` and `doctor` only ever report
this state and the manual `brew uninstall lingtai-ai/lingtai/lingtai-tui`
command — they never prompt or uninstall. It does not re-run the installer on
each detection.

For source-build failures, separate Go module fetches from the portal's
`npm ci`/frontend build. On asset download failure the installer is designed to
fall back to a source build; record the release tag, OS/architecture, and the
first actionable error rather than retrying blindly. Never paste environment
dumps, tokens, or private paths into reports. Python import, venv, or kernel
errors are outside this skill; follow the kernel's `system-manual`
runtime/kernel update manual.
