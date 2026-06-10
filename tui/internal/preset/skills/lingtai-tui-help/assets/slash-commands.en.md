# Slash commands

Slash commands drive everything in `lingtai-tui` beyond plain chat. They open
views, manage the agent lifecycle, and configure the runtime.

## Using the command palette

- Type `/` in the message box to open the **command palette**.
- Keep typing to filter. Matching is fuzzy: `/skl` finds `skills`, `/ref` finds
  `refresh`.
- `↑`/`↓` move the selection, `Enter` runs the highlighted command.
- Some commands take an argument typed after the name, e.g. `/sleep all` or
  `/refresh mimo`.

## Browsing this help

`/help` opens this guide in the markdown viewer. Use `Tab` to move focus to the
content panel so `PgUp`/`PgDn` scroll the page. `Ctrl+E` exports the current page
to `~/Downloads`. Press `Esc` or `q` to return to the mail view.

## Commands at a glance

### Talking to the agent
- `/btw` — ask a side question without interrupting the agent's work.
- `/insights` — request 2–3 concrete observations about the current task now.

### Agent lifecycle
- `/sleep` — put the agent to sleep (resumable with `/cpr`).
- `/suspend` — freeze the agent process entirely (resumable with `/cpr`).
- `/cpr` — revive a suspended or dead agent.
- `/refresh` — hard restart: reload config from disk, optionally switch presets.
- `/clear` — clear the conversation window, keep identity/pad/codex.
- `/molt` — force the agent to molt now (save context, reset the window).
- `/nirvana` — wipe everything and start fresh. Irreversible.

### Inspecting the agent
- `/kanban` — network dashboard of every agent's config and context usage.
- `/skills` — browse the agent's skill catalog.
- `/knowledge` — browse the agent's private knowledge (aliases: `/library`,
  `/codex`).
- `/system` — browse the agent's system files (system.md, covenant, …).
- `/daemons` — inspect per-agent daemon runs and their records.
- `/notification` — show the current agent's raw notification block (`.notification/*.json`).
- `/presets` — view the presets this agent can switch to with `/refresh`.

### Network & sharing
- `/mailbox` — browse all mailbox messages and attachments.
- `/projects` — browse registered projects and their networks.
- `/agora` — browse exported networks and recipes from the marketplace.
- `/export` — export a reusable recipe for sharing.
- `/viz` — open the network visualization in the browser.

### Configuration & diagnostics
- `/setup` — agent setup wizard (provider, model, capabilities).
- `/settings` — TUI preferences (theme, page size, language).
- `/mcp` — MCP control panel for external-service bridges.
- `/doctor` — diagnose connection issues.
- `/login` — check and manage saved credentials.

### This view & exit
- `/help` — open this help guide.
- `/quit` — quit `lingtai-tui` (agents keep running in the background).

---

## Command reference

### `/btw` — ask the agent a side question
**Usage:** `/btw <your question>`

Delivers your question as an *insight inquiry*. The agent reflects and responds
without interrupting the work it is currently doing — so you can probe its
thinking mid-task without derailing it. Reach for it when you want the agent's
take on something tangential and don't want to break its focus the way a normal
message would.

### `/insights` — request an insight now
**Usage:** `/insights`

Asks the agent to observe its current task right now and produce 2–3 concrete
observations. Use it when you want an immediate, structured read on what the
agent is seeing — without waiting for the auto-insights cadence (toggled in
`/settings`).

### `/sleep` — put the agent to sleep
**Usage:** `/sleep` (current agent) · `/sleep all` (every agent in the project)

Sleeping pauses the agent while preserving its full state. A sleeping agent can
be woken later with `/cpr`. Use it when you want to stop an agent from consuming
resources but intend to resume it exactly where it left off. For a harder freeze
of the OS process, use `/suspend`.

### `/suspend` — suspend the agent
**Usage:** `/suspend` (current agent) · `/suspend all` (every agent in the project)

