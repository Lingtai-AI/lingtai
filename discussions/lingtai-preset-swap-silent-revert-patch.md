# Lingtai Preset Swap Silently Reverts — `api_compat` Lost + Cascading AED Bug

**Date:** 2026-05-19
**Repo:** upstream `lingtai` Python package (installed at
`~/.lingtai-tui/runtime/venv/lib/python3.13/site-packages/`, **not** the TUI
repo). All file paths in this document are inside that installed package,
not this workspace.

## TL;DR

Two independent bugs in lingtai kernel combine into a single, very confusing
user-visible failure:

> "I tell my agent `system(action='refresh', preset='~/.../GLM5.1.json')`, the
> tool returns success, the agent restarts cleanly, but the next moment it's
> running on Qwen3.6_Plus again. Multiple retries — always reverts."

The agent did switch. The new process ran 3 LLM calls successfully on the new
preset. But every one of those 3 calls raised
`'str' object has no attribute 'choices'`, AED tripped its threshold, and
`preset_auto_fallback` silently rolled the agent back to its default preset.
The user only sees "switched, then reverted".

**Bug A (root cause):** `lingtai/cli.py` and `lingtai/agent.py` construct
`LLMService` with a `provider_defaults` dict that omits `api_compat`. The
custom-provider adapter factory in `lingtai/llm/_register.py:_custom` reads
`api_compat` *from `provider_defaults`* (not from `manifest.llm`) and falls
back to `"openai"`. A custom provider configured with
`api_compat="anthropic"` (e.g. local GLM proxy speaking the Anthropic
Messages API) gets silently routed to `OpenAIAdapter`, which accesses
`raw.choices` on the response and explodes with
`'str' object has no attribute 'choices'`.

**Bug B (the second-order amplifier):** `lingtai_kernel/base_agent/turn.py:147`
calls `agent._chat.has_pending_tool_calls()` instead of
`agent._chat.interface.has_pending_tool_calls()`. `has_pending_tool_calls`
lives on `ChatInterface`, not on `ChatSession`. When Bug A's error fires AED
recovery, the persistence helper inside the recovery branch raises a second
`AttributeError`, corrupting the AED counter logic and ensuring fallback
triggers on the first round-trip.

Either bug alone is bad. Together they make the symptom unrecognizable.

---

## Forensic timeline (one real reproduction)

Agent: `baigu`, working dir `.lingtai/baigu`. Provider config in
`init.json#manifest.llm` after switching to GLM5.1 preset:

```json
{
  "api_compat": "anthropic",
  "api_key_env": "CUSTOM_6_API_KEY",
  "base_url": "http://127.0.0.1:34891",
  "model": "GLM-5.1",
  "provider": "custom"
}
```

`init.json#manifest.preset.default = "~/.../Qwen3.6_Plus.json"` (the agent's
fallback). Events excerpted from `logs/events.jsonl`:

```
08:28:46.128  preset_swap_started      preset=GLM5.1.json reason="切换长期配置"
08:28:46.128  refresh_requested
08:28:46.128  refresh_start                       ← _activate_preset has already
                                                    persisted init.json
08:28:46.218  refresh_deferred_relaunch           ← watcher subprocess spawned
08:28:47.744  refresh_watcher_relaunch            ← new agent process launched
08:28:48.261  refresh_start                       ← new process _setup_from_init
08:28:48.266  capability_skipped       cap=library reason="Unknown capability"
08:28:48.293  capability_skipped       cap=vision  reason="no vision support for
                                                          provider 'custom'"
08:28:48.311  refresh_complete
08:28:48.825  llm_call                 model=GLM-5.1     ← preset DID switch
08:28:48.977  aed_attempt   attempt=1  error="'str' object has no attribute 'choices'"
08:28:48.977  llm_call                 model=GLM-5.1
08:28:48.994  aed_attempt   attempt=2  error="'str' object has no attribute 'choices'"
08:28:48.996  llm_call                 model=GLM-5.1
08:28:49.010  aed_attempt   attempt=3  error="'str' object has no attribute 'choices'"
08:28:49.010  preset_auto_fallback     reason="...choices..." failed_attempts=3
08:28:49.011  refresh_start                       ← AUTO-FALLBACK fires refresh
                                                    with preset=default
08:28:50.450  refresh_start                       ← yet another process boots
08:28:50.888  llm_call                 model=qwen3.6-plus  ← reverted
```

