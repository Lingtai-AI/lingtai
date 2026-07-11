---
name: lingtai-tui-dev
description: >
  Mandatory repository-local development guide for LingTai's Go repository — the
  two binaries lingtai-tui and lingtai-portal plus the install pipeline. Use
  this before changing code, architecture documents, tests, packaging,
  capabilities, adapters, or developer documentation in this repository. Routes
  each task through the exact baseline, the distributed ANATOMY/CONTRACT
  systems, focused validation, and pull-request safety gates.
---

# LingTai TUI/Portal Development

Read this skill before every development task in this repository. It owns the
**workflow**, not the architecture facts or interface promises. Follow its links
instead of copying the root documents into this file. This repository ships the
Go side of LingTai: `lingtai-tui`, `lingtai-portal`, and `install.sh`. The
Python kernel lives in the sibling `lingtai-kernel` repository and is launched
as a subprocess; this repository never imports it.

## First establish the task and baseline

1. Re-read the latest human or maintainer instruction. Separate the requested
   change from suggested follow-up work and from unauthorized side effects.
2. Name the selected baseline: normally live `origin/main`, or an explicit tag
   or commit chosen by the maintainer.
3. Work in a real repository worktree. Before analysis or editing, prove the
   worktree is clean and `HEAD` equals the selected baseline. A directory name
   or recorded SHA without equality is not enough. This repository's convention
   is `.worktrees/<slug>/` on a fresh branch off `origin/main`.
4. Use a focused branch and keep unrelated local/runtime worktrees untouched.
   Both this repository and its sibling kernel see concurrent branches and
   stashed WIPs; editing the main checkout has repeatedly lost in-flight work.

See [`README.md`](../README.md) and [`CLAUDE.md`](../CLAUDE.md) for the public
orientation and the Claude Code entry rules that route here.

## Read the distributed systems before editing

Use progressive disclosure in this order:

1. Read root [`ANATOMY.md`](../ANATOMY.md), then descend through the nearest
   child anatomy (`tui/ANATOMY.md`, `portal/ANATOMY.md`, or a per-package
   anatomy) until cited code answers where the relevant files, connections,
   composition, and state live.
2. Read root [`CONTRACT.md`](../CONTRACT.md). If the component is governed, read
   its paired local contract before changing its interface or expected behavior.
3. Read the cited code and narrow tests. Anatomy is navigation, not evidence in
   place of code; Contract is the normative promise, not a description to weaken
   when implementation drifts.
4. Load a narrower manual only when the task needs its commands, examples, or
   troubleshooting. Do not preload unrelated references. The Anatomy/Contract
   convention is defined by the root [`ANATOMY.md`](../ANATOMY.md) and
   [`CONTRACT.md`](../CONTRACT.md) themselves; the bundled `lingtai-tui-anatomy`
   skill is a legacy citation-navigation aid for the existing per-package
   anatomies and awaits a separate alignment, so prefer the root documents where
   they differ. The shipped `lingtai-dev-guide` skill owns the deeper per-topic
   procedures (setup, gotchas, releasing, PR deliverables). This root skill
   routes into those; it does not copy them.

The three local systems have different jobs:

- **ANATOMY** tells you where code is and how it is connected.
- **CONTRACT** tells you what interfaces and expected agent behavior promise.
- **This skill** tells you how to develop and validate a change.

## Make the smallest complete change

Before editing, state the relevant invariant, the intended variation axis, and
the explicit non-goals. Prefer one behavior-locked boundary or vertical slice
over a directory reshuffle or speculative abstraction.

For every code or architecture-document change, assess both distributed systems:

- Files, symbols, connections, composition, or state ownership changed:
  update the relevant Anatomy in the same PR.
- Port, Adapter, Behavior, error, ordering, retry, cancellation, recovery, or
  state semantics changed: update the relevant Contract and shared contract
  tests in the same PR.
- Both changed: update the pair together.
- Neither changed: record that both were checked; do not create documentation
  churn to simulate compliance.

This repository's two binaries share one on-disk state schema, but only the
*shared* part is a cross-binary obligation. A change to shared state that both
binaries consume — a `.lingtai/` field, folder layout, or signal semantic read
by both, or the shared `meta.json` migration version — must keep `lingtai-tui`
and `lingtai-portal` compatible, with the shared migration version space in
lockstep. This is compatibility, not mirrored code: binary-specific state and
capabilities — portal-only `.portal/` port and recordings, TUI-only or
kernel-facing behavior — do not require a mirrored implementation in the other
binary. Follow the repair direction defined
by the roots: verified code is normally the structural truth for Anatomy;
Contract is normative for interface and Behavior, so implementation drift is a
defect unless a maintainer explicitly authorizes a promise change. Do not create
a second graph registry; maintain YAML `related_files` around the nodes you
touch.

## Validate in layers

Run the narrowest decisive checks first, then the affected broader checks. At a
minimum:

```bash
# Architecture-document structural test (in the TUI module), when either graph changes:
cd tui && go test -run TestArchitecture ./...
# Go changes:
cd tui && go test ./... && go vet ./...
cd portal && go test ./... && go vet ./...
git diff --check
```

When either distributed graph changes, also run the anatomy citation scan — the
embedded `python3` snippet documented in the `lingtai-tui-anatomy` skill
(`tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`, under "scan anatomy
citations before commit"). It walks every `ANATOMY.md` and reports missing or
out-of-range `file:line` citation targets. Run it as documented there (paste the
heredoc, or wrap it in a local scratch script); there is no packaged `--check`
subcommand today. The architecture-document test validates document structure
and the graph; the citation scan validates that citations still point at real
code.

Also run package, import, build, adapter, or source-drift checks when the diff
crosses those boundaries (the portal additionally needs its web build:
`cd portal/web && npm ci && npm run build`). Inspect every non-zero exit, and
never report a timed-out or interrupted suite as passing. Review the final diff
against the human instruction and name any untested risk.

## Pull-request and side-effect gate

Use a pull request; never push directly to `main`. Before committing, pushing,
or opening the PR:

1. re-read the latest human scope and authorization;
2. verify live base equality and stop if the base moved;
3. verify local Git author identity and the intended GitHub CLI account;
4. ensure the staged diff contains only the reviewed change;
5. capture focused validation evidence and unresolved risks.

Commit, push, open/close/merge PRs, publish, install, refresh, release, rebuild
the running binaries, or change configuration only within the maintainer's
explicit authorization for that specific side effect. Opening a PR does not
imply permission to merge it.

## Grow this as the repository agent dev kit

This directory owns this repository's complete reusable development kit, not
only this entry document. Add supporting material when real repeated work
justifies it:

- `scripts/` for deterministic checks, maintenance, or generation;
- `references/` for deep procedures loaded only when needed;
- `assets/` for templates, examples, or fixed resources.

Add such a file only after real repeated work justifies it; document it from
this SKILL.md; keep it executable or self-contained as appropriate; and reject
secrets, generated output, one-off reports, empty directories, placeholders, or
speculative wrappers. Do not copy repository rules into support files. Keep one
`SKILL.md` entry, let it route progressively, and validate every script or asset
against the workflow that needs it.

## Keep the network maintainable

When this workflow changes, update this repository-local skill and the
README/Anatomy/Contract entry routes together. Change the normative root
documents only when their own meaning, schema, or promises change. Keep this
file a concise router: detailed architecture belongs in Anatomy/Contract, and
tool-specific recipes belong in the narrower manuals they own.
