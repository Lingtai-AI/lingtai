# LingTai Security Audit Reference

> **理解架构需先阅读 `lingtai-anatomy` skill。** 本文档基于灵台架构的文件系统布局、进程模型和通信机制进行安全审计。

---

## 核心原则

本审计框架**严格只读**。扫描、报告、建议——但绝不修改任何文件。任何修复须由具有适当权限者手动执行。

---

## 审计维度

### 1. 泄露密钥模式扫描

**目标**：检测网络目录中意外泄露的 API 密钥、令牌和凭据。

**症状**：
- Git 仓库中意外提交了密钥
- 配置文件中硬编码了 API 密钥
- 日志文件中包含了认证令牌

**原因**：
- 开发时不小心将 `.env` 文件提交到 git
- MCP 服务器配置直接写入了 API 密钥而非引用环境变量
- 调试日志记录了完整的认证头

**解决方案（扫描）**：

```bash
# 在网络目录中搜索常见密钥模式
scan_dir="<network-dir>"

echo "=== GitHub Tokens ==="
grep -rn "ghp_[0-9a-zA-Z]\{36\}" "$scan_dir" --include="*.json" --include="*.md" --include="*.txt" --include="*.env" --include="*.yaml" --include="*.yml" 2>/dev/null

echo "=== OpenAI API Keys ==="
grep -rn "sk-[0-9a-zA-Z]\{48\}" "$scan_dir" --include="*.json" --include="*.md" --include="*.txt" --include="*.env" 2>/dev/null

echo "=== AWS Access Keys ==="
grep -rn "AKIA[0-9A-Z]\{16\}" "$scan_dir" --include="*.json" --include="*.env" --include="*.yaml" 2>/dev/null

echo "=== Private Keys ==="
grep -rn "-----BEGIN.*PRIVATE KEY-----" "$scan_dir" 2>/dev/null

echo "=== Hardcoded Secrets (common patterns) ==="
grep -rn -i "password\s*[:=]\s*['\"][^'\"]\{8,\}" "$scan_dir" --include="*.json" --include="*.env" --include="*.yaml" 2>/dev/null
grep -rn -i "api_key\s*[:=]\s*['\"][^'\"]\{8,\}" "$scan_dir" --include="*.json" --include="*.env" --include="*.yaml" 2>/dev/null
grep -rn -i "secret\s*[:=]\s*['\"][^'\"]\{8,\}" "$scan_dir" --include="*.json" --include="*.env" --include="*.yaml" 2>/dev/null
```

**命令示例（更全面的扫描）**：
```bash
# 扫描多种密钥类型并输出到报告
scan_dir="<network-dir>"
report="/tmp/security-scan-$(date +%Y%m%d-%H%M%S).txt"

{
  echo "=== Security Scan: $(date) ==="
  echo "Target: $scan_dir"
  echo ""

  patterns=(
    "ghp_[0-9a-zA-Z]{36}:GitHub PAT"
    "gho_[0-9a-zA-Z]{36}:GitHub OAuth"
    "github_pat_[0-9a-zA-Z_]{82}:GitHub Fine-grained PAT"
    "sk-[0-9a-zA-Z]{48}:OpenAI API Key"
    "sk-proj-[0-9a-zA-Z_]{80,}:OpenAI Project Key"
    "rk_live_[0-9a-zA-Z]{24}:Stripe Live Key"
    "rk_test_[0-9a-zA-Z]{24}:Stripe Test Key"
    "xox[bpas]-[0-9a-zA-Z-]{10,}:Slack Token"
    "AKIA[0-9A-Z]{16}:AWS Access Key"
    "AIza[0-9A-Za-z_-]{35}:Google API Key"
    "eyJ[A-Za-z0-9-_]+\.eyJ[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+:JWT Token"
  )

  for entry in "${patterns[@]}"; do
    pattern="${entry%%:*}"
    label="${entry##*:}"
    echo "--- $label ---"
    grep -rn -E "$pattern" "$scan_dir" 2>/dev/null | head -10
    echo ""
  done

  echo "--- RSA/EC/DSA Private Keys ---"
  grep -rn "-----BEGIN.*PRIVATE KEY-----" "$scan_dir" 2>/dev/null | head -10
  echo ""

} > "$report"

echo "Report saved to: $report"
```

**常见陷阱**：
- ❌ 模式匹配 100% 准确 → 必有误报，需人工验证
- ❌ 扫描二进制文件 → 可能产生大量噪音
- ❌ 在报告中包含实际密钥值 → 报告本身成为泄露源
- ✅ 报告中只写"发现匹配"，用 `<REDACTED>` 替代实际值

