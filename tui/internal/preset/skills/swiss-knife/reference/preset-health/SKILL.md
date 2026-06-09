---
name: preset-health
description: >
  Nested swiss-knife reference for saved-preset health checks. Read this when
  the human asks whether saved presets still work, which preset is expired or
  misconfigured, why `system(action="presets")` shows a bad connectivity
  status, or for a safe procedure to enumerate saved presets, classify each
  failure (expired key, missing credentials, unreachable endpoint, invalid
  model/config, connectivity failure), and report results without leaking
  secrets. This is a READ-ONLY diagnostic workflow: it never edits a preset or
  an agent `init.json` on its own — it produces an actionable report and tells
  the human exactly which one-line change to confirm.
version: 1.0.0
tags: [presets, health-check, diagnostics, read-only, connectivity]
---

# Preset Health Check (read-only)

A repeatable, read-only procedure for testing whether saved presets are healthy,
classifying any failures, and producing an actionable report — without printing
or mutating credentials.

> **Read-only contract.** This reference enumerates, probes, and reports. It does
> **not** edit `~/.lingtai-tui/presets/saved/*.json`, agent `init.json`, or any
> `manifest.preset.allowed` list. Remediation is described as a recommendation
> for the human to confirm and apply (or to explicitly authorize you to apply
> through the normal preset tooling). Never run a "fix" step automatically.

## When to use

- The human asks "are my presets still working?" or "which preset is broken?"
- `system(action="presets")` surfaced a non-`ok` connectivity status (for
  example `no_credentials`, an auth failure, or an unreachable endpoint).
- A provider rotated keys, retired a model, or moved an endpoint and you need to
  find every saved preset affected.
- A new agent needs a clear procedure to triage presets before relying on them.

## Where saved presets live

Saved (user-owned) presets are JSON files here:

```
~/.lingtai-tui/presets/saved/*.json
```

Built-in templates live under `~/.lingtai-tui/presets/templates/` and are
rewritten on every `lingtai-tui bootstrap` — the directory itself is the marker
distinguishing built-in from user-owned, so a health check focuses on
`saved/`. The skill tree this reference ships in is extracted to
`~/.lingtai-tui/utilities/swiss-knife/reference/preset-health/SKILL.md`.

Each preset's relevant shape (other fields omitted):

```json
{
  "manifest": {
    "llm": {
      "provider": "gemini",
      "model": "gemini-2.0-flash",
      "base_url": "https://...",
      "api_key_env": "GEMINI_API_KEY"
    }
  }
}
```

The credential is referenced indirectly through `api_key_env` (an environment
variable **name**), not stored inline. That indirection is what lets a health
check report "credential missing" without ever reading the secret value.

## Step 1 — Enumerate saved presets

Prefer the structured listing the runtime already exposes:

```bash
lingtai-tui presets --saved --json
```

This emits one entry per preset (`name`, `description`, `source`, `path`). If the
CLI is unavailable, list the directory directly (read-only):

```bash
ls -1 ~/.lingtai-tui/presets/saved/*.json 2>/dev/null
```

For each preset, read only the non-secret config fields you need to classify it:
`manifest.llm.provider`, `manifest.llm.model`, `manifest.llm.base_url`, and the
**name** `manifest.llm.api_key_env`. Do not read or echo the value of the env
var named by `api_key_env`.

## Step 2 — Check credential presence (no secret values)

A preset that declares `api_key_env` needs that variable set in the agent's
resolved environment. Check **presence only**:

```bash
# Reports set / not-set WITHOUT revealing the value.
env_name="GEMINI_API_KEY"
if [ -n "${!env_name+x}" ] && [ -n "${!env_name}" ]; then
  echo "$env_name: set"
else
  echo "$env_name: NOT set"
fi
```

The runtime resolves credentials the same way internally: a preset with a
non-empty `api_key_env` is considered to have a usable credential only when that
variable is non-empty (Codex is the documented exception — it authenticates via
ChatGPT OAuth and declares no `api_key_env`). A "not set" result maps to the
`no_credentials` class below — this is exactly the case the issue saw with
`gemini-test.json` reporting `GEMINI_API_KEY not set in environment`.

## Step 3 — Probe connectivity (read-only)

Reuse the live check the TUI already performs rather than inventing a new probe.
`/doctor` runs the canonical LLM connectivity probe against the **active** agent's
configuration; run it (or read the most recent doctor output) to see the real
state. The doctor probe distinguishes these outcomes, which are the source of
truth for classification:

| Doctor probe outcome | Meaning |
|---|---|
| ok | endpoint reachable, auth accepted, response envelope non-empty |
| auth error | endpoint reachable but credential rejected (401/403, expired/invalid key) |
| no key | no credential configured for a preset that needs one |
| oauth | OAuth-style auth pending/needed (e.g. Codex) |
| network error | endpoint unreachable (DNS/connect/TLS/timeout) |
| rate limit / overloaded | reachable and authed, transiently throttled |
| empty response | endpoint replied but with an empty/invalid envelope (often a proxy) |

