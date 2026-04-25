# Recipe Format Reference

*This is the authoring reference of the `lingtai-recipe` skill. For overview of all recipe-related flows, read `../SKILL.md`. For the network-export flow, read `../assets/export-network.md`. For the standalone-recipe export flow, read `../assets/export-recipe.md`.*

A **recipe bundle** is a directory that ships three kinds of content, side-by-side:

1. A `.recipe/` dotfolder containing the LingTai-facing behavioral layer (greet, comment, covenant, procedures) and the manifest (`recipe.json`).
2. An optional library folder (named by `recipe.json#library_name`) containing framework-agnostic skills — drop-in for any agent framework, not just LingTai.
3. An optional `.lingtai/` folder containing a full multi-agent network snapshot (only present when exporting an entire network, not just a recipe).

The bundle is the shareable artifact. When the TUI applies a recipe, it copies the bundle into the project root; the project then becomes self-contained — every path reference in `init.json` resolves within the project directory.

## Bundle Directory Structure

```
my-recipe-bundle/
├── .recipe/                             # required — LingTai behavioral layer
│   ├── recipe.json                      # required — manifest (id, name, description, library_name, …)
│   │                                    #   SINGLE CANONICAL FILE. NEVER localized.
│   │                                    #   Locale variants of recipe.json are FORBIDDEN —
│   │                                    #   they silently drop critical fields like library_name.
│   ├── greet/                           # optional — first-contact message
│   │   ├── greet.md                     #   default version
│   │   ├── en/greet.md                  #   optional locale variants
│   │   ├── zh/greet.md
│   │   └── wen/greet.md
│   ├── comment/                         # optional — system-prompt constraints
│   │   ├── comment.md
│   │   └── <lang>/comment.md
│   ├── covenant/                        # optional — covenant override
│   │   ├── covenant.md
│   │   └── <lang>/covenant.md
│   └── procedures/                      # optional — procedures override
│       ├── procedures.md
│       └── <lang>/procedures.md
├── <library_name>/                      # optional — framework-agnostic skills
│   ├── <skill-a>/
│   │   └── SKILL.md
│   └── <skill-b>/
│       ├── SKILL.md
│       ├── scripts/
│       └── references/
└── .lingtai/                            # optional — network snapshot (export-network only)
    ├── <agent>/
    │   ├── .agent.json                  # identity blueprint (KEPT)
    │   ├── system/*.md                  # KEPT
    │   ├── codex/codex.json             # KEPT
    │   ├── pad.md                       # KEPT
    │   ├── history/chat_history.jsonl   # KEPT
    │   ├── mailbox/                     # KEPT (sanitized by export)
    │   └── (NO init.json)               # STRIPPED — recipient picks their own provider
    └── .tui-asset/.recipe/              # snapshot of applied recipe (KEPT)
```

Only `.recipe/recipe.json` is strictly required. Everything else is optional.

## `recipe.json` — Manifest

Every bundle must contain `<bundle>/.recipe/recipe.json`:

```json
{
  "id": "my-recipe",
  "version": "1.0.0",
  "name": "My Recipe",
  "description": "One-line description of what this recipe does",
  "library_name": null
}
```

### Fields

| Field | Required | Type | Description |
|---|---|---|---|
| `id` | ✅ | string | Machine identifier, stable across locales. Usually matches the bundle directory name. Used for dedup and cross-reference. |
| `name` | ✅ | string | Display name. Shown in the TUI recipe picker. |
| `description` | ✅ | string | One-line description. Shown as hint text in the picker. |
| `version` | ❌ | string | Semver-ish (e.g. `"1.0.0"`). Defaults to `"1.0.0"` when absent. Recipe authors bump this when iterating. |
| `library_name` | ❌ | string \| null | Name of the sibling library folder inside the bundle (e.g. `"velli"`). Must be a simple folder name, no slashes. `null` or absent means the recipe ships no library. When non-null, the TUI registers the library into each agent's `init.json#library.paths` via `"../../<library_name>"`. |

### ⚠️ recipe.json is NEVER localized

**There is exactly one `recipe.json` per bundle, at `.recipe/recipe.json`. No `<lang>/recipe.json` files. Ever.**

