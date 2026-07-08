# 灵台工作手册：从安装到第一个可用任务（初学者版）

> 这份手册写给第一次接触 LingTai / 灵台的人。目标只有一个：**照着做，能把灵台装起来、跑起来、知道下一步该点哪里、出了问题该查哪里。**
>
> 如果你已经会用命令行，可以直接看第 2 节安装；如果你完全没接触过终端，请从第 1 节开始。

## 0. 先用一句话讲清楚灵台

灵台不是一个只能在网页里聊天的机器人。它更像一个**本地工作台**：

- 你在终端里打开 `lingtai-tui`；
- TUI 帮你创建一个项目；
- 项目里会有一个或多个 agent；
- agent 可以读写本地文件、跑命令、查资料、写技能、发消息；
- 需要时还能分出临时小助手（daemon）或长期分身（avatar）；
- 所有状态都落在项目目录的 `.lingtai/` 里，方便检查、迁移和恢复。

可以先记住这张图：

```text
你
│
├─ 终端里的 lingtai-tui       ← 你看到的界面、命令、设置、状态
│
└─ 项目目录 .lingtai/          ← agent 的信箱、日志、记忆、配置
   ├─ 主 agent                 ← 主要和你协作的人
   ├─ daemon                   ← 临时分神：做一次任务，做完就退
   ├─ avatar                   ← 长期分身：适合长期专项工作
   ├─ skills / knowledge       ← 可复用流程与长期知识
   └─ MCP / IM / email         ← 外部服务与通信入口
```

**小白先别急着理解所有名词。** 先完成安装、启动、第一次任务；后面的能力会在需要时再用。

---

## 1. 安装前先确认三件事

### 1.1 你现在用的是什么系统？

推荐顺序：

1. **macOS**：最推荐，一行安装脚本 `curl -fsSL https://lingtai.ai/install.sh | bash` 即可。
2. **Linux**：同样用这行安装脚本。
3. **Windows**：推荐先装 WSL2 + Ubuntu，再按 Linux 路线走（完整能力）。也有**实验性**的原生 PowerShell 安装，能力有删减，见 4.2。

如果你只是想最快体验，优先找一台 macOS。

### 1.2 你需要会什么？

只需要会三件事：

- 打开终端；
- 复制一段命令进去；
- 看报错信息，按本手册排查。

不用先学 Python，不用自己装 `pip install lingtai`。普通用户安装 LingTai，**不要把系统 Python 当入口**。

### 1.3 灵台由哪两层组成？

这点很重要，因为很多安装问题都来自“装错层”。

| 层 | 你看到的名字 | 负责什么 | 普通用户怎么装 |
|---|---|---|---|
| TUI | `lingtai-tui` | 终端界面、项目向导、命令、可视化入口、升级/doctor | 用一行安装脚本 `install.sh` 安装 |
| Kernel | `lingtai` Python 包 | agent 真正运行的内核、工具、上下文、MCP | TUI 自动管理，不要手动装到系统 Python |

一句话：**你安装的是 `lingtai-tui`；Python 内核由 TUI 管。**

---

## 2. macOS 安装：最推荐路线

### 2.1 打开终端

在 macOS 上：

1. 按 `Command + Space`；
2. 输入“终端”或 `Terminal`；
3. 回车打开。

后面的命令都复制到终端里执行。

### 2.2 一行命令安装

复制执行：

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
```

这行脚本会装好 `lingtai-tui` 和 `lingtai-portal`，并配好 TUI 管理的 Python 运行时——优先用预编译好的 release 二进制，你的平台没有预编译版时再自动回退到源码编译（脚本会自己临时下载 Go / Node）。不需要先装 Homebrew，也不需要自己装 Python。

安装完成后，检查命令是否存在：

```bash
which lingtai-tui
```

能看到一个路径（例如 `/usr/local/bin/lingtai-tui` 或 `~/.local/bin/lingtai-tui`）即可。然后启动：

```bash
lingtai-tui
```

如果提示 `command not found: lingtai-tui`，脚本结尾会告诉你把哪一个目录加到 `PATH`；照做后重开一个终端即可。

### 2.3 以后怎么升级？

重新运行同一行安装脚本即可：

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
```

