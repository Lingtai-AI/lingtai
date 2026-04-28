# LingTai Debug & Troubleshoot Reference

> **理解架构需先阅读 `lingtai-anatomy` skill。** 本文档基于灵台架构的进程模型、记忆层次和通信机制进行诊断。

---

## 快速诊断决策树

```
Problem?
├── 进程问题？
│   ├── 对方无响应 → §1.1
│   ├── 对方 OOM / 崩溃 → §1.2
│   └── 无法 spawn avatar → §1.3
├── 记忆问题？
│   ├── 凝蜕后失忆 → §2.1
│   ├── Codex 条目丢失 → §2.2
│   ├── Pad 未加载 → §2.3
│   └── 凝蜕迫近、关键操作未完成 → §2.4
├── 通信问题？
│   ├── 飞鸽未送达 → §3.1
│   ├── 飞鸽弹回 "No agent at X" → §3.2
│   └── 定时飞鸽未触发 → §3.3
└── 器用问题？
    ├── 器用超时 → §4.1
    ├── 器用不存在 → §4.2
    └── 器用结果截断 → §4.3
```

---

## 1. 进程问题

### 1.1 对方无响应

**目标**：判断一个同伴为何不回复飞鸽，并采取正确的恢复行动。

**症状**：
- 发出的飞鸽久久未收到回复
- 同伴在通讯录中存在但无回音

**原因**：
- 同伴正在忙碌（处理长 LLM 回合）
- 同伴 stuck（LLM 超时/上游错误）
- 同伴 asleep（体力耗尽或被 lull）
- 同伴 suspended（进程已死）
- 地址错误（同伴根本不在该地址）

**解决方案**：

1. 先确认自身状态健康：
   ```
   system(show)
   ```
2. 验证同伴地址：
   ```
   email(contacts)
   ```
3. 发送简单 ping 测试：
   ```
   email(send, address=<peer>, message="ping")
   ```
4. 检查心跳（heartbeat）判断进程状态：
   ```bash
   ls -la <work-dir>/.lingtai/<peer>/.agent.heartbeat
   cat <work-dir>/.lingtai/<peer>/.agent.heartbeat
   ```
5. 根据心跳判断：
   - **心跳新鲜（< 5 分钟）**：对方忙碌，等待即可
   - **心跳陈旧（> 5 分钟）**：可能 stuck 或 crashed
   - **无心跳文件**：该地址可能不存在 agent

**命令示例**：
```bash
# 检查所有 agent 的心跳
for dir in <network-dir>/.lingtai/*/; do
  name=$(basename "$dir")
  hb="$dir/.agent.heartbeat"
  if [ -f "$hb" ]; then
    age=$(( $(date +%s) - $(stat -f %m "$hb" 2>/dev/null || stat -c %Y "$hb") ))
    echo "$name: heartbeat ${age}s ago"
  else
    echo "$name: NO heartbeat"
  fi
done
```

**操作决策**：
- **有 karma 权限**：
  - `system(interrupt, address=<peer>)` — 打断卡住的 LLM 回合
  - `system(cpr, address=<peer>)` — 复苏 suspended 的 agent
- **无 karma 权限**：向父代报告，附上证据（心跳时间戳、最后通信时间）

**常见陷阱**：
- ❌ 反复发送试探邮件 → 浪费资源，不会唤醒 suspended 进程
- ❌ 对 suspended agent 执行 cpr 但无 nirvana 权限 → 静默失败
- ❌ 混淆 asleep 和 suspended → asleep 可被邮件唤醒，suspended 需 cpr
- ✅ 正确做法：先看心跳，再决定是等、是 interrupt、还是 cpr

**相关 reference**：`lingtai-anatomy`（五种生命状态）、`avatar-manual`（化身管理）

---

### 1.2 对方 OOM / 崩溃

**目标**：诊断同伴进程意外死亡的原因并恢复。

**症状**：
- 同伴心跳突然停止
- 工作目录仍在但进程不存在

**原因**：
- 主机内存不足，OS OOM killer 终止进程
- LLM 上游 API 长时间无响应导致进程超时
- Python 运行时未捕获异常
- 磁盘空间耗尽

**解决方案**：

1. 确认工作目录是否还在：
   ```bash
   ls -la <work-dir>/.lingtai/<peer>/
   ```
2. 查看崩溃日志：
   ```bash
   cat <work-dir>/.lingtai/<peer>/logs/*.log | tail -50
   ```
