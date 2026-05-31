<div align="center">

<img src="docs/assets/network-demo.gif" alt="灵台 — 常驻本地的个人 AI 助理，需要时能成长为团队" width="100%">

# 灵台 LingTai

**常驻本地、随叫随到，能干活的 AI 助理。**

[English](README.md) · [中文](README.zh.md) · [文言](README.wen.md) · [lingtai.ai](https://lingtai.ai) · [发布日志](https://lingtai.ai/releases/)

[![Homebrew](https://img.shields.io/badge/brew-lingtai--tui-%237dab8f)](https://github.com/Lingtai-AI/homebrew-lingtai)
[![License](https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f)](LICENSE)
[![Kernel](https://img.shields.io/badge/内核-lingtai--kernel-%237dab8f)](https://github.com/Lingtai-AI/lingtai-kernel)
[![Blog](https://img.shields.io/badge/博客-lingtai.ai-%23d4a853)](https://lingtai.ai)
[![Discord](https://img.shields.io/badge/discord-加入-%235865F2?logo=discord&logoColor=white)](https://discord.gg/cMchjXpg)

</div>

---

**灵台**是一个本地常驻的 AI 助理：把它放进你的项目目录里，它会记住该记住的东西，通过你已经在用的渠道（Telegram、飞书、微信、WhatsApp、邮件、终端）跟你对话，替你跑工具、跑流程；当任务大到一个助理顾不过来，它能成长为一支由你监管的小型 AI 团队。

它不是一次性的对话窗口，不是 Notebook，也不是只懂写代码的智能体。一个灵台助理是一个真实的进程——有项目主目录、持久记忆、信箱、工具，还有心跳。你在终端、Telegram 或邮件里布置一件事，下次回来时它已经在做。一个人手不够时，可以让它**化出分身**——一个有自己记忆、自己生命周期的专才助理，长期负责某一类工作并不断学习。

如果你想要一个**真正属于你自己**的 AI 工作台——本地、持久、可脚本化、需要时能扩大规模——这就是它。

## 安装

macOS 和 Linux 推荐路径：

```bash
brew install lingtai-ai/lingtai/lingtai-tui
lingtai-tui
```

剩下的 TUI 都会自己处理：自带 Python 运行时、引导你选模型、打开第一个项目，几分钟内就有一个可用的助理出现在面前。首次启动建议选 **Adaptive**（自适应）配方让能力按需出现，或选 **Tutorial**（教程）走一遍引导。

> PyPI 上的 `lingtai` 包确实存在，但那是 TUI 自动管理的 Python 运行时。**安装/升级请用 Homebrew**（或下方的源码方式）；只有在你**开发内核本身**时才需要 `pip`。

国内镜像、Homebrew 安装、源码编译详见 [安装详解](#安装详解)。

## 用它来做什么

下面是灵台真实在做的事：

- **每日项目简报。** 早上你坐下之前，助理已经把昨天的进度扫了一遍，整理出待办，把简报发到 Telegram 或 TUI。
- **GitHub Issue/PR 分诊。** 它读取新动态、分类、起草回复让你过目，把有风险的部分挑出来给你定夺。
- **准备直播或讲座。** 列大纲、推演论点、收集参考资料、整理讲稿——这些都跨会话保留在项目记忆里。
- **调研与投资备忘录。** 多源网络调研、网页解析、论文抓取，再起草、修改，全程留有可审计的引用链。
- **长时间的编码与代码评审。** 它可以把 Claude Code、Codex 或 OpenCode 当作自己的“手”：编程 CLI 负责精确改文件和跑测试，灵台负责计划、记忆、评审记录和给人的同步。
- **能执行的提醒，不只是通知。** "每个工作日早上 9 点检查部署队列，卡住了在 Telegram 上叫我。"
- **跨会话不会消失的个人知识。** 决策、路径、合作者偏好、过往教训——作为持久知识条目存在助理那里，而不是某个对话窗口里。

## 它能做什么

| | |
|---|---|
| **就住在你的项目里** | 每个项目一个 `.lingtai/` 目录。助理是真正的进程，有自己的主目录——你可以 `ls`、`cat`、`tail -f` 它。 |
| **可读的长期记忆** | Pad（活跃上下文）、Knowledge（持久事实）、Character（助理是谁）。都是磁盘上的 Markdown，不是隐藏的向量库。 |
| **技能与工作流** | 按需加载的可复用流程：网络调研、论文抓取、图像/音频理解、MCP 排障、发布流水线，也包括你自己写的。技能按需出现，提示词保持精简。 |
| **多个渠道，同一个大脑** | TUI、Telegram、飞书、微信、WhatsApp、IMAP 邮箱——同一个长存进程，同一份记忆和工具。换渠道不换助理。 |
| **真工具** | 读写文件、执行 shell、网页搜索、抓取页面、看图、听音、调用任意 MCP 服务器，还能把实现工作委派给 Claude Code、Codex、OpenCode 等编程智能体。 |
| **定时任务与自动化** | 周期性任务、定时检查、提醒——助理**真的会去执行**，结果按你指定的渠道送达。 |
| **需要时能成长为团队** | 化出持久的**分身**（avatar，专才助理，有自己的记忆）或临时的**神識**（daemon，专门跑一波批量活儿的工作者）——整张网络在一个地方监管。 |
| **可视化门户** | `lingtai-portal` 实时呈现整张智能体网络：谁在线、在做什么、谁给谁发了信、网络如何长出来的。 |
| **生命周期由你掌控** | 休眠、唤醒、刷新、复苏、清空——一目了然。崩了？`/doctor` 会修常见的几类毛病。 |

## 跟着工作量一起长大

多数项目一辈子用一个助理就够了。够用的时候，下面这段你可以完全忽略。**当工作真的变大**，灵台留好了向上走的路：

- **忘掉噪音，留下经验。** 上下文窗口是有限的。会话拉长后，助理会**凝蜕（molt）**：写下总结、卸掉冗余对话，下一世的自己接手时，Pad、Character、Knowledge、技能、信件都还在。**你不会重头开始。**
- **交给专才。** 化出**分身（avatar）**——一个有自己主目录、记忆和生命周期的对等助理。给它一个长期目标（专管文档站、专管客服信箱、专管某条研究线索），合上电脑它也在学。
- **拆分批量工作。** 化出**神識（daemon）**——专门跑一项任务的短期工人（扫 200 个文件、起草 50 封回复、并行做 30 次代码审查）。完成后返还结果即消失，父助理继续替你守着主线。
- **门户里看它生长。** 网络扩展后，门户会显示谁在线、谁给谁发了信、拓扑如何演变——既可以用于调试，也可以单纯用于欣赏，或者用来规划下一次重构。

不必一开始就上这些。**一个助理本身就完整可用**——网络是天花板，不是地板。

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

常用斜杠命令：

| 命令 | 用途 |
|---|---|
| `/setup` | 调整模型、配方、语言、工具、行为 |
| `/kanban` | 查看助理与项目状态 |
| `/mcp` | 配置外部渠道（Telegram/飞书/微信/WhatsApp/IMAP/…） |
| `/skills` | 浏览可用技能与能力 |
| `/viz` | 打开网络可视化 |
| `/insights` | 让助理对当前工作做一次自我反思 |
| `/sleep` · `/refresh` · `/cpr` · `/clear` | 生命周期：休眠、重载、复苏、清空上下文 |
| `/projects` | 切换或查看已知项目 |
| `/doctor` | 排查安装/运行时问题 |

常用 Shell 入口：

```bash
lingtai-tui                          # 在当前项目打开 TUI
lingtai-tui list <project>            # 列出当前项目的助理及状态
lingtai-tui spawn <dir> --preset <name> [--agent-name <name>]
lingtai-tui bootstrap                # 重新展开自带技能/工具
lingtai-tui doctor                   # 修复/升级 TUI 运行时
```

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
lingtai-tui list /path/to/project                          # 列出在线助理及状态
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

### Homebrew（推荐）

```bash
brew install lingtai-ai/lingtai/lingtai-tui
lingtai-tui

# 之后升级
brew update
brew upgrade lingtai-ai/lingtai/lingtai-tui
```

升级完后重启 TUI，让新的二进制接管。Python 运行时由 TUI 在 `~/.lingtai-tui/runtime/venv/` 下统一管理——往系统 Python 里 `pip install lingtai` 不会影响在运行的项目。

<details>
<summary><b>首次安装？先装 Homebrew</b></summary>

**macOS：**
```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

**Linux / WSL：**
```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
sudo apt install build-essential
```

然后执行 `brew install lingtai-ai/lingtai/lingtai-tui`。

</details>

<details>
<summary><b>大陆用户：用清华镜像加速 Homebrew 本身</b>（推荐先做这一步）</summary>

如果 `brew install` / `brew update` 卡在拉取 `homebrew-core` 索引或下载 bottle（`ghcr.io`，国内经常不可达），先把 Homebrew 本身的源指向清华 [TUNA 镜像](https://mirrors.tuna.tsinghua.edu.cn/help/homebrew/)，再装本项目：

```bash
# 当前 shell 生效并写入 ~/.zprofile（macOS 默认 shell）。
# 用 bash 的用户把 ~/.zprofile 换成 ~/.bash_profile 即可。
cat >> ~/.zprofile <<'EOF'
export HOMEBREW_API_DOMAIN="https://mirrors.tuna.tsinghua.edu.cn/homebrew-bottles/api"
export HOMEBREW_BOTTLE_DOMAIN="https://mirrors.tuna.tsinghua.edu.cn/homebrew-bottles"
export HOMEBREW_BREW_GIT_REMOTE="https://mirrors.tuna.tsinghua.edu.cn/git/homebrew/brew.git"
EOF
source ~/.zprofile
brew update
```

之后正常 `brew install lingtai-ai/lingtai/lingtai-tui` 即可。这一步与下面的 Gitee tap 互相独立。

</details>

<details>
<summary><b>大陆用户：用 Gitee 镜像 tap</b>（brew tap 从 GitHub 拉取失败时）</summary>

如果 `brew install lingtai-ai/lingtai/lingtai-tui` 卡在 `brew tap` 阶段（GnuTLS / TLS 错误），改用 Gitee 镜像的 tap：

```bash
brew tap lingtai-ai/lingtai https://gitee.com/huangzesen1997/homebrew-lingtai.git
brew install lingtai-ai/lingtai/lingtai-tui
```

公式本身与 GitHub tap 一致——自动识别大陆网络，编译时使用 `goproxy.cn` + `registry.npmmirror.com`。Gitee tap 是镜像，公式更新可能比 GitHub 延迟几小时。

</details>

<details>
<summary><b>从源码编译</b>（大陆用户推荐，需要 Go 1.24+）</summary>

```bash
# 将 VERSION 替换为最新版本号
VERSION=v0.5.2

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

## 架构

灵台由两个仓库组成：

| 仓库 | 语言 | 负责 |
|---|---|---|
| [`Lingtai-AI/lingtai`](https://github.com/Lingtai-AI/lingtai)（本仓库） | Go + TypeScript | TUI、portal、Homebrew/源码安装、自带工具技能 |
| [`Lingtai-AI/lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python（+ Rust sidecar） | 智能体运行时、LLM 回合循环、固有工具、会话/上下文/凝蜕管理、MCP 宿主。在 PyPI 上以 `lingtai` 发布 |

Go 写的 TUI **不**承担智能体心智，它启动并监管 Python 内核智能体作为子进程；UI 与智能体之间所有交互都走项目文件系统（`.lingtai/` 信箱、心跳、日志、提示文件、portal 记录）。**这就是为什么状态如此易查、其他工具不靠任何 SDK 就能跟它协作。**

本仓库自带两个 Go 二进制：

| 目录 | 二进制 | 简介 |
|---|---|---|
| `tui/` | `lingtai-tui` | Bubble Tea 终端应用：安装向导、进程监控、斜杠命令、预设编辑器、升级/doctor |
| `portal/` | `lingtai-portal` | Go HTTP 服务器，内嵌 React 前端，做拓扑/重放可视化 |

## 文档路径

- **第一次用？** 跑 `lingtai-tui`，选 **Tutorial** 配方，跟着走一遍。
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

**`lingtai-tui` 找不到。** 确认 Homebrew 的 bin 目录在 `PATH`（`brew --prefix`/bin）。如果用 `install.sh` 装的，看 `/usr/local/bin/lingtai-tui` 或 Homebrew 前缀。

**TUI 起来了但助理不响应。** 跑 `lingtai-tui doctor` 和 `lingtai-tui list /path/to/project`，再 `tail -100 /path/to/project/.lingtai/<agent>/logs/agent.log`。

**技能或命令丢了。** `lingtai-tui bootstrap`（或在 TUI 里 `/doctor`）会重新展开自带工具。

**升级了但行为没变。** 两层：Go TUI 二进制（Homebrew/源码）和 Python 运行时（TUI 管理的 venv）。升级后**记得重启 TUI**。运行时看起来旧的话跑 `doctor`。往系统 Python 里 `pip install lingtai` 不会影响项目。

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
- Homebrew tap：<https://github.com/Lingtai-AI/homebrew-lingtai>
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
