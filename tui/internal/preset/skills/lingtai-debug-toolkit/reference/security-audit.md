# LingTai Security Audit Reference

> **Read the `lingtai-kernel-anatomy` skill first to understand the architecture.** This document performs security audits based on the Lingtai architecture's file system layout, process model, and communication mechanisms.

---

## Core Principle

This audit framework is **strictly read-only**. Scan, report, recommend — but never modify any files. All fixes must be performed manually by authorized personnel with appropriate permissions.

---

## Audit Dimensions

### 1. Secret Leak Pattern Scan

**Goal**: Detect accidentally exposed API keys, tokens, and credentials in the network directory.

**Symptoms**:
- Secrets accidentally committed to a Git repository
- API keys hardcoded in configuration files
- Authentication tokens present in log files

**Causes**:
- `.env` files accidentally committed to git during development
- MCP server configurations with API keys written directly instead of referencing environment variables
- Debug logs recording full authentication headers

**Resolution (Scan)**:

```bash
# Search for common secret patterns in the network directory
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

**Command Example (Comprehensive Scan)**:
```bash
# Scan multiple secret types and output to a report
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

**Common Pitfalls**:
- ❌ Pattern matching is not 100% accurate → false positives are expected; manual verification is required
- ❌ Scanning binary files → may produce excessive noise
- ❌ Including actual secret values in the report → the report itself becomes a leak source
- ✅ Report only "match found" and replace actual values with `<REDACTED>`

**Related References**: `lingtai-kernel-anatomy` (file system layout)

---

### 2. File Permission Audit

**Goal**: Detect overly permissive file permissions to prevent unauthorized access.

**Symptoms**:
- Sensitive files (keys, configs) are readable/writable by any user
- Agent working directories are modifiable by other agents

**Causes**:
- Default creation permissions are too loose (umask settings)
- Permissions not checked during manual operations
- Shared directory misconfiguration

**Resolution (Audit)**:

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
) -ls 2>/dev/null

