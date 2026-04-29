# Filesystem Layout

> Last updated: 2025-04-27
> Source: kernel source + base_agent analysis + init_schema analysis

---

## v1 Errata

旧版 anatomy（SKILL.md v1.2.0）目录树中，`system/rules.md` 在 L193 和 L199 各出现一次（"Protected: hard rules" 重复条目）。实际只有一个 `system/rules.md`，本版已修正。

此外，旧版 L61 描述 lingtai.md 注入到 `lingtai/identity` 段，实际注入到 `covenant` 段。本版已修正。

---

## 三层包装架构

灵台 agent 运行于三层 Python 包之上：

| 层 | 包名 | 定位 |
|---|---|---|
| **内核** | `lingtai_kernel` | 最小可运行 agent 运行时。提供 agent 循环、会话管理、工作目录锁、心跳/状态机、信箱文件系统协议、四个内建 intrinsic：`eigen`（灵台/简/名）、`mail`、`soul`、`system` |
| **包装层** | `lingtai` | 含电池包装。在内核之上添加高层能力：`psyche`（更丰富的灵台/简/典编辑）、`codex`、`library`、`daemon`、`avatar`、`bash`、文件操作等 |
| **用户定制** | `init.json` + `system/` 文件 | 每个 agent 的个性化配置：模型、prompt 段落（principle/covenant/pad/procedures/rules）、capabilities、addons |

**工具名映射**：内核层用 `eigen(pad, edit, ...)` / `eigen(context, forget, ...)`；包装层用 `psyche(pad, edit, ...)` / `psyche(object=context, action=molt, ...)`。本文档以包装层术语为主，内核等价物另注。

---

## 项目根目录树

```
.lingtai/
├── meta.json                    # 版本追踪（用于迁移）
├── .port                        # Portal 端口号（Portal 运行时写入）
├── .library/                     # 技能库
│   ├── intrinsic/               # 内建技能（符号链接，TUI 管理）
│   ├── <recipe-name>/           # 配方技能（TUI 管理）
│   └── custom/                  # agent 自建技能
├── .tui-asset/                  # TUI 专属元数据（非 agent 状态）
├── .portal/                     # Portal 专属数据
│   ├── topology.jsonl           # 网络快照（JSONL，每 3 秒）
│   ├── replay/
│   │   ├── chunks/              # 增量编码的小时级分片 (.json.gz)
│   │   └── manifest.json        # 分片元数据
│   └── reconstruct.progress     # 重建进度（瞬态）
├── human/                       # 人类参与者（见下文 agent 目录布局）
└── <agent-name>/                # 每个 agent 有独立目录
```

**`.library/` 三源扫描**：library intrinsic 扫描三个来源——`.library/intrinsic/`（内核/TUI 内建，每次 init 重写）、`.library/custom/`（agent 自建，主要编辑位置）、以及 `init.json` 中 `manifest.capabilities.library.paths` 声明的额外路径（支持 `~`、绝对路径、相对于工作目录的路径）。

> **v0.7.3 起，`.lingtai/.addons/` 共享 addon 配置目录已废弃**。Addon 现在以 MCP 子进程形式运行，每个 addon 的 secrets/config 由对应 agent 自管（典型路径 `<agent>/.secrets/<addon>.json`），`mcp_inbox/` 等 LICC 路径详见 [`mcp-protocol.md`](mcp-protocol.md)。在 v0.7.3 之前生成的项目中，`.lingtai/.addons/` 可能仍存在；TUI 迁移 m028 不会删除这些目录（保留作为 audit），但运行时不再读取。

---

## 器灵工作目录（完整版）

每个 agent（包括 `human/`）遵循相同结构。Human 目录 `.agent.json` 的 `admin` 为 `null`，且缺少 heartbeat/status 文件。

