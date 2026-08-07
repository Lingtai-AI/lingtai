---
related_files:
  - ANATOMY.md
  - README.md
  - README.zh.md
  - README.wen.md
  - docs/known-limitations.md
  - docs/status.md
  - docs/graphify.md
  - docs/i18n-vocab.md
  - docs/tool-descriptions.md
  - docs/22610.svg
  - docs/assets/network-demo.gif
  - docs/assets/wechat.png
  - docs/blog/2026-04-09-lingtai-agora.md
  - docs/blog/2026-04-10-launch-recipes.md
  - docs/blog/2026-04-14-four-layers-of-memory.md
  - docs/cyclic-manifold-architecture.md
  - docs/daily/2026-04-05.md
  - docs/daily/2026-04-06.md
  - docs/design-molt-and-network-intelligence.md
  - docs/design.md
  - docs/discussion-log.md
  - docs/example-role.md
  - docs/headless-runtime-contract.md
  - docs/plans/2026-04-18-lingtai-p2p-spec.md
  - docs/plans/design-postman-ipv6-mesh.md
  - docs/plans/tui-image-clipboard-paste.md
  - docs/stars/README.md
  - docs/stars/render_trend.py
  - docs/stars/stars.csv
  - docs/stars/trend.png
  - docs/superpowers/plans/2026-03-30-homebrew-distribution.md
  - docs/superpowers/plans/2026-04-06-scheduler-status.md
  - docs/superpowers/plans/2026-04-09-recipe-json.md
  - docs/superpowers/plans/2026-04-09-recipe-skills.md
  - docs/superpowers/plans/2026-04-10-markdown-viewer.md
  - docs/superpowers/plans/2026-04-10-recipe-covenant-dump-path.md
  - docs/superpowers/plans/2026-04-10-session-markdown-dump.md
  - docs/superpowers/plans/2026-04-11-wechat-addon-kernel.md
  - docs/superpowers/plans/2026-04-11-wechat-addon-tui.md
  - docs/superpowers/plans/2026-04-13-rename-pad-codex-library.md
  - docs/superpowers/specs/2026-03-29-agent-setup-page-design.md
  - docs/superpowers/specs/2026-03-29-self-sufficient-env-design.md
  - docs/superpowers/specs/2026-03-30-pip-bundled-tui-design.md
  - docs/superpowers/specs/2026-04-03-tape-reconstruction-design.md
  - docs/superpowers/specs/2026-04-06-scheduler-status-design.md
  - docs/superpowers/specs/2026-04-09-recipe-json-design.md
  - docs/superpowers/specs/2026-04-09-recipe-skills-design.md
  - docs/superpowers/specs/2026-04-10-markdown-viewer-design.md
  - docs/superpowers/specs/2026-04-10-recipe-covenant-and-dump-path-design.md
  - docs/superpowers/specs/2026-04-10-session-markdown-dump-design.md
  - docs/superpowers/specs/2026-04-11-wechat-addon-design.md
  - docs/superpowers/specs/2026-06-23-doctor-diagnostic-only-design.md
  - docs/superpowers/specs/2026-06-23-launch-kernel-upgrade-confirm-design.md
  - docs/superpowers/specs/2026-06-23-update-command-design.md
  - docs/三魂七魄-横版.jpeg
  - docs/三魂七魄.md
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
| `cyclic-manifold-architecture.md`, `design-molt-and-network-intelligence.md`, `三魂七魄.md` | Conceptual/architectural essays behind the agent model. Background reading, not normative — the binaries' anatomies and `CONTRACT.md` are. |
| `headless-runtime-contract.md` | The public controller contract for `lingtai-tui spawn` / `lingtai-tui list` (spawn readiness, output shape). The implementation map is `tui/internal/headless/ANATOMY.md`. |
| `discussion-log.md`, `example-role.md` | The 2026-03-15 design-discussion log, and a worked example agent role prompt. |
| `22610.svg`, `三魂七魄-横版.jpeg` | Diagram/glyph assets referenced by the docs above. The braille renderings of the same 𢘐 glyph live in `assets/braille/` (root `ANATOMY.md`). |

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
- **`related_files` is this directory tree's full inventory.** The repo-wide no-orphan rule (root `ANATOMY.md`, `## Anatomy convention`) requires every tracked file here to appear in the frontmatter above, and `TestArchitectureDocumentsCoverEveryTrackedFile` (`tui/architecture_documents_test.go`) fails when one is missing. The body stays the curated architectural map: adding a file does not oblige a new row above, but adding its `related_files` entry in the same commit is mandatory — and deleting a file means deleting its entry.
