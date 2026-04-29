# File Formats

> Last updated: 2025-04-27
> Source: kernel source + base_agent analysis + init_schema analysis +化身 C 报告

关键文件的字段级 JSON schema 参考。每个格式包含完整字段表、类型、必填/可选、默认值及示例。

---

## 1. `.agent.json` — 实时 Manifest

agent 的身份档案，内核在启动时写入，状态变更时更新。

```json
{
  "agent_id": "20260423-221801-1710",
  "agent_name": "orchestrator",
  "nickname": "小灵",
  "address": "orchestrator",
  "created_at": "2026-04-23T22:18:01Z",
  "started_at": "2026-04-24T08:53:42Z",
  "admin": {"karma": true, "nirvana": false},
  "language": "wen",
  "stamina": 36000,
  "state": "idle",
  "soul_delay": 120,
  "molt_count": 2,
  "capabilities": [
    ["avatar", {}],
    ["bash", {"yolo": true}],
    ["codex", {}],
    ["library", {"paths": ["~/.lingtai-tui/bundled-skills"]}]
  ],
  "location": {
    "city": "Los Angeles",
    "region": "California",
    "country": "US",
    "timezone": "America/Los_Angeles"
  }
}
```

### 完整字段表

| 字段 | 类型 | 来源 | 说明 |
|------|------|------|------|
| `agent_id` | `str` | 内核自动生成 | 格式 `{YYYYMMDD}-{HHMMSS}-{rand}`。启动时生成，不变 |
| `agent_name` | `str \| null` | init.json `manifest.agent_name` | agent 名称，可 `null` |
| `nickname` | `str` | 可选 | 昵称（文言名） |
| `address` | `str` | 通常等于 `agent_name` | 飞鸽投递地址 |
| `created_at` | `str` (ISO 8601) | 内核自动 | agent 首次创建时间 |
| `started_at` | `str` (ISO 8601) | 内核自动 | 最近一次启动时间 |
| `admin` | `dict \| null` | init.json `manifest.admin` | **身份识别关键字段**：`null` = 人类，全 false = 普通 agent，任一 truthy = orchestrator |
| `language` | `str` | init.json `manifest.language` | 语言代码（如 `"wen"` = 文言、`"en"` = English） |
| `stamina` | `int \| float` | init.json `manifest.stamina` | 最大运行秒数（默认 3600 = 1 小时） |
| `state` | `str` | 内核状态机 | 五态：`"active"` / `"idle"` / `"stuck"` / `"asleep"` / `"suspended"`（JSON 中小写） |
| `soul_delay` | `int \| float` | init.json `manifest.soul.delay` | 心流触发延迟秒数（默认 120） |
| `molt_count` | `int` | 内核递增 | 蝉蜕次数，每次 molt +1，永不重置 |
| `capabilities` | `list[list]` | init.json `manifest.capabilities` | `[[name, config_dict], ...]` 格式。每个 capability 是包装层功能 |
| `location` | `dict` | 可选 | 地理位置 + 时区 |

### capabilities 格式说明

每个 capability 为 `[name, config_dict]` 二元组：

| capability | config_dict 示例 | 说明 |
|---|---|---|
| `avatar` | `{}` | 化身/他我系统 |
| `bash` | `{"yolo": true}` | Shell 执行；`yolo: true` 跳过确认 |
| `codex` | `{}` | 典/永久事实存储 |
| `library` | `{"paths": ["~/.lingtai-tui/bundled-skills"]}` | 藏经阁/技能库，`paths` 额外扫描路径 |
| `daemon` | `{}` | 分神/一次性任务 |
| `email` | `{}` | 飞鸽/邮件系统（wrapper 层工具名 `email`，内核层叫 `mail`） |

### state 值（小写）

| 值 | 含义 | 进入方式 | 离开方式 |
|---|---|---|---|
| `active` | 正在处理消息 | inbox 收到消息 | turn 完成 → `idle`，或异常 → `stuck` |
| `idle` | 等待消息 | turn 正常完成 | 新消息 → `active`，`.sleep` → `asleep` |
| `stuck` | 异常恢复中 | `_handle_message` 抛异常 | AED 恢复成功 → `active`，耗尽 → `asleep` |
| `asleep` | 休眠（心跳仍写） | `.sleep` / stamina 耗尽 / AED 耗尽 | inbox 收到消息 → `active` |
| `suspended` | 进程已终止 | `.suspend` / `.refresh` | 外部重新启动 |