```
<agent-name>/
├── .agent.json                  # 实时 manifest（身份档案）
├── init.json                    # 完整初始化配置
├── .agent.heartbeat             # Unix 时间戳（浮点数），每秒更新
├── .agent.lock                  # agent 进程运行时持有
├── .status.json                 # 实时运行指标（token 用量、uptime、stamina）
│
├── system/                      # 系统 prompt 段落
│   ├── system.md                # 拼装后的完整系统 prompt
│   ├── principle.md             # 受保护：核心原则
│   ├── covenant.md              # 受保护：行为誓约
│   ├── rules.md                 # 受保护：硬性规则（网络规则）
│   ├── procedures.md            # 受保护：标准流程
│   ├── lingtai.md               # 可编辑：灵台/身份（agent 自写）
│   ├── pad.md                   # 可编辑：简/草稿板（agent 自写）
│   ├── pad_append.json          # 简的只读文件引用列表
│   ├── brief.md                 # 外部维护（secretary）
│   ├── llm.json                 # LLM 配置快照
│   └── comment.md               # 应用层指令
│
├── history/
│   ├── chat_history.jsonl       # 当前蝉蜕周期的对话日志（JSONL）
│   └── chat_history_archive.jsonl  # 所有过去蝉蜕周期的对话，追加式
│
├── logs/
│   ├── agent.log                # 子进程 stdout/stderr
│   ├── events.jsonl             # 生命周期/审计事件（JSONL）
│   ├── token_ledger.jsonl       # 每次 API 调用的 token 计数
│   ├── soul_flow.jsonl          # 周期性心流内省记录
│   └── soul_inquiry.jsonl       # 按需自省记录
│
├── mailbox/
│   ├── inbox/                   # 收到的飞鸽
│   │   └── <uuid>/
│   │       └── message.json
│   ├── outbox/                  # 待发送的飞鸽（预调度）
│   │   └── <uuid>/
│   │       └── message.json
│   ├── sent/                    # 已成功发送
│   │   └── <uuid>/
│   │       └── message.json
│   ├── archive/                 # 已归档
│   ├── contacts.json            # 通讯录：[{address, name, note}]
│   ├── read.json                # 已读追踪：{<uuid>: true|false}
│   └── schedules.json           # 定时发送计划
│
├── codex/
│   └── codex.json               # 永久事实条目（有上限，默认 20 条）
│
├── delegates/
│   └── ledger.jsonl             # 化身委派日志
│
├── mcp_registry.jsonl           # MCP 注册表（每行一条已注册 MCP；门控激活）
├── .mcp_inbox/                  # LICC v1：MCP 子进程推送的事件
│   └── <mcp-name>/
│       ├── <event-id>.json      # 待 kernel 投递（轮询 0.5s）
│       └── .dead/               # 验证失败的事件（不自动清理，供调试）
│
├── mcp/                         # Legacy 直接挂载路径（v0.7.3 前的方式）
│   └── servers.json             # 仍受支持，但优先用 init.json.mcp（见 init.json §2.7）
│
├── .secrets/                    # Agent-owned 凭据（典型 .secrets/<addon>.json）
│
│
└── 信号文件（瞬态）：
    ├── .sleep                   # 立即消费。agent 进入 ASLEEP
    ├── .suspend                 # 立即消费。agent 优雅退出
    ├── .interrupt               # 立即消费。取消当前 turn/工具调用
    ├── .prompt                  # 文本内容 → 注入为 [system] 消息后删除
    ├── .clear                   # 强制蝉蜕（可选 source tag）
    ├── .rules                   # 新网络规则 → 写入 system/rules.md 后删除
    ├── .inquiry                 # "<source>\n<question>" → 心流内省
    ├── .inquiry.taken           # 内省运行时的互斥标记
    ├── .refresh                 # 配置重载请求（与 .refresh.taken 握手）
    └── .refresh.taken           # agent 正在刷新中——等待进程锁释放
```

### System Prompt 段落说明

| 段落 | 文件 | 受保护 | 来源 |
|------|------|--------|------|
| principle | `system/principle.md` | 是 | init.json `principle` 或 `principle_file` |
| covenant | `system/covenant.md` + `system/lingtai.md` | 是 | init.json `covenant` / `covenant_file`；lingtai.md 由 agent 通过 `psyche(lingtai, update)` 写入 |
| procedures | `system/procedures.md` | 是 | init.json `procedures` 或 `procedures_file`（可选） |
| rules | `system/rules.md` | 是 | init.json（可选）或 `.rules` 信号文件更新 |
| brief | `system/brief.md` | 是 | 外部维护（secretary），init.json 可选 |
| pad | `system/pad.md` | 否 | agent 通过 `psyche(pad, edit)` 写入 |
| comment | `system/comment.md` | 否 | 应用层指令 |

