---
name: lingtai-recipe
description: 创建和理解启动配方的指南——配方决定了调度器如何问候用户、遵循什么行为约束、以及附带哪些技能。当用户询问配方、想创建自定义配方、或需要理解配方工作原理时使用。
version: 1.0.0
---

# lingtai-recipe：创建启动配方

**启动配方**是一个命名目录，用于塑造调度器的首次接触行为、持续约束和可用技能。每个灵台项目都使用一个配方——在 `/setup` 中选择，或通过 `/agora` 从已发布网络继承。

## 配方目录结构

```
my-recipe/
  recipe.json             # 必须 — 名称和描述
  en/
    recipe.json           # 可选 — 语言特定版本
    greet.md
    comment.md
    covenant.md           # 可选 — 覆盖系统级公约
    procedures.md         # 可选 — 覆盖系统级程序
  zh/
    greet.md
    comment.md
    covenant.md           # 可选
    procedures.md         # 可选
  skills/                 # 可选：配方附带技能
    my-skill/
      SKILL.md            # 元数据 + 指向语言版本的指引
      SKILL-en.md         # 完整说明（英文）
      SKILL-zh.md         # 完整说明（中文）
      scripts/            # 可选辅助脚本
      assets/             # 可选资源
    my-other-skill/
      SKILL.md            # 单语言技能（无需变体）
```

## 五个组件

### 1. `greet.md` — 首次接触

调度器在新用户打开 TUI 时发送的第一条消息。以调度器的视角（第一人称）撰写。

**用途：** 设定基调，介绍网络，告诉用户能做什么，提供引导。

**占位符**（在设置时替换）：

| 占位符 | 值 |
|---|---|
| `{{time}}` | 当前日期和时间 |
| `{{addr}}` | 用户在网络中的邮箱地址 |
| `{{lang}}` | 语言代码（en、zh、wen） |
| `{{location}}` | 用户地理位置（城市、地区、国家） |
| `{{soul_delay}}` | 灵魂循环间隔（秒） |

**规则：**
- 保持简短（最多 5-10 句）
- 主动介绍自己，不要等用户提问
- 始终提醒用户使用 `/cpr all` 唤醒全部团队
- 使用 `{{time}}` 和 `{{location}}` 让问候更生动

### 2. `comment.md` — 持续行为约束

在每个回合注入调度器系统提示。持久的行为手册。

**用途：** 定义涵盖的主题、委派方式、约束、语气。

**规则：**
- 无占位符——这是静态文本
- 保持精简——每个回合都会注入，每个 token 都算数
- 如果配方附带技能，通过名称引用它们

### 3. `covenant.md` — 公约覆盖（可选）

覆盖系统级公约（`~/.lingtai-tui/covenant/<lang>/covenant.md`）。当配方包含此文件时，使用配方的公约代替全局公约。

**用途：** 某些配方需要根本不同的公约。例如，一个不应生成分身或参与网络的工具型智能体需要比默认更简单的公约。

**规则：**
- 无占位符——静态文本
- 如果不存在，使用系统级公约（行为不变）
- 遵循与 greet.md 和 comment.md 相同的国际化回退规则

### 4. `procedures.md` — 程序覆盖（可选）

覆盖系统级程序（`~/.lingtai-tui/procedures/<lang>/procedures.md`）。当配方包含此文件时，使用配方的程序代替全局程序。

**用途：** 某些配方需要不同的操作程序。例如，工具型智能体可能需要简化或完全不同的程序。

**规则：**
- 无占位符——静态文本
- 如果不存在，使用系统级程序（行为不变）
- 遵循与 greet.md 和 comment.md 相同的国际化回退规则

### 5. `skills/` — 配方附带技能

可选。随配方一起分发的技能，TUI 启动时自动链接到 `.lingtai/.library/<配方名>/` 分组目录。

每个技能遵循标准 SKILL.md 格式：

```markdown
---
name: 技能名称
description: 一行描述
version: 1.0.0
---
```

