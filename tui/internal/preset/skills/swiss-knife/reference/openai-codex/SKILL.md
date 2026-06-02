---
name: openai-codex
description: >
  Nested swiss-knife reference for OpenAI Codex CLI. Manual (not a tool) for OpenAI Codex CLI — OpenAI's coding agent that runs
  locally from your terminal. Built in Rust for speed and efficiency. Supports
  headless remote control, Vim editing, plugin management, hooks, and Chrome
  browser integration. Read this when the human asks to use OpenAI Codex CLI,
  wants to compare it with Claude Code, or needs help with installation and
  configuration.
version: 1.0.1
---

# OpenAI Codex CLI

> **OpenAI's coding agent — run locally from your terminal.**
> Built in Rust for speed. Open source. ~4 million weekly active users (as of April 2026).

## CLI vs Daemon — Which to Use

LingTai exposes Codex in two forms. They are **not interchangeable** — pick the one whose shape matches the work.

### CLI (`codex exec ...` via bash)

A single synchronous subprocess. You wait for it to finish, you get one transcript back, the conversation ends when the bash call returns.

> **Agent responsiveness rule:** long synchronous `codex exec` jobs in an agent's main turn are **strongly discouraged**. The Codex CLI command is a blocking subprocess from the parent agent's point of view: if you run it through a normal blocking bash tool call, the whole agent is blocked until it returns. The agent cannot answer new human messages, cannot checkpoint progress, and appears "stuck" even though the process is alive. Use inline `codex exec` only for short, bounded jobs where waiting inline is acceptable. For PR-sized, multi-file, exploratory, or 15+ minute work, prefer the LingTai daemon Codex backend; if you must use the CLI, wrap it in an explicitly supervised background/async job with logs, timeout, and recovery instructions.

**Use the CLI when:**
- The task is **one-off** and you want the result inline — a tightly-scoped edit, a single deterministic refactor, a quick mechanical pass
- You want the output **threaded back into your current reasoning** (you'll read it and decide next steps yourself)
- The task is **quick**, well-bounded, and has an explicit bash timeout
- You only need **one** of these running at a time

**Synchronous CLI is strongly discouraged when:**
- The work is PR-sized, branch-producing, exploratory, or likely to run 15+ minutes
- The human is waiting for responsiveness or may send follow-up instructions
- A stalled subprocess would make the parent agent look dead
- You need progress checkpoints, retries, or the ability to inspect/interrupt work independently

`codex exec` does not provide a LingTai job protocol by itself. "Async" means a LingTai or OS wrapper around the CLI (for example bash `async=true`, a supervised background job, or an independent daemon/backend), and that wrapper must own logs, timeout, cancellation, and recovery notes.

**Examples:**
```bash
# Rename a symbol across a small file
codex exec "rename the function foo() to fooBar() in utils/helpers.py"

# Mechanical lint-style pass
codex exec --model gpt-5.5 "remove unused imports from src/main.py"

# Quick scoped fix
codex exec --dir /path/to/project "fix the off-by-one in parse_range()"
```

### Daemon (LingTai `daemon` capability with `backend="codex"`)

A persistent agent spawned by the LingTai kernel. Runs in its **own worktree**, with its **own context window**, on its **own branch**. You dispatch it, it works asynchronously, you come back and review the diff.

**Use the daemon when:**
- You need to run **multiple tasks in parallel** — several disjoint deterministic refactors at once, a batch validation sweep
- The task is **complex or multi-step** enough to deserve a fresh context window dedicated to it (not competing with your conversation history)
- You need **context isolation** — the daemon shouldn't see (and shouldn't pollute) your current session's context
- The work runs **long enough** that a synchronous bash call would be awkward — wide mechanical refactors, multi-file deterministic diffs, validation passes that touch the whole tree
- You're acting as an **orchestrator** — planning and reviewing, not hand-coding (see the LingTai contributing guide's orchestrator-and-daemons discipline)

Per the LingTai dev guide, Codex daemons are particularly good for **tightly-scoped diffs, deterministic refactors, and mechanical validation passes** — they're more conservative than Claude Code daemons and are the right choice when the change is well-specified and the scope is clear.

