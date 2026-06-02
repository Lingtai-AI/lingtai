---
name: minimax-cli
description: >
  Thin top-level entry point for MiniMax CLI work. Read this when the human asks
  for MiniMax media generation, TTS, ad-hoc `mmx vision`, or when another skill
  such as `dj` needs a MiniMax provider; then load the canonical procedure at
  `../swiss-knife/reference/minimax-cli/SKILL.md` (the Swiss Knife nested
  reference) so MiniMax credential, region, and CLI guidance has one maintained
  home.
version: 2.1.0
canonical: ../swiss-knife/reference/minimax-cli/SKILL.md
tags: [manual, cli, minimax, mmx, image, video, music, speech, tts, vision, media-generation, router]
---

# minimax-cli — top-level entry point

This top-level skill is a read-only pointer; edit the `canonical` nested reference instead of adding MiniMax command recipes here. It exists for discoverability: agents can find `minimax-cli`
by bare name when a task mentions MiniMax, `mmx`, media generation, TTS, or
one-shot shell vision.

The maintained procedure lives in the Swiss Knife nested reference:

```yaml
canonical_reference:
  name: minimax-cli
  location: ../swiss-knife/reference/minimax-cli/SKILL.md
  description: >
    Canonical MiniMax `mmx` CLI guide: install `mmx-cli`, discover the correct
    TUI-managed MiniMax preset/key slot without leaking secrets, match mainland
    vs international regions, and route image/video/music/TTS generation or
    one-shot shell vision.
```

## Routing table

| Need | Read |
|---|---|
| Install or troubleshoot `mmx-cli` | `../swiss-knife/reference/minimax-cli/SKILL.md` |
| Find the right MiniMax key slot from TUI presets | `../swiss-knife/reference/minimax-cli/SKILL.md` |
| Match mainland vs international MiniMax regions | `../swiss-knife/reference/minimax-cli/SKILL.md` |
| Generate image, video, music, or TTS with MiniMax | `../swiss-knife/reference/minimax-cli/SKILL.md` |
| Use `mmx vision` for ad-hoc shell image understanding | `../swiss-knife/reference/minimax-cli/SKILL.md` |

Do not duplicate MiniMax command recipes here. Update the canonical nested
reference instead, then keep this pointer current if the location changes.

---
> **Found a bug or issue?** If you encounter any problems with this skill, load
> the `lingtai-issue-report` skill and follow its instructions to report it.
