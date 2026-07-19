---
name: lingtai-update-mainland
description: Use when building or fetching TUI/portal releases from mainland China.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Mainland-China routing

Nested `lingtai-update` reference. Troubleshooting guidance, not a promise that
any mirror is reachable. Before selecting the install flow, `install.sh` probes
`https://proxy.golang.org/github.com/golang/go/@latest` for three seconds when
`curl` is available and `GOPROXY` is unset; if that probe fails it exports all
three variables below, overwriting existing `GOSUMDB` or `NPM_CONFIG_REGISTRY`
values:

```bash
export GOPROXY="https://goproxy.cn,direct"
export GOSUMDB="sum.golang.google.cn"
export NPM_CONFIG_REGISTRY="https://registry.npmmirror.com"
```

For release/tag/API access the installer still uses GitHub, so it may need an
accessible route, an explicitly supplied `--version vX.Y.Z`, or a local source
ref. Current installer code never selects a Gitee mirror: use one only when your
organization has a verified source mirror, and never present it as an automatic
LingTai fallback.

Homebrew has separate knobs because its superenv can scrub ordinary variables:
`HOMEBREW_GOPROXY` maps to `GOPROXY` and `HOMEBREW_NPM_CONFIG_REGISTRY` to
`NPM_CONFIG_REGISTRY`. The formula probes the npm registry with `npm ping`; a
failed probe leaves npm's registry alone. Verify TLS and the actual client
(`go`, `npm`, or `curl`) independently, then retry the smallest failing phase.
Do not mix kernel/PyPI connectivity advice into this TUI/portal build route.