echo ""
echo "=== Files Not Owned by Current User ==="
find "$scan_dir" ! -user "$(whoami)" -ls 2>/dev/null | head -20
```

**Checking Specific Sensitive File Permissions**:
```bash
# Check .secrets directory (if it exists)
for f in <network-dir>/.lingtai/*/.secrets/*.json; do
  if [ -f "$f" ]; then
    perms=$(stat -f "%Lp" "$f" 2>/dev/null || stat -c "%a" "$f")
    if [ "$perms" != "600" ] && [ "$perms" != "400" ]; then
      echo "⚠️  $f has permissions $perms (should be 600 or 400)"
    fi
  fi
done
```

**Common Pitfalls**:
- ❌ Modifying file permissions without understanding the impact → may break agent functionality
- ❌ Only checking top-level directories → sensitive files may be in deeply nested subdirectories
- ✅ Audit is read-only; modifications must be performed by authorized personnel
- ✅ Focus on `.secrets/`, `mcp/servers.json`, and `.env` files

**Related References**: `lingtai-kernel-anatomy` (directory structure)

---

### 3. MCP Configuration Audit

**Goal**: Check MCP server configurations for security risks.

**Symptoms**:
- MCP configuration contains plaintext API keys
- The `command` field points to untrusted executables
- Environment variable references point to non-existent variables

**Causes**:
- Convenience-driven hardcoded keys during configuration
- Absolute paths used to reference local scripts
- MCP server sources not verified

**Resolution (Audit)**:

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

**Itemized Checklist**:

| Check Item | Safe | Risk |
|--------|------|------|
| API key referenced via `${ENV_VAR}` | ✅ | — |
| API key hardcoded in JSON | — | 🔴 Critical |
| `command` points to system path (`/usr/bin/`, `npx`) | ✅ | — |
| `command` points to project-local script | ⚠️ | 🟡 Requires verification |
| `command` points to `/tmp/` or downloaded script | — | 🔴 Critical |

**Command Example (Extract All command Fields)**:
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

**Common Pitfalls**:
- ❌ Hardcoding keys and tracking with git → secrets enter version history
- ❌ Not verifying MCP server sources → potential execution of malicious code
- ✅ Store secrets in the `.secrets/` directory; MCP configurations should reference environment variables
- ✅ Audit the source code and permissions of third-party MCP servers

**Related References**: `mcp-manual` (MCP configuration specification — kernel `mcp` capability)

---

### 4. Communication Security Audit

**Goal**: Evaluate the security posture of the Lingtai pigeon communication system.

**Symptoms**:
- Sensitive information transmitted via pigeon without encryption
- Messages readable by anyone with file system access
- No message integrity verification

**Causes (Architectural Limitations)**:
- Pigeons are stored as **plaintext JSON** on the file system
- No message encryption (at rest or in transit)
- No message authentication or integrity verification
- Inconsistent `to` field types (agent-sent vs kernel-sent)

**Resolution (Audit and Document)**:

These are **architectural limitations**, not configuration issues, and cannot be fixed through simple reconfiguration. Auditors should document and report them.

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

**Risk Assessment**:

| Risk | Severity | Description |
|------|----------|-------------|
| Plaintext mail storage | 🟡 Medium | Anyone with file system access can read all mail |
| No message integrity verification | 🟡 Medium | Messages can be tampered with undetected |
| Inconsistent `to` field types | 🟢 Low | May cause routing issues |
| No transport encryption | 🟢 Low | Local file system, no network transmission |

**Common Pitfalls**:
- ❌ Transmitting actual secrets via pigeon → use environment variables or .secrets instead
- ❌ Assuming pigeon is a secure communication channel → anyone with file system access can read messages
- ✅ Pass sensitive information through environment variables or the `.secrets/` directory, never in plaintext mail

**Related References**: `lingtai-kernel-anatomy` (mail protocol; communication model)

---

### 5. Data Exposure Audit

**Goal**: Detect sensitive data that may be accidentally exposed in the network directory.

**Symptoms**:
- Codex entries contain sensitive information that should not be shared
- Large data dump files left in the directory
- Export files (`codex export`) containing full content

**Causes**:
- Agent records sensitive data in codex without marking it
- Temporary files not cleaned up
- Export files left in shared paths

**Resolution (Audit)**:

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
) -ls 2>/dev/null

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

**Common Pitfalls**:
- ❌ Sharing codex entry IDs with others → IDs are private; others cannot access them
- ❌ Leaving export files in shared paths → any agent can read them
- ✅ When sharing knowledge, pass the actual content (via pigeon or shared files), not just IDs
- ✅ Do not enter sensitive data into codex, or clearly mark it as sensitive

**Related References**: `lingtai-kernel-anatomy` (codex; five-layer accumulation)

---

### 6. Agent Permission Audit

**Goal**: Check agent init.json files for overly privileged configurations.

**Symptoms**:
- Low-privilege agents with admin (karma/nirvana) permissions
- Multiple agents with identical full permissions

**Causes**:
- Convenience-driven granting of all permissions to every agent during configuration
- Permissions not revoked after requirements change

**Resolution (Audit)**:

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

**Principle of Least Privilege**:

| Permission | Applicable Scenario | Risk |
|------|----------|------|
| `karma=True` | Orchestrators, administrators | Can suspend/lull/interrupt any agent |
| `nirvana=True` | Primary orchestrator only | Can permanently delete agents and their working directories |
| Both False | Worker avatars | ✅ Minimal privilege |

**Common Pitfalls**:
- ❌ Granting karma=True to all avatars → any compromised avatar can affect the entire network
- ❌ Assigning nirvana=True to avatars → avatars can delete their parent
- ✅ Only grant karma/nirvana to orchestrators; avatars should have zero admin privileges
- ✅ Avatars encountering permission issues should report to their parent via pigeon

**Related References**: `lingtai-kernel-anatomy` (avatar permission model / network topology)

---

## Comprehensive Audit Workflow

### Steps to Execute a Full Audit

1. **Secret Scan**: Run the scan scripts from §1
2. **Permission Audit**: Run the find commands from §2
3. **MCP Configuration Check**: Run the check scripts from §3
4. **Communication Security Assessment**: Document architectural limitations from §4
5. **Data Exposure Check**: Run the file scans from §5
6. **Agent Permission Review**: Run the init.json audit from §6

### Severity Classification

| Severity | Meaning | Action |
|--------|------|------|
| 🔴 Critical | Active secret leak | Fix immediately: rotate keys, clean git history |
| 🟠 High | Sensitive file exposure | Review and restrict permissions |
| 🟡 Medium | Permission or configuration risk | Schedule remediation |
| 🟢 Low | Informational / architectural limitation | Document for review |

### Report Format

When reporting security findings to upstream (parent or human):

1. **Severity**: Critical / High / Medium / Low
2. **Location**: Exact file path
3. **Evidence**: Matched pattern (**always redact actual secret values**)
4. **Recommendation**: Specific remediation steps
5. **Never** include actual secret values in the report

---

## Out of Scope

- Network-layer security (TLS, firewalls)
- Inter-agent process isolation
- External user authentication
- Runtime memory inspection
- The above require system-level access privileges, which are beyond an agent's capabilities

For audits in these areas, report to your system administrator.