Freezes the agent process entirely. A suspended agent must be revived with `/cpr`
before it can resume. Reach for it when you want a hard stop of the underlying
process — heavier than `/sleep`. Use `/sleep` if you just want a resumable pause.

### `/cpr` — revive a suspended or dead agent
**Usage:** `/cpr` (current agent) · `/cpr all` (every agent in the project)

Brings a suspended or dead agent back to life. Use it to resume an agent paused
by `/sleep` or `/suspend`, or to recover one that has died. For routine restarts
prefer `/refresh` — `/cpr` is specifically for agents that are down.

### `/refresh` — hard restart the agent
**Usage:** `/refresh` (restart, default preset) · `/refresh <name>` (switch
preset, e.g. `/refresh mimo`) · `/refresh all` (every agent)

Reloads `init.json`, capabilities, and all configuration from disk, then
relaunches the agent. Passing a preset name switches the active preset before
relaunching; the preset must be in the agent's `manifest.preset.allowed` list
(see `/presets`). It is the go-to command after editing configuration, or to
switch the agent onto a different LLM/capability preset — reach for `/cpr` only
when an agent is actually down.

### `/clear` — clear the context window and restart
**Usage:** `/clear`

Clears the agent's entire context window and restarts it with a blank
conversation. Identity, pad, and codex are preserved — only the live conversation
is wiped. Use it when the conversation has accumulated noise or gone off track and
you want a clean slate without losing durable memory. To preserve context by
saving it first, use `/molt`.

### `/molt` — force the agent to molt now
**Usage:** `/molt`

Forces the agent to molt immediately: it saves its context and then resets the
conversation window. Use it when the context window is getting full but you don't
want to lose what the agent has built up. Unlike `/clear`, a molt saves context
before resetting.

### `/nirvana` — wipe everything and start fresh
**Usage:** `/nirvana`

Deletes `.lingtai/` and all agent data, returning the project to a completely
fresh state. **⚠️ Irreversible** — this cannot be undone. If you only want a clean
conversation, use `/clear`; to reset while preserving context, use `/molt`. Reach
for `/nirvana` only when you genuinely want to start the project over from nothing.

### `/kanban` — open the agent network dashboard
**Usage:** `/kanban`

Shows a dashboard of every agent in the network: properties, LLM config,
capabilities, and context usage at a glance. Use it for a single terminal view
comparing all agents' configuration and how full their context windows are. For a
graphical, browser-based view use `/viz`.

### `/skills` — browse the skill catalog
**Usage:** `/skills`

Opens the skill catalog — the reusable procedures the agent can invoke on demand.
`Ctrl+T` switches between agents; `Enter` on a skill drills into the files inside
that skill's folder. Use it to see what an agent can do on demand, or inspect a
specific skill.

### `/knowledge` — browse an agent's private knowledge
**Aliases:** `/knowledge`, `/library`, `/codex` · **Usage:** `/knowledge`

Browses an agent's private knowledge — local decisions, notes, references, and
migrated codex entries, including its durable knowledge library of accumulated
research and findings. `Ctrl+T` switches between agents. Use it to read what an
agent has recorded for itself, as opposed to the shared skill catalog (`/skills`)
or its system files (`/system`).

### `/system` — browse an agent's system files
**Usage:** `/system`

Browses an agent's system files: `system.md`, covenant, principle, procedures,
pad, and llm config. `Ctrl+T` switches between agents. Use it to read the durable
scaffolding that defines an agent — its governing documents and runtime config —
rather than its conversation or private knowledge (`/knowledge`).

### `/daemons` — browse daemon runs
**Usage:** `/daemons`

Opens the daemon browser: inspect per-agent daemon runs and their status, full
tasks, full `chat_history` interactions, and full tool/event records. Use it to
trace exactly what a background daemon did.

### `/notification` — view the current notification block
**Usage:** `/notification`

Shows the raw JSON notification block in the current agent's `.notification/` directory, plus one entry per channel file. Use it to confirm exactly which notification payloads the agent can see before the next `system(action="notification")` injection. Notifications already consumed or cleared by the kernel will no longer appear here, though they may still be present in history as structured tool-call/tool-result records.

