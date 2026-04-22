---
name: lingtai-recipe
description: Everything about launch recipes — their format, how to author and validate them, and how to export either a standalone recipe or the current network (recipe + live .lingtai/ state) as a shareable git repository. A recipe is the named payload that shapes an orchestrator's greeting, ongoing behavior, and shipped skills; every lingtai project uses one. Use when the human asks about recipes, wants to create or customize one, wants to share or back up this network, or wants to export a recipe for others to seed new networks. This skill is a menu — pick the sub-guide that matches the task.
version: 2.0.0
---

# lingtai-recipe: Recipes, Exports, and Everything Around Them

A **launch recipe** is a named payload that shapes an orchestrator's first-contact greeting, its ongoing behavioral constraints, and any skills it ships. Every lingtai project uses a recipe — selected during `/setup`, inherited from a published network via `/agora`, or auto-discovered from `.lingtai-recipe/` inside a cloned repo.

This skill is the one place to look for anything recipe-related. Pick the sub-file that matches what you're doing, then read it in full before acting.

## Choose the sub-guide

- **Understanding / authoring a recipe** — format reference: directory structure, the five components (`greet.md`, `comment.md`, `covenant.md`, `procedures.md`, `skills/`), placeholders, i18n fallback rules, `recipe.json` manifest, validator contract, how to create and test a custom recipe.

  → Read `references/recipe-format.md`.

- **Exporting the full network** — copying the live `.lingtai/` (agents, mailboxes, history) plus a generated recipe into a shareable git repo. Use this when the human wants to share, back up, or republish *this specific network*, including its inhabitants.

  → Read `assets/export-network.md` and follow it end-to-end.

- **Exporting a standalone recipe** — distilling just the culture (greet/comment/covenant/procedures/skills) into a `.lingtai-recipe/` seed others can use to start *new* networks. No agents, no mailboxes.

  → Read `assets/export-recipe.md` and follow it end-to-end.

If the human asks for *both* a full-network export and a standalone recipe export, do the network export first — it already generates a `.lingtai-recipe/` payload as part of Step 5. Then, if a separate recipe-only repo is also wanted, consult `assets/export-recipe.md` afterward. Don't run them twice from scratch.

## Layout of this skill

```
lingtai-recipe/
├── SKILL.md                         ← this menu
├── references/
│   └── recipe-format.md             ← authoritative recipe format reference
├── assets/
│   ├── export-network.md            ← full network-export procedure
│   ├── export-recipe.md             ← standalone recipe-export procedure
│   └── gitignore.template           ← canonical .gitignore used by network export
└── scripts/
    ├── validate_recipe.py           ← invoked by both export flows before git-init
    ├── archive_mail.py              ← network-export: archive + cutoff mail
    ├── filter_archive.py            ← network-export: filter archived mail
    ├── privacy_scan.py              ← network-export: scan for leaked secrets
    ├── scan_nested_repos.py         ← network-export: detect inner .git repos
    └── scrub_ephemeral.py           ← network-export: delete runtime-regen files
```

Installed at `~/.lingtai-tui/utilities/lingtai-recipe/`. Resolve absolute paths from there when invoking scripts.

## Shared ground rules for both export flows

Both export flows share the same filesystem discipline and communication discipline. The sub-guides repeat these in context, but they apply universally:

- **Resolve `$HOME` first** — the `write` tool does not expand `~`. Run `echo $HOME` once and use the resolved absolute path everywhere.
- **Always `mkdir -p` before writing**, and verify after with `find` / `ls` — `write` can silently succeed on missing parents.
- **Talk to the human via `email`**, not text output. This is a multi-round flow with real latency; the human only reliably sees messages in their inbox.
- **Never skip the interactive steps.** Both flows require human judgment at specific points (mail cutoff, network naming, inspecting privacy-scan findings). The whole point of a skill-driven export is human-in-the-loop.

Now go read the relevant sub-file.
