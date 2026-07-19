---
name: recipe-export-flow
description: >
  Nested lingtai-recipe reference for exporting a standalone recipe bundle from
  a live network or methodology: scope disambiguation, human-in-the-loop metadata
  collection, bundle authoring, validation, git initialization, and handoff.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Exporting a Recipe

Nested lingtai-recipe reference. Read this after the top-level router sends you here.

*This is the recipe-export sub-guide of the `lingtai-recipe` skill. For an overview of all recipe-related flows, read `../../SKILL.md`.*

**Prerequisites:** Read `../recipe-format/SKILL.md` first — it defines the bundle shape, `recipe.json` schema, the four behavioral layers (all optional), the library sibling, and how the validator enforces all of it. This sub-guide assumes you understand all of that.

A recipe is the culture of a network, distilled into a portable seed. Your job is to help the human reflect on their network's culture and package the parts worth sharing. An exported recipe is a **bundle directory** — a git repo the recipient clones and points `/setup` at.

## First: which "recipe" does the human mean?

`../../SKILL.md` ("Disambiguate scope BEFORE picking a sub-guide") defines the three recipe-shaped artifacts a project can hold at once. Confirm scope with the human before authoring anything, then apply the export-side rule for each:

1. **A recipe distilled from the inner network** (the agents in `.lingtai/`) — the default, and what this sub-guide assumes. You write a NEW `.recipe/` capturing what makes this network distinctive.

2. **The outer project's own `.recipe/`** at the project root (sibling of `.lingtai/`). If the human says "re-export the recipe," they may mean republish this existing bundle, NOT write a new one. Ask. Republishing is mostly a copy job (copy `<project>/.recipe/` and the matching `<project>/<library_name>/` sibling into `$HOME/lingtai-agora/recipes/<id>/`, then jump to Step 4 to validate). Distilling a fresh recipe is what the rest of this sub-guide covers.

3. **The applied-recipe snapshot at `.lingtai/.tui-asset/.recipe/`** — a TUI-managed copy of #2, useful as *evidence* of what behavior is currently in force. Not a separate artifact to ship: if the human wants what's in there, they almost certainly mean #2.

The most common failure mode in a project that was itself seeded from a recipe (e.g., a methodology recipe applied to produce a domain network): the agent confuses #2 with #1 and ends up republishing the seeding methodology instead of distilling the network's actual culture. If the project has both a `.lingtai/` *and* its own `.recipe/` at the root, ASK the human which scope they want before proceeding.

## What is an exported recipe bundle?

A recipe bundle is the shareable artifact the TUI copies into a project on selection. Minimum shape:

```
<bundle-root>/                          # the repo root the recipient clones
└── .recipe/
    └── recipe.json                     # manifest (id, name, description, …)
```

Everything else is optional, and `../recipe-format/SKILL.md` ("Bundle Directory Structure") is the authoritative layout. Typical bundles add `greet/greet.md` (first-contact) and `comment/comment.md` (ongoing behavioral constraints), their `<lang>/` variants, occasionally `covenant/covenant.md` or `procedures/procedures.md`, and a `<library_name>/` sibling of framework-agnostic skills. Export adds one file the format reference does not: an optional `README.md` at the bundle root, for GitHub display.

A recipe ships no `.lingtai/` snapshot — just the seed. The garden grows fresh in each new project that applies the recipe.

## How to talk to the human during this skill

**Use the `email` tool for every message to the human. Never rely on text output.**

This is a multi-round conversation with real latency between turns. The human may not be watching their terminal — they will see your messages reliably only through their inbox. Every question, status update, and confirmation goes through `email(action="send", address="human", ...)`.

## Critical: Filesystem Rules

These rules prevent silent failures. Follow them without exception.

1. **Resolve `$HOME` first.** The `write` tool does NOT expand `~`. At the start of this skill, run:
   ```bash
   echo $HOME
   ```
   Use the result (e.g., `/Users/alice`) as the prefix for ALL file paths. Never use `~` in a `write` or `file` tool call.

2. **Always use absolute paths.** Every `write` call must use a full absolute path.

3. **Always `mkdir -p` before writing.** The `write` tool may silently fail or report false success if the parent directory does not exist.

4. **Verify after writing; never trust a write success message at face value.** After writing all files in a step, run `find <bundle-dir> -type f | sort` (or `ls`) and confirm the output lists every file you intended to create.

## Step 0: Resolve Paths + Reflect on the Network

**0a. Resolve the bundle base directory.**

```bash
echo $HOME
```

Store the result. All paths use `$HOME/lingtai-agora/recipes/` as the base. **Note: `lingtai-agora`, NOT `.lingtai-agora` — no leading dot.** The agora directory is a user-visible workspace, not a hidden config directory.

**0b. Read `../recipe-format/SKILL.md`** to refresh your understanding of the bundle shape.

