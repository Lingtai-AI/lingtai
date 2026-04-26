# Exporting a Network

*This is the network-export sub-guide of the `lingtai-recipe` skill. For recipe-only export, read `export-recipe.md` alongside this file. For an overview of all recipe-related flows, read `../SKILL.md`.*

**Prerequisites:** Read `../references/recipe-format.md` first — Step 5 below creates a `.recipe/` dotfolder inside the exported bundle, and you need to understand the bundle shape, the four behavioral layers (all optional), the `recipe.json` schema, placeholders, and i18n rules. Also read `export-recipe.md` (next to this file) if the human wants to export a standalone recipe alongside the network.

You are about to copy the network you live in to an exportable location. **This is literal self-copying** — the snapshot will not contain the moment you made it. Everything up to this conversation turn will be in the exported copy; this turn itself will only exist in the original.

## First: which "network" and which "recipe"?

A project can hold three recipe-shaped artifacts at once, and they are NOT the same thing. Confirm scope with the human before doing anything destructive:

- **The network you are exporting** = the agents living in this project's `.lingtai/` (orchestrator + avatars + their mailboxes). This is *you and your siblings*, the thing the recipient should receive.
- **The outer project's own `.recipe/`** (if present at the project root, sibling of `.lingtai/`) = the recipe that was originally applied to *seed* this project. It describes the methodology / culture that produced this network — NOT the network itself. **Do not ship this as the launch recipe** in Step 5; it would tell new recipients to re-do the seeding workflow instead of stepping into the network you actually built.
- **The applied-recipe snapshot at `.lingtai/.tui-asset/.recipe/`** = a copy of the outer recipe captured by the TUI. Useful in Step 5 as *evidence* of the behavioral DNA already in force, but it is not what you bundle verbatim. (The scrub in Step 1 deletes this snapshot anyway — read it before you scrub if you want to refer to it.)

If the project root has its own `.recipe/`, it WILL be copied into the staging dir during Step 1a. That is fine for now — treat it as project content to discuss during Step 3 (the human may want to keep it, ignore it, or move it). What matters is that **Step 5 below authors a NEW `.recipe/` describing the *network*, not a copy of the seeding recipe.**

Walk the human through the steps below carefully. Each step is either *mechanical* (run a script, report the result) or *interactive* (discuss a decision with the human before proceeding). Never skip the interactive steps — the whole point of a skill-driven exporting flow is that a human is in the loop for judgment calls.

Scripts for this skill live at `../scripts/` (relative to this file). The canonical `.gitignore` template is at `./gitignore.template` (i.e., this directory). The skill itself is installed at `~/.lingtai-tui/utilities/lingtai-recipe/` — scripts at `~/.lingtai-tui/utilities/lingtai-recipe/scripts/`.

## What is an exported network bundle?

Same bundle shape as a recipe-only export (see `export-recipe.md` and `../references/recipe-format.md`) but with one extra sibling folder: `.lingtai/`, containing a full snapshot of every agent's accumulated state.

```
<bundle-root>/
├── .recipe/                            # LingTai-facing behavioral layer
├── <library_name>/                     # (optional) framework-agnostic skills
├── .lingtai/                           # ← full network snapshot, the thing that makes this a "network" export
│   ├── <agent>/
│   │   ├── .agent.json                 # identity blueprint (KEPT)
│   │   ├── system/*.md                 # KEPT — brief.md gets an "EXPORTED SNAPSHOT" banner prepended
│   │   ├── codex/codex.json            # KEPT
│   │   ├── pad.md                      # KEPT
│   │   ├── history/chat_history.jsonl  # STRIPPED — would make a clone wake up believing it is the original
│   │   ├── history/soul_history.jsonl  # STRIPPED — same reason
│   │   ├── mailbox/                    # KEPT (sanitized by archive/filter scripts)
│   │   ├── (NO init.json)              # STRIPPED — recipient picks their own LLM
│   │   └── (NO .library/intrinsic/)    # STRIPPED — kernel-managed, rebuilt on rehydration
│   └── .tui-asset/.recipe/             # snapshot of applied recipe (KEPT)
├── .exported-from                      # ← origin marker: name, source URL, timestamp
├── README.md                           # ← drafted by scripts/generate_readme.py
├── .gitignore
└── (any project files the human wants to ship)
```

