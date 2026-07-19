---
name: dev-guide-repo-watch
description: >
  Read-only workflow for sweeping the Lingtai-AI GitHub org for open issues,
  open PRs, and recent activity. Use this when a LingTai developer/operator asks
  what changed across the org, wants to monitor non-self PRs/issues, or needs an
  idempotent repository-health digest. This is a nested lingtai-dev-guide
  reference, not a top-level intrinsic skill.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# LingTai Repo Watch

This nested lingtai-dev-guide reference is a read-only developer workflow for
sweeping the `Lingtai-AI` GitHub org. It lives here because it is
LingTai-project-specific; it should not be installed as a kernel intrinsic
capability for every agent.

## Boundaries

- Read-only by default: do not comment, label, assign, close, merge, or edit
  GitHub state unless the human explicitly asks for that side effect.
- Report facts with source URLs and timestamps.
- Prefer one concise digest over piecemeal alerts unless the human requested a
  live monitor.
- If automating, store state locally and alert only on changes to avoid spam.

## One-shot org sweep

Use `gh` from a clean shell with GitHub auth already configured.

```bash
gh repo list Lingtai-AI --limit 100 \
  --json name,visibility,isArchived,pushedAt,url

gh search issues --owner Lingtai-AI --state open --limit 200 \
  --json number,title,repository,state,createdAt,updatedAt,author,labels,assignees,url

gh search prs --owner Lingtai-AI --state open --limit 200 \
  --json number,title,repository,state,isDraft,createdAt,updatedAt,author,labels,url
```

`gh search prs --json` does not support `reviewDecision` on all installed gh
versions. If review-decision status is needed, fetch it separately for a small
set of PRs with a command/API that supports that field; do not put
`reviewDecision` in the broad `gh search prs` field list unless you have verified
that local `gh` supports it.

## Optional filters

For "non-self" monitoring, filter after fetching rather than hard-coding a single
human into this workflow. Example for Jason's machine:

```bash
# Conceptual filter after JSON fetch, not a GitHub write:
author.login != "huangzesen"
```

## Digest shape

Group output in this order:

1. **New or updated open PRs** — ready vs draft, author, repo, age, URL.
2. **Open issues** — repo, title, author, labels, stale/updated time, URL.
3. **Recently merged/closed items** if the caller asked for recent activity.
4. **Stale or blocked items** — old open PRs, no assignee, labels implying bug or
   release blocker.
5. **No-action footer** — say explicitly that no GitHub state was changed.

Keep each item compact:

```text
<repo>#<num> — <title> · <author> · opened <relative date> · updated <relative date> · <url>
```

A full report adds a header (`# LingTai org watch — <timestamp>`, scope, filters,
`GitHub writes: none`), a "Changes since last run" block (new / updated /
closed-or-removed) when running statefully, and an artifact/state-paths footer.

## Cron / LaunchAgent monitor pattern

When the human asks for ongoing monitoring:

1. Create a small script under the agent workspace (for example
   `workspace/lingtai_org_watch/monitor_lingtai_org.py`).
2. Query open issues/PRs with the read-only commands above.
3. Store a JSON state file keyed by `issue:<repo>#<num>` and `pr:<repo>#<num>`.
4. Alert only on new, updated, or closed/removed items.
5. Install a macOS LaunchAgent or other host scheduler with explicit logs.
6. Record the script, state path, report path, and launchd plist in the agent's
   durable stores.

Do not embed secrets in the script output or report. If a Telegram bot token is
needed for local alerts, read it from the existing local secret file and never
print it.

## Validation

Before claiming the monitor is installed:

- Run the script once without state/notification and confirm `gh` exits 0.
- Run it once with state enabled and confirm a report file is written.
- If using launchd, `launchctl print gui/$(id -u)/<label>` should show the job
  loaded and the latest exit status 0; stdout/stderr logs should be clean.
- Confirm future runs do not resend the baseline unless explicitly requested.
