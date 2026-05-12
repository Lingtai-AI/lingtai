# tui/internal/tui — Bubble Tea screens

> **Maintenance:** see `lingtai-tui-anatomy` (at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`).

This is the ~19k LOC Bubble Tea v2 package that renders every screen of `lingtai-tui`. Each screen is a struct implementing `Init()`, `Update(msg)`, `View()`, living in its own `.go` file. The package is intentionally flat — breaking it into sub-packages would fight Bubble Tea's convention (every model must be the same `tea.Model` type for the dispatcher), and Go's single-type-per-package constraint makes this the grain that matches the framework.

Screen routing is centralized in the `App` struct (`app.go`), which holds every screen as a field, dispatches commands via `switchToView`, and maps the slash-command palette (`/mail`, `/setup`, `/doctor`, etc.) to view transitions.

## Components

### Root model and dispatcher

- **`app.go:23-43`** — `appView` enum: 15 view constants (`appViewFirstRun` through `appViewPresets`).
- **`app.go:46-82`** — `App` struct: holds every screen model plus routing state (`currentView`, `orchDir`, `orchName`, `recoveryMode`, `inSecretaryView`).
- **`app.go:97-183`** — `NewApp`: constructor deciding initial view — mail view (returning user), first-run wizard (new user or rehydration), or recovery mode (global config lost, agents intact).
- **`app.go:185-193`** — `App.Init()`: delegates to the initial view's `Init()`.
- **`app.go:195-589`** — `App.Update()`: the central dispatcher. Three layers: (1) `WindowSizeMsg` forwarded to current view, (2) cross-view messages (`ViewChangeMsg`, `FirstRunDoneMsg`, `SetupSavedMsg`, `NirvanaDoneMsg`, `AddonSavedMsg`, etc.), (3) `KeyPressMsg` for `ctrl+c`/`q` quit, (4) fallthrough to current view's `Update()`.
- **`app.go:591-956`** — `handlePaletteCommand`: maps slash-command strings to view transitions (`/doctor` → `appViewDoctor`, `/library` → `appViewCodex` (hidden `/codex` alias), `/skills` → `appViewLibrary`, `/secretary` → toggle secretary sub-view, etc.) and direct actions (`/suspend`, `/cpr`, `/refresh`, `/clear`, `/molt`, `/btw`, `/export`).
- **`app.go:1104-1216`** — `switchToView(viewName string)`: the canonical route-to-view dispatcher used by `ViewChangeMsg` and palette commands returning to a view. Reconstructs models fresh on entry.
- **`app.go:1218-1273`** — `App.View()`: delegates to current view's `View()`, wraps in `tea.NewView` with alt-screen + mouse mode.
- **`app.go:1277-1465`** — portal launch, style helpers, `SetTUIVersion`.

### Screens

- **`firstrun.go:135-1989`** (4150 lines) — `FirstRunModel`. Multi-step wizard with constructors for three flows: `NewFirstRunModel` (new project, `firstrun.go:283`), `NewSetupModeModel` (reconfiguring an existing agent, `firstrun.go:452`), `NewRehydrateModel` (cloned network, `firstrun.go:548`). Steps (`firstRunStep` enum, `firstrun.go:63-76`): Welcome → API Key → Pick Preset → Edit Preset → Preset Key → Capabilities → Agent Presets → Agent Name/Dir → Recipe → (optional) Rehydrate Propagate → Launching. Emits `FirstRunDoneMsg` when complete.
- **`mail.go:103-1066`** (1066 lines) — `MailModel`. The network home screen — async message thread, compose input, slash-command palette, heartbeat pulse, and agent status bar. Constructor: `NewMailModel` (`mail.go:146`). Refresh/poll ticks, message history pagination, and the `/human` compose path that writes to `human/mailbox/outbox/`.
- **`props.go:23-1059`** (1059 lines) — `PropsModel` (KANBAN). Agent dashboard: status, heartbeat, token ledger visualization, session history, signal controls. Constructor: `NewPropsModel` (`props.go:60`).
- **`library.go:532-906`** — `LibraryModel`. Skill catalog browser, agent-scoped — scans `<agent>/.library/` plus all `skills.paths` from `init.json`. Constructor: `NewLibraryModel` (`library.go:567`). Renders skill metadata in a sidebar list with markdown content pane.
- **`doctor.go:49-724`** — `DoctorModel`. Health check screen: version drift detection, heartbeat stats, capability validation, Python venv path, MCP registry dump. Constructor: `NewDoctorModel` (`doctor.go:58`).
- **`preset_editor.go:244-1691`** — `PresetEditorModel`. Full preset editing form (LLM provider/model, API key, capabilities on/off, model parameters). Constructor: `NewPresetEditorModel` (`preset_editor.go:317`). Used by both the standalone `/presets` flow and the first-run wizard's stepEditPreset.
- **`preset_library.go:101-593`** — `PresetLibraryModel`. Preset browser: list templates/saved, create new, import. Constructor: `NewPresetLibraryModel` (`preset_library.go:120`). Wired to `/presets`.
- **`settings.go:72-553`** — `SettingsModel`. TUI preferences: theme, language, mail page size, agent default language, insights toggle. Constructor: `NewSettingsModel` (`settings.go:87`). Wired to `/settings`.
- **`addon.go:21-164`** — `AddonModel`. Addon (MCP) configuration screen. Reads/writes `init.json` addon paths. Constructor: `NewAddonModel` (`addon.go:34`). Wired to `/addon`.
- **`login.go:52-475`** — `LoginModel`. OAuth flows (Codex, Anthropic API key login). Constructor: `NewLoginModel` (`login.go:88`). Wired to `/login`.
- **`agora.go:44-404`** — `AgoraModel`. Network sharing: import/export workflows, clone discovery, recipe bundle manager. Constructor: `NewAgoraModel` (`agora.go:67`). Wired to `/agora`.
- **`system.go:117-380`** — `SystemModel`. Agent filesystem browser: init.json, .agent.json, pad.md, system prompt files, logs. Constructor: `NewSystemModel` (`system.go:138`). Wired to `/system`.
- **`codex.go:18-283`** — `CodexModel`. Codex (knowledge archive) browser. Reads `codex/codex.json`, renders entries as markdown. Constructor: `NewCodexModel` (`codex.go:41`). Wired to `/library` (with hidden `/codex` alias).
- **`mailbox.go:18-295`** — `MailboxModel`. Per-agent mail folder browser (inbox/sent/archive). Constructor: `NewMailboxModel` (`mailbox.go:46`). Wired to `/mailbox`.
- **`nirvana.go:47-209`** — `NirvanaModel`. Confirmation screen for wiping `.lingtai/`. Constructor: `NewNirvanaModel` (`nirvana.go:56`). Emits `NirvanaDoneMsg` → triggers first-run wizard flow.
- **`projects.go:35-448`** — `ProjectsModel`. Global project list browser. Constructor: `NewProjectsModel` (`projects.go:50`). Wired to `/projects`.
- **`mdviewer.go:40-518`** — `MarkdownViewerModel`. Generic markdown display with sidebar navigation. Used by `/brief`, `/library` detail views, recipe previews, and agora browse results.
- **`setup.go:42-242`** — `SetupModel` (legacy). Older `/setup` form (API key, preset selection). Constructor: `NewSetupModel` (`setup.go:54`). Mostly subsumed by `firstrun.go`'s setup mode, kept for the recovery path.

### Non-screen shared types and helpers

- **`input.go:26-277`** — `InputModel`. Reusable compose widget with textarea, paste support, and multiline expand. Used by `MailModel`.
- **`palette.go:28-231`** — `PaletteModel`. Slash-command palette widget (type `/` to trigger, `/help` lists commands). Used by `MailModel` and `SettingsModel`.
- **`styles.go:1-471`** — Theme system: `Theme` type, `ActiveTheme()`, `SetThemeByName()`, `Color*` constants, `themedTextareaStyles()`, lipgloss rendering helpers.
- **`codex_entries.go:13-88`** — `buildAgentCodexEntries`: reads `codex/codex.json`, converts to `MarkdownEntry` slices for the `CodexModel`.
- **`mailbox_entries.go:17-321`** — `buildMailboxEntries`: reads per-agent mailbox folders, converts to `MarkdownEntry` slices for the `MailboxModel`.
- **`recipe_entries.go:14-96`** — `buildRecipeEntries`: scans recipe directories for markdown files (greet, comment, covenant, procedures, skills).
- **`recipe_save.go:14-202`** — Recipe save helpers: `recipeUsesCustomDir`, `sourceBundleDir`, `saveCustomRecipe`, `ApplyRecipeToAgent`.
- **`secretary_setup.go:1-224`** — `setupSecretary`: one-shot secretary agent initialization (preset, init.json, launch).
- **`secretary_briefs.go`** — `buildSecretaryBriefs`: collects secretary briefing markdown files.
- **`skill_files.go:1-188`** — `SkillFilesModel`: embedded sub-model for browsing `.library/` skill directories within the wizard.
- **`wizard_footer.go:16-61`** — `renderWizardFooter`: Back/Next button row shared by all wizard pages.
- **`detect.go:1-158`** — `IsOrchestrator`, `DetectOrchestrators`, `ExportCommandsJSON`, `ValidateCodexAuthOnStartup`, `SubstituteGreetPlaceholders`. Utility functions called by `main.go`.
- **`lock_unix.go` / `lock_windows.go`** — `tryLock`: platform-specific file locking for agent suspend/restart coordination.

## Connections

- **Called from:** `tui/main.go:354` creates the `App` via `tui.NewApp(...)` and wraps it in `tea.NewProgram`.
- **Calls out (read):** `tui/internal/fs/` (agent state, heartbeat, mail, session, signal, network), `tui/internal/preset/` (load/save/apply presets, recipes, bootstrap, library), `tui/internal/config/` (global config, venv, upgrade checks), `tui/internal/process/` (agent launch), `tui/internal/migrate/` (addon comment detection), `tui/internal/secretary/` (assistant agent management), `tui/i18n/` (all screen strings).
- **Calls out (write):** signal files (`.sleep`, `.suspend`, `.interrupt`, `.clear`, `.prompt`, `.refresh`, `.inquiry`, `.forget`), `init.json` via `preset.GenerateInitJSON`, human outbox via `fs.WriteOutboxMessage`, `.lingtai/.tui-asset/settings.json` (per-project settings).
- **Cross-view messages:** `ViewChangeMsg` (routes between screens), `FirstRunDoneMsg` (wizard → launch agent → mail view), `NirvanaDoneMsg` (wipe complete → wizard), `SetupSavedMsg` (setup complete → propagate config → mail view), `AddonSavedMsg`, `MarkdownViewerCloseMsg`.
- **Palette dispatch:** `PaletteSelectMsg` from the `PaletteModel` carries a slash-command string; `App.Update()` maps it to `handlePaletteCommand`.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`)
- **Subfolders:** none — the package is intentionally flat.
- **Siblings in `tui/internal/`:** `preset/`, `migrate/`, `globalmigrate/`, `fs/`, `config/`, `process/`, `postman/`, `timemachine/`, `secretary/`.
- **File count:** 37 `.go` files (20 screen models + supporting types + helpers).

