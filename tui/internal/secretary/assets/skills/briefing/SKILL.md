---
name: briefing
description: Scan session history dumps and produce profile, journal, and brief files. Invoke this skill at the start of every briefing cycle.
version: 1.0.0
---

# Briefing Skill

This skill guides your hourly briefing cycle. You maintain three types of files:

| File | Scope | Location |
|---|---|---|
| `profile.md` | Universal (all projects) | `~/.lingtai-tui/brief/profile.md` |
| `journal.md` | Per-project | `~/.lingtai-tui/brief/projects/<hash>/journal.md` |
| `brief.md` | Per-project | `~/.lingtai-tui/brief/projects/<hash>/brief.md` |

## Step 1: Discover Projects

Read the project registry to find all registered projects:

```bash
cat ~/.lingtai-tui/registry.jsonl
```

Each line is `{"path": "/absolute/path/to/project"}`. For each project path, compute its brief directory hash. The hash is the first 12 hex characters of SHA-256 of the project path. You can compute it with:

```bash
echo -n "/absolute/path/to/project" | shasum -a 256 | cut -c1-12
```

The brief directory for each project is at `~/.lingtai-tui/brief/projects/<hash>/`. Inside each: `history/` (hourly dumps), `journal.md`, `brief.md`.

Use the project path's basename as the human-readable project name in your journals (e.g., `/Users/alice/my-app` → "my-app").

## Step 2: Scan for New History

For each project, check the `history/` directory for files newer than your last processed timestamp (stored in your memory).

```bash
ls -lt ~/.lingtai-tui/brief/projects/<hash>/history/
```

History files are named `YYYY-MM-DD-HH.md`. Compare against your last-processed timestamp to find new ones. Read each new file to understand what happened in that hour.

## Step 3: Update Journal

Read the existing `journal.md` for the project (if it exists). Rewrite it in full, incorporating the new history. The journal should capture:

- **What the human is working on** in this project — active tasks, goals, current focus
- **Key decisions made** — architectural choices, tool selections, design directions
- **Collaborators** — which agents are active, what they specialize in
- **Recent progress** — what was accomplished in the latest sessions
- **Pending items** — what is unfinished or blocked

Keep the journal concise but complete. It is read by agents on every `/refresh` — every token counts. Target 500-2000 words depending on project complexity.

Write the journal:

```bash
cat > ~/.lingtai-tui/brief/projects/<hash>/journal.md << 'JOURNAL_EOF'
<journal content>
JOURNAL_EOF
```

## Step 4: Update Profile

Read the existing `profile.md` (if it exists). Rewrite it in full, incorporating observations from ALL projects. The profile captures:

- **Who the human is** — role, expertise, domain knowledge
- **Communication style** — how they give instructions, level of detail they expect
- **Preferences** — tools, languages, frameworks they favor
- **Working patterns** — when they work, how they structure sessions, how they delegate

The profile is universal — it should not contain project-specific details. It helps agents across ALL projects understand who they are working with.

Write the profile:

```bash
cat > ~/.lingtai-tui/brief/profile.md << 'PROFILE_EOF'
<profile content>
PROFILE_EOF
```

## Step 5: Construct Brief

For each project, construct `brief.md` by concatenating profile and journal:

```bash
cat ~/.lingtai-tui/brief/profile.md > ~/.lingtai-tui/brief/projects/<hash>/brief.md
echo -e "\n---\n" >> ~/.lingtai-tui/brief/projects/<hash>/brief.md
cat ~/.lingtai-tui/brief/projects/<hash>/journal.md >> ~/.lingtai-tui/brief/projects/<hash>/brief.md
```

This is the file that agents read via `brief_file` in their init.json. It is reloaded on every `/refresh` or `/cpr`.

## Step 6: Record State

After processing, update your memory with the latest processed timestamp for each project. Include the project name for readability:

```
psyche(memory, edit, content="last processed: my-app (a1b2c3d4e5f6)=2026-04-10-14, my-site (f6e5d4c3b2a1)=2026-04-10-13, ...")
```

This ensures the next cycle only processes new history.

## Notes

- If a project has no new history, skip it entirely — do not rewrite unchanged files.
- If `profile.md` does not exist yet, create it from whatever history is available. First impressions matter — even a sparse profile is better than none.
- If a project's `journal.md` does not exist yet, create it from the available history.
- Git operations are not needed — the files are ephemeral working state, not versioned artifacts.
- Be efficient. Read only what is new. Write only what has changed.
