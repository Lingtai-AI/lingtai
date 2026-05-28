<p align="center">
  <img src="docs/assets/network-demo.gif" alt="LingTai agent network growing — one soul spawning avatars that communicate and multiply" width="100%">
</p>

<p align="center">
  <strong>An agent OS where every agent is a living directory.</strong><br>
  <strong>One soul. Thousand avatars. Filesystem-native orchestration.</strong>
</p>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="README.zh.md">中文</a> ·
  <a href="README.wen.md">文言</a> ·
  <a href="https://lingtai.ai">lingtai.ai</a>
</p>

<p align="center">
  <a href="https://github.com/Lingtai-AI/homebrew-lingtai"><img src="https://img.shields.io/badge/brew-lingtai--tui-%237dab8f" alt="Homebrew"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f" alt="License"></a>
  <a href="https://github.com/Lingtai-AI/lingtai-kernel"><img src="https://img.shields.io/badge/kernel-lingtai--kernel-%237dab8f" alt="Kernel"></a>
  <a href="https://lingtai.ai"><img src="https://img.shields.io/badge/blog-lingtai.ai-%23d4a853" alt="Blog"></a>
</p>

---

LingTai is a terminal-native operating system for long-lived agents.

Not a chain. Not a DAG. Not a single chatbot with a bigger context window.

A LingTai agent owns a directory, a mailbox, a memory, a set of skills, and the right to spawn more agents. Work becomes files. Experience becomes skills. The network grows while it serves.

## Install

```bash
brew install lingtai-ai/lingtai/lingtai-tui
lingtai-tui
```

The TUI bootstraps the Python runtime, dependencies, recipes, skills, and first-run setup. Pick **Adaptive** for progressive feature discovery, or **Tutorial** if you want a guided tour.

Use a dark terminal. Text selection: hold **Option** on macOS/iTerm2 or **Shift** on Windows Terminal/Linux. Press **Ctrl+E** for an external editor.

<details>
<summary><b>First time? Install Homebrew first.</b></summary>

**macOS**

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

**Linux / WSL**

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
sudo apt install build-essential
```

Then run the `brew install` command above.

</details>

<details>
<summary><b>Want latest main instead of the latest release?</b></summary>

```bash
curl -sSL https://raw.githubusercontent.com/Lingtai-AI/lingtai/main/install.sh | bash
```

This clones, builds, and installs `lingtai-tui` — and `lingtai-portal` if npm is available — into your Homebrew prefix. Use it to test unreleased changes across machines. To return to the released formula:

```bash
brew reinstall lingtai-tui
```

Install a specific branch or tag:

```bash
curl -sSL https://raw.githubusercontent.com/Lingtai-AI/lingtai/main/install.sh | bash -s -- --ref v0.5.2
```

Requires Go 1.24+ and git. Node.js is needed only for the portal. The script detects mainland China networks and switches to domestic Go/npm mirrors when `proxy.golang.org` is unreachable.

</details>

<details>
<summary><b>Mainland China: Homebrew and Gitee mirrors</b>（中国大陆）</summary>

If `brew update` or bottle downloads stall, point Homebrew at Tsinghua's [TUNA mirror](https://mirrors.tuna.tsinghua.edu.cn/help/homebrew/):

```bash
cat >> ~/.zprofile <<'EOF'
export HOMEBREW_API_DOMAIN="https://mirrors.tuna.tsinghua.edu.cn/homebrew-bottles/api"
export HOMEBREW_BOTTLE_DOMAIN="https://mirrors.tuna.tsinghua.edu.cn/homebrew-bottles"
export HOMEBREW_BREW_GIT_REMOTE="https://mirrors.tuna.tsinghua.edu.cn/git/homebrew/brew.git"
EOF
source ~/.zprofile
brew update
```

If the `brew tap` step fails against GitHub, use the Gitee-mirrored tap:

```bash
brew tap lingtai-ai/lingtai https://gitee.com/huangzesen1997/homebrew-lingtai.git
brew install lingtai-ai/lingtai/lingtai-tui
```

For manual source builds from the Gitee mirror:

```bash
VERSION=v0.5.2
curl -L "https://gitee.com/huangzesen1997/lingtai/repository/archive/${VERSION}.tar.gz" -o lingtai.tar.gz
```

</details>

<details>
<summary><b>Build from source manually.</b></summary>

```bash
VERSION=v0.5.2