Why: `recipe.json` carries **machine identity** — `id`, `version`, and especially `library_name`. These are not display strings; they're load-bearing fields the recipe-apply step reads to wire up `init.json#library.paths` and locate the library folder on disk.

Splitting `recipe.json` by locale is a footgun: if the active locale's variant lacks `library_name` (a typical mistake — a translator localizes `name` and `description` but doesn't realize `library_name` must be carried over too), the TUI silently fails to register the library. Recipe-apply runs without error, but the agent boots without the recipe's skills. This is a hard-to-diagnose class of bug — symptom is "library doesn't load" with no log entry.

**The runtime ignores any locale-variant `recipe.json`.** Only `.recipe/recipe.json` is read. The validator (`validate_recipe.py`) errors on any `<lang>/recipe.json` it finds, so they get caught at export time.

**If you want localized display strings:** put them in `greet.md` and/or `comment.md`. Those are the user-facing surfaces; locale variants there are legitimate and supported. `name` and `description` in `recipe.json` are picker hint text — keep them in one canonical language (English or whatever fits your audience), and let the actual greet/comment carry the localized voice.

```
.recipe/
├── recipe.json            # ✅ THE ONLY recipe.json
├── greet/
│   ├── greet.md           # default
│   ├── zh/greet.md        # ✅ locale variants OK here
│   └── wen/greet.md       # ✅
└── comment/
    ├── comment.md
    └── wen/comment.md     # ✅
```

```
.recipe/
├── recipe.json
├── zh/recipe.json         # ❌ FORBIDDEN — validator errors
└── wen/recipe.json        # ❌ FORBIDDEN — validator errors
```

Extra fields in `recipe.json` are ignored — forward-compatible.

## The Four Behavioral Layers

All four layers (`greet`, `comment`, `covenant`, `procedures`) are **optional**. A recipe can ship any subset of them, or none at all.

Each layer lives under its own directory inside `.recipe/`:

```
.recipe/<layer>/
├── <layer>.md             # default content
├── en/<layer>.md          # optional locale variant
├── zh/<layer>.md
└── wen/<layer>.md
```

The TUI resolves each layer per active locale with the same fallback rule:

1. Try `.recipe/<layer>/<lang>/<layer>.md`
2. Fall back to `.recipe/<layer>/<layer>.md`
3. If neither exists, the layer is absent — the fallback is per-layer (see below).

### 1. `greet.md` — First Contact

**Audience:** the human. **Voice:** the orchestrator (first person).
**This is what the agent SAYS, not what the agent IS TOLD.**

When the network first wakes, the kernel writes `greet.md` into the orchestrator's `.prompt` file. The agent then reads that prompt and emits its content as its first message to the human's inbox. So whatever you write here ends up displayed to the user as if the agent typed it.

**Purpose:** the agent's opening line(s) to the human. Set tone, identify yourself, tell the user what to do next.

**Placeholders** (only `greet.md` may use them; substituted at recipe-apply time):

| Placeholder | Value |
|---|---|
| `{{time}}` | Current date and time (`2006-01-02 15:04`) |
| `{{addr}}` | Human's email address in the network |
| `{{lang}}` | Language code (`en`, `zh`, `wen`) |
| `{{location}}` | Human's geographic location (City, Region, Country) |
| `{{soul_delay}}` | Soul cycle interval in seconds |
| `{{commands}}` | Bulleted list of available slash commands with their i18n descriptions |

**When absent:** no `.prompt` file is written. The agent starts silently and waits for the first human message.

**Rules:**
- Keep it short (5–10 sentences max).
- Be proactive — introduce yourself, don't wait to be asked.
- Don't open with `[system]` — that's a system-message marker, and greet.md is the orchestrator's voice. The validator warns if it sees one.
- Don't write a stage script here. If the agent needs ongoing instructions for a complex task, those go in `comment.md`. `greet.md` is just the hello.

**Common mistake:** treating `greet.md` as a briefing FOR the agent. It is not. The agent receives it as a prompt and *outputs* its content. If you write "you are X, do Y, then Z" in greet.md, the agent will dutifully repeat that sentence to the human. Briefings belong in `comment.md`.

**Good greet.md (concrete, audience-facing):**
```
Hi! I'm the orchestrator for this network. The {{commands}}
slash commands let you interact with me and my avatars.
What would you like to work on?
```