**Examples of daemon-shaped work:**
- "Apply this codemod across `src/**/*.ts` and open a PR" — wide, mechanical, deserves its own worktree
- Three parallel deterministic refactors that don't share files — dispatch three daemons, review three diffs
- A validation sweep over the repo that produces a report and (optionally) fixes
- A "fire-and-check-back-later" task

### Quick decision rule

| Signal | Pick |
|--------|------|
| "I want the answer in this conversation, now" | **CLI** |
| "I want to do three of these at once" | **Daemon** (one per task) |
| "I'll review a diff afterward, not the transcript" | **Daemon** |
| "The output is a small string/snippet I'll paste somewhere" | **CLI** |
| "This might block my main turn while a human waits" | **Daemon** or supervised background wrapper |
| "This will take 15+ minutes and produce a branch" | **Daemon** |
| "I'm the orchestrator; the daemon is the worker" | **Daemon** |

### Codex vs Claude Code (same axis)

Both backends are available as CLI and daemon. The CLI-vs-daemon choice is about **shape of work** (one-shot vs parallel/long/isolated). The Codex-vs-Claude-Code choice is about **style of work**:

- **Codex daemons** — tightly-scoped diffs, deterministic refactors, mechanical validation
- **Claude Code daemons** — exploratory code reading, multi-file edits, skill/doc work, PR composition

When in doubt for non-trivial work: daemon. See `utilities/lingtai-dev-guide/reference/contributing/SKILL.md` for the full orchestrator/daemon convention.

## Agent Responsiveness Practices

1. **Keep synchronous calls short and explicitly timed**: For inline `codex exec`, set a short explicit bash timeout appropriate to the scoped task. Solving a long task by raising the synchronous timeout to 15+ minutes while the main agent waits is strongly discouraged.

2. **Prefer daemon or supervised background execution for long or PR-sized work**: If the task is complex, multi-file, branch-producing, exploratory, or a wide validation sweep, dispatch it to the LingTai daemon Codex backend or another independently inspectable supervised wrapper. The parent agent should stay responsive and able to report progress. Remember: the wrapper is asynchronous; the CLI subprocess itself is not a LingTai job.

3. **Checkpoint before delegation**: For any task that might outlive the current turn, write the worktree, branch, goal, and recovery instructions to pad or a journal before dispatching.

4. **Split large tasks**: Multiple smaller, bounded `codex exec` calls are safer than one monolithic prompt. If the steps still take long or need a branch, prefer a daemon or supervised background wrapper.


## Installation

```bash
npm install -g @openai/codex@0.130.0
```

Update existing installation:
```bash
codex update
# or
npm i -g @openai/codex@latest
```

## Configuration

### API Key
Set your OpenAI API key:
```bash
export OPENAI_API_KEY="your-api-key"
```

Or configure in `~/.codex/config.toml`:
```toml
[api]
key = "your-api-key"
```

### Models
Codex CLI supports multiple models:
- GPT-5.5 (latest, recommended)
- GPT-5.4
- GPT-5.3-Codex (specialized for coding)

Configure in `config.toml`:
```toml
[model]
default = "gpt-5.5"
```

### Bedrock Auth
For AWS Bedrock, use console-login credentials:
```bash
aws login
codex exec "your prompt"
```

## Key Features

### 1. Remote Control
New in 0.130.0 — headless, remotely controllable app-server:
```bash
codex remote-control
```
- Start a headless app-server
- Control Codex remotely
- Page large threads with different view modes (unloaded/summary/full)

### 2. Vim Editing
Full Vim modal editing in the TUI:
```bash
codex exec "your prompt"
# In TUI:
/vim                    # Toggle Vim mode
:set default-mode=insert  # Set default mode
```

### 3. Plugin Management
Workspace sharing and marketplace:
```bash
codex plugins list      # List installed plugins
codex plugins install   # Install from marketplace
codex plugins share     # Share with workspace
```

Features:
- Workspace sharing with access controls
- Source filtering and local share path tracking
- Marketplace removal/upgrades
- Remote bundle sync
- Admin-disabled status handling

### 4. Hooks
Browseable and toggleable hooks:
```bash
codex hooks list        # List available hooks
codex hooks toggle      # Toggle hook on/off
```