Net effect from the user's vantage point: 4.7s elapse between
`preset_swap_started(GLM5.1)` and `llm_call(qwen3.6-plus)`. The user pings the
agent half a minute later and gets a Qwen response. `init.json` on disk has
`manifest.preset.active = "...Qwen3.6_Plus.json"`. The kernel did exactly
what it was designed to do — it's the failed `'str' object` calls that look
like a mystery.

A separate isolation test reproducing `_activate_preset` against
`baigu/init.json` confirmed the write path itself is sound — the file does
get rewritten to GLM5.1. The reversion happens *because of* the LLM failure,
not because the write was lost.

---

## Bug A — `api_compat` is silently dropped when LLMService is built

### Root cause

`lingtai/llm/_register.py:_custom` (factory for `provider == "custom"`):

```python
def _custom(*, model=None, defaults=None, **kw):
    from .custom.adapter import create_custom_adapter
    kw.pop("model", None)
    compat = defaults.get("api_compat", "openai") if defaults else "openai"
    return create_custom_adapter(api_compat=compat,
                                 **{k: v for k, v in kw.items() if v is not None})
```

The factory consults `defaults["api_compat"]` and defaults to `"openai"` if
absent. `defaults` here is the per-provider dict from
`LLMService._provider_defaults`, **not** the `manifest.llm` block read from
`init.json`. The two are different things and somebody has to bridge them.
The bridging happens in two places, and **both bridges drop `api_compat`**:

`lingtai/cli.py` (initial boot path), around line 95:

```python
max_rpm = m.get("max_rpm", 60)
provider_key = llm["provider"].lower()
per_provider: dict = {}
if max_rpm > 0:
    per_provider["max_rpm"] = max_rpm
user_headers = llm.get("default_headers")
if isinstance(user_headers, dict) and user_headers:
    per_provider["default_headers"] = dict(user_headers)
# ↑ only max_rpm and default_headers. api_compat is on llm but never copied.
provider_defaults: dict | None = {provider_key: per_provider} if per_provider else None
service = LLMService(
    provider=llm["provider"], model=llm["model"],
    api_key=api_key, base_url=llm.get("base_url"),
    context_window=m.get("context_limit", 200_000),
    provider_defaults=provider_defaults,
)
```

`lingtai/agent.py` (`_setup_from_init` rebuild path), around line 1043:

```python
new_max_rpm = m.get("max_rpm", 60)
new_provider_key = new_provider.lower()
new_per_provider: dict = {}
if new_max_rpm > 0:
    new_per_provider["max_rpm"] = new_max_rpm
new_user_headers = llm.get("default_headers")
if isinstance(new_user_headers, dict) and new_user_headers:
    new_per_provider["default_headers"] = dict(new_user_headers)
# ↑ same omission — api_compat from manifest.llm is read nowhere here.
new_provider_defaults: dict | None = (
    {new_provider_key: new_per_provider} if new_per_provider else None
)
```

When the test agent's preset is materialized into `manifest.llm` with
`api_compat: "anthropic"`, the rebuilt `LLMService._provider_defaults["custom"]`
contains only `{"max_rpm": 60}`. `_custom(defaults=...)` doesn't see
`api_compat`, defaults to `"openai"`, and `create_custom_adapter` dispatches
to `OpenAIAdapter`. The Anthropic Messages-shaped response (content blocks,
no `.choices`) then surfaces inside `openai/adapter.py:241`
(`choice = raw.choices[0]`) as `'str' object has no attribute 'choices'`.