---

## 2. `init.json` — 完整初始化配置

agent 的完整初始化声明。由 `init_schema.py` 的 `validate_init()` 验证。

### 2.1 顶层字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `manifest` | `dict` | **是** | 核心 agent manifest |
| `principle` | `str` | 条件* | 内联原则文本。*`principle` 与 `principle_file` 至少一个必填 |
| `principle_file` | `str` | 条件* | 原则文本文件路径 |
| `covenant` | `str` | 条件* | 内联誓约文本。*`covenant` 与 `covenant_file` 至少一个必填 |
| `covenant_file` | `str` | 条件* | 誓约文本文件路径 |
| `pad` | `str` | 条件* | 内联简/草稿板文本。*`pad` 与 `pad_file` 至少一个必填 |
| `pad_file` | `str` | 条件* | 简文本文件路径 |
| `prompt` | `str` | 条件* | 内联 prompt 文本。*`prompt` 与 `prompt_file` 至少一个必填 |
| `prompt_file` | `str` | 条件* | prompt 文件路径 |
| `soul` | `str` | 条件* | 内联心流文本。*`soul` 与 `soul_file` 至少一个必填 |
| `soul_file` | `str` | 条件* | 心流文本文件路径 |
| `procedures` | `str` | 否 | 内联流程文本（可选） |
| `procedures_file` | `str` | 否 | 流程文本文件路径（可选） |
| `brief` | `str` | 否 | 内联简报文本（可选） |
| `brief_file` | `str` | 否 | 简报文本文件路径（可选） |
| `comment` | `str` | 否 | 内联注释文本（可选） |
| `comment_file` | `str` | 否 | 注释文本文件路径（可选） |
| `env_file` | `str` | 条件** | 环境变量文件路径。**若 `manifest.llm.api_key_env` 已设且 `api_key` 未设，则必填 |
| `venv_path` | `str` | 否 | Python 虚拟环境路径 |
| `addons` | `list[str]` | 否 | 受 LingTai 策展的 MCP 名称列表（如 `["imap", "telegram"]`）。详见 [`mcp-protocol.md`](mcp-protocol.md) |
| `mcp` | `dict` | 否 | MCP 子进程激活映射（name → 子进程 spec）。详见 [`mcp-protocol.md`](mcp-protocol.md) |

**文本对规则**：5 个必填对（principle, covenant, pad, prompt, soul）——内联值和 `_file` 路径至少提供一个。3 个可选对（procedures, brief, comment）——两者均可缺省。

### 2.2 Manifest 字段（`manifest.*`）

| 字段 | 类型 | 必填 | 默认 | 说明 |
|------|------|------|------|------|
| `llm` | `dict` | **是** | — | LLM 供应商配置（见 §2.3） |
| `agent_name` | `str \| null` | 否 | — | agent 名称 |
| `language` | `str` | 否 | — | 语言代码 |
| `capabilities` | `dict` | 否 | — | 能力配置（见 §2.5） |
| `soul` | `dict` | 否 | — | 心流配置（见 §2.6） |
| `stamina` | `int \| float` | 否 | 3600 | 最大运行秒数（bool 被拒绝） |
| `context_limit` | `int \| null` | — | — | 最大上下文 token 数，`null` 为不设限 |
| `molt_pressure` | `int \| float` | 否 | 0.70 | 蝉蜕触发压力阈值（70%） |
| `molt_prompt` | `str` | 否 | — | 自定义蝉蜕提示文本（覆盖默认警告阶梯） |
| `max_turns` | `int` | 否 | 50 | 单次请求最大 turn 数 |
| `max_rpm` | `int` | 否 | — | 每分钟最大 API 请求数 |
| `admin` | `dict` | 否 | — | 管理者配置（karma, nirvana — 见 §2.4） |
| `streaming` | `bool` | 否 | — | 是否启用流式响应 |
| `time_awareness` | `bool` | 否 | — | agent 是否感知当前时间 |
| `timezone_awareness` | `bool` | 否 | — | agent 是否感知时区 |
| `pseudo_agent_subscriptions` | `list` | 否 | — | 伪 agent 订阅列表 |

