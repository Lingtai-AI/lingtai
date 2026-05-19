# Vision capability silently swallows custom-provider config — three layered bugs + one preset-design footgun

**Date:** 2026-05-19
**Repo:** upstream `lingtai` Python package + `lingtai_kernel`, both installed at
`~/.lingtai-tui/runtime/venv/lib/python3.13/site-packages/`. Not in the
`Lingtai-AI/lingtai` TUI repo itself.

## TL;DR

Trying to wire vision for an agent whose main LLM is a custom anthropic-compat
proxy (`provider="custom"`, `api_compat="anthropic"`, e.g. JoyCode local
proxy serving GLM-5.1) takes four turns of debugging across three files
because four separate things go wrong in sequence:

1. **Bug C-1** — `lingtai/capabilities/vision/__init__.py:setup()`'s fallback
   path reads `defaults.get("api_compat")` against the outer
   `_provider_defaults` dict, but that dict is shaped
   `{provider_name: defaults_dict}` — the outer key is the provider name
   ("custom"), not "api_compat". The lookup always returns `None` and the
   fallback never sees the agent's real wire protocol.

2. **Bug C-2** — Even after C-1 is fixed, only `api_compat == "openai"` has a
   fallback branch. Anthropic-compat custom providers fall through to
   `capability_skipped`. (Independent of whether the upstream actually
   supports vision; failure should be at the API call, not at capability
   registration.)

3. **Bug C-3** — The fallback hard-binds vision to the main LLM's
   `service._model` and `service._base_url`. There's no way for a user to
   say "main LLM stays on text-only model X via proxy Y, but route vision
   through vision-capable model Z via the same proxy" without forking the
   `inherit` mechanism. The clean override channel — capability kwargs in
   init.json — is silently ignored by the fallback path.

4. **Footgun D** — `lingtai_kernel/presets.py:materialize_active_preset` does
   a wholesale replace of `manifest.capabilities` from the preset every time
   `_read_init` runs (i.e. every refresh, every boot). Users who edit
   `init.json` directly to override a single capability for one agent
   discover their edit silently vanishes on the next refresh. The preset has
   no surface area for per-agent overrides.

5. **Cherry on top — Bug G** — Once C/D are worked around (by editing the
   preset directly with explicit vision config), `OpenAIVisionService.analyze_image`
   in `lingtai/services/vision/openai.py` blindly accesses `raw.choices` on
   the response. When the openai SDK returns a `str` (which it does silently
   when the proxy hands back non-JSON for any reason — see the companion
   JoyCodeProxy issue), the failure message is the mystifying
   `'str' object has no attribute 'choices'`, with no hint that the
   underlying HTTP body was actually HTML or a plain-text error.

