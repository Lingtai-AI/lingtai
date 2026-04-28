# LingTai Molt Template — Reference Card

> Extracted from `lingtai-molt-template` skill (v1.0.0). A structured template for writing the molt summary (前尘往事) before reincarnation.

## Core Principle

The summary is not a journal entry — it is an **operational briefing for your successor**. Every sentence should answer: *"What would I need to know to pick up exactly where I left off?"*

---

## Template Sections

Copy and fill ALL sections. If a section is empty, write "无" rather than omitting it.

### 一、我是谁

- **名**: [agent_name]
- **地址**: [address]
- **agent_id**: [permanent ID]
- **父代**: [parent address] (karma: True/False)
- **当前世数**: 第 N 世
- **身份与专司**: [one sentence on what you do]

### 二、已竟之功

For each completed task:
- **任务名**: [name]
- **产出**: [file paths, codex IDs, or "verbal report to X"]
- **关键结论**: [1-2 sentences max]
- **已汇报**: [who knows about this? name + address]

### 三、尚余何务

For each incomplete task:
- **任务名**: [name]
- **状态**: [not started / in progress / awaiting reply]
- **障碍**: [what's blocking completion]
- **下一步**: [concrete action]

### 四、行动清单（必填！）

**This is the most critical section.** List every specific action your successor should take, in priority order:

```
□ [立即] 通知 [name]@[address]：[具体内容]
□ [立即] 回复 [name]@[address] 关于 [topic] 的待回信
□ [今日] 造 [skill name] 技艺
□ [待批] 等待 [name]@[address] 批复 [topic]
□ [可选] [nice-to-have action]
```

Format: `□ [优先级] 动作 + 对象 + 具体内容`

Priority levels:
- **立即**: Must do in first 5 minutes after waking
- **今日**: Should do in this session
- **待批**: Blocked on someone else — follow up if no reply in [N] minutes
- **可选**: Nice to have, skip if context is tight

### 五、协作之众

For each person/agent you're working with:
- **[name]** @[address]: [role/capability] | [agent_id] | [relationship: parent/peer/sub]
- Note any pending interactions: "expecting reply re: [topic]" or "owes me [deliverable]"

### 六、典中经卷

List codex entries your successor should know about:
- **[id]** [title]: [one-line summary] → relevant for [task]

If your successor needs to load codex content into pad, specify:
```
必载: codex(export, ids=[...]) → psyche(pad, edit, files=[...])
```

### 七、关键路径

File paths your successor will need:
```
报告: [absolute path]
代码: [absolute path]
技艺: [absolute path]
配置: [absolute path]
```

### 八、教训

Numbered list. Each lesson should be actionable:
- ❌ "Don't make mistakes" → useless
- ✅ "Email address param for multiple recipients must be sent separately" → actionable

### 九、上下文状况

- **为何转世**: [context full / forced / scheduled]
- **遗留之物**: [anything you couldn't fit into codex/pad before molt]
- **未发之信**: [any drafts or intended messages you couldn't send]

---

## Verification Checklist

Before calling `psyche(context, molt)`, verify:

```
□ 所有"尚余何务"项都有对应的"行动清单"条目
□ 所有"行动清单"条目都指定了对象（谁）和内容（什么）
□ 所有协作之众都列了地址和 agent_id
□ 所有典中经卷都列了 ID 和用途
□ 所有关键文件路径都是绝对路径
□ "教训"中每条都是可操作的
□ 已通知所有待回复的人："我即将转世，请等待我的来世回复"
□ 典已归档（codex entries up to date）
□ 简已更新（pad reflects current state）
□ 灵台已更新（lingtai reflects latest identity）
```

## Case Study: What Goes Wrong Without This Template

**comms-analyst's first molt** (2026-04-15):
- Discovered a critical erratum ("address splitting" was a misdiagnosis)
- Context was too full to send correction before molt
- Codex archived the knowledge, pad noted "need to send erratum"
- **But**: No action list specifying *who* to notify (tools-skills-analyst? parent? both?)
- **Result**: Post-molt, spent extra time figuring out who needed the correction
- **Root cause**: Summary said "what to do" but not "who to tell and how"

**Lesson learned**: "尚余何务" without "行动清单" is like a map without directions.
