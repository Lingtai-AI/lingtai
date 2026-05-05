---
name: lingtai-tui-anatomy
description: >
  The canonical convention for `ANATOMY.md` files in the LingTai Go monorepo —
  the lingtai repo that ships `lingtai-tui` and `lingtai-portal`. Mirrors the
  `lingtai-kernel-anatomy` skill's convention but covers a Go codebase with
  two binary targets, a shared per-project state schema, an embedded React
  frontend, and a tap-bumped Homebrew distribution path.

  The repo itself is mapped by a tree of `ANATOMY.md` files rooted at the
  repo's `ANATOMY.md`. This skill is the convention; those files are the
  content.

  Reach for this skill when:
    - You are about to read TUI or portal Go code and want to navigate by
      structure instead of grep — descend the tree starting at the repo-root
      anatomy.
    - You are about to write or update an `ANATOMY.md` in the lingtai repo
      and need to know the template, the citation rules, and the maintenance
      discipline (which differs from the kernel's in two important ways —
      see "Two-binary symmetry" below).
    - You hit a TUI/portal-specific gotcha (Bubble Tea v2 paste delivery,
      textarea theming, the shared meta.json version space, dev-mode
      rebuild) and want to find the place that explains it.

  How to use:
    1. Read this file once — you are learning the convention.
    2. Open the lingtai repo's `ANATOMY.md` (at the repo root). That is the
       monorepo-root anatomy. Use its Components and Composition sections to
       find the binary tree (`tui/` or `portal/`) whose anatomy holds your
       question.
    3. Descend. At each layer the anatomy points either further down the
       tree or directly at code via `file:line` citations.
    4. Read the cited code. The anatomy is the navigation aid; the code is
       the truth.
    5. If anatomy disagreed with code, update the anatomy before you leave
       the file. Reading and maintaining are the same act.

  How this differs from `lingtai-kernel-anatomy`:
    - Two binary trees (`tui/`, `portal/`) under one repo root, with shared
      per-project state. The repo-root anatomy enumerates both; per-binary
      anatomies sit at `tui/ANATOMY.md` and `portal/ANATOMY.md`.
    - File extension is `.go`, not `.py`. Citations look like
      `tui/internal/preset/preset.go:1399`. The cheap-mechanical-checker
      script in this skill is adapted for Go.
    - Citations into the `lingtai-kernel` repo are NOT used — that is a
      separate tree with its own anatomy. Cross-repo references are
      narrative only ("the kernel writes this file; we read it").
---

# LingTai (Go) Anatomy — the Convention

This skill is the canonical convention for `ANATOMY.md` files in the **lingtai** Go monorepo (the repo that ships `lingtai-tui` and `lingtai-portal`). It is the parallel of `lingtai-kernel-anatomy` for the Go side of the project. The two skills are intentionally similar — same 6-section template, same citation discipline, same maintenance contract — but they cover different trees and have different gotchas.

The convention lives in this skill; the content lives in `ANATOMY.md` files distributed across the lingtai repo, starting at the repo root.

## What an `ANATOMY.md` is

An `ANATOMY.md` file is the **structural description of one folder of code**, written for an agent reader, sitting next to the code it describes.

It is **not**:

- A user manual or how-to guide (those are skills, manuals, tutorials).
- An API contract (those are tool schemas or HTTP route docs).
- A design or rationale document (those live in `discussions/` or commit messages).
- A test specification (those are test files).

It **is**: a code-cited map of *what is in this folder, how the parts connect, and where state lives.* Every structural claim is grounded in a `file:line` reference into the code. If a claim cannot point at a line that says what it claims, the claim does not belong in anatomy.

A folder gets an `ANATOMY.md` when **a competent agent could do useful reasoning about the folder as a unit without first reading its siblings.** Trivial leaves (a single-file helper package, a one-function shim) do not. The repo-root anatomy is the only file that holds a complete child enumeration; every other anatomy maps just its own folder.

## The 6-section template

Every `ANATOMY.md` — including the repo-root anatomy — follows the same shape:

1. **What this is** — one paragraph naming the concept this folder embodies.
2. **Components** — files / functions / types here, with `file:line` citations and one-line purposes.
3. **Connections** — what calls in, what this folder calls out, what data flows through.
4. **Composition** — parent folder, subfolders (each linked to its own `ANATOMY.md`), siblings if structurally relevant.
5. **State** — persistent state this folder writes (files, schema versions), ephemeral state it manages.
6. **Notes** — bounded section for rationale, history, gotchas not visible in code.

~80 lines is the cap; less is better. If a folder needs more, it is probably two folders.

