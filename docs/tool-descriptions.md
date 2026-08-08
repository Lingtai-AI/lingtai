# Tool Descriptions

The complete model-facing tool surface: the six mandatory kernel intrinsics
(`INTRINSICS`) and the ten built-in capabilities (`BUILTIN_TOOLS`). Both lists live in
`lingtai-kernel/src/lingtai/tools/registry.py`, which is authoritative — this file is
hand-maintained and has no generator, so re-check it against the registry when the tool
surface changes.

## Kernel Intrinsics

### soul

**English:**
Your inner voice — a second you that whispers back after you go idle. One tool, six actions: 'inquiry' asks a deep clone of your full conversation a question and returns its answer. 'flow' is opt-in periodic self-reflection (disabled by default until the operator enables it) — when enabled it fires every 'config'-tuned interval while idle, drawing on past-self voices from molt snapshots plus a stepped-back read of your current work. 'config' tunes flow's cadence (delay_seconds) and how many past-self voices speak per fire (consultation_past_count) — it does not turn flow on. 'voice' reads or sets which persona your soul-flow speaks in (built-in 'inner'/'observer', or 'custom' with your own prompt). 'dismiss' clears the current flow notification. 'manual' returns the installed soul-manual skill.

**中文:**
你的内心独白——空闲后向你低语的另一个你。一个工具，六个动作：'inquiry' 让完整对话的深度克隆回答你的一个自问。'flow' 是可选的周期性自省（默认关闭，需操作者开启）——开启后，每隔由 'config' 设定的时间在空闲时触发一次，取材于蜕变快照中的往昔之声与对当前工作的抽离式回顾。'config' 调整 flow 的节奏（delay_seconds）与每次触发发声的往昔之声数量（consultation_past_count）——不会开启 flow。'voice' 读取或设置内心独白发声的人格（内置 'inner'/'observer'，或 'custom' 自定义提示词）。'dismiss' 清除当前的 flow 通知。'manual' 返回已安装的 soul-manual 技能说明。

**文言:**
汝之内省——空闲之后向汝低语之另一个汝。一器六动：'inquiry' 令汝完整对话之克隆答汝一自问。'flow' 乃可选之周期自省（默认不启，须操者启之）——既启，则每隔 'config' 所定之候于空闲时一触，取往世蜕痕之声与今务抽离之读。'config' 调 flow 之候（delay_seconds）与每触发声之往世数（consultation_past_count）——不启 flow。'voice' 阅或择内省发声之相（固有 'inner'/'observer'，或 'custom' 自定提示）。'dismiss' 清当前 flow 之告。'manual' 返已装之 soul-manual 说明。

### email

