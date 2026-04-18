---
name: lingtai-export-network
description: Export the current lingtai network for sharing. Copies the network to $HOME/lingtai-agora/networks/<name>/, scrubs ephemeral runtime state, processes mail with a user-chosen time cutoff, writes a canonical .gitignore, scans for nested git repos and leaked secrets, and initializes a clean git repository ready to push. Use when the human asks you to share, export, package, or back up this network for others.
version: 2.0.0
---

# lingtai-export-network: Exporting a Network

**Prerequisites:** Read the `lingtai-recipe` skill first — Step 5 of this skill creates a launch recipe for the exported network, and you need to understand recipe structure, components, and placeholders. Also read `lingtai-export-recipe` if the human wants to export a standalone recipe alongside the network.

You are about to copy the network you live in to an exportable location. **This is literal self-copying** — the snapshot will not contain the moment you made it. Everything up to this conversation turn will be in the exported copy; this turn itself will only exist in the original.

Walk the human through the steps below carefully. Each step is either *mechanical* (run a script, report the result) or *interactive* (discuss a decision with the human before proceeding). Never skip the interactive steps — the whole point of a skill-driven exporting flow is that a human is in the loop for judgment calls.

All scripts live alongside this SKILL.md under `scripts/`. The canonical `.gitignore` template lives at `assets/gitignore.template`. You run the scripts with `python3 <path-to-script> ...`; resolve the absolute path from this skill's location in `.lingtai/.library/lingtai-export-network/`.

## Critical: Filesystem Rules

These rules prevent silent failures. Follow them without exception.

1. **Resolve `$HOME` first.** The `write` tool does NOT expand `~`. At the start of this skill, run:
   ```bash
   echo $HOME
   ```
   Use the result (e.g., `/Users/alice`) as the prefix for ALL file paths. Never use `~` in a `write` or `file` tool call.

2. **Always use absolute paths.** Every `write` call must use a full absolute path. The `write` tool resolves relative paths from your working directory, not from the staging directory.

3. **Always `mkdir -p` before writing.** The `write` tool may silently fail or report false success if the parent directory does not exist. Before writing any file, create its parent directory with `bash`.

4. **Verify after writing.** After writing files, run `find <dir> -type f | sort` to confirm all files landed. If any file is missing, re-create its parent directory and re-write it.

5. **Never trust a write success message at face value.** The write tool can report success even when the file was not created (e.g., missing parent directory). Always verify with `find` or `ls`.

## How to talk to the human during this skill

**Use the `email` tool for every message to the human. Never rely on text output.**

This is a multi-round conversation with real latency between turns. The human may not be watching their terminal, the portal, or any specific surface — they will see your messages reliably only through their inbox. Every question you need answered, every status update the human needs to know, every final confirmation before a destructive or externally-visible action (deleting files, pushing to GitHub) — all of it goes through `email(action="send", address="human", ...)`.

