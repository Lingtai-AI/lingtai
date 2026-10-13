---
name: bot-guide-initiation
description: >
  Use when choosing the directory, ownership, recipe, model/preset, permissions
  and bootstrap route for a new LingTai bot.
version: 1.0.0
last_changed_at: "2026-10-12T18:16:58Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Initiation: decide what is being created

Nested lingtai-bot-guide reference. Start here for a new bot; then load only the
recipe, preset or channel owner needed for the chosen route.

## Establish inputs and authority

- Choose a canonical `PROJECT_DIR` owned by the intended user, an agent name,
  language (`en`, `zh`, `wen`), channel audience and designated maintenance owner.
  Check the target and parent permissions, existing `.lingtai/`, and running
  inventory. A new target is not permission to overwrite unrelated files.
- Obtain explicit authorization for project creation/process launch and separately
  for provider/auth/config writes and external contact. Reusing a token or another
  agent's preset access is a privilege decision, not merely copying files.
- Preserve existing/partial state on failure. Do not retry creation over it,
  delete it, call `clean`/`purge`/`nirvana`, or manually fabricate `.lingtai/`.
  Recovery/deletion requires a separate human decision.

## Choose the current creation surface

| Intent | Existing route and boundary |
|---|---|
| Interactive new TUI project with recipe selection | Run the installed `lingtai-tui` in the selected directory; follow its first-run/setup UI and `lingtai-tui-help`. Recipe composition remains TUI-owned. |
| New headless project bot | `lingtai-tui spawn` through [headless bot](../headless-bot/SKILL.md). It creates **and launches**, selects `plain`, and emits JSON. It is not a custom-recipe selector or an installer. |
| Existing valid kernel agent without the TUI | Use installed kernel CLI help and `system-manual` → `reference/substrate-manual/SKILL.md` (§11). Verify interpreter/config ownership before running. Do not translate TUI recipe flags into imaginary kernel project flags. |
| Persistent specialist inside the existing network | [Avatars](../avatars/SKILL.md), not a new project and not a channel automatically inherited from the parent. |
| Connect a channel to an existing bot | [Channels](../channels/SKILL.md); do not create a second agent merely for another entrance to the same mind. |

The public [kernel repository](https://github.com/Lingtai-AI/lingtai-kernel)
and its installed CLI help own the kernel creation/run surface. This guide does
not promote a cross-repository candidate into a shipped command. TUI source
routes: [headless anatomy](../../../../../headless/ANATOMY.md) and
[process anatomy](../../../../../process/ANATOMY.md).

## Recipe, LLM and permissions are different choices

1. **Recipe** shapes behavior/library, not provider credentials. Discover
   `lingtai-recipe` → `reference/recipe-format/SKILL.md`. Pass the bundle directory
   containing `.recipe/`, not `.recipe/` itself. A live network, the root recipe,
   and the applied TUI snapshot are distinct; recipe export is not an agent or
   network clone. Headless spawn's persisted `plain` receipt does not prove all
   best-effort recipe artifacts were applied.
2. **Preset** is an atomic LLM + capabilities bundle. Discover
   `lingtai-preset-skill`: select the provider-family child (`codex`, `claude`,
   `openai`, `anthropic`) separately from the operation child. Named vendors are
   endpoint/model choices, not extra provider families. The provider owner holds
   auth/login and exact fields; never copy auth files, tokens or account pools.
3. **Policy** is `manifest.preset`: authorized references, not copied preset JSON.
   `active` and `default` must both belong to `allowed`. A new bot should receive
   only the intended set, not an orchestrator's whole set by assumption.
   `reference/operations/saved-presets/SKILL.md` owns template vs saved storage;
   TUI templates are regenerated, user saved presets are user-owned.
4. **Admin** is separate from a channel allowlist and from preset access. A TUI
   project needs one orchestrator; avatars are members, not extra root wills.
   Do not hand-edit `.agent.json` to manufacture admin privileges. Use the current
   creation/schema owner and inspect the published manifest after launch.

## Runtime and service bootstrap

Use `lingtai-update` → `reference/install/SKILL.md` for TUI installation and
`system-manual` → `reference/runtime-update-checks/SKILL.md` for kernel provenance
and installer ownership. `spawn` checks an existing runtime; it does not silently
install/repair it. `lingtai-tui bootstrap` materializes TUI assets; it does not
prove a bot or listener is live. Treat installation and bootstrap as authorized
writes, not read-only discovery.

The kernel agent is an independent long-lived process. Closing the TUI does not
stop it. Do not add `launchd`/cron/systemd supervision just to keep it running;
if a service/reboot policy is actually requested, discover `shell-manual`'s
scheduled-work/async owner before choosing a single supervisor. Detached work
requires a retained receipt and terminal verification, not fire-and-forget.

Next: [channels](../channels/SKILL.md), then [operations](../operations/SKILL.md).
