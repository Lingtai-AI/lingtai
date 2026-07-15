---
name: lingtai-update-detection
description: Use when determining how the running lingtai-tui was installed.
version: 1.0.0
last_changed_at: "2026-07-15T12:00:00-07:00"
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

Symlinks matter: a manually linked or development binary may not be the Cellar
copy that Homebrew upgrades. Check `lingtai-tui doctor` output and the resolved
executable before choosing a manual command. The metadata is diagnostic and
routing information; it is not a credential store.
