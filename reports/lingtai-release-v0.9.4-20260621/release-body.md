# LingTai TUI/Portal v0.9.4

Patch release for TUI/Portal metadata rendering after the tool-result/runtime guidance work.

## Highlights

- Ctrl+O compact layer now caps very long one-line tool-call summaries more aggressively.
- `/notification` renders structured metadata blocks for tool/runtime/notification payloads.
- Mail replay and Ctrl+O full layer render structured tool-result metadata blocks without session-ingestion pre-truncation.

## Validation

- `git diff --check` passed.
- Focused and full TUI Go tests passed.
- Release build check printed `lingtai-tui v0.9.4`.
- Portal web install/build passed; portal Go tests passed.

## Notes

- GitHub release carries no manual assets, matching v0.9.3.
- Pushing tag `v0.9.4` should trigger the repository release workflow that updates the Homebrew tap.
- `npm ci` reported existing audit warnings; no new mitigation was attempted in this release.