也可以运行 `lingtai-tui self-update`（按检测到的安装方式升级）。升级后要**重启 TUI**；只升级不重启，看起来可能还是旧行为。

---

## 3. 中国大陆网络下的安装办法

大陆网络最常见的问题不是 LingTai 本身，而是安装脚本拉 GitHub、Go、npm 资源时卡住。按下面顺序试，不要一上来乱改很多变量。

### 3.1 第一选择：先直接重跑安装脚本

先试最简单命令；很多时候只是某次网络抖动，重跑一次就过了：

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
```

如果能装上，就不要再折腾镜像。脚本本身会自动探测大陆网络，并在需要时为 Go 模块 / npm / Go 校验库切换到可访问的镜像，通常不需要你手动设置。

### 3.2 如果提示 `curl` 没装

安装脚本需要 `curl`。Ubuntu / Debian / WSL 上先装它，再重跑脚本：

```bash
sudo apt update
sudo apt install -y curl
curl -fsSL https://lingtai.ai/install.sh | bash
```

### 3.3 如果回退到源码编译时报缺工具

脚本通常会自己临时下载 Go 和 Node；极简 Linux / WSL 镜像若缺基础工具，先补齐再重跑：

```bash
sudo apt update
sudo apt install -y build-essential curl git
curl -fsSL https://lingtai.ai/install.sh | bash
```

### 3.4 如果仍然失败：把报错存下来

重跑一次并把完整输出保存下来，方便求助或排查：

```bash
curl -fsSL https://lingtai.ai/install.sh | bash 2>&1 | tee ~/lingtai-install.log
```

装好后也可以运行 `lingtai-tui doctor` 检查运行时是否就绪。

---

## 4. Linux / Windows 怎么装？

### 4.1 Linux

和 macOS 一样，用同一行安装脚本：

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
lingtai-tui
```

如果系统提示缺少编译工具，Ubuntu/Debian 可先装：

```bash
sudo apt update
sudo apt install -y build-essential curl git
```

### 4.2 Windows

**推荐（完整能力）：WSL2。** Windows 原生终端环境差异很大，最稳的路线是：

1. 安装 WSL2；
2. 安装 Ubuntu；
3. 进入 Ubuntu 终端；
4. 按 Linux 路线运行 `curl -fsSL https://lingtai.ai/install.sh | bash`。

**实验性：原生 PowerShell 安装。** 如果你不想装 WSL，可以在 PowerShell（5.1+）里运行：

```powershell
iwr -useb https://lingtai.ai/install.ps1 | iex
```

它会下载预编译的 Windows 二进制、装进每用户目录并加入 `PATH`、用 `uv` 配好运行时。但请知道原生 Windows 现在是**降级能力**：

- daemon / 分身（子 agent）暂不可用；
- `bash` 工具走的是 `cmd.exe`，不是 bash；
- 没有原生 PowerShell 自更新——升级请重新运行上面那行 `iwr ... | iex`。

要完整能力，仍建议用 WSL2。

### 4.3 从源码安装（进阶）

安装脚本默认已经优先用预编译二进制、必要时自动回退源码编译。若你想手动从源码装：

```bash
git clone https://github.com/Lingtai-AI/lingtai.git
cd lingtai
./install.sh
lingtai-tui
```

源码安装更容易遇到 Go / Node / npm 环境问题。小白优先用一行安装脚本。

---

## 5. 第一次启动：按这个顺序走

启动：

```bash
lingtai-tui
```

第一次进来，不要急着开很多功能。按顺序做：

### 5.1 创建或选择项目

灵台围绕“项目目录”工作。建议：

- 一个研究/代码/写作主题建一个项目；
- 不要把所有事都塞进一个项目；
- 项目目录最好放在你能找到的位置，比如 `~/work/my-lingtai-project`。

### 5.2 跑 `/setup`

TUI 里按提示或输入：

```text
/setup
```

你通常会设置：

- 使用什么模型 / preset；
- agent 的名字；
- 是否加载某个 recipe；
- 是否配置外部渠道或密钥。

小白建议：先选默认或 Tutorial 类配置，跑通再改。

### 5.3 给第一个任务

