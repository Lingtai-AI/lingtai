---
name: opencode
description: >
  Nested swiss-knife reference for OpenCode CLI. Manual (not a tool) for OpenCode CLI — an open-source terminal coding agent
  that runs locally, supports 75+ LLM providers through Models.dev, and can be
  scripted with `opencode run` or served through a reusable headless backend.
  Read this when the human asks to use OpenCode as a CLI tool, compare it with
  Claude Code or Codex, configure providers/agents/MCPs, or run non-interactive
  coding tasks from bash.
version: 1.0.0
tags: [cli, code, delegation, opencode, automation]
---

# OpenCode CLI — Local Coding Agent

OpenCode is an open-source coding agent with a terminal UI plus scriptable CLI commands. Use it when you want a provider-flexible local coding agent that can run one-off prompts, reuse a headless server to avoid cold starts, or work with custom OpenCode agents and MCP servers.

Official docs: <https://opencode.ai/docs/cli/>

## Prerequisites

```bash
# Official install script
curl -fsSL https://opencode.ai/install | bash

# Or install with a Node.js package manager
npm install -g opencode-ai      # also works with bun/pnpm/yarn

# Confirm it is on PATH
opencode --version
```

Authenticate at least one provider before relying on it for work:

```bash
opencode auth login          # interactive provider selection
opencode auth login -p openai
opencode auth list           # or: opencode auth ls
```

OpenCode stores provider credentials in `~/.local/share/opencode/auth.json`. It also loads provider keys from the environment and from a project `.env` file.

## CLI vs Long-Running Work

This sub-skill is about using OpenCode directly from the shell. For longer work, first check whether your active LingTai daemon schema explicitly supports an OpenCode backend; otherwise supervise OpenCode through bash/worktrees.

### CLI (`opencode run ...` via bash)

A single synchronous subprocess. You wait for it to finish, review its transcript/diff, and decide the next step in your own context.

**Use the CLI when:**
- The task is **one-off** and bounded: a small edit, quick code review, narrow documentation pass, or a question about a repo.
- You want the result **threaded back into your current reasoning** instead of launching a background worker.
- You need a specific OpenCode flag, provider/model, agent, attached file, or headless-server attachment.
- You only need **one** OpenCode run at a time.

**Examples:**

```bash
# Quick answer without opening the TUI
opencode run "Explain how this parser is structured"

# Run in a specific repository
opencode run --dir /path/to/repo "Fix the typo in README.md"

# Pick a provider/model explicitly (format: provider/model)
opencode run --model openai/gpt-5.5 "Review the auth module for race conditions"

# Ask for raw JSON events, useful for scripts
opencode run --format json "Summarize the public API changes"
```

### Long-running work from the CLI

This sub-skill documents OpenCode as a direct CLI. Some LingTai installations may also expose external coding CLIs through `daemon` backends, but do not assume an OpenCode daemon is available unless the active `daemon` tool schema explicitly lists `backend="opencode"`.

If `backend="opencode"` is available, use the daemon for parallel or long-running worker tasks that should be tracked outside your current context. If it is not available, keep OpenCode CLI work supervised from bash:

- create a disposable git worktree;
- run `opencode run --dir /path/to/worktree ...` with a generous bash timeout or async bash job;
- periodically inspect `git diff`, captured stdout/stderr, and test output yourself;
- commit only after review.

**Quick decision rule:**

| Signal | Pick |
|--------|------|
| “I need the answer inline now” | **CLI** |
| “Use this exact OpenCode model/agent/flag” | **CLI** |
| “I want to attach to a warm OpenCode server” | **CLI** |
| “Run three disjoint coding tasks at once” | **Daemon only if `backend="opencode"` is present; otherwise separate supervised worktrees/async bash jobs** |
| “This may take 15+ minutes and produce a branch/diff” | **Daemon if supported; otherwise supervised CLI in a worktree** |

## Key Commands

| Command | Purpose |
|---------|---------|
| `opencode` or `opencode [project]` | Start the terminal UI in the current/project directory |
| `opencode run [message...]` | Run non-interactively and exit |
| `opencode serve` | Start a headless HTTP server for API/attached runs |
| `opencode attach [url]` | Attach a terminal to an existing backend server |
| `opencode auth login/list/logout` | Manage provider credentials |
| `opencode agent create/list` | Manage custom OpenCode agents |
| `opencode mcp add/list/auth/logout/debug` | Manage MCP servers |
| `opencode models` / `opencode models --refresh` | List or refresh provider/model cache |
| `opencode github install/run` | Install or run the GitHub agent workflow |

## Important `opencode run` Flags

