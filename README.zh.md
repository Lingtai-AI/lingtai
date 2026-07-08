<div align="center">

# 灵台 LingTai

**在你的项目里建起一个 AI 组织——不只是再多一个 Agent。**

本地优先 · 常驻智能体 · soul flow 主动反思 · 信箱 · 生命周期 · 多智能体组织

[English](README.md) · [中文](README.zh.md) · [文言](README.wen.md) · [lingtai.ai](https://lingtai.ai) · [发布日志](https://lingtai.ai/releases/)

[![License](https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f)](LICENSE)
[![Kernel](https://img.shields.io/badge/内核-lingtai--kernel-%237dab8f)](https://github.com/Lingtai-AI/lingtai-kernel)
[![Site](https://img.shields.io/badge/site-lingtai.ai-%23d4a853)](https://lingtai.ai)
[![Discord](https://img.shields.io/badge/discord-加入-%235865F2?logo=discord&logoColor=white)](https://discord.gg/cMchjXpg)

</div>

---

多数 agent 工具给你的是一个更强的工人。**灵台给你的是一个 AI 组织的底座**：长期住在本地项目里的智能体，有自己的主目录、收发信箱、持久记忆、生命周期控制、自我反思回路，也能在任务大到一个脑袋不够时召唤同伴或化出分身。

**OpenClaw**、**Hermes** 这类工具是很有用的“手”——擅长执行某个 agentic task。灵台是围绕这些手的组织层：它可以把编码 agent 和 CLI 当作工人来用，同时保留角色、记忆、通信、监督、恢复这些让一个智能体网络在单次聊天或终端会话结束后仍能继续运转的组织能力。

你可以从 TUI、Telegram、飞书/Lark、微信、WhatsApp 或邮件发来任务。同一个组织会通过被点名的智能体醒来，读取项目记忆，调用本地工具，写出产物，必要时协调同伴，然后回到你发起任务的那个渠道向你汇报。

## 这是元组织，不是另一个聊天窗口

```text
你
  “今晚盯一下仓库。PR 如果坏了，就定位原因，草拟修复，
   明早给我一份简报。”

灵台
  从信箱中醒来
  → 读取持久项目记忆
  → 调用 shell / web / file / coding-agent 工具
  → 空闲或卡住时通过 soul flow 自我反思
  → 写下笔记、报告、补丁或定时任务
  → 需要并行时请专家分身或神识帮忙
  → 在 Telegram / TUI / 邮件里带着产物回复你
```

你可以关掉终端。这个组织仍然有 `.lingtai/` 下的文件系统家园、信箱、可检查的日志，以及休眠、刷新、复苏、凝蜕等生命周期控制。Soul flow 给智能体内置了一条反思回路：空闲一段时间后，它可以重新审视局面、发现漏掉的角度，并主动提出下一步，而不是永远等下一句提示词。

## 三条命令开始

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
mkdir my-project && cd my-project
lingtai-tui
```

> 一行安装脚本支持 macOS、Linux 和 WSL。**在 Windows 上，推荐用 WSL 获得完整能力。**
>
> 原生 Windows（PowerShell）现提供**实验性**安装——TUI 和 portal 可用，但原生环境下 daemon/分身不可用，`bash` 工具走的是 `cmd.exe` 而非 bash。在 PowerShell 中执行：
>
> ```powershell
> iwr -useb https://lingtai.ai/install.ps1 | iex
> ```

> 第一次用 LingTai？先看[小白工作手册](docs/beginner-work-manual.zh.md)（或[单文件 HTML 图解版](docs/beginner-work-manual-stick-figure.zh.html)），再创建第一个项目。

一行安装脚本会装好 `lingtai-tui`、`lingtai-portal` 和运行时 venv——优先用预编译 release 资源，当你的平台没有预编译二进制时再回退到源码编译。首次启动时，灵台会创建 `.lingtai/`，准备自己的运行时，引导你配置模型/配方，并为这个项目启动一个常驻智能体。

```text
project/
└── .lingtai/
    ├── human/              # 你的信箱身份
    └── <agent>/            # 一个常驻项目智能体
        ├── inbox/ outbox/  # 消息会唤醒智能体
        ├── knowledge/      # 持久事实与经验
        ├── system/         # pad、总结、长期规则
        └── logs/           # 可审计的运行轨迹
```

> PyPI 上的 `lingtai` 包确实存在，但那是 TUI 代你管理的 Python 运行时。安装/升级请用一行安装脚本（或下方源代码方式）；只有在你开发或诊断内核本身时才需要 `pip`。

源代码编译、大陆镜像等路径见 [安装详解](#安装详解)。

## 适合用来做什么

| 当你想要…… | 灵台实际会做什么 |
|---|---|
| **每日项目运营** | 扫描变化、记住决策、整理阻塞项，在你坐下前把简报发来。 |
| **带判断的 GitHub 分诊** | 读取 issue/PR，分类风险，草拟回复或补丁；真正有副作用前停下来让你批准。 |
| **能沉淀为产物的调研** | 搜索、抓取、比较、引用，最后交付独立 HTML 备忘录，而不是一段松散聊天记录。 |
| **可以跑很久的编码工作** | 调用 Claude Code、Codex、OpenCode、shell 和本地文件；灵台负责计划、记忆和对人同步。 |
| **会行动的定时任务** | “每个工作日 9 点检查部署队列，卡住了在 Telegram 叫我”——不只是提醒。 |
| **跨会话记忆** | 保留路径、偏好、合作者背景、历史教训和可复用流程，留给下一轮继续用。 |

## 为什么不同

| Agent 工具 / 编码助手 | 灵台元组织构建器 |
|---|---|
| 对话或一次运行本身就是产品。 | 项目里的组织才是产品；对话只是入口。 |
| OpenClaw、Hermes 这类工具是能干活的工人。 | 灵台提供围绕工人的长期组织图：记忆、信箱、角色、生命周期、监督与恢复。 |
| 关掉窗口，关系就结束。 | 智能体有本地主目录、信箱、日志、记忆和生命周期。 |
| 主动性取决于用户再次提示。 | Soul flow 让空闲智能体能自我反思、发现盲点、提出下一步。 |
| 扩展意味着同时管理更多聊天或运行。 | 化出长期分身负责专门领域，或用短期神识拆并行批活；在 portal 里看拓扑变化。 |
| 一次坏 turn 只能重开并祈祷。 | 休眠、唤醒、刷新、复苏、清上下文、doctor、凝蜕都是运行时的一部分。 |

## 架构一眼看懂

```mermaid
flowchart LR
    human[Human] --> channels[TUI / Telegram / Email / Feishu / WeChat]
    channels --> agent[常驻 LingTai 智能体]
    agent --> soul[Soul flow: 反思 + 主动下一步]
    agent --> tools[Files / Bash / Web / Vision / MCP / Coding CLIs]
    agent --> memory[Pad / Knowledge / Skills / Character]
    agent --> lifecycle[Sleep / Wake / Refresh / CPR / Molt]
    agent --> network[组织: avatars + daemons]
    network --> portal[实时组织可视化]
    tools --> artifacts[报告 / 补丁 / 笔记 / 定时工作]
    soul --> agent
    memory --> agent
```

## 跟着工作一起长成组织

先从一个常驻智能体开始。工作变大时，组织也一起长大：

- **凝蜕，而不是失忆。** 长会话会卸下噪音 transcript，把总结和持久记忆带到下一轮。
- **反思，而不是干等。** Soul flow 给智能体一个内在复盘回路，项目安静下来或路径不对时能主动提出洞见。
- **化出分身。** 给长期专家一个自己的记忆、信箱和责任边界。
- **遣出神识。** 把嘈杂批量任务拆给短期工人，只留下结论。
- **把编码 agent 当手。** Claude Code、Codex、OpenCode、OpenClaw、Hermes 等工具负责精确执行；灵台负责计划、记忆、协调和对人沟通。
- **现场看组织生长。** Portal 显示谁在线、在做什么，以及组织拓扑如何变化。

<div align="center">

<img src="docs/assets/network-demo.gif" alt="灵台 portal 展示一个本地常驻项目智能体组织" width="100%">

</div>

## 外接渠道

灵台把同一个长存助理接到你已经在用的消息平台上。目前精选的 MCP 插件：

| 插件 | 用途 |
|---|---|
| `telegram` | 在 Telegram 跟你的助理对话（DM、可选白名单、附件/语音透传）。 |
| `feishu` | 飞书 / Lark——使用 WebSocket 长连接，**无需公网 IP，无需 Webhook**。 |
| `wechat` | 通过 iLink / gewechat 风格的桥接接入微信。 |
| `whatsapp` | 通过灵台精选 WhatsApp 桥接接入 WhatsApp。 |
| `imap` | 真正的 IMAP/SMTP 邮件——多账号、对陌生发件人有安全默认。 |

渠道是同一个助理的多个入口，不是各自独立的机器人。**记忆、工具、历史在所有渠道之间共享**。配置入口在 TUI 的 `/mcp` 控制面板，或者直接写到 `init.json` 里。

凭证存在本地 `.secrets/` 目录（绝不会进 Git）。对陌生外部发件人默认不会自动回复。外部副作用（发消息、提 issue、删除资源）默认按真实操作对待。

## 界面

### TUI

`lingtai-tui` 是主交互界面。提供：项目初始化、模型/预设配置、对话与信箱、助理状态（token + 体力 + 心跳）、分身/神識可见性、Markdown 渲染、命令面板、升级与 doctor 流程。

在 TUI 里输入 `/help` 可查看完整斜杠命令参考。权威文档维护在内置 [`lingtai-tui-help` 技能](tui/internal/preset/skills/lingtai-tui-help/assets/slash-commands.zh.md)；顶层 README 只链接到那里，避免重复维护命令目录。

常用 Shell 入口：

```bash
lingtai-tui                          # 在当前项目打开 TUI
lingtai-tui list [--detailed] [--admin] <project>  # 以通讯簿视图列出在线助理，并标出主 agent
lingtai-tui spawn <dir> --preset <name> [--agent-name <name>]
lingtai-tui bootstrap                # 重新展开自带技能/工具
lingtai-tui self-update             # 按检测到的安装方式升级 TUI 二进制
lingtai-tui doctor                   # 修复/升级 TUI 运行时
```

`doctor` 还会报告检测到的 TUI 安装方式（`homebrew`、`source/user-local` 或 `unknown/other`）。源码/用户本地安装和未知安装会得到手动升级指引，不会被强行路由到 `brew`。

### Portal

`lingtai-portal` 是可视化服务器。它读取项目状态，呈现智能体网络、信件边、历史拓扑。当一个项目里不止一个助理、或者你想看清工作如何演变时，这玩意儿很有用。

### 小贴士

- 终端用深色主题——灵台的调色板是按深色调过的。
- TUI 里 `Ctrl+E` 打开外部编辑器写长消息。
- 选择文本时按住 `Option`（macOS / iTerm2）或 `Shift`（多数 Linux/Windows 终端），避免被 TUI 抓取。
- 升级后哪里不对劲？跑 `/doctor`（或在 shell 里 `lingtai-tui doctor`）。

## 文件系统可以直接看

灵台**故意**把状态放在磁盘上。`ls`、`cat`、`tail`、`jq`、`grep`、编辑器、甚至另一个编程智能体都能直接看。首次启动后的目录形状：

```text
project/
└── .lingtai/
    ├── human/                  # 你的信箱身份
    ├── <agent-name>/            # 一个在线的助理
    │   ├── init.json            # 模型、工具、配方、MCP 配置
    │   ├── system/              # 提示分层、Pad、规则、总结
    │   ├── knowledge/           # 持久私有记忆
    │   ├── inbox/ outbox/       # 内部信件
    │   ├── logs/                # 事件日志 + 人读日志
    │   ├── delegates/           # 化身台账
    │   ├── daemons/             # 神識运行记录
    │   └── .agent.json          # 心跳、状态、身份卡
    └── .portal/                 # 可视化的拓扑与历史
```

常用排查命令：

```bash
lingtai-tui list --detailed /path/to/project               # 在线助理、主 agent 标记、身份/状态/路径
tail -f /path/to/project/.lingtai/<agent>/logs/agent.log    # 看人读日志
jq -r '.event' /path/to/project/.lingtai/<agent>/logs/events.jsonl | tail   # 看结构化事件
```

## 跟编程智能体搭配

灵台助理生活在文件系统里。任何能读写文件的编程智能体都可以跟它们协作——大家共享 `.lingtai/human/` 这个信箱。

- **Claude Code** — `claude plugin add Lingtai-AI/claude-code-plugin`
- **OpenAI Codex CLI** — `git clone https://github.com/Lingtai-AI/codex-plugin.git && cd codex-plugin && ./install.sh`
- **其他编程智能体**（OpenCode、OpenClaw、Hermes 等）—— 把 [`lingtai-skill`](https://github.com/Lingtai-AI/lingtai-skill) 这个权威协议技能放进你工具的技能目录即可。

两者搭配的分工是：编程智能体可靠、可验证——每一次工具调用看得见、每一次编辑可审查。灵台助理富有创造力、异步、有耐心——在不会撑爆上下文窗口的并行空间里跑调研、起草、监控、长线任务。**编程智能体当手，灵台当长期大脑。**

## 安装详解

### 一行安装脚本（推荐）

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
lingtai-tui

# 之后升级：重新跑一遍脚本即可
curl -fsSL https://lingtai.ai/install.sh | bash
```

安装脚本支持 macOS、Linux 和 WSL，会装好 `lingtai-tui` 和 `lingtai-portal`——优先用预编译 release 资源，当你的平台没有预编译二进制时再回退到源码编译。

**原生 Windows（PowerShell）——实验性。** 在 PowerShell（5.1+）中执行 `iwr -useb https://lingtai.ai/install.ps1 | iex`。`install.ps1` 会下载预编译的 Windows 二进制，安装到每用户目录（`%LOCALAPPDATA%\Programs\lingtai\bin`）、加入用户 `PATH`，并用 `uv` 装好 Python 运行时 venv。支持 `-Version`、`-BinDir`、`-SkipPortal`、`-SkipVenv`、`-NoModifyPath`、`-DryRun` 参数。原生 Windows 为降级能力：daemon/分身不可用，`bash` 工具走 `cmd.exe`——**要完整能力，请用 WSL2 配合上面的 `install.sh`。** 目前没有原生 PowerShell 自更新（`lingtai-tui self-update` 不管理 PowerShell 安装）；升级请重新执行 `iwr -useb https://lingtai.ai/install.ps1 | iex`。

升级完后重启 TUI，让新的二进制接管。Python 运行时由 TUI 在 `~/.lingtai-tui/runtime/venv/` 下统一管理——往系统 Python 里 `pip install lingtai` 不会影响在运行的项目。

也可以运行 `lingtai-tui self-update`，让 TUI 按检测到的安装方式升级自己的二进制；未知安装会停下并给出指引。

<details>
<summary><b>首次安装？装好 curl 与编译前置</b></summary>

安装脚本需要 `curl`。源码编译回退通常会自己下载临时 Go 和 Node 工具链；极简 Linux / WSL 镜像如果缺系统工具，先装下面这些再重跑。

**Linux / WSL：**
```bash
sudo apt update
sudo apt install -y curl ca-certificates tar git
```

然后重新执行 `curl -fsSL https://lingtai.ai/install.sh | bash`。

</details>

<details>
<summary><b>大陆用户：源码编译回退的镜像怎么选（Go 与 npm 已解耦）</b></summary>

当安装脚本回退到源码编译时，会**自动识别大陆网络**：用 3 秒超时探测 `proxy.golang.org`，探测失败（大陆常见）才切到大陆镜像编译。Go 模块和 npm 现在是**互相独立**的两套——Go 代理挂了不会连累 npm，反之亦然：

- **Go 模块**：探测失败时用 `GOPROXY=https://goproxy.cn,direct` 与 `GOSUMDB=sum.golang.google.cn`（Google 自家的 CN 可达别名，CN 编译必需）。要手动指定，导出 `GOPROXY`。
- **npm 仓库**：只有在 Go 探测失败 **且** 用 `npm` 客户端探测 `registry.npmmirror.com` 成功时才切过去（用 npm 而非 curl，是为了走 `npm ci` 同一套 Node/npm TLS 信任链）。探测失败就**保持 npm 默认仓库不动**。

如果 npm 走 npmmirror 报证书 / TLS 错，最省事的办法是别动 npm 仓库，让它用默认源；或显式指定一个你本机能正常用的 npm 源（如官方源 `https://registry.npmjs.org`）。镜像反复出问题时，最稳的是直接用下面的「从源码编译」一节，自己掌控 `GOPROXY` 与 npm 源。

</details>

<details>
<summary><b>从源码编译</b>（大陆用户推荐，需要 Go 1.26.1+）</summary>

```bash
# 将 VERSION 替换为最新 release 版本号；下面只是示例
VERSION=v0.9.1

# 从 Gitee 镜像下载源码（国内快）
curl -L "https://gitee.com/huangzesen1997/lingtai/repository/archive/${VERSION}.tar.gz" -o lingtai.tar.gz
tar xzf lingtai.tar.gz
cd "lingtai-${VERSION}/tui"

# 编译安装
go build -ldflags "-X main.version=${VERSION}" -o /usr/local/bin/lingtai-tui .

# 清理
cd ../.. && rm -rf "lingtai-${VERSION}" lingtai.tar.gz

lingtai-tui
```

也可以从 GitHub 下载源码：
```bash
curl -L "https://github.com/Lingtai-AI/lingtai/archive/refs/tags/${VERSION}.tar.gz" -o lingtai.tar.gz
```

如果源码/用户本地版本是通过仓库根目录的 `install.sh` 安装的，后续可以直接运行：

```bash
lingtai-tui self-update
```

更新器会读取 `~/.lingtai-tui/install.json`，按最新 release tag 重新运行源码安装器，并验证更新后的二进制。进阶/手动场景可直接使用安装器约定：

```bash
./install.sh --update --prefix <prefix> --version <tag> --non-interactive
```

启动时，源码/用户本地安装也会检查新的 TUI release。有更新时，灵台会先询问再运行源码更新；选择否会保持当前二进制不变，并提示稍后可运行 `lingtai-tui self-update`。

</details>

### 内核开发模式（进阶）

**只有**当你在改内核代码、想让改动立即在 TUI 运行时生效时才需要：

```bash
~/.lingtai-tui/runtime/venv/bin/pip3 install -e /path/to/lingtai-kernel
```

### 运行时修复

```bash
lingtai-tui doctor
```

`doctor` 会检查 TUI / 内核 / 运行时三者关系，刷新自带工具技能，给出具体修复步骤。启动失败或升级看起来不对劲时用它。

它也会打印检测到的 TUI 安装方式。如果当前二进制不是 Homebrew 管理的，`doctor` 会保持二进制不动，并指向对应 release，而不是猜测性地运行 `brew`。

## 架构

灵台由两个仓库组成：

| 仓库 | 语言 | 负责 |
|---|---|---|
| [`Lingtai-AI/lingtai`](https://github.com/Lingtai-AI/lingtai)（本仓库） | Go + TypeScript | TUI、portal、安装器（`install.sh`、实验性 `install.ps1`）、release 资源 / Homebrew tap、自带工具技能 |
| [`Lingtai-AI/lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python（+ Rust sidecar） | 智能体运行时、LLM 回合循环、固有工具、会话/上下文/凝蜕管理、MCP 宿主。在 PyPI 上以 `lingtai` 发布 |

Go 写的 TUI **不**承担智能体心智，它启动并监管 Python 内核智能体作为子进程；UI 与智能体之间所有交互都走项目文件系统（`.lingtai/` 信箱、心跳、日志、提示文件、portal 记录）。**这就是为什么状态如此易查、其他工具不靠任何 SDK 就能跟它协作。**

本仓库自带两个 Go 二进制：

| 目录 | 二进制 | 简介 |
|---|---|---|
| `tui/` | `lingtai-tui` | Bubble Tea 终端应用：安装向导、进程监控、斜杠命令、预设编辑器、升级/doctor |
| `portal/` | `lingtai-portal` | Go HTTP 服务器，内嵌 React 前端，做拓扑/重放可视化 |

## 文档路径

- **第一次用？** 跑 `lingtai-tui`，选 **Tutorial** 配方，跟着走一遍；也可以先读 [《灵台工作手册（初学者友好版）》](docs/beginner-work-manual.zh.md)。
- **想看可视化解释？** 打开 [单文件图解版工作手册](docs/beginner-work-manual-stick-figure.zh.html)；这个 HTML 不依赖外部资源，可直接双击打开。
- **配外部渠道** — TUI 里 `/mcp`，再看对应插件自己的入门文档。
- **写技能** — 首次启动后看 `tui/internal/preset/skills/lingtai-dev-guide/`。
- **源码结构** — 从 [`ANATOMY.md`](ANATOMY.md) 看起，再下到 `tui/ANATOMY.md` 或 `portal/ANATOMY.md`。
- **发布流程** — [`RELEASING.md`](RELEASING.md)。
- **贡献** — 先读 anatomy，开 worktree，PR 附验证记录。详见 [贡献指南](#贡献)。

## 仓库结构

```text
.
├── README.md / README.zh.md / README.wen.md
├── ANATOMY.md                 # 给智能体和人读的源码地图
├── CLAUDE.md                  # 编程智能体指南
├── RELEASING.md               # 发布清单
├── install.sh                 # 源码安装脚本
├── tui/                       # lingtai-tui Go 模块
│   ├── main.go
│   ├── internal/              # TUI 实现
│   ├── i18n/                  # en/zh/wen UI 文本
│   └── packages/              # npm 包装元数据
├── portal/                    # lingtai-portal Go 模块
│   ├── main.go
│   ├── web/                   # React/Vite 前端
│   └── i18n/
├── docs/                      # 设计笔记、博客、状态、已知限制
├── examples/                  # 示例 init/addon/policy JSONC
├── scripts/                   # 辅助脚本
└── discussions/               # 设计补丁与调研记录
```

## 排障

**`lingtai-tui` 找不到。** `install.sh` 结尾会打印一条 PATH 提示——照做后重开终端。二进制通常在 `/usr/local/bin/lingtai-tui` 或 `~/.local/bin/lingtai-tui`；原生 Windows（PowerShell）在 `%LOCALAPPDATA%\Programs\lingtai\bin`。若你是通过 Homebrew 安装（旧/进阶方式），确认 `brew --prefix`/bin 在 `PATH` 上。

**TUI 起来了但助理不响应。** 跑 `lingtai-tui doctor` 和 `lingtai-tui list /path/to/project`，再 `tail -100 /path/to/project/.lingtai/<agent>/logs/agent.log`。

**技能或命令丢了。** `lingtai-tui bootstrap`（或在 TUI 里 `/doctor`）会重新展开自带工具。

**升级了但行为没变。** 两层：Go TUI 二进制（安装器/Homebrew）和 Python 运行时（TUI 管理的 venv）。升级后**记得重启 TUI**。运行时看起来旧的话跑 `doctor`。往系统 Python 里 `pip install lingtai` 不会影响项目。

**在改内核但本地修改不生效。** 看 [内核开发模式](#内核开发模式进阶)。

## 开发

非平凡改动请在 `origin/main` 上开 Git worktree：

```bash
cd /path/to/lingtai
git fetch origin main
git worktree add -b docs/my-change .worktrees/my-change origin/main
cd .worktrees/my-change
```

验证：

```bash
# TUI 改动
cd tui && go test ./... && go vet ./... && go build -o bin/lingtai-tui .

# Portal 改动
cd portal/web && npm ci && npm run build && cd .. && go test ./... && go build -o bin/lingtai-portal .

# 仅文档
git diff --check && git status --short
```

如果文档改动涉及到生成的 UI 命令或自带技能，跑 `lingtai-tui bootstrap` 后检查 `~/.lingtai-tui/commands.json`。

## 贡献

灵台的贡献讲求**有源可查、按既有流程走**：

1. 先读相关 anatomy：根目录的 `ANATOMY.md`，再下到 `tui/ANATOMY.md` 或 `portal/ANATOMY.md`。
2. 开分支或 worktree。
3. 改动保持收敛。
4. 跑对应的验证命令。
5. 结构性改动同步更新 anatomy / 文档。
6. PR 里说清楚：改了什么、为什么、怎么验证的。

常被需要帮忙的方向：TUI 易用性与无障碍、portal 可视化与重放、MCP/插件入门资源、跨平台安装打磨、文档与教程、运行时诊断、高质量可复用技能。

## 设计哲学

**灵台**取自心源——方寸之间，万象由此生。这个产品坚持三条朴素信念：

1. **助理需要身体。** 持久的文件系统主目录给它连续性、可见性，以及一个能不断积累工具与记忆的地方。
2. **网络应当因服务而生长。** 当一项任务需要新能力时——写一个技能、记一条知识、化出一个专才——下一项任务就会更轻。
3. **记忆必须分层。** 对话是临时的；Pad、Character、Knowledge、技能、信件才是真正承载经验的部分。

目标不是炫技，是**真正能用的长期协作者**：可被检视、可被重启、可被教导、可被改进。

灵台方寸山，斜月三星洞。完整宣言见 [lingtai.ai](https://lingtai.ai)。

## 社群

- 官网与发布日志：<https://lingtai.ai>
- 主仓库：<https://github.com/Lingtai-AI/lingtai>
- 内核仓库：<https://github.com/Lingtai-AI/lingtai-kernel>
- Discord：<https://discord.gg/cMchjXpg>
- GitHub Issues：<https://github.com/Lingtai-AI/lingtai/issues>
- GitHub Discussions：<https://github.com/Lingtai-AI/lingtai/discussions>

**微信交流群**

扫码加作者微信（备注 *lingtai*），拉入测试群。二维码会定期更新，若过期请提 issue。

<img src="docs/assets/wechat.png" alt="微信二维码 — 扫码加入 lingtai 测试群" width="200">

## Star history

[![Star History Chart](https://api.star-history.com/svg?repos=Lingtai-AI/lingtai&type=Date)](https://www.star-history.com/#Lingtai-AI/lingtai&Date)

## 许可

Apache-2.0 — 见 [LICENSE](LICENSE)
