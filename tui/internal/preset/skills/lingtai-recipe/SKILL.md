---
name: lingtai-recipe
description: Everything about recipes — their bundle format, how to author and validate them, and how to export either a standalone recipe or the current network (recipe + live .lingtai/ state, with per-agent init.json stripped) as a shareable git repository. A recipe is the named payload that shapes an orchestrator's greeting, ongoing behavior, and shipped library; every lingtai project uses one. Use when the human asks about recipes, wants to create or customize one, wants to share or back up this network, or wants to export a recipe for others to seed new networks. This skill is a menu — pick the sub-guide that matches the task.
version: 3.0.0
---

# lingtai-recipe: Recipes, Exports, and Everything Around Them

> **Bundle root convention**: The bundle root is the directory that **contains** `.recipe/` at its top level (alongside the library folder). When pointing the TUI or any tool at a recipe, pass **this directory**, not `.recipe/` itself and not a parent of it. For recipes published via `lingtai-recipe` skill, this is `$HOME/lingtai-agora/recipes/<id>/`.

A **recipe bundle** is a directory with three possible siblings at its root:

- `.recipe/` (required) — the LingTai-facing behavioral layer: `recipe.json` manifest, optional `greet/`, `comment/`, `covenant/`, `procedures/` layer dirs with locale variants.
- `<library_name>/` (optional) — a framework-agnostic skill library, named by `recipe.json#library_name`. Drop-in usable by any agent framework that reads `SKILL.md`.
- `.lingtai/` (optional) — a full multi-agent network snapshot, present only when exporting an entire network. Every agent's `init.json` is stripped so the recipient picks their own LLM preset on rehydration.

Every LingTai project uses a recipe — selected during `/setup`, inherited from a cloned network, or auto-discovered when a project already has `.recipe/` at its root.

This skill is the one place to look for anything recipe-related. Pick the sub-file that matches what you're doing, then read it in full before acting.

## Choose the sub-guide

- **Understanding / authoring a recipe** — format reference: bundle directory structure, `recipe.json` schema (`id`, `name`, `description`, `version`, `library_name`), the four optional behavioral layers, locale fallback rules, library sibling mechanics, network snapshot rules, validator contract, how to create and test a custom recipe.

  → Read `references/recipe-format.md`.

- **Exporting the full network** — copying the live `.lingtai/` (agents, mailboxes, history, etc.) plus a generated `.recipe/` bundle into a shareable git repo, with per-agent `init.json` stripped. Use this when the human wants to share, back up, or republish *this specific network*, including its inhabitants.

  → Read `assets/export-network.md` and follow it end-to-end.

- **Exporting a standalone recipe** — distilling just the culture (optional `greet/comment/covenant/procedures`, optional library of skills) into a bundle others can use to start *new* networks. No agents, no mailboxes.

  → Read `assets/export-recipe.md` and follow it end-to-end.

If the human asks for *both* a full-network export and a standalone recipe export, do the network export first — it already authors a `.recipe/` payload as part of Step 5. Then, if a separate recipe-only repo is also wanted, consult `assets/export-recipe.md` afterward. Don't run them twice from scratch.

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

## Key structural rules that differ from older skills

If you have memory of an older version of this skill, these are the things that changed. When in doubt, the validator (`scripts/validate_recipe.py`) is the source of truth.

- **Recipe bundles now have three siblings, not one.** Old format: recipe files all lived under `.lingtai-recipe/` at the repo root. New format: `.recipe/` dotfolder holds only LingTai-facing behavioral layers; libraries live at a sibling folder named by `recipe.json#library_name`; full network snapshots live at a sibling `.lingtai/`.
- **`recipe.json` moved into `.recipe/`.** Old location: `<repo-root>/recipe.json`. New location: `<bundle-root>/.recipe/recipe.json`. Schema also grew — see the format reference.
- **All four behavioral layers are optional.** Old format: `greet.md` and `comment.md` were required. New format: every layer is optional. Absent greet → silent agent. Absent comment → no comment file in init.json. Absent covenant / procedures → kernel defaults.
- **Library is a sibling, not inside `.recipe/`.** Old format: skills lived at `.lingtai-recipe/skills/<name>/SKILL.md`. New format: skills live at `<bundle>/<library_name>/<skill>/SKILL.md` — the library is a separate sibling folder, and the recipe declares its name via `recipe.json#library_name`. This makes libraries drop-in-usable by non-LingTai agent frameworks.
- **Library skills are monolingual.** No more `SKILL-en.md` / `SKILL-zh.md` variants. One `SKILL.md` per skill.
- **Network exports strip per-agent `init.json`.** The recipient picks their own LLM preset during rehydration, not the publisher's. The validator enforces this: if `.lingtai/<agent>/init.json` is present in a bundle, the validator errors.
- **Layer directories have their own fallback structure.** Old: `.lingtai-recipe/<lang>/greet.md`. New: `.recipe/greet/<lang>/greet.md` (layer-then-lang, with `<layer>.md` at the root of the layer dir as the default).

Now go read the relevant sub-file.