**0c. Reflect on the living network.** Before asking the human anything, examine the network to understand its culture. **You are reflecting on the inner network (the agents in `.lingtai/`), not on whatever recipe was originally applied to seed it.** The applied recipe is one input among several — the network's *current* behavior may have drifted, grown new skills, or specialized in ways the seeding recipe never described. Distill what the network is now, not what it was told to be.

1. Read the applied-recipe state for context only: `ls -la .lingtai/.tui-asset/` and look at `.recipe/` (directory — the snapshot of the currently-applied recipe) or `.recipe` (JSON file — UI picker state). The directory snapshot is what the agents were initially shaped by; it tells you the starting point, not the destination. If the project also has its own `.recipe/` at the project root, that's the same content — see "First: which 'recipe' does the human mean?" above.
2. Read the orchestrator's comment from `.lingtai/.tui-asset/.recipe/comment/comment.md` (or the locale variant) — that's the behavioral DNA the current network is running under. Treat it as a *baseline*; what you ship in Step 2c will refine, extend, or partially replace it based on how the network actually evolved.
3. List libraries currently registered: `cat .lingtai/<orchestrator>/init.json | python3 -c "import json,sys; m=json.load(sys.stdin); print(m['manifest']['capabilities']['skills']['paths'])"`
4. Scan the network structure: `ls .lingtai/*/.agent.json 2>/dev/null | head` and read a couple to see agent names and roles.
5. Skim recent mail for tone and delegation patterns: `ls -t .lingtai/<orchestrator>/mailbox/sent/ | head -20`. The actual delegation, the actual tone, the actual workflow — this trumps anything the seeding recipe says.

Build a mental model of: what does this network *do* now? How does it *behave* now? What *skills* has it grown that the seeding recipe didn't ship? What makes it distinctive *as it stands*, not as it was originally configured?

## Step 1: Collect Metadata from the Human

Send the human **one** email introducing the export flow and collecting all key decisions upfront. This reduces round-trips.