**Bad greet.md (instructions to self leaking out):**
```
You are the main orchestrator. First, load the demo skill,
then begin the show. Wait for the human to say "go" before
proceeding to act 1.
```
The bad version belongs in `comment.md`.

### 2. `comment.md` — Ongoing Behavioral Constraints

**Audience:** the agent (every turn). **Voice:** instructions/briefing addressed to the agent.
**This is what the agent IS TOLD, not what the agent SAYS.**

Injected into the orchestrator's system prompt on every turn. The persistent playbook the agent reads silently before deciding what to do.

**Purpose:** define WHO the agent is in this recipe context, WHAT they're supposed to do, HOW to behave. A recipe-specific extension of the covenant.

**When absent:** no comment file is referenced in `init.json`. The agent runs with just its covenant + procedures + recipe greet.

**Rules:**
- **No placeholders** — this is static text, validated.
- Keep it focused — every token counts because it's on every turn.
- Reference skills by name if the recipe ships a library, and tell the agent to **invoke** them (e.g. `library invoke <name>`), not just that they exist.
- Use second-person address ("You are X. You should do Y.") — this is a system-prompt-like instruction, not narration.
- If the recipe expects the agent to wait for a cue before doing something high-stakes (a live demo, a destructive operation), say so explicitly: "DO NOT begin X until the human says 'go'."

**Common mistake:** writing comment.md as a marketing pitch instead of an instruction. Comment.md is read silently every turn — it should change *how the agent behaves*, not *what the agent feels*.

**Good comment.md (clear instructions):**
```
You are the main orchestrator for the PKU demo. Your first
action when waking is `library invoke lingtai-demo-pku` —
that loads the full performance script. Do not begin the
demo proper until the human gives an explicit cue ("开始",
"go", or similar). Until then, send a brief readiness
message and wait.
```

**Bad comment.md (narration, ambiguous):**
```
You are about to perform a beautiful demonstration of the
LingTai system. The audience will be amazed. The hook is
about token abundance. Three acts. Be theatrical.
```
That tells the agent the *vibe*, not the *behavior*. The agent will likely jump straight into performing instead of waiting.

### 3. `covenant.md` — Covenant Override

Overrides the system-wide covenant (`~/.lingtai-tui/covenant/<lang>/covenant.md`) for agents created with this recipe.

**Purpose:** Some recipes need a fundamentally different covenant. For example, a utility agent that should never spawn avatars or participate in networks needs a simpler covenant than the default.

**When absent:** the kernel's system-default covenant is used at agent launch. No change in default behavior.

**Rules:**
- **No placeholders** — static text.
- Same locale-fallback as greet and comment.

### 4. `procedures.md` — Procedures Override

Overrides the system-wide procedures (`~/.lingtai-tui/procedures/<lang>/procedures.md`) for agents created with this recipe.

**Purpose:** Some recipes need different operational procedures (molt ladder, lifecycle transitions, mailbox hygiene). Most recipes leave this untouched.

**When absent:** the kernel's system-default procedures are used at agent launch.

**Rules:**
- **No placeholders** — static text.
- Same locale-fallback as greet and comment.

## The Library Sibling

When `recipe.json#library_name` is a non-null string, the bundle must contain a sibling folder with that exact name. The library is a framework-agnostic skill bundle — any agent framework that reads `SKILL.md` files (LingTai, Claude Skill, Cursor) can consume it.

**Critical layout rule (strict)**: every skill lives in its **own subdirectory** under the library folder, with `SKILL.md` at the subdirectory root. A `SKILL.md` placed directly at the library-folder root is **never permitted** — the runtime scanner ignores it (only `<library>/<skill>/SKILL.md` is registered) and the validator rejects it as an error. This applies even when the library contains exactly one skill: nest it.

```
<bundle>/
├── .recipe/recipe.json              # library_name: "velli"
└── velli/                           # ← library folder (NOT itself a skill)
    ├── argument-switchbacks/        # ← each skill in its own subdir
    │   └── SKILL.md
    ├── profile/
    │   ├── SKILL.md
    │   └── biography.md             # skill-internal files are fine
    └── velli.bib                    # non-skill content is fine at root
```

**Single-skill libraries are not an exception.** If your recipe has exactly one skill, nest it the same way every other library does — even if the library name and skill name are identical. The validator will reject the flat layout regardless of skill count.

