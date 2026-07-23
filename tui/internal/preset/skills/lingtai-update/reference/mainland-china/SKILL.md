---
name: lingtai-update-mainland
description: Use when building or fetching TUI/portal releases from mainland China.
version: 1.1.0
last_changed_at: "2026-07-21T00:00:00Z"
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

For release/tag/API access, `install.sh --source auto|github|gitee` (or
`LINGTAI_SOURCE`) selects GitHub or the verified LingTai Gitee mirror. `auto`
runs a bounded, fail-open country lookup, prefers Gitee in mainland China, and
keeps any provider fallback on the same resolved tag. Explicit `github` or
`gitee` always wins and skips detection.

The Python `lingtai` runtime itself is installed from the bundle's verified
local artifact, not requested by package name from an index. Its third-party
dependencies use one index: explicit `LINGTAI_PYPI_INDEX_URL` always wins;
otherwise a final Gitee bundle uses Tsinghua TUNA
(`https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple`) and a final GitHub
bundle uses `https://pypi.org/simple`. Do not add an extra index automatically.

Homebrew has separate knobs because its superenv can scrub ordinary variables:
`HOMEBREW_GOPROXY` maps to `GOPROXY` and `HOMEBREW_NPM_CONFIG_REGISTRY` to
`NPM_CONFIG_REGISTRY`. The formula probes the npm registry with `npm ping`; a
failed probe leaves npm's registry alone. Verify TLS and the actual client
(`go`, `npm`, `curl`, `pip`, or `uv`) independently, then retry the smallest
failing phase. Release-bundle reachability and Python dependency-index
reachability are separate checks even though their defaults are coordinated.