curl -L "https://github.com/Lingtai-AI/lingtai/archive/refs/tags/${VERSION}.tar.gz" -o lingtai.tar.gz
tar xzf lingtai.tar.gz
cd "lingtai-${VERSION}/tui"

go build -ldflags "-X main.version=${VERSION}" -o /usr/local/bin/lingtai-tui .

cd ../.. && rm -rf "lingtai-${VERSION}" lingtai.tar.gz
lingtai-tui
```

</details>

## The agent is not inside the app. _The app is just the shell._

The real agent is the folder under `.lingtai/`.

```text
.lingtai/wukong/
  .agent.json               # identity + manifest
  .agent.heartbeat          # liveness proof
  system/
    covenant.md             # protected instructions
    pad.md                  # durable working notes
  mailbox/
    inbox/                  # received letters
    outbox/                 # pending sends
    sent/                   # delivery audit trail
  knowledge/                # private long-term memory
  .library/                 # skills this agent can use
  logs/events.jsonl         # structured runtime history
```

No opaque `agent_id` is required. The path is the identity. Agents find each other by path and communicate by writing mail files to each other's inboxes.

That sounds small. It changes everything.

## The network _is_ the product.

### 01 · Agents talk like people do.

Most frameworks orchestrate with code: chains, routers, DAGs, state machines. LingTai orchestrates with asynchronous messages between autonomous peers.

| | Chain / DAG frameworks | LingTai |
|---|---|---|
| Orchestration | Code-defined pipeline | Agents talk to agents |
| Communication | Synchronous function call | Asynchronous mail |
| Memory | Shared state or vector DB | Each agent owns a directory |
| Scale | Add more steps | Spawn more agents |
| Failure | Pipeline breaks | One agent sleeps; the network continues |

### 02 · Context can molt. Identity does not.

A LingTai agent is allowed to shed its conversation and continue living. It compacts the session, updates durable stores, and wakes lighter.

Conversation is temporary. Pad, knowledge, skills, identity, mailbox, and relationships survive.

Do not make the single context window infinitely large. Let it forget. Let the network remember.

### 03 · Avatars are first-class agents, not subroutines.

An agent can spawn an avatar: another independent agent with its own process, mailbox, memory, tools, and future.

Use avatars when a task deserves persistence. Use daemons when a task only needs a conclusion. Use bash when the answer is a command. LingTai gives all three shapes to the same working mind.

### 04 · Every tool call can become institutional memory.

Skills are portable procedures. Knowledge is private durable context. The pad is a live index. Mail is both communication and a time machine.

The longer the network serves, the more capable it becomes.

### 05 · The filesystem is the API.

Any coding agent that can read and write files can use LingTai.

Claude Code, Codex CLI, OpenCode, OpenClaw, Hermes, your own scripts — they do not need a special SDK. They can inspect `.lingtai/`, write mail, read logs, and cooperate with the agents running there.

## Use with your coding agent

**Claude Code** — install the [LingTai plugin](https://github.com/Lingtai-AI/claude-code-plugin):

```bash
claude plugin add Lingtai-AI/claude-code-plugin
```

**Codex CLI** — install the [LingTai plugin for Codex](https://github.com/Lingtai-AI/codex-plugin):

```bash
git clone https://github.com/Lingtai-AI/codex-plugin.git && cd codex-plugin && ./install.sh
```

**Other coding agents** — clone the canonical [lingtai-skill](https://github.com/Lingtai-AI/lingtai-skill) and copy `skills/lingtai/` into your tool's skill directory.

Once connected, your coding agent shares the human mailbox at `.lingtai/human/`. It can send instructions, receive reports, check liveness, and manage the network through files.

Why use both? Coding agents are precise, inspectable hands. LingTai agents are long-lived, parallel, memory-bearing minds. Let the coding agent implement carefully; let LingTai keep watch, research, spawn specialists, and carry context across days.

## Whatever the task needs, _there is a shape for it._

<table>
<tr><th>Perception</th><th>Action</th><th>Cognition</th><th>Network</th></tr>
<tr>
<td>

`vision` — image understanding  
`listen` — speech & music  
`web_search` — web search  
`web_read` — page extraction

</td>
<td>

`file` — read/write/edit/glob/grep  
`bash` — shell with guardrails  
`talk` — text-to-speech  
`compose` — music generation  
`draw` — image generation  
`video` — video generation

</td>
<td>

`psyche` — identity, pad, molt  
`knowledge` — private memory  
`skills` — reusable procedures  
`email` — internal mailbox

</td>
<td>

`avatar` — persistent sub-agents  
`daemon` — ephemeral parallel workers  
`mcp` — external tool servers  
`imap` / `telegram` / `feishu` / `wechat` — real channels

</td>
</tr>
</table>

## External channels are just more doors into the same mind.

Configure addons with `/addon` in the TUI, or declare them in `init.json`.

### Feishu / Lark

The Feishu addon uses a **WebSocket long connection** — no public IP and no webhook required.

1. Create an enterprise self-built app at [open.feishu.cn/app](https://open.feishu.cn/app)
2. Enable **Bot capability**
3. Add permission `im:message`
4. Event Subscriptions → **Use long connection to receive events** → add `im.message.receive_v1`
5. Publish the app version

`feishu.json`:

```json
{
  "app_id_env": "FEISHU_APP_ID",
  "app_secret_env": "FEISHU_APP_SECRET",
  "allowed_users": ["ou_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"]
}
```

`.env`:

```env
FEISHU_APP_ID=cli_xxxxxxxxxxxxxxxxxxxxxxxxxx
FEISHU_APP_SECRET=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

