---
name: lingtai-update-diagnosis
description: Use when a TUI or portal install, update, build, or restart fails.
version: 1.0.0
last_changed_at: "2026-07-15T12:00:00-07:00"
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
requested tag is a strict `vX.Y.Z` release. If a Homebrew update fails, run
`brew update`, then inspect `brew info lingtai-ai/lingtai/lingtai-tui` and the
formula source before retrying. A successful binary update still requires a
fresh process: quit and relaunch the TUI.

For source-build failures, separate Go module fetches from the portal's
`npm ci`/frontend build. For an asset download failure, the installer is
designed to fall back to a source build; record the release tag, OS/architecture,
and the first actionable error rather than retrying blindly. Do not paste
environment dumps, tokens, or private paths into reports. A Python import,
venv, or kernel error is outside this skill; follow the kernel's
`system-manual` runtime/kernel update manual.
