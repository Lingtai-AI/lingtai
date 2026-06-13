# ctrl+o API-call spacing hotfix — 2026-06-12

## Problem

After rebuilding the local TUI at `0511a89`, Jason noticed ctrl+o verbose output still ran diary/internal entries together. The intended grouping is by LLM API round-trip: entries from the same `api_call_id` should stay together, and a new `api_call_id` should start after a blank line.

PR #323 only assigned/rendered API-call grouping for `text_output` and tool entries, leaving `thinking`, `diary`, and `text_input` outside the derived grouping stream.

## Fix

- `MailModel.buildMessages` now derives `ApiCallID` for `thinking`, `diary`, and `text_input`, in addition to `text_output`, `tool_call`, and `tool_result`.
- `renderMessages` now tracks one previous visible API-grouped verbose entry across all ctrl+o internal/tool event types.
- New helper `apiCallGroupSeparatorBefore` inserts a blank line only when consecutive visible verbose entries move between different non-empty `api_call_id` values.
- Mixed entries from the same API call (`diary` + `text_output` + `tool_call` + `tool_result`) remain contiguous.
- Legacy tool streams without metadata retain the old `tool_result` → `tool_call` fallback separator.
- `ANATOMY.md` now describes the unified ctrl+o grouping rule.

## Validation

- Focused tests:
  - diary entries from different API-call groups separate;
  - mixed verbose entries from the same API call do not get spurious separators;
  - mixed verbose entries across API calls separate;
  - `buildMessages` assigns derived API ids to `diary`, `thinking`, and `text_input`;
  - existing text-output/tool grouping tests still pass.
- `cd tui && go test ./internal/tui -run 'TestRenderMessages_InsertsBlankLineBetweenDiaryApiCallGroups|TestRenderMessages_KeepsMixedVerboseEntriesInSameApiCallGroup|TestRenderMessages_SeparatesMixedVerboseApiCallGroups|TestBuildMessagesAssignsApiCallIDToDiaryThinkingAndTextInput|TestRenderMessages_InsertsBlankLineBetweenTextOutputApiCallGroups|TestRenderMessages_DoesNotSeparateSameTextOutputApiCallGroup|TestRenderMessages_InsertsBlankLineBetweenApiCallGroups|TestRenderMessages_DoesNotSeparateSameApiCallGroup' -count=1`
- `cd tui && go test ./internal/tui -count=1`
- `git diff --check`
