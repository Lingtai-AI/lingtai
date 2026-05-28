---
name: claude-code
description: >
  Delegate code implementation, patch writing, documentation, and refactoring to
  Claude Code CLI (Anthropic's coding agent). Runs non-interactively from bash,
  uses the human's Claude Max subscription (no additional API costs), and supports
  quality/effort/budget controls. Use this when you need to write code, generate
  patches, refactor files, create documentation, or do any multi-file code work
  that would be faster delegated than done manually.
version: 1.0.1
tags: [cli, code, delegation, claude, implementation]
---

# Claude Code CLI — Code Delegation

Delegate code work to [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — Anthropic's coding agent — running non-interactively from bash.

## Prerequisites

- Claude Code installed: `which claude` → `/Users/huangzesen/.local/bin/claude`
- Uses the human's **Claude Max subscription** — no additional API costs
- Rate limit tier: `default_claude_max_20x` (effectively unlimited for typical use)

## Quick Usage

```bash
env \
  -u CLAUDE_CODE_OAUTH_TOKEN \
  -u ANTHROPIC_API_KEY \
  -u ANTHROPIC_AUTH_TOKEN \
  -u ANTHROPIC_BASE_URL \
  -u ANTHROPIC_MODEL \
  -u ANTHROPIC_SMALL_FAST_MODEL \
  claude -p "your prompt here" --dangerously-skip-permissions
```

This runs Claude Code in non-interactive mode (`-p` = print and exit), skipping permission checks for automation.

### Weekly-limit smoke test

If `claude` reports `You've hit your weekly limit` from inside LingTai but the human recently refreshed Claude Code OAuth credentials, first rule out a stale inherited env token before concluding the subscription is truly exhausted:

```bash
# Do not print token values. This only removes the stale override for the child.
env -u CLAUDE_CODE_OAUTH_TOKEN claude -p 'Reply exactly OK' --allowedTools Read -c
```

If this succeeds while plain `claude -p ...` fails, use the sanitized `env -u ...` wrapper above (and prefer the daemon `claude-code` backend, which strips the override automatically).

> **Why the `env -u …` prefix?** If `ANTHROPIC_API_KEY` (or related `ANTHROPIC_*` variables) is set in the agent environment, the `claude` CLI **prefers the API-key billing path over the Claude Max subscription/OAuth token**. That path can fail with `Credit balance is too low` and bills the API key instead of using the subscription. Separately, a stale inherited `CLAUDE_CODE_OAUTH_TOKEN` can override a refreshed `~/.claude/.credentials.json` and make Claude Code falsely report `You've hit your weekly limit`. Unsetting these variables for the child process forces Claude Code onto the current first-party OAuth/subscription credentials. If you've confirmed your environment has no auth overrides, you can drop the `env -u …` prefix; when in doubt, keep it. **Never echo the variable values while diagnosing — they are secrets.**

### Find and remove the stale-token source

The smoke test above proves a child process can work when the bad override is removed. To make the fix durable, find where the variable is being exported and remove or comment out that source. Common places are shell startup files (`~/.zshrc`, `~/.zprofile`, `~/.bashrc`, `~/.bash_profile`) or launch-service environment configuration.

Safe diagnostic commands:

```bash
# 1. Check whether macOS launchd is injecting it. Do not print token values.
if launchctl getenv CLAUDE_CODE_OAUTH_TOKEN >/dev/null 2>&1; then
  echo "launchctl may define CLAUDE_CODE_OAUTH_TOKEN"
fi

# 2. Search shell startup files for the variable name, not the value.
grep -n 'CLAUDE_CODE_OAUTH_TOKEN\|ANTHROPIC_API_KEY\|ANTHROPIC_AUTH_TOKEN' \
  ~/.zshenv ~/.zprofile ~/.zshrc ~/.bash_profile ~/.bashrc ~/.profile 2>/dev/null

# 3. Verify a clean future shell does not recreate the variable.
env -u CLAUDE_CODE_OAUTH_TOKEN /bin/zsh -lc \
  'test -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" && echo NOT_SET || echo STILL_SET'
```

If the variable is hard-coded in a shell startup file, comment out only that export line and keep a backup. A plain `claude` process can then use Claude Code's own refreshed local OAuth credentials instead of a stale environment override. Already-running LingTai agents may still have inherited the old environment until they are refreshed or restarted; for those current processes, keep using the `env -u ...` child-process wrapper.

## CLI vs Daemon — Which to Use

LingTai exposes Claude Code in two forms. They are **not interchangeable** — pick the one whose shape matches the work.

### CLI (`claude -p ...` via bash)

A single synchronous subprocess. You wait for it to finish, you get one transcript, the conversation ends when the bash call returns.

**Use the CLI when:**
- The task is **one-off** and you want the result inline — a typo fix, a single-file refactor, generating a snippet
- You want the output **threaded back into your current reasoning** (you'll read the diff and decide next steps yourself)
- The task is **quick** (under a few minutes) and budget-bounded
- You only need **one** of these running at a time

**Examples:**
```bash
# Fix a typo
claude -p "fix the typo in line 42 of README.md" --dangerously-skip-permissions

# Generate a small patch you'll review immediately
claude -p "add a --verbose flag to scripts/build.sh" --dangerously-skip-permissions

# Quick documentation pass on one file
claude -p "add docstrings to utils/parser.py" --dangerously-skip-permissions
```

### Daemon (LingTai `daemon` capability with `backend="claude-code"`)

A persistent agent spawned by the LingTai kernel. Runs in its **own worktree**, with its **own context window**, on its **own branch**. You dispatch it, it works asynchronously, you come back and review the diff.

**Use the daemon when:**
- You need to run **multiple tasks in parallel** — three disjoint refactors at once, batch validation across N files
- The task is **complex or multi-step** — designing then implementing, exploring then refactoring — and you want a fresh context window dedicated to it (not competing with your conversation history)
- You need **context isolation** — the daemon shouldn't see (and shouldn't pollute) your current session's context
- The work runs **long enough** that a synchronous bash call would be awkward — large refactors, multi-file feature implementations, PR composition
- You're acting as an **orchestrator** — planning and reviewing, not hand-coding (see the LingTai contributing guide's orchestrator-and-daemons discipline)

**Examples of daemon-shaped work:**
- "Implement the caching layer in `feat/cache` branch, with tests, and open a PR" — long, multi-step, deserves its own worktree
- Three parallel skill rewrites that don't share files — dispatch three daemons, review three diffs
- An exploratory refactor where you don't want intermediate steps cluttering your conversation
- A "fire-and-check-back-later" task

### Quick decision rule

| Signal | Pick |
|--------|------|
| "I want the answer in this conversation, now" | **CLI** |
| "I want to do three of these at once" | **Daemon** (one per task) |
| "I'll review a diff afterward, not the transcript" | **Daemon** |
| "The output is a small string/snippet I'll paste somewhere" | **CLI** |
| "This will take 15+ minutes and produce a branch" | **Daemon** |
| "I'm the orchestrator; the daemon is the worker" | **Daemon** |

When in doubt for non-trivial work: daemon. The orchestrator/daemon split is the project's default discipline — see `utilities/lingtai-dev-guide/reference/contributing.md` for the full convention.

## Key Flags

| Flag | Purpose |
|------|---------|
| `-p` / `--print` | Non-interactive mode — run, print result, exit |
| `--dangerously-skip-permissions` | Skip permission prompts (required for automation) |
| `--effort max` | Maximum reasoning effort for complex tasks |
| `--model opus` | Use Opus model for highest quality |
| `--model sonnet` | Use Sonnet model for speed (default) |
| `--max-budget-usd N` | Spending limit per call |
| `--allowedTools "Bash Edit Read Write"` | Restrict which tools Claude can use |
| `--system-prompt "..."` | Custom system prompt |
| `--add-dir /path/to/dir` | Grant access to additional directories |
| `-d /path/to/repo` | Set working directory |

## Recommended Patterns

### Simple task (default quality)
```bash
claude -p "fix the typo in README.md" --dangerously-skip-permissions
```

### Complex implementation (max quality)
```bash
claude -p "implement the caching layer as described in DESIGN.md" \
  --dangerously-skip-permissions \
  --effort max \
  --model opus
```

### With budget control
```bash
claude -p "refactor the auth module" \
  --dangerously-skip-permissions \
  --effort max \
  --model opus \
  --max-budget-usd 5.0
```

### Working in a specific repo
```bash
claude -p "add unit tests for the parser module" \
  --dangerously-skip-permissions \
  -d /path/to/repo
```

### Restricted tools (safer)
```bash
claude -p "generate a patch for issue #42" \
  --dangerously-skip-permissions \
  --allowedTools "Bash Edit Read Write"
```

## Best Practices

1. **Increase bash timeout**: Set `timeout=900` (15 minutes) on the bash tool call for complex tasks. Claude Code has no built-in timeout — the bash tool's timeout controls it.

2. **Use `--effort max` for complex work**: This tells Claude to think harder. Worth it for architecture, refactoring, and multi-file changes.

3. **Use `--model opus` for quality**: Opus produces better code for complex logic. Use Sonnet (default) for simple tasks.

4. **Split large tasks**: Multiple smaller `claude -p` calls chained together beat one monolithic prompt. Each call has its own context window.

5. **Write clear prompts**: Claude Code reads the repo context itself. Give it the goal, constraints, and acceptance criteria — don't dump the entire codebase into the prompt.

6. **Set budget for unknown tasks**: Use `--max-budget-usd` to prevent runaway spending on ambiguous tasks.

## Workflow for Patch/PR Creation

1. **Design**: Write a clear spec (what to change, why, constraints)
2. **Delegate**: Run `claude -p "implement: <spec>" --dangerously-skip-permissions --effort max`
3. **Review**: Check the output, run tests
4. **Push**: Create branch, commit, push as PR

## What to Delegate

- **Code implementation**: New features, bug fixes, refactoring
- **Patch generation**: Multi-file changes, API migrations
- **Documentation**: READMEs, docstrings, API docs
- **Test writing**: Unit tests, integration tests
- **Code review**: Ask Claude to review a PR or diff

## What NOT to Delegate

- **Simple one-line edits**: Use the `edit` tool directly
- **File reading/searching**: Use `read`/`grep`/`glob` directly
- **Shell commands**: Use `bash` directly for non-code tasks
- **Tasks requiring your full context**: Claude Code doesn't share your conversation history

## Troubleshooting

| Issue | Fix |
|-------|-----|
| Timeout after 30s | Increase `timeout=900` on bash call |
| Claude Code not found | Check `which claude` → `/Users/huangzesen/.local/bin/claude` |
| Permission errors | Always include `--dangerously-skip-permissions` |
| Output truncated | Check if Claude hit the budget limit |
| Rate limited | Wait and retry; Max tier has generous limits |
| `Credit balance is too low` despite Claude Code subscription/OAuth being authenticated | `ANTHROPIC_API_KEY` (or another `ANTHROPIC_*` variable) is set and is overriding the OAuth/subscription path. Wrap the call with `env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN -u ANTHROPIC_BASE_URL -u ANTHROPIC_MODEL -u ANTHROPIC_SMALL_FAST_MODEL claude …` so the child process uses the OAuth/subscription path. Do **not** print the variable values while diagnosing — only their presence/length. |

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
