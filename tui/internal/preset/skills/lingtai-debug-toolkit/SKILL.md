---
name: lingtai-debug-toolkit
description: "Operational toolkit for lingtai agents — what to do when things break. Covers troubleshooting (process, memory, communication, tools), security auditing (secrets, permissions, MCP, data exposure), molt preparation (checklist + template), and avatar network governance. Read lingtai-kernel-anatomy first to understand the architecture, then come here for operational procedures."
version: 1.0.0
tags: [lingtai, debug, troubleshoot, security, audit, molt, governance, operations]
companion: lingtai-kernel-anatomy
---

# Lingtai Debug Toolkit

> **Read lingtai-kernel-anatomy first** to understand the architecture; return here for operational procedures when something goes wrong.
> anatomy = "how things work" (descriptive), debug-toolkit = "what to do when broken" (operational).

## When to Use

- Agent is unresponsive, stuck, or behaving abnormally
- Network security audit is needed
- Preparing for a molt and need the checklist/template
- Managing the lifecycle of an avatar network

## Reference Files

| Reference | File | Coverage |
|-----------|------|----------|
| Troubleshooting | [debug-troubleshoot.md](reference/debug-troubleshoot.md) | Process, memory, communication, tools, health checks, escalation protocol |
| Security Audit | [security-audit.md](reference/security-audit.md) | Secret scanning, file permissions, MCP config, communication security, data exposure, agent permissions |
| Molt Template | [molt-template.md](reference/molt-template.md) | Four-layer storage prep, summary template, verification checklist |
| Network Governance | [network-governance.md](reference/network-governance.md) | Avatar lifecycle, permission management, health monitoring, CPR protocol |

## Quick Diagnosis

```
Agent unresponsive?     → debug-troubleshoot.md → Process diagnosis → Five lifecycle states lookup
Lost memory after molt? → molt-template.md → Four-layer storage checklist
Found exposed secrets?  → security-audit.md → Secret scanning scripts
Avatar network unstable?→ network-governance.md → Health monitoring + CPR protocol
```

## Relationship to lingtai-kernel-anatomy

- **lingtai-kernel-anatomy**: Descriptive documentation — five-layer storage, filesystem layout, runtime state machine, email protocol
- **lingtai-debug-toolkit**: Operational documentation — fault diagnosis, security scanning, molt procedures, network governance
- Recommended reading order: anatomy → debug-toolkit

## Security Audit Principles

1. **Strictly read-only**: Audit scripts must not modify any files
2. **Severity ratings**: Critical / High / Medium / Low
3. **Known architectural limitations**: Plaintext storage, etc. — flag these as "architectural constraints," not "configuration issues"

## Out of Scope

- **lingtai-agora** (network publishing/packaging tool) — its purpose is "publishing," not "debugging," and it remains in the original project. To publish a network, use the lingtai-agora skill directly.