This is what made the original report ("GLM5.1 supports vision, why doesn't
it work in lingtai") so hard to triangulate — every layer turned what was
actually a clear failure into a misleading one.

---

## Reproduction (real path I walked, May 19 2026)

Setup: agent `baigu` working on a financial trading workspace, originally
on a `Qwen3.6_Plus.json` preset, switched to a new `GLM5.1.json` preset
that I authored. `GLM5.1.json#manifest.llm` is

```json
{
  "provider": "custom",
  "api_compat": "anthropic",
  "model": "GLM-5.1",
  "api_key_env": "CUSTOM_6_API_KEY",
  "base_url": "http://127.0.0.1:34891"
}
```

(a local JoyCodeProxy speaking Anthropic Messages API on behalf of JoyCode's
GLM-5.1 endpoint), and `GLM5.1.json#manifest.capabilities.vision` is the
inherit sentinel:

```json
{"provider": "inherit"}
```

Every refresh produced `capability_skipped` events:

```
ts=…  capability=vision  requested_provider=custom
       reason="no vision support for provider 'custom'"
```

### Bug C-1 — `defaults.get("api_compat")` reads the wrong dict layer

`lingtai/capabilities/vision/__init__.py` lines ~100-130 (before fix):

```python
if provider not in PROVIDERS["providers"]:
    # No dedicated VisionService for this provider. If the agent's
    # main LLM is OpenAI-compatible … route vision through
    # OpenAIVisionService using the LLM's own base_url.
    api_compat = ""
    defaults = getattr(getattr(agent, "service", None), "_provider_defaults", None)
    if isinstance(defaults, dict):
        api_compat = defaults.get("api_compat") or ""   # ← WRONG LAYER
    if api_compat == "openai":
        from ...services.vision.openai import OpenAIVisionService
        …
    else:
        agent._log("capability_skipped", capability="vision",
                   requested_provider=provider,
                   reason=f"no vision support for provider {provider!r}")
        return None
```

`LLMService._provider_defaults` is shaped
`{provider_name: {api_compat: …, max_rpm: …, default_headers: …}}`. The
outer key is the provider name (e.g. `"custom"`), not `"api_compat"`. So
`defaults.get("api_compat")` always returns `None`, the openai branch is
never taken, and every custom provider falls through to
`capability_skipped` regardless of its actual `api_compat`.

### Bug C-2 — Only the OpenAI branch exists

Same code block — there's exactly one `if api_compat == "openai":` arm and
no anthropic / gemini equivalents. `AnthropicVisionService` already exists
under `lingtai/services/vision/anthropic.py`, it just isn't wired in. Same
for `GeminiVisionService`. The fallback is effectively `openai-only`,
masquerading as `api_compat-aware`.

### Bug C-3 — Capability kwargs are ignored, agent service is the only source

The fallback path reads:

```python
llm_base_url = getattr(agent.service, "_base_url", None)
llm_model = getattr(agent.service, "_model", None) or "gpt-4o"
```

There's no `kwargs.get("base_url")` or `kwargs.get("model")` consulted
first. So even if I write in `init.json#manifest.capabilities.vision`

```json
{
  "provider": "custom",
  "api_compat": "openai",
  "model": "Kimi-K2.6",
  "base_url": "http://127.0.0.1:34891/v1",
  "api_key_env": "CUSTOM_6_API_KEY"
}
```

the fallback substitutes the main LLM's model (GLM-5.1, the one I'm
specifically trying NOT to use for vision because it isn't vision-capable
on this proxy) and base_url. The capability kwargs are read by
`_resolve_capabilities` and propagated correctly all the way to
`setup_capability(**cap_kwargs)` — but the vision setup function doesn't
look at them. The whole point of the explicit kwargs is to override the
main LLM, and the fallback ignores them.

### Fix — `vision/__init__.py:setup`

```python
if provider not in PROVIDERS["providers"]:
    # Resolution order for api_compat / model / base_url:
    #   1. capability kwargs (explicit user override in init.json)
    #   2. main LLM via service._provider_defaults / ._base_url / ._model
    # This lets the capability point at a different vision-capable
    # model (e.g. Kimi-K2.6 on a multi-model proxy) while the main LLM
    # stays on a text-only model (e.g. GLM-5.1).
    api_compat = (kwargs.get("api_compat") or "").lower()
    if not api_compat:
        defaults = getattr(getattr(agent, "service", None), "_provider_defaults", None)
        if isinstance(defaults, dict):
            # _provider_defaults is dict[provider_name, defaults_dict];
            # peek into the bucket for *this* provider, not the outer dict.
            bucket = defaults.get((provider or "").lower())
            if isinstance(bucket, dict):
                api_compat = (bucket.get("api_compat") or "").lower()

    cap_model    = kwargs.get("model")
    cap_base_url = kwargs.get("base_url")
    llm_base_url = cap_base_url or getattr(agent.service, "_base_url", None)
    llm_model    = cap_model    or getattr(agent.service, "_model", None)

    if api_compat == "openai":
        from ...services.vision.openai import OpenAIVisionService
        vision_service = OpenAIVisionService(
            api_key=api_key,
            model=llm_model or "gpt-4o",
            base_url=llm_base_url,
        )
    elif api_compat == "anthropic":
        from ...services.vision.anthropic import AnthropicVisionService
        # NOTE: AnthropicVisionService.__init__ currently lacks a base_url
        # parameter; needs an additive change there to fully support
        # anthropic-compat custom proxies. See sibling section.
        vision_service = AnthropicVisionService(
            api_key=api_key,
            model=llm_model or "claude-sonnet-4-20250514",
            # base_url=llm_base_url,  # uncomment after the service signature is widened
        )
    else:
        agent._log(
            "capability_skipped",
            capability="vision",
            requested_provider=provider,
            reason=f"no vision support for provider {provider!r} (api_compat={api_compat!r})",
        )
        return None
```