**国际化：** 多语言技能使用 `SKILL.md` 作为元数据指引，提供 `SKILL-en.md`、`SKILL-zh.md` 等语言版本。智能体读取 `SKILL.md` 后，选择对应语言的文件。单语言技能直接将所有内容放在 `SKILL.md` 中。

**分组：** 配方技能出现在 `.lingtai/.library/<配方名>/` 下——配方名是 `/library` 视图中的分组标题。

## recipe.json — 配方清单

每个配方的根目录必须包含 `recipe.json`（语言特定版本可选）：

```json
{
  "name": "配方名称",
  "description": "一行描述"
}
```

- `name` — **必须**，显示在 TUI 配方选择器中
- `description` — **必须**，作为提示文本显示
- 额外字段会被忽略但不会报错（向前兼容）

没有有效 `recipe.json` 的配方不会被识别为可导入。TUI 仅自动检测包含有效清单的 `.lingtai-recipe/` 目录。

## 国际化回退规则

所有配方文件（greet.md、comment.md、covenant.md、procedures.md、技能目录）使用相同的解析规则：

1. 尝试 `<lang>/` — 语言特定版本
2. 回退到根目录

**语言子目录优先于根目录。** TUI 的 i18n 查找先尝试 `<lang>/<文件>`，再回退到根目录版本。两种布局都合法。校验脚本（见下文「校验配方」）接受文件存在于根目录、任意语言子目录、或两者兼有——按配方意图决定。根目录放一份即可服务所有语言；语言子目录的副本会覆盖根目录版本。

## `.lingtai-recipe/` 统一约定

`/export recipe` 和 `/export network` 产出的 `.lingtai-recipe/` 结构**完全相同**。一个导出的网络，本质就是一个导出的配方外加 `.lingtai/` 状态目录：

- **导出的配方** = 仓库根目录有 `recipe.json` 和 `.lingtai-recipe/`（没有 `.lingtai/`）
- **导出的网络** = 以上内容，再加上 `.lingtai/`（完整网络状态）和项目文件

两个导出流程都会在 `git init` 之前运行 `validate_recipe.py`（见下一节），因此载荷结构是通过程序强制保证的。如果格式需要演进，验证脚本是唯一的真相之源——先更新它，再同步更新本说明。

接收者克隆仓库后用 `lingtai-tui` 打开即可。TUI 通过 `ProjectLocalRecipeDir()` 自动发现 `.lingtai-recipe/` 并在初始化时使用。无需手动配置路径。

## 校验配方

`tui/internal/preset/skills/lingtai-recipe/scripts/validate_recipe.py` 是两个导出流程在 `git init` 前都会调用的合规性检查脚本。它验证：

- 仓库根目录有 `recipe.json`，且包含 `name` 和 `description` 字段
- `.lingtai-recipe/` 目录存在
- 至少有一种语言下（或根目录下）存在 `greet.md` 和 `comment.md`
- `comment.md`、`covenant.md`、`procedures.md` 中不含占位符（占位符只允许出现在 `greet.md`）
- `skills/<name>/` 下的每个技能都有带完整元数据（`name`、`description`、`version`）的 `SKILL.md`

用法（从运行中的网络内，路径通过已绑定的技能符号链接解析）：

```bash
python3 .lingtai/.library/intrinsic/lingtai-recipe/scripts/validate_recipe.py <仓库根路径>
```

退出码 0 表示载荷结构合法。警告（未知语言代码、`.lingtai-recipe/` 根目录下有额外文件）会被报告但不会阻塞。

## 如何创建自定义配方

1. 按上述结构创建目录
2. 至少编写一个 `greet.md`（comment.md 和 skills/ 可选）
3. 在 TUI 中运行 `/setup`，选择「自定义」配方，输入目录路径
4. 调度器会重启并使用你的配方

## 如何发布配方

使用 `/export recipe` 导出纯配方，或 `/export network` 导出完整网络快照。两者都会在输出仓库中创建 `.lingtai-recipe/`。接收者克隆后直接用 `lingtai-tui` 打开——无需手动指定配方路径。