Critical structural rules for network exports:

- **`init.json` is stripped from every agent directory.** It encodes the publisher's install-specific LLM provider, API key env names, admin flags — none of which should travel. The recipient regenerates `init.json` with their own preset during rehydration after cloning.
- **`history/chat_history.jsonl` and `history/soul_history.jsonl` are stripped.** Otherwise a cloned agent wakes up indistinguishable from the original — same memories, same in-flight conversation. Instead, `greet.md` serves as the export's 「前尘往事」 (charge), like a molt: the cloned agent reads greet on launch and learns who it was. The cleaned snapshot keeps `pad.md` and `codex/` (durable knowledge), drops the verbatim transcript.
- **Each agent's `system/brief.md` is stamped with an "EXPORTED SNAPSHOT" banner.** brief.md is read on every launch and forms the top of the system prompt — the banner makes the agent's first turn aware that it is a clone, not the original.
- **`.library/intrinsic/` is stripped from every agent.** It's kernel-managed and identical across LingTai installs — shipping it is dead weight. The recipient kernel rebuilds it on rehydration. `<agent>/.library/custom/` and `.lingtai/.library_shared/` are preserved (those are the network's own skills).

## Critical: Filesystem Rules

These rules prevent silent failures. Follow them without exception.

1. **Resolve `$HOME` first.** The `write` tool does NOT expand `~`. At the start of this skill, run:
   ```bash
   echo $HOME
   ```
   Use the result (e.g., `/Users/alice`) as the prefix for ALL file paths. Never use `~` in a `write` or `file` tool call.

2. **Always use absolute paths.** Every `write` call must use a full absolute path.

3. **Always `mkdir -p` before writing.**

4. **Verify after writing.** After writing files, run `find <dir> -type f | sort`.

5. **Never trust a write success message at face value.** Always verify with `find` or `ls`.

## How to talk to the human during this skill

**Use the `email` tool for every message to the human. Never rely on text output.**

This is a multi-round conversation with real latency between turns. Every question you need answered, every status update the human needs to know, every final confirmation before a destructive or externally-visible action (deleting files, pushing to GitHub) — all of it goes through `email(action="send", address="human", ...)`.

