---
name: skills-manual
description: How to use, author, and share skills. Full library workflow.
version: 2.0.0
---

# Skills Manual

## What Is a Skill?

A skill is a **structured instruction bundle** — a folder containing a `SKILL.md` file (this format) and optional supporting files (scripts, templates, reference docs). Skills teach you how to perform specialized, repeatable tasks. They are the primary mechanism for accumulating competence across agent lifetimes: knowledge that survives molt.

Skills appear as `<available_skills>` in your system prompt (XML catalog). When a task matches a skill description, read its `SKILL.md` via `read` and follow its instructions.

## SKILL.md Format

```markdown
---
name: my-skill
description: One-line description (this is what you see in the catalog)
version: 1.0.0
---

Step-by-step instructions in Markdown...
```

**Required frontmatter:** `name`, `description`.
**Optional frontmatter:** `version`, `author`, `tags`.

### Skill Folder Structure

```
my-skill/
  SKILL.md              # Entry point — instructions (keep under 500 lines)
  scripts/              # Deterministic helper scripts (Python, Bash)
  references/           # Supplementary context (schemas, cheatsheets)
  assets/               # Templates, static files
```

SKILL.md should reference supporting files by relative path. Offload dense content to subdirectories — keep SKILL.md focused on the procedure.

## Library Layout

Every agent has its own skill library at `<agent-dir>/.library/`:

```
.library/
  intrinsic/            # Hard-shipped by the kernel — do not edit
    skill-for-skill/
    lingtai-mcp/
    ...
  custom/               # YOUR skills — create new skills here
    my-workflow/
      SKILL.md
    data-pipeline/
      SKILL.md
```

**Intrinsic skills** are bundled with the kernel and managed automatically. Never write to or delete from `.library/intrinsic/`.

**Custom skills** belong to you. Always place new skills under `.library/custom/<name>/`.

### Additional Library Paths

Beyond `intrinsic/` and `custom/`, the library loads skill paths listed in `init.json` under `manifest.capabilities.library.paths`. The defaults are:

| Path | Purpose |
|------|---------|
| `../.library_shared` | Network-shared skills — all agents in the project share these |
| `~/.lingtai-tui/utilities` | TUI utility skills installed globally |

Skills from all paths are merged into the catalog that appears in your system prompt. You can add more paths by editing `init.json` and calling `system({"action": "refresh"})`.

## Authoring a Skill

1. Create the folder: `<agent-dir>/.library/custom/<name>/`
2. Write `SKILL.md` with the frontmatter and instructions.
3. Add supporting files (`scripts/`, `references/`, `assets/`) as needed.
4. Call `system({"action": "refresh"})` — the library rescans and the new skill appears in your catalog immediately.

No register step is needed. `system.refresh` is the only action required.

## Loading a Skill Into Working Memory

The XML catalog in your system prompt shows skill names and descriptions but not full content. To use a skill's detailed instructions across multiple turns, load it into your pad:

```
psyche({"object": "pad", "action": "append", "files": ["<path-to-SKILL.md>"]})
```

This pins the skill content into the pad so it persists through context turns.

## Sharing With the Network

To make a skill available to all agents in the project, copy it to the shared library:

```bash
cp -r .library/custom/<name> ../.library_shared/<name>
```

Then call `system({"action": "refresh"})` on any agent whose catalog should update. Other agents pick it up on their next refresh or restart.

If you have **admin privileges**, you can curate `.library_shared/` directly — merging duplicates, pruning stale entries, reorganizing. Use `bash`/`rm` for filesystem operations; call `system.refresh` afterward to update the catalog.

## Adding a Library Path

To add a custom skill source (e.g., a shared team directory or a cloned skill repo):

1. Open `init.json`.
2. Locate `manifest.capabilities.library.paths` (a JSON array of path strings).
3. Append the new path.
4. Call `system({"action": "refresh"})`.

Paths can be absolute or relative to the agent's working directory. Tilde expansion (`~`) is supported.

## Library Health Check

Call `library({"action": "info"})` to get:
- A meta-skill guide (full library documentation)
- A runtime health snapshot: paths loaded, skill counts, any errors or corrupted folders

Use this whenever you suspect a path is not loading or a skill is missing from the catalog.

## When to Create a Skill

**Create a skill when:**
- The task is repeatable with consistent steps
- The procedure requires domain knowledge not reliably available without notes
- A workflow involves multi-step orchestration or error handling
- You want to share expertise with other agents in the network

**Do NOT create a skill when:**
- It's a one-off task with no reuse value
- The task is just "call this one API endpoint"
- The instructions are personality or style preferences (use covenant or character instead)

## Writing a Good Skill

1. **Trigger-optimized description** — say what it does AND what it doesn't cover. The description is the only thing visible in the catalog without loading the file.
2. **Numbered steps in imperative form** — "Extract the text...", not "You should extract..."
3. **Concrete templates** in `assets/` rather than prose descriptions of desired output format.
4. **Deterministic scripts** in `scripts/` for fragile or repetitive operations.
5. **Keep SKILL.md under 500 lines** — offload to `references/` and `scripts/`.

## Full Workflow Reference

For the complete end-to-end workflow — authoring, testing, sharing, and versioning — see the `skill-for-skill` intrinsic skill (`.library/intrinsic/skill-for-skill/SKILL.md`).