## State

- **Writes:** per-project `settings.json` (orchestrator selection, mail page size, theme, language). Signal files on agent directories. `init.json` rewrites during setup/preset edits.
- **Reads:** agent working directories (`.agent.json`, `.agent.heartbeat`, `init.json`, `codex/codex.json`, `mailbox/`, `logs/token_ledger.jsonl`, `history/chat_history.jsonl`, `system/*.md`, `.library/`). Global config (`~/.lingtai-tui/config.json`, `presets/`, `runtime/`).
- **Ephemeral:** `App.currentView`, `App.inSecretaryView`, `App.startupBanner`, `App.recoveryMode`, `App.lastEscTime`. All screens maintain local cursor positions, scroll offsets, and input buffers — lost on process exit (Bubble Tea is stateless across launches).

## Notes

- **~19k LOC in one package is deliberate, not a refactor debt.** Bubble Tea's `tea.Model` interface requires every model to be the same Go interface type. Splitting screens into sub-packages would either (a) require import cycles (the root model dispatches to sub-package models, but sub-package models emit cross-view messages consumed by the root) or (b) require an interface-indirection layer that fights Bubble Tea's convention. The lint is correct here: this is the grain that fits the framework.
- **Screen-per-file is the convention, not a strict rule.** `firstrun.go` (4150 lines) combines the wizard with embedded sub-models (preset editor, skill files browser). `firstrun.go` is the largest single file because the wizard is a modal flow where steps share internal state (selected preset, agent name, recipe) that's harder to split cleanly.
- **All screens are eagerly constructed in `App` as zero-value fields** — only the active view gets a `New*Model()` call. When switching views, `switchToView` reconstructs the model fresh (except `secretaryMail` which is cached).
- **Paste delivery requires forwarding `tea.PasteMsg`** alongside `tea.KeyPressMsg`. The `InputModel` handles this; any text widget embedded in another model must forward paste messages in its host's `Update()`.
- **`textarea` over `textinput`** for paste-friendly fields (API keys, base URLs). Apply `themedTextareaStyles()` from `styles.go` — bare `textarea.New()` renders a dark cursor that clashes with the warm theme.
- **Mail-view chat replay: verbosity gate + render switch.** `MailModel.verbose` (`mail.go:71-77`) is a 3-level enum (`verboseOff` → `verboseThinking` → `verboseExtended`) cycled by `ctrl+o`. Two coupled switches gate every rendered event: `shouldShow` (`mail.go:335`) decides which `SessionEntry` types are visible at the current level, and `renderMessages` (`mail.go:~700`) maps each `ChatMessage.Type` to a styled block. `verboseThinking` shows the agent's inner state and kernel diagnostics — `thinking`, `diary`, `text_input`, `text_output`, `soul_flow`, `notification`, `aed`. `verboseExtended` adds raw `tool_call` / `tool_result`. Soul flow and notification share the green palette (`ColorAccent` bold header, `ColorAgent` italic body indented 4) — they're agent-side reflections. AED uses the orange `ColorTool` palette to mark kernel distress (LLM empty-response retries, recovery timeouts) so it stands out from normal inner state. To add a new event type to the chat replay, extend `fs/session.go`'s `parseEvent` (allow-list + body extractor), then both `shouldShow` and `renderMessages` here.
