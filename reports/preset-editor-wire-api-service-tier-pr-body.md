## Summary

- Add an `API mode` row to the preset editor for `wire_api` (`auto`, `chat_completions`, `responses`).
- Replace the raw service-tier row with a `Fast mode` default/fast control available across providers.
- Preserve existing non-empty provider-specific `service_tier` values on unrelated saves while still letting the explicit default toggle remove the field.
- Update English, Chinese, and wen i18n strings, preset editor tests, and TUI anatomy.

## Motivation

This exposes the kernel-side OpenAI-compatible provider controls needed for Codex subscription routing through sub2api/intermediate OpenAI-compatible providers, without requiring manual JSON preset edits for Responses-only endpoints or fast-tier request behavior.

Issue: N/A — direct PR authorized; no tracking issue filed.

## Validation

- `git diff --check`
- `go test ./internal/tui -run 'TestPresetEditor'`
- `make build` (`lingtai-tui v0.10.5-21-gf976aecb`)
- `go test ./...` was also run; it failed only at `TestStartOAuthFlow_LoopbackCallbackCompletesLegacyBrowserFlow` with a 3s OAuth session-message timeout, matching the earlier observed full-suite failure outside the preset-editor surface.

## Local explainer

- `reports/preset-editor-wire-api-service-tier-20260709.html`

## Notes / risks

- Selecting `auto` deletes `wire_api` rather than persisting `wire_api="auto"`, keeping saved presets minimal.
- Existing non-standard `service_tier` values are preserved but displayed as the default radio state until the user changes the row.
- No push/open PR has been performed in local prep.