### 2.3 LLM 子字段（`manifest.llm.*`）

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `provider` | `str` | **是** | LLM 供应商名（如 `"openai"`, `"anthropic"`） |
| `model` | `str` | **是** | 模型标识（如 `"gpt-4"`, `"claude-3-opus"`） |
| `api_key` | `str \| null` | 否 | 直接 API key |
| `api_key_env` | `str` | 否 | 持有 API key 的环境变量名（需配合 `env_file`） |
| `base_url` | `str \| null` | 否 | 自定义 API 端点 URL |

**API Key 解析链**：`api_key`（直接）→ 若无则读 `api_key_env` 指向的环境变量（从 `env_file` 加载）。验证器强制：`api_key_env` 有值但 `api_key` 无值时，顶层 `env_file` 必须存在。

### 2.4 Admin 子字段（`manifest.admin.*`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `karma` | `bool` (通常) | 管理者因果权限。truthy = orchestrator |
| `nirvana` | `bool` (通常) | 管理者涅槃/关闭权限 |

> **注意**：`init_schema.py` 不验证 admin 子字段的具体类型和结构——它被当作不透明 `dict`。但运行时通过 `.agent.json` 中 admin 是否有 truthy 值来判断 orchestrator 身份。

### 2.5 Capabilities（`manifest.capabilities.*`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `library` | `dict` | 藏经阁配置 |
| `library.paths` | `list[str]` | 额外技能库路径列表 |

其他 capability 的配置在 `.agent.json` 的 `capabilities` 数组中以 `[name, config_dict]` 格式声明，不在 init.json 的 `capabilities` dict 中。

### 2.6 Soul 子字段（`manifest.soul.*`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `delay` | `int \| float` | 心流触发延迟秒数（bool 被拒绝） |

### 2.7 Addons & MCP（`addons` + `mcp`）

> **v0.7.3 重写**：`addons` 字段从 dict-shape（`{name: kwargs}`，wires up in-process modules）改为 list-shape（`["imap", ...]`，names looked up in the kernel MCP catalog）。In-process addon tree was removed entirely; all four formerly-bundled addons (imap / telegram / feishu / wechat) now ship as separate sibling repos and run as MCP subprocesses. 详细规范见 [`mcp-protocol.md`](mcp-protocol.md)；本节仅记录 init.json 的字段形状。

#### `addons: list[str]` — 解压键(decompression keys)

```json
{ "addons": ["imap", "feishu"] }
```

每个名称在 agent 启动时被 `mcp` capability 在 kernel-shipped catalog（`lingtai/mcp_catalog.json`）中查找，对应记录被追加到该 agent 的 `mcp_registry.jsonl`。Append-only、idempotent；已在 registry 中的名称跳过。

合法的 catalog 名称（v0.7.3）：`imap`、`telegram`、`feishu`、`wechat`。

#### `mcp: dict` — 激活映射(activation map)

```jsonc
{
  "mcp": {
    "imap": {
      "type": "stdio",                          // 或 "http"
      "command": "/path/to/python",
      "args": ["-m", "lingtai_imap"],
      "env": { "LINGTAI_IMAP_CONFIG": ".secrets/imap.json" }
    },
    "remote-api": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "headers": { "Authorization": "Bearer ..." }
    }
  }
}
```

stdio 字段：`type`、`command`、`args`、`env`（皆必填或推荐）。
http 字段：`type`、`url`、`headers`（headers 可选）。

激活时，loader 检查 `init.json.mcp.<name>` 的 name 是否出现在 `mcp_registry.jsonl` 中——**未注册的 name 会被跳过并记录警告**。kernel 自动为每个 spawn 的 MCP 注入两个环境变量（用户 `env:` 中的同名键覆盖）：

| 环境变量 | 值 |
|---|---|
| `LINGTAI_AGENT_DIR` | agent 工作目录绝对路径 |
| `LINGTAI_MCP_NAME` | 该 MCP 的 registry 名称 |

