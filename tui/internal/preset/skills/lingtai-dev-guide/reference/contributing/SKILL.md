---
name: dev-guide-contributing
description: >
  Nested lingtai-dev-guide reference for contribution workflow: issue/worktree/PR discipline, stale-worktree cleanup, daemon decomposition, portfolio sweeps, repo-specific build/test commands, skill changes, and anatomy maintenance.
version: 1.1.0
---

# Contributing to LingTai


Nested lingtai-dev-guide reference. Read this after the top-level router sends you here.
This guide covers how to make changes to each component of the LingTai project.

## General principles

1. **Filesystem-only IPC.** The TUI, portal, and kernel communicate exclusively through files. If you need cross-process communication, write a file and let the other side poll.
2. **Anatomy updates are part of the code change.** If your change moves, renames, splits, merges, or deletes a file/function/class cited by an `ANATOMY.md`, update the anatomy in the **same commit**. See the `lingtai-kernel-anatomy` and `lingtai-tui-anatomy` skills for the full convention.
3. **Three-locale rule.** Adding an i18n key means updating all three of `en.json`, `zh.json`, `wen.json` in both `tui/i18n/` and (where applicable) `portal/i18n/`. Missing translations render as the raw key on screen — they don't fall back.
4. **Binary naming.** The TUI binary is `lingtai-tui`, never `lingtai`. `lingtai` is the Python agent CLI inside the runtime venv.
5. **Every non-trivial PR gets a self-contained HTML explainer for the human, but the explainer is local-only by default.** Write a single `.html` file with inline CSS, no remote assets, and no build step, then hand the human its absolute `file://`-openable path in the short message. Prefer an ignored local location such as `artifacts/pr<NUMBER>-<slug>-explainer.html`, `reports/pr<NUMBER>-<slug>-explainer.html`, `tmp/<topic>-<date>.html`, or the agent/worktree report workspace; do **not** commit routine PR explainers. Commit an HTML report only when the human explicitly asks, when it is a release/long-term reference artifact, or when repo documentation links to it deliberately; use `git add -f` for that exception and explain the reason in the PR. Name it `pr<NUMBER>-<slug>-explainer.html` once the PR exists (use `<topic>-<date>.html` pre-push, then rename locally). Write it **before** asking for review or merge, and refresh it locally when blockers or fixes materially change the story. Required sections: TL;DR, baseline, what-was-done (with diff snippets), validation, risks/decisions, next steps, source index. Plain text/Markdown is reserved for the short pointer message and conversational replies. The only exception is a strictly one-line docs/chore PR where the human has explicitly said "no report needed" — absent that waiver, write the HTML even for a small fix. If a change is too small for a useful explainer, that is a signal to bundle it with related work or get the waiver.

## Orchestrator + daemons (how the work happens)

This is the operating discipline for *any* non-trivial LingTai contribution — TUI, portal, kernel, addons, or skills. Read this before you start writing code.

### 1. Clarify and restate the contract

Before dispatching work, restate the task in your own words: what changes, what does not, what "done" looks like, and what is explicitly out of scope. If the request is ambiguous, ask before dispatching. A daemon that runs against a fuzzy brief will deliver a fuzzy diff — and you will pay for it in review time.

### 2. Issue → worktree/branch → PR → merge

Non-trivial work flows through this loop. No exceptions for "small" fixes that turn out to be non-small:

1. **Issue.** Open or pick a GitHub issue that names the problem. If one does not exist, write one — it is the durable record of the contract.
2. **Worktree + branch.** Create an isolated `git worktree` off `origin/main` on a topic branch (`fix/...`, `feat/...`, `docs/...`, `chore/...`). Never edit the main checkout, and never share a worktree across two parallel daemons.
3. **PR.** Push the branch and open a PR against `Lingtai-AI/<repo>`. The PR body cites the issue, summarizes the change, and lists validation steps.
4. **Merge.** After review, merge via the GitHub UI (or `gh pr merge`). Delete the branch and clean up the worktree. Worktrees that outlive this step accumulate — see "Worktree hygiene" below for the periodic cleanup procedure.

### 3. Decompose into daemon-sized tasks

Orchestrators *plan, dispatch, and review*; they do not hand-code. The right tools for code reading, modification, testing, refactoring, PR preparation, batch scanning, and mechanical validation are the daemon backends:

- **Claude Code daemons** — best for exploratory code reading, multi-file edits, skill/doc work, and PR composition.
- **Codex daemons** — best for tightly-scoped diffs, deterministic refactors, and mechanical validation passes.

Each dispatched daemon must receive:

- **A scoped brief.** What to change, what to leave alone, what "done" looks like, where the source-of-truth files live (absolute paths).
- **Its own worktree and branch.** Daemons do not share a working tree. Parallelism is safe only when worktrees are disjoint.
- **Tests or validation steps.** Whatever check confirms the change works — `go test ./...`, `python -m pytest`, frontmatter parse, `git diff --check`, a grep for the new headings. If no test is applicable, say so explicitly.
- **A do-not-touch list.** Files, directories, or branches the daemon must not modify (e.g., unrelated untracked files in the main checkout, sibling worktrees, the main branch).

Use as much **safe parallelism** as the decomposition allows. Independent daemons run concurrently; dependent steps run sequentially. The orchestrator's leverage comes from running many disjoint daemons in parallel, not from doing more of the work itself.

### 4. Orchestrator reviews diffs and tests; does not hand-code

When a daemon reports back, the orchestrator's job is to:

1. Read the diff (not the daemon's summary — diffs are the ground truth).
2. Run or inspect the validation output.
3. Check imports, cross-file consistency, and adherence to the brief.
4. Either merge/forward, or send the daemon back with a tightened brief.

The orchestrator hand-codes only in narrow cases: emergency hotfixes when daemon dispatch overhead is unjustified, throwaway scratch work, or steering the daemon out of a stuck state. Default to dispatch.

### 5. Routine portfolio sweep before broad planning

Before planning any broad LingTai dev work, run — or dispatch — an org-wide **portfolio sweep**:

- Run a read-only `gh` org sweep across `Lingtai-AI/*` to enumerate open issues and PRs.
- Summarize: stale items, unreviewed PRs, items relevant to the planned work, and items that conflict with what you are about to do.
- Let the current PR/issue surface guide which pieces to pick up, defer, or coordinate around.
- Keep the sweep **read-only**. It informs planning; it does not file new issues or comment on PRs as a side effect.

Skipping the sweep is how you end up duplicating in-flight work, stomping on someone else's branch, or shipping a fix that conflicts with a pending refactor.

### 6. Self-operate GitHub via `GH_TOKEN` when the human provides one

For any of the `gh` invocations above — issue triage, PR creation, the portfolio sweep — if the human pastes a GitHub token into the session and you have bash, use it directly: `GH_TOKEN=$TOKEN gh ...`. Don't print commands for the human to copy-paste and don't require `gh auth login`. Read-only probe first (`gh repo view`, `gh issue list`), then ask explicit per-action consent before any mutation (issue creation, PR open/merge, comments). Never echo, log, or persist the token; let it live only in the env of the single command. The full protocol lives in `procedures.md` under "Self-Operating GitHub via GH_TOKEN".

## Worktree hygiene: cleaning stale local worktrees

Every merged PR leaves a worktree behind unless someone removes it, and with
multiple agents/daemons running in parallel, `.worktrees/` fills up fast. Run
this cleanup periodically (or when a sweep shows worktrees piling up). The
discipline is **audit first, remove conservatively, record everything**.

### 1. Audit first — never remove on sight

Fetch with prune so remote-branch existence checks are accurate, then list
every worktree with its merge/dirt/remote status:

```bash
cd <repo-primary-checkout>
git fetch --prune origin

git worktree list --porcelain | awk '/^worktree /{print $2}' | tail -n +2 |
while read -r wt; do
  branch=$(git -C "$wt" branch --show-current)        # empty = detached HEAD
  head=$(git -C "$wt" rev-parse HEAD)
  if git merge-base --is-ancestor "$head" origin/main; then merged=yes; else merged=no; fi
  if [ -n "$branch" ] && git ls-remote --exit-code --heads origin "$branch" >/dev/null 2>&1; then
    remote=exists; else remote=gone; fi
  dirty=$(git -C "$wt" status --porcelain | wc -l | tr -d ' ')
  echo "$wt | branch=${branch:-DETACHED} | merged=$merged | remote=$remote | dirty_files=$dirty"
done
```

(`tail -n +2` skips the first entry, which is the primary checkout.) For any
dirty worktree, inspect `git -C "$wt" status --porcelain` by hand to see
*what* is dirty before deciding anything.

### 2. Removal criteria — all must hold

Remove a worktree only when **every** condition is true:

- It is a **secondary** worktree (never the primary checkout).
- Its HEAD is an **ancestor of `origin/main`** (`git merge-base --is-ancestor`
  succeeded — the work is fully merged).
- Its **remote branch is gone** (deleted after merge) or it is on a detached
  HEAD. A remote branch that still exists may back an in-flight PR.
- It is **clean**, or dirty only with **generated artifacts** — `logs/`,
  `artifacts/`, review-tool outputs (Claude/GLM review files), build
  droppings. Generated-only dirt may be force-removed; anything that looks
  like source edits may not.

### 3. Keep/skip list — when in doubt, skip

- **Primary checkout** — never a removal candidate.
- **Live/protected worktrees** — anything currently serving a purpose, e.g. a
  release worktree that a deployed binary was built from and still resolves to
  (such as `release-vX.Y.Z-<date>`). Check `which`/symlinks before touching
  anything release-shaped.
- **Unmerged branches** — HEAD not an ancestor of `origin/main`. That is
  in-flight or abandoned-but-undecided work; not yours to delete.
- **Remote branch still exists** — likely an open PR; skip.
- **Source-dirty worktrees** — uncommitted edits to tracked source files mean
  skip, *even if the branch is merged*. Someone may have WIP there.
- **Other agents' worktrees** — directories under another agent's project or
  working area are out of scope unless the human explicitly included them.

### 4. Remove

```bash
git worktree remove .worktrees/<slug>            # clean worktree
git worktree remove --force .worktrees/<slug>    # ONLY for generated-only dirt (step 2)
git worktree prune                               # drop metadata for already-deleted dirs
git branch -d <branch>                           # -d (not -D): refuses unmerged branches
```

Stick with `git branch -d` — its refusal on an unmerged branch is a safety
net, not an obstacle. If `-d` refuses, re-check your audit instead of
escalating to `-D`.

### 5. Record and report

Write down what was removed (worktree path, branch, HEAD SHA) and what was
skipped with the reason (e.g. `~/path/to/.worktrees/<slug> — skipped:
source-dirty`), save it as a small report file, and tell the human what was
cleaned. The SHA list is the recovery path: a merged branch's commits remain
reachable from `origin/main`, and even unmerged SHAs stay in the reflog for a
while.

## Changing the TUI (`tui/`)

### Where to look

- **Screens / UI models:** `tui/internal/tui/` — one file per screen (Bubble Tea convention)
- **Presets:** `tui/internal/preset/` — `preset.go` (~1900 lines) handles load/save/list
- **Migrations:** `tui/internal/migrate/` — append a new `m<NNN>_<name>.go` file
- **Filesystem access:** `tui/internal/fs/` — read-only window into agent working directories
- **Subprocess launch:** `tui/internal/process/` — how agents are spawned
- **i18n:** `tui/i18n/` — en/zh/wen JSON tables

### Build and test

```bash
cd ~/Documents/GitHub/lingtai/tui
make build                    # builds to tui/bin/lingtai-tui
make cross-compile            # all platforms
go test ./...                 # run tests
```

### Adding a migration

1. Create `tui/internal/migrate/m<NNN>_<name>.go` exporting `func migrate<Name>(lingtaiDir string) error`.
2. Register in `migrate.go`: append to the `migrations` slice, bump `CurrentVersion`.
3. **Also bump `CurrentVersion` in `portal/internal/migrate/migrate.go`** — the TUI and portal share the `meta.json` version space.
4. If the migration touches shared on-disk state (init.json schema, preset paths), implement it in both packages with identical logic.
5. If it's TUI-only, add a no-op stub `Fn: func(_ string) error { return nil }` in the portal registry to preserve the version slot.

### Adding a new screen

1. Create a new Bubble Tea model in `tui/internal/tui/`.
2. Wire it into the main app model's `Update` function.
3. Add i18n keys to all three locale files.
4. Handle `tea.PasteMsg` forwarding if the screen has text inputs (see gotchas).

## Changing the portal (`portal/`)

### Where to look

- **API handlers:** `portal/internal/api/` — `server.go`, `handlers.go`, `replay.go`
- **Filesystem access:** `portal/internal/fs/` — same shape as TUI's, portal-tailored
- **Web frontend:** `portal/web/src/` — React 19 + TypeScript + Vite
- **Migrations:** `portal/internal/migrate/` — shares version space with TUI
- **i18n:** `portal/i18n/` — independent of TUI's i18n, same three-locale rule

### Build and test

```bash
cd ~/Documents/GitHub/lingtai/portal
make build                    # builds web frontend + Go binary
# Output: portal/bin/lingtai-portal
```

The `make build` pipeline: `npm install` → `npm run build` (in `web/`) → `go build` (embeds `web/dist/` via `embed.go`).

### Changing the web frontend

1. Edit files in `portal/web/src/`.
2. `cd portal/web && npm run build` to rebuild the frontend.
3. `cd portal && make build` to embed the new frontend into the Go binary.
4. The frontend is embedded at compile time via `//go:embed all:web/dist` in `portal/embed.go`.

### Migrations

Same contract as TUI — see "Adding a migration" above. Portal-only migrations get a no-op stub in the TUI registry.

## Changing the kernel (`lingtai-kernel/`)

### Where to look

- **Agent runtime:** `src/lingtai_kernel/` — turn loop, lifecycle, tool dispatch, mailbox, soul/molt
- **Wrapper (CLI + services):** `src/lingtai/` — MCP, FileIO, Vision, Search, CLI
- **Intrinsics:** `src/lingtai_kernel/intrinsics/` — email, soul, system, psyche, codex, etc.
- **Skills:** `src/lingtai/intrinsic_skills/` — bundled skill manuals

The kernel-root anatomy at `src/lingtai_kernel/ANATOMY.md` is the entry point for navigating the source. See the `lingtai-kernel-anatomy` skill for the convention.

### Build and test

```bash
cd ~/Documents/GitHub/lingtai-kernel
pip install -e .              # editable install
python -m pytest              # run tests
```

With the TUI's runtime venv:
```bash
~/.local/bin/uv pip install -e ~/Documents/GitHub/lingtai-kernel \
    -p ~/.lingtai-tui/runtime/venv
```

Changes to the kernel source are reflected immediately in the running agent — no rebuild needed (editable install).

### Auto-upgrader gotcha

The TUI's auto-upgrader (`tui/main.go:283`, `config.CheckUpgrade`) compares `lingtai.__version__` to PyPI's latest. If your local source's `pyproject.toml` version is **lower** than PyPI's, the upgrader replaces the editable install with the PyPI wheel — silently undoing dev mode.

**Prevention:** Ensure `lingtai-kernel/pyproject.toml` `version` is `>=` PyPI's latest. After a release bump, pull the kernel repo so your local source matches.

**Recovery:**
```bash
~/.local/bin/uv pip install -e ~/Documents/GitHub/lingtai-kernel \
    -p ~/.lingtai-tui/runtime/venv
```

## Changing MCP addons

Each addon (imap, telegram, feishu, wechat) is a separate repo with its own MCP server. See the `mcp-manual` skill for the registration workflow.

```bash
# Install in editable mode
~/.local/bin/uv pip install -e ~/Documents/GitHub/lingtai-imap \
    -p ~/.lingtai-tui/runtime/venv

# Register the MCP server
# See mcp-manual skill for the workflow
```

## Changing skills

Skills live in two places:

| Location | Who owns it | Editable? |
|---|---|---|
| `<agent>/.library/intrinsic/` | CLI-managed. Wiped and rewritten on every refresh. | No — edits will be erased. |
| `<agent>/.library/custom/` | You. CLI never touches this. | Yes. |
| `../.library_shared/` | Network-shared. Add with `cp -r`, edit with admin permission. | Admin only. |
| `~/.lingtai-tui/utilities/` | TUI-shipped utilities. | Depends on the skill. |

To author a new skill, see the `skills-manual` skill for the full workflow (frontmatter schema, template, validator, publishing).

## Anatomy maintenance

Every `ANATOMY.md` must be updated in the same commit as the code change it describes. The rules:

- **Every named symbol in Components has a `file:line` citation.**
- **Citations are line ranges, not paragraphs.**
- **Every citation has been verified** — open the cited line and confirm.
- **Cross-references use kernel-root-relative paths.**
- **No leaf stubs, no paraphrase.**

For the full convention, see the `lingtai-kernel-anatomy` skill (Python) or `lingtai-tui-anatomy` skill (Go).

### Cheap mechanical check (Go)

```bash
python - <<'PY'
import pathlib, re
root = pathlib.Path("tui")
for anatomy in root.rglob("ANATOMY.md"):
    text = anatomy.read_text()
    for rel, line in re.findall(r"`?([A-Za-z0-9_./-]+\.(?:go|ts|tsx)):(\d+)", text):
        path = root / rel if not rel.startswith("tui/") else pathlib.Path(rel)
        if not path.exists():
            print(f"{anatomy}: missing citation target {rel}:{line}")
            continue
        n = len(path.read_text().splitlines())
        if int(line) > n:
            print(f"{anatomy}: out-of-range citation {rel}:{line} > {n}")
PY
```

### Cheap mechanical check (Python)

```bash
python - <<'PY'
import pathlib, re
root = pathlib.Path("src/lingtai_kernel")
for anatomy in root.rglob("ANATOMY.md"):
    text = anatomy.read_text()
    for rel, line in re.findall(r"`?([A-Za-z0-9_./-]+\.py):(\d+)", text):
        path = root / rel if not rel.startswith("src/") else pathlib.Path(rel)
        if not path.exists():
            print(f"{anatomy}: missing citation target {rel}:{line}")
            continue
        n = len(path.read_text().splitlines())
        if int(line) > n:
            print(f"{anatomy}: out-of-range citation {rel}:{line} > {n}")
PY
```
