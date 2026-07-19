---
name: dev-guide-security-audit
description: >
  Nested lingtai-dev-guide reference for security audits: secret scanning, file permissions, MCP config, communication security, data exposure, agent permission review, severity classification, and report format.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# LingTai Security Audit Reference

Nested lingtai-dev-guide reference. Read this after the top-level router sends you here.

> **Read the `lingtai-kernel-anatomy` skill first to understand the architecture.** This document audits against the LingTai filesystem layout, process model, and communication mechanisms.

---

## Core Principle

This audit framework is **strictly read-only**. Scan, report, recommend — but never modify any files. All fixes must be performed manually by authorized personnel with appropriate permissions.

Two rules bind every dimension below: pattern matching produces false positives, so every hit needs manual verification; and **the report must never contain an actual secret value** — report "match found" with the value replaced by `<REDACTED>`, or the report itself becomes the leak.

Each dimension is scoped to a `scan_dir`:

```bash
scan_dir="<network-dir>"
```

---

## Audit Dimensions

### 1. Secret Leak Pattern Scan

Detects API keys, tokens, and credentials exposed in the network directory — `.env` files committed during development, MCP configurations with keys written inline instead of referencing environment variables, and debug logs that captured full authentication headers.

```bash
# Scan every known secret type; persist matching file paths only
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
    grep -rl -E "$pattern" "$scan_dir" 2>/dev/null | head -10
    echo ""
  done

  echo "--- RSA/EC/DSA Private Keys ---"
  grep -rl "-----BEGIN.*PRIVATE KEY-----" "$scan_dir" 2>/dev/null | head -10
  echo ""

  echo "--- Hardcoded Secrets (generic patterns) ---"
  grep -rl -i "password\s*[:=]\s*['\"][^'\"]\{8,\}" "$scan_dir" --include="*.json" --include="*.env" --include="*.yaml" 2>/dev/null
  grep -rl -i "api_key\s*[:=]\s*['\"][^'\"]\{8,\}" "$scan_dir" --include="*.json" --include="*.env" --include="*.yaml" 2>/dev/null
  grep -rl -i "secret\s*[:=]\s*['\"][^'\"]\{8,\}" "$scan_dir" --include="*.json" --include="*.env" --include="*.yaml" 2>/dev/null

} > "$report"

echo "Report saved to: $report"
```

The persisted report is a path-only triage index. For a quick inline check of one pattern group, run any single `grep -rn` above without the redirect, narrowing with `--include="*.json" --include="*.md" --include="*.txt" --include="*.env" --include="*.yaml" --include="*.yml"`.

**Pitfalls.** Scanning binary files produces heavy noise. Never paste a matched secret into the report.

**Related References**: `lingtai-kernel-anatomy` (file system layout)

---

### 2. File Permission Audit

Detects overly permissive files — sensitive keys/configs readable or writable by any user, or agent working directories modifiable by other agents. Usually caused by a loose umask, unchecked manual operations, or shared-directory misconfiguration.

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

