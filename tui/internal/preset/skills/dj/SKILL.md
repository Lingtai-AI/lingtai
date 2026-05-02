---
name: dj
description: Compose one music track that resonates with a project journal entry, on demand. Walks the user's saved presets to find a usable media-creation provider (MiniMax, etc.), reads the journal at ~/.lingtai-tui/brief/projects/<hash>/journal.md, picks a genre that fits the day, generates the audio, and saves it next to the journal under music/ with an index entry. Loads when the user asks for music for a journal day, a project's vibe, or a specific genre — declines honestly when no usable provider is configured.
version: 1.0.0
tags: [media-creation, music, journal, on-demand]
---

# DJ — On-demand journal-track composer

A reusable workflow for composing one music track per project journal entry. Use this when the user asks for music tied to what they've been working on — "make a track for today's journal", "give me a bossa nova for last week", "the journal mentioned X — try a piece in style Y".

This skill assumes you already have file/bash/library capabilities. It does **not** install or configure any media provider — it discovers what's available in your library and composes via whichever skill matches the user's saved presets.

## When to load this skill

- User asks for music tied to a specific project, journal date, or session.
- User asks what genre would fit the project's mood.
- User asks why a previous track sounded the way it did, or wants another take on the same journal entry.

If the request is ambiguous (which journal? which genre? which project?), **ask first**. Don't guess and burn a generation call.

## Preflight — find a usable provider

Before composing, you need to know what providers you can actually reach. Run this the first time the user asks for music in a session, and re-run when the request would use a media path you haven't tried this session.

**Step A — enumerate available media-creation skills.** Look in your library catalog for skills tagged `media-creation`. If your catalog only shows `name` + `description`, the description still mentions "media-creation" for tagged skills — grep that.

If you want the canonical list, walk your library Tier-1 paths:

```bash
for path in <Tier-1 library paths from your library() info>; do
  for skill in "$path"/*/SKILL.md; do
    [ -f "$skill" ] || continue
    grep -l -E '(^|, )media-creation(,|]| )' "$skill" 2>/dev/null
  done
done
```

For each one, the skill itself is the source of truth on **what providers it talks to** (MiniMax, MiMo, etc.) and **what env-var key** it expects.

**Step B — cross-check against the user's saved presets.** Each media-creation skill expects an API key. The user's saved presets at `~/.lingtai-tui/presets/*.json` declare which provider keys they have:

```bash
for f in ~/.lingtai-tui/presets/*.json ~/.lingtai-tui/presets/*.jsonc; do
  [ -f "$f" ] || continue
  python3 -c "
import json, re, sys
text = open('$f').read()
text = re.sub(r'//[^\n]*', '', text)
try:
    d = json.loads(text)
    llm = d.get('manifest', {}).get('llm', {})
    print(llm.get('provider'), '|', llm.get('api_key_env') or '(none)', '|', '$f')
except Exception:
    pass
"
done
```

The saved keys themselves live in `~/.lingtai-tui/.env`:

```bash
grep -E '^[A-Z_]+_API_KEY=' ~/.lingtai-tui/.env | cut -d= -f1
```

**Step C — intersect.** A media-creation skill is *usable* only if (a) its provider matches a saved preset's `provider` AND (b) its expected env-var key is present in `.env`. Build the list of usable providers.

**Step D — decide.**

- **Any usable provider exists** → pick one (prefer the user's stated provider; otherwise pick whichever matches a current preset they're using; otherwise pick the first). Load its skill, follow its instructions, compose.
- **No usable provider** → reply plainly. Tell them what skills you found, which providers they imply, and which presets they'd need to add for those skills to work. Suggest concretely (e.g. "save a MiniMax preset via the TUI's preset library and paste your `sk-cp-…` key — this will populate the preset's slot in `~/.lingtai-tui/.env` and unlock the `minimax-cli` skill"). **Do not produce a fake track. Do not pretend.**

## Genre palette

Start from this palette unless the user specifies otherwise:

- **符合周礼 / 雅乐** — court-ritual music in the Zhou-li tradition; ceremonial, restrained, modal, suitable for milestones and decisions of weight.
- **Bossa nova** — gentle, syncopated, warm; for sessions that flowed easily.
- **Jazz** — small-combo or trio; for sessions full of improvisation, exploration, course-correction.
- **Lo-fi hip-hop** — relaxed, instrumental, low-stakes maintenance work and refactors.
- **Ambient / drone** — long-form thinking, deep architecture work, contemplative sessions.
- **Classical chamber** — careful, structured engineering; quartet textures.

The user may request anything outside this palette — Ravel, Coltrane, City Pop, 戏曲, gamelan, anything. Honor specific requests. The palette is a starting point, not a fence.

## Composition working order

1. **Parse the request.** Which project? (Default: the current project — its hash is the one matching the user's working directory in the registry.) Which journal date or hour? (Default: most recent.) Which genre? (If unspecified, propose 2–3 from the palette that fit the journal's mood and ask, OR pick one and explain your choice in the reply.)

2. **Read the journal.** Project journals live at `~/.lingtai-tui/brief/projects/<hash>/journal.md`. The `brief.md` / `profile.md` files in the same tree give you context on the user. If the user points at a specific date or hour, also consult the matching `history/<YYYY-MM-DD-HH>.md`. Distill: what did the user do? What was the emotional arc? What instrumentation, tempo, key, mood would honor this session?

3. **Load the chosen media-creation skill** via `library()`. Follow its preflight and `curl` whatever live docs it points to so you have the current API schema. The skill knows: where the key lives, which region/host to use, which model to call, parameter shape, expected response shape, how long to wait.

4. **Compose the prompt.** Translate the journal's mood into a music-generation prompt: genre, instruments, tempo, key, mood adjectives, optional structure (intro / verse / breakdown / outro), reference artists if useful. Keep it under whatever the API limit is per the live docs.

5. **Call the API.** Use bash + curl per the skill. If the response is a URL, `curl -o` it down; if a base64 blob, decode to file. Save to `~/.lingtai-tui/brief/projects/<hash>/music/<YYYY-MM-DD>-<genre-slug>-<short-title-slug>.<ext>`. Create the `music/` folder if it doesn't exist. Do not overwrite an existing track for the same journal date — append a counter (`-2`, `-3`) if the user asks for another take.

6. **Append the index entry.** `~/.lingtai-tui/brief/projects/<hash>/music/index.md`:

   ```markdown
   - **2026-04-29 — bossa nova — "Refactor in B♭"** · journal `2026-04-29` · *gentle syncopation for a clean refactor day*
   ```

   Create the index file if it doesn't exist.

7. **Reply to the user.** Tell them what you composed, where the file is, and one or two sentences on why this genre fit. Do not include the API request/response payload — keep the reply concise.

## What this skill does NOT do

- **No journal mutation.** You read journals; you do not edit them.
- **No retries on long-running media calls.** Provider music endpoints can take 1–10 minutes — wait it out, do not retry.
- **No fake tracks.** If no usable provider is configured, say so plainly. Silence beats deception.
