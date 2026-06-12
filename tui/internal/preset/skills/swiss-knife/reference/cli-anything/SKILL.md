---
name: cli-anything
description: >
  Nested swiss-knife reference for CLI-Anything. Use this for authenticated
  websites without public APIs, browser-based CLIs, site-specific harnesses,
  CLI-Hub discovery, or agent-native CLI harnesses for GUI, desktop software,
  web-service, and browser workflows. If the task is only to fetch, extract,
  scrape, or search web content, use the web-browsing skill instead.
version: 1.0.0
tags: [cli, automation, cli-anything, cli-hub, browser, gui, harness, web-browsing, authenticated-site]
---

# CLI-Anything — Agent-Native CLI Harnesses

[CLI-Anything](https://github.com/HKUDS/CLI-Anything) is an ecosystem for making
software “agent-native” by wrapping GUI applications, services, and workflows in
command-line harnesses. Its README describes two entry points:

1. **CLI-Hub** — `pip install cli-anything-hub`, then `cli-hub list/search/info/install/launch` to discover and run existing harnesses.
2. **Harness generation / plugin workflow** — a SKILL/plugin-guided workflow that builds a new harness, tests it, documents it, and installs it as a CLI.

Use this reference as the LingTai swiss-knife bridge: first look for an existing
CLI-Hub harness; if none fits, design a narrow harness with explicit safety,
tests, and audit logging.

**Read vs. act boundary:** if the task is only to fetch, extract, scrape, search,
or JS-render a webpage, route back to `web-browsing` and its `extract_page.py`
pipeline. Use this reference when the goal is to expose an application workflow as
a command interface or operate an authenticated site under explicit approval.

## Read this when

- The human asks “can we use CLI-Anything for this?”
- A task asks for a browser-based CLI, site-specific harness, or practical path
  for an authenticated website with no clean public API.
- A task needs a reusable command interface for GUI software, desktop apps,
  browser workflows, web services, or authenticated websites without a clean API.
- You are considering turning a repeated browser/manual workflow into a LingTai
  skill or local CLI.
- You arrived from `web-browsing` because the need changed from reading content
  to operating a site or wrapping a workflow.
- A GitHub issue asks for “browser-based CLI”, “site-specific CLI harness”,
  “agent-native software”, or similar.

## Hard safety rules

- **Do not install packages, launch external apps, or operate authenticated
  websites without explicit human approval.** Discovery and repo inspection are
  fine; installation/execution against real accounts is a side effect.
- **Prefer official APIs and MCPs first.** CLI-Anything is most useful when no
  stable API/MCP exists, or when a GUI workflow is the source of truth.
- **Do not ask agents to handle credentials, cookies, session storage,
  `localStorage`, MFA codes, recovery codes, or long-lived tokens.** Do not pass
  them through CLI flags, environment variables, prompts, logs, or skill files. If
  a browser harness needs login, prefer a human-owned local browser/profile/session
  and document only the non-secret setup.
- **Separate read/draft/action modes.** Read-only inspection is safer; sending
  messages, submitting forms, changing records, or bulk operations need explicit
  approval and an audit log.
- **Respect site terms and rate limits.** Do not use browser automation to bypass
  access controls, CAPTCHAs, anti-bot systems, or terms-of-service restrictions.

## Quick decision tree

1. **Is there an official API or existing LingTai MCP?** Use that instead of a
   browser/GUI harness.
2. **Is there an existing CLI-Hub harness?** Search CLI-Hub; if one matches,
   inspect its README/SKILL before installing.
3. **Is the task one-off?** If yes, do not build a harness; use normal browser,
   web, or manual handoff.
4. **Is the workflow repeated and structured?** Design a narrow CLI harness with
   a manifest, JSON output, tests, and logs.
5. **Does it touch authenticated external state?** Start with `read_only`, then
   `draft_only`, then `approve_before_action`. Treat `full_action` as a future,
   explicit, allowlisted mode only.

## Inspect CLI-Anything / CLI-Hub

Use a temporary workspace first. Do not install globally unless the human asks.

```bash
# Read-only repo inspection
cd /tmp
git clone --depth 1 https://github.com/HKUDS/CLI-Anything.git
cd CLI-Anything
grep -n "^##" README.md | head -80
find . -maxdepth 2 -type f | sort | head -120
```

If the human approves installing the hub, prefer a project-local venv:

```bash
python3 -m venv .venv-cli-hub
. .venv-cli-hub/bin/activate
python -m pip install --upgrade pip
python -m pip install cli-anything-hub

cli-hub list
cli-hub search browser
cli-hub info browser
cli-hub info web-yu-pri
```

CLI-Hub commands from the upstream README:

| Command | Use |
|---|---|
| `cli-hub list` | Browse registry |
| `cli-hub search <query>` | Search by keyword |
| `cli-hub info <name>` | Inspect one harness |
| `cli-hub install <name>` | Install a harness |
| `cli-hub update <name>` | Update an installed harness |
| `cli-hub uninstall <name>` | Remove an installed harness |
| `cli-hub launch <name> [args...]` | Run an installed harness |

Some harnesses require upstream software such as Blender, GIMP, Chrome/Edge,
DOMShell, Playwright, or a specific service account. Check `cli-hub info` and the
harness README before use.

## Important distinction: harness vs agent-native browser

For issue #193-style requests, be precise about the layer:

- A **harness** exposes a known workflow as commands (`search`, `read`, `draft`,
  `submit`) once the underlying app/browser can be controlled reliably.
- An **agent-native browser** is the lower substrate that makes arbitrary pages
  observable and actionable for agents: stable page state, handles/semantic tree,
  browser sessions, screenshots/traces, action execution, and safety controls.

LingTai should not pretend that a single generated harness can solve arbitrary
authenticated websites. The practical path is to study or integrate an
agent-native browser/browser-agent substrate, then build narrow harnesses on top
of it for repeated workflows.

## Open-source projects to review

When the human asks for agent-native browser projects, start with these candidates
(read their READMEs and licenses before recommending adoption):

| Project | What it is useful for | Notes |
|---|---|---|
| [`browser-use/browser-use`](https://github.com/browser-use/browser-use) | Python library to make websites accessible to AI agents using browser automation. | Strong general-purpose browser-agent reference; Playwright-oriented. |
| [`browserbase/stagehand`](https://github.com/browserbase/stagehand) | SDK for browser agents combining code and natural-language browser actions. | Good for production-ish browser workflows where code plus AI fallbacks are both useful. |
| [`Skyvern-AI/skyvern`](https://github.com/Skyvern-AI/skyvern) | AI browser workflow automation using LLMs/computer vision. | Closer to workflow automation/RPA for websites; useful comparison for action reliability. |
| [`browseros-ai/BrowserOS`](https://github.com/browseros-ai/BrowserOS) | Open-source agentic browser. | More “browser product” shaped than a harness; useful when the problem is the browser substrate itself. |
| [`steel-dev/steel-browser`](https://github.com/steel-dev/steel-browser) | Open-source browser API/sandbox for AI agents and apps. | Useful if the need is managed browser infrastructure/API rather than per-site CLI. |
| [`apireno/DOMShell`](https://github.com/apireno/DOMShell) | Maps Chrome accessibility/browser state toward a shell/MCP-like interface. | Relevant to filesystem/handle-addressable browser control; CLI-Anything has a `browser` harness using DOMShell. |
| [`lavague-ai/LaVague`](https://github.com/lavague-ai/LaVague) | Large Action Model framework for AI web agents. | Older but still conceptually useful for natural-language-to-browser-action pipelines. |
| [`HKUDS/CLI-Anything`](https://github.com/HKUDS/CLI-Anything) | Ecosystem for agent-native CLI harnesses and CLI-Hub discovery. | Best treated as the harness/workflow layer; not by itself a full agent-native browser. |

Use these as references, not as a blanket endorsement. For each concrete task,
verify current maintenance state, install model, runtime requirements, licenses,
security posture, and whether authenticated sessions are handled safely.

## Relationship to web-browsing

Use `web-browsing` first for **reading**: fetch, extract, scrape, search,
JavaScript-render, or inspect a page without side effects. Use this CLI-Anything
reference for **acting/wrapping**: authenticated workflows, browser-based CLIs,
site-specific harnesses, command interfaces for repeated manual steps, and any
workflow that could submit, modify, send, delete, buy, or otherwise mutate
external state.

This cross-reference is intentional: `web-browsing` is the read/extract/search
router; Swiss Knife owns small tool/harness references. Agents should move
between the two based on the read-vs-act boundary, not just on whether a browser
is involved.

## Relevance to browser/authenticated-site work

CLI-Anything is relevant to raw browser-site issues because its registry already
contains web/browser-oriented examples, including:

- `browser` — browser automation via DOMShell MCP, mapping Chrome's Accessibility
  Tree to a virtual filesystem for navigation.
- `web-yu-pri` — an example of a site-specific browser workflow harness for a
  real web service, including login/inspection/screenshot/dry-run style tasks.
- other web CLIs in the registry, such as browser/search/form-oriented tools.

That means LingTai does **not** need to invent the entire idea from scratch. For
an issue asking for “browser-based CLIs for websites with no API”, the practical
LingTai response is usually:

1. Add/use this swiss-knife skill.
2. Search CLI-Hub for an existing harness.
3. If none exists, create a scoped site-specific harness following the safety
   contract below.
4. Record usage in a LingTai skill so future agents can call the CLI correctly.

## New harness safety contract

For a new site or app harness, require these before action-taking use:

- **Manifest / README:** name, target site/app, prerequisites, install commands,
  command list, and risk mode for each command.
- **Modes:** classify commands as `read_only`, `draft_only`,
  `approve_before_action`, or explicitly opt-in `full_action`.
- **Structured output:** commands should support JSON output.
- **No secret handling:** do not store or print raw credentials/session cookies.
- **Audit log:** append command, timestamp, redacted args, target identifiers,
  result status, and screenshots/traces where useful.
- **Idempotency / duplicate protection:** especially for send/submit/update
  commands.
- **Tests:** at least command parsing, output schema, dry-run behavior, and one
  fixture/mocked workflow where possible.
- **Human-readable skill:** write or update a LingTai skill that tells agents
  exactly when and how to use the harness.

## Suggested LingTai workflow

When asked to use CLI-Anything for a task:

1. **Acknowledge** the human and state that install/account actions require
   explicit approval.
2. **Inspect** the upstream repo/registry read-only.
3. **Search** for an existing harness by app/site/domain.
4. **Report fit:** direct fit, partial fit, or no fit.
5. If direct fit and approved, install in an isolated venv/worktree and run a
   harmless `--help` / read-only command first.
6. If no fit, propose a narrow harness scope: commands, modes, prerequisites,
   test plan, and audit log format.
7. Only after approval, implement the harness and corresponding LingTai skill.

## Example answer pattern

```text
CLI-Anything is a good fit as the harness layer, not as a magic full solution.
I found/ did not find an existing CLI-Hub harness for <site>. If we proceed, I
recommend starting with read-only commands:

- <site>-cli inspect-session
- <site>-cli search ... --json
- <site>-cli read-item <id> --json

Action commands such as send/update/submit should be draft-only or
approve-before-action until the workflow has tests and an audit log.
```

## Notes for issue #193-style requests

For requests like “Let LingTai build and operate browser-based CLIs for any
website”, do **not** claim the issue is solved by installing CLI-Anything alone.
CLI-Anything provides the right ecosystem/pattern, but each authenticated site
still needs review, a scoped harness, user-owned session handling, and safety
mode design.

A small first PR should therefore add this skill and document the operational
workflow. A later PR can add a concrete harness for one low-risk site or demo.
