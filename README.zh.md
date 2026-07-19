<div align="center">

# 灵台 LingTai

**会自我进化的数字科学家——一个陪你和你的工作一起成长的终身智能体。**

数字科学家 · 终身智能体 · 自生长记忆 · 持久知识与技能 · 本地优先 · 多智能体网络

[English](README.md) · [中文](README.zh.md) · [文言](README.wen.md) · [官网](https://lingtai.ai) · [教程](https://lingtai.ai/zh/tutorial/) · [发布日志](https://lingtai.ai/releases/)

[![License](https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f)](LICENSE)
[![Kernel](https://img.shields.io/badge/内核-lingtai--kernel-%237dab8f)](https://github.com/Lingtai-AI/lingtai-kernel)
[![Site](https://img.shields.io/badge/site-lingtai.ai-%23d4a853)](https://lingtai.ai)
[![Discord](https://img.shields.io/badge/discord-加入-%235865F2?logo=discord&logoColor=white)](https://discord.gg/8KBGVYMS)

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
  → 在 Telegram / TUI / 邮件里，带着产物给你一份简报
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
- **在你已有的地方触达你**——你通过 TUI 和 Telegram、飞书、微信、WhatsApp、邮件等外接渠道跟同一位科学家对话，而 portal 则呈现网络与历史。
- **始终可查、可恢复**——持久的项目状态以可查的文件形式存在本地 `.lingtai/` 下，而不是困在某个托管的聊天记录里。

## 快速开始

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
mkdir my-project && cd my-project
lingtai-tui
```

安装脚本支持 macOS、Linux 和 WSL，会装好 `lingtai-tui` 和 `lingtai-portal`。之后**其余的事都交给 TUI**——首次启动时它会创建 `.lingtai/`，准备自己的 Python 运行时，引导你配置模型/配方，并为这个项目启动一个常驻的科学家。之后升级，重新跑一遍安装脚本（或 `lingtai-tui self-update`）再重启 TUI 即可。

原生 Windows amd64 现有一个[仅含 TUI/Portal 的实验性预览](docs/windows-preview.md)；它不是受支持的原生 Python/内核运行时安装路径。

> **第一次用？** 跟着 [lingtai.ai 上的教程](https://lingtai.ai/zh/tutorial/) 一步步来——安装、第一个任务、外接渠道、记忆与生命周期，从头到尾走一遍。

> Homebrew（`brew install lingtai-ai/lingtai/lingtai-tui`）对老用户依然可用，但新安装推荐用一行安装脚本。PyPI 上的 `lingtai` 包是 TUI 代你管理的 Python 运行时——只有在开发或诊断内核本身时才需要动 `pip`。

更深入的 TUI/portal 更新、安装方式检测、Homebrew 与中国大陆构建路由，见内置的 [`lingtai-update` 技能](tui/internal/preset/skills/lingtai-update/SKILL.md)。

## 与它协作的几种方式

**TUI——`lingtai-tui`** 是主交互界面：项目初始化、模型/预设配置、对话与信箱、助理状态（token + 上下文 + 心跳），以及通往持久状态的各个视图——`/knowledge` 看它的知识库，`/skills` 看它的技能目录，`/system` 看它的性格与契约，`/daemons` 看后台运行，`/goal` 设一个长线目标。输入 `/help` 查看完整斜杠命令参考（权威目录是内置的 [`lingtai-tui-help` 技能](tui/internal/preset/skills/lingtai-tui-help/assets/slash-commands.zh.md)，本 README 不再重复）。升级后哪里不对劲，跑 `lingtai-tui doctor`。

**Portal——`lingtai-portal`** 是可视化服务器。它读取项目状态，呈现实时智能体网络、信件边、历史拓扑——当一个项目里不止一个智能体、或你想看清工作如何演变时，很有用。

**外接渠道** 把**同一个**科学家接到你已经在用的平台上——记忆、工具、历史在所有渠道之间共享，它们是同一个助理的多个入口，不是各自独立的机器人。配置入口在 TUI 的 `/mcp` 面板，或直接写到 `init.json` 里。凭证存在本地 `.secrets/` 目录（绝不进 Git）；外部副作用（发消息、提 issue）默认按真实操作对待，对陌生发件人默认不自动回复。

| 插件 | 用途 |
|---|---|
| `telegram` | 在 Telegram 跟你的科学家对话（DM、可选白名单、附件/语音透传）。 |
| `feishu` | 飞书 / Lark——WebSocket 长连接，无需公网 IP，无需 Webhook。 |
| `wechat` | 通过 iLink / gewechat 风格桥接接入微信。 |
| `whatsapp` | 通过灵台精选桥接接入 WhatsApp。 |
| `imap` | 真正的 IMAP/SMTP 邮件——多账号，对陌生发件人有安全默认。 |

**把编码智能体当手。** 编码 CLI 是做精确实现的好手，而灵台是这双手背后的心智——它掌管长线的计划、记忆与协调。受支持的编码 CLI（如 **Claude Code**、**Codex**）可作为 daemon 后端跑专注的实现活儿；其他智能体则可通过共享的 `.lingtai/human/` 信箱协议作为同伴协作。

- **Claude Code** — `claude plugin add Lingtai-AI/claude-code-plugin`
- **OpenAI Codex CLI** — `git clone https://github.com/Lingtai-AI/codex-plugin.git && cd codex-plugin && ./install.sh`
- **其他智能体**（OpenCode、OpenClaw、Hermes 等）—— 把 [`lingtai-skill`](https://github.com/Lingtai-AI/lingtai-skill) 协议技能放进你工具的技能目录即可。

## 可查的架构

灵台由两个仓库组成：

| 仓库 | 语言 | 负责 |
|---|---|---|
| [`Lingtai-AI/lingtai`](https://github.com/Lingtai-AI/lingtai)（本仓库） | Go + TypeScript | TUI、portal、安装流水线、自带工具技能。产出 `lingtai-tui` 与 `lingtai-portal`。 |
| [`Lingtai-AI/lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python（+ Rust sidecar） | 智能体运行时、LLM 回合循环、固有工具、会话/上下文/凝蜕管理、MCP 宿主。在 PyPI 上以 `lingtai` 发布。 |

Go 写的 TUI **不**承担智能体心智，它启动并监管 Python 内核智能体作为子进程；UI 与智能体之间所有交互都走项目文件系统（`.lingtai/` 信箱、心跳、日志、提示文件、portal 记录）。这就是为什么状态如此易查、其他工具不靠任何 SDK 就能跟它协作。

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
- Discord：<https://discord.gg/8KBGVYMS>
- Issues：<https://github.com/Lingtai-AI/lingtai/issues> · Discussions：<https://github.com/Lingtai-AI/lingtai/discussions>

**微信交流群**：扫码加作者微信（备注 *lingtai*），拉入测试群。二维码会定期更新，若过期请提 issue。

<img src="docs/assets/wechat.png" alt="微信二维码 — 扫码加入 lingtai 测试群" width="200">

## 许可

Apache-2.0 — 见 [LICENSE](LICENSE)