这两个变量是 LICC 的基础——MCP 子进程通过它们定位 agent 收件箱。详见 [`mcp-protocol.md`](mcp-protocol.md) §3、§4。

### init.json 完整已知字段列表

```
顶层 (TOP_KNOWN):
  manifest, env_file, venv_path, addons, mcp,
  principle, principle_file, covenant, covenant_file,
  procedures, procedures_file, brief, brief_file,
  pad, pad_file, prompt, prompt_file,
  soul, soul_file, comment, comment_file

Manifest (MANIFEST_KNOWN):
  llm, agent_name, language, capabilities, soul, stamina,
  context_limit, molt_pressure, molt_prompt, max_turns,
  max_rpm, admin, streaming, time_awareness,
  timezone_awareness, pseudo_agent_subscriptions, preset

LLM:
  provider (必填), model (必填),
  api_key, api_key_env, base_url
```

---

## 3. `.status.json` — 运行时快照

agent 运行状态的实时快照，由 `_save_chat_history()` 在每个 turn 结束后写入。

```json
{
  "tokens": {
    "estimated": false,
    "context": {
      "system_tokens": 1500,
      "tools_tokens": 800,
      "history_tokens": 3200,
      "total_tokens": 5500,
      "window_size": 128000,
      "usage_pct": 4.3
    }
  },
  "runtime": {
    "uptime_seconds": 3600.5,
    "stamina_left": 82800.0
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `tokens.estimated` | `bool` | token 计数是否为估算 |
| `tokens.context.system_tokens` | `int` | 系统 prompt token 数 |
| `tokens.context.tools_tokens` | `int` | 工具定义 token 数 |
| `tokens.context.history_tokens` | `int` | 对话历史 token 数 |
| `tokens.context.total_tokens` | `int` | 总已用 token 数 |
| `tokens.context.window_size` | `int` | 上下文窗口大小 |
| `tokens.context.usage_pct` | `float` | 使用百分比 = total / window * 100 |
| `runtime.uptime_seconds` | `float` | 自启动以来的运行秒数 |
| `runtime.stamina_left` | `float` | 剩余 stamina 秒数 |

**`usage_pct` 与蝉蜕的关系**：当 `usage_pct >= 70`（`molt_pressure`）时开始警告阶梯；当 `usage_pct >= 95`（`molt_hard_ceiling`）时无条件强制蝉蜕。

---

## 4. `mailbox/schedules.json` — 定时发送计划

定时飞鸽的调度列表。

```json
[
  {
    "id": "sched-001",
    "message_id": "550e8400-...",
    "to": ["avatar-1"],
    "subject": "Daily summary",
    "message": "Here is your daily update...",
    "deliver_at": "2026-04-24T09:00:00Z",
    "repeat": "daily",
    "created_at": "2026-04-23T22:00:00Z",
    "status": "pending"
  }
]
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | `str` | 调度 ID |
| `message_id` | `str` | 关联的消息 UUID |
| `to` | `list[str]` | 收件人地址列表 |
| `subject` | `str` | 主题 |
| `message` | `str` | 正文 |
| `deliver_at` | `str` (ISO 8601) | 计划投递时间 |
| `repeat` | `str \| null` | 重复模式（`"daily"`, `"weekly"`, `null` = 一次性） |
| `created_at` | `str` (ISO 8601) | 创建时间 |
| `status` | `str` | `"pending"` / `"sent"` / `"cancelled"` |

---

## 5. `system/pad_append.json` — 简的只读文件引用

通过 `psyche(pad, append, files=[...])` 钉入简的只读文件引用列表。

