---
name: lingtai-update-mainland
description: Use when building or fetching TUI/portal releases from mainland China.
version: 1.0.0
last_changed_at: "2026-07-15T01:50:00-07:00"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Mainland-China routing

Nested `lingtai-update` reference. This is troubleshooting guidance, not a
promise that any mirror is reachable. `install.sh` probes
`https://proxy.golang.org/github.com/golang/go/@latest` for three seconds before
selecting the install flow, when `curl` is available, `GOPROXY` is unset, and
the probe fails. On failure it exports all three variables below, including
overwriting existing `GOSUMDB` or `NPM_CONFIG_REGISTRY` values:

```bash
export GOPROXY="https://goproxy.cn,direct"
export GOSUMDB="sum.golang.google.cn"
export NPM_CONFIG_REGISTRY="https://registry.npmmirror.com"
```

For GitHub release/tag/API access, the installer still uses GitHub and may need
an accessible route, an explicitly supplied `--version vX.Y.Z`, or a local
source ref. A Gitee mirror is not selected by current installer code: use it
only when your organization has a verified source mirror, and do not present
it as an automatic LingTai fallback.

Homebrew has separate knobs because its superenv can scrub ordinary variables:
`HOMEBREW_GOPROXY` maps to `GOPROXY`, and
`HOMEBREW_NPM_CONFIG_REGISTRY` maps to `NPM_CONFIG_REGISTRY`. The formula probes
the npm registry with `npm ping`; a failed probe leaves npm's registry alone.
Verify TLS and the actual client (`go`, `npm`, or `curl`) independently, then
retry the smallest failing phase. Do not mix kernel/PyPI connectivity advice
into this TUI/portal build route.