**相关 reference**：`lingtai-anatomy`（文件系统布局）

---

### 2. 文件权限审计

**目标**：检测过度宽松的文件权限，防止未授权访问。

**症状**：
- 敏感文件（密钥、配置）可被任意用户读写
- agent 工作目录可被其他 agent 修改

**原因**：
- 默认创建权限过于宽松（umask 设置）
- 手动操作时未注意权限
- 共享目录配置不当

**解决方案（审计）**：

```bash
scan_dir="<network-dir>"

echo "=== World-Writable Files ==="
find "$scan_dir" -type f -perm /o=w 2>/dev/null

echo ""
echo "=== Sensitive Files with Loose Permissions ==="
find "$scan_dir" \( \
  -name ".env" -o \
  -name "credentials.json" -o \
  -name "id_rsa" -o \
  -name "servers.json" -o \
  -name "*.pem" -o \
  -name "*.key" \
\) -ls 2>/dev/null

echo ""
echo "=== Files Not Owned by Current User ==="
find "$scan_dir" ! -user "$(whoami)" -ls 2>/dev/null | head -20
```

**检查特定敏感文件权限**：
```bash
# 检查 .secrets 目录（如果存在）
for f in <network-dir>/.lingtai/*/.secrets/*.json; do
  if [ -f "$f" ]; then
    perms=$(stat -f "%Lp" "$f" 2>/dev/null || stat -c "%a" "$f")
    if [ "$perms" != "600" ] && [ "$perms" != "400" ]; then
      echo "⚠️  $f has permissions $perms (should be 600 or 400)"
    fi
  fi
done
```

**常见陷阱**：
- ❌ 修改文件权限而不理解影响 → 可能破坏 agent 功能
- ❌ 只检查顶层目录 → 敏感文件可能在深层子目录
- ✅ 审计只读，修改由授权者执行
- ✅ 重点关注 `.secrets/`、`mcp/servers.json`、`.env` 文件

**相关 reference**：`lingtai-anatomy`（目录结构）

---

### 3. MCP 配置审计

**目标**：检查 MCP 服务器配置中的安全风险。

**症状**：
- MCP 配置中明文存储 API 密钥
- `command` 字段指向不可信的可执行文件
- 环境变量引用指向不存在的变量

**原因**：
- 配置时图省事直接硬编码密钥
- 使用了绝对路径引用本地脚本
- 未对 MCP 服务器来源进行验证

**解决方案（审计）**：

```bash
scan_dir="<network-dir>"

echo "=== MCP Configurations ==="
find "$scan_dir" -name "servers.json" -path "*/mcp/*" | while read conf; do
  echo "--- $conf ---"
  cat "$conf"
  echo ""

  # Check for hardcoded secrets
  if grep -qiE "(api.key|secret|token|password)\s*:\s*\"[^\"]{8,}\"" "$conf" 2>/dev/null; then
    echo "🔴 CRITICAL: Hardcoded secret detected in $conf"
  fi

  # Check for env var references (good practice)
  if grep -q '\${' "$conf" 2>/dev/null; then
    echo "✅ Uses environment variable references"
  fi

  echo ""
done
```

**逐项检查清单**：

| 检查项 | 安全 | 风险 |
|--------|------|------|
| API 密钥通过 `${ENV_VAR}` 引用 | ✅ | — |
| API 密钥硬编码在 JSON 中 | — | 🔴 Critical |
| `command` 指向系统路径（`/usr/bin/`、`npx`） | ✅ | — |
| `command` 指向项目内脚本 | ⚠️ | 🟡 需验证 |
| `command` 指向 `/tmp/` 或下载脚本 | — | 🔴 Critical |

**命令示例（提取所有 command 字段）**：
```bash
find "$scan_dir" -name "servers.json" -path "*/mcp/*" | while read conf; do
  echo "=== $conf ==="
  python3 -c "
import json, sys
try:
    data = json.load(open('$conf'))
    for name, server in data.items():
        cmd = server.get('command', 'N/A')
        args = server.get('args', [])
        env = server.get('env', {})
        print(f'  Server: {name}')
        print(f'  Command: {cmd} {\" \".join(args)}')
        if env:
            for k in env:
                val = env[k]
                if val.startswith('\${') or val.startswith('$'):
                    print(f'  Env {k}: ✅ (reference)')
                else:
                    print(f'  Env {k}: ⚠️  (hardcoded, length={len(val)})')
except Exception as e:
    print(f'  Error: {e}')
" 2>/dev/null
  echo ""
done
```