### `/presets` — open the preset library
**Usage:** `/presets`

Opens the preset library scoped to the current agent. It shows only the presets
in this agent's `manifest.preset.allowed` list — exactly the ones you can switch
to with `/refresh <name>`. The active preset is marked with ●. You can view each
preset's LLM and capabilities, and tag it with a 1–5 star cost/quality tier
(higher is better); tags propagate to agents and guide daemon/avatar selection.
This view is read-only inspection plus tag editing — full preset creation happens
in `/setup`.

### `/mailbox` — browse all mailbox messages
**Usage:** `/mailbox`

Browses every mailbox message — inbox, sent, and IMAP — with the full message
body and inline attachment rendering. Use it for the complete mail record for an
agent, including external IMAP mail and attachments, rather than the live chat
stream.

### `/projects` — browse registered projects
**Usage:** `/projects`

Lists registered projects and their agent networks. Use it when you work across
several LingTai projects and want to see them and their networks in one place.

### `/agora` — browse the agora marketplace
**Usage:** `/agora`

Browses exported networks and recipes published to the agora marketplace. Use it
to discover and pull in networks or recipes others have shared. To publish your
own, use `/export`.

### `/export` — export a reusable recipe
**Usage:** `/export` · `/export recipe` (explicit form)

Exports a reusable recipe of the current setup so it can be shared. Use it to
package your agent configuration for reuse or to publish to the `/agora`
marketplace.

### `/viz` — open the network visualization
**Usage:** `/viz`

Opens the agent network visualization in your browser — topology, mail flows, and
agent states. Requires `lingtai-portal` to be on your `PATH`. Use it for a live
graphical view of how agents relate and communicate. For a terminal-only overview
use `/kanban`.

### `/setup` — run the agent setup wizard
**Usage:** `/setup`

Opens the agent setup wizard, where you change the LLM provider, model,
capabilities, and runtime settings. Changes propagate to all agents. Use it for
first-time configuration or any substantive change to the agent's provider,
model, or capabilities. For TUI-only preferences (theme, language) use
`/settings`; full preset creation also happens in this wizard.

### `/settings` — edit TUI preferences
**Usage:** `/settings`

Opens TUI preferences: theme, mail page size, language, and the auto-insights
toggle. Use it for look-and-feel and client-side behavior of the TUI itself. To
change the agent's provider, model, or capabilities, use `/setup`.

### `/mcp` — open the MCP control panel
**Usage:** `/mcp`

Opens the MCP control panel, where you review each MCP bridge's resources,
status, and config — the bridges (IMAP email, Telegram, Feishu, WeChat) that
connect the agent to external services. Use it when configuring or
troubleshooting an external-service integration. After changing MCP config, run
`/refresh` to apply it.

### `/doctor` — diagnose connection issues
**Usage:** `/doctor`

Runs diagnostics on the agent's connectivity — checks API keys, model
availability, and network configuration — and reports what it finds. Use it when
an agent fails to start or stops responding and you suspect a credential, model,
or network problem. Pair with `/login` to re-authenticate if `/doctor` flags a
credential issue.

### `/login` — check and manage credentials
**Usage:** `/login`

Shows the authentication status for all saved credentials and lets you
re-authenticate if needed. Use it when a provider credential has expired or you
need to sign in again — often in response to a problem surfaced by `/doctor`.

### `/help` — open this help guide
**Usage:** `/help`

Opens the help guide you are reading now: an overview of how slash commands work,
plus a reference entry for every command. Press `Esc` or `q` to return to the
mail view. Use it any time you want to look up what a command does or discover
commands you haven't used.

### `/quit` — quit lingtai-tui
**Usage:** `/quit`

Exits `lingtai-tui`. Agents continue running in the background unless you
suspended them first. If you want agents to stop too, use `/suspend all` (or
`/sleep all`) before quitting.