echo ""
echo "=== .secrets/ Permission Check ==="
for f in <network-dir>/.lingtai/*/.secrets/*.json; do
  if [ -f "$f" ]; then
    perms=$(stat -f "%Lp" "$f" 2>/dev/null || stat -c "%a" "$f")
    if [ "$perms" != "600" ] && [ "$perms" != "400" ]; then
      echo "⚠️  $f has permissions $perms (should be 600 or 400)"
    fi
  fi
done
```

**Pitfalls.** Changing permissions without understanding the impact can break agent functionality — the audit is read-only, and fixes belong to authorized personnel. Checking only top-level directories misses sensitive files nested deeper; focus on `.secrets/`, `mcp/servers.json`, and `.env`.

**Related References**: `lingtai-kernel-anatomy` (directory structure)

---

### 3. MCP Configuration Audit

Checks MCP server configurations for plaintext keys, `command` fields pointing at untrusted executables, and env-var references that resolve to nothing.

```bash
scan_dir="<network-dir>"

find "$scan_dir" -name "servers.json" -path "*/mcp/*" | while read conf; do
  echo "=== $conf ==="

  # Hardcoded secrets vs env references
  if grep -qiE "(api.key|secret|token|password)\s*:\s*\"[^\"]{8,}\"" "$conf" 2>/dev/null; then
    echo "🔴 CRITICAL: Hardcoded secret detected in $conf"
  fi
  if grep -q '\${' "$conf" 2>/dev/null; then
    echo "✅ Uses environment variable references"
  fi

  # Per-server command/env breakdown (reports references, never values)
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

**Itemized Checklist**:

| Check Item | Safe | Risk |
|--------|------|------|
| API key referenced via `${ENV_VAR}` | ✅ | — |
| API key hardcoded in JSON | — | 🔴 Critical |
| `command` points to system path (`/usr/bin/`, `npx`) | ✅ | — |
| `command` points to project-local script | ⚠️ | 🟡 Requires verification |
| `command` points to `/tmp/` or downloaded script | — | 🔴 Critical |

**Pitfalls.** Hardcoded keys under git enter version history permanently. Unverified MCP server sources can execute malicious code — audit third-party servers' source and permissions. Store secrets in `.secrets/` and reference them from MCP config by environment variable.

**Related References**: `mcp-manual` (MCP configuration specification — kernel `mcp` capability)

---

### 4. Communication Security Audit

The LingTai pigeon system has **architectural limitations**, not misconfigurations: mail is stored as plaintext JSON with no encryption at rest or in transit, no message authentication or integrity verification, and inconsistent `to` field types between agent-sent and kernel-sent mail. These cannot be fixed by reconfiguration — auditors document and report them.

```bash
scan_dir="<network-dir>"

echo "--- Sensitive Patterns in Mail ---"
find "$scan_dir" -path "*/mailbox/*" -name "*.json" | head -5 | while read mail; do
  if grep -qiE "(password|secret|token|api.key)" "$mail" 2>/dev/null; then
    echo "⚠️  Potential sensitive data in mail: $mail"
  fi
done

echo ""
echo "--- Credential Patterns in Mailboxes ---"
grep -rn -iE "(password|api_key|secret|token)\s*[:=]" "$scan_dir" --include="*.json" -l 2>/dev/null | grep mailbox | head -10
```

**Risk Assessment**:

| Risk | Severity | Description |
|------|----------|-------------|
| Plaintext mail storage | 🟡 Medium | Anyone with file system access can read all mail |
| No message integrity verification | 🟡 Medium | Messages can be tampered with undetected |
| Inconsistent `to` field types | 🟢 Low | May cause routing issues |
| No transport encryption | 🟢 Low | Local file system, no network transmission |

**Pitfalls.** Pigeon is not a secure channel — filesystem permissions are the only access control. Pass sensitive values through environment variables or `.secrets/`, never in plaintext mail.

**Related References**: `lingtai-kernel-anatomy` (mail protocol; communication model)

---

### 5. Data Exposure Audit

Detects sensitive data left lying in the network directory: codex entries holding information that should not be shared, large data dumps, and `codex export` files containing full entry content.

```bash
scan_dir="<network-dir>"

echo "--- Large Files (>10MB) ---"
find "$scan_dir" -type f -size +10M -ls 2>/dev/null

echo ""
echo "--- Codex Export Files ---"
find "$scan_dir" -name "*.codex.*" -ls 2>/dev/null

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

echo ""
echo "--- Git History (if applicable) ---"
if [ -d "$scan_dir/.git" ]; then
  cd "$scan_dir"
  echo ".gitignore contents:"
  cat .gitignore 2>/dev/null || echo "No .gitignore found!"
  echo ""
  echo "Recent commits touching sensitive-looking files:"
  git log --oneline -10 -- "*.env" "*.key" "*.pem" "*.secret" ".secrets/" 2>/dev/null
fi
```

**Pitfalls.** Codex entry IDs are private — sharing an ID gives another agent nothing, so share the actual content (via pigeon or a shared file) instead. Export files left in shared paths are readable by any agent. Keep sensitive data out of codex, or mark it clearly.

**Related References**: `lingtai-kernel-anatomy` (codex; five-layer accumulation)

---

### 6. Agent Permission Audit

Checks init.json files for over-privileged agents — low-privilege workers holding karma/nirvana, or every agent granted identical full permissions because it was convenient at configuration time.

```bash
scan_dir="<network-dir>"

find "$scan_dir" -name "init.json" | while read conf; do
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

**Pitfalls.** karma on every avatar means any compromised avatar can affect the whole network; nirvana on an avatar lets it delete its own parent. Grant karma/nirvana only to orchestrators, and have avatars report permission problems to their parent via pigeon.

**Related References**: `lingtai-kernel-anatomy` (avatar permission model / network topology)

---

## Reporting

A full audit runs §1–§6 in order, then classifies and reports.

### Severity Classification

| Severity | Meaning | Action |
|--------|------|------|
| 🔴 Critical | Active secret leak | Fix immediately: rotate keys, clean git history |
| 🟠 High | Sensitive file exposure | Review and restrict permissions |
| 🟡 Medium | Permission or configuration risk | Schedule remediation |
| 🟢 Low | Informational / architectural limitation | Document for review |

### Report Format

When reporting findings upstream (parent or human), give for each finding:

1. **Severity**: Critical / High / Medium / Low
2. **Location**: exact file path
3. **Evidence**: the matched pattern, with the value **always** replaced by `<REDACTED>`
4. **Recommendation**: specific remediation steps

**Never** include actual secret values in the report.

---

## Out of Scope

Network-layer security (TLS, firewalls), inter-agent process isolation, external user authentication, and runtime memory inspection all require system-level access privileges beyond an agent's capabilities. Report those areas to your system administrator.
