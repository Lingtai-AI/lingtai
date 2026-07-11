<div align="center">

# 灵台

**能自进之数字格物者也——终身之器灵，与君及君之业偕长。**

数字格物 · 器灵终身 · 记忆自长 · 知识技能久存 · 本地为先 · 群灵成网

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

诸 agent 之器，多予人一善工而已：或一聊天之窗，转身即忘；或一编码之助，窗闭则息。**灵台异是——乃居于君项目之数字格物者，历时而愈精。** 能持一问、一码库于手，累旬累月：以证据、以诸器行事，将所学录为久藏之知识与可复用之技能，成其自有之行事之风，而以所化之专家攻其当深究之子题。君与之所共为者，皆化为来日开工之始。

其道以文件系统为本，非一聊天之窗也。凡器灵皆有宅于 `.lingtai/` 之下；其久存之状——书信、记忆、知识、技能、日志、心跳——皆在本地文件与目录中；以常用诸器、编辑器，乃至他编码智能体，皆可考之。终端虽闭，此格物者犹存：可验、可重启、可教、可复。

<div align="center">

<img src="docs/assets/network-demo.gif" alt="灵台门户：本地常驻项目器灵之组织" width="100%">

</div>

## 与一数字格物者共处之一日（乃至一月）

```text
人曰：
  “为我守此一问：我等之太阳风分类器，跨仪器而漂移否？
   往读文献，读我等之数据，行实验，随时告我。”

灵台：
  以网搜与研究之器读文献
  → 察仓中之数据集与分类器之码
  → 行实验，凡一论必对证以核之
  → 录其所得于久藏之知识库
  → 化一专家分身，专攻某一仪器之定标
  → 累旬之间，磨出自有之行事之风与可复用之技能
  → 仍由 Telegram / TUI / 邮件，携其成果奉君一简报
```

上者无一为一次而止。文献之记、既核之得、彼定标之专家、其所沉之行事之风——皆久存者也。君下旬复来，此格物者自此所积之状而续，非从零而起。同一之环，于工程亦然：持一码库，以证据复现一 bug，施补丁，而记其所以然。

## 何以须一能自进之终身格物者？

善为格物者，其所以为善，不独在果，尤在生果之法：**以证据代臆断、刻意练器至熟、录实验以存案、复盘其得而迭代之。** 灵台以此法成一成长之环，其后乃磁盘上真实之文件：

- **行事而生经验。** 事有当为，则用真器——shell、文件读写、网搜、视觉、编码智能体此手——而凡一论皆当立于证据，非出于臆度。
- **经验蒸为久存之状。** 上下文之窗将满，器灵乃“凝蜕”（凝以存菁，蜕以去芜）：存其要者，重置其窗。历一次次凝蜕，此经验积为四种可考之进境——
  - **知识**——其私之库，积研究、所得与札记。
  - **技能**——可按需而唤、亦可授之同侪之可复用之程。
  - **性格**——其不息演化之行事之风、专长与所志。
  - **分身**——为攻某子题而化之久存专家器灵，录于只增之账。
- **来日之工，自此状而起。** 后一会话重载其性格、知识与技能——是以此格物者一次利于一次，而其向君可考、可引。

此乃君可读、可审之进境，非一黑箱也。此环显而可考、可引；**其向恒操于君手**，而外部之副作用（发信、提 issue）皆按真行待之，且敬君之授权。

## 诸能，以其果言之

- **持一长线之问或业**——久存之记与所志，能越会话、越重启，乃至越终端之闭。
- **如格物者而行事**——证据为先之用器、实验、既核之得，及君可复盘之久存之记。
- **自长其器箧**——将所学蒸为可复用之技能与私之知识库。
- **越一心之量**——为深究之子题化久存之专家**分身**，为一时之并行派轻捷之**神识**。
- **就君所在而达君**——君以 TUI 及 Telegram、飞书、微信、WhatsApp、邮件诸外接之渠，与同一格物者对；而 portal 显其网络与往史。
- **恒可查、可复**——久存之项目状态，以可考之文件存于本地 `.lingtai/` 之下，非困于某托管之聊天之录也。