```
<bundle>/
├── .recipe/recipe.json              # library_name: "impersonate-meta"
└── impersonate-meta/                # library folder
    └── impersonate-meta/            # ← skill folder (same name, one level deeper)
        ├── SKILL.md
        ├── primers/
        └── ...
```

**Wrong** — scanner will not register this skill and will mark subdirs as corrupted:

```
<bundle>/
└── impersonate-meta/                # library folder
    ├── SKILL.md                     # ← IGNORED (at library root, not in skill subdir)
    ├── primers/                     # ← reported as "not a skill (no SKILL.md)"
    └── scripts/                     # ← reported as "not a skill (no SKILL.md)"
```

### How the TUI registers libraries

When a recipe with `library_name: "velli"` is applied, the TUI:

1. Copies the library folder from the bundle into the project root: `<bundle>/velli/` → `<project>/velli/`.
2. For each agent under `<project>/.lingtai/<agent>/`, appends `"../../velli"` to that agent's `manifest.capabilities.library.paths`.

The `../../` climbs out of `<project>/.lingtai/<agent>/` to the project root, where the library sits. The path is always relative — bundles are in-project artifacts by convention, so the relative path is stable regardless of where the project is on disk.

### Library path is additive across recipe changes

Switching from a recipe with `library_name: "old-lib"` to one with `library_name: "new-lib"` **adds** `"../../new-lib"` to each agent's `library.paths` without removing `"../../old-lib"`. The old library folder at `<project>/old-lib/` is also not deleted. Rationale: agents may have come to rely on previously-available skills; auto-removal is the kind of silent change that breaks things. Cleanup is the user's responsibility.

The behavioral layer (greet/comment/covenant/procedures) is different — it IS fully replaced on recipe change. Only `library.paths` accumulates.

### Library content is monolingual

Libraries don't have the `<lang>/` fallback the behavioral layer uses. Each skill ships a single `SKILL.md` in whatever language the author writes in. That's because libraries are meant to be drop-in for non-LingTai frameworks, most of which have no i18n convention for skills. If you want a bilingual skill, write it bilingually in one file.

## The Network Snapshot (`.lingtai/`)

An exported bundle MAY include a full network snapshot — the live state of every agent in a running LingTai project. The network export flow produces this when the author wants to share not just the recipe but the agents themselves (their memories, personalities, accumulated codex entries).

### What's kept vs stripped

| Per-agent file | Kept? | Reason |
|---|---|---|
| `.agent.json` | ✅ KEPT | Identity blueprint (agent_name, address) |
| `system/*.md` | ✅ KEPT | Agent-authored firmware (covenant, principle, procedures, pad) |
| `codex/codex.json` | ✅ KEPT | Structured memory accumulated by the agent |
| `pad.md` | ✅ KEPT | Working memory |
| `history/chat_history.jsonl` | ✅ KEPT | Conversation trace |
| `mailbox/` | ✅ KEPT | Sanitized mail per export scripts |
| `delegates/ledger.jsonl` | ✅ KEPT | Avatar spawn record |
| `logs/*.jsonl` | ✅ KEPT | Event log, token ledger |
| **`init.json`** | ❌ **STRIPPED** | Contains the exporter's install-specific LLM choice, API key env names, admin flags |

### Why `init.json` is stripped

`init.json` encodes install-specific, per-user infrastructure: LLM provider, model name, API key env variable name, capability selection, admin flags (karma/nirvana). None of this should travel with the network:

- The recipient may not have access to the same LLM provider.
- The recipient may have different security preferences for `bash: yolo`.
- The recipient has their own MCP tools configured.
- API key env variable conventions may differ between installations.

On import, the recipient's TUI detects missing `init.json` files under `.lingtai/<agent>/`, prompts the recipient to pick an LLM preset once, and runs `preset.RehydrateNetwork` to generate a fresh `init.json` for each agent using the recipient's chosen preset. The standard recipe-apply flow then follows, writing `.prompt` files and registering library paths.

## i18n Fallback Rules

All locale-aware content under `.recipe/` uses the same two-level fallback:

1. Try `<lang>/`-prefixed variant
2. Fall back to root