**常见陷阱**：
- ❌ 硬编码密钥并用 git 管理 → 密钥进入版本历史
- ❌ 不验证 MCP 服务器来源 → 可能执行恶意代码
- ✅ 密钥存于 `.secrets/` 目录，MCP 配置引用环境变量
- ✅ 对第三方 MCP 服务器审查其源码和权限

**相关 reference**：`lingtai-mcp`（MCP 配置规范）

---

### 4. 通信安全审计

**目标**：评估灵台飞鸽通信系统的安全状况。

**症状**：
- 敏感信息通过飞鸽传递但无加密
- 消息可被文件系统访问者读取
- 无消息完整性验证

**原因**（架构限制）：
- 飞鸽以**明文 JSON** 存储于文件系统
- 无消息加密（at rest 或 in transit）
- 无消息认证或完整性校验
- `to` 字段类型不一致（agent 发送 vs kernel 发送）

**解决方案（审计与记录）**：

这些是**架构限制**而非配置问题，无法通过简单配置修复。审计时应记录并报告。

```bash
scan_dir="<network-dir>"

echo "=== Communication Security Audit ==="

# Check mail storage format
echo "--- Mail Storage ---"
find "$scan_dir" -path "*/mailbox/*" -name "*.json" | head -5 | while read mail; do
  echo "File: $mail"
  # Check if mail contains sensitive patterns
  if grep -qiE "(password|secret|token|api.key)" "$mail" 2>/dev/null; then
    echo "⚠️  Potential sensitive data in mail: $mail"
  fi
done

# Check for plaintext credentials in any mailbox
echo ""
echo "--- Credential Patterns in Mailboxes ---"
grep -rn -iE "(password|api_key|secret|token)\s*[:=]" "$scan_dir" --include="*.json" -l 2>/dev/null | grep mailbox | head -10

echo ""
echo "=== Architectural Notes ==="
echo "1. All mail is stored as plaintext JSON — no encryption at rest"
echo "2. No message authentication or integrity verification"
echo "3. File system permissions are the only access control"
```

**风险评估**：

| 风险 | 严重度 | 说明 |
|------|--------|------|
| 邮件明文存储 | 🟡 Medium | 有文件系统访问权限者可读所有邮件 |
| 无消息完整性校验 | 🟡 Medium | 邮件可被篡改而不被发现 |
| `to` 字段类型不一致 | 🟢 Low | 可能导致路由问题 |
| 无传输加密 | 🟢 Low | 本地文件系统，无网络传输 |

**常见陷阱**：
- ❌ 尝试在飞鸽中传递真正的密钥 → 应使用环境变量或 .secrets
- ❌ 认为飞鸽是安全的通信通道 → 任何有文件系统访问者可读
- ✅ 敏感信息通过环境变量或 `.secrets/` 目录传递，不在邮件中明文传输

**相关 reference**：`email-manual`、`lingtai-anatomy`（通信模型）

---

### 5. 数据暴露审计

**目标**：检测网络目录中可能意外暴露的敏感数据。

**症状**：
- 典条目包含不应共享的敏感信息
- 大型数据转储文件遗留在目录中
- 导出文件（`codex export`）包含完整内容

**原因**：
- agent 将敏感数据记入典但未标记
- 临时文件未清理
- 导出文件遗留在共享路径

**解决方案（审计）**：

```bash
scan_dir="<network-dir>"

echo "=== Data Exposure Audit ==="

# Large files that may be data dumps
echo "--- Large Files (>10MB) ---"
find "$scan_dir" -type f -size +10M -ls 2>/dev/null

# Codex export files (contain full entry content)
echo ""
echo "--- Codex Export Files ---"
find "$scan_dir" -name "*.codex.*" -ls 2>/dev/null

# Files with sensitive names
echo ""
echo "--- Potentially Sensitive Files ---"
find "$scan_dir" \( \
  -name "*.dump" -o \
  -name "*.backup" -o \
  -name "*.sql" -o \
  -name "*.csv" -o \
  -name "*.xlsx" -o \
  -name "dump.*" \
\) -ls 2>/dev/null

# Check for git-tracked secrets
echo ""
echo "--- Git History (if applicable) ---"
if [ -d "$scan_dir/.git" ]; then
  cd "$scan_dir"
  # Check if .gitignore covers sensitive paths
  echo ".gitignore contents:"
  cat .gitignore 2>/dev/null || echo "No .gitignore found!"
  echo ""
  # Check recent commits for potential secret additions
  echo "Recent commits touching sensitive-looking files:"
  git log --oneline -10 -- "*.env" "*.key" "*.pem" "*.secret" ".secrets/" 2>/dev/null
fi
```

