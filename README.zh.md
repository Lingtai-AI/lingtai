<div align="center">

# 灵台 LingTai

**会自我进化的数字科学家——一个陪你和你的工作一起成长的终身智能体。**

数字科学家 · 终身智能体 · 自生长记忆 · 持久知识与技能 · 本地优先 · 多智能体网络

[English](README.md) · [中文](README.zh.md) · [文言](README.wen.md) · [官网](https://lingtai.ai) · [教程](https://lingtai.ai/zh/tutorial/) · [发布日志](https://lingtai.ai/releases/)

[![License](https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f)](LICENSE)
[![Kernel](https://img.shields.io/badge/内核-lingtai--kernel-%237dab8f)](https://github.com/Lingtai-AI/lingtai-kernel)
[![Site](https://img.shields.io/badge/site-lingtai.ai-%23d4a853)](https://lingtai.ai)
[![Discord](https://img.shields.io/badge/discord-加入-%235865F2?logo=discord&logoColor=white)](https://discord.gg/pfc7z2TRq)

</div>

---

多数 agent 工具给你的是一个更强的一次性工人：一个转身就忘的聊天窗口，或者一个随终端一起关闭的编码助手。**灵台不一样——它是一个住在你项目里、并会随时间越来越强的数字科学家。** 它能把一个问题、一个代码库握在手里好几周：以证据和工具做事，把学到的东西记成持久知识与可复用技能，形成自己的做事风格，并把需要深入的子问题交给它化出的专家去攻。你们一起做过的工作，会变成下一次开工的起点。

它**以文件系统为原生，不是一个聊天窗口**。每个智能体都在 `.lingtai/` 下有一个家；它的持久状态——信件、记忆、知识、技能、日志、心跳——都保存在本地文件和目录里，可以用常用工具、编辑器，甚至另一个编码智能体直接检查。关掉终端，这位科学家依然存在：可被检视、可被重启、可被教导、可被恢复。

<div align="center">

<img src="docs/assets/network-demo.gif" alt="灵台 portal 展示一个本地常驻项目智能体组织" width="100%">

</div>

## 与一位数字科学家共处的一天（乃至一个月）

```text
你
  “帮我盯住这个研究问题：我们的太阳风分类器在不同仪器上会不会漂移？
   去读文献、读我们的数据，做实验，随时同步给我。”

灵台
  用网络搜索与研究工具读文献
  → 检视仓库里的数据集与分类器代码
  → 做实验，每个论断都对着证据核验
  → 把发现记进它持久的知识库
  → 化出一个专家分身，专攻某一台仪器的定标
  → 数周之间，打磨出自己的做事风格与可复用技能
  → 通过 Desktop / TUI / Telegram / 邮件，带着产物给你一份简报
```

上面没有一样是一次性的。文献笔记、核验过的发现、那个定标专家、它沉淀下来的做事风格——全都是持久的。等你下周回来，这位科学家从这些积累的状态继续，而不是从零开始。同一套循环也一样服务工程：握住一个代码库，用证据复现一个 bug，打上补丁，并记住为什么这么改。

## 为何要一个会自我进化的终身科学家？

一个好的科学家，不只由结果定义，更由产出结果的方法定义：**以证据取代臆断、刻意把工具练熟、把实验记录在案、对发现复盘并迭代。** 灵台把这套方法化成一条成长回路，背后是磁盘上真实的文件：

- **做事产生经验。** 需要行动时，任务便调用真实的工具——shell、文件读写、网络搜索、视觉、编码智能体这双手——而每一句论断都应当立足于证据，而非猜测。
- **经验被蒸馏为持久状态。** 当上下文窗口将满，智能体会“凝蜕”（凝以存菁，蜕以去芜）：保住要紧的，重置窗口。跨越一次次凝蜕，这些经验沉淀为四种可查的成长——
  - **知识**——它私有的库，积累研究、发现与笔记。
  - **技能**——可按需调用、也可分享给同伴的可复用流程。
  - **性格**——它不断演化的做事风格、专长与目标。
  - **分身**——它为攻克某个子问题而化出的持久专家智能体，记录在只追加的账本里。
- **未来的工作从这些状态起步。** 下一次会话会重新载入性格、知识与技能——于是这位科学家一次比一次更利落，而且方向是你能查、能引导的。

这是你读得到、审得了的成长，不是一个黑箱。这条回路是显式的、可查的、可引导的；**方向始终由你掌握**，而外部副作用（发信、提 issue）都按真实操作对待，并尊重你的授权。

## 能力，以结果表述

- **握住一个长线的问题或项目**——持久记忆与目标能扛过会话、重启，乃至关掉终端。
- **像科学家一样做事**——证据优先的工具使用、实验、核验过的发现，以及你可复盘的持久记录。
- **长出自己的工具箱**——把学到的东西蒸馏为可复用技能与私有知识库。
- **超越一个脑袋的规模**——为深入的子问题化出持久的专家**分身**，为临时的并行活儿派出轻量的**神识**。
- **在你已有的地方触达你**——你通过 Desktop、TUI 和 Telegram、飞书、微信、WhatsApp、邮件等外接渠道跟同一位科学家对话，而 portal 则呈现网络与历史。
- **始终可查、可恢复**——持久的项目状态以可查的文件形式存在本地 `.lingtai/` 下，而不是困在某个托管的聊天记录里。

## 快速开始

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
mkdir my-project && cd my-project
lingtai-tui
```

<details>
<summary><b>开发版安装</b>——TUI 与内核当前 <code>main</code></summary>

<br>

如需显式安装 TUI 与内核当前 `main` 的开发版本，请运行：

```bash
curl -fsSL https://lingtai.ai/install.sh | bash -s -- --latest
```

它会打印并记录两个仓库的完整提交 SHA。该模式独立于默认稳定版安装，不能与 `--version`、`--ref`、`--update`、`--source` 或 `--skip-python` 同时使用。

</details>

安装脚本支持 macOS、Linux 和 WSL，会装好 `lingtai-tui` 和 `lingtai-portal`；macOS 还会提供 `lingtai-desktop`。

原生 Windows/PowerShell 现已支持：

```powershell
irm https://lingtai.ai/install.ps1 | iex
```

它会解析最新的发布标签，校验 Windows 二进制压缩包与锁定的内核发布版本的校验和，并安装 `lingtai-tui`/`lingtai-portal` 及 Python 运行时虚拟环境。加上 `-SkipVenv` 可只安装 TUI/portal 二进制文件。详细契约见 [`RELEASING.md`](RELEASING.md)。

<details>
<summary><b>原生 Windows 主线调试安装</b>——<code>install.ps1 -Latest</code></summary>

<br>

若要在原生 Windows 上调试当前主线，请运行 `.\install.ps1 -Latest`。它仅支持 amd64；ARM64 请使用 WSL2 和 `install.sh --latest`。

它会一次检查 Git、Go、Node.js/npm（Node 20.19+、22.12+ 或更高主版本；Node 21 及 Node 22.<12 不支持）和 64 位 CPython 3.11–3.13，然后只为缺失或不受支持的前置条件运行 `winget install --id <ID> --exact --source winget --accept-source-agreements --accept-package-agreements --disable-interactivity --silent`（`Git.Git`、`GoLang.Go`、`OpenJS.NodeJS.LTS` 和/或 `Python.Python.3.13`）。成功安装的前置条件是外部 winget 变更；如果后续包、检出或构建失败，不会自动回滚，但 LingTai 目标目录写入仍会等到验证/构建成功之后。安装器会刷新当前进程 PATH 并重新验证，再固定两个仓库 `main` 的完整 SHA、构建两个二进制文件，并通过本地路径把准确的内核检出以非 editable 方式安装到 `%USERPROFILE%\.lingtai-tui\runtime\venv`。winget 或包策略/提权阻止修复时，会失败并给出精确补救命令。

`-Latest -DryRun` 只报告精确修复计划，不调用 winget，也不写入目标目录、PATH 或配置；`-Latest` 不能与 `-Version`、`-ArchivePath` 或 `-SkipVenv` 合用。网站仓库仍需另行补充对应安装说明。

</details>

> [!TIP]
> **第一次用？** 跟着 [lingtai.ai 上的教程](https://lingtai.ai/zh/tutorial/) 一步步来——安装、第一个任务、外接渠道、记忆与生命周期，从头到尾走一遍。

> [!NOTE]
> Homebrew（`brew install lingtai-ai/lingtai/lingtai-tui`）对老用户依然可用，但新安装推荐用一行安装脚本。PyPI 上的 `lingtai` 包是 TUI 代你管理的 Python 运行时——只有在开发或诊断内核本身时才需要动 `pip`。

更深入的 TUI/portal 更新、安装方式检测、Homebrew 与中国大陆构建路由，见内置的 [`lingtai-update` 技能](tui/internal/preset/skills/lingtai-update/SKILL.md)。

## 与它协作的几种方式

**Desktop——`lingtai-desktop`（macOS）** 是灵台的原生 App：在一处查看项目和科学家、对话与收发信件、调整设置和预设，也能随时管理它们的工作。

**TUI——`lingtai-tui`** 把灵台带进终端：设置项目和模型、对话与读信、查看科学家状态；需要更深入时，可打开 `/knowledge`、`/skills`、`/system`、`/daemons` 或 `/goal`。输入 `/help` 查看完整斜杠命令参考（权威目录是内置的 [`lingtai-tui-help` 技能](tui/internal/preset/skills/lingtai-tui-help/assets/slash-commands.zh.md)，本 README 不再重复）。升级后哪里不对劲，跑 `lingtai-tui doctor`。

**Portal——`lingtai-portal`** 是可视化服务器。它读取项目状态，呈现实时智能体网络、信件边、历史拓扑——当一个项目里不止一个智能体、或你想看清工作如何演变时，很有用。

**外接渠道** 把**同一个**科学家接到你已经在用的平台上——记忆、工具、历史在所有渠道之间共享，它们是同一个助理的多个入口，不是各自独立的机器人。设置请遵循当前 MCP/精选插件文档，并先取得明确授权；TUI 的 `/mcp` 面板是只读的，只用于查看已配置的桥接及其状态。凭证存在本地 `.secrets/` 目录（绝不进 Git）；外部副作用（发消息、提 issue）默认按真实操作对待；渠道插件支持发件人白名单，随附的示例配置默认启用白名单——开放访问必须显式选择。

| 插件 | 用途 |
|---|---|
| `telegram` | 在 Telegram 跟你的科学家对话（DM、可选白名单、附件/语音透传）。 |
| `feishu` | 飞书 / Lark——WebSocket 长连接，无需公网 IP，无需 Webhook。 |
| `wechat` | 通过 iLink / gewechat 风格桥接接入微信。 |
| `whatsapp` | 通过灵台精选桥接接入 WhatsApp。 |
| `imap` | 真正的 IMAP/SMTP 邮件——多账号，带可选发件人白名单。 |

**把编码智能体当手。** 编码 CLI 是做精确实现的好手，而灵台是这双手背后的心智——它掌管长线的计划、记忆与协调。受支持的编码 CLI（如 **Claude Code**、**Codex**）可作为 daemon 后端跑专注的实现活儿；其他智能体则可通过共享的 `.lingtai/human/` 信箱协议作为同伴协作。

- **Claude Code** — `claude plugin add Lingtai-AI/claude-code-plugin`
- **OpenAI Codex CLI** — `git clone https://github.com/Lingtai-AI/codex-plugin.git && cd codex-plugin && ./install.sh`
- **其他智能体**（OpenCode、OpenClaw、Hermes 等）—— 把 [`lingtai-skill`](https://github.com/Lingtai-AI/lingtai-skill) 协议技能放进你工具的技能目录即可。

## 可查的架构

灵台由三个产品仓库组成：

| 仓库 | 产品职责 |
|---|---|
| [`Lingtai-AI/lingtai`](https://github.com/Lingtai-AI/lingtai)（本仓库） | 终端 App、可视化 portal 与安装脚本。 |
| [`Lingtai-AI/lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | 让科学家持续运行，处理工具、记忆与对话。 |
| `Lingtai-AI/lingtai-desktop` | 用来管理项目和科学家的原生 macOS App。 |

Desktop 和 TUI 是同一个灵台项目、同一批科学家的两种界面。即使你关掉界面，内核仍会让科学家继续运行、继续听候消息。项目留在本地，也始终可查，因此编辑器和其他工具一样能跟它协作。

想看有源可查的仓库地图，从 [`ANATOMY.md`](ANATOMY.md) 看起，再下到 [`tui/ANATOMY.md`](tui/ANATOMY.md) 或 [`portal/ANATOMY.md`](portal/ANATOMY.md)。想知道每一层的接口与预期的 agent 行为承诺什么，读 [`CONTRACT.md`](CONTRACT.md)。想按知识图谱导航，见 [`docs/graphify.md`](docs/graphify.md)。

## 开发与贡献

编译 TUI：`cd tui && make build`；编译 portal：`cd portal && make build`。需要 Go 1.26+、`make`，以及（portal 用的）Node.js/npm。

灵台的贡献讲求**有源可查、按既有流程走**。任何开发工作之前，先找到并阅读本仓库的本地开发指南——仓库根目录的 [`dev-guide-skill`](dev-guide-skill/SKILL.md)；它把每个任务引导到基线、分布式的 [`ANATOMY.md`](ANATOMY.md) 与 [`CONTRACT.md`](CONTRACT.md) 两套系统、验证以及 PR 关卡，而不重复它们的内容：

1. 先读相关 anatomy——根目录 [`ANATOMY.md`](ANATOMY.md)，再下到 `tui/ANATOMY.md` 或 `portal/ANATOMY.md`；改动接口或预期行为时，读配对的 [`CONTRACT.md`](CONTRACT.md)。
2. 在 `origin/main` 上开分支或 worktree；改动保持收敛。
3. 跑对应的验证。结构/导航改动，同步更新 [`ANATOMY.md`](ANATOMY.md)；接口或预期行为改动，同步更新 [`CONTRACT.md`](CONTRACT.md) 及其一致性测试；两者都变时才两者都更新。
4. PR 里说清楚：改了什么、为什么、怎么验证的。

```bash
# TUI 改动
cd tui && go test ./... && go vet ./... && go build -o bin/lingtai-tui .

# Portal 改动
cd portal/web && npm ci && npm run build && cd .. && go test ./... && go build -o bin/lingtai-portal .

# 仅文档
git diff --check && git status --short
```

发布流程见 [`RELEASING.md`](RELEASING.md)。常被需要帮忙的方向：TUI 易用性与无障碍、portal 可视化、MCP/插件入门、跨平台安装打磨、文档、运行时诊断、可复用技能。

## 社群

- 官网、教程与发布日志：<https://lingtai.ai>
- 主仓库：<https://github.com/Lingtai-AI/lingtai> · 内核：<https://github.com/Lingtai-AI/lingtai-kernel>
- Discord：<https://discord.gg/pfc7z2TRq>
- Issues：<https://github.com/Lingtai-AI/lingtai/issues> · Discussions：<https://github.com/Lingtai-AI/lingtai/discussions>

**微信交流群**：扫码加作者微信（备注 *lingtai*），拉入测试群。二维码会定期更新，若过期请提 issue。

<img src="docs/assets/wechat.png" alt="微信二维码 — 扫码加入 lingtai 测试群" width="200">

## 许可

Apache-2.0 — 见 [LICENSE](LICENSE)