To probe a **saved preset that is not the active one** without mutating
anything, do not edit the agent to switch presets. Instead report what static
inspection (Steps 1–2) shows, and recommend the human refresh onto that preset
in a throwaway/dev agent if a live probe is required. Switching the active
preset is a configuration change and is out of scope for this read-only skill.

## Step 4 — Classify each preset

Map findings to a fixed taxonomy so reports are consistent:

| Class | Trigger | Typical doctor outcome |
|---|---|---|
| `ok` | reachable, authed, valid model | ok |
| `no_credentials` | `api_key_env` set but variable empty/unset | no key |
| `auth_failed` / `expired_key` | credential present but rejected (401/403) | auth error |
| `endpoint_unreachable` | DNS/connect/TLS/timeout to `base_url` | network error |
| `model_not_found` | auth ok but model rejected/unknown (404 model, "model not found") | auth error/unknown w/ model detail |
| `config_error` | malformed preset JSON, missing `provider`/`model`, invalid `base_url` | varies (often unknown/empty) |
| `rate_limited` | reachable + authed but throttled (transient) | rate limit / overloaded |
| `connectivity_failure` | reachable but empty/invalid response envelope | empty response |
| `unknown` | none of the above | default/unknown |

Notes:
- `model_not_found` vs `auth_failed`: read the error **detail** — an
  authentication message points to credentials; a "model" message points to the
  `model` field. When the provider conflates them, report both as candidates.
- `config_error` is a static finding (bad JSON, empty `provider`/`model`, a
  `base_url` that is not a valid URL) and needs no network call. The runtime's
  own validation requires a non-empty `summary`, a `tier` in 1..5, and non-empty
  `llm.provider`/`llm.model`; a preset failing those is `config_error`.
- A transient class (`rate_limited`) should be retried before being reported as a
  hard failure.

## Step 5 — Report safely (no secrets)

Produce a concise table. Columns: preset name, provider/model, status class,
short error summary, recommended fix.

**Redaction rules (mandatory):**
- Never print API key values, tokens, or full `Authorization` headers.
- Refer to credentials by env-var **name** only (`GEMINI_API_KEY`), never value.
- If a raw error body might embed a key or signed URL, redact it: keep the
  status code and the human-readable message, drop query strings and bearer
  tokens. A safe redactor for ad-hoc error text:

  ```bash
  # Redact bearer tokens, sk-/key-like strings, and URL query strings.
  redact() {
    sed -E \
      -e 's/(Bearer )[A-Za-z0-9._-]+/\1[REDACTED]/g' \
      -e 's/\b(sk|key|tok)[-_][A-Za-z0-9]{6,}/[REDACTED]/g' \
      -e 's/([?&](api[_-]?key|token|key|sig)=)[^&[:space:]]+/\1[REDACTED]/gi'
  }
  ```

Example report shape (illustrative values):

| Preset | Provider / model | Status | Error summary | Recommended fix |
|---|---|---|---|---|
| `gemini-test` | gemini / gemini-2.0-flash | `no_credentials` | `GEMINI_API_KEY not set` | Set `GEMINI_API_KEY` in the agent env, then refresh |
| `minimax_cn` | minimax / abab6.5 | `auth_failed` | `401 invalid api key` | Replace expired `MINIMAX_API_KEY`, or remove preset from this agent's allowed list |
| `kimi` | moonshot / kimi-k2 | `ok` | — | none |

## Step 6 — Remediation guidance (recommend, do not apply)

State the smallest change and let the human confirm it. Common fixes:

- **`no_credentials`** — set the env var named by `api_key_env` in the agent's
  environment (or `env_file`), then `system(action="refresh")` so the agent
  re-resolves config.
- **`auth_failed` / `expired_key`** — rotate the credential (ask the human for a
  replacement key) and refresh. If the preset is simply no longer wanted on this
  agent, recommend removing it from `manifest.preset.allowed` — flag that this is
  a config edit requiring explicit human confirmation before anyone applies it.
- **`endpoint_unreachable`** — verify `base_url`, network/DNS, and provider
  status; do not change the URL speculatively.
- **`model_not_found`** — update `manifest.llm.model` to a currently-served model
  for that provider (human-confirmed).
- **`config_error`** — fix the malformed field; re-validate.

Every one of these is a recommendation. This skill stops at the report — it does
not write to any preset or `init.json`.

## Read-only checklist

- [ ] Listed presets via `presets --json` or a directory read — no writes.
- [ ] Checked credential **presence**, never value.
- [ ] Used `/doctor`'s existing probe for live state; did not switch the active
      preset to probe another one.
- [ ] Classified each preset against the fixed taxonomy.
- [ ] Redacted all secrets and key-bearing URLs in the report.
- [ ] Presented remediation as human-confirmed recommendations only.

---
> **Found a bug or issue?** If you encounter any problems with this skill, load
> the `lingtai-issue-report` skill and follow its instructions to report it.