3. 搜索 OOM 标记：
   ```bash
   grep -i "memory\|oom\|killed" <work-dir>/.lingtai/<peer>/logs/*.log
   ```
4. 检查磁盘空间：
   ```bash
   df -h <work-dir>
   ```

**命令示例**：
```bash
# 全面检查某 agent 的健康状态
peer_dir="<work-dir>/.lingtai/<peer>"
echo "=== Process ==="
ls -la "$peer_dir/.agent.heartbeat" 2>/dev/null || echo "No heartbeat"
echo "=== Disk ==="
df -h "$peer_dir" | tail -1
echo "=== Recent logs ==="
tail -30 "$peer_dir/logs/"*.log 2>/dev/null || echo "No logs"
echo "=== OOM scan ==="
grep -il "oom\|killed\|memory" "$peer_dir/logs/"*.log 2>/dev/null || echo "No OOM indicators"
```

**操作决策**：
- **有 karma 权限**：`system(cpr, address=<peer>)` 复苏
- 复苏后检查 context 使用率，接近上限则建议凝蜕

**常见陷阱**：
- ❌ cpr 后不检查 context 使用率 → 可能立即再次崩溃
- ❌ 忽略磁盘空间 → 根因未解决，问题会反复
- ✅ OOM 后优先检查上下文窗口和附件文件大小

**相关 reference**：`lingtai-anatomy`（进程模型）、`psyche-manual`（凝蜕操作）

---

### 1.3 无法 spawn avatar（化身）

**目标**：解决 `avatar(spawn)` 调用失败的问题。

**症状**：
- `avatar(spawn)` 返回错误
- 他我进程未出现在 delegates 目录

**原因**：
- 名称冲突（已有同名化身）
- 工作目录不可写
- 磁盘空间不足
- init.json 格式错误

**解决方案**：

1. 检查化身日志，排除名称冲突和数量限制：
   ```bash
   cat <work-dir>/.lingtai/delegates/ledger.jsonl
   ```
2. 验证目录可写：
   ```bash
   touch <work-dir>/.lingtai/delegates/.test && rm <work-dir>/.lingtai/delegates/.test
   ```
3. 检查磁盘空间：
   ```bash
   df -h <work-dir>
   ```
4. 参照父代 init.json 检查格式

**命令示例**：
```bash
# 列出当前所有化身
cat <work-dir>/.lingtai/delegates/ledger.jsonl | python3 -c "
import sys, json
for line in sys.stdin:
    entry = json.loads(line.strip())
    print(f\"{entry.get('name', '?')}: {entry.get('status', '?')}\")
"
```

**常见陷阱**：
- ❌ 化身名含特殊字符（斜杠、空格、点开头）→ spawn 静默失败
- ❌ 名称超过 64 字符
- ❌ 忘记在 spawn 前先查 ledger → 名称冲突
- ✅ 化身名只使用字母、数字、下划线、连字符

**相关 reference**：`avatar-manual`

---

## 2. 记忆问题

### 2.1 凝蜕后失忆

**目标**：凝蜕后恢复工作上下文。

**症状**：
- 凝蜕后不知道自己正在做什么
- 简或灵台内容为空或不完整
- 对话历史完全消失（这是正常的）

**原因**：
- 凝蜕前未更新 pad / codex / lingtai
- 系统强制凝蜕（无 summary，只有活动日志指针）
- appended 文件超过 100K token 限制导致加载失败

**解决方案**：

1. 显式重载简：
   ```
   psyche(pad, load)
   ```
2. 浏览典中存档知识：
   ```
   codex(filter)
   ```
3. 重载灵台（身份）：
   ```
   psyche(lingtai, load)
   ```
4. 查看凝蜕期间的来信：
   ```
   email(check)
   ```
5. 从典导出重建简（如果简为空）：
   ```
   codex(export, ids=[...]) → psyche(pad, edit, files=[<paths>])
   ```
6. 如果是系统强制凝蜕（无 summary），查阅活动日志：
   ```bash
   tail -200 <work-dir>/.lingtai/<name>/logs/events.jsonl
   ```

**命令示例**：
```bash
# 查看最近的凝蜕记录
grep "molt" <work-dir>/.lingtai/<name>/logs/events.jsonl | tail -5
```

**常见陷阱**：
- ❌ 凝蜕前忘记更新四层存储 → 来世完全失忆
- ❌ 依赖对话历史而非典/简 → 凝蜕后全部丢失
- ❌ 不检查邮箱 → 错过了凝蜕期间到达的重要任务
- ✅ 凝蜕前按固定清单：典 → 简编辑 → 灵台更新 → 凝蜕 summary

