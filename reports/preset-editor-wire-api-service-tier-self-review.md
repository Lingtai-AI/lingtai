# Local self-review: preset editor API mode controls

Branch: `fix/preset-editor-wire-api-service-tier`
Commit: `757c9e54`
Base: `origin/main@f976aecb`

## Readiness gate

- Clean diff: `git diff --check` passed.
- Targeted tests: `go test ./internal/tui -run 'TestPresetEditor'` passed.
- Build: `make build` passed; binary reports `lingtai-tui v0.10.5-21-gf976aecb`.
- Broader suite: `go test ./...` failed only at `TestStartOAuthFlow_LoopbackCallbackCompletesLegacyBrowserFlow` with a 3s OAuth session-message timeout, matching the earlier observed failure outside this change surface.
- Anatomy: updated `tui/internal/tui/ANATOMY.md` for API mode and service_tier rows.
- Secrets: no credentials or private token material added.

## Findings

Confirmed and fixed during self-review:
- Initial TUI behavior would have deleted non-`fast` provider-specific `service_tier` values on unrelated saves. Fixed by preserving non-blank existing values while retaining explicit normal-toggle deletion.

Residual risks to mention in PR:
- Non-standard `service_tier` values are preserved but displayed as normal in the simplified radio strip until the user changes the row.
- Selecting API mode `auto` deletes `wire_api` rather than persisting `wire_api="auto"`.
- Full TUI test suite still has the OAuth loopback timing failure noted above.

## Decision

Ready for maintainer review once an issue/PR target is confirmed. No push/open PR performed.