不要一上来问“你能做什么”。直接给一个小任务，例如：

```text
请在当前项目里创建一个 notes.md，写三条今天要做的事。
```

或：

```text
帮我读一下 README.md，告诉我这个项目怎么启动。
```

这样你会立刻看到它如何读文件、写文件、汇报结果。

### 5.4 看状态

常用：

```text
/kanban
```

你可以看到 agent 是否在忙、是否卡住、token / 状态等。

---

## 6. 日常工作时，应该怎么和灵台说话？

### 6.1 最好这样说

```text
我要做一个申请书。材料在 docs/apply/ 里。请先读模板，再整理我的开源贡献，最后输出一版 Markdown 和 Word 版本。不要提交，先给我审核。
```

这句话里有五个关键信息：

1. 目标：申请书；
2. 材料位置：`docs/apply/`；
3. 步骤：先读模板，再整理，再输出；
4. 格式：Markdown 和 Word；
5. 红线：不要提交，先审核。

### 6.2 不建议这样说

```text
帮我弄一下。
```

agent 不知道你要弄什么、做到什么程度、能不能改文件、能不能发出去。

### 6.3 一句好用模板

```text
目标：……
材料：……
输出：……
不要做：……
如果不确定，先问我。
```

---

## 7. 什么时候用主 agent、daemon、avatar？

### 7.1 主 agent：负责判断和对话

适合：

- 和你确认目标；
- 做最终判断；
- 汇总多个结果；
- 写给人看的最终回复；
- 涉及发布、删除、医学/法律/学术强结论等高风险事项。

### 7.2 daemon：临时分神，适合脏活累活

适合：

- 扫很多文件；
- 查一批资料；
- 跑测试；
- 对一个方案做严厉审稿；
- 让它写报告给主 agent 审。

一句话：**daemon 做一次性任务，做完就退；主 agent 负责验收。**

### 7.3 avatar：长期分身，适合长期专项

适合：

- 一个长期营养学助手；
- 一个专门维护某仓库的工程 agent；
- 一个长期跟进某论文方向的研究 agent。

不要为了一个小问题就开 avatar；小任务优先 daemon。

---

## 8. 文件、技能、知识：三种常用“记住”方式

### 8.1 文件

文件是普通项目材料，适合给人看、提交、版本管理。

例：`README.md`、`申请书.docx`、`实验报告.html`。

### 8.2 Knowledge

Knowledge 是某个 agent 的长期私有记忆，适合放项目事实、路径、决策、历史。

例：“这个项目的正式仓库在哪”“某论文曾被谁否决”“某数据不能公开”。

### 8.3 Skills

Skills 是可复用流程，适合沉淀方法。

例：“学术写作防幻觉流程”“三日饮食记录分析流程”“发布前检查清单”。

一句话：

```text
一次性交付 → 文件
长期事实 → knowledge
以后还会复用的方法 → skill
```

---

## 9. 上下文满了怎么办？

agent 的对话上下文不是无限的。长任务做久后，它需要整理现场。

常见处理：

- 让 agent 把大段工具结果整理成摘要，避免对话里塞满原始日志；
- 让 agent 做一次“凝蜕”：保存关键状态，再换一个干净上下文继续；
- 让 agent 把当前任务状态写进内部任务索引；
- 重要的长期事实，放进 knowledge；
- 以后还会复用的方法，写成 skill。

这里的“摘要、凝蜕、任务索引”不一定都是你直接输入的命令；你只要用自然语言说“请先收束现场再继续”，agent 会按当前能力处理。

你可以这样说：

```text
请先收束现场：列出已完成、未完成、关键文件、下一步，然后再继续。
```

不要害怕凝蜕。好的凝蜕不是失忆，而是“把桌面收拾干净再继续”。

---

## 10. 外部渠道：Telegram / 微信 / 飞书 / 邮箱

灵台可以接外部消息，但不是一开始必须配置。

适合配置外部渠道的情况：

- 你想在手机上给 agent 发消息；
- 你希望任务完成后它主动通知你；
- 你不想一直开着 TUI 等回复。

在 TUI 里看：

```text
/mcp
```