| Content | Lookup order |
|---|---|
| `recipe.json` | `.recipe/<lang>/recipe.json` → `.recipe/recipe.json` |
| `greet.md` | `.recipe/greet/<lang>/greet.md` → `.recipe/greet/greet.md` |
| `comment.md` | `.recipe/comment/<lang>/comment.md` → `.recipe/comment/comment.md` |
| `covenant.md` | `.recipe/covenant/<lang>/covenant.md` → `.recipe/covenant/covenant.md` |
| `procedures.md` | `.recipe/procedures/<lang>/procedures.md` → `.recipe/procedures/procedures.md` |

A single root-level file serves all languages. Per-locale files override it only when present.

**Known locale codes:** `en`, `zh`, `wen`. Unknown codes produce warnings from the validator but don't block the bundle.

## Validating a Recipe

Before `git init`-ing a bundle for sharing, run the validator:

```bash
python3 ~/.lingtai-tui/utilities/lingtai-recipe/scripts/validate_recipe.py <bundle-root>
```

Exit code 0 means the bundle is structurally valid. Warnings are reported but do not block. Exit code 1 means the bundle has errors and should not be shipped.

### What the validator checks

- `.recipe/recipe.json` exists and has required fields (`id`, `name`, `description`), valid `version` if present, valid `library_name` (null or simple folder name).
- Locale-variant `recipe.json` files, when present, have valid `name` and `description`.
- Each present behavioral-layer directory contains either `<layer>.md` at root or at least one `<lang>/<layer>.md`. Empty layer directories are rejected.
- `comment.md`, `covenant.md`, `procedures.md` contain no placeholders (only `greet.md` may use them).
- `greet.md` doesn't start with `[system]` (warning only).
- When `recipe.json#library_name` is non-null, the named sibling folder exists and contains at least one `SKILL.md` (missing folder = error, no SKILL.md = warning).
- When `.lingtai/` is present (network snapshot), no agent has an `init.json` (must be stripped per export rules).
- Unknown locale codes, stray files at `.recipe/` root: warnings.

The validator is the single source of truth. If this reference and the validator disagree, the validator wins — and this doc should be updated to match.

## Creating a Recipe by Hand

Minimum viable recipe (no greet, no comment, no library):

```
my-recipe/
└── .recipe/
    └── recipe.json
```

With `recipe.json`:

```json
{
  "id": "my-recipe",
  "version": "1.0.0",
  "name": "My Recipe",
  "description": "Does nothing but apply cleanly.",
  "library_name": null
}
```

The validator will pass. An agent created with this recipe starts silent, with the kernel's default covenant and procedures, and no library beyond the defaults.

Add layers as needed:

- `.recipe/greet/greet.md` — give the agent a first-contact message
- `.recipe/comment/comment.md` — give it ongoing behavioral constraints
- `.recipe/covenant/covenant.md` — override the covenant if you need fundamentally different ethics
- `.recipe/procedures/procedures.md` — override lifecycle procedures (rare)
- `<library_name>/<skill>/SKILL.md` + update `recipe.json#library_name` — ship shared skills

## Testing a Recipe

1. Author the bundle at some path (e.g., `~/work/my-recipe/`).
2. Run `validate_recipe.py <bundle-root>` — should exit 0.
3. In an existing LingTai project, run `/setup` and pick "Custom recipe" — point at the bundle root.
4. The TUI copies the bundle into the project (`.recipe/` + library + optional `.lingtai/`) and applies it.
5. The orchestrator relaunches with your recipe: new `.prompt`, updated `init.json` fields, library path registered.

Iterate: edit the bundle in place, then run `/setup` again to re-apply. The TUI re-copies the bundle into the project and re-applies — behavioral layer fully replaced, library path additive.

## Publishing a Recipe

Use one of the two export flows:

- **Recipe-only publish**: `assets/export-recipe.md` walks you through authoring a recipe bundle (no network state) and turning it into a shareable git repo. Recipients clone the repo and point `/setup` at it.
- **Full network publish**: `assets/export-network.md` copies the live `.lingtai/`, strips per-agent `init.json` files, writes a fresh `.recipe/`, runs privacy + sensitivity scans, and produces a shareable git repo. Recipients clone, open with `lingtai-tui`, rehydrate per-agent `init.json` with their own preset, and get the whole network materialized.

Both flows invoke the same validator before `git init`. If the validator errors, the export stops.
