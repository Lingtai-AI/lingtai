# Graphify repo map

This document explains how contributors use [Graphify](https://github.com/) to build a
knowledge graph of this repository, what the generated artifacts contain, how to
regenerate them, and which navigation/documentation improvements the latest run
surfaced.

The graph itself is **not committed**. Output lands in `graphify-out/`, which is
git-ignored and regenerated locally on demand. This doc plus
[`reports/graphify-repo-map-20260609.html`](../reports/graphify-repo-map-20260609.html)
is the curated, checked-in record derived from the run.

## What Graphify is used for here

LingTai is a multi-language, multi-surface codebase (Go TUI + portal, a Python
runtime, prompt assets, and three README language variants). Graphify gives us a
single semantic map across all of it so we can:

- See the high-level **community structure** of the codebase as a map instead of a
  file tree.
- Find **god nodes** (over-connected symbols) that signal noisy or generic naming.
- Surface **semantic connections** between docs and concepts that file paths hide
  (e.g. the three README variants all mapping to the same concept).
- Identify **weakly connected** docs/code that need stronger cross-links back to
  core concepts before a larger refactor.

Treat the graph as a navigation aid and a source of *leads*, not facts — inferred
and ambiguous edges must be verified against source files before being used as
design evidence.

## Latest run summary

From the local run captured in issue
[#259](https://github.com/Lingtai-AI/lingtai/issues/259):

| Metric | Value |
| --- | --- |
| Supported files in corpus | 490 |
| Approx. words | 563,347 |
| Graph nodes | 6,588 |
| Graph edges | 9,908 |
| Communities | 365 |
| Edge extraction mix | 87% extracted / 13% inferred |
| HTML view | aggregated community view (graph is above the 5,000-node threshold) |

Outputs from that run:

- `graphify-out/graph.html` — interactive community map.
- `graphify-out/GRAPH_REPORT.md` — god nodes, surprising connections, suggested
  questions.
- `graphify-out/graph.json` — raw graph for tooling.

## How to regenerate

For agent-assisted use inside Codex/Claude-style environments:

```bash
/graphify .
```

For headless CLI use, semantic extraction needs an LLM backend when docs, papers,
or images are included:

```bash
export GEMINI_API_KEY=...
graphify extract . --backend gemini
```

Useful follow-up commands once the graph exists:

```bash
graphify query "Why does contains() connect so many code and test nodes?"
graphify explain "Preset"
graphify path "Agent Operating System" "Filesystem-only IPC"
graphify export html
```

### Recommended workflow

1. Generate the graph locally with `/graphify .` or
   `graphify extract . --backend gemini`.
2. Open `graphify-out/graph.html` for the high-level community map.
3. Read `graphify-out/GRAPH_REPORT.md` for god nodes, surprising connections, and
   suggested questions.
4. Use `graphify query`, `graphify explain`, and `graphify path` to investigate
   architecture questions *before* making broad changes.
5. Treat inferred and ambiguous edges as leads, not facts, and verify them against
   source files before using them as design evidence.

## Findings from the latest graph

### Top hub nodes

| Node | Edges |
| --- | --- |
| `contains()` | 92 |
| `Preset` | 71 |
| `FirstRunModel` | 57 |
| `PresetEditorModel` | 55 |
| `App` | 48 |

`Preset`, `FirstRunModel`, `PresetEditorModel`, and `App` are genuine architectural
hubs. `contains()` is a **generic / noisy hub** — see below.

### Interesting semantic connections

- `Network Intelligence` is semantically similar to `AI Organization Substrate`.
- `Agent Operating System` is semantically similar to `AI Organization Substrate`.
- `Email Not Talk Decision` is semantically similar to `Filesystem-only IPC`.
- The English, Chinese, and classical-Chinese README variants all map back to the
  same AI-organization concept.

### Noisy / generic hubs to review

The graph flags several generic high-degree nodes whose labels are not
package-qualified enough to be useful for navigation:

`contains()`, `Delete()`, `Path`, `T`, `main()`.

These are candidates for better package-qualified labels, exclude rules, or
naming conventions in future Graphify runs.

## Actionable navigation & documentation improvements

These are the concrete, low-risk follow-ups the graph suggests. They are tracked
against issue #259's acceptance criteria; this PR completes the documentation
items and records the analysis for the rest.

1. **Cross-link the architecture docs.** The strongest semantic clusters all orbit
   the AI-organization concept. The major architecture docs are:
   - [`ANATOMY.md`](../ANATOMY.md) — source-grounded repo map.
   - [`docs/design.md`](./design.md) — core design.
   - [`docs/design-molt-and-network-intelligence.md`](./design-molt-and-network-intelligence.md)
     — molt + network intelligence.
   - Surface anatomy: [`tui/ANATOMY.md`](../tui/ANATOMY.md),
     [`portal/ANATOMY.md`](../portal/ANATOMY.md).

2. **Strengthen weakly connected docs.** Use the graph's weakly-connected-node list
   to find docs or code areas that need stronger cross-links back to the core
   concepts: `Preset`, `Filesystem-only IPC`, `Agent Operating System`, and
   `AI Organization Substrate`.

3. **Clean up noisy hubs (future Graphify run).** Review `contains()`, `Delete()`,
   `Path`, `T`, and `main()` for package-qualified labels, exclude rules, or
   naming conventions so the graph navigates better.

4. **Maintainer refresh workflow.** Regenerate the graph before larger architecture
   changes or releases (see *How to regenerate*). This stays manual for now; CI can
   come later if the generated artifacts prove useful enough.

5. **Artifact policy.** Keep `graphify-out/` generated locally and git-ignored
   (already configured in `.gitignore`). Commit only curated docs derived from the
   report — this file and the HTML explainer under `reports/`.
