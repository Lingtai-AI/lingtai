---
related_files:
  - ANATOMY.md
  - README.md
  - README.zh.md
  - README.wen.md
  - docs/windows-preview.md
  - docs/known-limitations.md
  - docs/status.md
  - docs/graphify.md
  - docs/i18n-vocab.md
  - docs/tool-descriptions.md
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# docs/

Human- and developer-facing documentation for the Go-side LingTai repo.

> **Maintenance:** see the `lingtai-tui-anatomy` skill. This file is part of the repo anatomy tree. Update it in the same PR when documentation entry points move or when docs stop matching the product. The step-by-step beginner guide is owned by the website tutorial (`https://lingtai.ai/{en,zh,wen}/tutorial/`), not this directory; `docs/` holds repo-native developer and reference material.

## Components

| Path | Role |
|---|---|
| `assets/` | Documentation screenshots and visual assets. |
| `blog/` | Narrative product/dev blog posts. |
| `daily/` | Daily notes / project logs. |
| `plans/` and `superpowers/` | Historical design plans/specs. Treat as background unless a current README/ANATOMY cites them. |
| `stars/` | GitHub stars trend data and rendering helper. |
| `known-limitations.md`, `status.md`, `design*.md`, `graphify.md`, `i18n-vocab.md`, `tool-descriptions.md` | Topic documents that complement README and the binary anatomies. |

## Connections

- `README.md`, `README.zh.md`, and `README.wen.md` each point first-time users to their locale's website tutorial (`https://lingtai.ai/{en,zh,wen}/tutorial/`) for step-by-step learning; the READMEs themselves carry only concise orientation.
- TUI command/help truth lives in the TUI command registry and shipped help assets (`tui/internal/preset/skills/lingtai-tui-help/assets/`); README and the website tutorial defer to those sources rather than duplicating the command catalog.
- Install/upgrade truth lives in README, release notes, and TUI/doctor behavior; the website tutorial gives the friendly route and defers to those sources for exact commands.
- Addon/channel truth lives in the TUI `/mcp` surface and curated addon docs; README and the tutorial summarize the concept and common channels.

## Composition

The documentation tree serves three audiences:

1. **First-time humans:** README orientation → website tutorial (`https://lingtai.ai/{en,zh,wen}/tutorial/`) for the step-by-step walkthrough. This directory no longer holds a beginner manual.
2. **Maintainers / coding agents:** repo-root `ANATOMY.md` → `tui/ANATOMY.md` / `portal/ANATOMY.md` / this file → cited code/docs.
3. **Historical researchers:** plans, specs, blog, daily logs, and design notes.

`docs/` is repo-native developer and reference material. The human-facing beginner guide is deliberately kept as one source of truth on the website, so the READMEs link to it instead of duplicating a manual here.

## State

- The repo-owned Chinese beginner manual (`beginner-work-manual.zh.md`, added by community PR `Lingtai-AI/lingtai#255`, plus its single-file illustrated companion `beginner-work-manual-stick-figure.zh.html`) was removed when the beginner guide moved to the website tutorial. The READMEs now link to `https://lingtai.ai/{en,zh,wen}/tutorial/` instead. Do not reintroduce an in-repo beginner manual — that recreates a second source of truth.
- Routine PR explainers under `reports/` remain local-only by default; only deliberately long-lived docs live under `docs/`.

## Notes

- **Update trigger:** when a PR changes a user-visible slash command, setup flow, install/upgrade path, `/mcp`/addon/channel behavior, daemon/avatar guidance, memory/molt behavior, soul-flow explanation, safety boundary, or troubleshooting path, keep the README orientation and shipped help assets accurate, and flag the website tutorial for a matching update (tracked in the separate website repo).
- **No private paths or secrets:** docs may mention `.secrets/` and placeholders, but must not include real local paths, tokens, chat IDs, or account-specific values.
- **Do not commit routine reports:** `reports/*.html` is local-only by default. Long-lived docs belong under `docs/` and should be represented here when they become maintenance obligations.
