---
name: lingtai-debug-toolkit
description: "Operational toolkit for lingtai agents — what to do when things break. Covers troubleshooting (process, memory, communication, tools), security auditing (secrets, permissions, MCP, data exposure), molt preparation (checklist + template), and avatar network governance. Read lingtai-anatomy first to understand the architecture, then come here for operational procedures."
version: 1.0.0
tags: [lingtai, debug, troubleshoot, security, audit, molt, governance, operations]
companion: lingtai-anatomy
---

# Lingtai Debug Toolkit

> **先读 lingtai-anatomy** 理解架构，遇到问题时回来查操作手册。
> anatomy = "how things work"（描述性），debug-toolkit = "what to do when broken"（操作性）。

## 何时使用

- Agent 不响应、卡死、或行为异常
- 需要审计网络安全性
- 准备凝蜕（molt）需要检查清单和模板
- 管理化身网络的生命周期

## Reference 文件

| Reference | 文件 | 覆盖范围 |
|-----------|------|----------|
| 故障排查 | [debug-troubleshoot.md](reference/debug-troubleshoot.md) | 进程、记忆、通信、器用、健康检查、上报协议 |
| 安全审计 | [security-audit.md](reference/security-audit.md) | 密钥扫描、文件权限、MCP 配置、通信安全、数据暴露、Agent 权限 |
| 凝蜕模板 | [molt-template.md](reference/molt-template.md) | 四层存储准备、summary 模板、验证清单 |
| 网络治理 | [network-governance.md](reference/network-governance.md) | 化身生命周期、权限管理、健康监测、CPR 协议 |

## 快速诊断

```
Agent 无响应？     → debug-troubleshoot.md → 进程诊断 → 五种生命状态速查
Molt 后丢失记忆？  → molt-template.md → 四层存储检查清单
发现密钥暴露？     → security-audit.md → 密钥扫描脚本
化身网络不稳定？   → network-governance.md → 健康监测 + CPR 协议
```

## 与 lingtai-anatomy 的关系

- **lingtai-anatomy**：描述性文档——五层存储、文件系统布局、运行时状态机、邮件协议
- **lingtai-debug-toolkit**：操作性文档——故障定位、安全扫描、凝蜕操作、网络治理
- 推荐阅读顺序：anatomy → debug-toolkit

## 安全审计原则

1. **严格只读**：审计脚本不修改任何文件
2. **结果分级**：Critical / High / Medium / Low
3. **已知架构限制**：飞鸽明文存储等——标注为"架构限制"而非"配置问题"

## 未纳入的内容

- **lingtai-agora**（网络发布/打包工具）——定位是"发布"而非"调试"，保留在原项目中。如需发布网络，直接使用 lingtai-agora skill。
