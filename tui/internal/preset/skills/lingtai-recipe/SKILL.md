---
name: lingtai-recipe
description: >
  Menu manual (not a tool) for everything recipe-related in LingTai. A
  **recipe** is the named payload that shapes an orchestrator's
  greeting, ongoing behaviour, and shipped library; every LingTai
  project uses one (selected at `/setup` time, inherited from a clone,
  or auto-discovered when a project already has `.recipe/` at its
  root). The skill body fans out into two substantive sub-guides so
  you load only what the task needs: `reference/recipe-format.md` for
  the bundle format + `recipe.json` schema + library sibling rules +
  validator contract (read first when authoring or customising); and
  `assets/export-recipe.md` for shipping the methodology / culture as
  a bundle others can use to seed *new* networks (no agents, no
  mailboxes). Body also warns about the three different
  recipe-shaped artifacts that can co-exist in one project (inner
  network, outer applied recipe, captured applied snapshot — easy to
  conflate). Read this skill when the human mentions recipes, wants
  to author / customise one, or wants to publish a recipe for seeding
  new networks. Do NOT use for one-off exports of a single agent
  (that's just `cp -r`), or for in-network behaviour edits to the
  live system (those go through the kernel's writes to the agent's
  working directory, not through a recipe round-trip).
version: 3.2.0
---

# lingtai-recipe: Recipes and How to Publish Them

> **Bundle root convention**: The bundle root is the directory that **contains** `.recipe/` at its top level (alongside the library folder). When pointing the TUI or any tool at a recipe, pass **this directory**, not `.recipe/` itself and not a parent of it. For recipes published via `lingtai-recipe` skill, this is `$HOME/lingtai-agora/recipes/<id>/`.

A **recipe bundle** is a directory with two possible siblings at its root:

- `.recipe/` (required) — the LingTai-facing behavioral layer: `recipe.json` manifest, optional `greet/`, `comment/`, `covenant/`, `procedures/` layer dirs with locale variants.
- `<library_name>/` (optional) — a framework-agnostic skill library, named by `recipe.json#library_name`. Drop-in usable by any agent framework that reads `SKILL.md`.

Every LingTai project uses a recipe — selected during `/setup`, inherited from a cloned network, or auto-discovered when a project already has `.recipe/` at its root.

This skill is the one place to look for anything recipe-related. Pick the sub-file that matches what you're doing, then read it in full before acting.

## Choose the sub-guide

- **Understanding / authoring a recipe** — format reference: bundle directory structure, `recipe.json` schema (`id`, `name`, `description`, `version`, `library_name`), the four optional behavioral layers, locale fallback rules, library sibling mechanics, validator contract, how to create and test a custom recipe.

  → Read `reference/recipe-format.md`.

- **Exporting a standalone recipe** — distilling just the culture (optional `greet/comment/covenant/procedures`, optional library of skills) into a bundle others can use to start *new* networks. No agents, no mailboxes.

  → Read `assets/export-recipe.md` and follow it end-to-end.

## Disambiguate scope BEFORE picking a sub-guide

A single project directory can hold up to three recipe-shaped artifacts at once, and they are NOT interchangeable. Identify which the human means before going further:

1. **The inner network** — the agents currently living in `.lingtai/` of the project you're invoked in (orchestrator + avatars, their mailboxes, their accumulated state). This is "your network." When the human says "export the recipe of this network," they want you to distil this network's *behaviour* into a fresh recipe — not ship the network itself.

2. **The outer project's own `.recipe/`** — a recipe bundle sitting at the project root (sibling of `.lingtai/`), put there because the project was *seeded* from that recipe at `/setup` time. This is the methodology / culture *that produced* the inner network. It is a separate artifact with its own identity, version, and library. **Do not conflate it with the recipe you author for the export.** If asked to "re-export this recipe," check whether the human means this one (just republish the existing bundle) or wants a fresh recipe distilled from the network's current behavior — ask if ambiguous.

3. **The applied-recipe snapshot at `.lingtai/.tui-asset/.recipe/`** — a copy of #2 captured by the TUI when the recipe was applied. Useful as *evidence* of what behavior is currently in force inside the network, but it is not the artifact to ship. The recipe you ship is freshly distilled from how the inner network *actually behaves now*, not a verbatim copy of what was originally applied.

## Layout of this skill

```
lingtai-recipe/
├── SKILL.md                         ← this menu
├── reference/
│   └── recipe-format.md             ← authoritative recipe format reference
├── assets/
│   ├── export-recipe.md             ← standalone recipe-export procedure
│   └── gitignore.template           ← canonical .gitignore for exported recipes
└── scripts/
    └── validate_recipe.py           ← invoked by the export flow before git-init
```

Installed at `~/.lingtai-tui/utilities/lingtai-recipe/`. Resolve absolute paths from there when invoking scripts.

## Ground rules for the export flow

- **Resolve `$HOME` first** — the `write` tool does not expand `~`. Run `echo $HOME` once and use the resolved absolute path everywhere.
- **Always `mkdir -p` before writing**, and verify after with `find` / `ls` — `write` can silently succeed on missing parents.
- **Talk to the human via `email`**, not text output. This is a multi-round flow with real latency; the human only reliably sees messages in their inbox.
- **Never skip the interactive steps.** The flow requires human judgment at specific points (recipe naming, inspecting validator findings). The whole point of a skill-driven export is human-in-the-loop.

## Key structural rules that differ from older skills

If you have memory of an older version of this skill, these are the things that changed. When in doubt, the validator (`scripts/validate_recipe.py`) is the source of truth.

- **Recipe bundles now have two siblings, not one.** Old format: recipe files all lived under `.lingtai-recipe/` at the repo root. New format: `.recipe/` dotfolder holds only LingTai-facing behavioral layers; libraries live at a sibling folder named by `recipe.json#library_name`.
- **`recipe.json` moved into `.recipe/`.** Old location: `<repo-root>/recipe.json`. New location: `<bundle-root>/.recipe/recipe.json`. Schema also grew — see the format reference.
- **All four behavioral layers are optional.** Old format: `greet.md` and `comment.md` were required. New format: every layer is optional. Absent greet → silent agent. Absent comment → no comment file in init.json. Absent covenant / procedures → kernel defaults.
- **Library is a sibling, not inside `.recipe/`.** Old format: skills lived at `.lingtai-recipe/skills/<name>/SKILL.md`. New format: skills live at `<bundle>/<library_name>/<skill>/SKILL.md` — the library is a separate sibling folder, and the recipe declares its name via `recipe.json#library_name`. This makes libraries drop-in-usable by non-LingTai agent frameworks.
- **Library skills are monolingual.** No more `SKILL-en.md` / `SKILL-zh.md` variants. One `SKILL.md` per skill.
- **Layer directories have their own fallback structure.** Old: `.lingtai-recipe/<lang>/greet.md`. New: `.recipe/greet/<lang>/greet.md` (layer-then-lang, with `<layer>.md` at the root of the layer dir as the default).
- **`recipe.json` is single-canonical, never localized.** Localized display strings belong only in `greet.md` / `comment.md` / `covenant.md` / `procedures.md`.
- **Network exports are gone (v3.2).** Earlier versions of this skill described an `export-network` flow that shipped the live `.lingtai/` snapshot alongside the recipe. That flow has been retired — `/export` now means recipe-only. Recipes are the seed; the garden is grown fresh in each new project.

Now go read the relevant sub-file.
