# Recipe Format Reference

*This is the authoring reference of the `lingtai-recipe` skill. For overview of all recipe-related flows, read `../SKILL.md`. For the network-export flow, read `../assets/export-network.md`. For the standalone-recipe export flow, read `../assets/export-recipe.md`.*

A **launch recipe** is a named directory that shapes an orchestrator's first-contact behavior, ongoing constraints, and available skills. Every lingtai project uses a recipe — selected during `/setup` or inherited from a published network via `/agora`.

## Recipe Directory Structure

```
my-recipe/
  recipe.json             # Required — name and description
  en/
    recipe.json           # Optional — lang-specific override
    greet.md
    comment.md
    covenant.md           # Optional — overrides system-wide covenant
    procedures.md         # Optional — overrides system-wide procedures
  zh/
    greet.md
    comment.md
    covenant.md           # Optional
    procedures.md         # Optional
  skills/                 # Optional: recipe-shipped skills
    my-skill/
      SKILL.md            # Frontmatter + pointer to lang variants
      SKILL-en.md         # Full instructions (English)
      SKILL-zh.md         # Full instructions (Chinese)
      scripts/            # Optional helper scripts
      assets/             # Optional assets
    my-other-skill/
      SKILL.md            # Single-language skill (no variants needed)
```

## The Five Components

### 1. `greet.md` — First Contact

The first message the orchestrator sends when a new user opens the TUI. Written from the orchestrator's perspective (first person).

**Purpose:** Set the tone, introduce the network, tell the user what they can do, offer guidance.

**Placeholders** (substituted at setup time):

| Placeholder | Value |
|---|---|
| `{{time}}` | Current date and time (2006-01-02 15:04) |
| `{{addr}}` | Human's email address in the network |
| `{{lang}}` | Language code (en, zh, wen) |
| `{{location}}` | Human's geographic location (City, Region, Country) |
| `{{soul_delay}}` | Soul cycle interval in seconds |

**Example:**

```markdown
Welcome to the OpenClaw Explainer Network! It's {{time}}.

I'm the lead orchestrator of a team of 10 agents. Type /cpr all
to wake everyone up, then tell me what you'd like to explore.
```

**Rules:**
- Keep it short (5-10 sentences max)
- Be proactive — introduce yourself, don't wait to be asked
- Always remind users to `/cpr all` to wake the full team (if the network has multiple agents)
- Use `{{time}}` and `{{location}}` to make the greeting feel alive

### 2. `comment.md` — Ongoing Behavioral Constraints

Injected into the orchestrator's system prompt on every turn. The persistent playbook.

**Purpose:** Define what topics to cover, how to delegate, constraints, tone. Think of it as a covenant extension specific to this recipe.

**Rules:**
- No placeholders — this is static text
- Keep it focused and concise — it's injected every turn, so every token counts
- Reference skills by name if the recipe ships skills (the agent can load them on demand)

### 3. `covenant.md` — Covenant Override (Optional)

Overrides the system-wide covenant (`~/.lingtai-tui/covenant/<lang>/covenant.md`) for agents created with this recipe. When present, the recipe's covenant is used instead of the global one.

**Purpose:** Some recipes need a fundamentally different covenant. For example, a utility agent that should never spawn avatars or participate in networks needs a simpler covenant than the default.

**Rules:**
- No placeholders — static text
- If absent, the system-wide covenant is used (no change in behavior)
- Follows the same i18n fallback as greet.md and comment.md

### 4. `procedures.md` — Procedures Override (Optional)

Overrides the system-wide procedures (`~/.lingtai-tui/procedures/<lang>/procedures.md`) for agents created with this recipe. When present, the recipe's procedures are used instead of the global ones.

**Purpose:** Some recipes need different operational procedures. For example, a utility agent may need simplified or entirely different procedures than the default.

**Rules:**
- No placeholders — static text
- If absent, the system-wide procedures are used (no change in behavior)
- Follows the same i18n fallback as greet.md and comment.md

### 5. `skills/` — Recipe-Shipped Skills

Optional. Skills that travel with the recipe. Currently, recipe skills are NOT auto-injected into agent catalogs — the old symlink-farm mechanism was removed when the library capability moved to a per-agent model. Agents who want recipe skills should add the recipe's `skills/` directory to their `init.json` `manifest.capabilities.library.paths` manually, then call `system({"action": "refresh"})`.

Each skill follows the standard SKILL.md contract:

```markdown
---
name: my-skill-name
description: One-line description of what this skill does
version: 1.0.0
---

# Skill content here...
```