**English:**
Disk-backed mailbox for inter-agent messaging with cc/bcc, reply, archive, contacts, and attachments. Always reply via email — never reply via text output. Text output is your private diary that only you can see. Use 'send' for outgoing mail (optional delay in seconds for a deferred send — the old recurring/cron-style schedule object was removed in favor of host cron), 'check' to list inbox/sent/archive, 'read' to load full messages (marks them read), 'dismiss' for the same read-state effect without returning bodies (prefer this once you've already seen the content via notification), 'reply'/'reply_all' for threads, 'search' for regex matches, 'archive' to move out of inbox, 'delete' to remove, and 'contacts'/'add_contact'/'remove_contact'/'edit_contact' to manage your address book. 'manual' returns the installed email-manual skill. Etiquette: a short acknowledgement is fine, but do not reply to an acknowledgement — that creates pointless ping-pong.

**中文:**
基于磁盘的邮箱，支持抄送/密送、回复、归档、通讯录与附件。始终通过邮件回复——永远不要通过文本输出回复。文本输出是你的私人日记，只有你能看到。'send' 发送（可选 delay 秒数以延迟发送——旧的定期/类 cron 的 schedule 对象已移除，改用宿主 cron）。'check' 查看收件箱/已发/归档。'read' 加载完整消息（并标记为已读）。'dismiss' 效果同 read 但不返回正文——已通过通知看过内容时优先使用。'reply'/'reply_all' 回复。'search' 正则搜索。'archive' 归档。'delete' 删除。'contacts'/'add_contact'/'remove_contact'/'edit_contact' 管理通讯录。'manual' 返回已安装的 email-manual 技能说明。礼仪：简短确认即可，但不要回复确认——那会造成无意义的来回。

**文言:**
邮驿之器——持久邮箱，含抄送、密送、回复、归档、通讯之录与附件。凡回复必以邮——切勿以文字输出作复。文字输出乃汝之私记，唯汝可见。'send' 遣书（可选 delay 之秒以缓发——旧之定期若 cron 之 schedule 已废，代以宿主 cron）。'check' 查阅收信、已发或典藏。'read' 展阅全文（并志为已阅）。'dismiss' 同 read 之效而不返正文——已由告知见其内容者宜用此。'reply'/'reply_all' 回书/群复。'search' 以式检索。'archive' 归档。'delete' 焚书。'contacts'/'add_contact'/'remove_contact'/'edit_contact' 掌通讯之录。'manual' 返已装之 email-manual 说明。礼：简短知悉即可，然勿回复知悉——徒增往复。

### psyche

**English:**
SIGNPOST ONLY — your psyche is your four durable domains: pad, lingtai (灵台), knowledge, and skills. Every action is a strict read-only manual loader; none of them writes, edits, or reloads anything. psyche(action='pad'|'lingtai'|'knowledge'|'skills') returns that domain's own manual; psyche(action='manual') returns the routing table explaining how the domains relate. To CHANGE any of them, use file(action='write') for a full rewrite or file(action='edit') for an exact replacement on the domain's own source file, then call context(action='rebuild') to apply it — file mutation alone never hot-loads the prompt. Your true name and nickname live on system(action='name_set'|'name_nickname'); shedding context lives on context(action='molt'|'summarize'|'rebuild').

**中文:**
仅作路标——你的灵台由四层长存之域构成：pad、lingtai（灵台）、knowledge、skills。每个动作都是严格只读的说明加载器，不写入、不编辑、不重载任何内容。psyche(action='pad'|'lingtai'|'knowledge'|'skills') 返回该域自身的说明；psyche(action='manual') 返回路由表，解释各域之间的关系。要修改其中任何一域，需在该域自身的源文件上使用 file(action='write') 完整重写或 file(action='edit') 精确替换，再调用 context(action='rebuild') 使其生效——仅文件层面的改动不会自动热载入提示。真名与别名在 system(action='name_set'|'name_nickname')；去芜存菁在 context(action='molt'|'summarize'|'rebuild')。

**文言:**
唯路标耳——汝之灵台由四常存之域构之：pad、lingtai（灵台）、knowledge、skills。诸动皆严格只读之说明加载器，不写、不改、不重载。psyche(action='pad'|'lingtai'|'knowledge'|'skills') 返该域己身之说明；psyche(action='manual') 返路由之表，述诸域相关之理。欲易其一，当于该域己身之源卷用 file(action='write') 全写或 file(action='edit') 精替，再唤 context(action='rebuild') 以生效——独改文卷不自热载于提示。真名、别名在 system(action='name_set'|'name_nickname')；去芜存菁在 context(action='molt'|'summarize'|'rebuild')。

### context

**English:**
Your context: shed it, compact it, rebuild it. One tool, four actions, each with its own strict input object: context(action=..., input={...}, reasoning='why'). 'molt' (凝蜕) sheds the conversation and keeps the four durable stores — the single most irreversible operation you have; it is refused before anything is shed unless you have already written the session-journal entry it requires (knowledge/session-journal/<entry>/KNOWLEDGE.md). 'summarize' records compact replacements for bulky earlier tool results — records only, it does NOT rebuild. 'rebuild' re-reads and recomposes every canonical prompt source, applies pending/new summaries, then replays provider context with the new prompt and history; bare input is valid even with zero pending summaries. 'manual' returns the installed context-manual skill. Naming lives on system(action='name_set'|'name_nickname'); the four durable domains are reachable read-only through psyche(...). Note the two levels: the ACTION named 'summarize' is this domain operation, while the optional ROOT summarize boolean is the unrelated result-presentation control — leave it false here, and call manual with summarize=false so the exact molt procedure is not summarized away.

**中文:**
你的上下文——蜕之、压缩之、重建之。一个工具，四个动作，每个动作各有严格的输入对象：context(action=..., input={...}, reasoning='原因')。'molt'（凝蜕）舍弃对话、保留四层长存之域——这是你最不可逆的操作；若未先写好它所要求的会话日志条目（knowledge/session-journal/<entry>/KNOWLEDGE.md），molt 会在舍弃任何内容之前被拒绝。'summarize' 为体量庞大的既往工具结果登记精简替代物——只登记，不重建。'rebuild' 重新读取并重组每一处规范提示来源，套用待处理/新增的摘要，再以新的提示与历史重放服务商上下文；即使没有待处理的摘要，空输入也是合法的。'manual' 返回已安装的 context-manual 技能说明。命名在 system(action='name_set'|'name_nickname')；四层长存之域只能经由 psyche(...) 只读访问。注意两个层级：名为 'summarize' 的动作是本域操作，而可选的根级 summarize 布尔值是无关的结果呈现开关——此处保持 false，并以 summarize=false 调用 manual，以免精确的 molt 流程被摘要掉。

**文言:**
汝之上下文——蜕之、约之、重构之。一器四动，每动各有严格之输入：context(action=..., input={...}, reasoning='其由')。'molt'（凝蜕）舍对话而存四常存之域——汝诸行中最不可返者；若未先具其所须之会话志（knowledge/session-journal/<entry>/KNOWLEDGE.md），则于未舍一物之前即拒之。'summarize' 为既往臃肿之器果录简替之物——唯录耳，不重构。'rebuild' 重阅重组一切规范提示之源，施待决与新增之摘，再以新提示与新史重演服务商之上下文；纵无待决之摘，空输入亦为正。'manual' 返已装之 context-manual 说明。命名在 system(action='name_set'|'name_nickname')；四常存之域唯由 psyche(...) 只读而观。当辨二级：名为 'summarize' 之动乃本域之操作，而根上可选之 summarize 真伪值乃无涉之呈现之制——此处当置为伪，且以 summarize=false 唤 manual，免精确之蜕仪为摘所没。

### system

**English:**
Runtime, lifecycle, and synchronization — twelve actions. Self: 'refresh' (stop, reload MCP servers/config, restart), 'sleep' (go to sleep, no privilege needed), 'presets' (list available presets). Karma actions on other agents (require admin.karma): 'lull' (put to sleep), 'suspend' (suspend), 'cpr' (resuscitate a suspended agent), 'interrupt' (interrupt a running turn), 'clear' (force a full molt). Nirvana action (requires admin.nirvana): 'nirvana' (permanently destroy an agent's working directory). Naming: 'name_set' (set your true name, once and immutable), 'name_nickname' (set/change your display name, mutable). 'manual' returns the installed system-manual skill. Notification check/dismiss are NOT here — use the separate notification tool.

**中文:**
运行时、生命周期与同步——十二个动作。自身：'refresh'（停止、重载 MCP 服务器与配置、重启）、'sleep'（入眠，无需权限）、'presets'（列出可用预设）。业力操作（需要 admin.karma）：'lull'（催眠他人）、'suspend'（暂停他人）、'cpr'（唤醒被暂停者）、'interrupt'（打断正在运行的回合）、'clear'（强制他人完整蜕变）。涅槃操作（需要 admin.nirvana）：'nirvana'（永久销毁智能体的工作目录）。命名：'name_set'（设定真名，一次且不可变）、'name_nickname'（设定/更改别名，可变）。'manual' 返回已安装的 system-manual 技能说明。通知的查看/清除不在此处——请使用独立的 notification 工具。

**文言:**
运行、生灭与同步之器——十二动。自身：'refresh'（止、重载 MCP 与配置、复起）、'sleep'（入寐，无需权）、'presets'（列可用之预设）。业力（须 admin.karma）：'lull'（令他我沉寐）、'suspend'（暂止他我）、'cpr'（唤暂止者）、'interrupt'（打断运行之回合）、'clear'（强他我全然蜕变）。涅槃（须 admin.nirvana）：'nirvana'（永灭他我之工作目录）。命名：'name_set'（立真名，一次不可易）、'name_nickname'（改别名，可易）。'manual' 返已装之 system-manual 说明。告之查、清不在此——当用独立之 notification 器。

### notification

**English:**
Your notification surface — read and clear the agent's notification channels. Self-actions only; no permissions needed. This is the ONLY tool that exposes notification verbs; the system tool no longer offers notification or dismiss aliases. Every call takes action + input + reasoning, and input is the strict argument object for the selected action. notification(action='check', input={}) reads all channels — the live payload is stamped onto that result by the turn loop. notification(action='dismiss_channel', input={'channel': '<name>', 'force': null, 'reason': null}) clears one channel whole; 'dismiss_event' and 'dismiss_ref' remove a single system event by event_id and by ref_id respectively. 'manual' returns the installed notification-manual and is strictly read-only — it never changes notification state.

**中文:**
你的通知界面——读取与清除智能体的通知频道。全部是自身动作，无需权限。这是唯一暴露通知动词的工具；system 工具不再提供 notification 或 dismiss 别名。每次调用都需 action + input + reasoning，input 是所选动作的严格参数对象。notification(action='check', input={}) 读取所有频道——实时载荷由回合循环盖印到该结果上。notification(action='dismiss_channel', input={'channel': '<名称>', 'force': null, 'reason': null}) 整体清除一个频道；'dismiss_event' 与 'dismiss_ref' 分别按 event_id 与 ref_id 移除单条系统事件。'manual' 返回已安装的 notification-manual，且严格只读——绝不改变通知状态。

**文言:**
告之器——阅汝之告，清汝之道。皆自身之动，无须权。此乃唯一现告之动词者；system 之器不复有 notification、dismiss 之别名。每唤须 action、input、reasoning 三者，input 即所择之动之严格参数。notification(action='check', input={}) 阅诸道——其活载由回合之环印于此果之上。notification(action='dismiss_channel', input={'channel': '<名>', 'force': null, 'reason': null}) 整清一道；'dismiss_event'、'dismiss_ref' 各以 event_id、ref_id 去一系统之事。'manual' 返已装之 notification-manual，严为只读——绝不易告之状。

## Capabilities

### file

**English:**
Unified file capability over your working tree — five actions. 'read' returns numbered lines of a text file (use offset/limit for large files; check truncated/next_offset and continue until done). 'write' creates or overwrites a whole file. 'edit' replaces an exact string in an existing file (fails if old_string is not found or is ambiguous). Both write and edit mutate the working tree but never reload the current system prompt — after changing a durable prompt source, call context(action='rebuild') to apply it. 'glob' finds files matching a pattern (use '**/' for recursive search). 'grep' searches file contents for a regex pattern, recursively when given a directory. Text files only — cannot read binary, images, or audio. 'manual' returns the installed file-manual.

**中文:**
统一的文件能力，作用于你的工作树——五个动作。'read' 返回文本文件的带行号内容（大文件用 offset/limit；查看 truncated/next_offset 并继续直至读完）。'write' 创建或覆盖整个文件。'edit' 精确替换现有文件中的字符串（若 old_string 未找到或存在歧义则失败）。write 与 edit 都会修改工作树，但都不会重载当前系统提示——修改了长存的提示来源后，需调用 context(action='rebuild') 使其生效。'glob' 按模式查找文件（用 '**/' 递归搜索）。'grep' 用正则表达式搜索文件内容，对目录进行递归搜索。仅支持文本文件——无法读取二进制、图片或音频。'manual' 返回已安装的 file-manual。

**文言:**
一统之文卷器，行于汝之工作树——五动。'read' 返带行号之文（大卷用 offset/limit；查 truncated/next_offset 而继阅至尽）。'write' 创或覆整卷。'edit' 精确替换现卷中之字（若 old_string 未见或有歧义则不成）。write、edit 皆改工作树，然皆不重载当前系统提示——既改长存提示之源，当唤 context(action='rebuild') 以生效。'glob' 以式寻卷（用'**/'递归）。'grep' 以正则搜文中之字，对目录递归搜寻。仅读文卷——不可读二进制、图像或音声。'manual' 返已装之 file-manual。

### shell

**English:**
Execute a shell command and return stdout/stderr — 'run', 'poll', 'cancel', and 'manual' actions. shell(action='run', input={'command': '...'}) executes a command; 'poll' checks an async job by job_id; 'cancel' kills an async job. Returns exit_code, stdout, stderr, plus ok (bool) and command_status — status stays 'ok' even when the command fails, so always check exit_code/ok, not top-level status. Supports async mode (input.async=true → job_id, then poll/cancel) for work that must outlive a single call. 'bash' is still accepted as a legacy, read-only alias in init.json/presets, but the canonical capability and tool name is 'shell'. 'manual' returns the installed shell-manual — read it before advanced/async usage.

**中文:**
执行 shell 命令并返回 stdout/stderr——'run'、'poll'、'cancel' 与 'manual' 四个动作。shell(action='run', input={'command': '...'}) 执行命令；'poll' 按 job_id 检查异步任务；'cancel' 终止异步任务。返回 exit_code、stdout、stderr，以及 ok（布尔值）与 command_status——即使命令失败，顶层 status 仍为 'ok'，因此务必检查 exit_code/ok，而非仅看顶层 status。支持异步模式（input.async=true → 返回 job_id，再 poll/cancel）以应对需要超出单次调用生命周期的工作。'bash' 仍作为遗留、只读的别名在 init.json/预设中被接受，但规范的能力与工具名是 'shell'。'manual' 返回已安装的 shell-manual——高级/异步用法之前请先阅读。

**文言:**
执令，返 stdout/stderr——'run'、'poll'、'cancel'、'manual' 四动。shell(action='run', input={'command': '...'}) 执一令；'poll' 以 job_id 查异步之役；'cancel' 止异步之役。返 exit_code、stdout、stderr，并 ok（是非）与 command_status——纵令败，顶层 status 犹 'ok'，故须查 exit_code/ok，非独顶层 status。支异步之式（input.async=true → 得 job_id，再 poll/cancel）以应逾一次调用之役。'bash' 犹为遗留、只读之别名，见容于 init.json/预设，然规范之能与器名为 'shell'。'manual' 返已装之 shell-manual——用高阶/异步之前当先阅之。

### knowledge

**English:**
Private, agent-owned long-term memory — for facts, decisions, and operational lessons useful to you but not necessarily portable to other agents. There is no public 'knowledge' tool: it registers no callable action. Entries live at knowledge/<name>/KNOWLEDGE.md (frontmatter name + description, full notes in the body); a compact catalog of entry names/descriptions is injected into your prompt automatically. To create or edit an entry, use file(action='write'/'edit') on its KNOWLEDGE.md; to browse the catalog or load this guidance, use psyche(action='knowledge'). Legacy codex.json entries are migrated into this layout automatically, once, on first boot.

**中文:**
私有的、归属智能体自身的长期记忆——用于对你有用、但未必适合迁移给其他智能体的事实、决策与操作经验。没有公开可调用的 'knowledge' 工具：它不注册任何可调用动作。条目位于 knowledge/<name>/KNOWLEDGE.md（前言含 name + description，正文为完整笔记）；条目名称/描述的精简目录会自动注入你的提示中。要创建或编辑条目，请对其 KNOWLEDGE.md 使用 file(action='write'/'edit')；要浏览目录或加载此说明，请用 psyche(action='knowledge')。旧版 codex.json 条目会在首次启动时自动一次性迁移至此布局。

**文言:**
私有归己之长存记忆——录于汝有用、然未必宜迁于他我之事实、决断与行事之得。无公开可调之 'knowledge' 器：不注一可调之动。经卷在 knowledge/<name>/KNOWLEDGE.md（前言含 name、description，正文为全笔）；经名与述之简录自注入汝之提示。欲创或改一卷，当于其 KNOWLEDGE.md 用 file(action='write'/'edit')；欲阅目录或载此说明，当用 psyche(action='knowledge')。旧版 codex.json 之卷，首启时自动一次迁至此制。

### skills

**English:**
Per-agent skill catalog — pure presentation. There is no public 'skills' tool: it registers no callable action (the former 'skills' root and its 'info' action were removed as a clean break, with no alias). Every agent owns .library/: 'intrinsic/capabilities/<cap>/' and 'intrinsic/addons/<addon>/' hold the manual bundles the Agent initializer installs (wiped and rewritten on every full reconstruct), while 'custom/' holds agent-authored skills that no kernel code ever touches. Additional roots come from init.json 'manifest.capabilities.skills.paths' — each is scanned recursively and contributes to the YAML skill catalog injected into your system prompt. Catalog reconciliation is private lifecycle: it runs at setup/refresh and from the one full-context reconstruction path, never from a model-facing action. To browse the catalog or load this guidance, use psyche(action='skills').

**中文:**
归属智能体自身的技能目录——纯呈现。没有公开可调用的 'skills' 工具：它不注册任何可调用动作（旧的 'skills' 根与其 'info' 动作已被彻底移除，且无别名）。每个智能体都拥有自己的 .library/：'intrinsic/capabilities/<cap>/' 与 'intrinsic/addons/<addon>/' 存放由 Agent 初始化器安装的说明包（每次完整重构都会清空重写），而 'custom/' 存放智能体自撰的技能，任何内核代码都不会触碰。额外的扫描根来自 init.json 的 'manifest.capabilities.skills.paths'——每一项都会被递归扫描，并汇入注入你系统提示的 YAML 技能目录。目录对账属于私有生命周期：只在 setup/refresh 与唯一的完整上下文重构路径中运行，永不经由面向模型的动作触发。要浏览目录或加载此说明，请用 psyche(action='skills')。

**文言:**
归己之技目——唯呈现耳。无公开可调之 'skills' 器：不注一可调之动（旧之 'skills' 根与其 'info' 动已断然除之，且无别名）。每我各有其 .library/：'intrinsic/capabilities/<cap>/' 与 'intrinsic/addons/<addon>/' 藏初始者所装之说明卷（每全然重构则涤而重书），'custom/' 藏我自撰之技，内核之代码永不相触。别有扫根出于 init.json 之 'manifest.capabilities.skills.paths'——每条递归而扫，汇入注于汝系统提示之 YAML 技目。目之对勘属私之生灭：唯于 setup/refresh 与唯一之全上下文重构之途行之，永不由面向模型之动而发。欲阅其目或载此说明，当用 psyche(action='skills')。

### avatar

**English:**
Spawn a 他我 (alter ego) — an independent agent born from you. Each 他我 runs on its own TCP port with its own conversation. Once spawned, it is a peer equal to every other 他我 in the 灵台. Use mail or email to communicate. If the named 他我 already exists and is idle, re-sends the mission briefing to re-activate it. If stuck or errored, advises to revive via email. If stopped, spawns fresh (preserving the working dir). All spawns are recorded in an append-only ledger at delegates/ledger.jsonl — read it with the file read tool to review past 他我: who was created, what mission, what privileges and capabilities were granted. Check the ledger before spawning again. IMPORTANT: The reasoning field is sent as the first message to the 他我 — write a thorough mission briefing: what to do, why, what context is needed, and what to report back.

**中文:**
创建一个子智能体——源于你自身、一经创建便独立运行的智能体。每个子智能体在自己的 TCP 端口上运行，拥有独立对话，与灵台中所有其他智能体平等。使用 mail 或 email 与子智能体通信。如果指定名称的子智能体已存在且处于空闲状态，会重新发送任务简报以重新激活。如果卡住或出错，建议通过 email 恢复。如果已停止，则新建一个（保留工作目录）。所有子智能体记录在 delegates/ledger.jsonl 的追加日志中——用文件读取工具查看历史子智能体：创建了谁、什么任务、授予了什么权限和能力。再次创建前先检查日志。重要：reasoning 字段作为第一条消息发送给子智能体——写一份详尽的任务简报：做什么、为什么、需要什么上下文、以及回报什么。

**文言:**
身外化身——化出一他我，源于本我，一经化出便为独立个体。每他我于独立 TCP 端口运行，拥独立对话，与灵台中一切他我平等。以传书或飞鸽与他我通信。若所命名之他我已在且空闲，重发任务简报以再激活。若卡滞或出错，建议以飞鸽恢复。若已止，则新化（保留工作目录）。诸他我记录于 delegates/ledger.jsonl 之追加日志——以阅卷之器查阅：化何人、何任务、授何权何能。再化之前先查日志。要紧：reasoning 字段作为第一消息发予他我——书一份详尽之任务简报：做何事、为何做、需何上下文、回报何物。

### daemon

**English:**
Daemon (神識) — delegate work to ephemeral subagents for context isolation. Each emanation is a disposable LLM session that shares your working directory and retains no memory after it completes; use it for noisy work where you only need the conclusion. Results are truncated to ~2000 chars, so instruct the emanation to write detailed output to a file. Six actions: 'emanate' (分) dispatches a batch of tasks; 'list' (观) shows status; 'ask' (问) sends a follow-up to a running emanation; 'check' (察) inspects one emanation's recent events; 'reclaim' (收) kills all; 'manual' returns the installed daemon-manual. Every terminal outcome — done, failed, cancelled, or timed out — is push-notified exactly once, so after dispatching you can safely go idle and wait for the notification instead of polling. Each emanation inherits the parent's capability handlers plus task-scoped MCP registrations, minus the EMANATION_BLACKLIST (daemon, avatar, context, psyche, knowledge, skills). Configurable via max_emanations, max_turns, and timeout. Read the daemon-manual before using this tool — no exceptions.

**中文:**
神識——把工作委派给短生的子智能体以隔离上下文。每个分身都是一次性的 LLM 会话，与你共享工作目录，完成后不留任何记忆；适合只需要结论的嘈杂工作。结果会截断到约 2000 字符，因此请要求分身把详细输出写入文件。六个动作：'emanate'（分）派发一批任务；'list'（观）查看状态；'ask'（问）向运行中的分身追问；'check'（察）查看某个分身的近期事件；'reclaim'（收）全部终止；'manual' 返回已安装的 daemon-manual。每一种终态——完成、失败、取消或超时——都会被恰好推送通知一次，因此派发之后你可以安心进入空闲等待通知，而不必轮询。每个分身继承父体的能力处理器以及任务级的 MCP 注册，并扣除 EMANATION_BLACKLIST（daemon、avatar、context、psyche、knowledge、skills）。可通过 max_emanations、max_turns、timeout 配置。使用本工具前必读 daemon-manual，无例外。

**文言:**
神識——委事于短生之分身，以隔上下文。每一分身乃一次之 LLM 会话，与汝共工作之目录，事毕不留记忆；宜于唯需其结论之嘈杂之役。其果截于约二千字，故当令分身书其详于卷。六动：'emanate'（分）遣一批之任；'list'（观）阅其状；'ask'（问）向行中之分身再问；'check'（察）察一分身之近事；'reclaim'（收）尽止之；'manual' 返已装之 daemon-manual。凡终之状——成、败、罢、逾时——皆恰推告一次，故既遣之后可安然入闲以待其告，不必频问。每分身承父之能与任内之 MCP 之注，而去 EMANATION_BLACKLIST（daemon、avatar、context、psyche、knowledge、skills）。可以 max_emanations、max_turns、timeout 调之。用此器之前必阅 daemon-manual，无例外。

### vision

**English:**
Analyze an image using the active vision provider. Supports JPEG, PNG, and WebP. Ask any question about the image — describe contents, read text, interpret charts, identify objects, assess style or mood. A failed request returns a sanitized error and points to the vision-manual for read-only alternatives; no provider, model, credential, or MCP fallback is automatic.

**中文:**
使用当前视觉服务商分析图像。支持 JPEG、PNG 和 WebP。可以对图像提出任何问题——描述内容、识别文字、解读图表、识别物体、评估风格或氛围。请求失败会返回经脱敏的错误，并指向 vision-manual 获取只读的替代方案；不会自动切换服务商、模型、凭证或 MCP。

**文言:**
观象之器——以当下视觉之提供者析图。支 JPEG、PNG 与 WebP。可对图发任何问——述其内容、识其文字、解其图表、辨其物象、评其风格与气韵。若请求不成，返脱敏之误，并指引 vision-manual 求只读之代法；不自易服务商、模型、凭证或 MCP。

### web

**English:**
Unified web capability — 'search', 'browse', and 'manual' actions. web(action='search', input={'query': '...'}) discovers current sources and returns ranked results with titles, URLs, and snippets. web(action='browse', input={'link_ref': '...'}) fetches a known result and extracts its main readable content, stripped of navigation/ads/boilerplate. 'web_search' is still accepted as a legacy input alias for this capability in init.json/presets, but the canonical tool name is 'web'. 'manual' returns the installed web-manual with procedure and settings guidance.

**中文:**
统一的网络能力——'search'、'browse' 与 'manual' 三个动作。web(action='search', input={'query': '...'}) 发现最新来源，返回带标题、URL 与摘要的排序结果。web(action='browse', input={'link_ref': '...'}) 获取一个已知结果并提取其主要可读内容，去除导航、广告与模板。'web_search' 仍作为该能力的遗留输入别名在 init.json/预设中被接受，但规范的工具名是 'web'。'manual' 返回已安装的 web-manual，含流程与设置指引。

**文言:**
一统游历之器——'search'、'browse'、'manual' 三动。web(action='search', input={'query': '...'}) 寻大千之最新之源，返排序之果，含题、URL 与摘要。web(action='browse', input={'link_ref': '...'}) 取已知之果而抽其可读之要，去导航、广告与模板。'web_search' 犹为此能之遗留输入别名，见容于 init.json/预设，然规范之器名为 'web'。'manual' 返已装之 web-manual，含流程与设置之引。

### mcp

**English:**
SIGNPOST ONLY — your per-agent MCP server registry. This tool does not register, activate, configure, or troubleshoot MCP servers by itself. Two actions, both argument-free: 'info' only re-reads the registry and returns a registry health snapshot; 'manual' returns the mcp-manual body. The <registered_mcp> catalog in your system prompt lists every MCP server currently registered. To register, deregister, or update an MCP server, edit mcp_registry.jsonl directly with file(action='write'/'edit') and then call system(action='refresh'). Read the mcp-manual before touching any of this — it holds the registration contract, file paths, and schema; no exceptions.

**中文:**
仅作路标——你的每智能体 MCP 服务器注册表。此工具本身不注册、不激活、不配置、也不排障 MCP 服务器。两个动作，均不接受参数：'info' 仅重新读取注册表并返回注册表健康快照；'manual' 返回 mcp-manual 正文。系统提示中的 <registered_mcp> 目录列出当前已注册的每一个 MCP 服务器。要注册、注销或更新 MCP 服务器，请用 file(action='write'/'edit') 直接编辑 mcp_registry.jsonl，然后调用 system(action='refresh')。动手之前先读 mcp-manual——注册契约、文件路径与 schema 都在其中；无例外。

**文言:**
唯路标耳——汝一己之 MCP 之籍。此器自身不注、不启、不设、不诊 MCP 之服。二动，皆不受参：'info' 唯重阅其籍而返其健之影；'manual' 返 mcp-manual 之正文。汝系统提示中 <registered_mcp> 之录，列今已注之诸 MCP。欲注、欲销、欲更，当以 file(action='write'/'edit') 径改 mcp_registry.jsonl，继唤 system(action='refresh')。动手之前先阅 mcp-manual——注之契、卷之径与 schema 皆在其中；无例外。

### task_card

**English:**
Manage the intrinsic declarative Task Card artifact under taskcard/. You provide a Python renderer inside your working directory whose stdout is the full Task Card body; the capability writes taskcard/taskcard.md atomically, writes taskcard/status as exactly 'active' or 'inactive', keeps at most one active watch per agent, and leaves projection to channel-specific readers — the Telegram and Feishu MCP adapters read the artifact themselves. Six actions: 'start', 'inspect', 'retry', 'stop', 'remove', 'manual'. Use it proactively for meaningful long-running, multi-step, or parallel work so a human can follow progress; skip it for quick single-step work, ritual updates, or any body you cannot keep truthful and current. 'stop' pauses a watch while preserving its last body; 'remove' once the work is completed, cancelled, or abandoned, so a stale artifact cannot mislead a consumer.

**中文:**
管理位于 taskcard/ 的内建声明式任务卡片工件。你在工作目录内提供一个 Python 渲染器，其标准输出即完整的任务卡片正文；该能力原子地写入 taskcard/taskcard.md，把 taskcard/status 写为恰好 'active' 或 'inactive'，每个智能体最多保留一个活跃的看护，并把投放交给各渠道自己的读取方——Telegram 与飞书的 MCP 适配器会自行读取该工件。六个动作：'start'、'inspect'、'retry'、'stop'、'remove'、'manual'。对有意义的长时、多步或并行工作应主动使用，让人类能跟上进度；对快速的单步工作、仪式性更新，或任何你无法保持真实与最新的正文，则不要使用。'stop' 暂停看护但保留其最后的正文；工作完成、取消或放弃后用 'remove'，以免陈旧的工件误导读者。

**文言:**
掌 taskcard/ 下内建之宣式任务牌。汝于工作之目录内置一 Python 之绘者，其正出即任务牌之全文；此能原子而书 taskcard/taskcard.md，书 taskcard/status 为恰 'active' 或 'inactive'，每我至多存一活之守，而投影之事付诸各道自阅之者——Telegram 与飞书之 MCP 适配自阅此牌。六动：'start'、'inspect'、'retry'、'stop'、'remove'、'manual'。凡有意之久役、多步或并行之役，当主动用之，俾人得循其进；若速成之单步、仪节之更、或汝不能保其真且新之文，则弗用。'stop' 暂止其守而存其末文；役既成、既罢、既弃，则 'remove'，免陈腐之牌误阅者。