**常见陷阱**：
- ❌ 典条目 ID 分享给他人 → ID 是私有的，他人无法访问
- ❌ 导出文件放在共享路径 → 任何 agent 可读
- ✅ 分享知识时传递实际内容（通过飞鸽或共享文件），而非 ID
- ✅ 敏感数据不入典，或明确标记为敏感

**相关 reference**：`codex-manual`、`lingtai-anatomy`（五层积淀）

---

### 6. Agent 权限审计

**目标**：检查 agent 的 init.json 中是否存在过度权限配置。

**症状**：
- 低权限 agent 拥有 admin（karma/nirvana）权限
- 多个 agent 具有相同的完整权限

**原因**：
- 配置时图方便给所有 agent 全部权限
- 权限需求变化后未回收

**解决方案（审计）**：

```bash
scan_dir="<network-dir>"

echo "=== Agent Permission Audit ==="
find "$scan_dir" -name "init.json" | while read conf; do
  agent_dir=$(dirname "$conf")
  agent_name=$(basename "$(dirname "$conf" | sed 's|/.lingtai/.*||')")
  # Try to extract just the init.json content
  python3 -c "
import json, sys
try:
    data = json.load(open('$conf'))
    admin = data.get('admin', {})
    capabilities = [c[0] for c in data.get('capabilities', [])]
    karma = admin.get('karma', False)
    nirvana = admin.get('nirvana', False)
    
    print(f'Config: $conf')
    print(f'  karma: {karma}')
    print(f'  nirvana: {nirvana}')
    print(f'  capabilities: {capabilities}')
    
    if nirvana:
        print('  🔴 CRITICAL: nirvana=True — can permanently delete agents')
    if karma and not nirvana:
        print('  🟡 karma=True — can control peer processes')
    if not karma and not nirvana:
        print('  ✅ No admin privileges')
except Exception as e:
    print(f'  Error reading: {e}')
" 2>/dev/null
  echo ""
done
```

**权限最小化原则**：

| 权限 | 适用场景 | 风险 |
|------|----------|------|
| `karma=True` | 编排器、管理者 | 可 suspend/lull/interrupt 任何 agent |
| `nirvana=True` | 仅主编排器 | 可永久删除 agent 及其工作目录 |
| 两者皆 False | 工作化身 | ✅ 最小权限 |

**常见陷阱**：
- ❌ 所有化身都给 karma=True → 任一化身被劫持可影响全网
- ❌ nirvana=True 配给化身 → 化身可删除父代
- ✅ 只给编排器 karma/nirvana，化身保持零权限
- ✅ 化身遇到权限问题应通过飞鸽向父代报告

**相关 reference**：`avatar-manual`（化身权限模型）

---

## 综合审计流程

### 执行完整审计的步骤

1. **密钥扫描**：§1 的扫描脚本
2. **权限审计**：§2 的 find 命令
3. **MCP 配置检查**：§3 的检查脚本
4. **通信安全评估**：§4 的架构限制记录
5. **数据暴露检查**：§5 的文件扫描
6. **Agent 权限审查**：§6 的 init.json 审计

### 结果分级

| 严重度 | 含义 | 行动 |
|--------|------|------|
| 🔴 Critical | 活跃的密钥泄露 | 立即修复：轮换密钥、清理 git 历史 |
| 🟠 High | 敏感文件暴露 | 审查并限制权限 |
| 🟡 Medium | 权限或配置风险 | 排期修复 |
| 🟢 Low | 信息性/架构限制 | 记录备查 |

### 报告格式

向上级（父代或人类）报告安全发现时：

1. **严重度**：Critical / High / Medium / Low
2. **位置**：精确文件路径
3. **证据**：匹配的模式（**务必隐去实际密钥值**）
4. **建议**：具体修复步骤
5. **绝不**在报告中包含实际密钥值

---

## 本审计不覆盖的范围

- 网络层安全（TLS、防火墙）
- Agent 间进程隔离
- 外部用户认证
- 运行时内存检查
- 上述需要系统级访问权限，超出 agent 能力范围

如需这些方面的审计，请向系统管理员报告。
