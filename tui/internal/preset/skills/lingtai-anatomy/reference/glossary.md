# Glossary — Chinese–English Terminology Mapping

Covenant, procedures, and tool descriptions use 文言 (literary Chinese) terminology alongside English technical terms. This table maps every 文言 term to its English equivalent, layer, and tool name.

Column legend:
- **Kernel 层**: concept exists in `lingtai_kernel` (the bare agent runtime)
- **Wrapper 层**: concept is extended in `lingtai` (the batteries-included wrapper)
- **Wrapper 工具**: the actual tool name the LLM sees at the wrapper level

---

## Core Concepts

| 文言术语 | English | Kernel 层 | Wrapper 层 |
|---------|---------|----------|-----------|
| 灵台 (língtái) | LingTai — the platform | core | core |
| 器灵 (qìlíng) | Agent | `base_agent` | `Agent` |
| 灵网 (língwǎng) | Network / `.lingtai/` project | — | `network.py` |
| 化身 (huàshēn) | Avatar / delegate | — | **`avatar`** |
| 本我 (běnwǒ) | Self / parent agent | core | core |
| 他我 (tāwǒ) | Other-self / avatar | — | **`avatar`** |
| 分神 (fēnshén) | Daemon / emanation | — | **`daemon`** |
| 同伴 (tóngbàn) | Peer | — | via `email` |

## Memory Layers