Capabilities:
- Before/after compaction support
- PreToolUse context injection
- Codex Apps auth integration
- MCP elicitations through TUI/Guardian flows

### 5. Chrome Extension
Browser integration without takeover:
- Works in parallel across tabs
- Background operation
- User controls which websites Codex can use
- Install from Chrome Web Store

### 6. App-Server
Thread management and pagination:
```bash
codex exec "your prompt"
# In TUI:
# - Resume/fork picker
# - Raw scrollback mode
# - /ide context injection
# - /diff workspace-aware diffing
```

## Usage Examples

### Basic Usage
```bash
# Start interactive session
codex exec "Create a Python script that reads CSV files"

# With specific model
codex exec --model gpt-5.5 "Refactor this function"

# In specific directory
codex exec --dir /path/to/project "Fix the bug in main.py"
```

### Remote Control
```bash
# Start headless server
codex remote-control

# Connect from another terminal
codex connect localhost:8080
```

### Plugin Management
```bash
# List plugins
codex plugins list

# Install plugin
codex plugins install @openai/plugin-name

# Share with workspace
codex plugins share ./my-plugin
```

### Hooks
```bash
# List hooks
codex hooks list

# Toggle hook
codex hooks toggle my-hook

# Run with hooks
codex exec --hooks my-hook "your prompt"
```

## Integration with LingTai

### Workflow Integration
Codex CLI can be used alongside Claude Code for different tasks:

| Task | Claude Code | OpenAI Codex |
|------|-------------|--------------|
| Complex reasoning | ✅ Excellent | ✅ Good |
| Local file operations | ✅ Good | ✅ Excellent |
| Browser integration | ❌ No | ✅ Chrome extension |
| Remote control | ❌ No | ✅ Yes |
| Plugin ecosystem | ❌ Limited | ✅ Rich marketplace |

### When to Use Codex CLI
- **Browser automation**: Use Chrome extension for web tasks
- **Remote development**: Use remote-control for headless operation
- **Plugin ecosystem**: Leverage marketplace for specialized tools
- **Vim users**: Native Vim editing support

### When to Use Claude Code
- **Complex reasoning**: Deep analysis and multi-step problem solving
- **LingTai integration**: Native integration with LingTai kernel
- **Cost efficiency**: Uses Claude Max subscription

## Comparison with Claude Code

| Feature | OpenAI Codex CLI | Claude Code |
|---------|------------------|-------------|
| Language | Rust | TypeScript |
| Open Source | ✅ Yes | ❌ No |
| Vim Support | ✅ Native | ❌ No |
| Browser Extension | ✅ Chrome | ❌ No |
| Remote Control | ✅ Yes | ❌ No |
| Plugin Marketplace | ✅ Rich | ❌ Limited |
| LingTai Integration | ❌ No | ✅ Native |
| Cost | API usage | Claude Max subscription |

## Troubleshooting

### Common Issues

1. **Installation fails**
   ```bash
   # Clear npm cache
   npm cache clean --force
   # Reinstall
   npm install -g @openai/codex@0.130.0
   ```

2. **API key not found**
   ```bash
   # Check environment variable
   echo $OPENAI_API_KEY
   # Or check config file
   cat ~/.codex/config.toml
   ```

3. **Plugin installation fails**
   ```bash
   # Check marketplace connectivity
   codex plugins search
   # Clear plugin cache
   rm -rf ~/.codex/plugins/cache
   ```

4. **Agent appears stuck while `codex exec` runs**
   - You likely used synchronous CLI for work that should have been daemon-backed or supervised in the background.
   - Inspect the child process and worktree. If needed, kill the child so the blocking bash call returns.
   - Resume with the LingTai daemon Codex backend or a supervised background wrapper that records logs, timeout, cancellation path, and recovery notes.

## Resources

- **GitHub**: https://github.com/openai/codex
- **Documentation**: https://developers.openai.com/codex
- **Changelog**: https://developers.openai.com/codex/changelog
- **Chrome Extension**: Available on Chrome Web Store

## Version History

- **0.130.0** (May 8, 2026): Remote control, plugin hooks, Bedrock auth
- **0.129.0** (May 7, 2026): Vim editing, Chrome extension, plugin management
- **0.128.0** (May 5, 2026): /goal command, Ralph loop

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
