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
│   ├── recipe.json                      # required — manifest (id, name, description, …)
│   ├── en/recipe.json                   # optional — locale variants for name/description
│   ├── zh/recipe.json
│   ├── wen/recipe.json
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

### Locale variants

`name` and `description` are themselves localized. The TUI resolves them via this fallback chain per the active locale:

1. Try `<bundle>/.recipe/<lang>/recipe.json`
2. Fall back to `<bundle>/.recipe/recipe.json`

Locale-variant `recipe.json` files only need `name` and `description`. `id`, `version`, and `library_name` are inherited from the root manifest.

```
.recipe/
├── recipe.json            # {"id":"greeter","name":"Greeter","description":"...","library_name":null}
├── zh/recipe.json         # {"name":"问候者","description":"..."}
└── wen/recipe.json        # {"name":"问候者","description":"..."}
```

Extra fields in any `recipe.json` are ignored — forward-compatible.

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

The first message the orchestrator sends when the human opens the TUI. Written from the orchestrator's perspective (first person) — it's their voice, not a system message.

**Purpose:** Set the tone, introduce the network, tell the user what they can do.

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

### 2. `comment.md` — Ongoing Behavioral Constraints

Injected into the orchestrator's system prompt on every turn. The persistent playbook.

**Purpose:** Define what topics to cover, how to delegate, constraints, tone. A recipe-specific extension of the covenant.

**When absent:** no comment file is referenced in `init.json`. The agent runs with just its covenant + procedures + recipe greet.

**Rules:**
- **No placeholders** — this is static text, validated.
- Keep it focused — every token counts because it's on every turn.
- Reference skills by name if the recipe ships a library.

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

```
<bundle>/
├── .recipe/recipe.json              # library_name: "velli"
└── velli/                           # ← sibling library folder
    ├── SKILL.md                     # optional library-entry skill
    ├── argument-switchbacks/
    │   └── SKILL.md
    ├── profile/
    │   └── SKILL.md
    └── velli.bib                    # any non-skill content is fine
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
