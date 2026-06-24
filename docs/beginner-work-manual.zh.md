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

1. **macOS**：最推荐，直接用 Homebrew 安装。
2. **Linux**：可用 Homebrew for Linux，或从源码安装。
3. **Windows**：建议先装 WSL2 + Ubuntu，再按 Linux 路线走。

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
| TUI | `lingtai-tui` | 终端界面、项目向导、命令、可视化入口、升级/doctor | 用 Homebrew 或源码安装 |
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

### 2.2 先检查有没有 Homebrew

输入：

```bash
brew --version
```

可能出现两种情况：

- 看到 `Homebrew 4.x.x`：说明已经装好了，跳到 2.4。
- 看到 `command not found: brew`：说明还没装，继续 2.3。

### 2.3 安装 Homebrew

复制执行：

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

安装过程中可能会要求输入电脑密码。输入时终端不会显示星号，这是正常的。

装完后，Homebrew 通常会提示你执行两行 `eval "$(... brew shellenv)"`。如果你没看懂，可以先按下面常见路径试：

Apple Silicon（M1/M2/M3）Mac：

```bash
eval "$(/opt/homebrew/bin/brew shellenv)"
```

Intel Mac：

```bash
eval "$(/usr/local/bin/brew shellenv)"
```

再检查：

```bash
brew --version
```

能看到版本号，就可以继续。

### 2.4 安装 LingTai TUI

```bash
brew install lingtai-ai/lingtai/lingtai-tui
```

安装完成后，检查命令是否存在：

```bash
which lingtai-tui
```

能看到类似下面的路径即可：

```text
/opt/homebrew/bin/lingtai-tui
```

然后启动：

```bash
lingtai-tui
```

### 2.5 以后怎么升级？

```bash
brew update
brew upgrade lingtai-ai/lingtai/lingtai-tui
```

升级后要**重启 TUI**。如果你只是升级了却没重启，看起来可能还是旧行为。

---

## 3. 中国大陆网络下的安装办法

大陆网络最常见的问题不是 LingTai 本身，而是 Homebrew 拉 GitHub、Go、npm 资源时卡住。按下面顺序试，不要一上来乱改很多变量。

### 3.1 第一选择：先直接装

先试最简单命令：

```bash
brew install lingtai-ai/lingtai/lingtai-tui
```

如果能装，就不要再改镜像。

### 3.2 如果 Homebrew 更新很慢：先用清华 TUNA 加速 Homebrew 本身

如果卡在 `brew update`、`homebrew-core`、`ghcr.io`，可执行：

```bash
cat >> ~/.zprofile <<'EOF'
export HOMEBREW_API_DOMAIN="https://mirrors.tuna.tsinghua.edu.cn/homebrew-bottles/api"
export HOMEBREW_BOTTLE_DOMAIN="https://mirrors.tuna.tsinghua.edu.cn/homebrew-bottles"
export HOMEBREW_BREW_GIT_REMOTE="https://mirrors.tuna.tsinghua.edu.cn/git/homebrew/brew.git"
EOF
source ~/.zprofile
brew update
```

然后再装：

```bash
brew install lingtai-ai/lingtai/lingtai-tui
```

### 3.3 如果卡在 GitHub tap：改用 Gitee 镜像 tap

如果报 TLS、GnuTLS、GitHub 拉取失败，可用：

```bash
brew untap lingtai-ai/lingtai 2>/dev/null || true
brew tap lingtai-ai/lingtai https://gitee.com/huangzesen1997/homebrew-lingtai.git
brew install lingtai-ai/lingtai/lingtai-tui
```

注意：Gitee 是镜像，可能比 GitHub 慢几个小时。如果版本落后，过一会儿再试，或改回 GitHub tap。

### 3.4 如果 Go / npm 编译阶段出问题

默认公式会自动判断是否切换镜像。只有在自动判断不灵时，才手动指定：

```bash
HOMEBREW_GOPROXY="https://goproxy.cn,direct" \
HOMEBREW_NPM_CONFIG_REGISTRY="https://registry.npmmirror.com" \
  brew install lingtai-ai/lingtai/lingtai-tui
```

如果 npm 镜像证书报错，反而应清掉 npm 镜像：

```bash
unset HOMEBREW_NPM_CONFIG_REGISTRY
brew install lingtai-ai/lingtai/lingtai-tui
```

### 3.5 如果仍然失败

先运行：

```bash
brew doctor
brew update
```

再把报错保存下来。常用信息：

```bash
brew --version
brew config
brew info lingtai-ai/lingtai/lingtai-tui
```

---

## 4. Linux / Windows 怎么装？

### 4.1 Linux

Linux 也可以用 Homebrew for Linux：

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
brew install lingtai-ai/lingtai/lingtai-tui
lingtai-tui
```

如果系统提示缺少编译工具，Ubuntu/Debian 可先装：

```bash
sudo apt update
sudo apt install -y build-essential curl git
```

### 4.2 Windows

Windows 原生终端环境差异很大。推荐路线：

1. 安装 WSL2；
2. 安装 Ubuntu；
3. 进入 Ubuntu 终端；
4. 按 Linux 路线安装。

不建议小白一开始在 Windows 原生 PowerShell 里折腾源码编译。

### 4.3 从源码安装（进阶）

Homebrew 不可用时可从源码装：

```bash
git clone https://github.com/Lingtai-AI/lingtai.git
cd lingtai
./install.sh
lingtai-tui
```

源码安装更容易遇到 Go / Node / npm 环境问题。小白优先用 Homebrew。

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

- Homebrew 升级的是 `lingtai-tui`；
- TUI 还会管理 Python runtime。

做：

```bash
brew update
brew upgrade lingtai-ai/lingtai/lingtai-tui
lingtai-tui doctor
```

然后重启 TUI。

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