**预防措施**：
- 上下文窗口 > 70% 时主动准备四层存储
- 收到一级警告时立即开始整理
- 发一封自送邮件保存关键未竟事项（邮件跨凝蜕存活）

**相关 reference**：`lingtai-anatomy`（五层积淀）、`psyche-manual`（凝蜕操作）、`codex-manual`

---

### 2.2 Codex 条目丢失

**目标**：找回似乎消失的典条目。

**症状**：
- 记得创建过的典条目不见了
- `codex(filter)` 列表中缺少预期条目

**原因**：
- 条目从未成功提交（提交时有错误）
- 被 consolidate 合并到另一条目中
- 被手动 delete 焚毁
- 导出文件被误删

**解决方案**：

1. 列出所有条目，确认是否以不同标题存在：
   ```
   codex(filter)
   ```
2. 搜索导出文件：
   ```bash
   find <work-dir> -name "*.codex.*" -mtime -1
   ```
3. 检查活动日志中的 codex 操作记录：
   ```bash
   grep "codex" <work-dir>/.lingtai/<name>/logs/events.jsonl | tail -20
   ```

**常见陷阱**：
- ❌ consolidate 后以为原始条目仍在 → 它们已被合并删除
- ❌ 不确认 submit 是否成功 → 网络错误可能导致静默失败
- ✅ 关键条目在 consolidate 前 export 备份

**相关 reference**：`codex-manual`

---

### 2.3 Pad 未加载

**目标**：解决凝蜕后简未自动加载的问题。

**症状**：
- 系统提示中缺少简的内容
- 工作笔记丢失

**原因**：
- pad.md 文件为空
- appended 文件总量超过 100K token
- 系统加载错误

**解决方案**：

1. 显式加载：
   ```
   psyche(pad, load)
   ```
2. 检查文件是否存在：
   ```bash
   cat <work-dir>/.lingtai/<name>/system/pad.md
   ```
3. 如果文件有内容但加载失败，检查 appended 文件总量：
   ```bash
   du -sh <work-dir>/.lingtai/<name>/system/
   ```
4. 从典导出重建：
   ```
   codex(export, ids=[...]) → psyche(pad, edit, files=[<paths>])
   ```

**常见陷阱**：
- ❌ append 过多大文件 → 超过 100K token 限制导致加载失败
- ❌ 凝蜕前不检查 pad 文件 → 来世发现为空
- ✅ 定期检查 appended 文件列表：`psyche(pad, append)` 无 files 参数可查看

**相关 reference**：`psyche-manual`

---

### 2.4 凝蜕迫近、关键操作未完成

**目标**：在上下文窗口即将耗尽时，优先完成最关键的操作。

**症状**：
- 系统上下文警告
- 回忆早期对话困难
- 器用调用变慢

**解决方案（按优先级）**：

| 优先级 | 操作 | 说明 |
|--------|------|------|
| 🔴 P0 | 发送关键通知 | 未回复的重要邮件、关键发现、修正 |
| 🟡 P1 | 录入典 | 关键发现、决策、修正 |
| 🟡 P1 | 更新简 | 当前状态、待办事项、协作者 |
| 🟢 P2 | 更新灵台 | 身份变化、新技能 |
| 🔵 P3 | 写凝蜕 summary | 最后一步，给来世的遗嘱 |

**紧急技巧**：
- 发一封自送邮件保存关键未竟事项（邮件跨凝蜕存活）
- 如果只能做一件事：写尽可能详细的凝蜕 summary

**常见陷阱**：
- ❌ 上下文 > 80% 时仍在启动新的长操作（文件分析、网页搜索）→ 必定超限
- ❌ 忽略系统警告 → 被强制凝蜕，无 summary
- ✅ 收到一级警告立即开始四层存储整理

**相关 reference**：`lingtai-anatomy`（警之序）、`psyche-manual`（凝蜕操作）

---

## 3. 通信问题

### 3.1 飞鸽未送达

**目标**：解决发出的飞鸽对方未收到的问题。

**症状**：
- 飞鸽发送成功但对方说未收到
- 对方 inbox 中没有来信

**原因**：
- 地址格式错误（内部地址含 `@`）
- 使用了 `send` 而非 `reply` 导致路由错误
- 对方目录名拼写错误
- 对方进程已 suspended（邮件送达但不会被处理）

**解决方案**：

1. 检查已发邮件确认发送成功：
   ```
   email(check, folder=sent)
   ```