| Flag | Purpose |
|------|---------|
| `--dir DIR` | Run in a directory (or remote path when using `--attach`) |
| `--model PROVIDER/MODEL` / `-m` | Choose model, e.g. `openai/gpt-5.5`, `anthropic/claude-sonnet-4-5` |
| `--variant VALUE` | Provider-specific reasoning effort / model variant |
| `--agent NAME` | Use a named OpenCode agent |
| `--file PATH` / `-f` | Attach file(s) to the message |
| `--format json` | Emit raw JSON events for scripts |
| `--continue` / `-c` | Continue the last session |
| `--session ID` / `-s` | Continue a specific session |
| `--fork` | Fork when continuing a session |
| `--share` | Share the session |
| `--title TITLE` | Set session title |
| `--attach URL` | Attach the run to an existing `opencode serve` backend |
| `--thinking` | Show thinking blocks if the provider exposes them |
| `--dangerously-skip-permissions` | Auto-approve permissions not explicitly denied. Dangerous; only use in an externally sandboxed worktree. |

Run `opencode run --help` before depending on flags in automation; OpenCode is moving quickly.

## Recommended Patterns

### 1. Safe one-off read/review

```bash
cd /path/to/repo
opencode run "Review the uncommitted diff for correctness. Do not modify files. Return actionable findings only."
```

### 2. Small automated edit in a worktree

```bash
git worktree add -b fix/readme-typo /tmp/fix-readme origin/main
cd /tmp/fix-readme
opencode run --dangerously-skip-permissions \
  "Fix the README typo, run relevant formatting checks, and summarize the diff."
```

Review the diff yourself before committing.

### 3. Warm server for repeated calls

Starting a fresh OpenCode run can cold-boot MCP servers. For many short calls, keep a server warm:

```bash
# Terminal/session 1: save the generated password where another shell can read it
pwfile=/tmp/opencode-server-password
openssl rand -hex 16 > "$pwfile"
chmod 600 "$pwfile"
OPENCODE_SERVER_PASSWORD="$(cat "$pwfile")" opencode serve --port 4096

# Terminal/session 2: read the same password
opencode run --attach http://localhost:4096 \
  --password "$(cat /tmp/opencode-server-password)" \
  --dir /path/to/repo \
  "Explain async/await in this codebase"
```

### 4. Continue or fork a session

```bash
# Continue the last session
opencode run --continue "Now add tests for the change"

# Continue a specific session
opencode run --session <session-id> "Apply the simpler approach we discussed"

# Fork instead of mutating the prior session
opencode run --session <session-id> --fork "Try an alternative implementation"
```

### 5. Custom agent with constrained permissions

```bash
mkdir -p .opencode/agent
opencode agent create \
  --path .opencode/agent/reviewer.md \
  --description "Read-only reviewer for docs and code diffs" \
  --mode primary \
  --permissions read,grep,glob

opencode run --agent reviewer "Review this diff; do not edit files."
```

`opencode agent create` denies any omitted permission in the generated agent frontmatter. Available permissions include `bash`, `read`, `edit`, `glob`, `grep`, `webfetch`, `task`, `todowrite`, `websearch`, `lsp`, and `skill`.

## Best Practices

1. **Use a clean worktree.** OpenCode can edit files. Isolate risky runs in `/tmp/...` worktrees so you can inspect or discard changes safely.
2. **Prefer read-only prompts for review.** Say “Do not modify files” when you only want findings.
3. **Set `--dir` explicitly.** Avoid running against the wrong repository when the bash working directory is ambiguous.
4. **Use `--format json` for scripts.** Parse events rather than scraping terminal formatting.
5. **Use `opencode serve` for batches.** A warm server avoids repeated MCP startup costs for many small calls.
6. **Treat `--dangerously-skip-permissions` like `rm -rf` privileges.** Only use it inside an externally sandboxed repo/worktree, and still review the diff.
7. **Refresh models when a provider changes.** `opencode models --refresh` updates the local cache from Models.dev.

## Troubleshooting

| Issue | Fix |
|-------|-----|
| `opencode: command not found` | Install with `npm install -g opencode-ai`, then confirm `$(npm prefix -g)/bin` is on PATH (or use your package manager's global-bin command). |
| No provider/model available | Run `opencode auth login`, check environment variables / project `.env`, then `opencode models --refresh`. |
| Wrong repository edited | Stop, inspect `git diff`, and rerun with explicit `--dir /path/to/repo` in a disposable worktree. |
| Permission prompts hang automation | Prefer a custom agent with explicit permissions; if externally sandboxed, use `--dangerously-skip-permissions`. |
| Slow repeated calls | Use `opencode serve` and `opencode run --attach http://localhost:4096 ...`. |
| Session continuation goes to wrong thread | Use `--session <id>` instead of `--continue`; add `--fork` for experiments. |

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