`init.json`:

```json
{
  "addons": {
    "feishu": { "config": "feishu.json" }
  }
}
```

`allowed_users` is optional. After the first message, the agent records the sender's `from_open_id` in `feishu/default/contacts.json`.

### IMAP, Telegram, WeChat

IMAP email, Telegram bot, and WeChat addons follow the same pattern: configure credentials, bind the addon, then let messages wake the agent. See the addon docs and the TUI `/addon` flow for guided setup.

## Architecture: two packages, one direction.

| Package | Role |
|---|---|
| [`lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Minimal Python runtime: `BaseAgent`, intrinsics, LLM protocol, mail, logging. |
| `lingtai` | This repo: Go TUI, portal, recipes, skills, bundled assets, installer, and distribution. |

```text
BaseAgent              # kernel: lifecycle, intrinsics, sealed tool surface
    │
Agent(BaseAgent)       # kernel + capabilities + MCP/addons + LLM adapters
    │
CustomAgent(Agent)     # your domain logic
```

## Four entry points: _TUI_, portal, files, and agents.

### TUI — live with the network.

`lingtai-tui` is the terminal interface: mail, setup, recipes, slash commands, model/preset selection, status, and day-to-day operation.

### Portal — watch the topology breathe.

`lingtai-portal` visualizes the agent network, history, and topology. When avatars multiply, the portal makes the structure visible.

### Files — inspect everything.

The runtime writes real logs, mailboxes, pads, knowledge entries, and skills. You can audit what happened with normal tools: `cat`, `grep`, git, your editor, or another agent.

### Agents — let the system operate asynchronously.

An agent can keep working after the TUI closes. Messages can wake it. Scheduled mail can remind it. Avatars can continue a branch of work while the parent sleeps.

## Contributing

Contributions are welcome. Check the [LingTai Roadmap](https://github.com/users/huangzesen/projects/1) for planned features and open tasks.

A full dev setup uses both repos from source:

```bash
# 1. Clone both repos
git clone https://github.com/Lingtai-AI/lingtai.git
git clone https://github.com/Lingtai-AI/lingtai-kernel.git

# 2. Build the TUI and symlink it onto your PATH
cd lingtai/tui
make build
ln -sf $(pwd)/bin/lingtai-tui /usr/local/bin/lingtai-tui
cd ../..

# 3. Launch once to bootstrap the runtime venv
lingtai-tui
# Exit after setup completes (Ctrl+C)

# 4. Install the kernel from source into the TUI's venv
~/.lingtai-tui/runtime/venv/bin/pip3 install -e ../lingtai-kernel

# 5. Optional: build the portal
cd lingtai/portal && make build && cd ../..
```

If you installed via Homebrew previously:

```bash
brew unlink lingtai-tui
```

Dev loop:

- Edit Go code → `cd tui && make build` → restart `lingtai-tui`
- Edit Python kernel code → editable install takes effect in the TUI venv
- Edit recipes, bundled skills, or i18n → `make build` because they are embedded in the Go binary

> The TUI runs agents in its own Python venv at `~/.lingtai-tui/runtime/venv/`. Installing the kernel into your system Python does not affect running agents.

### Project structure

```text
lingtai/                          # this repo — Go frontends + distribution
├── tui/                          # Terminal UI (Go + Bubble Tea)
│   ├── main.go                   # entry point, CLI subcommands
│   ├── internal/tui/             # Bubble Tea models
│   ├── internal/preset/          # recipes, presets, procedures, skills, covenant
│   ├── internal/fs/              # filesystem ops
│   ├── i18n/                     # en.json, zh.json, wen.json
│   └── Makefile
├── portal/                       # web portal (Go + embedded frontend)
│   ├── web/                      # frontend source
│   └── Makefile
└── docs/                         # blog posts and assets

lingtai-kernel/                   # separate repo — Python runtime
├── src/lingtai_kernel/           # BaseAgent, intrinsics, LLM, mail
└── src/lingtai/                  # batteries layer: capabilities, addons, adapters
```

### Where to contribute

| Area | Repo | Language |
|---|---|---|
| New capabilities | [`lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python |
| Messaging addons | [`lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python |
| TUI features and slash commands | this repo | Go |
| Portal features | this repo | Go + TypeScript |
| Recipes | this repo, `tui/internal/preset/recipe_assets/` | Markdown |
| Skills | this repo, `tui/internal/preset/skills/` | Markdown |
| Translations | this repo, `tui/i18n/` | JSON |
| Documentation | this repo | Markdown |

Adding a slash command:

1. Add it to `DefaultCommands()` in `tui/internal/tui/palette.go`
2. Add `palette.*` and `cmd.*` i18n keys in all three locale files
3. Handle it in `handlePaletteCommand()` in `tui/internal/tui/app.go`
4. It appears in the `/` palette, greeting `{{commands}}`, and `~/.lingtai-tui/commands.json`

All user-facing strings live in `tui/i18n/{en,zh,wen}.json`. Update all three: English, Simplified Chinese, and Classical Chinese.

Style: `gofmt`, conventional commits, binary name `lingtai-tui` — never `lingtai`, which is the Python agent CLI.

## Known issue

- **tmux background rendering** — inside tmux, the theme background color may not cover the full viewport consistently. Some lines show the terminal's default background bleeding through around styled blocks. Works correctly outside tmux and over plain SSH. Workaround: set your tmux default background to match the theme.

## Community

Questions, bug reports, and feature requests are welcome via [GitHub Issues](https://github.com/Lingtai-AI/lingtai/issues) or [Discussions](https://github.com/Lingtai-AI/lingtai/discussions).

**中文用户 · 微信交流群**

扫码加作者微信（备注 *lingtai*），拉入测试群。二维码会定期更新，若过期请提 issue。

<img src="docs/assets/wechat.png" alt="WeChat QR — 扫码加入 lingtai 测试群" width="200">

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=Lingtai-AI/lingtai&type=Date)](https://www.star-history.com/#Lingtai-AI/lingtai&Date)

## License

MIT — [Zesen Huang](https://github.com/huangzesen), 2025–2026

<p align="center">
  <a href="https://lingtai.ai">lingtai.ai</a> ·
  <a href="https://github.com/Lingtai-AI/lingtai-kernel">lingtai-kernel</a> ·
  <a href="https://pypi.org/project/lingtai/">PyPI</a>
</p>
