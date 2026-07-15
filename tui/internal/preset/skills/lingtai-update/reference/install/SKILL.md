---
name: lingtai-update-install
description: Use when installing or building the lingtai-tui and lingtai-portal binaries.
version: 1.0.0
last_changed_at: "2026-07-15T12:00:00-07:00"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Install and build

Nested `lingtai-update` reference. The supported public first-install path is:

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
```

`install.sh` resolves the latest GitHub Release, prefers a matching
`lingtai-<tag>-<os>-<arch>.tar.gz` asset, and falls back to building the release
source when no asset is available. It installs both binaries into a selected
bin directory and prepares the TUI-managed runtime; that runtime is not a
second TUI update path.

For a deliberate source build, the current repository layout is:

```bash
cd tui && make build
cd ../portal && make build
```

The portal build first needs its checked-in web dependencies and frontend build
(`cd portal/web && npm ci && npm run build`); `portal/embed.go` then embeds the
result into `lingtai-portal`. Go and Node are build prerequisites, not runtime
dependencies of the portal binary.

The installer accepts `--version <tag>`, `--ref <ref>`, `--bin-dir <dir>` or
`--prefix <dir>`; its `--update --prefix <prefix> --version <tag>
--non-interactive` form is the source updater contract. Prefer the public
`lingtai.ai` URL for new installs. Homebrew remains a supported migration path;
bare `pip install/upgrade lingtai` is kernel development/diagnosis guidance,
not normal TUI update guidance.
