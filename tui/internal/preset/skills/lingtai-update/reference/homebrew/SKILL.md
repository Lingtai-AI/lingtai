---
name: lingtai-update-homebrew
description: Use when updating through or exploring the Lingtai Homebrew tap.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
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
smoke test. To inspect the installed formula and its origin, pair `brew info`
above with `brew cat lingtai-ai/lingtai/lingtai-tui`; `brew --repository`
locates the checkout, so inspect the reported tap directory. Treat manual tap
edits as debugging only; release automation owns normal formula updates.

The formula builds Go and the embedded portal frontend and runs its own
connectivity probes. The mainland reference owns the `HOMEBREW_*` proxy and
registry overrides and their non-guaranteed mirror behavior.