注意：`/mcp` 主要用来查看连接状态。具体配置要按对应插件说明走，不要把 token 发到公开地方。

---

## 11. 常用 slash 命令小抄

| 命令 | 什么时候用 |
|---|---|
| `/setup` | 第一次配置模型、recipe、行为 |
| `/help` | 查看命令说明 |
| `/kanban` | 看 agent 是否忙、卡住、休眠 |
| `/goal` | 设置当前目标，防止任务跑偏 |
| `/viz` | 看 agent / avatar 网络图 |
| `/mcp` | 看外部渠道和 MCP 状态 |
| `/doctor` | 启动、升级、工具异常时排查 |
| `/clear` | 对话太乱，清理上下文 |
| `/sleep` | 当前项目暂时不用，让 agent 睡眠待命 |
| `/suspend all` | 你要离开很久，暂停所有 agent |
| `/projects` | 切换或查看项目 |
| `/export` | 导出成果或项目材料 |

不需要一开始全记住。先记：`/setup`、`/kanban`、`/doctor`、`/help`。

---

## 12. 常见问题排查

### 12.1 `lingtai-tui: command not found`

先查 Homebrew 位置：

```bash
brew --prefix
```

Apple Silicon 常见路径：`/opt/homebrew`。执行：

```bash
eval "$(/opt/homebrew/bin/brew shellenv)"
```

Intel Mac 常见路径：`/usr/local`。执行：

```bash
eval "$(/usr/local/bin/brew shellenv)"
```

再试：

```bash
which lingtai-tui
lingtai-tui
```

### 12.2 TUI 打开了，但 agent 不回复

先跑：

```bash
lingtai-tui doctor
```

在 TUI 里也可以用：

```text
/doctor
/kanban
```

如果仍不行，看日志：

```bash
find .lingtai -name agent.log -maxdepth 3 -print
```

### 12.3 升级后好像没变化

记住两层：

- 安装脚本升级的是 `lingtai-tui`；
- TUI 还会管理 Python runtime。

做（重跑安装脚本，或用自更新）：

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
lingtai-tui doctor
```

原生 Windows PowerShell 用户改为重跑 `iwr -useb https://lingtai.ai/install.ps1 | iex`。然后重启 TUI。

### 12.4 技能、命令或工具不见了

先用：

```text
/doctor
```

或命令行：

```bash
lingtai-tui doctor
```

### 12.5 不要这样修

不要一看到问题就执行：

```bash
pip install -U lingtai
```

这通常不会修好 TUI 项目里的 runtime，反而容易让你误以为已经升级。

---

## 13. 一个最小可行工作流

第一次真正用，可以照这个流程：

1. 安装并启动 `lingtai-tui`；
2. `/setup` 选一个基础配置；
3. 建一个测试项目；
4. 让 agent 创建 `notes.md`；
5. 让 agent 读 `notes.md` 并改一版；
6. 用 `/kanban` 看状态；
7. 用 `/doctor` 确认环境健康；
8. 再开始真正项目。

示例任务：

```text
请在当前项目创建 notes.md，写下“今日目标、已有材料、下一步”。写完后告诉我文件路径。
```

如果这一步能成功，你已经跑通了最核心的能力。

---

## 14. 新手最容易踩的坑

1. **把 LingTai 当普通网页聊天机器人。** 它其实是本地项目工作台。
2. **把所有任务塞进一个项目。** 一个项目一个主题更稳。
3. **不给材料路径。** agent 找不到材料就会猜。
4. **不说红线。** 是否能发布、删除、提交 PR，一定要说清楚。
5. **裸 `pip install lingtai`。** 普通用户不要这样装。
6. **长任务不让它收束。** 做久了要让它写“已完成/未完成/下一步”。
7. **外部渠道 token 乱发。** 密钥只放配置，不要贴到公开 issue 或 README。

---

## 15. 这份手册的边界

这份文档只解决“新手如何安装、启动、理解基本工作流”。

更深入的内容请看：

- README：完整安装、架构、排障；
- `/help`：TUI 命令；
- `/doctor`：本机环境检查；
- `ANATOMY.md`：贡献者读源码；
- 对应 MCP / skill 文档：外部渠道与可复用技能。