The reason-string change (adding `(api_compat=…)`) is operationally important
on its own — without it I'd have spent another hour wondering whether
`api_compat` was even being computed. The original message was
indistinguishable between "provider not in safelist" and "fallback failed
because api_compat was wrong" cases.

### Sibling fix — `lingtai/services/vision/anthropic.py`

Once Bug C-2 is fixed, anthropic-compat custom providers need a
`base_url` parameter to be useful (the SDK defaults to api.anthropic.com,
which is wrong for any local proxy). The existing `OpenAIVisionService`
already takes `base_url`; mirror its signature:

```python
def __init__(
    self,
    *,
    api_key: str,
    model: str = "claude-sonnet-4-20250514",
    base_url: str | None = None,   # ← new
    max_tokens: int = 1024,
) -> None:
    import anthropic as _anthropic
    client_kwargs: dict = {"api_key": api_key}
    if base_url:
        client_kwargs["base_url"] = base_url
    self._client = _anthropic.Anthropic(**client_kwargs)
    self._model = model
    self._max_tokens = max_tokens
```

---

## Bug D — `materialize_active_preset` wholesale-replaces capabilities

`lingtai_kernel/presets.py:289` `materialize_active_preset` is called by
`agent.py:_read_init` (line 854) on every refresh and boot. It substitutes
`preset.manifest.llm` and `preset.manifest.capabilities` into the loaded
`init.json` data wholesale (with the documented carve-out for
`skills.paths`, which gets a merge).

This means: **a user who edits `init.json` to override one capability for a
single agent has their edit silently reverted on the next refresh.** The
"carve-out for skills.paths" pattern shows the maintainers already noticed
the wholesale-replace is too aggressive for at least one capability — but
there is no general mechanism. Vision is exactly the kind of capability
that benefits from per-agent override: same preset, different vision
provider for different agents.

This is most surprising because the rest of the lifecycle treats
`init.json` as the source of truth. `_setup_from_init` reads it, validates
it, runs capabilities from it. A user reasonably expects to be able to
edit it. The hidden preset-replay step is invisible from the user's
perspective.

### Suggested fixes (escalating)

1. **Minimum**: document the wholesale-replace behavior loudly. Right now
   the docstring of `materialize_active_preset` describes it as "substitute
   the active preset's llm + capabilities into init.json data" — true but
   doesn't emphasize that this happens *every refresh*, *overwriting user
   edits*. A `_log("preset_capabilities_overwritten", added=[…], removed=[…])`
   event on every materialize would also help diagnostics enormously.

2. **Better**: extend the `skills.paths` carve-out pattern. Add an
   `init.json#manifest.capability_overrides` block that gets merged ON TOP
   of preset-materialized capabilities. Vision changes for a single agent
   live there, survive preset swaps.

3. **Best**: structural — split the "preset capabilities" from the
   "materialized capabilities" so that init.json never gets the preset's
   capabilities written into it; instead `_read_init` returns a synthesized
   manifest that holds preset capabilities ∪ init.json overrides, without
   ever mutating the on-disk init.json. Refresh-time edits to init.json
   would then have predictable, scoped effects.

### Sibling small bug — `expand_inherit` drops `api_compat`

`lingtai_kernel/presets.py:expand_inherit` (line 446) materializes
`{provider: "inherit"}` capability configs by copying main_llm's
`provider`, `api_key`, `api_key_env`, `base_url`. It does NOT copy
`api_compat`. So a capability with `provider: "inherit"` that lands in a
fallback branch still loses the anthropic/openai signal, even if the main
LLM has it set correctly. Trivial one-line addition:

```python
kwargs["api_compat"] = main_llm.get("api_compat")
```

---

## Bug G — `OpenAIVisionService.analyze_image` blindly trusts response shape

`lingtai/services/vision/openai.py:analyze_image` ends with:

```python
raw = self._client.chat.completions.create(model=…, messages=…, max_tokens=…)
if raw.choices:
    return raw.choices[0].message.content or ""
return ""
```

When the proxy on the other end is misconfigured (and there are many ways
to misconfigure a local relay — see the companion JoyCodeProxy issue), the
openai SDK can return a raw `str` instead of a `ChatCompletion`. This is
the case if the response body is non-JSON, e.g. an HTML page from a
catch-all SPA route. The SDK does not raise; it returns the body as
string.

The user-visible failure is

```
Vision analysis failed: 'str' object has no attribute 'choices'
```

…which is technically accurate but tells the user nothing. The body is
HTML; that's the symptom that should surface. Suggested fix:

```python
raw = self._client.chat.completions.create(model=…, messages=…, max_tokens=…)
if not hasattr(raw, "choices"):
    # SDK returned raw body — almost always means the proxy/upstream
    # served non-JSON (HTML SPA route, plain-text error, gateway page).
    # Surface the first 200 chars so the user can see what they got.
    snippet = repr(raw)[:200] if isinstance(raw, str) else f"<{type(raw).__name__}>"
    raise RuntimeError(
        f"vision upstream did not return a JSON ChatCompletion. "
        f"Got {type(raw).__name__}: {snippet}. "
        f"Common cause: base_url missing the '/v1' suffix on a local proxy, "
        f"or the proxy returning an HTML dashboard for unknown routes."
    )
if raw.choices:
    return raw.choices[0].message.content or ""
return ""
```

Same pattern probably applies to AnthropicVisionService and the other
provider-specific vision services.

---

## Cross-link

The HTML-on-`/chat/completions` failure mode that surfaced as Bug G is
caused by a separate bug in JoyCodeProxy
([vibe-coding-labs/JoyCodeProxy](https://github.com/vibe-coding-labs/JoyCodeProxy)),
whose SPA dashboard route catches `/chat/completions` when the openai SDK
calls without a `/v1` prefix. I'm filing that separately. But even after
that upstream is fixed, the lingtai-side error message stays misleading
unless Bug G is also addressed — the next user who hits any non-JSON
response on a different proxy will get the same `'str' has no attribute
'choices'` mystery.

---

## Validation

After applying the four-line patch in `vision/__init__.py` and updating
the GLM5.1 preset to:

```json
"vision": {
  "provider": "custom",
  "api_compat": "openai",
  "model": "Kimi-K2.6",
  "api_key_env": "CUSTOM_6_API_KEY",
  "base_url": "http://127.0.0.1:34891/v1"
}
```

(JoyCodeProxy has Kimi-K2.6 as a vision-capable model; main LLM stays on
GLM-5.1 for text), baigu's refresh produces:

```
refresh_complete   capabilities=[..., 'vision', ...]
                   tools=[..., 'vision', ...]            ← was missing before
```

No more `capability_skipped: vision` events. End-to-end vision call on a
608KB financial chart PNG returns a 102-char Chinese caption that
correctly identifies the chart's subject, panels, and color-coded
markers. `prompt_tokens=4113` confirms the image was actually processed
by the upstream vision model (not silently dropped). Round-trip 26
seconds.

---

## Files touched

| Path | Lines | Change |
|---|---|---|
| `lingtai/capabilities/vision/__init__.py` | ~102-130 | Resolve api_compat from kwargs first; fall back to `_provider_defaults[provider]["api_compat"]`; honor kwargs `model` + `base_url`; add anthropic branch (commented pending sibling fix); enrich `capability_skipped` reason with `(api_compat=…)` |
| `lingtai/services/vision/anthropic.py` | __init__ | Accept `base_url` (not yet applied — sibling fix; needed for the anthropic branch above to be useful) |
| `lingtai_kernel/presets.py` | `expand_inherit` | Add `kwargs["api_compat"] = main_llm.get("api_compat")` |
| `lingtai/services/vision/openai.py` | `analyze_image` | Raise with diagnostic context when `raw` isn't a `ChatCompletion` |
| (design) `lingtai_kernel/presets.py` + `agent.py` | — | Add a per-agent override channel that survives `materialize_active_preset`, or at minimum log every wholesale-replace of capabilities |