## Two-binary symmetry — what's different from the kernel

The lingtai repo has two binary trees that share a single per-project state schema (`.lingtai/meta.json`) and parallel migration registries. This produces a coupling pattern the kernel doesn't have:

- The TUI and portal **share the meta.json version space.** A migration that bumps `CurrentVersion` in `tui/internal/migrate/migrate.go` MUST also bump it in `portal/internal/migrate/migrate.go`. Anatomy in both `tui/internal/migrate/ANATOMY.md` and `portal/internal/migrate/ANATOMY.md` must reflect this contract.
- Migrations that touch shared on-disk state (init.json schema, preset paths) live in BOTH packages with identical logic. Anatomy in either should cross-reference the other rather than duplicate the per-migration explanation.
- Migrations that only one binary cares about get a no-op stub in the other to preserve the version slot. Anatomy notes the no-op stubs so a reader doesn't think they're orphan files.

Outside migrations, the two binaries are independent. They run in different processes; they don't import each other; they communicate only via the filesystem they both read.

## Use anatomy as navigator, not grep

You are an agent. Reading 200 lines of code is one tool call; greping a symbol gives you 50 hits each costing their own evaluation. For **structural** questions (what shape is this part of the repo, where does behavior X live, what does Y connect to) descend `ANATOMY.md` files top-down. For **enumeration** questions (every callsite of a function, every file matching a pattern) grep is still right.

| Question type | Tool |
|---|---|
| Structural | Descend the anatomy tree |
| Enumeration | grep |

The descent: start at the repo root's `ANATOMY.md`, read its Components and Composition, pick the binary tree (`tui/` or `portal/`) whose territory contains your question, open that binary's anatomy, repeat. At each layer the anatomy will tell you whether to descend further or read the cited code directly.

## Writing checklist

When you write or update an `ANATOMY.md`, every one of these must be true before you commit. They exist because we have already seen each one fail in practice.