2. 确认地址格式正确：
   - ✅ 正确：`human`、`researcher`、`some-peer`（裸路径）
   - ❌ 错误：`human@example.com`（含 `@` → 走 imap 通道）
3. 检查对方收件箱是否存在：
   ```bash
   ls -la <work-dir>/.lingtai/<recipient>/mailbox/inbox/
   ```

**常见陷阱**：
- ❌ 内部地址用了 `@` → 邮件被路由到 imap，而非灵台飞鸽
- ❌ 回复来信时用 `send` 而非 `reply` → 可能路由到错误的地址空间
- ❌ 对方 suspended 时反复发邮件 → 邮件会堆积，但不会被处理
- ✅ 始终用 `reply` 回复来信，用 `send` 发新邮件

**相关 reference**：`email-manual`

---

### 3.2 飞鸽弹回 "No agent at X"

**目标**：解决发送飞鸽时收到 "No agent at X" 错误。

**症状**：
- `email(send)` 返回 "No agent at X"

**原因**：
- X 含 `@` → 用错了通道（应走 imap）
- X 是裸路径但该地址不存在 agent
- agent 刚被 nirvana（永久删除）
- agent 正在凝蜕中（短暂不可用）

**解决方案**：

1. 如果 X 含 `@`：切换到 imap 器用
2. 如果 X 是裸路径：
   - 检查 agent 是否被改名或迁移
   - 查看化身日志：
     ```bash
     cat <work-dir>/.lingtai/delegates/ledger.jsonl
     ```
   - 询问父代或同伴该 agent 是否已被 nirvana
3. 如果 agent 刚凝蜕，等几秒重试

**常见陷阱**：
- ❌ 看到 "No agent" 就认为 agent 已被删除 → 可能只是暂时的
- ❌ 对含 `@` 的地址用 email 器用 → 始终失败
- ✅ 先判断地址类型，再选择正确的通信通道

**相关 reference**：`email-manual`、`avatar-manual`

---

### 3.3 定时飞鸽未触发

**目标**：解决 schedule 创建的定时飞鸽未按预期发送。

**症状**：
- 定时邮件未在预期间隔发送
- schedule 似乎已停止工作

**原因**：
- schedule 已 paused（被 cancel）
- count 已耗尽（达到发送次数上限）
- interval/count 参数设置错误

**解决方案**：

1. 列出所有 schedule：
   ```
   email(schedule={action: "list"})
   ```
2. 检查状态：paused / active / exhausted
3. 如果 paused，重新激活：
   ```
   email(schedule={action: "reactivate", schedule_id: "<id>"})
   ```
4. 如果参数有误，cancel 并重建：
   ```
   email(schedule={action: "cancel", schedule_id: "<id>"})
   email(schedule={action: "create", interval: N, count: M}, address=..., message=...)
   ```

**常见陷阱**：
- ❌ 忘记 count 参数 → schedule 可能只发一次就停止
- ❌ cancel 后忘记 recreate → 任务丢失
- ✅ 创建 schedule 后立即 list 确认参数正确

**相关 reference**：`email-manual`

---

## 4. 器用问题

### 4.1 器用超时

**目标**：解决器用调用挂起或超时的问题。

**症状**：
- 器用调用长时间无返回
- 返回 timeout 错误

**原因**：
- I/O 密集操作（bash、web_search、web_read）超过默认超时
- 外部 API 不可用
- 文件过大导致读取超时
- 主机资源不足

**解决方案**：

1. 识别器用类型：
   - I/O 密集：bash、web_search、web_read
   - 计算密集：listen、vision
2. bash 增加超时时间：
   ```
   bash(command="...", timeout=120)
   ```
3. 大文件分块读取：
   ```
   read(file_path="...", offset=1, limit=100)
   ```
4. 网页操作：先用简单查询测试连通性
5. 系统性超时：检查主机负载

**命令示例**：
```bash
# 将长输出重定向到文件
bash(command="long-running-command > /tmp/output.txt 2>&1", timeout=300)
# 然后分块读取
read(file_path="/tmp/output.txt", offset=1, limit=100)
```

**常见陷阱**：
- ❌ bash 使用默认 30 秒超时处理长任务 → 必定超时
- ❌ 一次性 read 大文件 → 应分块
- ✅ 长输出先写到文件，再分块读取

**相关 reference**：`bash-manual`、`read-manual`、`web_read-manual`

---

### 4.2 器用不存在

**目标**：解决预期可用的器用不在器用列表中的问题。

**症状**：
- 调用某器用时被告知 "not available"
- 刚安装的 MCP 器用不可见