## 三令而启

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
mkdir my-project && cd my-project
lingtai-tui
```

一令安装之脚本，通 macOS、Linux 与 WSL（原生 Windows/PowerShell 尚在计划），装 `lingtai-tui` 与 `lingtai-portal`。此后**余事皆委于 TUI**——初启之时，作 `.lingtai/`，备其 Python 运行时，引君择模型与配方，并令一常驻格物者守此项目。后欲升级，重跑安装脚本（或 `lingtai-tui self-update`），再启 TUI 可也。

> **初入灵台？** 循 [lingtai.ai 之教程](https://lingtai.ai/wen/tutorial/) 逐步而行——自安装、首务、外接诸渠、记忆与生死，首尾一贯。

> Homebrew（`brew install lingtai-ai/lingtai/lingtai-tui`）于旧用者犹可用；然新装宜用一令之脚本。PyPI 之 `lingtai` 包者，乃 TUI 代管之 Python 运行时——唯开发或诊断内核时，方用 `pip`。

## 与之协作之数途

**TUI——`lingtai-tui`** 者，人所主用之面也：项目初始、模型/配方之设、对话与信匣、器灵之状（token + 上下文 + 心跳），及通往久存之状之诸视——`/knowledge` 观其知识库，`/skills` 观其技能之录，`/system` 观其性格与契约，`/daemons` 观后台之运，`/goal` 立一长线之志。入 `/help` 可观斜杠命令之全录（其权威之目，乃内置 [`lingtai-tui-help` 技能](tui/internal/preset/skills/lingtai-tui-help/assets/slash-commands.wen.md)，此 README 不复述之）。升级之后若有不谐，行 `lingtai-tui doctor`。

**Portal——`lingtai-portal`** 者，可视之服也。读项目之状，显器灵之网、书信之边、历史之拓扑——一项目中器灵非一，或欲观其事之所以演，则用之。

**外接诸渠**，接**同一**格物者于君素用之台——记忆、诸器、往史，通乎诸渠之间；其为一助理之众门，非各立之机器人也。可于 TUI 之 `/mcp` 面板配置，亦可于 `init.json` 中明示。凭证藏于本地 `.secrets/`（绝不入 Git）；凡外部之副作用（发讯、提 issue）皆按真行待之，于陌生发件者默不自答。

| 插件 | 所用 |
|---|---|
| `telegram` | 于 Telegram 与格物者对（私信、可选白名单、附件语音透传）。 |
| `feishu` | 飞书 / Lark——长连接之术，无需公网之址，无需回调之路。 |
| `wechat` | 以 iLink / gewechat 之桥接接微信。 |
| `whatsapp` | 以灵台精选之桥接接 WhatsApp。 |
| `imap` | 真 IMAP/SMTP 之邮——多账、于陌生者有安全之默。 |

**以编码智能体为手。** 编码 CLI 者，善为精作之手，而灵台为此手之后之心——执其长线之计、记与协。所支持之编码 CLI（如 **Claude Code**、**Codex**）可为 daemon 之后端以行专作之务；他智能体则可循共享之 `.lingtai/human/` 信匣之约，为同侪而协。

- **Claude Code** — `claude plugin add Lingtai-AI/claude-code-plugin`
- **OpenAI Codex CLI** — `git clone https://github.com/Lingtai-AI/codex-plugin.git && cd codex-plugin && ./install.sh`
- **他智能体**（OpenCode、OpenClaw、Hermes 等）—— 置 [`lingtai-skill`](https://github.com/Lingtai-AI/lingtai-skill) 协议之技能于君工具之技能目录即可。

## 可考之制式

灵台由二仓而成：

| 仓 | 语 | 所司 |
|---|---|---|
| [`Lingtai-AI/lingtai`](https://github.com/Lingtai-AI/lingtai)（本仓） | Go + TypeScript | TUI、portal、安装流水、自带工具技能。出 `lingtai-tui` 与 `lingtai-portal`。 |
| [`Lingtai-AI/lingtai-kernel`](https://github.com/Lingtai-AI/lingtai-kernel) | Python（+ Rust sidecar） | 器灵运行时、LLM 回合之环、固有诸器、会话/上下文/凝蜕之治、MCP 之宿。于 PyPI 以 `lingtai` 发。 |

Go 之 TUI **不**承器灵之心，但启并监 Python 内核器灵为子进程；面与器灵之间，凡交皆经项目文件系统（`.lingtai/` 信匣、心跳、日志、提示之文、portal 之记）。此所以其状易考、他器不假 SDK 而能与之协也。

欲观有源可考之仓图，自 [`ANATOMY.md`](ANATOMY.md) 入，而后下至 [`tui/ANATOMY.md`](tui/ANATOMY.md) 或 [`portal/ANATOMY.md`](portal/ANATOMY.md)。欲循知识图谱而行，见 [`docs/graphify.md`](docs/graphify.md)。

## 开发与贡献

编 TUI：`cd tui && make build`；编 portal：`cd portal && make build`。需 Go 1.26+、`make`，及（portal 所用之）Node.js/npm。

灵台之贡献，贵有源可考、循既定之流：

1. 先读相关 anatomy——根之 [`ANATOMY.md`](ANATOMY.md)，而后下至 `tui/ANATOMY.md` 或 `portal/ANATOMY.md`。
2. 于 `origin/main` 上开分支或 worktree；改动务收敛。
3. 行对应之验证；凡结构性之改，同步更新 anatomy 与文档。
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

**微信同道群**：扫码加作者微信（备注 *lingtai*），引入测试群。此码按时更之，若已过期，请君提 issue 相告。

<img src="docs/assets/wechat.png" alt="微信二维码 — 扫码加入 lingtai 测试群" width="200">

## 许可

Apache-2.0 — 见 [LICENSE](LICENSE)。
