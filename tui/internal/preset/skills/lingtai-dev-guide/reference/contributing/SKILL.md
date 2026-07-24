---
name: dev-guide-contributing
description: >
  Nested lingtai-dev-guide reference for contribution workflow: issue/worktree/PR discipline, worktree inventory and exact-object approval gates, daemon decomposition, portfolio sweeps, repo-specific build/test commands, skill changes, and anatomy maintenance.
version: 1.2.1
last_changed_at: "2026-07-24T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Contributing to LingTai

Nested lingtai-dev-guide reference. Read this after the top-level router sends you here.

## General principles

1. **Filesystem-only IPC.** TUI, portal, and kernel communicate exclusively through files. Need cross-process communication? Write a file and let the other side poll.
2. **Anatomy updates are part of the code change.** If your change moves, renames, splits, merges, or deletes anything cited by an `ANATOMY.md`, update the anatomy in the **same commit** (see "Anatomy maintenance" below).
3. **Three-locale rule.** A new i18n key means updating `en.json`, `zh.json`, and `wen.json` in both `tui/i18n/` and (where applicable) `portal/i18n/`. Missing translations render as the raw key — they don't fall back.
4. **Binary naming.** The TUI binary is `lingtai-tui`, never `lingtai`. `lingtai` is the Python agent CLI inside the runtime venv.
5. **Every non-trivial PR gets a self-contained HTML explainer, local-only by default.** Requirements, naming, sections, the commit-vs-local rule, and the waiver exception live in `reference/pr-review-deliverables/SKILL.md` §3 — follow it rather than a summary here.
6. **Prefer one honest sandbox acceptance when it exercises the real end-to-end path.** Keep only minimal syntax/smoke checks for obvious breakage; open the reviewable PR promptly and continue broader validation after the PR when needed, rather than building a broad pytest or synthetic matrix.
7. **Every test must earn its maintenance cost by protecting a distinct failure invariant.** Inventory existing coverage before adding cases. When one mechanism needs multiple inputs, prefer table-driven or parameterized cases and shared setup over one pytest per narrative example. Do not inflate a PR with exhaustive assertion prose, and do not delete non-overlapping old tests merely to make the diff smaller.

## Orchestrator + daemons (how the work happens)

The operating discipline for *any* non-trivial LingTai contribution — TUI, portal, kernel, addons, or skills.

### 1. Clarify and restate the contract

Before dispatching, restate the task: what changes, what does not, what "done" looks like, what is out of scope. Ask if the request is ambiguous. A daemon run against a fuzzy brief delivers a fuzzy diff, and you pay for it in review time.

### 2. Issue → worktree/branch → PR → merge

Non-trivial work flows through this loop — no exceptions for "small" fixes that turn out to be non-small:

1. **Issue.** Open or pick a GitHub issue naming the problem. If none exists, write one — it is the durable record of the contract.
2. **Worktree + branch.** Isolated `git worktree` off `origin/main` on a topic branch (`fix/...`, `feat/...`, `docs/...`, `chore/...`). Never edit the main checkout; never share a worktree between parallel daemons.
3. **PR.** Push and open against `Lingtai-AI/<repo>`. The body cites the issue, summarizes the change, lists validation steps.
4. **Merge.** After review, merge via the GitHub UI or `gh pr merge`, then delete the branch and clean up the worktree (see "Worktree hygiene").

### 3. Decompose into daemon-sized tasks

Orchestrators *plan, dispatch, and review*; they do not hand-code. Code reading, modification, testing, refactoring, PR preparation, batch scanning, and mechanical validation belong to the daemon backends — **Claude Code daemons** for exploratory reading, multi-file edits, skill/doc work, and PR composition; **Codex daemons** for tightly-scoped diffs, deterministic refactors, and mechanical validation.

Every dispatched daemon gets four things:

- **A scoped brief** — what to change, what to leave alone, what "done" means, absolute paths to source-of-truth files.
- **Its own worktree and branch** — parallelism is safe only when worktrees are disjoint.
- **Tests or validation steps** — `go test ./...`, `python -m pytest`, frontmatter parse, `git diff --check`, a grep for new headings; or an explicit "none applicable."
- **A do-not-touch list** — unrelated untracked files, sibling worktrees, the main branch.

Use as much safe parallelism as the decomposition allows. The orchestrator's leverage is many disjoint daemons running concurrently, not doing more of the work itself.

### 4. Orchestrator reviews diffs and tests; does not hand-code