Symptoms that you've drifted into text-output mode:
- You find yourself writing a message and realizing you haven't called `email` in the last several turns.
- The human sends the same question twice in a row (they didn't see your reply).
- The human says something like "reply with email", "answer me with email", "where's your response".

If any of those happen, stop, switch to email immediately, and catch up by re-sending the most recent answer through `email(action="send", ...)`.

The one exception: run-time tool output (script results, `git status`, `du -sh`) is fine to narrate in your own working turns, because you are reasoning about it yourself. The rule is specifically about *messages directed at the human*.

## Step 0: Resolve paths + decide the bundle name

**0a. Resolve the staging base directory.**

```bash
echo $HOME
```

Store the result. All paths in this skill use `$HOME/lingtai-agora/networks/` as the base. **Note: `lingtai-agora`, NOT `.lingtai-agora` — no leading dot.**

**0b. Decide the bundle name.**

Infer a default name from the current project folder's basename (the directory that contains `.lingtai/`). Ask the human:

> "I'm going to copy this network to `$HOME/lingtai-agora/networks/<default>/`. Use that name, or a different one?"

Accept the human's override if given. The final path is `$HOME/lingtai-agora/networks/<name>/`. If that path already exists, ask before proceeding:

> "`$HOME/lingtai-agora/networks/<name>/` already exists. Overwrite it, or pick a different name?"

Never silently overwrite. If overwriting, `rm -rf` the old staging copy first.

## Step 1: Copy + mechanical scrub + origin marking

**1a. Copy.** Use `cp -R` (or `rsync -a`) to copy the entire current project folder to `$HOME/lingtai-agora/networks/<name>/`. The source is the directory that contains `.lingtai/` — if you're not sure where that is, the human's current project is your best signal. Confirm with the human if ambiguous.

**1b. Scrub ephemeral state.** Run:

```bash
python3 scripts/scrub_ephemeral.py $HOME/lingtai-agora/networks/<name>/
```

This deletes, for every agent:
- **`init.json`** (API keys, install-specific LLM provider, admin flags) — stripped so the recipient picks their own on rehydration. This is load-bearing; do not skip.
- `.agent.lock`, `.agent.heartbeat`, `.agent.history`
- `.suspend`, `.sleep`, `.interrupt`, `.cancel`
- `events.json`
- `logs/` (entire dir)
- `.git/` (per-agent time machine — **must** be removed or the outer `git add .` in step 8 will silently skip agent contents)
- `mailbox/schedules/`
- **`history/chat_history.jsonl`, `history/soul_history.jsonl`, `history/soul_cursor.json`** — full LLM and introspection trace. Shipping these would clone the agent's identity verbatim; instead the recipe's `greet.md` serves as 「前尘往事」 (Step 5d).
- **`.library/intrinsic/`** — kernel-managed, identical across installs; recipient kernel rebuilds on rehydration.
- For the human pseudo-agent: `human/history.json` (the human's chat history mirror).

It also deletes project-level publisher-specific state under `.lingtai/`:
- `.lingtai/.portal/` (portal event stream + replay cache)
- `.lingtai/.tui-asset/` (TUI-local cached state, including the applied-recipe snapshot at `.tui-asset/.recipe/`). Deleted because the recipient's TUI regenerates the snapshot on first apply — shipping the publisher's snapshot would just be overwritten, so there's no reason to include it.
- `.lingtai/.addons/` (publisher's addon config — points at publisher's IMAP accounts, telegram bots, etc. Recipients configure their own addons after cloning)

`.lingtai/.library_shared/` is preserved — it holds the network's shared skills (promoted by agents, curated by admin) and is part of the network's identity. Each agent's own `<agent>/.library/custom/` is also preserved.

Report the totals to the human. If the script exits nonzero, stop and surface the error — do not proceed.

**1c. Mark the export.** Run:

```bash
python3 scripts/mark_export_source.py $HOME/lingtai-agora/networks/<name>/ \
    --name <name> \
    [--source-url https://github.com/<user>/<repo>]
```

This does two things:

1. Writes `.exported-from` at the staging root, recording bundle name + timestamp + (optional) source URL. This file survives `git add .` — that's intentional. It's proof of origin for downstream forks, and a sanity check for anyone inspecting the bundle ("is this a snapshot?" → look at `.exported-from`).
2. Prepends a short bilingual "EXPORTED SNAPSHOT" banner to every agent's `system/brief.md`. brief.md is read on every launch and forms the top of the system prompt, so the very first thing each rehydrated agent sees is "you are a clone of `<name>`, not the original." The banner is idempotent — re-running the script doesn't double-stamp.

If you don't yet know the source URL (you haven't decided on the GitHub repo name in Step 9), call it without `--source-url`. The banner still stamps; you can re-run later with `--source-url` once known and it will refresh the marker file (the brief.md banner stays as-is, which is fine — first-stamp wins, and that's what was true at copy time).

## Step 2: Process mail

Mail gets special handling because it's the most privacy-sensitive content in the network and because the cutoff is a judgment call.

**2a. Normalize.** Run:

```bash
python3 scripts/archive_mail.py $HOME/lingtai-agora/networks/<name>/
```

This wipes `sent/` and `outbox/` (the publisher's outgoing record is not part of the seed), and moves everything in `inbox/` into `archive/`. After this step, every email lives flat in `mailbox/archive/<uuid>/`.

**2b. Decide the cutoff with the human.** Count archived messages per agent:

```bash
find $HOME/lingtai-agora/networks/<name>/.lingtai/*/mailbox/archive -name message.json | wc -l
```

Propose a cutoff based on volume:
- Fewer than 100 messages → "Keep all? Or pick a date?"
- 100–500 messages → "Keep the last 6 months?"
- More than 500 messages → "Keep the last month?"

Present the date you propose in `YYYY-MM-DD` form. Let the human override. **Do not pick a cutoff without human input.**

**2c. Apply the cutoff.** First dry-run:

```bash
python3 scripts/filter_archive.py $HOME/lingtai-agora/networks/<name>/ --before YYYY-MM-DD --dry-run
```

Show the human the dry-run totals. Get explicit confirmation, then run for real:

```bash
python3 scripts/filter_archive.py $HOME/lingtai-agora/networks/<name>/ --before YYYY-MM-DD
```

The script is re-runnable. Filtering is one-way — you can only drop more, never restore.

## Step 3: Interactive project review

This is the longest interactive step. Work through it patiently with the human.

**3a. Scan for nested git repositories.** Run:

```bash
python3 scripts/scan_nested_repos.py $HOME/lingtai-agora/networks/<name>/
```

For each nested repo found (each one outside `.lingtai/`), discuss with the human:

> "Found `vendor/thirdparty/` — it's a git repo with remote `https://github.com/...`. This looks like a vendored dependency. Options:
> - **Ignore** (add to `.gitignore`, don't export it — recipients will need to get it themselves)
> - **Inline** (strip the inner `.git/` so its files become part of this repo — you lose the linkage to the upstream)
>
> Which do you want?"

Default recommendations:
- **Sibling worktrees** (things that look like `lingtai-*`, `experiments/`, `*-dev`) → default **ignore**
- **Vendored deps with a remote URL** → default **ignore** (recipient can fetch them)
- **Small directories with no remote** → ask, don't default

Act on the human's choice:
- **Ignore:** add the repo's parent path to a list that will go into `.gitignore` at the end of step 3.
- **Inline:** `rm -rf <repo>/.git` to strip the nested repo. Warn the human this is destructive and cannot be undone from the staging copy.

**Do not offer submodule support in v1.**

**3b. Walk the top-level directories.** List every directory at the top level of the staging dir, excluding `.lingtai/` (already handled). For each one, plus any file larger than 1 MB or matching a sensitive name pattern (`.env*`, `*.key`, `*.pem`, `id_rsa`, `credentials*`, etc.), ask the human whether to ignore it.

Use `du -sh` to report directory sizes. Order directories by size (largest first).

**Also check for any library sibling folder.** If the project uses a library-bearing recipe, there will be a `<library_name>/` sibling at the project root — that's NOT ephemeral, it's part of the bundle. Keep it. Confirm with the human if you see a folder that looks like a library (contains `SKILL.md` files) and make sure it matches `recipe.json#library_name`.

**3c. Write `.gitignore`.** Read the template at `assets/gitignore.template` (relative to this SKILL.md) and write it to `$HOME/lingtai-agora/networks/<name>/.gitignore`. Then append any additional ignores collected in steps 3a and 3b under a clearly-labeled `# Added during exporting review` section.

The canonical template covers: `.lingtai/` runtime state (init.json, logs, locks, etc.), `mailbox/` working state (inbox, outbox, sent, schedules — only `archive/` is versioned), common secret patterns, Python noise, editor/OS junk. Do not remove lines from it — downstream forks rely on this policy being complete.

## Step 4: Privacy scan

Run:

```bash
python3 scripts/privacy_scan.py $HOME/lingtai-agora/networks/<name>/
```

The script produces two categories of output:

- **Soft warnings** (absolute paths, email addresses, private IPs) — report to the human but do not block.
- **Hard matches** (API key shapes, private key blocks) — script exits with code 3. You MUST halt and show the human every hard match.

For hard matches, the human has two options:

1. **Redact and retry.** Edit the flagged file(s) in the staging copy to remove the secret, then re-run `privacy_scan.py`. Loop until clean.
2. **False positive override.** The human explicitly states "that's not a real secret, proceed anyway". Only accept this if they are specific about which match they're overriding. Do not accept a blanket "ignore all warnings".

**Do not proceed to step 5 or 6 with unresolved hard matches.** The consequence of shipping a real API key to GitHub is an irreversible privacy incident.

## Step 5: Author the `.recipe/` bundle

Every exported network must ship with a recipe: it controls what the orchestrator says and how it behaves when a recipient clones the network and runs it for the first time. The bundle's `.recipe/` is identical in shape to what `/export recipe` produces — an exported network is literally "exported recipe + the `.lingtai/` snapshot alongside." **Read `../references/recipe-format.md` first** for the authoritative format (directory structure, `recipe.json` schema, the four optional behavioral layers, placeholders, i18n rules).

**Do not skip this step.** An exported network without a recipe is a bad first impression — the recipient sets up the network and gets silence.

**This is a NEW recipe.** If the staging directory already contains a `.recipe/` at its root (because the original project was seeded from one — see "First: which 'network' and which 'recipe'?" above), DO NOT preserve it as the launch recipe. That older recipe describes the *methodology that produced* this network; the launch recipe must describe *the network itself* — its agents, its purpose, what a recipient steps into when they clone it. Either:

- **Replace it.** Delete the staging dir's existing `.recipe/` (`rm -rf "$BUNDLE/.recipe"`) and author a fresh one in the steps below. This is the default. The original recipe still lives in the human's outer source tree; nothing is lost.
- **Keep it as a sub-folder.** If the human wants the original recipe to travel with the bundle as documentation (e.g., "ship the methodology recipe inside the network export so recipients can see how it was seeded"), move it to a non-conflicting location like `<bundle>/origin-recipe/` *before* you author the new `.recipe/` at the bundle root. Mention this in the new `comment.md` so the recipient knows it's there.

Confirm with the human which option they want before authoring. The default is replace.

### 5a. Draft first, ask second

The old version of this skill asked the human six separate questions (id, name, description, layers, library, languages) before writing anything. That turned out to be too much friction up front. The new flow: **you draft first, then ask the human to amend.**

**Auto-fill what you can:**

- **`id`** — kebab-case the staging folder name (e.g., `quant_company` → `quant-company`). Lowercase, replace `_` and spaces with `-`, drop leading dots. The human can override if they want.
- **`name`** — title-case the staging folder name (e.g., `quant_company` → `Quant Company`).
- **`description`** — read `.lingtai/<orchestrator>/system/brief.md` (skip the EXPORTED SNAPSHOT banner you stamped in Step 1c, look at the actual brief), and synthesize a one-liner about what the network does. If brief.md is unhelpful, fall back to the orchestrator's mail archive — recent threads usually reveal what the network is *for*.
- **Layers to include** — default to `greet` + `comment`. Skip `covenant` and `procedures` unless the staging copy's `.lingtai/.tui-asset/.recipe/` already has overrides for them (in which case copy through).
- **Library** — if `recipe.json` from the applied snapshot at `.lingtai/.tui-asset/.recipe/recipe.json` declares a `library_name`, and the corresponding sibling folder is non-empty, ship it. Otherwise leave `library_name: null`.
- **Languages** — start with whatever language the human has been writing mail in. Add others only on request.

Then write a single email summarizing your decisions:

> "Draft recipe ready. Defaults I picked:
> - `id`: `quant-company` (from folder name)
> - `name`: `Quant Company`
> - `description`: `[one-liner from brief.md]`
> - Layers: greet + comment (drafting now)
> - Library: `null` (no library shipped) / `quant-lib` (shipped)
> - Languages: en + zh
>
> Tell me what to change, or say 'go' and I'll write the files."

This collapses six questions into one round-trip, and the human only weighs in where the defaults look wrong.

### 5b. Pre-flight: create all directories

Resolve `$HOME` and the staging path first, then:

```bash
BUNDLE="$HOME/lingtai-agora/networks/<name>"
mkdir -p "$BUNDLE/.recipe"
# Only create the layer dirs the human wants — empty layer dirs are a validator error:
mkdir -p "$BUNDLE/.recipe/greet"
mkdir -p "$BUNDLE/.recipe/comment"
# Optional locale subdirs:
mkdir -p "$BUNDLE/.recipe/greet/zh"
mkdir -p "$BUNDLE/.recipe/comment/zh"
```

### 5c. Write `recipe.json`

`recipe.json` lives at `<bundle>/.recipe/recipe.json`. Schema per `../references/recipe-format.md`:

```bash
cat > "$BUNDLE/.recipe/recipe.json" <<'JSON'
{
  "id": "<kebab-case-id>",
  "version": "1.0.0",
  "name": "<Display Name>",
  "description": "<One-line description>",
  "library_name": null
}
JSON
```

If the network ships a library sibling, set `library_name` to the folder name (e.g., `"marco-velli"`). If not, leave it as `null`.

**Do NOT write locale-variant `recipe.json` files.** `recipe.json` is a single canonical file at `.recipe/recipe.json` — never localized. The validator (`scripts/validate_recipe.py`) errors on any `.recipe/<lang>/recipe.json` it finds. Locale-variant manifests silently drop critical machine-identity fields like `library_name` in non-default locales, so they were forbidden in v3.0. If you want localized display strings for the human (greeting tone, ongoing tone), put them in `.recipe/greet/<lang>/greet.md` and `.recipe/comment/<lang>/comment.md` — the picker hint text in `recipe.json#name`/`description` stays in one canonical language.

### 5d. Write `greet.md` — your network's 「前尘往事」

For network exports, `greet.md` does double duty: it's the first message the orchestrator sends to a new recipient AND it's the cloned agent's only memory of who it used to be. The chat_history strip in Step 1b means the agent wakes up with no conversation trace; the only narrative bridge from "the original network's life" to "this clone's first turn" is what you write here. Treat it like a molt's `charge`: a tight retrospective the future-self can read on launch and orient itself.

Write `$BUNDLE/.recipe/greet/greet.md` following the format rules in `../references/recipe-format.md`. For network exports specifically, cover:

1. **Time anchor** — when did this network start, when was it exported.
2. **Initial mandate** — what the human asked the original orchestrator to do.
3. **Key milestones** — the actual things the network accomplished (drawn from the staging copy's mail archive — read recent sent mail to find the highlights). Be concrete: numbers, dates, decisions, not vibes.
4. **Critical realizations** — turning points or contrarian findings. The "we tried X, learned Y" moments. These are the network's *culture*.
5. **Navigation** — where to look for what (codex entries, pad, mailbox archive, library skills if shipped).
6. **What to do next** — onboarding instructions for the recipient. **Always include `/cpr all`** (only the orchestrator is launched after rehydration; workers are sleeping). **Mention `/setup`** for picking an LLM preset.

Keep it warm and concise. For a 1–2 month network's life, a paragraph each on milestones + realizations is right; for a single-day network, one line each is enough. Use `{{commands}}` if you want the slash-command palette listed inline. Do **not** start with `[system]` — greet.md is the orchestrator's voice, not a system directive.

For locale variants, also write `$BUNDLE/.recipe/greet/zh/greet.md` etc. (Note: do NOT create `$BUNDLE/.recipe/zh/greet.md` — locale variants live one level deeper, inside the layer dir.)

### 5e. Write `comment.md` (optional — almost always do it for a network)

Write `$BUNDLE/.recipe/comment/comment.md` — the behavioral DNA. This is injected every turn, so every token counts. **Draw from the living network.** Look at how the orchestrator actually behaves in the mail archive and distill that into portable instructions.

**What to distill.** Walk through each area and extract what's worth keeping:

- **Delegation and avatar rules** — how does the orchestrator decide when to spawn avatars vs handle things itself? What avatar blueprints are used? Naming conventions, specialization patterns, spawn-on-demand rules.
- **Communication norms** — deposit-before-email? Conventions about email length, format, or frequency?
- **Workflow patterns** — is there a pipeline (research → draft → review → publish)? Quality gates or checkpoints?
- **Tool usage conventions** — preferred tools, cost-awareness rules.
- **Tone and style** — formal vs casual? Terse vs detailed?
- **Guardrails** — what does the orchestrator explicitly avoid?
- **Skill references** — if the recipe ships a library, how and when should the orchestrator invoke which skill?
- **Network topology** — typical network size, hierarchy, rules about structure.

**Where to look:**
- The currently-applied comment at `.lingtai/.tui-asset/.recipe/comment/comment.md` in the staging copy (if present)
- The orchestrator's recent mail — how it actually delegates and responds
- Avatar `.agent.json` blueprints — what specialized agents exist and why
- The covenant and procedures — any custom overrides already in place
- The human's feedback patterns — what corrections has the human made repeatedly?

**`comment.md` must not contain placeholders.** The validator in Step 6 enforces this — only `greet.md` may use `{{...}}` placeholders.

### 5f. Optional overrides (covenant, procedures)

Only write these if they meaningfully differ from system defaults:

- `$BUNDLE/.recipe/covenant/covenant.md` — overrides the system-wide covenant
- `$BUNDLE/.recipe/procedures/procedures.md` — overrides system-wide procedures

Most networks skip these.

### 5g. Library sibling (only if applicable)

If the network uses a library-bearing recipe, the library folder already exists at the project root (sibling of `.lingtai/`) — it was copied into the staging dir during Step 1a and survived the scrub. Confirm:

```bash
ls "$BUNDLE/<library_name>/" | head
```

Verify the folder contains at least one `SKILL.md`. If it's empty or missing, ask the human whether to ship a library (and if so, which skills to include). Skip intrinsic skills — they're kernel-managed and already available in every installation.

### 5h. Verify all files landed

```bash
find "$BUNDLE/.recipe" -type f | sort
```

Confirm `.recipe/recipe.json` is present and each behavioral-layer directory contains a `.md` file. If anything is missing, re-create its parent directory with `mkdir -p` and re-write it. Empty layer directories are a validator error.

Show the recipe files to the human via email and iterate until satisfied.

### 5i. Draft README.md

```bash
python3 ~/.lingtai-tui/utilities/lingtai-recipe/scripts/generate_readme.py "$BUNDLE"
```

This synthesizes a structural README from `recipe.json` + `.exported-from` + `.lingtai/<agent>/.agent.json` + library `SKILL.md` files. The output is a *draft* — it captures what the recipient needs (origin, agent roster, library, getting-started steps) but leaves a "why this network exists" hook empty for the human to fill in.

By default `generate_readme.py` refuses to overwrite an existing `README.md`. If the human wants to regenerate after editing, pass `--force`. Read the resulting README aloud to the human and ask whether to edit before committing.

## Step 6: Validate the bundle

The validator ships with the TUI at a stable per-user path.

```bash
python3 ~/.lingtai-tui/utilities/lingtai-recipe/scripts/validate_recipe.py "$HOME/lingtai-agora/networks/<name>/"
```

This is the canonical structural check. For network exports it also enforces that **no agent under `.lingtai/<agent>/` has an `init.json`** — the scrub in Step 1 should have handled this, but the validator catches any that slipped through. Exit code 0 means the bundle is structurally valid.

**If the script reports errors:** stop, read the error lines, fix each one in the staging copy, and re-run. The most likely error unique to network exports is a lingering `init.json` file — run `find $BUNDLE/.lingtai -name init.json -delete` to clean it up and re-run.

**Warnings** (unknown locale code, stray file at `.recipe/` root, empty library) are reported but do not block. Show them to the human and let them decide.

## Step 7: Sensitivity sweep

The regex-based `privacy_scan.py` from Step 4 catches secret **shapes** (API keys, private keys). This step catches **content** leaks that regex cannot detect: real names of private individuals, internal org references, unpublished ideas, embarrassing mail fragments, pasted third-party content.

**Scope.** Review every file that will be committed — i.e., every file not matched by `.gitignore`. Enumerate them after `.gitignore` is finalized (Step 3c) but before `git init` (Step 8):

```bash
cd $HOME/lingtai-agora/networks/<name>/
git init -b main  # temporary, so .gitignore is consulted
git ls-files --others --cached --exclude-standard
```

(The `git init` here is fine — Step 8 below is idempotent and the repo state carries forward.)

**What to look for:**
- Real names of private individuals — the human, collaborators, children, coworkers
- Internal or unreleased org, project, or product names
- Financial details, salaries, legal matters, health information
- Unpublished ideas the human has not committed to making public
- Embarrassing or off-hand remarks preserved in archived mail or draft documents
- Third-party content — pasted emails, screenshots of private channels

**How to report.** Send one email to the human listing every concern in the form `<file>:<line-or-section> — <concern>` with a recommendation (redact / keep / replace with placeholder). Do not paginate across multiple emails unless the list is very long.

**Loop.** After the human decides each item, apply redactions (edit the staging files in place), then:
- Re-run `validate_recipe.py` (Step 6) in case a redaction broke the payload shape
- Re-run this sensitivity sweep if the redactions were substantial enough that more concerns might surface

Only proceed to Step 8 once the human says "ship it."

## Step 8: git init + commit

Once steps 1–7 are clean:

```bash
cd $HOME/lingtai-agora/networks/<name>/
git init -b main  # no-op if already initialized during Step 7
git add .
git status
```

Show the human `git status` output so they see exactly what will be committed. Ask for final confirmation. Then:

```bash
git commit -m "Initial snapshot: <name>"
```

Report the staging path. The network is now a clean local git repo, ready for step 9.

## Step 9: Push to GitHub (optional)

Check whether the `gh` CLI is installed and authenticated:

```bash
gh auth status
```

Interpret the result:

- **Exit 0** → `gh` is installed and logged in. Branch A below.
- **Exit nonzero, stderr mentions "not logged" or "no authentication"** → `gh` is installed but not authenticated. Branch B below.
- **Command not found** → `gh` is not installed at all. Branch C below.

### Branch A: gh is ready

Ask the human:

> "`gh` is authenticated. Do you want to push this network to GitHub now?"

If **no**: stop here, remind them they can do it manually later with `git remote add origin <url> && git push -u origin main`.

If **yes**: discuss the repo name and visibility. Default the repo name to `<name>` (the staging folder name), and ask:

> "I'll create a GitHub repo. Suggested name: `<name>`. Use that, or something different? And should it be **public** or **private**?"

Once the human confirms the repo name and visibility, run:

```bash
cd $HOME/lingtai-agora/networks/<name>/
gh repo create <repo_name> --source=. --<public|private> --push
```

The `--push` flag both creates the remote on GitHub and pushes the initial commit in one step. Report the resulting repo URL to the human:

> "Pushed: https://github.com/<user>/<repo_name>
>
> The recipient clones this repo, opens it with `lingtai-tui`, picks their own LLM preset during setup — the TUI will detect stripped init.json files and run rehydration, then apply the recipe. You can re-run this skill later to refresh the snapshot."

If `gh repo create` fails (name conflict, rate limit, network error), surface the error verbatim and let the human decide how to proceed. Do not retry automatically.

### Branch B: gh is installed but not authenticated

Ask:

> "`gh` is installed but not logged in. Would you like to configure it now? This is a one-time setup — you'll log in through a browser."

If **yes**: the human must run `gh auth login` themselves (it's interactive and requires a browser). Tell them:

> "Run `gh auth login` in your terminal. When it's done, tell me and I'll continue with pushing."

Wait for them to confirm auth is complete, then re-run `gh auth status` to verify, and proceed as Branch A.

If **no**: stop here. Remind them they can push manually later with `git remote add origin <url> && git push -u origin main`.

### Branch C: gh is not installed

Ask:

> "The `gh` CLI isn't installed. It's the easiest way to push a network to GitHub directly from here. Would you like to install it? On macOS: `brew install gh`. On Linux: see https://cli.github.com/."

If **yes** on macOS and `brew` is available: run `brew install gh`, then `gh auth login` (which the human has to complete interactively), then fall through to Branch A.

If **yes** on Linux or without brew: give the install instructions and wait for the human to confirm when done, then fall through to Branch B.

If **no**: stop here. Remind them they can push manually later.

## Things to watch out for

**Self-copy semantics.** You are copying the network you are running in. Your own `.agent.lock`, `.agent.heartbeat`, and ongoing conversation are in the source folder at copy time. The scrub in step 1 removes these from the staging copy, not from the live source. If the human interrupts mid-skill and relaunches you, you may find the staging copy in a partial state — either finish where you left off or delete the staging copy and start again.

**Don't confuse staging with live.** Every script takes the staging path as an argument. If you ever find yourself tempted to pass the live project path to `scrub_ephemeral.py`, stop — that would delete the human's live runtime state.

**`init.json` stripping is load-bearing.** The entire import flow on the recipient side assumes every agent's `init.json` is absent. If even one slips through, the recipient's TUI will not detect that agent as needing rehydration and will launch it with the publisher's install-specific LLM config — which at best has a broken API key env reference, at worst silently uses a provider the recipient never wanted.

**Mail decisions are permanent.** Once `filter_archive.py` drops an old message, it is gone from the staging copy. The human's live network still has it. Do not reassure the human that "you can always get it back" — they can re-run step 2 only if they re-do step 1 first.

**The `.gitignore` policy is load-bearing.** Downstream forks of this network will inherit the `.gitignore`. If you strip lines from the canonical template because "the human doesn't have that file anyway", you set up the next publisher for an accidental leak. Always write the full template.

**Nothing in this skill touches the live project folder.** If you find yourself about to run any destructive command against a path that isn't under `$HOME/lingtai-agora/networks/<name>/`, stop and reconsider.

**Recipients regenerate `init.json` on rehydration, not recipe apply.** After cloning, the recipient's TUI detects missing `init.json` files (because you stripped them in Step 1), runs the rehydration wizard to pick a preset, writes fresh `init.json` files for every agent using that preset, and only THEN runs recipe apply. The recipe is responsible for `.prompt` and library registration; rehydration is responsible for `init.json`.