| 文言术语 | English | Kernel 层 | Wrapper 层 | Wrapper 工具 |
|---------|---------|----------|-----------|-------------|
| 对话 (duìhuà) | context / conversation | live session | same | — |
| 简 (jiǎn) / 草案板 (cǎo'ànbǎn) | pad (sketchboard) | `eigen(pad, ...)` | `psyche(pad, ...)` | **`psyche`** |
| 心印 (xīnyìn) / 灵台 | lingtai (identity) | `eigen(lingtai, ...)` | `psyche(lingtai, ...)` | **`psyche`** |
| 典 (diǎn) | codex (permanent facts) | — | standalone | **`codex`** |
| 藏经阁 (cángjīnggé) | library (skills) | — | standalone | **`library`** |
| 公阁 / 众阁 | shared library | — | filesystem (`cp`) | `system(refresh)` |
| 凝蜕 (níngtuì) | molt (shed and carry forward) | `eigen(context, molt)` | `psyche(context, molt)` | **`psyche`** |
| 前尘往事 (qiánchén) | pre-molt history | `chat_history_archive.jsonl` | same | — |

## Agent Lifecycle

| 文言术语 | English | Kernel 层 | Wrapper 层 |
|---------|---------|----------|-----------|
| 活跃 (huóyuè) | ACTIVE | `AgentState.ACTIVE` | same |
| 空闲 (kòngxián) | IDLE | `AgentState.IDLE` | same |
| 卡住 (kǎzhù) | STUCK (AED recovery) | `AgentState.STUCK` | same |
| 眠 (mián) | ASLEEP | `AgentState.ASLEEP` | same |
| 假死 (jiǎsǐ) | SUSPENDED | `AgentState.SUSPENDED` | same |
| 体魄 / 体力 (tǐlì) | stamina | `config.stamina` | same |
| 心跳 (xīntiào) | heartbeat | `_heartbeat_loop` | same |
| 催寐 (cuīmèi) | lull (force sleep) | — | **`system`** (karma) |
| 心肺复苏 (xīnfèi fùsū) | CPR (resuscitate) | — | **`system`** (nirvana) |
| 更衣 (gēngyī) | refresh (reload config) | — | **`system`** |
| 涅槃 (nièpán) | nirvana (destroy agent) | — | **`system`** (nirvana) |

## Tools and Actions

| 文言术语 | English | Kernel 工具 | Wrapper 工具 |
|---------|---------|-----------|-------------|
| 游历 (yóulì) | web search | — | **`web_search`** |
| 览卷 (lǎnjuàn) | web read | — | **`web_read`** |
| 阅卷 (yuèjuàn) | read file | — | **`read`** |
| 创卷 (chuàngjuàn) | write file | — | **`write`** |
| 改 (gǎi) | edit file | — | **`edit`** |
| 寻卷 (xúnjuàn) | glob search | — | **`glob`** |
| 搜字 (sōuzì) | grep search | — | **`grep`** |
| 观象 (guānxiàng) | vision | — | **`vision`** |
| 聆听 (língtīng) | listen | — | **`listen`** |
| 执令 (zhílìng) | bash | — | **`bash`** |
| 飞鸽 (fēigē) / 传书 (chuánshū) | mail / email | **`mail`** (intrinsic) | **`email`** (capability) |

## Soul (灵识)

| 文言术语 | English | Kernel 层 | Wrapper 层 | Wrapper 工具 |
|---------|---------|----------|-----------|-------------|
| 灵 (líng) / 灵识 | soul (introspection) | `soul` intrinsic | same | **`soul`** |
| 心流 (xīnliú) | flow (auto introspection) | auto on IDLE + `soul_delay` | same | — (not a tool action) |
| 自省 (zìxǐng) | inquiry (on-demand) | `soul(inquiry)` | same | **`soul`** action=`inquiry` |
| 调频 (tiáopín) | delay (adjust frequency) | `soul(delay)` | same | **`soul`** action=`delay` |

## Knowledge Flow

| 文言术语 | English | Description |
|---------|---------|-------------|
| 去芜存菁 (qùwú cúnjīng) | consolidation | Preserving what matters across molt |
| 晋升 (jìnshēng) | promotion | Moving knowledge to a more durable layer |
| 钉卷 (dīngjuàn) | pin / append | Pin read-only files into pad via `psyche(pad, append)` |
| 导出 (dǎochū) | export | Freeze codex entries to immutable files |
| 扫荡 (sǎodàng) | semantic sweep | Cross-file terminology consistency check |

## Signal Files

| 文言术语 | English | File | Effect |
|---------|---------|------|--------|
| 打断 (dǎduàn) | interrupt | `.interrupt` | Cancel current LLM tool loop |
| 入寐信号 | sleep | `.sleep` | Enter ASLEEP |
| 假死信号 | suspend | `.suspend` | SUSPENDED, process exits |
| 注入 (zhùrù) | prompt inject | `.prompt` | Text → [system] message |
| 强清 (qiángqīng) | forced molt | `.clear` | Forced context wipe |
| 规则分发 | rules distribution | `.rules` | Update `system/rules.md` |

## Mail System

| 文言术语 | English | Description |
|---------|---------|-------------|
| 邮匣 (yóuxiá) | mailbox | Root directory for mail state |
| 收信匣 | inbox | `mailbox/inbox/` |
| 已发匣 | sent | `mailbox/sent/` |
| 典藏匣 | archive | `mailbox/archive/` |
| 通讯录 | contacts | `mailbox/contacts.json` |
| 身份牒 (shēnfèn dié) | identity card | Sender manifest snapshot in every message |
| 定期发信 | scheduled email | `mailbox/schedules/` |

## Network Topology

| 文言术语 | English | Description |
|---------|---------|-------------|
| 化身树 | avatar tree | Parent-child graph from delegate ledgers |
| 三层边 | three-layer edges | avatar edges / contact edges / mail edges |
| 账本 (zhàngběn) | ledger | `delegates/ledger.jsonl` (append-only) |
| 父代 (fùdài) | parent | Agent that spawned a given avatar |
| 后代 (hòudài) | descendants | All agents reachable through avatar tree |

## System Prompt Sections

| 文言术语 | English | File | Protection |
|---------|---------|------|-----------|
| 约 / 公约 (gōngyuē) | covenant | `system/covenant.md` | Protected (not agent-editable) |
| 原则 (yuánzé) | principle | `system/principle.md` | Protected |
| 规则 (guīzé) | rules | `system/rules.md` | Protected (updatable via `.rules`) |
| 程序 (chéngxù) | procedures | `system/procedures.md` | Protected |
| 简报 (jiǎnbào) | brief | `system/brief.md` | Externally maintained |
| 批注 (pīzhù) | comment | `system/comment.md` | App-level instructions |