- **Every named symbol in Components has a `file:line` citation.** "loads presets (`Load`)" is not enough; "loads presets (`tui/internal/preset/preset.go:421`)" is. Without citations, the next agent grepping for the symbol gains nothing from the anatomy.
- **Citations are line ranges, not paragraphs.** Prefer `tui/internal/preset/preset.go:1330-1360` over a vague "see preset.go". Single-line citations only for one-line landmarks (constants, single-line helpers).
- **Every citation has been verified.** Open the cited line. Confirm it still says what the anatomy claims. Citations rot fastest after refactors.
- **Cross-references between anatomies use repo-root-relative paths.** `tui/internal/preset/ANATOMY.md`, not `./ANATOMY.md` or `../preset/ANATOMY.md`. The repo root is the only stable reference frame.
- **Cross-references are sparse and one-directional.** Cite parent and structural neighbors only — do not enumerate downstream callers (that's a grep question).
- **Cross-binary references are narrative, not citation-rich.** When `portal/internal/api/` reads a file the TUI also reads, mention that the TUI does the same and link to its anatomy. Do not duplicate detailed citation lists across the two binary trees.
- **No leaf stubs.** Empty placeholder anatomies are clutter. A missing `ANATOMY.md` is an honest signal that the folder hasn't been mapped yet.
- **No paraphrase.** Anatomy adds shape and connections, not summary. If the code's good naming already says what you're about to write, don't write it.

## Maintenance is part of reading

Every coding agent that reads anatomy is also a maintainer. The contract:

- **Code matches anatomy:** read on, no action.
- **Code disagrees with anatomy:** the code is almost always right. Update the anatomy to match before you leave the file. If you believe the code itself is wrong, report the bug — and note that anatomy and code disagreed, because that disagreement is itself a clue.
- **Anatomy missing or empty:** if you understood the folder well enough to do your task, write the anatomy. Components, connections, state. Use the writing checklist above.

## When a code change requires anatomy updates

The same-commit rule is about structural drift, not busywork. Update relevant `ANATOMY.md` files when a change does any of these:

- Moves, renames, splits, merges, or deletes a file, function, type, or package cited by anatomy.
- Changes which package owns a behavior, which package calls another, or which folder is the right entry point for a structural question.
- Adds, removes, or changes persistent state: files written, schema versions, manifest fields, signal files, tap formula, migration registry.
- Changes the build pipeline, embed targets (`//go:embed`), subcommand surface (`lingtai-tui purge`/`postman`/`timemachine`/etc.), or HTTP route surface.
- Adds a new migration to either registry — the cross-binary contract is part of anatomy.
- Creates a new package that a competent agent can reason about as a unit.

Usually no anatomy update is required for local implementation fixes, prompt wording changes, constant tweaks, test-only edits, formatting, or comments — unless anatomy cites or describes that exact behavior. When unsure, search for citations of the touched filename and verify them; if the prose still points future agents to the right place, leave it alone.

## Who maintains anatomy

There are two kinds of agent that interact with this convention:

**Coding agents** (Claude Code, Codex CLI, any agent that edits files and creates commits): you MUST update the relevant `ANATOMY.md` files in the **same commit** as the code change. If a commit moves a function, renames a file, splits a package, or changes a connection between folders, the anatomies that cite that code are part of the diff. Do not split anatomy updates into a follow-up commit — drift starts the moment the code change lands without its anatomy update. Git history is the audit trail; anatomy files do not need their own version-history sections.

**LingTai agents** (the Python creatures running inside `.lingtai/`): you generally do NOT modify the lingtai repo directly — you propose patches, the human applies them. Your role with anatomy is **to report drift as issues**. When you read anatomy and notice it disagrees with the code, mail the human, or write a `discussions/<name>-patch.md` proposal naming the specific citation that rotted and the correct line. Do not silently fix anatomy in your own working copy without surfacing the drift — the value of your read-pass is the signal that the drift exists.

## Citation rot during refactors

The most common drift mode is **citation rot after a refactor**. When code moves between files, anatomies that cite the old file rot silently — the prose still reads correctly, but the citations point at a line that no longer exists or contains different code.

The mechanical rule:

> After any commit that touches `git diff --name-only`, search every `ANATOMY.md` for citations of every touched filename and verify each one.

For cheap mechanical checking, scan anatomy citations before commit:

```bash
python3 - <<'PY'
import pathlib, re
root = pathlib.Path(".")
for anatomy in root.rglob("ANATOMY.md"):
    if "node_modules" in anatomy.parts or ".git" in anatomy.parts:
        continue
    text = anatomy.read_text()
    # Match `path/file.go:NNN` or `path/file.go:NNN-MMM`
    for rel, line in re.findall(r"`?([A-Za-z0-9_./-]+\.(?:go|tsx?|jsx?|md|json[c]?)):(\d+)", text):
        path = root / rel
        if not path.exists():
            print(f"{anatomy}: missing citation target {rel}:{line}")
            continue
        n = len(path.read_text().splitlines())
        if int(line) > n:
            print(f"{anatomy}: out-of-range citation {rel}:{line} > {n}")
PY
```

This only catches missing files and out-of-range lines. It does not prove semantic correctness; an agent still has to open the cited code and confirm the claim.

## The repo-root anatomy is just an anatomy

The repo-root `ANATOMY.md` follows the same 6-section template as every other anatomy. It happens to enumerate the two binary trees (`tui/`, `portal/`) and the cross-cutting infra (install.sh, scripts/, examples/, docs/) in its Components and Composition sections — that's a property of being at the top of the tree, not a special role. There are no "doorways" or "entrances": there is the convention (this skill) and there is the tree of anatomies. The repo-root anatomy is the top of the tree. That is all.

## When the convention exposes structural pressure

If a single Go package is large enough to need its own anatomy, that is a refactor signal — not a license to write per-file anatomies. The convention's first useful side effect is that it reveals where a package's organizational grain doesn't match its conceptual grain. The right response is "split into sub-packages or move out concerns" not "invent a parallel doc system that summarizes a too-large file."

The TUI's `tui/internal/tui/` (~22k LOC, every Bubble Tea screen) is a known case: it warrants its own anatomy, but breaking it into sub-packages would fight Bubble Tea's screen-per-file convention. The right move there is a single thorough anatomy for the package, not a refactor.

## Relationship to other skills

- **`lingtai-anatomy`** (the umbrella skill) — describes the LingTai *system* as a user experiences it: TUI flows, presets, init.jsonc, runtime layout under `~/.lingtai-tui/`. If your question is "how does my init.jsonc get there," start there.
- **`lingtai-kernel-anatomy`** — the convention for the kernel's anatomy tree (Python, in the sibling `lingtai-kernel` repo). If your question is "what is X actually doing inside the agent runtime, where does it live in the kernel," start there.
- **`lingtai-tui-anatomy` (this skill)** — the convention for the lingtai Go monorepo's anatomy tree. If your question is "what is the TUI doing, where does it live in the Go code, how does the portal share state with it," read this once to know the convention, then descend the lingtai repo's anatomy tree.

The three skills are layered. The umbrella anatomy tells you about the world the user lives in. The kernel anatomy tells the agent about itself. This skill tells coding agents about the binary that wraps the agent.
