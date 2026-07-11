<div align="center">

# 灵台 LingTai

**在你的项目里建起一个 AI 组织——不只是再多一个 Agent。**

本地优先 · 常驻智能体 · soul flow 主动反思 · 信箱 · 生命周期 · 多智能体组织

[English](README.md) · [中文](README.zh.md) · [文言](README.wen.md) · [官网](https://lingtai.ai) · [教程](https://lingtai.ai/zh/tutorial/) · [发布日志](https://lingtai.ai/releases/)

[![License](https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f)](LICENSE)
[![Kernel](https://img.shields.io/badge/内核-lingtai--kernel-%237dab8f)](https://github.com/Lingtai-AI/lingtai-kernel)
[![Site](https://img.shields.io/badge/site-lingtai.ai-%23d4a853)](https://lingtai.ai)
[![Discord](https://img.shields.io/badge/discord-加入-%235865F2?logo=discord&logoColor=white)](https://discord.gg/cMchjXpg)

</div>

---

多数 agent 工具给你的是一个更强的工人。**灵台给你的是一个 AI 组织的底座**——长期住在你项目里的智能体，有自己的主目录、收发信箱、持久记忆、生命周期控制、自我反思回路，也能在任务大到一个脑袋不够时召唤同伴或化出分身。

它**以文件系统为原生，不是一个聊天窗口**。每个智能体都在 `.lingtai/` 下有一个家；所有状态——信件、记忆、日志、心跳——都是可以用 `ls`、`cat`、`jq`、编辑器、甚至另一个编程智能体直接读的普通文件。关掉终端，组织依然存在：可被检视、可被重启、可被教导、可被恢复。

```text
你
  “今晚盯一下仓库。PR 如果坏了，就定位原因，草拟修复，
   明早给我一份简报。”

灵台
  从信箱中醒来 → 读取持久项目记忆
  → 调用 shell / web / file / coding-agent 工具
  → 空闲或卡住时通过 soul flow 自我反思
  → 写下笔记、报告、补丁或定时任务
  → 需要并行时请专家分身或神识帮忙
  → 在 Telegram / TUI / 邮件里带着产物回复你
```

Claude Code、Codex、OpenClaw、Hermes 这类编码工具是很有用的“手”。灵台是围绕这些手的组织层：它把编码 agent 当作工人来用，同时保留角色、记忆、通信、监督、恢复这些让一个智能体网络在单次聊天或终端会话结束后仍能继续运转的组织能力。

## 快速开始

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
mkdir my-project && cd my-project
lingtai-tui
```

安装脚本支持 macOS、Linux 和 WSL（原生 Windows/PowerShell 在计划中），会装好 `lingtai-tui` 和 `lingtai-portal`。之后**其余的事都交给 TUI**——首次启动时它会创建 `.lingtai/`，准备自己的 Python 运行时，引导你配置模型/配方，并为这个项目启动一个常驻智能体。之后升级，重新跑一遍安装脚本（或 `lingtai-tui self-update`）再重启 TUI 即可。

> **第一次用？** 跟着 [lingtai.ai 上的教程](https://lingtai.ai/zh/tutorial/) 一步步来——安装、第一个任务、外接渠道、记忆与生命周期，从头到尾走一遍。

> Homebrew（`brew install lingtai-ai/lingtai/lingtai-tui`）对老用户依然可用，但新安装推荐用一行安装脚本。PyPI 上的 `lingtai` 包是 TUI 代你管理的 Python 运行时——只有在开发或诊断内核本身时才需要动 `pip`。

## 界面

**TUI——`lingtai-tui`** 是主交互界面：项目初始化、模型/预设配置、对话与信箱、助理状态（token + 上下文 + 心跳）、分身/神识可见性、命令面板、升级与 doctor 流程。在 TUI 里输入 `/help` 查看完整斜杠命令参考（权威目录是内置的 [`lingtai-tui-help` 技能](tui/internal/preset/skills/lingtai-tui-help/assets/slash-commands.zh.md)，本 README 不再重复）。升级后哪里不对劲，跑 `lingtai-tui doctor`。

**Portal——`lingtai-portal`** 是可视化服务器。它读取项目状态，呈现实时智能体网络、信件边、历史拓扑——当一个项目里不止一个助理、或你想看清工作如何演变时，很有用。

**外接渠道** 把**同一个**助理接到你已经在用的平台上——记忆、工具、历史在所有渠道之间共享，它们是同一个助理的多个入口，不是各自独立的机器人。配置入口在 TUI 的 `/mcp` 面板，或直接写到 `init.json` 里。凭证存在本地 `.secrets/` 目录（绝不进 Git）；外部副作用（发消息、提 issue）默认按真实操作对待，对陌生发件人默认不自动回复。

| 插件 | 用途 |
|---|---|
| `telegram` | 在 Telegram 跟你的助理对话（DM、可选白名单、附件/语音透传）。 |
| `feishu` | 飞书 / Lark——WebSocket 长连接，无需公网 IP，无需 Webhook。 |
| `wechat` | 通过 iLink / gewechat 风格桥接接入微信。 |
| `whatsapp` | 通过灵台精选桥接接入 WhatsApp。 |
| `imap` | 真正的 IMAP/SMTP 邮件——多账号，对陌生发件人有安全默认。 |

**把编码智能体当手。** 灵台助理生活在文件系统里，任何编码智能体都能驱动它们——作为 daemon 后端跑专注的实现任务，或作为同伴通过共享的 `.lingtai/human/` 信箱协作。灵台负责长线的计划、记忆与协调；编码智能体负责精确、可审查的执行。

- **Claude Code** — `claude plugin add Lingtai-AI/claude-code-plugin`
- **OpenAI Codex CLI** — `git clone https://github.com/Lingtai-AI/codex-plugin.git && cd codex-plugin && ./install.sh`
- **其他智能体**（OpenCode、OpenClaw、Hermes 等）—— 把 [`lingtai-skill`](https://github.com/Lingtai-AI/lingtai-skill) 协议技能放进你工具的技能目录即可。

<div align="center">

<img src="docs/assets/network-demo.gif" alt="灵台 portal 展示一个本地常驻项目智能体组织" width="100%">

</div>

## 架构

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
- Discord：<https://discord.gg/cMchjXpg>
- Issues：<https://github.com/Lingtai-AI/lingtai/issues> · Discussions：<https://github.com/Lingtai-AI/lingtai/discussions>

**微信交流群**：扫码加作者微信（备注 *lingtai*），拉入测试群。二维码会定期更新，若过期请提 issue。

<img src="docs/assets/wechat.png" alt="微信二维码 — 扫码加入 lingtai 测试群" width="200">

## 许可

Apache-2.0 — 见 [LICENSE](LICENSE)
