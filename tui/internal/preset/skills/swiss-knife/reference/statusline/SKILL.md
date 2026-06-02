---
name: statusline
description: >
  Nested swiss-knife reference for customizing Claude Code's status line. Generate
  and install a per-user statusLine command that displays selected fields such as
  model, context usage, cost, effort, output style, repo directory, worktree,
  git branch, and vim mode. Use when the human asks to customize Claude Code's
  status bar, status line, context display, effort display, or personal coding
  CLI prompt.
version: 1.0.0
tags: [claude-code, statusline, cli, customization, python]
---

# Claude Code Status Line Customizer

Generate a Claude Code `statusLine` command from LingTai's bundled utility
library. The generated status line script reads Claude Code's session JSON from
stdin and prints exactly one line to stdout, which Claude Code displays under
the conversation.

This is inspired by Claude Code's official status line interface
(https://code.claude.com/docs/en/statusline) and the Webup statusline skill
(https://github.com/webup/skills-cc#-webup-statusline), but it is packaged as a
LingTai swiss-knife nested utility and uses only Python standard-library code.

## Quick Usage

After `lingtai-tui bootstrap` or `/doctor` refreshes bundled utilities:

```bash
~/.lingtai-tui/runtime/venv/bin/python3 \
  ~/.lingtai-tui/utilities/swiss-knife/reference/statusline/scripts/generate_statusline.py \
  --preset default \
  --theme lingtai \
  --install
```

Preview a sample without modifying Claude Code settings:

```bash
python3 ~/.lingtai-tui/utilities/swiss-knife/reference/statusline/scripts/generate_statusline.py \
  --preset everything \
  --theme dracula \
  --sample
```

Print the generated runtime script instead of installing it:

```bash
python3 ~/.lingtai-tui/utilities/swiss-knife/reference/statusline/scripts/generate_statusline.py \
  --elements model,context,cost,effort,git,dir,worktree \
  --theme minimal \
  --print-script
```

## Presets

| Preset | Elements |
|---|---|
| `essentials` | `model,context,git,dir` |
| `default` | `model,context,effort,style,git,dir` |
| `everything` | `model,context,cost,effort,style,git,dir,worktree,vim` |

You can bypass presets with `--elements` and a comma-separated list.

## Elements

| Element | Source |
|---|---|
| `model` | Claude Code session JSON `model.display_name` or `model.name` |
| `context` | `context_window.remaining_percentage` or `used_percentage`; renders an ASCII progress bar |
| `cost` | `cost.total_cost_usd`; hidden when it rounds to zero |
| `effort` | session JSON `effortLevel`, then Claude settings fallback |
| `style` | `output_style.name`; hidden for default style |
| `git` | `worktree.branch`, then `git rev-parse --abbrev-ref HEAD`; appends `*` when dirty |
| `dir` | `worktree.original_repo_dir`, then `workspace.current_dir` |
| `worktree` | `worktree.name`, then git common-dir fallback |
| `vim` | `vim.mode`; hidden when inactive |

## Themes

| Theme | Behavior |
|---|---|
| `lingtai` | Calm cyan/green/yellow/red palette with compact labels |
| `dracula` | Higher-saturation purple/cyan/pink palette |
| `gruvbox` | Warm retro terminal palette |
| `minimal` | No ANSI color and fewer labels |

The `context` element changes color by remaining capacity: green above 50%,
yellow from 20% to 50%, and red below 20%. The `effort` element is green for
low/minimal, yellow for medium, and bold red for high/max.

## Install Behavior

`--install` writes:

- `~/.claude/scripts/lingtai_statusline.py`
- `~/.claude/settings.json`, preserving existing settings and updating only
  `statusLine`

The generator creates a `.bak` copy of `settings.json` by default before
rewriting it. Pass `--no-backup` to skip that backup.

## Workflow

1. Pick a preset and theme from the tables above.
2. Run the generator with `--sample` first when tuning the layout.
3. Run with `--install`.
4. Restart Claude Code, or start a new Claude Code session, to see the updated
   status line.

## Troubleshooting

| Problem | Check |
|---|---|
| No status line appears | Verify `~/.claude/settings.json` has `statusLine.type=command` and `statusLine.command` points to an executable script |
| Git branch is blank | Run Claude Code inside a git worktree, or include `dir` instead of `git` |
| Colors render as raw escapes | Reinstall with `--theme minimal` or set `NO_COLOR=1` |
| Effort is blank | Claude Code may not expose it in session JSON; set it in Claude settings or remove `effort` |

---
> Found a bug or issue? Load the `lingtai-issue-report` skill and follow its
> instructions to report it.