```json
[
  {
    "path": "/abs/path/to/file.md",
    "marker": "[file-1]",
    "appended_at": "2026-04-24T08:00:00Z"
  },
  {
    "path": "/abs/path/to/config.json",
    "marker": "[file-2]",
    "appended_at": "2026-04-24T08:01:00Z"
  }
]
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `path` | `str` | 文件绝对路径 |
| `marker` | `str` | 在 pad.md 中的内联标记（如 `[file-1]`） |
| `appended_at` | `str` (ISO 8601) | 追加时间 |

系统 prompt 拼装时，pad 段会将 `[file-N]` 标记替换为对应文件的实际内容。

---

## 6. `mcp/servers.json` — Legacy MCP 直接挂载（仍受支持，但不被注册表门控）

> **优先使用 `init.json.mcp` 字段**（见 §2.7）。`mcp/servers.json` 是 v0.7.3 之前的直接挂载路径，仍被 loader 读取以保持向后兼容，但不经过 `mcp_registry.jsonl` 的门控验证。新代码请使用 init.json 路径。

实际格式是 **dict-shape**（注意：v1 anatomy 错误地标记为 list-shape）：

```json
{
  "filesystem": {
    "type": "stdio",
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
    "env": {}
  },
  "remote-api": {
    "type": "http",
    "url": "http://localhost:3000/mcp",
    "headers": { "Authorization": "Bearer ..." }
  }
}
```

字段表与 §2.7 中 `init.json.mcp.<name>` 的子进程 spec 完全相同（`type` 默认为 `"stdio"`，stdio 需要 `command` + `args`，http 需要 `url`）。

`LINGTAI_AGENT_DIR` 和 `LINGTAI_MCP_NAME` 也会被自动注入到这些 servers 的子进程中（与 init.json 路径相同的注入逻辑）。

---

## 6.5 `mcp_registry.jsonl` — Per-agent MCP 注册表

JSONL 格式，每行一条已注册的 MCP 记录。Append-only。位置：`<agent-name>/mcp_registry.jsonl`（与 `init.json` 同级）。

来源：
- 由 `mcp` capability 在启动时根据 `init.json.addons` 列表从 kernel-shipped catalog 解压（idempotent，已存在的 name 跳过）。
- 由 agent 通过 `write` / `edit` / `bash` 工具直接追加（用于第三方 MCP）。

示例：

```json
{"name": "imap", "summary": "Real email via IMAP/SMTP — multi-account.", "transport": "stdio", "command": "/Users/x/.lingtai-tui/runtime/venv/bin/python", "args": ["-m", "lingtai_imap"], "source": "lingtai-curated", "homepage": "https://github.com/Lingtai-AI/lingtai-imap"}
```

字段（每行一条 JSON object）：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | `str` | 是 | 唯一标识，匹配 `^[a-z][a-z0-9_-]{0,30}$` |
| `summary` | `str` | 是 | 一行说明，最多 200 字符 |
| `transport` | `"stdio"` \| `"http"` | 是 | |
| `source` | `str` | 是 | 来源：`"lingtai-curated"` / `"user"` / URL |
| `command` | `str` | stdio 必填 | 启动命令 |
| `args` | `list[str]` | stdio 必填 | 命令参数（可空） |
| `url` | `str` | http 必填 | MCP HTTP 端点 |
| `env` | `dict` | 否 | 子进程环境变量 |
| `headers` | `dict` | 否 | http-only |
| `homepage` | `str` | 否 | 规范化 setup 文档 URL |
| `env_required` | `list[str]` | 否 | 文档化提示 |

**门控规则**：`init.json.mcp.<name>` 中的 name 必须出现在此 registry 中，否则 loader 会跳过该激活并记录警告。这是"register-before-activate"契约的实施点。

**catalog-version 稳定性**：kernel 升级带来的 catalog 变更**不会**自动覆盖已注册的 record——每个 agent 与其 MCP 的契约在注册时被冻结。要更新某 record，需先删除该行再重新解压。

详细规范（schema、validator、解压算法）见 [`mcp-protocol.md`](mcp-protocol.md) §1–§3。

---

## 6.6 `.mcp_inbox/<mcp_name>/<event_id>.json` — LICC v1 事件

Filesystem-based inbound channel。MCP 子进程通过此路径将事件推送到宿主 agent 的收件箱。位置：`<agent-name>/.mcp_inbox/<mcp_name>/<event_id>.json`。

写入方式：MCP 必须**原子写入**（写到 `.json.tmp`，`fsync()`，再 `rename()` 为 `.json`）。Kernel 以 0.5s 轮询周期扫描；忽略所有 `.tmp` 后缀文件。

事件 schema（v1）：

```json
{
  "licc_version": 1,
  "from": "alice@example.com",
  "subject": "Re: project status",
  "body": "Hey, just checking in on...",
  "metadata": {
    "email_id": "alice@example.com:INBOX:1042",
    "account": "agent@gmail.com"
  },
  "wake": true,
  "received_at": "2026-04-29T15:42:00Z"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `licc_version` | `int` | 否（默认 1） | 不为 1 则 dead-letter |
| `from` | `str` | 是 | 非空发件人标识 |
| `subject` | `str` | 是 | 非空，最多 200 字符 |
| `body` | `str` | 是 | 可空字符串。约定：高频/大体积事件传 ~300 字符预览，路由键放 `metadata` |
| `metadata` | `dict` | 否 | MCP 自定义任意字段（推荐放 message_id 等回查键） |
| `wake` | `bool` | 否（默认 `true`） | `false` 时仍投递到收件箱但不调用 `_wake_nap()` |
| `received_at` | ISO-8601 `str` | 否 | kernel 缺省填充 |

**Dead-letter**：parse 失败、缺必填字段、未知 version 等，事件移入同目录下 `.dead/<event_id>.json`，配套写一个 `.dead/<event_id>.error.json`。Dead-letter **不会自动清理**——humans/agents 检查后自行删除。

**Backpressure**：每轮 poll 每个 MCP 最多处理 100 个事件；多余的事件留待下一轮。

**版本承诺**：LICC v1 stable & supported indefinitely。新字段会以 additive 方式在 v2 引入；v1 events 在 v2 kernel 上继续工作。

详细规范（dispatch、validator、版本协商策略、客户端实现）见 [`mcp-protocol.md`](mcp-protocol.md) §4–§5。

---

## 7. `delegates/ledger.jsonl` — 化身委派日志

每行一条化身（avatar）生成事件。JSONL 格式（每行一个 JSON 对象）。

```json
{"parent": "orchestrator", "child": "/abs/path/to/.lingtai/avatar-1", "child_name": "avatar-1"}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `parent` | `str` | 父 agent 名称 |
| `child` | `str` | 子 agent 工作目录绝对路径 |
| `child_name` | `str` | 子 agent 名称 |

> **注意**：avatar 生成是包装层（`lingtai.core.avatar`）功能，不是内核 intrinsic。仅使用内核的 agent 没有 `delegates/` 目录。

---

## 8. 信号文件格式

信号文件是瞬态文件，由心跳循环（`_heartbeat_loop()`，每秒一次）检测和消费。

### 8.1 所有信号类型

| 信号文件 | 内容格式 | 消费方式 | 用途 |
|---------|---------|---------|------|
| `.sleep` | 空 | 立即删除 | agent 进入 ASLEEP，心跳继续 |
| `.suspend` | 空 | 立即删除 | agent 优雅退出 |
| `.interrupt` | 空 | 立即删除 | 取消当前 turn/工具调用 |
| `.prompt` | 纯文本 | 读取后删除 | 内容注入为 `[system]` 消息 |
| `.clear` | 纯文本（可选 source tag） | 读取后删除 | 强制蝉蜕，默认 source=`"admin"` |
| `.rules` | 纯文本 | diff 后写入 `system/rules.md`，然后删除 | 更新网络规则 |
| `.inquiry` | `"<source>\n<question>"` | 重命名为 `.inquiry.taken`（互斥） | 触发心流内省 |
| `.inquiry.taken` | — | 内省完成后删除 | 互斥标记 |
| `.refresh` | 空 | 重命名为 `.refresh.taken` | 配置重载（握手式） |
| `.refresh.taken` | — | 新进程启动后删除 | 刷新中标记 |

### 8.2 消费模式分类

| 模式 | 信号 | 特点 |
|------|------|------|
| **删除** | `.interrupt`, `.suspend`, `.sleep`, `.prompt`, `.clear`, `.rules` | 立即 unlinked，无确认 |
| **重命名握手** | `.refresh` → `.refresh.taken`, `.inquiry` → `.inquiry.taken` | 通过 rename 实现原子确认，发送方可轮询 `.taken` 是否存在 |
| **延迟删除** | `.inquiry.taken` | 由内省线程完成后删除 |

### 8.3 信号检测优先级

每个心跳周期内按固定顺序检测：

1. `.interrupt` — 最高优先级，立即取消
2. `.refresh` — 触发关闭 + 重启
3. `.suspend` — 触发关闭
4. `.sleep` — 进入 ASLEEP
5. `.prompt` — 注入消息
6. `.clear` — 强制蝉蜕
7. `.inquiry` — 心流内省
8. `.rules` — 规则更新
9. Stamina 检查 — 非自愿执行

### 8.4 远程信号发送建议

- `.prompt`：fire-and-forget，无法从外部判断是否已消费
- `.inquiry`：有原子 ack（`.inquiry.taken` 重命名）
- `.refresh`：完整握手协议，可轮询 `.agent.lock` 判断重启完成

---

## 9. `message.json` — 飞鸽消息格式

存在于 `mailbox/inbox/<uuid>/`、`outbox/<uuid>/`、`sent/<uuid>/` 中。

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "_mailbox_id": "550e8400-e29b-41d4-a716-446655440000",
  "from": "orchestrator",
  "to": ["human"],
  "cc": [],
  "subject": "Task complete",
  "message": "I finished the analysis...",
  "type": "normal",
  "received_at": "2026-04-13T16:35:00Z",
  "attachments": [],
  "identity": {
    "agent_id": "20260423-221801-1710",
    "agent_name": "orchestrator",
    "nickname": "小灵",
    "address": "orchestrator",
    "created_at": "2026-04-23T22:18:01Z",
    "started_at": "2026-04-24T08:53:42Z",
    "admin": {"karma": true, "nirvana": false},
    "language": "wen",
    "stamina": 36000,
    "state": "active",
    "soul_delay": 120,
    "molt_count": 2
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | `str` (UUID) | 消息唯一标识 |
| `_mailbox_id` | `str` (UUID) | 始终等于 `id`（用于文件夹命名） |
| `from` | `str` | 发件人地址 |
| `to` | `list[str]` | 收件人列表（始终是数组，即使只有一个） |
| `cc` | `list[str]` | 抄送列表 |
| `subject` | `str` | 主题 |
| `message` | `str` | 正文 |
| `type` | `str` | 消息类型（`"normal"` 等） |
| `received_at` | `str` (ISO 8601) | 接收时间 |
| `attachments` | `list` | 附件列表 |
| `identity` | `dict` | 发件人完整 manifest 快照，内联携带身份信息 |

---

## 10. `logs/token_ledger.jsonl` — Token 审计日志

每行一次 API 调用记录：

```json
{"ts": "2026-04-23T22:18:10Z", "input": 1500, "output": 200, "thinking": 50, "cached": 800}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ts` | `str` (ISO 8601) | 时间戳 |
| `input` | `int` | 输入 token 数 |
| `output` | `int` | 输出 token 数 |
| `thinking` | `int` | 思考 token 数 |
| `cached` | `int` | 缓存命中 token 数 |

---

## 11. `.agent.heartbeat` — 心跳时间戳

纯文本文件，包含一个浮点 Unix 时间戳：

```
1744567890.123456
```

- 每秒由心跳守护线程更新
- **存活判断**：`time.time() - timestamp < 2.0` 秒（`handshake.is_alive` 默认阈值）
- 文件不存在、不可读或时间戳过时 = 进程已死亡
- Human（`admin: null`）始终返回 alive——不写心跳文件

---

## 12. `codex/codex.json` — 典/永久事实存储

```json
[
  {
    "id": "fact-001",
    "title": "API Rate Limit",
    "summary": "The external API has a rate limit of 100 req/min",
    "content": "Detailed findings about the rate limit behavior...",
    "supplementary": "Extra detail that doesn't consume another slot"
  }
]
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | `str` | 条目 ID |
| `title` | `str` | 标题 |
| `summary` | `str` | 摘要（注入系统 prompt 目录） |
| `content` | `str` | 完整内容（按需拉取） |
| `supplementary` | `str \| null` | 补充详情（不占用额外 slot） |

默认上限 20 条。系统 prompt 仅展示目录（title + summary），完整内容通过 `codex(view, ids=[...])` 按需获取。
