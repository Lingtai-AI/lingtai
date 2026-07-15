---
name: lingtai-update-homebrew
description: Use when updating through or exploring the Lingtai Homebrew tap.
version: 1.0.0
last_changed_at: "2026-07-15T01:50:00-07:00"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Homebrew path and tap exploration

Nested `lingtai-update` reference. The supported formula is
`lingtai-ai/lingtai/lingtai-tui`:

```bash
brew install lingtai-ai/lingtai/lingtai-tui
brew update
brew upgrade lingtai-ai/lingtai/lingtai-tui
brew info lingtai-ai/lingtai/lingtai-tui
brew --repository
```

The release workflow writes `Lingtai-AI/homebrew-lingtai/lingtai-tui.rb` from
the tagged source tarball, builds `tui` and `portal`, and runs the TUI version
smoke test. To inspect the installed formula and its origin, use `brew info`
and `brew cat lingtai-ai/lingtai/lingtai-tui`; to locate the checkout, use
`brew --repository` and inspect the reported tap directory. Treat manual tap
edits as debugging only; release automation owns normal formula updates.

Homebrew's formula builds Go and the embedded portal frontend. It honors
`HOMEBREW_GOPROXY` and `HOMEBREW_NPM_CONFIG_REGISTRY` overrides and otherwise
performs its own connectivity probes; see the mainland reference for the
non-guaranteed mirror behavior.
