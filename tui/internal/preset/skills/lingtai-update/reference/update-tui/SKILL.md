---
name: lingtai-update-command
description: Use when operating /update-tui or lingtai-tui self-update.
version: 1.0.0
last_changed_at: "2026-07-15T12:00:00-07:00"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `/update-tui`

Nested `lingtai-update` reference. `/update-tui` compares the running TUI with
the latest GitHub release, detects the install method, and requires explicit
confirmation before changing the TUI binary. It never updates the Python
kernel, presets, utility library, or portal as a separate runtime action, and
it never auto-restarts the current TUI; relaunch after a successful update.

- Homebrew: runs `brew upgrade lingtai-ai/lingtai/lingtai-tui`.
- Source/user-local: runs the versioned `install.sh --update --prefix ...
  --version <tag> --non-interactive` path and verifies the result.
- Unknown/other or non-comparable/dev versions: reports guidance and does not
  guess a package manager.

`lingtai-tui self-update` is the shell command for the same manual update
surface when the interactive TUI is unavailable. `lingtai-tui doctor` is a
broader repair/report path and can also run the detected TUI backend; use this
skill only for its TUI/portal side and defer Python-runtime decisions to the
kernel `system-manual` runtime/kernel update manual.