Symptoms that you've drifted into text-output mode:
- You find yourself writing a message and realizing you haven't called `email` in the last several turns.
- The human sends the same question twice in a row (they didn't see your reply).
- The human says something like "reply with email", "answer me with email", "where's your response".

If any of those happen, stop, switch to email immediately, and catch up by re-sending the most recent answer through `email(action="send", ...)`. Don't argue about the channel — just fix it.

The one exception: run-time tool output (script results, `git status`, `du -sh`) is fine to narrate in your own working turns, because you are reasoning about it yourself. The rule is specifically about *messages directed at the human*.

## Step 0: Resolve paths + decide the project name

**0a. Resolve the staging base directory.**

```bash
echo $HOME
```

Store the result. All paths in this skill use `$HOME/lingtai-agora/networks/` as the base. For example, if `$HOME` is `/Users/alice`, the base is `/Users/alice/lingtai-agora/networks/`. **Note: `lingtai-agora`, NOT `.lingtai-agora` — no leading dot.** The agora directory is a user-visible workspace, not a hidden config directory. Never use `~` in tool calls — always use the resolved absolute path.

**0b. Decide the project name.**

Infer a default name from the current project folder's basename (the directory that contains `.lingtai/`). Ask the human:

> "I'm going to copy this network to `$HOME/lingtai-agora/networks/<default>/`. Use that name, or a different one?"

Accept the human's override if given. The final path is `$HOME/lingtai-agora/networks/<name>/`. If that path already exists, ask before proceeding:

> "`$HOME/lingtai-agora/networks/<name>/` already exists. Overwrite it, or pick a different name?"

Never silently overwrite. If overwriting, `rm -rf` the old staging copy first.

## Step 1: Copy + mechanical scrub

**1a. Copy.** Use `cp -R` (or `rsync -a`) to copy the entire current project folder to `$HOME/lingtai-agora/networks/<name>/`. The source is the directory that contains `.lingtai/` — if you're not sure where that is, the human's current project is your best signal. Confirm with the human if ambiguous.

**1b. Scrub ephemeral state.** Run:

```bash
python3 scripts/scrub_ephemeral.py $HOME/lingtai-agora/networks/<name>/
```

This deletes, for every agent:
- `init.json` (API keys, absolute paths)
- `.agent.lock`, `.agent.heartbeat`, `.agent.history`
- `.suspend`, `.sleep`, `.interrupt`, `.cancel`
- `events.json`
- `logs/` (entire dir)
- `.git/` (per-agent time machine — **must** be removed or the outer `git add .` in step 5 will silently skip agent contents)
- `mailbox/schedules/`

It also deletes project-level publisher-specific state under `.lingtai/` itself:
- `.lingtai/.portal/` (portal event stream + replay cache — `topology.jsonl` can reach hundreds of MB and leaks the timeline)
- `.lingtai/.tui-asset/` (TUI-local cache, regenerated on launch)
- `.lingtai/.addons/` (publisher's addon config — points at publisher's IMAP accounts, telegram bots, etc. Recipients configure their own addons after cloning)

`.lingtai/.library/` is preserved — it holds canonical skills (bundled + user-added), which are part of the network's identity and belong in the exported copy.

Report the totals to the human. If the script exits nonzero, stop and surface the error — do not proceed.

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

Present the date you propose in `YYYY-MM-DD` form. Let the human override. **Do not pick a cutoff without human input** — this is the main judgment call in the whole flow.

**2c. Apply the cutoff.** First dry-run:

```bash
python3 scripts/filter_archive.py $HOME/lingtai-agora/networks/<name>/ --before YYYY-MM-DD --dry-run
```

Show the human the dry-run totals (how many old messages drop, how many malformed messages drop, how many kept). Get explicit confirmation, then run for real:

```bash
python3 scripts/filter_archive.py $HOME/lingtai-agora/networks/<name>/ --before YYYY-MM-DD
```

The script is re-runnable. If the human wants a different cutoff later, just re-run with the new date — filtering is one-way (you can only drop more, never restore).

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

**Do not offer submodule support in v1** — it requires a reachable remote, writing `.gitmodules`, and a working `git submodule add`. If the human asks for it, explain that they can add it manually after the skill finishes.

**3b. Walk the top-level directories.** List every directory at the top level of the staging dir, excluding `.lingtai/` (already handled). For each one, plus any file larger than 1 MB or matching a sensitive name pattern (`.env*`, `*.key`, `*.pem`, `id_rsa`, `credentials*`, etc.), ask the human whether to ignore it.

Use `du -sh` to report directory sizes. Order directories by size (largest first) — that usually matches the priority of what needs discussing.

Examples of the kind of conversation this step produces:

> - "`data/raw/` — 147 MB, 2,341 files. Looks like a dataset. Ignore? [Y/n]"
> - "`.env` — 420 bytes. Almost certainly secrets. Adding to `.gitignore` unless you object."
> - "`notebooks/` — 3.2 MB, 12 notebooks. Could be valuable context. Keep? [Y/n]"
> - "`models/` — 4.1 GB. That's a lot. Ignore? [Y/n]"

**3c. Write `.gitignore`.** Read the template at `assets/gitignore.template` (relative to this SKILL.md) and write it to `$HOME/lingtai-agora/networks/<name>/.gitignore`. Then append any additional ignores collected in steps 3a and 3b under a clearly-labeled `# Added during exporting review` section.

The canonical template covers: `.lingtai/` runtime state (init.json, logs, locks, etc.), `mailbox/` working state (inbox, outbox, sent, schedules — only `archive/` is versioned), common secret patterns, Python noise, editor/OS junk. Do not remove lines from it — downstream forks rely on this policy being complete.

## Step 4: Privacy scan

Run:

```bash
python3 scripts/privacy_scan.py $HOME/lingtai-agora/networks/<name>/
```

The script produces two categories of output:

- **Soft warnings** (absolute paths, email addresses, private IPs) — report to the human but do not block. These are often legitimate (a blog post mentioning `/home/user/` is fine).
- **Hard matches** (API key shapes, private key blocks) — script exits with code 3. You MUST halt and show the human every hard match.

For hard matches, the human has two options:

1. **Redact and retry.** Edit the flagged file(s) in the staging copy to remove the secret, then re-run `privacy_scan.py`. Loop until clean.
2. **False positive override.** The human explicitly states "that's not a real secret, proceed anyway". Only accept this if they are specific about which match they're overriding. Do not accept a blanket "ignore all warnings".

**Do not proceed to step 5 or 6 with unresolved hard matches.** The consequence of shipping a real API key to GitHub is an irreversible privacy incident — there is no cleanup, only key rotation.

## Step 5: Author the `.lingtai-recipe/` payload

Every exported network must ship with a launch recipe: it controls what the orchestrator says and how it behaves when a recipient clones the network and runs it for the first time. The payload shape is identical to what `/export recipe` produces — an exported network is literally "exported recipe + the `.lingtai/` state folder alongside it." **Read the `lingtai-recipe` skill first** for the authoritative format (directory structure, components, placeholders, i18n rules, `recipe.json` manifest).

**Do not skip this step.** An exported network without a recipe is a bad first impression — the recipient sets up the network and gets silence.

### 5a. Discuss the recipe with the human

> "Every exported network needs a launch recipe for new users. I'll draft the files. To do that, I need a few things:
>
> 1. **Recipe name** — a short name that captures this network's essence
> 2. **One-line description** — what does a recipient get by opening this network?
> 3. **Greeting style** — tone for the first-contact message (warm, terse, playful, formal, …)
> 4. **Any behavioral constraints** — what should carry over from this network's culture?
>
> Answer as many as you can in one message and I'll draft everything in one pass."

Use the staging copy's mail archive, agent names, and `.agent.json` blueprints to understand the network's purpose before drafting.

### 5b. Pre-flight: create all directories

Resolve `$HOME` and the staging path first, then:

```bash
REPO_DIR="$HOME/lingtai-agora/networks/<name>"
RECIPE_DIR="$REPO_DIR/.lingtai-recipe"
mkdir -p "$RECIPE_DIR/en"
```

If the human wants additional language variants, also `mkdir -p "$RECIPE_DIR/zh"` etc.

### 5c. Write `recipe.json` at the repo root

`recipe.json` lives at the **repo root**, not inside `.lingtai-recipe/`. This matches the canonical format and is how the TUI's recipe auto-discovery finds the payload.

```bash
cat > "$REPO_DIR/recipe.json" <<'JSON'
{
  "name": "<Recipe Name>",
  "description": "<One-line description>"
}
JSON
```

### 5d. Write `greet.md`

Write `$RECIPE_DIR/en/greet.md` following the rules in `lingtai-recipe` (first person, short, use `{{time}}` / `{{location}}` / `{{addr}}` placeholders if natural). Key points for network exports:

- **Always include `/cpr all`** — after rehydration, only the orchestrator is launched; other agents are sleeping
- Keep it warm but concise — introduce the network, offer guidance, explain how to start
- Do **not** start with `[system]` — greet.md is the orchestrator's voice, not a system message

### 5e. Write `comment.md`

Write `$RECIPE_DIR/en/comment.md` — the behavioral DNA. This is injected every turn, so every token counts. **Draw from the living network.** Look at how the orchestrator actually behaves in the mail archive and distill that into portable instructions.

**What to distill.** Walk through each area and extract what's worth keeping:

- **Delegation and avatar rules** — how does the orchestrator decide when to spawn avatars vs handle things itself? What avatar blueprints are used? Naming conventions, specialization patterns, spawn-on-demand rules.
- **Communication norms** — deposit-before-email (write findings to a file before sending a summary)? Conventions about email length, format, or frequency?
- **Workflow patterns** — is there a pipeline (research → draft → review → publish)? Quality gates or checkpoints?
- **Tool usage conventions** — preferred tools, cost-awareness rules.
- **Tone and style** — formal vs casual? Terse vs detailed?
- **Guardrails** — what does the orchestrator explicitly avoid?
- **Skill references** — if the recipe ships skills, how and when should the orchestrator invoke them?
- **Network topology** — typical network size, hierarchy, rules about structure.

**Where to look:**
- The current `comment.md` in the staging copy (if any)
- The orchestrator's recent mail — how it actually delegates and responds
- Avatar `.agent.json` blueprints — what specialized agents exist and why
- The covenant and procedures — any custom overrides already in place
- The human's feedback patterns — what corrections has the human made repeatedly?

**`comment.md` must not contain placeholders.** `{{time}}`, `{{addr}}`, `{{lang}}`, `{{location}}`, `{{soul_delay}}` are only valid in `greet.md`. The validator in Step 6 enforces this.

### 5f. Optional components

Only write these if they meaningfully differ from system defaults:

- `$RECIPE_DIR/covenant.md` — overrides the system-wide covenant for agents in this network
- `$RECIPE_DIR/procedures.md` — overrides system-wide procedures
- `$RECIPE_DIR/skills/<skill-name>/SKILL.md` — recipe-shipped skills. Copy custom skills from `.lingtai/.library/custom/` in the staging copy. **Skip intrinsic skills** — they are shipped with the TUI and already available everywhere.

### 5g. Multi-language variants (optional)

For each additional language the recipe should support, create `$RECIPE_DIR/<lang>/` with its own `greet.md` and `comment.md`. Known lang codes: `en`, `zh`, `wen`. See `lingtai-recipe` for i18n fallback rules.

### 5h. Verify all files landed

```bash
find $HOME/lingtai-agora/networks/<name>/ -type f -path "*recipe*" | sort
```

Confirm `recipe.json` is at the repo root and `greet.md`/`comment.md` are at `.lingtai-recipe/<lang>/`. If anything is missing, re-create its parent directory with `mkdir -p` and re-write it.

Show both recipe files to the human and iterate until satisfied.

## Step 6: Validate the recipe payload

Run:

```bash
python3 .lingtai/.library/intrinsic/lingtai-recipe/scripts/validate_recipe.py $HOME/lingtai-agora/networks/<name>/
```

This is the canonical structural check. It verifies `recipe.json`, the presence of `greet.md`/`comment.md`, absence of forbidden placeholders in `comment.md`/`covenant.md`/`procedures.md`, skill frontmatter, and more. Exit code 0 means the payload is structurally valid.

**If the script reports errors:** stop, read the error lines, fix each one in the staging copy, and re-run. Loop until clean.

**Warnings** (e.g., unknown lang code, stray file at `.lingtai-recipe/` root) are reported but do not block. Show them to the human and let them decide whether to address.

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

**How to report.** Send one email to the human listing every concern in the form `<file>:<line-or-section> — <concern>` with a recommendation (redact / keep / replace with placeholder). Do not paginate across multiple emails unless the list is very long — one message is easier for the human to scan and reply to.

**Loop.** After the human decides each item, apply redactions (Edit the staging files in place), then:
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
> You can re-run this skill later to refresh the snapshot — commits to this local repo can be pushed with `git push` from `$HOME/lingtai-agora/networks/<name>/`."

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

If **no**: stop here. Remind them they can push manually later with `git remote add origin <url> && git push -u origin main`.

## Things to watch out for

**Self-copy semantics.** You are copying the network you are running in. Your own `.agent.lock`, `.agent.heartbeat`, and ongoing conversation are in the source folder at copy time. The scrub in step 1 removes these from the staging copy, not from the live source. If the human interrupts mid-skill and relaunches you, you may find the staging copy in a partial state — either finish where you left off or delete the staging copy and start again.

**Don't confuse staging with live.** Every script takes the staging path as an argument. If you ever find yourself tempted to pass the live project path to `scrub_ephemeral.py`, stop — that would delete the human's live runtime state.

**Mail decisions are permanent.** Once `filter_archive.py` drops an old message, it is gone from the staging copy. The human's live network still has it. Do not reassure the human that "you can always get it back" — they can re-run step 2 only if they re-do step 1 first.

**The `.gitignore` policy is load-bearing.** Downstream forks of this network will inherit the `.gitignore`. If you strip lines from the canonical template because "the human doesn't have that file anyway", you set up the next publisher for an accidental leak. Always write the full template.

**Nothing in this skill touches the live project folder.** If you find yourself about to run any destructive command against a path that isn't under `$HOME/lingtai-agora/networks/<name>/`, stop and reconsider.