Subtle additional rule: `lingtai/llm/openai/defaults.py` declares
`"api_compat": "openai"` and `lingtai/llm/anthropic/defaults.py` declares
`"api_compat": "anthropic"`. These per-package defaults aren't relevant for
`provider="custom"` — the custom adapter's dispatch is what matters here, and
it has no per-package defaults file to consult; it leans entirely on the
`api_compat` propagated through `provider_defaults`.

### Why nobody noticed before

- The built-in providers (`provider="anthropic"`, `provider="openai"`, etc.)
  each have their own factory in `_register.py` that doesn't consult
  `api_compat` at all — they're hardcoded to their wire protocol. The
  silent-drop only bites `provider="custom"` with a non-default
  `api_compat`.
- The most common `provider="custom"` usage is OpenAI-compatible proxies
  (vLLM, OpenRouter, opencode.ai's Zen passthrough). `api_compat="openai"`
  is the silent fallback, so those work by accident.
- Anthropic-compat custom providers are a niche: local GLM proxies, Bedrock
  routed through a custom base_url, etc.

### Fix

`lingtai/cli.py`, after the `default_headers` block:

```python
user_headers = llm.get("default_headers")
if isinstance(user_headers, dict) and user_headers:
    per_provider["default_headers"] = dict(user_headers)
# api_compat is consulted by the custom adapter factory
# (lingtai/llm/_register.py:_custom) to dispatch between
# OpenAI/Anthropic/Gemini wire protocols. Without this,
# custom providers with api_compat="anthropic" silently fall
# back to "openai" and the OpenAI adapter chokes on the
# response shape (raw.choices vs response.content[]),
# surfacing as `'str' object has no attribute 'choices'`
# followed by preset_auto_fallback reverting the preset.
api_compat = llm.get("api_compat")
if api_compat:
    per_provider["api_compat"] = api_compat
```

`lingtai/agent.py`, mirror change with `new_` prefix in the
`_setup_from_init` rebuild block (around line 1043).

That's the whole fix for Bug A.

### Suggested follow-ups (out of scope for this patch)

1. Move the `api_compat` lookup into the bridging step, but more
   structurally — extract a single helper that converts `manifest.llm` into
   `provider_defaults`, used by both `cli.py` and `agent.py`. Right now the
   two sites duplicate the logic and drifted out of sync historically.
2. Have `_custom` raise (not silently default) when `api_compat` is missing
   but the provider name isn't in the known-OpenAI-compatible alias list.
   The current silent default is the kind of leniency that hides this exact
   class of bug.
3. Consider populating a fixed safelist of `manifest.llm` keys into
   `provider_defaults` rather than enumerating the ones we happen to care
   about. Anything in `manifest.llm` that an adapter factory might consult
   should flow through; an opt-in safelist (api_compat, timeout_ms,
   default_headers, max_rpm, …) is safer than the current
   enumerate-per-site approach.

---

## Bug B — `agent._chat.has_pending_tool_calls()` missing `.interface`

### Root cause

`lingtai_kernel/base_agent/turn.py:147` (inside
`_persist_tool_results_on_continuation_failure`):

```python
if (
    agent._chat is None
    or not agent._chat.has_pending_tool_calls()   # ← AttributeError
):
    return
agent._chat.commit_tool_results(tool_results)
```

`has_pending_tool_calls` is defined on `ChatInterface`
(`lingtai_kernel/llm/interface.py`), not on `ChatSession`
(`lingtai_kernel/llm/base.py`). The other four sites in turn.py and across
the kernel that call this method all go through `.interface`:

- `turn.py:179`  — `agent._chat.interface.has_pending_tool_calls()` ✓
- `turn.py:593` — `iface = agent._chat.interface; iface.has_pending_tool_calls()` ✓
- `base_agent/__init__.py:1003`  — `iface.has_pending_tool_calls()` ✓
- `intrinsics/psyche/_molt.py`   — `iface_pre = agent._chat.interface` ✓

So line 147 is a typo, not a design choice.

It's only reached when a previous LLM call has dangling unanswered tool_calls
AND the next call fails AND AED tries to persist the tool_results before
retry. In quiet operation it never fires. In Bug A's failure mode it fires
on every AED attempt.

### Fix

```python
if (
    agent._chat is None
    or not agent._chat.interface.has_pending_tool_calls()
):
    return
```

Single-character intent fix. Optionally add a test that exercises the AED
retry path on a session with pending tool_calls — it would have caught this
on day one.

### Why this matters even after Bug A is fixed

Bug B isn't only "bad in combination with Bug A". Any transient LLM error
(rate-limit, socket reset, upstream timeout) mid-tool-result-persist will
trip the same `AttributeError`, AED will spend its budget on bogus retries,
and the agent ends up in `STUCK` or auto-fallback for the wrong reason.

---

## Validation

After patching both files, restart the agent on the GLM5.1 preset and watch
events:

```
09:14:49  refresh_complete
09:14:50  agent_state              idle → active (tc_wake)
09:14:50  llm_call                 model=GLM-5.1
09:15:05  llm_response                              ← 15s, anthropic adapter,
                                                      no choices access, no AED
09:15:05  tool_call                tool=email
09:15:05  tool_result              status=ok
09:15:05  llm_call                 model=GLM-5.1
09:15:18  llm_response                              ← turn-2 also clean
09:15:18  tool_call                tool=email (sent)
09:15:18  mail_sent
09:15:18  llm_call                 model=GLM-5.1
09:15:28  llm_response                              ← turn-3
09:15:28  diary
09:15:28  agent_state              active → idle
```

`.agent.json` after the run:

```json
{
  "state": "idle",
  "started_at": "2026-05-19T01:14:49Z",
  "llm": {
    "model": "GLM-5.1",
    "base_url": "http://127.0.0.1:34891",
    "api_compat": "anthropic"
  }
}
```

Three full LLM round-trips with tool calling on the anthropic adapter, zero
`aed_attempt`, zero `preset_auto_fallback`. The preset stays put.

---

## Adjacent issues observed but not addressed by this patch

These showed up in the same investigation but are independent — flagging
for follow-up:

1. **`capability_skipped: library`** during refresh — the GLM5.1 preset
   declares a `library` capability that the kernel's capability registry
   doesn't know. Preset is older than the current capability set, or the
   `library` umbrella was renamed/removed without a migration. Harmless at
   runtime (cap is skipped), but every preset using `library` will warn on
   every boot.
2. **`capability_skipped: vision` with reason "no vision support for provider
   'custom'"** — the vision capability's provider safelist doesn't include
   `custom`. For custom providers that *do* support vision (most
   OpenAI-compat proxies pass `image_url` through), this means the agent
   silently has no vision tool. Either widen the safelist or make vision's
   provider check key off `api_compat` instead of `provider`.
3. **`agent.log` rotation across refresh** — the file handler points at the
   pre-refresh path and the new process doesn't pick it up. New events go to
   `events.jsonl` correctly but `agent.log` (the Python-level WARNING/ERROR
   stream) ends up frozen at the moment of refresh. Made this
   investigation harder than it should have been.
4. **No watcher on a manually-launched agent** — after `_perform_refresh`
   relaunches via its short-lived watcher script, the watcher exits and the
   new agent has nobody monitoring it. A subsequent `kill -9` of the agent
   leaves no autorestart. Probably intentional (the agent isn't a system
   service), but worth a one-line note in the lifecycle docs because the
   asymmetry surprised me during this investigation.

---

## Files touched by this patch

| Path (inside installed lingtai package) | Lines | Change |
|---|---|---|
| `lingtai/cli.py` | ~99–104 → ~99–113 | Append `api_compat` to `per_provider` |
| `lingtai/agent.py` | ~1043–1048 → ~1043–1056 | Mirror change for refresh path |
| `lingtai_kernel/base_agent/turn.py` | 147 | Add `.interface` |

Backups left at `*.bak-bug1-api-compat` and the pre-existing `.bak` for
turn.py.
