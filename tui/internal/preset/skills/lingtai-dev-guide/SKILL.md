---
name: lingtai-dev-guide
description: >
  Router for contributing to the LingTai project. Use this when you are about
  to change LingTai code or docs, set up a dev environment, navigate the Go
  TUI/portal repo or Python kernel, develop MCP addons, prepare a release,
  troubleshoot a running network, audit security, or govern avatars. This is
  for developers and contributors; for end-user lessons, use tutorial-guide.
version: 2.2.0
---

# LingTai Developer Guide

This skill is the developer router for LingTai. Start here, choose exactly the
reference you need, then read that nested file before touching code. The root
stays short on purpose; the detailed procedures live under `reference/<topic>/`.

## Non-negotiable rules

- **Progressive disclosure:** router → nested reference → anatomy skill → code +
  tests. Do not jump straight from memory to edits.
- **Code is truth:** reference files route and summarize; cited source files,
  tests, and `ANATOMY.md` files are authoritative.
- **Anatomy travels with code:** if you move/rename/split/delete code cited by an
  `ANATOMY.md`, update the anatomy in the same commit.
- **Explicit human authorization gates:** do not open/merge PRs, push commits,
  file issues, close/delete resources, or change config unless the human gave an
  imperative authorization for that side effect.
- **Human-facing deliverables prefer HTML:** substantial plans, audits, release
  notes, and PR-readiness reports should be standalone HTML unless waived.
- **Release/install docs rule:** LingTai runtime is normally managed by the
  TUI-created project venv; do not present bare `pip install/upgrade lingtai` as
  the standard user path. Use manual pip/venv commands only for developer,
  diagnostic, or verification contexts.

## Nested reference catalog

`lingtai-dev-guide` owns these nested references. They are parent-owned
drill-down files, not standalone top-level skills.

```yaml
- name: dev-guide-architecture
  location: reference/architecture/SKILL.md
  description: |
    Project shape, repositories, IPC boundaries, runtime state layout, and where
    to start when orienting yourself in LingTai development.
- name: dev-guide-setup
  location: reference/setup/SKILL.md
  description: |
    Local development environment setup for the Go TUI/portal repo, Python
    kernel, MCP addons, and related verification commands.
- name: dev-guide-contributing
  location: reference/contributing/SKILL.md
  description: |
    Contribution workflows for TUI, portal, kernel, addons, bundled utilities,
    skill changes, tests, PR preparation, review discipline, and local
    worktree hygiene (auditing and cleaning stale git worktrees).
- name: dev-guide-gotchas
  location: reference/gotchas/SKILL.md
  description: |
    Known pitfalls and footguns while coding LingTai: runtime venv assumptions,
    prompt/system behavior, packaging, state files, and stale-doc hazards.
- name: dev-guide-releasing
  location: reference/releasing/SKILL.md
  description: |
    Release procedures for TUI/portal and kernel changes, including readiness
    checks, changelog/reporting expectations, and release artifact guidance.
- name: dev-guide-debug-troubleshoot
  location: reference/debug-troubleshoot/SKILL.md
  description: |
    Diagnosing stuck, errored, quiet, or misbehaving LingTai networks with logs,
    health surfaces, doctor checks, and code-backed troubleshooting.
- name: dev-guide-security-audit
  location: reference/security-audit/SKILL.md
  description: |
    Security auditing for secrets, permissions, MCP/addon config, channels,
    data exposure, and safe reporting of findings.
- name: dev-guide-network-governance
  location: reference/network-governance/SKILL.md
  description: |
    Operating avatar networks over time: delegation, collaboration, durable
    knowledge, stewardship norms, and long-running network maintenance.
```

## Routing table

| If you need to... | Read |
|---|---|
| Understand the project shape, repos, IPC, and state layout | `reference/architecture/SKILL.md` |
| Set up a local development environment | `reference/setup/SKILL.md` |
| Make a contribution in TUI, portal, kernel, addons, or skills | `reference/contributing/SKILL.md` |
| Avoid common footguns while coding | `reference/gotchas/SKILL.md` |
| Ship a TUI/portal or kernel release | `reference/releasing/SKILL.md` |
| Diagnose a stuck, errored, or misbehaving LingTai network | `reference/debug-troubleshoot/SKILL.md` |
| Audit secrets, permissions, MCP config, channels, or data exposure | `reference/security-audit/SKILL.md` |
| Operate an avatar network over time | `reference/network-governance/SKILL.md` |

## Related skills to load instead or next

| Need | Skill |
|---|---|
| Navigate Go TUI/portal code structurally | `lingtai-tui-anatomy` |
| Navigate Python kernel code structurally | `lingtai-kernel-anatomy` |
| Develop, register, or troubleshoot MCP servers/addons | `mcp-manual` first, then `lingtai-kernel-anatomy` `reference/mcp-protocol.md` |
| Author or publish skills | `skills-manual` |
| Customize, export, or package project methodology as a recipe | `lingtai-recipe` |
| Work on portal APIs, topology recording, replay, or `.portal/` state | `lingtai-portal-guide` |
| Prepare for a consequential molt during long dev work | `psyche-manual` |
| Explain LingTai to an end user lesson-by-lesson | `tutorial-guide` |
| Sweep the GitHub org read-only for current issues/PRs | `lingtai-repo-watch` |
| Report a LingTai bug or stale documentation | `lingtai-issue-report` |

## Orientation snapshot

| Repo / package | Stack | Main role | Where to start |
|---|---|---|---|
| `Lingtai-AI/lingtai` | Go + TypeScript | `lingtai-tui`, `lingtai-portal`, bundled utilities | `reference/architecture/SKILL.md`, then `lingtai-tui-anatomy` |
| `Lingtai-AI/lingtai-kernel` | Python | agent runtime, tools, mailbox, soul/molt, intrinsic capabilities | `lingtai-kernel-anatomy` |
| `lingtai-imap`, `lingtai-telegram`, `lingtai-feishu`, `lingtai-wechat`, `lingtai-whatsapp` | Python MCPs | channel/addon integrations | `mcp-manual` plus each addon's README |

## Common routing examples

- **"I need to change a TUI screen"** → `reference/contributing/SKILL.md` →
  `lingtai-tui-anatomy` → relevant Go files → focused `go test`.
- **"I need to add a capability or inspect runtime behavior"** →
  `lingtai-kernel-anatomy` → relevant kernel anatomy/code → kernel tests.
- **"An agent is quiet or unreachable"** → `reference/debug-troubleshoot/SKILL.md`
  → `lingtai-doctor` if local health surfaces disagree.
- **"I am preparing a release"** → `reference/releasing/SKILL.md` and use
  `reference/release-html-log-template.html` as the starter HTML report if you do
  not already have a stronger release-specific design.
- **"This broad dev task needs triage"** → run the read-only portfolio sweep in
  `reference/contributing/SKILL.md`, then ask for authorization before mutating
  GitHub state.
- **"Local worktrees are piling up"** → the "Worktree hygiene" section in
  `reference/contributing/SKILL.md`: audit first, remove only merged + clean
  secondary worktrees, record what was removed.

## Skill layout

```text
lingtai-dev-guide/
├── SKILL.md
└── reference/
    ├── architecture/SKILL.md
    ├── setup/SKILL.md
    ├── contributing/SKILL.md
    ├── gotchas/SKILL.md
    ├── releasing/SKILL.md
    ├── release-html-log-template.html
    ├── debug-troubleshoot/SKILL.md
    ├── security-audit/SKILL.md
    └── network-governance/SKILL.md
```

Now read the nested reference that matches the task, then verify against current
repo state before acting.
