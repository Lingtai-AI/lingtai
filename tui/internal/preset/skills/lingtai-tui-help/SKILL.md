---
name: lingtai-tui-help
description: >
  Canonical reference for the lingtai-tui terminal UI — what the slash commands
  do and how to drive the interface. Read this when a human asks how to use the
  TUI, what a slash command does, how the command palette works, or which
  command to reach for. The in-app `/help` command is a shortcut that opens this
  skill's slash-command guide in the markdown viewer, jumping to the asset that
  matches the current UI language.
version: 1.0.0
tags: [tui, help, slash-commands, reference, palette, lingtai-tui]
---

# lingtai-tui help — canonical TUI reference

This skill is the single source of truth for `lingtai-tui` usage documentation:
the slash-command catalog and how to operate the terminal interface. The in-app
`/help` command is only a shortcut — it opens the markdown viewer on this skill's
slash-command guide, jumping straight to the asset that matches the current UI
language.

Keep this skill in sync with `DefaultCommands()` in
`tui/internal/tui/palette.go`: every slash command shipped there must be
described in the slash-command assets below.

## Slash-command assets

The full slash-command reference is stored as three language assets so `/help`
can show the human their own language without translating UI prose at runtime:

- `assets/slash-commands.en.md` — English (canonical wording).
- `assets/slash-commands.zh.md` — 简体中文.
- `assets/slash-commands.wen.md` — 文言.

The English asset is canonical; the Chinese and Wen assets are concise but
complete translations of the same content. When a slash command is added,
changed, or removed, update **all three** assets together.

## How `/help` resolves the language

`/help` calls `i18n.Lang()` (returns `"en"`, `"zh"`, or `"wen"`) and opens the
matching `assets/slash-commands.<lang>.md` page. Any unknown locale falls back to
the English asset.
