---
name: lingtai-recipe
description: >
  Menu manual (not a tool) for everything recipe-related in LingTai. A
  **recipe** is the named payload that shapes an orchestrator's
  greeting, ongoing behaviour, and shipped library; every LingTai
  project uses one (selected at `/setup` time, inherited from a clone,
  or auto-discovered when a project already has `.recipe/` at its
  root). The skill body fans out into three substantive sub-guides
  (~1300 lines total) so you load only what the task needs:
  `references/recipe-format.md` for the bundle format + `recipe.json`
  schema + library sibling rules + validator contract (read first when
  authoring or customising); `assets/export-network.md` for shipping
  the *current* multi-agent network as a shareable git repo (live
  `.lingtai/` state + freshly-distilled `.recipe/`, per-agent
  `init.json` stripped so the recipient picks their own LLM); and
  `assets/export-recipe.md` for shipping just the methodology /
  culture as a bundle others can use to seed *new* networks (no
  agents, no mailboxes). Body also catalogues the 8 helper scripts
  the export flows invoke (validate, archive_mail, privacy_scan,
  scrub_ephemeral, etc.) and warns about the three different
  recipe-shaped artifacts that can co-exist in one project (inner
  network, outer applied recipe, captured applied snapshot — easy to
  conflate). Read this skill when the human mentions recipes, wants
  to author / customise one, wants to share or back up this network,
  or wants to publish a recipe for seeding new networks. Do NOT use
  for one-off exports of a single agent (that's just `cp -r`), or
  for in-network behaviour edits to the live system (those go through
  the kernel's writes to the agent's working directory, not through a
  recipe round-trip).
version: 3.1.1
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

## Disambiguate scope BEFORE picking a sub-guide

Both export flows talk about "the network" and "the recipe" — but a single project directory can hold up to three recipe-shaped artifacts at once, and they are NOT interchangeable. Identify which of these the human means before going further:

1. **The inner network** — the agents currently living in `.lingtai/` of the project you're invoked in (orchestrator + avatars, their mailboxes, their accumulated state). This is "your network." When the human says "export this network" or "export the recipe of this network," this is almost always what they mean.

2. **The outer project's own `.recipe/`** — a recipe bundle sitting at the project root (sibling of `.lingtai/`), put there because the project was *seeded* from that recipe at `/setup` time. This is the methodology / culture *that produced* the inner network. It is a separate artifact with its own identity, version, and library. **Do not conflate it with the recipe you author for the export.** If asked to "re-export this recipe," check whether the human means this one (just republish the existing bundle) or wants a fresh recipe distilled from the network's current behavior — ask if ambiguous.

3. **The applied-recipe snapshot at `.lingtai/.tui-asset/.recipe/`** — a copy of #2 captured by the TUI when the recipe was applied. Useful as *evidence* of what behavior is currently in force inside the network, but it is not the artifact to ship. The recipe you ship is freshly distilled from how the inner network *actually behaves now*, not a verbatim copy of what was originally applied.

If you find a `.recipe/` at the project root and you're exporting the *network*, treat that outer recipe as project content to inspect during Step 3 (interactive review) — not as the launch recipe to bundle. Step 5 of the network-export flow authors a NEW `.recipe/` for the recipient that introduces the *network*, not the methodology that originally seeded it.

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
    ├── privacy_scan.py              ← network-export: scan for leaked secrets (folds .lingtai breadcrumb noise)
    ├── scan_nested_repos.py         ← network-export: detect inner .git repos
    ├── scrub_ephemeral.py           ← network-export: delete runtime-regen + chat history + intrinsic library
    ├── mark_export_source.py        ← network-export: stamp .exported-from + brief.md banner
    └── generate_readme.py           ← network-export: draft README.md from recipe + agents + library
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
- **Network exports strip chat_history and stamp brief.md (v3.1).** A v3.0 export shipped each agent's full `history/chat_history.jsonl`, so a clone woke up indistinguishable from the original. v3.1 strips the conversation/introspection trace and instead stamps `system/brief.md` with an "EXPORTED SNAPSHOT" banner and writes `.exported-from` at the bundle root — the recipe's `greet.md` carries the network's 「前尘往事」 (charge), and the brief banner reaches the agent on its first turn. `pad.md` and `codex/` are still preserved (durable knowledge); only the verbatim transcript is dropped.
- **Network exports strip per-agent `.library/intrinsic/` (v3.1).** It's kernel-managed, identical across LingTai installs, and the recipient kernel rebuilds it on rehydration — shipping it added hundreds of duplicated `SKILL.md` files. `<agent>/.library/custom/` and `.lingtai/.library_shared/` are still preserved (those are the network's own skills).
- **`recipe.json` is single-canonical, never localized.** Already the rule in v3.0 but the network-export sub-guide previously instructed agents to write `.recipe/zh/recipe.json` etc. — that's a validator error. v3.1 fixes the doc. Localized display strings belong only in `greet.md` / `comment.md` / `covenant.md` / `procedures.md`.

Now go read the relevant sub-file.