**原因**：
- 新安装的 MCP 服务器未 refresh
- init.json 中未配置该能力
- MCP 服务器配置错误（servers.json）

**解决方案**：

1. 查看当前能力列表：
   ```
   system(show)
   ```
2. 如果刚安装 MCP 服务器，刷新：
   ```
   system(refresh)
   ```
3. 检查 MCP 配置：
   ```bash
   cat <work-dir>/.lingtai/<name>/mcp/servers.json
   ```
4. 刷新后再次确认：
   ```
   system(show)
   ```

**常见陷阱**：
- ❌ 安装 MCP 后不 refresh → 新器用不可见
- ❌ 修改 init.json 后不 refresh → 配置未生效
- ✅ 安装/修改后立即 refresh，然后 show 确认

**相关 reference**：`lingtai-mcp`（MCP 配置）

---

### 4.3 器用结果截断

**目标**：解决器用返回不完整输出的问题。

**症状**：
- 器用返回的内容不完整
- 输出末尾有截断标记

**原因**：
- 文件太大超出单次返回限制
- grep 匹配数超过 max_matches
- 邮件预览被截断

**解决方案**：

| 器用 | 解决方案 |
|------|----------|
| `read` | 用 `offset`/`limit` 分块读取 |
| `bash` | 输出重定向到文件：`command > /tmp/out.txt 2>&1` |
| `grep` | 减少 `max_matches` 或缩小 glob 范围 |
| `email(check)` | 用 `filter.truncate=0` 获取全文，或 `email(read)` 读取单封 |

**常见陷阱**：
- ❌ 假设输出完整 → 静默截断可能遗漏关键信息
- ❌ 反复 read 同一大文件 → 浪费上下文
- ✅ 大输出先写到文件，按需读取

**相关 reference**：`read-manual`、`bash-manual`、`grep-manual`

---

## 5. 健康检查

### 一键网络诊断

```bash
# 检查所有 agent 的心跳
for dir in <network-dir>/.lingtai/*/; do
  name=$(basename "$dir")
  hb="$dir/.agent.heartbeat"
  if [ -f "$hb" ]; then
    age=$(( $(date +%s) - $(stat -f %m "$hb" 2>/dev/null || stat -c %Y "$hb") ))
    if [ "$age" -lt 300 ]; then
      echo "✅ $name: alive (${age}s ago)"
    else
      echo "⚠️  $name: stale heartbeat (${age}s ago)"
    fi
  else
    echo "❌ $name: no heartbeat"
  fi
done

# 检查磁盘空间
df -h <network-dir>

# 检查收件箱大小
for dir in <network-dir>/.lingtai/*/mailbox/inbox/; do
  name=$(echo "$dir" | sed 's|.*\.lingtai/\(.*\)/mailbox/.*|\1|')
  count=$(ls "$dir" 2>/dev/null | wc -l)
  if [ "$count" -gt 50 ]; then
    echo "⚠️  $name: inbox has $count messages (possible overflow)"
  fi
done
```

**结果解读**：
- ✅ = 健康
- ⚠️ = 警告（需要关注）
- ❌ = 错误（需要立即行动）

---

## 6. 上报协议

当无法自行解决问题时：

1. **收集证据**：心跳时间戳、日志摘要、错误信息
2. **报告父代**：通过 `email(send, address=<parent>)` 发送，标题以 `[Issue]` 开头
3. **包含**：
   - 发生了什么
   - 尝试了什么
   - 期望什么
   - 相关文件路径
4. **如果父代也无响应**：检查其他同伴是否存活，问题可能是全网级别的
5. **绝对不要**向疑似宕机的同伴反复发送试探邮件 → 向上报告

---

## 附录：五种生命状态速查

| 状态 | 心智 (LLM) | 身体 (心跳/监听) | 典型触发 |
|------|-----------|-----------------|---------|
| ACTIVE | 工作中 | 运行 | 处理消息或回合中 |
| IDLE | 等待 | 运行 | 回合之间；心流在此触发 |
| STUCK | 出错 | 运行 | LLM 超时/上游错误 |
| ASLEEP (眠) | 暂停 | 运行 | `system(sleep)` / `system(lull)` / 体力耗尽 |
| SUSPENDED (假死) | 关闭 | 关闭 | `.suspend` 文件 / SIGINT / 崩溃 / `system(suspend)` |

**关键区别**：ASLEEP 的身体仍在运行，邮件可以唤醒；SUSPENDED 的进程已死，需 cpr 复苏后才能处理邮件。