**受保护**：LLM 无法在运行时通过 prompt injection 覆写这些段落。宿主 API（`write_section()`）可绕过保护检查。

**covenant 段特殊拼装**：identity 是 `system/covenant.md`（受保护，宿主设定）与 `system/lingtai.md`（agent 可编辑）的合并，两者拼接后注入系统 prompt 的受保护 `covenant` 段。

---

## 身份识别规则

通过 `.agent.json` 的 `admin` 字段判断 agent 类型：

| `admin` 值 | 含义 | 角色 |
|---|---|---|
| `null` 或不存在 | 人类参与者 | `human/` 目录 |
| `{"karma": false, "nirvana": false}` | 普通 agent | 化身/他我 |
| `{"karma": true, ...}` (任一值为 truthy) | 管理者 | orchestrator |

**判断算法**：扫描 `.lingtai/*/.agent.json`，找到 `admin` 为 JSON 对象且至少有一个 truthy boolean 值的 agent 即为 orchestrator。

典型项目只有一个 orchestrator，是人类交互的主要 agent。

---

## 启动链路摘要

```
cli.py → build_agent() → _setup_from_init() → start()
```

### 各阶段职责

1. **`cli.py`** — 命令行入口。解析参数，确定工作目录（agent 目录路径），调用 `build_agent()`。

2. **`build_agent()`** — 构建 agent 实例。
   - 读取 `init.json`
   - 验证 init 配置（`validate_init()`）
   - 解析 `_file` 路径为绝对路径
   - 加载 `env_file` 中的环境变量（用于 `api_key_env`）
   - 创建 `BaseAgent`（内核）或 `Agent`（包装层）实例

3. **`_setup_from_init()`** — 初始化 agent 内部状态。
   - 从 init.json 解析 manifest → 写入 `.agent.json`
   - 解析文本段落 → 写入 `system/` 下各 `.md` 文件
   - 初始化 prompt manager（注册所有段落）
   - 配置 LLM 会话（provider、model、API key）
   - 扫描 library 路径 → 注册技能
   - 启动 heartbeat 线程
   - 获取 `.agent.lock`（文件锁，确保单实例）

4. **`start()`** — 启动主循环。
   - 启动主线程（`_run_loop()`）：消息循环，从 inbox 取消息 → `_handle_message()` → LLM 调用 → 工具执行 → 持久化
   - 启动心跳守护线程（`_heartbeat_loop()`）：每秒写入 `.agent.heartbeat`，检查信号文件，检查 stamina
   - 拼装并写入 `system/system.md`（`_flush_system_prompt()`）

### API Key 解析链

```
init.json:
  manifest.llm.api_key       → 直接使用（如果存在）
  manifest.llm.api_key_env   → 从 env_file 加载对应环境变量
```

验证器强制要求：如果设置了 `api_key_env` 但没有 `api_key`，则顶层 `env_file` 必须存在。

### 文件锁机制

`.agent.lock` 在 agent 进程启动时获取，进程退出时释放。同一工作目录只能运行一个 agent 实例。刷新（refresh）时：当前进程释放锁 → 观察者子进程等待锁释放 → 重新启动 agent。

---

## 共享库与技能层级

```
.library/
├── intrinsic/                   # 内建技能（TUI 管理，每次 init 重写）
├── <recipe-name>/               # 配方技能（TUI 管理）
└── custom/                      # agent 自建技能
    └── <skill-name>/
        └── SKILL.md             # YAML frontmatter + Markdown 正文

../.library_shared/              # 共享库（网络内所有 agent 可用）
└── <skill-name>/
    └── SKILL.md
```

**晋升路径**：`cp -r .library/custom/<name> ../.library_shared/<name>` + `system({"action": "refresh"})`。晋升是 agent 的显式决策，无自动化晋升机制。