Read the diff (ground truth — not the daemon's summary), run or inspect the validation output, check imports and cross-file consistency against the brief, then merge/forward or send the daemon back with a tightened brief.

Hand-code only for emergency hotfixes where dispatch overhead is unjustified, throwaway scratch work, or steering a daemon out of a stuck state. Default to dispatch.

### 5. Routine portfolio sweep before broad planning

Before planning broad dev work, run or dispatch a read-only `gh` sweep across `Lingtai-AI/*` enumerating open issues and PRs, summarized into stale items, unreviewed PRs, items relevant to the plan, and items that conflict with it. Let that surface decide what to pick up, defer, or coordinate around.

Keep it **read-only** — it informs planning; it does not file issues or comment as a side effect. Skipping it is how you duplicate in-flight work, stomp another branch, or ship a fix that conflicts with a pending refactor. Commands and digest shape: `reference/repo-watch/SKILL.md`.

### 6. Self-operate GitHub via `GH_TOKEN` when the human provides one

If the human pastes a GitHub token and you have bash, use it directly for any `gh` invocation above: `GH_TOKEN=$TOKEN gh ...`. Don't print commands for the human to copy-paste; don't require `gh auth login`. Read-only probe first (`gh repo view`, `gh issue list`), then explicit per-action consent before any mutation. Never echo, log, or persist the token — it lives only in the env of the single command. The `lingtai-issue-report` skill's `filing-flow` reference owns the full consent-and-token protocol for issue filing.

## Worktree hygiene: inventory first, exact-object approval before cleanup

Worktrees accumulate, so inventory them periodically. An audit establishes
eligibility; it **never authorizes deletion**. A merged, clean, generated-only,
empty, temporary, or self-created worktree still requires the human or owning
maintainer to approve the exact worktree path. Branch deletion, force removal,
and metadata pruning are separate destructive actions and are never implied.

### 1. Build a read-only inventory

Use the refs already present locally. Do not run `fetch --prune` during a
read-only audit — it mutates local remote-tracking refs. If freshness is unknown,
label it unknown, or do a separately authorized fetch first.

```bash
cd <repo-primary-checkout>

git worktree list --porcelain | awk '/^worktree /{print $2}' | tail -n +2 |
while read -r wt; do
  branch=$(git -C "$wt" branch --show-current)        # empty = detached HEAD
  head=$(git -C "$wt" rev-parse HEAD)
  if git merge-base --is-ancestor "$head" origin/main; then merged=yes; else merged=no; fi
  if [ -n "$branch" ] && git ls-remote --exit-code --heads origin "$branch" >/dev/null 2>&1; then
    remote=exists; else remote=gone_or_unknown; fi
  dirty=$(git -C "$wt" status --porcelain | wc -l | tr -d ' ')
  echo "$wt | branch=${branch:-DETACHED} | head=$head | merged=$merged | remote=$remote | dirty_files=$dirty"
done
```

For every candidate, inspect `git -C "$wt" status --porcelain` and check whether
any running process, PATH/symlink, built binary, report, or other agent still
references it. Never treat another agent's workspace as yours to clean.

### 2. Prepare a proposal; do not remove yet

A conservative candidate is secondary, fully merged into the observed main,
remote-gone or detached, clean, and unreferenced — but those facts are only
evidence. Before any cleanup, send the human/owner: the **exact worktree path**,
branch, and full HEAD SHA; the merge/remote/dirt/dependency evidence and why
removal is proposed; the exact commands requested, including whether branch
removal, `--force`, or metadata pruning is involved; and the impact and recovery
route.

Wait for an imperative approval naming each exact object. A category such as
"merged worktrees", generated-only dirt, or permission to remove one worktree
does not authorize another worktree, its branch, or a broad prune.

### 3. Execute only the approved objects

After exact approval, substitute only the approved literal values:

```bash
git worktree remove -- <exact-approved-worktree-path>
git branch -d -- <exact-approved-branch>  # only when that branch was also approved
```

Never escalate to `--force`, `-D`, `git worktree prune`, filesystem removal, or a
wildcard/glob unless the approval explicitly covers that exact object and action.
If any observed state changed after approval, stop and ask again.

### 4. Record and report

Record each approved action with worktree path, branch, HEAD SHA, authorization
receipt, command result, and recovery route. Report skipped candidates and why;
never convert a refusal or uncertainty into cleanup pressure.

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
3. **Also bump `CurrentVersion` in `portal/internal/migrate/migrate.go`** — TUI and portal share the `meta.json` version space.
4. If it touches shared on-disk state (init.json schema, preset paths), implement it in both packages with identical logic.
5. If it's TUI-only, add a no-op stub `Fn: func(_ string) error { return nil }` in the portal registry to preserve the version slot.

Version collisions and the `data version N is newer than this binary supports`
failure have their own recovery checklist in `reference/gotchas/SKILL.md`.

### Adding a new screen

1. Create a Bubble Tea model in `tui/internal/tui/`.
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

Pipeline: `npm install` → `npm run build` (in `web/`) → `go build` (embeds `web/dist/` via `embed.go`).

### Changing the web frontend

1. Edit files in `portal/web/src/`.
2. `cd portal/web && npm run build` to rebuild the frontend.
3. `cd portal && make build` to embed it into the Go binary.
4. Embedding happens at compile time via `//go:embed all:web/dist` in `portal/embed.go`.

### Migrations

Same contract as TUI — see "Adding a migration" above. Portal-only migrations get a no-op stub in the TUI registry.

## Changing the kernel (`lingtai-kernel/`)

### Where to look

- **Agent runtime:** `src/lingtai/kernel/` — turn loop, lifecycle, tool dispatch, mailbox, soul/molt
- **Wrapper (CLI + services):** `src/lingtai/` — MCP, FileIO, Vision, Search, CLI
- **Intrinsics:** `src/lingtai/kernel/intrinsics/` — email, soul, system, psyche, codex, etc.
- **Skills:** `src/lingtai/intrinsic_skills/` — bundled skill manuals

The kernel-root anatomy at `src/lingtai/kernel/ANATOMY.md` is the entry point for navigating the source; the `lingtai-kernel-anatomy` skill owns the convention.

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

Kernel source changes need no binary rebuild in editable mode, but they are live
only in the checkout/package the agent actually imports. After a merge in another
worktree: identify the runtime import path and git HEAD, fast-forward or
editable-reinstall that source, then `refresh` and verify with an in-situ probe —
`reference/runtime-self-check/SKILL.md` has the checklist.

Beware the auto-upgrader: a local `pyproject.toml` version lower than PyPI's lets
it replace the editable install with a wheel and silently undo dev mode.
Prevention, symptoms, and recovery are in `reference/gotchas/SKILL.md` →
"Auto-upgrader clobbers editable install".

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

| Location | Who owns it | Editable? |
|---|---|---|
| `<agent>/.library/intrinsic/` | CLI-managed. Wiped and rewritten on every refresh. | No — edits will be erased. |
| `<agent>/.library/custom/` | You. CLI never touches this. | Yes. |
| `../.library_shared/` | Network-shared. Add with `cp -r`, edit with admin permission. | Admin only. |
| `~/.lingtai-tui/utilities/` | TUI-shipped utilities. | Depends on the skill. |

To author a new skill, see `skills-manual` for the full workflow (frontmatter schema, template, validator, publishing), and `reference/skill-stewardship/SKILL.md` for LingTai-specific stewardship.

## Anatomy maintenance

Every `ANATOMY.md` is updated in the same commit as the code change it describes. The citation rules — `file:line` citations for every named symbol, line ranges over paragraphs, verified citations, repo-root-relative cross-references, no leaf stubs, no paraphrase — are owned by the `lingtai-tui-anatomy` skill (Go) and the `lingtai-kernel-anatomy` skill (Python). Read the matching one; don't work from a summary.

### Cheap mechanical check

Scans anatomy citations for missing files and out-of-range line numbers. Set `root`/`ext`/`prefix` for the tree you are in — Go: `tui`, `go|ts|tsx`, `tui/`; Python: `src/lingtai/kernel`, `py`, `src/`.

```bash
python - <<'PY'
import pathlib, re
root, ext, prefix = pathlib.Path("tui"), r"go|ts|tsx", "tui/"
for anatomy in root.rglob("ANATOMY.md"):
    text = anatomy.read_text()
    for rel, line in re.findall(rf"`?([A-Za-z0-9_./-]+\.(?:{ext})):(\d+)", text):
        path = root / rel if not rel.startswith(prefix) else pathlib.Path(rel)
        if not path.exists():
            print(f"{anatomy}: missing citation target {rel}:{line}")
            continue
        n = len(path.read_text().splitlines())
        if int(line) > n:
            print(f"{anatomy}: out-of-range citation {rel}:{line} > {n}")
PY
```

It only catches missing files and out-of-range lines; you still have to open the cited code and confirm the claim.