**i18n:** For multi-language skills, use `SKILL.md` as a frontmatter-only pointer and provide `SKILL-en.md`, `SKILL-zh.md`, etc. for full instructions. The agent reads `SKILL.md`, sees which lang variants are available, and reads the appropriate one. Single-language skills just put everything in `SKILL.md`.

**Grouping:** When an agent adds a recipe's `skills/` path to its `library.paths`, the scanner treats each subdirectory as an independent skill. There is no automatic group header; skills appear flat in the catalog with their own names.

**Scripts and assets:** Place them alongside `SKILL.md` in the skill directory. They are self-contained per skill.

## recipe.json — Recipe Manifest

Every recipe must contain a `recipe.json` at root level (language-specific overrides are optional):

```json
{
  "name": "My Recipe Name",
  "description": "One-line description of what this recipe does"
}
```

- `name` — **required**, displayed in the TUI recipe picker
- `description` — **required**, shown as hint text in the picker
- Extra fields are ignored but tolerated (forward-compatible)

Without a valid `recipe.json`, the recipe will not be recognized as importable. The TUI only auto-detects `.lingtai-recipe/` directories that contain a valid manifest.

## i18n Fallback Rules

All recipe files (greet.md, comment.md, covenant.md, procedures.md, skill directories) use the same resolution:

1. Try `<lang>/` — language-specific version
2. Fall back to root

**Resolution prefers lang over root.** The TUI's i18n lookup tries `<lang>/<file>` first and falls back to the root-level version. Either layout is valid. The validator (see "Validating a Recipe" below) accepts a file present at the root, under any lang subdirectory, or both — whichever fits the recipe's intent. A single-root-level copy serves all languages; per-lang copies override it.

## Recipe Types

| Type | Location | When Linked |
|---|---|---|
| Bundled | `~/.lingtai-tui/recipes/<name>/` | Always (shipped with TUI) |
| Custom | User-specified directory | When set via `/setup` |
| Shared | `<project>/.lingtai-recipe/` | Auto-discovered by TUI at setup |

All types follow the same directory structure and rules.

## The `.lingtai-recipe/` Convention

Both `/export recipe` and `/export network` author the **same** `.lingtai-recipe/` payload. An exported network is literally an exported recipe plus the `.lingtai/` state folder alongside it:

- **Exported recipe** = a repo with `recipe.json` at root and `.lingtai-recipe/` (no `.lingtai/`)
- **Exported network** = the same, but also with `.lingtai/` (full network state) and any project files

Both exporters run `validate_recipe.py` (see next section) before git-init, so the payload shape is enforced programmatically. If the format ever evolves, the validator is the single source of truth — update it first, then update these skills.

The recipient clones either kind of repo and opens it with `lingtai-tui`. The TUI auto-discovers `.lingtai-recipe/` via `ProjectLocalRecipeDir()` and uses it during setup. No manual path entry needed.

## Validating a Recipe

`tui/internal/preset/skills/lingtai-recipe/scripts/validate_recipe.py` is a sanity-check script that both exporters invoke before `git init`. It verifies:

- `recipe.json` at the repo root with `name` and `description`
- `.lingtai-recipe/` directory exists
- `greet.md` and `comment.md` present at `.lingtai-recipe/<lang>/` for at least one lang (or at `.lingtai-recipe/` root)
- No placeholders in `comment.md`, `covenant.md`, or `procedures.md` (only `greet.md` may use them)
- Every skill under `skills/<name>/` has `SKILL.md` with valid frontmatter (`name`, `description`, `version`)

Usage (the validator ships with the TUI at a stable per-user path):

```bash
python3 ~/.lingtai-tui/utilities/lingtai-recipe/scripts/validate_recipe.py <repo-root>
```

Exit code 0 means the payload is structurally valid. Warnings (unknown lang code, stray files at `.lingtai-recipe/` root) are reported but do not block.

## How to Create a Custom Recipe

1. Create a directory with the structure above
2. Write at least a `greet.md` (comment.md and skills/ are optional)
3. In the TUI, run `/setup`, select "Custom" recipe, and enter the path to your directory
4. The orchestrator will restart and use your recipe

## How to Publish a Recipe

Use `/export recipe` for a recipe-only export, or `/export network` for a full network snapshot. Both create `.lingtai-recipe/` in the output repo. The recipient clones the repo and opens it directly with `lingtai-tui` — no manual recipe path configuration needed.

## Testing

Point `/setup`'s custom recipe picker at your directory. The orchestrator restarts with your greet, comment, and skills immediately. Iterate until satisfied, then publish.
