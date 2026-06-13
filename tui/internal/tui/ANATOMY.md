# tui/internal/tui — Bubble Tea screens

> **Maintenance:** see `lingtai-tui-anatomy` (at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`).

This is the ~20k LOC Bubble Tea v2 package that renders every screen of `lingtai-tui`. Each screen is a struct implementing `Init()`, `Update(msg)`, `View()`, living in its own `.go` file. The package is intentionally flat — breaking it into sub-packages would fight Bubble Tea's convention (every model must be the same `tea.Model` type for the dispatcher), and Go's single-type-per-package constraint makes this the grain that matches the framework.

Screen routing is centralized in the `App` struct (`app.go`), which holds every screen as a field, dispatches commands via `switchToView`, and maps the slash-command palette (`/mail`, `/setup`, `/doctor`, `/daemons`, `/notification`, `/goal`, etc.) to view transitions or narrow file-protocol actions.

## Components

### Root model and dispatcher

- **`app.go:22-43`** — `appView` enum: 17 view constants (`appViewFirstRun` through `appViewHelp`), including `appViewNotification` for `/notification`.
- **`app.go:46-79`** — `App` struct: holds every screen model plus routing state (`currentView`, `orchDir`, `orchName`, `recoveryMode`).
- **`app.go:91-177`** — `NewApp`: constructor deciding initial view — mail view (returning user), first-run wizard (new user or rehydration), or recovery mode (global config lost, agents intact).
- **`app.go:179-187`** — `App.Init()`: delegates to the initial view's `Init()`.
- **`app.go:191-561`** — `App.Update()`: the central dispatcher. Three layers: (1) `WindowSizeMsg` forwarded to current view, (2) cross-view messages (`ViewChangeMsg`, `FirstRunDoneMsg`, `SetupSavedMsg`, `NirvanaDoneMsg`, `AddonSavedMsg`, etc.), (3) `KeyPressMsg` for `ctrl+c`/`q` quit, (4) fallthrough to current view's `Update()`.
- **`app.go:563-1170`** — `handlePaletteCommand`: maps slash-command strings to view transitions (`/doctor` → `appViewDoctor`, `/daemons` → `appViewDaemons`, `/notification` → `appViewNotification`, `/knowledge` → `appViewCodex` (canonical; hidden `/library` and `/codex` aliases), `/skills` → `appViewLibrary`, etc.) and direct actions (`/suspend`, `/cpr`, `/refresh`, `/clear`, `/molt`, `/btw`, `/goal`, `/export`).
- **`app.go:1211-1302`** — `switchToView(viewName string)`:  the canonical route-to-view dispatcher used by `ViewChangeMsg` and palette commands returning to a view. Reconstructs models fresh on entry.
- **`app.go:1304-1356`** — `App.View()`:  delegates to current view's `View()`, wraps in `tea.NewView` with alt-screen + mouse mode.
- **`app.go:1347-1529`** — portal launch, style helpers, `SetTUIVersion`.

### Screens

- **`firstrun.go:135-2004`** (4285 lines) — `FirstRunModel`. Multi-step wizard with constructors for three flows: `NewFirstRunModel` (new project, `firstrun.go:305`), `NewSetupModeModel` (reconfiguring an existing agent, `firstrun.go:479`), `NewRehydrateModel` (cloned network, `firstrun.go:579`). Its background bootstrap calls `config.EnsureRuntimeQuiet`, so first-run venv creation is followed by the same non-blocking Python `lingtai` upgrade check as returning-user startup. Steps (`firstRunStep` enum, `firstrun.go:62-77`): Welcome → API Key → Pick Preset → Edit Preset → Preset Key → Capabilities → Agent Presets → Agent Name/Dir → Recipe → (optional) Rehydrate Propagate → Launching. Emits `FirstRunDoneMsg` when complete.
- **`mail.go:103-1228`** — `MailModel`. The network home screen — async message thread, adaptive compose input, slash-command palette, heartbeat pulse, agent state, and project-level activity badge. Constructor: `NewMailModel` (`mail.go:164`). Refresh/poll ticks, message history pagination, and the `/human` compose path that writes to `human/mailbox/outbox/`.
- **`props.go:22-1057`** (1057 lines) — `PropsModel` (KANBAN). Agent dashboard: status, heartbeat, token ledger visualization, selected-agent daemon run counts, network activity, session history, signal controls. Constructor: `NewPropsModel` (`props.go:59`).
- **`library.go:615-989`** — `LibraryModel`. Skill catalog browser, agent-scoped — scans `<agent>/.library/` plus all effective `skills.paths`: `readLibraryPaths` (`library.go:270`) prefers the kernel-published resolved-manifest artifact `<agent>/system/manifest.resolved.json` (kernel issue #259) and falls back to raw `init.json` when the artifact is absent or malformed (stopped / never-booted agents). Constructor: `NewLibraryModel` (`library.go:650`). Renders skill metadata in a sidebar list with markdown content pane.
- **`doctor.go:49-757`** — `DoctorModel`. Health check screen. First runs `config.RunDoctorUpdate` (forced TUI + Python upgrade) and refreshes `preset.Bootstrap`, utility skills, and `ExportCommandsJSON`; then continues with the traditional diagnostics — version drift, heartbeat stats, capability validation, Python venv path, kernel CLI probe, LLM reachability. Constructor: `NewDoctorModel` (`doctor.go:58`). The same forced-update routine is reachable from the shell as `lingtai-tui doctor`, useful when the TUI cannot start.
- **`preset_editor.go:244-1691`** — `PresetEditorModel`. Full preset editing form (LLM provider/model, API key, capabilities on/off, model parameters). Constructor: `NewPresetEditorModel` (`preset_editor.go:317`). Used by both the standalone `/presets` flow and the first-run wizard's stepEditPreset.
- **`preset_library.go:101-593`** — `PresetLibraryModel`. Preset browser: list templates/saved, create new, import. Constructor: `NewPresetLibraryModel` (`preset_library.go:120`). Wired to `/presets`.
- **`settings.go:72-553`** — `SettingsModel`. TUI preferences: theme, language, mail page size, agent default language, insights toggle, tool-call display limit (`tool_truncate`; "off" = full content, the default). Constructor: `NewSettingsModel` (`settings.go:87`). Wired to `/settings`.
- **`addon.go:21-164`** — `AddonModel`. The `/mcp` control panel — a read-only view of each MCP bridge's config and status (IMAP, Telegram, Feishu, WeChat). Configs live at `<lingtaiDir>/.addons/<name>/config.json`. Constructor: `NewAddonModel` (`addon.go:34`). Wired to `/mcp` (the Go type retains the historical `Addon*` naming; the `/addon` slash-command was retired by PR #204 and `TestDefaultCommandsDoesNotKeepAddonAlias` enforces it stays gone).
- **`login.go:52-475`** — `LoginModel`. OAuth flows (Codex, Anthropic API key login). Constructor: `NewLoginModel` (`login.go:88`). Wired to `/login`.
- **`system.go:117-380`** — `SystemModel`. Agent filesystem browser: init.json, .agent.json, pad.md, system prompt files, logs. Constructor: `NewSystemModel` (`system.go:138`). Wired to `/system`.
- **`codex.go:18-283`** — `CodexModel`. The `/knowledge` view — agent private knowledge browser. Scans each agent's `knowledge/<name>/KNOWLEDGE.md` folder layout via `buildAgentCodexEntries` (`codex_entries.go:36`); legacy `codex/codex.json` / `knowledge/knowledge.json` stores are read only via a one-time migration into the folder layout (`codex_entries.go:13-16`, `:40`). Constructor: `NewCodexModel` (`codex.go:41`). Wired to `/knowledge` (canonical) with hidden `/library` and `/codex` aliases.
- **`mailbox.go:18-295`** — `MailboxModel`. Per-agent mail folder browser (inbox/sent/archive). Constructor: `NewMailboxModel` (`mailbox.go:46`). Wired to `/mailbox`.
- **`daemons.go:30-928`** — `DaemonsModel`. Read-only daemon run browser for `/daemons`: discovers agents with `fs.BuildNetwork`, opens a Ctrl+T agent picker, scans `<agent>/daemons/em-*/daemon.json`, `logs/events.jsonl`, `history/chat_history.jsonl`, and `result.txt`, then renders a left run list and right detail pane with task, metadata, full chat_history interactions, full event/tool records, and full result text. The panes are independently scrollable: ↑↓/jk keeps selecting daemon runs while tab/←/→ changes which pane receives page/mouse scroll, and selection changes reset only the detail pane while keeping the selected list row visible. Constructor: `NewDaemonsModel` (`daemons.go:113`). Wired to `/daemons`.
- **`notification.go:18-194`** — `NotificationModel`. Read-only `/notification` view over the current agent's `<agent>/.notification/*.json` files: renders an aggregate raw notification block plus one MarkdownViewer entry per channel file, with `r` to reload. Constructor: `NewNotificationModel` (`notification.go:156`).
- **`goal.go:14-62`** — `/goal` filesystem writer. `writeGoalRequestNotification` appends a `source="goal.request"` event to the current agent's `.notification/system.json`, preserving existing `data.events`, capping at 20, and writing via temp-file + rename. The event asks the agent to read the goal manual, guide objective/criteria/reminder/cancel semantics with the human, and only then create `.notification/goal.json` after confirmation.
- **`nirvana.go:47-209`** — `NirvanaModel`. Confirmation screen for wiping `.lingtai/`. Constructor: `NewNirvanaModel` (`nirvana.go:56`). Emits `NirvanaDoneMsg` → triggers first-run wizard flow.
- **`projects.go:35-448`** — `ProjectsModel`. Global project list browser. Constructor: `NewProjectsModel` (`projects.go:50`). Wired to `/projects`.
- **`mdviewer.go:40-518`** — `MarkdownViewerModel`. Generic markdown display with sidebar navigation. Used by `/skills` detail views, recipe previews, and `/help`.
- **`help.go:1-79`** — `HelpModel`. Thin `MarkdownViewerModel` wrapper for `/help`. No embedded help docs of its own: it reads the slash-command guide from the bundled `lingtai-tui-help` skill via `preset.ReadBundledSkillFile`, picking the asset for the current UI language (`i18n.Lang()` → `assets/slash-commands.<lang>.md`, English fallback). Constructor: `NewHelpModel` (`help.go:66`). Wired to `/help`.
- **`setup.go:42-242`** — `SetupModel` (legacy). Older `/setup` form (API key, preset selection). Constructor: `NewSetupModel` (`setup.go:54`). Mostly subsumed by `firstrun.go`'s setup mode, kept for the recovery path.

### Non-screen shared types and helpers

- **`input.go:28-294`** — `InputModel`. Reusable compose widget with textarea, paste support, and multiline expand. Used by `MailModel`.
- **`palette.go:28-232`** — `PaletteModel`. Slash-command palette widget (type `/` to trigger, `/help` lists commands). Used by `MailModel` and `SettingsModel`; `DefaultCommands` includes `/notification` and `/goal` (`palette.go:60-61`).
- **`styles.go:1-471`** — Theme system: `Theme` type, `ActiveTheme()`, `SetThemeByName()`, `Color*` constants, `themedTextareaStyles()`, lipgloss rendering helpers.
- **`codex_entries.go:13-88`** — `buildAgentCodexEntries`: scans `knowledge/<name>/KNOWLEDGE.md` folders (after a one-time migration of legacy `codex/codex.json` / `knowledge/knowledge.json` stores via `migrateLegacyJSONStores`), converts to `MarkdownEntry` slices for the `CodexModel`.
- **`mailbox_entries.go:17-321`** — `buildMailboxEntries`: reads per-agent mailbox folders, converts to `MarkdownEntry` slices for the `MailboxModel`.
- **`daemons.go:514-798`** — daemon artifact readers: `loadDaemonSummaries`, `readDaemonSummary`, event/chat/result full-file readers, and small rendering helpers for full daemon detail output.
- **`recipe_entries.go:14-96`** — `buildRecipeEntries`: scans recipe directories for markdown files (greet, comment, covenant, procedures, skills).
- **`recipe_save.go:14-202`** — Recipe save helpers: `recipeUsesCustomDir`, `sourceBundleDir`, `saveCustomRecipe`, `ApplyRecipeToAgent`.
- **`skill_files.go:1-188`** — `SkillFilesModel`: embedded sub-model for browsing `.library/` skill directories within the wizard.
- **`wizard_footer.go:16-61`** — `renderWizardFooter`: Back/Next button row shared by all wizard pages.
- **`detect.go:1-158`** — `IsOrchestrator`, `DetectOrchestrators`, `ExportCommandsJSON`, `ValidateCodexAuthOnStartup`, `SubstituteGreetPlaceholders`. Utility functions called by `main.go`.
- **`lock_unix.go` / `lock_windows.go`** — `tryLock`: platform-specific file locking for agent suspend/restart coordination.

## Connections

- **Called from:** `tui/main.go:354` creates the `App` via `tui.NewApp(...)` and wraps it in `tea.NewProgram`.
- **Calls out (read):** `tui/internal/fs/` (agent state, heartbeat, mail, session, signal, network), `tui/internal/preset/` (load/save/apply presets, recipes, bootstrap, utility skills), `tui/internal/config/` (global config, venv, upgrade checks), `tui/internal/process/` (agent launch), `tui/internal/migrate/` (addon comment detection), `tui/i18n/` (all screen strings).
- **Calls out (write):** signal files (`.sleep`, `.suspend`, `.interrupt`, `.clear`, `.prompt`, `.refresh`, `.inquiry`, `.forget`), `init.json` via `preset.GenerateInitJSON`, human outbox via `fs.WriteOutboxMessage`, `.lingtai/.tui-asset/settings.json` (per-project settings).
- **Cross-view messages:** `ViewChangeMsg` (routes between screens), `FirstRunDoneMsg` (wizard → launch agent → mail view), `NirvanaDoneMsg` (wipe complete → wizard), `SetupSavedMsg` (setup complete → propagate config → mail view), `AddonSavedMsg`, `MarkdownViewerCloseMsg`.
- **Palette dispatch:** `PaletteSelectMsg` from the `PaletteModel` carries a slash-command string; `App.Update()` maps it to `handlePaletteCommand`.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`)
- **Subfolders:** none — the package is intentionally flat.
- **Siblings in `tui/internal/`:** `preset/`, `migrate/`, `globalmigrate/`, `fs/`, `config/`, `process/`, `postman/`, `timemachine/`.
- **File count:** 35 `.go` files (20 screen models + supporting types + helpers).

## State

- **Writes:** per-project `settings.json` (orchestrator selection, mail page size, theme, language). Signal files on agent directories. `init.json` rewrites during setup/preset edits.
- **Reads:** agent working directories (`.agent.json`, `.agent.heartbeat`, `init.json`, `knowledge/<name>/KNOWLEDGE.md` (legacy `codex/codex.json` / `knowledge/knowledge.json` only via one-time migration), `mailbox/`, `logs/token_ledger.jsonl`, `history/chat_history.jsonl`, `system/*.md`, `.library/`). Global config (`~/.lingtai-tui/config.json`, `presets/`, `runtime/`).
- **Ephemeral:** `App.currentView`, `App.startupBanner`, `App.recoveryMode`. All screens maintain local cursor positions, scroll offsets, and input buffers — lost on process exit (Bubble Tea is stateless across launches).

## Notes

- **~19k LOC in one package is deliberate, not a refactor debt.** Bubble Tea's `tea.Model` interface requires every model to be the same Go interface type. Splitting screens into sub-packages would either (a) require import cycles (the root model dispatches to sub-package models, but sub-package models emit cross-view messages consumed by the root) or (b) require an interface-indirection layer that fights Bubble Tea's convention. The lint is correct here: this is the grain that fits the framework.
- **Screen-per-file is the convention, not a strict rule.** `firstrun.go` (4150 lines) combines the wizard with embedded sub-models (preset editor, skill files browser). `firstrun.go` is the largest single file because the wizard is a modal flow where steps share internal state (selected preset, agent name, recipe) that's harder to split cleanly.
- **All screens are eagerly constructed in `App` as zero-value fields** — only the active view gets a `New*Model()` call. When switching views, `switchToView` reconstructs the model fresh.
- **Paste delivery requires forwarding `tea.PasteMsg`** alongside `tea.KeyPressMsg`. The `InputModel` handles this; any text widget embedded in another model must forward paste messages in its host's `Update()`.
- **`textarea` over `textinput`** for paste-friendly fields (API keys, base URLs). Apply `themedTextareaStyles()` from `styles.go` — bare `textarea.New()` renders a dark cursor that clashes with the warm theme.
- **Mail-view chat replay: verbosity gate + render switch.** `MailModel.verbose` (`mail.go:76-80`) is a 3-level enum (`verboseOff` → `verboseThinking` → `verboseExtended`) cycled by `ctrl+o`. Two coupled switches gate every rendered event: `shouldShow` (`mail.go:~370`) decides which `SessionEntry` types are visible at the current level, and `renderMessages` (`mail.go:~820`) maps each `ChatMessage.Type` to a styled block. `verboseThinking` shows the agent's inner state and kernel diagnostics — `thinking`, `diary`, `text_input`, `text_output`, `soul_flow`, `notification`, `aed`. Verbose LLM-round-trip entries (`thinking`/`diary`/`text_input`/`text_output`, plus `tool_call`/`tool_result` in extended mode) inherit the current `api_call_id` and are visually separated when that id changes (`apiCallGroupSeparatorBefore` in `toolcall_display.go`), so separate API responses do not run together in ctrl+o while mixed entries from the same API call stay grouped. `verboseExtended` adds raw `tool_call` / `tool_result` — each prefixed with a faint local `15:04` timestamp (`formatToolTimestamp`) and truncated per the user's `tool_call_truncate` setting (`truncateToolBody`, both in `toolcall_display.go`; the default of 0 shows full content). Legacy tool entries without grouping metadata keep the old `tool_result`→`tool_call` fallback (`toolGroupSeparatorBefore`). `fs/session.go` carries the full tool args verbatim — truncation is render-time only, driven by `MailModel.toolCallTruncate`. Soul flow and notification share the green palette (`ColorAccent` bold header, `ColorAgent` italic body indented 4) — they're agent-side reflections. AED uses the orange `ColorTool` palette to mark kernel distress (LLM empty-response retries, recovery timeouts) so it stands out from normal inner state. To add a new event type to the chat replay, extend `fs/session.go`'s `parseEvent` (allow-list + body extractor), then both `shouldShow` and `renderMessages` here.

- `doctor_intrinsic.go`: `/doctor` shells out through the runtime venv to the kernel `lingtai-doctor` intrinsic script for per-agent state/MCP/log/notification diagnostics.