> "I've looked at your network and here's what I see as its culture:
>
> [2-3 sentence summary of the network's identity, style, and capabilities]
>
> A recipe distills this into something portable. To get started, I need a few things:
>
> 1. **Recipe id** — a short machine identifier, kebab-case (e.g., `scholar-distiller`, `greeter-for-physics-postdocs`). Stable across locales.
> 2. **Display name** — human-readable name shown in the TUI picker.
> 3. **One-line description** — what does this recipe give someone?
> 4. **Which layers to include?** All four are optional:
>    - greet.md — first-contact message (leave off for a silent agent)
>    - comment.md — ongoing behavioral constraints
>    - covenant.md — override the system covenant (rare; skip unless fundamentally different)
>    - procedures.md — override lifecycle procedures (very rare)
> 5. **Library?** If you want to ship skills as a library sibling, give me a `library_name` (kebab-case folder name) and a list of skills to include.
> 6. **Languages** — just English (en), or also zh/wen?
>
> Here are the candidate skills I found:
> [list skills from <project>/<library_name>/ if the project already uses a library, or recent custom skills across agents]
>
> Answer as many as you can in one message and I'll draft everything in one pass."

If `$HOME/lingtai-agora/recipes/<id>/` already exists, ask before overwriting.

## Step 2: Author the Bundle

Once you have the human's input, author all files in one pass. You WRITE the content (not copy) — the recipe should be a distillation, not a raw dump of existing files. Refer to `../recipe-format/SKILL.md` for the exact format and rules of each component.

### Pre-flight: Create all directories first

```bash
BUNDLE="$HOME/lingtai-agora/recipes/<id>"
mkdir -p "$BUNDLE/.recipe"
# Only create the layer dirs the human wants — don't make empty dirs,
# the validator rejects them.
mkdir -p "$BUNDLE/.recipe/greet"       # if shipping greet
mkdir -p "$BUNDLE/.recipe/comment"     # if shipping comment
# Optional locale subdirs, if going multilingual:
mkdir -p "$BUNDLE/.recipe/greet/zh"
mkdir -p "$BUNDLE/.recipe/greet/wen"
# Optional library sibling:
mkdir -p "$BUNDLE/<library_name>"
```

### 2a. `recipe.json` (manifest)

Write `$BUNDLE/.recipe/recipe.json`. Field semantics, requiredness, and defaults live in `../recipe-format/SKILL.md` ("`recipe.json` — Manifest"); for an export, write `id` in kebab-case and keep `name` / `description` in one canonical language. Example:

```json
{
  "id": "scholar-distiller",
  "version": "1.0.0",
  "name": "Scholar Distiller",
  "description": "Distills an academic's public work into a queryable persona skill.",
  "library_name": "scholar-kit"
}
```

Do **not** write locale variants of `recipe.json` — there is exactly one manifest, `$BUNDLE/.recipe/recipe.json`, and the validator rejects `.recipe/<lang>/recipe.json` because the runtime ignores those files and they can silently drop load-bearing fields like `library_name`. If the recipe is multilingual, localize the human-facing layers instead (`greet/<lang>/greet.md`, `comment/<lang>/comment.md`, etc.).

### 2b. `greet.md` — First Contact (optional)

Write `$BUNDLE/.recipe/greet/greet.md` (and `$BUNDLE/.recipe/greet/zh/greet.md` if multilingual). Follow the rules and placeholder list in `../recipe-format/SKILL.md`. Write fresh recipe-specific content — do NOT copy templates or include `[system]` prefixes.

Skip this layer entirely if the recipe wants a silent agent (agent waits for the first human message instead of sending a greeting).

### 2c. `comment.md` — Behavioral DNA (optional)

Write `$BUNDLE/.recipe/comment/comment.md`. This is where the network's culture gets distilled. **Draw from the living network** — look at how the orchestrator actually behaves and distill that into portable instructions. See `../recipe-format/SKILL.md` for the format rules (no placeholders, static text, injected every turn).

**What to distill.** Walk through each of these areas and extract what's worth keeping:

- **Delegation and avatar rules** — how does the orchestrator decide when to spawn avatars vs handle things itself? What avatar blueprints does it use? If there are specific naming conventions, specialization patterns, or spawn-on-demand rules, capture them.
- **Communication norms** — does the network enforce deposit-before-email (write findings to a file before sending a summary)? Are there conventions about email length, format, or frequency between agents?
- **Workflow patterns** — is there a specific order of operations? Does the orchestrator follow a pipeline (research → draft → review → publish)? Are there quality gates or checkpoints?
- **Tool usage conventions** — any rules about which tools to prefer, when to use bash vs file tools, when to use web search? Any cost-awareness rules?
- **Tone and style** — formal vs casual? Terse vs detailed? Does the orchestrator have a persona or voice?
- **Guardrails** — what does the orchestrator explicitly avoid? Topics it won't engage with? Actions it won't take without human approval?
- **Skill references** — if the recipe ships a library, how and when should the orchestrator invoke which skill?

**Where to look:** the sources you already read in Step 0c — the currently-applied comment at `.lingtai/.tui-asset/.recipe/comment/comment.md` (what's already codified), the orchestrator's recent mail (how it actually delegates and responds), and avatar `.agent.json` blueprints (what specialized agents exist and why) — plus the covenant and procedures for custom overrides already in place, and the human's feedback patterns: what corrections has the human made repeatedly?

**Distillation technique:** For each behavioral norm you observe (e.g., "agents always deposit findings before emailing"), write it as an explicit rule (e.g., "Always write your findings to a file before sending an email summary"). Transform living behavior → explicit rule → readable prose.

### 2d. Library sibling (optional)

If the recipe ships a library, copy the skills you want to include into `$BUNDLE/<library_name>/`. **Every skill must live in its own subdirectory** — `SKILL.md` at the library-folder root is ignored by the scanner.

```bash
# Copy from the project's own library (if the project already uses one):
cp -R $PROJECT/<library_name>/<skill-a> $BUNDLE/<library_name>/<skill-a>

# Or from a specific agent's custom library:
cp -R .lingtai/<agent>/.library/custom/<skill-a> $BUNDLE/<library_name>/<skill-a>

# Or from the network-shared library:
cp -R .lingtai/.library_shared/<skill-a> $BUNDLE/<library_name>/<skill-a>
```

Each skill directory must contain a valid `SKILL.md` (the validator does not walk skills deeply; it just warns if the library contains zero `SKILL.md` files anywhere).

**Single-skill libraries**: if you're shipping exactly one skill and the library name matches the skill name, still nest it — `$BUNDLE/<library_name>/<library_name>/SKILL.md`, the same name one level deeper, which IS intentional. Do NOT flatten this to `$BUNDLE/<library_name>/SKILL.md` — the scanner only registers skills that are direct-child subdirectories of the library folder. A flat layout silently produces zero registered skills. (This mistake was observed on real bundles during export; prefer the nested layout even when it feels redundant.)

**Skills are monolingual.** Libraries do not have `<lang>/SKILL.md` variants — write skills in one language. If you want a bilingual skill, write both languages in the same `SKILL.md`.

**Skills must be self-contained.** Each skill directory should work independently when dropped into any agent framework. Check that scripts don't reference absolute paths or project-specific resources.

### 2e–2f. `covenant.md` / `procedures.md` (optional, usually skip)

Only create these if the network's principles or procedures fundamentally differ from the system default. Most recipes don't need them. See `../recipe-format/SKILL.md` for details.

```
$BUNDLE/.recipe/covenant/covenant.md
$BUNDLE/.recipe/procedures/procedures.md
```

Locale variants follow the same pattern: `$BUNDLE/.recipe/covenant/<lang>/covenant.md`.

### Post-write verification (MANDATORY)

```bash
find $HOME/lingtai-agora/recipes/<id>/ -type f | sort
```

**Check the output against your intended file list.** Confirm that:
- `.recipe/recipe.json` is present at the bundle root
- Each layer directory you created contains at least one `.md` file (empty layer dirs are a validator error)
- No stray files at `.recipe/` root (only `recipe.json` is expected there)
- The library folder (if any) is a sibling of `.recipe/`, not inside it

If any file is missing, re-create its parent directory and re-write it. **Do not proceed until all files are confirmed on disk.**

## Step 3: Review with the Human

Show the human the `find` output and read back each file's content via email. Iterate until the human approves.

## Step 4: Validate the bundle

The validator ships with the TUI at a stable per-user path, so you can run it from anywhere.

```bash
TOOL_DIR=scripts
VALIDATOR="$HOME/.lingtai-tui/utilities/lingtai-recipe/$TOOL_DIR/validate_recipe.py"
python3 "$VALIDATOR" "$HOME/lingtai-agora/recipes/<id>/"
```

This is the canonical structural check; `../recipe-format/SKILL.md` ("What the validator checks") enumerates every check it runs. Exit code 0 means the bundle is structurally valid.

**If the script reports errors:** stop, read the error lines, fix each one in the bundle directory, and re-run. Loop until clean.

**Warnings** (unknown locale code, stray file at `.recipe/` root, empty library) are reported but do not block. Show them to the human and let them decide whether to address.

## Step 5: Sensitivity sweep

Before committing and pushing, review every file that will be committed for content the human may not want to publish. This catches leaks that automated scans cannot.

**Scope.** Every file in `$HOME/lingtai-agora/recipes/<id>/` will be committed — recipes do not have a `.gitignore` to filter, so the scope is the whole staging directory. Typical contents: `.recipe/recipe.json`, everything inside `.recipe/<layer>/`, the library folder if any, an optional `README.md`, and any other files you created during authoring. Sweep all of them.

**What to look for:**
- Real names of private individuals — the human, collaborators, children, coworkers
- Internal or unreleased org, project, or product names
- Financial details, salaries, legal matters, health information
- Unpublished ideas the human has not committed to making public
- Embarrassing or off-hand remarks preserved in source material you drew from
- Third-party content — pasted emails, screenshots of private channels

**How to report.** Send one email to the human listing every concern in the form `<file>:<line-or-section> — <concern>` with a recommendation (redact / keep / replace with placeholder). Do not paginate across multiple emails unless the list is very long — one message is easier for the human to scan and reply to.

**Loop.** After the human decides each item, apply redactions (edit the bundle files in place), then:
- Re-run `validate_recipe.py` (Step 4) in case a redaction broke the payload shape
- Re-run this sensitivity sweep if the redactions were substantial enough that more concerns might surface

Only proceed to Step 6 once the human says "ship it."

## Step 6: `git init` + commit

```bash
cd $HOME/lingtai-agora/recipes/<id>/
git init -b main
git add .
git status
```

Show `git status` to the human. Get confirmation. Then: `git commit -m "Recipe: <id>"`

## Step 7: Push to GitHub (optional)

Check `gh auth status` and follow the three-branch pattern:

- **Branch A (gh ready):** Ask if they want to push, confirm repo name and visibility, run `gh repo create`
- **Branch B (gh installed but not authenticated):** Guide through `gh auth login`
- **Branch C (gh not installed):** Offer install instructions

## Things to Watch Out For

**Don't copy blindly.** The recipe should be authored, not dumped. A raw copy of the current `comment.md` might reference project-specific agents, paths, or context that won't exist in the recipient's network.

**The bundle IS the project-to-be.** When the recipient runs `/setup` and picks this bundle, the TUI copies the whole bundle into their project root. The bundle's `.recipe/` becomes their `.recipe/`; the library folder becomes a sibling at their project root. So treat the bundle structure as the final on-disk structure — don't assume the recipient will move files around.

**The recipe is a seed, not a clone.** It shapes behavior — it does NOT reproduce the network's state, history, or data. Recipients grow their own network from the seed; their agents are born fresh, shaped by your recipe but living their own life.

**Intrinsic skills don't need copying.** Skills under `.library/intrinsic/` are shipped with the TUI itself and already available in every installation. Only ship custom skills — the ones that grew out of this network.

**Libraries are additive on recipe change.** Applying your recipe appends `"../../<library_name>"` to the recipient's agents' `skills.paths` without removing whatever a previous recipe registered (`../recipe-format/SKILL.md`, "Library path is additive across recipe changes"). Keep this in mind when naming — avoid names that could collide with common libraries the recipient might already have.
