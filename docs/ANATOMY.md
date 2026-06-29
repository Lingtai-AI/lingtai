---
related_files:
  - ANATOMY.md
  - README.md
  - README.zh.md
  - README.wen.md
  - docs/beginner-work-manual.zh.md
  - docs/beginner-work-manual-stick-figure.zh.html
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

> **Maintenance:** see the `lingtai-tui-anatomy` skill. This file is part of the repo anatomy tree. Update it in the same PR when documentation entry points move, when a human-facing manual becomes authoritative for a capability surface, or when docs stop matching the product.

## Components

| Path | Role |
|---|---|
| `beginner-work-manual.zh.md` | **Human-facing anatomy / capability map** for first-time users. Explains what LingTai is, first-use flow, common slash commands, files/bash/web/vision/skills/knowledge/pad, MCP/addons, daemon/avatar, context pressure and molt, soul flow, safety/privacy, and troubleshooting. |
| `beginner-work-manual-stick-figure.zh.html` | Standalone browser-friendly animated explainer for the same beginner manual. No remote assets; keep it consistent with the Markdown manual. |
| `assets/` | Documentation screenshots and visual assets. |
| `blog/` | Narrative product/dev blog posts. |
| `daily/` | Daily notes / project logs. |
| `plans/` and `superpowers/` | Historical design plans/specs. Treat as background unless a current README/ANATOMY cites them. |
| `stars/` | GitHub stars trend data and rendering helper. |
| `known-limitations.md`, `status.md`, `design*.md`, `graphify.md`, `i18n-vocab.md`, `tool-descriptions.md` | Topic documents that complement README and the binary anatomies. |

## Connections

- `README.md`, `README.zh.md`, and `README.wen.md` point first-time users to `beginner-work-manual.zh.md` and the animated HTML version.
- TUI command/help truth lives in the TUI command registry and shipped help assets; the beginner manual summarizes it for humans and must not drift from those sources.
- Install/upgrade truth lives in README, release notes, and TUI/doctor behavior; the beginner manual gives a friendly route and should defer to those sources for exact commands.
- Addon/channel truth lives in the TUI `/mcp` surface and curated addon docs; the manual summarizes the concept and common channels.

## Composition

The documentation tree serves three audiences:

1. **First-time humans:** README docs-by-goal → `beginner-work-manual.zh.md` → optional animated HTML.
2. **Maintainers / coding agents:** repo-root `ANATOMY.md` → `tui/ANATOMY.md` / `portal/ANATOMY.md` / this file → cited code/docs.
3. **Historical researchers:** plans, specs, blog, daily logs, and design notes.

The beginner manual is intentionally not just a loose article: it is the human-facing counterpart to code anatomy. It maps the product surface into concepts a user can act on.

## State

- `beginner-work-manual.zh.md` and `beginner-work-manual-stick-figure.zh.html` were added by community PR `Lingtai-AI/lingtai#255` and linked from the three READMEs.
- The animated HTML is checked in deliberately as long-lived documentation, unlike routine PR explainers under `reports/`.
- The manual uses version-drift disclaimers; do not rely on those disclaimers as a substitute for updating it when the product surface changes.

## Notes

- **Update trigger:** when a PR changes a user-visible slash command, setup flow, install/upgrade path, `/mcp`/addon/channel behavior, daemon/avatar guidance, memory/molt behavior, soul-flow explanation, safety boundary, or troubleshooting path, check whether the beginner manual and animated HTML need edits.
- **Keep Markdown and HTML aligned:** if one is updated substantively, update the other or leave a clear note explaining why the HTML remains a high-level explainer.
- **No private paths or secrets:** docs may mention `.secrets/` and placeholders, but must not include real local paths, tokens, chat IDs, or account-specific values.
- **Do not commit routine reports:** `reports/*.html` is local-only by default. Long-lived docs belong under `docs/` and should be represented here when they become maintenance obligations.
