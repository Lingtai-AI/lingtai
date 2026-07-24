---
name: lingtai-update-detection
description: Use when determining how the running lingtai-tui was installed.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Install-method detection

Nested `lingtai-update` reference. Detection is deliberately conservative.

1. Read `~/.lingtai-tui/install.json` (or the configured global directory) and
   validate the `lingtai.tui.install/v1` metadata. Matching the running
   executable, including the managed `lingtai-tui` alias, makes this a
   source/user-local install.
2. Otherwise, recognize Homebrew from the executable path and Homebrew
   prefixes/environment, including `/opt/homebrew`, `/usr/local`, and
   `/home/linuxbrew/.linuxbrew` patterns.
3. Otherwise classify it as unknown/other. Do not run `brew` merely because it
   is available, and do not infer source ownership from a path alone when
   source metadata says otherwise.

Symlinks matter: a manually linked or development binary may not be the
Homebrew Cellar copy. Check `lingtai-tui doctor` output and the resolved
executable before choosing a manual command. This metadata is diagnostic and
routing information, not a credential store.

A Homebrew-detected install is not upgraded through brew: `/update-tui` and
`lingtai-tui self-update` migrate it to the native installer instead (see
`reference/update-tui/SKILL.md`). Migrating installs and verifies a native
binary, but does not by itself guarantee the shell will run it: on a standard
Apple-Silicon Homebrew host, `/opt/homebrew/bin` is earlier on PATH than
install.sh's default native bin dir, so the resolved `lingtai-tui` can still
be the old Homebrew one even after a "successful" install. Detection accounts
for this directly — when valid native (`install.json`) metadata exists but
does not match the currently resolved executable, and that executable still
resolves as Homebrew, detection reports `DuplicateNativeInstall` instead of
silently treating it as a plain fresh Homebrew install: the method stays
Homebrew (the resolved binary really is still Homebrew's), and every update
entry point surfaces "native installed, migration not complete, remove
Homebrew yourself" instead of re-prompting or re-running the installer. Only
once the resolved executable actually matches the native metadata does
detection report the install as source/user-local from then on — until then,
the Homebrew Cellar copy stays on disk. Removing it always requires a
concrete interactive "y" to the "Remove the old Homebrew installation now?"
prompt at startup (see `reference/update-tui/SKILL.md`); `self-update` and
`doctor` never uninstall it themselves, only report the manual command.
