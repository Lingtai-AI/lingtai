<div align="center">

# 灵台

**于一项目之中，立器灵之组织；非但增一智能体也。**

本地为先 · 器灵常驻 · 心流自省 · 信匣往来 · 生死有制 · 群灵成网

> *灵台，心也。*
>
> *灵台者有持，而不知其所持，而不可持者也。*
> — 庄子 · 庚桑楚

[English](README.md) · [中文](README.zh.md) · [文言](README.wen.md) · [官网](https://lingtai.ai) · [教程](https://lingtai.ai/wen/tutorial/) · [发布日志](https://lingtai.ai/releases/)

[![License](https://img.shields.io/github/license/Lingtai-AI/lingtai?color=%237dab8f)](LICENSE)
[![Kernel](https://img.shields.io/badge/内核-lingtai--kernel-%237dab8f)](https://github.com/Lingtai-AI/lingtai-kernel)
[![Site](https://img.shields.io/badge/site-lingtai.ai-%23d4a853)](https://lingtai.ai)
[![Discord](https://img.shields.io/badge/discord-入-%235865F2?logo=discord&logoColor=white)](https://discord.gg/cMchjXpg)

</div>

---

诸 agent 之器，多予人一善工。**灵台所予者，AI 组织之基也**——器灵久居本地项目，各有庐舍目录、收发信匣、久藏之记、生死之制、自省之心流；事大而一心不足，则可召同侪，亦可化分身。

其道以文件系统为本，非一聊天之窗也。凡器灵皆有宅于 `.lingtai/` 之下；其一切状态——书信、记忆、日志、心跳——皆寻常文件，可以 `ls`、`cat`、`jq`、编辑器，乃至他编码智能体径读之。终端虽闭，其组织犹存：可验、可重启、可教、可复。

```text
人曰：
  “今夜守此仓。若 PR 坏，则察其故，草其修，
   明旦以简报告我。”

灵台：
  自信匣而醒 → 读久存项目之记
  → 用 shell / web / file / coding-agent 诸器
  → 闲或滞时，以 soul flow 自省
  → 书札、成报、补丁、定期之务
  → 需并行，则请分身或神识
  → 仍由 Telegram / TUI / 邮件奉复
```

Claude Code、Codex、OpenClaw、Hermes 诸编码之器，善为可役之手。灵台则立其上之组织法：能以编码智能体为工，而自守其角色、记忆、书信、督察、复苏之道，使一网器灵不以一窗既闭、一终端既息而散。

## 三令而启

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
mkdir my-project && cd my-project
lingtai-tui
```

一令安装之脚本，通 macOS、Linux 与 WSL（原生 Windows/PowerShell 尚在计划），装 `lingtai-tui` 与 `lingtai-portal`。此后**余事皆委于 TUI**——初启之时，作 `.lingtai/`，备其 Python 运行时，引君择模型与配方，并令一常驻器灵守此项目。后欲升级，重跑安装脚本（或 `lingtai-tui self-update`），再启 TUI 可也。

> **初入灵台？** 循 [lingtai.ai 之教程](https://lingtai.ai/wen/tutorial/) 逐步而行——自安装、首务、外接诸渠、记忆与生死，首尾一贯。

> Homebrew（`brew install lingtai-ai/lingtai/lingtai-tui`）于旧用者犹可用；然新装宜用一令之脚本。PyPI 之 `lingtai` 包者，乃 TUI 代管之 Python 运行时——唯开发或诊断内核时，方用 `pip`。

## 诸界面

**TUI——`lingtai-tui`** 者，人所主用之面也：项目初始、模型/配方之设、对话与信匣、器灵之状（token + 上下文 + 心跳）、分身神识之可见、命令之面板、升级与 doctor 之流。于 TUI 中入 `/help`，可观斜杠命令之全录（其权威之目，乃内置 [`lingtai-tui-help` 技能](tui/internal/preset/skills/lingtai-tui-help/assets/slash-commands.wen.md)，此 README 不复述之）。升级之后若有不谐，行 `lingtai-tui doctor`。

**Portal——`lingtai-portal`** 者，可视之服也。读项目之状，显器灵之网、书信之边、历史之拓扑——一项目中器灵非一，或欲观其事之所以演，则用之。

**外接诸渠**，接**同一**助理于君素用之台——记忆、诸器、往史，通乎诸渠之间；其为一助理之众门，非各立之机器人也。可于 TUI 之 `/mcp` 面板配置，亦可于 `init.json` 中明示。凭证藏于本地 `.secrets/`（绝不入 Git）；凡外部之副作用（发讯、提 issue）皆按真行待之，于陌生发件者默不自答。

| 插件 | 所用 |
|---|---|
| `telegram` | 于 Telegram 与助理对（私信、可选白名单、附件语音透传）。 |
| `feishu` | 飞书 / Lark——长连接之术，无需公网之址，无需回调之路。 |
| `wechat` | 以 iLink / gewechat 之桥接接微信。 |
| `whatsapp` | 以灵台精选之桥接接 WhatsApp。 |
| `imap` | 真 IMAP/SMTP 之邮——多账、于陌生者有安全之默。 |

**以编码智能体为手。** 灵台之助理居于文件系统，故凡编码智能体皆能驱之——或为 daemon 之后端以行专务，或为同侪而通乎共享之 `.lingtai/human/` 信匣。灵台执其长线之计、记与协；编码智能体司精确可审之作。

- **Claude Code** — `claude plugin add Lingtai-AI/claude-code-plugin`
- **OpenAI Codex CLI** — `git clone https://github.com/Lingtai-AI/codex-plugin.git && cd codex-plugin && ./install.sh`
- **他智能体**（OpenCode、OpenClaw、Hermes 等）—— 置 [`lingtai-skill`](https://github.com/Lingtai-AI/lingtai-skill) 协议之技能于君工具之技能目录即可。

<div align="center">

<img src="docs/assets/network-demo.gif" alt="灵台门户：本地常驻项目器灵之组织" width="100%">

</div>

## 制式

灵台由二仓而成：

| 仓 | 语 | 所司 |
|---|---|---|
| [`Lingtai-AI/lingtai`](https://github.com/Lingtai-AI/lingtai)（本仓） | Go + TypeScript | TUI、portal、安装流水、自带工具技能。出 `lingtai-tui` 与 `lingtai-portal`。 |
| [`Lingtai-AI/lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python（+ Rust sidecar） | 器灵运行时、LLM 回合之环、固有诸器、会话/上下文/凝蜕之治、MCP 之宿。于 PyPI 以 `lingtai` 发。 |

Go 之 TUI **不**承器灵之心，但启并监 Python 内核器灵为子进程；面与器灵之间，凡交皆经项目文件系统（`.lingtai/` 信匣、心跳、日志、提示之文、portal 之记）。此所以其状易考、他器不假 SDK 而能与之协也。

欲观有源可考之仓图，自 [`ANATOMY.md`](ANATOMY.md) 入，而后下至 [`tui/ANATOMY.md`](tui/ANATOMY.md) 或 [`portal/ANATOMY.md`](portal/ANATOMY.md)。欲知每层之接口与所期 agent 行为何所许，读 [`CONTRACT.md`](CONTRACT.md)。欲循知识图谱而行，见 [`docs/graphify.md`](docs/graphify.md)。

## 开发与贡献

编 TUI：`cd tui && make build`；编 portal：`cd portal && make build`。需 Go 1.26+、`make`，及（portal 所用之）Node.js/npm。

灵台之贡献，贵有源可考、循既定之流。凡开发之先，必觅而读本仓之本地开发指南——仓根之 [`dev-guide-skill`](dev-guide-skill/SKILL.md)；其引每务至基线、分布式之 [`ANATOMY.md`](ANATOMY.md) 与 [`CONTRACT.md`](CONTRACT.md) 二系、验证及 PR 之关，而不复述其文：

1. 先读相关 anatomy——根之 [`ANATOMY.md`](ANATOMY.md)，而后下至 `tui/ANATOMY.md` 或 `portal/ANATOMY.md`；改接口或所期行为者，读配对之 [`CONTRACT.md`](CONTRACT.md)。
2. 于 `origin/main` 上开分支或 worktree；改动务收敛。
3. 行对应之验证。凡结构、导览之变，同步更新 [`ANATOMY.md`](ANATOMY.md)；凡接口或所期行为之变，同步更新 [`CONTRACT.md`](CONTRACT.md) 及其一致之测；二者俱变，乃俱更之。
4. PR 中明言：何所改、何以改、何以验之。

```bash
# TUI 之改
cd tui && go test ./... && go vet ./... && go build -o bin/lingtai-tui .

# Portal 之改
cd portal/web && npm ci && npm run build && cd .. && go test ./... && go build -o bin/lingtai-portal .

# 唯文档
git diff --check && git status --short
```

发布之流见 [`RELEASING.md`](RELEASING.md)。常需相助之处：TUI 之易用与无障、portal 之可视、MCP/插件之入门、跨平台安装之磨、文档、运行时之诊、可复用之技能。

## 同道

- 官网、教程与发布日志：<https://lingtai.ai>
- 主仓：<https://github.com/Lingtai-AI/lingtai> · 内核：<https://github.com/Lingtai-AI/lingtai-kernel>
- Discord：<https://discord.gg/cMchjXpg>
- Issues：<https://github.com/Lingtai-AI/lingtai/issues> · Discussions：<https://github.com/Lingtai-AI/lingtai/discussions>

**微信同道群**：扫码加作者微信（备注 *lingtai*），引入测试群。此码按时更之，若已过期，烦君提 issue 相告。

<img src="docs/assets/wechat.png" alt="微信二维码 — 扫码加入 lingtai 测试群" width="200">

## 许可

Apache-2.0 — 见 [LICENSE](LICENSE)。
