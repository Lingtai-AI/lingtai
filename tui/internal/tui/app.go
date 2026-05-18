package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
)

type appView int

const (
	appViewFirstRun appView = iota
	appViewMail
	appViewSetup
	appViewSettings
	appViewProps
	appViewAddon
	appViewDoctor
	appViewNirvana
	appViewLibrary
	appViewProjects
	appViewAgora
	appViewLogin
	appViewCodex
	appViewMailbox
	appViewSystem
	appViewPresets
)

// App is the root Bubble Tea model. Routes between views via slash commands.
type App struct {
	currentView   appView
	mail          MailModel
	setup         SetupModel
	settings      SettingsModel
	props         PropsModel
	library       LibraryModel
	projects      ProjectsModel
	agora         AgoraModel
	codex         CodexModel
	system        SystemModel
	mailbox       MailboxModel
	presetLibrary PresetLibraryModel
	firstRun      FirstRunModel
	addon         AddonModel
	doctor        DoctorModel
	nirvana       NirvanaModel
	login         LoginModel

	globalDir        string
	projectDir       string // .lingtai/ directory
	orchDir          string // full path to orchestrator dir
	orchName         string
	lingtaiCmd       string
	width            int
	height           int
	tuiConfig        config.TUIConfig
	pendingRecipe    string
	pendingCustomDir string
	recoveryMode     bool   // global config lost, agents intact — setup then propagate
	startupBanner    string // non-empty warning shown on first render
}

func humanAddr(projectDir string) string {
	return "human"
}

// NewApp creates the root app model.
// NewApp constructs the top-level TUI app.
//
// rehydrateOrchDir and rehydrateOrchName, when both non-empty, signal that
// the network is a cloned agora network awaiting rehydration. The app
// enters first-run view with a FirstRunModel constructed via
// NewRehydrateModel, which prefills the orchestrator's name/dir and adds
// a final stepPropagate page to copy the new init.json to every worker.
func NewApp(globalDir, projectDir string, needsFirstRun, needsRecovery bool, orchestrators []string, tuiCfg config.TUIConfig, rehydrateOrchDir, rehydrateOrchName string) App {
	// Apply persisted theme (or default).
	SetThemeByName(tuiCfg.Theme)

	lingtaiCmd := config.LingtaiCmd(globalDir)

	app := App{
		globalDir:  globalDir,
		projectDir: projectDir,
		lingtaiCmd: lingtaiCmd,
		tuiConfig:  tuiCfg,
	}

	if needsRecovery && len(orchestrators) > 0 {
		// Global config lost but agents intact — show setup for API keys,
		// then propagate LLM config to all agents and go to mail view.
		orchName := orchestrators[0]
		orchDir := filepath.Join(projectDir, orchName)
		// Check per-project settings for saved orchestrator
		localSettings := LoadSettings(projectDir)
		if localSettings.Orchestrator != "" {
			for _, o := range orchestrators {
				if o == localSettings.Orchestrator {
					orchName = o
					orchDir = filepath.Join(projectDir, o)
					break
				}
			}
		}
		app.orchName = orchName
		app.orchDir = orchDir
		app.recoveryMode = true
		app.currentView = appViewFirstRun
		app.firstRun = NewSetupModeModel(projectDir, globalDir, orchDir, orchName)
	} else if needsFirstRun {
		app.currentView = appViewFirstRun
		hasPresets := preset.HasAny()
		if rehydrateOrchDir != "" && rehydrateOrchName != "" {
			app.firstRun = NewRehydrateModel(projectDir, globalDir, rehydrateOrchDir, rehydrateOrchName, hasPresets)
		} else {
			app.firstRun = NewFirstRunModel(projectDir, globalDir, hasPresets, "")
		}
	} else {
		// Determine orchestrator
		localSettings := LoadSettings(projectDir)
		if len(orchestrators) == 1 {
			app.orchName = orchestrators[0]
			app.orchDir = filepath.Join(projectDir, orchestrators[0])
		} else if len(orchestrators) > 1 {
			// Check saved setting
			if localSettings.Orchestrator != "" {
				// Verify it still exists
				found := false
				for _, o := range orchestrators {
					if o == localSettings.Orchestrator {
						found = true
						break
					}
				}
				if found {
					app.orchName = localSettings.Orchestrator
					app.orchDir = filepath.Join(projectDir, localSettings.Orchestrator)
				}
			}
			// If no saved or stale, use first (app could prompt, but keep simple for now)
			if app.orchName == "" {
				app.orchName = orchestrators[0]
				app.orchDir = filepath.Join(projectDir, orchestrators[0])
				localSettings.Orchestrator = orchestrators[0]
				SaveSettings(projectDir, localSettings)
			}
		}

		app.currentView = appViewMail
		humanDir := filepath.Join(projectDir, "human")
		addr := humanAddr(projectDir)
		app.mail = NewMailModel(humanDir, addr, projectDir, app.orchDir, app.orchName, tuiCfg.MailPageSize, globalDir, tuiCfg.Language, tuiCfg.Insights)

		// Validate codex-auth.json if any agent uses a codex preset.
		if warn := validateCodexAuthForAgents(globalDir, projectDir); warn != "" {
			app.startupBanner = warn
		}

	}

	return app
}

func (a App) Init() tea.Cmd {
	switch a.currentView {
	case appViewFirstRun:
		return a.firstRun.Init()
	case appViewMail:
		return a.mail.Init()
	}
	return nil
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		// Forward to current view so it can resize
		var cmd tea.Cmd
		switch a.currentView {
		case appViewMail:
			a.mail, cmd = a.mail.Update(msg)
		case appViewSetup:
			a.setup, cmd = a.setup.Update(msg)
		case appViewSettings:
			a.settings, cmd = a.settings.Update(msg)
		case appViewProps:
			a.props, cmd = a.props.Update(msg)
		case appViewAddon:
			a.addon, cmd = a.addon.Update(msg)
		case appViewDoctor:
			a.doctor, cmd = a.doctor.Update(msg)
		case appViewNirvana:
			a.nirvana, cmd = a.nirvana.Update(msg)
		case appViewLibrary:
			a.library, cmd = a.library.Update(msg)
		case appViewProjects:
			a.projects, cmd = a.projects.Update(msg)
		case appViewAgora:
			a.agora, cmd = a.agora.Update(msg)
		case appViewFirstRun:
			a.firstRun, cmd = a.firstRun.Update(msg)
		case appViewLogin:
			a.login, cmd = a.login.Update(msg)
		case appViewCodex:
			a.codex, cmd = a.codex.Update(msg)
		case appViewMailbox:
			a.mailbox, cmd = a.mailbox.Update(msg)
		case appViewSystem:
			a.system, cmd = a.system.Update(msg)
		case appViewPresets:
			a.presetLibrary, cmd = a.presetLibrary.Update(msg)
		}
		return a, cmd

	// === Cross-view messages ===

	case ViewChangeMsg:
		return a.switchToView(msg.View)

	case MarkdownViewerCloseMsg:
		// If agora is active, forward to AgoraModel (detail → list, not mail)
		if a.currentView == appViewAgora {
			updated, cmd := a.agora.Update(msg)
			a.agora = updated
			return a, cmd
		}
		a.currentView = appViewMail
		return a, tea.Batch(a.mail.refreshMail, tickEvery(a.mail.pollRate), pulseTick(), a.sendSize())

	case doctorResultMsg:
		if a.currentView == appViewDoctor {
			a.doctor, _ = a.doctor.Update(msg)
		}
		return a, nil

	case loginHealthMsg:
		if a.currentView == appViewLogin {
			a.login, _ = a.login.Update(msg)
		}
		return a, nil

	case CodexOAuthDoneMsg:
		if a.currentView == appViewLogin {
			a.login, _ = a.login.Update(msg)
		} else if a.currentView == appViewFirstRun {
			a.firstRun, _ = a.firstRun.Update(msg)
		}
		return a, nil

	case refreshDoneMsg:
		if msg.err != nil {
			a.mail.AddSystemMessage(i18n.TF("mail.launch_failed", firstLine(msg.err)))
		} else {
			a.mail.AddSystemMessage(i18n.T("mail.refreshed"))
		}
		return a, a.mail.refreshMail

	case refreshAllDoneMsg:
		if len(msg.failures) > 0 {
			a.mail.AddSystemMessage(i18n.TF("mail.refresh_all_with_failures", msg.count-len(msg.failures), len(msg.failures), strings.Join(msg.failures, ", ")))
		} else {
			a.mail.AddSystemMessage(i18n.TF("mail.refresh_all", msg.count))
		}
		return a, a.mail.refreshMail

	case PaletteSelectMsg:
		return a.handlePaletteCommand(msg.Command, msg.Args)

	case FirstRunDoneMsg:
		// First-run complete: launch agent and switch to mail.
		// Reload tuiConfig from disk so any settings the wizard saved
		// (theme, mail page size, insights) are reflected downstream.
		// a.tuiConfig was captured at NewApp time and is otherwise stale
		// after the wizard's SaveTUIConfig calls.
		a.tuiConfig = config.LoadTUIConfig(a.globalDir)
		// Ensure human folder exists before launching — InitProject is
		// idempotent and prevents the race where the agent tries to
		// send mail before the human mailbox is ready.
		if err := process.InitProject(a.projectDir, a.globalDir); err != nil {
			a.currentView = appViewMail
			humanDir := filepath.Join(a.projectDir, "human")
			addr := humanAddr(a.projectDir)
			a.mail = NewMailModel(humanDir, addr, a.projectDir, "", "", a.tuiConfig.MailPageSize, a.globalDir, a.tuiConfig.Language, a.tuiConfig.Insights)
			a.mail.AddSystemMessage(i18n.TF("mail.launch_failed", err))
			return a, tea.Batch(a.mail.Init(), a.sendSize())
		}
		a.orchDir = msg.OrchDir
		a.orchName = msg.OrchName
		// Propagate LLM config to all agents in the network
		PropagateOrchestratorConfig(a.projectDir, a.orchDir)

		// Recipe application: when the project carries a .recipe/ bundle
		// (set by the first-run wizard or imported from a bundle), make
		// sure every agent's .prompt + skills.paths + .tui-asset/.recipe/
		// snapshot are in sync before the agent process boots. This
		// catches the rehydration case: RehydrateNetwork just generated
		// init.json for each imported agent, but .prompt and library
		// registration haven't run yet for this launch. The startup
		// reconciliation in main.go covers subsequent launches, but the
		// very first launch after rehydration needs this hook too.
		if preset.RecipeNeedsApply(a.projectDir) {
			humanDir := filepath.Join(a.projectDir, ".lingtai", "human")
			haddr := "human"
			if humanNode, err := fs.ReadAgent(humanDir); err == nil && humanNode.Address != "" {
				haddr = humanNode.Address
			}
			lang := a.tuiConfig.Language
			if lang == "" {
				lang = "en"
			}
			subst := func(tmpl string) string {
				return SubstituteGreetPlaceholders(tmpl, haddr, humanDir, lang, "120")
			}
			_, _ = preset.ApplyRecipe(a.projectDir, lang, subst)
		}

		// Launch the agent
		var launchErr string
		if a.lingtaiCmd != "" {
			if _, err := process.LaunchAgent(a.lingtaiCmd, a.orchDir); err != nil {
				launchErr = i18n.TF("mail.launch_failed", err)
			}
		}
		// Initialize mail view
		a.currentView = appViewMail
		humanDir := filepath.Join(a.projectDir, "human")
		addr := humanAddr(a.projectDir)
		a.mail = NewMailModel(humanDir, addr, a.projectDir, a.orchDir, a.orchName, a.tuiConfig.MailPageSize, a.globalDir, a.tuiConfig.Language, a.tuiConfig.Insights)

		if launchErr != "" {
			a.mail.messages = append(a.mail.messages, ChatMessage{From: i18n.T("mail.system_sender"), Body: launchErr, Type: "mail"})
		}
		return a, tea.Batch(a.mail.Init(), a.sendSize())

	case RecipeFreshStartMsg:
		a.pendingRecipe = msg.Recipe
		a.pendingCustomDir = msg.CustomDir
		a.currentView = appViewNirvana
		a.nirvana = NewNirvanaModel(a.projectDir)
		return a, tea.Batch(a.nirvana.Init(), a.sendSize())

	case NirvanaDoneMsg:
		// Nirvana complete: .lingtai/ wiped, go to first-run.
		// Re-init project to recreate the human folder so agents can
		// deliver mail once the new orchestrator starts.
		process.InitProject(a.projectDir, a.globalDir)
		a.orchDir = ""
		a.orchName = ""
		a.currentView = appViewFirstRun
		hasPresets := preset.HasAny()
		preselected := a.pendingRecipe
		a.pendingRecipe = ""
		pendingCustom := a.pendingCustomDir
		a.pendingCustomDir = ""
		a.firstRun = NewFirstRunModel(a.projectDir, a.globalDir, hasPresets, preselected)
		if preselected == preset.RecipeCustom && pendingCustom != "" {
			a.firstRun.recipeCustomInput.SetValue(pendingCustom)
		}
		return a, tea.Batch(a.firstRun.Init(), a.sendSize())

	case AddonSavedMsg:
		a.mail.AddSystemMessage(i18n.T("addon.saved"))
		return a.switchToView("mail")

	case SetupSavedMsg:
		if a.recoveryMode {
			// Recovery: global config was lost but agents are intact.
			// Propagate the new LLM + capabilities to all agents, init
			// the mail view, and launch the orchestrator.
			a.recoveryMode = false
			a.tuiConfig = config.LoadTUIConfig(a.globalDir)
			PropagateOrchestratorConfig(a.projectDir, a.orchDir)
			a.currentView = appViewMail
			humanDir := filepath.Join(a.projectDir, "human")
			addr := humanAddr(a.projectDir)
			a.mail = NewMailModel(humanDir, addr, a.projectDir, a.orchDir, a.orchName, a.tuiConfig.MailPageSize, a.globalDir, a.tuiConfig.Language, a.tuiConfig.Insights)
			if a.lingtaiCmd != "" {
				if _, err := process.LaunchAgent(a.lingtaiCmd, a.orchDir); err != nil {
					a.mail.AddSystemMessage(i18n.TF("mail.launch_failed", err))
				}
			}
			return a, tea.Batch(a.mail.Init(), a.sendSize())
		}
		PropagateOrchestratorConfig(a.projectDir, a.orchDir)
		a.mail.AddSystemMessage(i18n.T("setup.saved_refresh"))
		return a.switchToView("mail")

	case SetupDoneMsg:
		// During first-run, forward to firstrun model (needs to create default preset)
		if a.currentView == appViewFirstRun {
			updated, cmd := a.firstRun.Update(msg)
			a.firstRun = updated
			return a, cmd
		}
		return a.switchToView("mail")

	case UsePresetMsg:
		// Create agent from preset
		process.InitProject(a.projectDir, a.globalDir)
		p, err := preset.Load(msg.Name)
		if err != nil {
			return a, nil
		}
		agentName := p.Name
		if err := preset.GenerateInitJSON(p, agentName, agentName, a.projectDir, a.globalDir); err != nil {
			return a, nil
		}
		orchDir := filepath.Join(a.projectDir, agentName)
		var launchErr string
		if a.lingtaiCmd != "" {
			if _, err := process.LaunchAgent(a.lingtaiCmd, orchDir); err != nil {
				launchErr = i18n.TF("mail.launch_failed", err)
			}
		}
		a.orchDir = orchDir
		a.orchName = agentName
		a.currentView = appViewMail
		humanDir := filepath.Join(a.projectDir, "human")
		addr := humanAddr(a.projectDir)
		a.mail = NewMailModel(humanDir, addr, a.projectDir, a.orchDir, a.orchName, a.tuiConfig.MailPageSize, a.globalDir, a.tuiConfig.Language, a.tuiConfig.Insights)

		if launchErr != "" {
			a.mail.messages = append(a.mail.messages, ChatMessage{From: i18n.T("mail.system_sender"), Body: launchErr, Type: "mail"})
		}
		return a, tea.Batch(a.mail.Init(), a.sendSize())

	// === Global keys ===

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "q":
			// Only quit if not in a text input context
			if a.currentView != appViewSetup && a.currentView != appViewFirstRun && a.currentView != appViewMail && a.currentView != appViewProps && a.currentView != appViewAddon && a.currentView != appViewNirvana && a.currentView != appViewLibrary && a.currentView != appViewProjects && a.currentView != appViewAgora && a.currentView != appViewLogin && a.currentView != appViewCodex && a.currentView != appViewMailbox && a.currentView != appViewSystem && a.currentView != appViewPresets {
				return a, tea.Quit
			}
		}
	}

	// === Forward to current view ===
	switch a.currentView {
	case appViewFirstRun:
		updated, cmd := a.firstRun.Update(msg)
		a.firstRun = updated
		return a, cmd
	case appViewMail:
		updated, cmd := a.mail.Update(msg)
		a.mail = updated
		return a, cmd
	case appViewSetup:
		var cmd tea.Cmd
		a.setup, cmd = a.setup.Update(msg)
		return a, cmd
	case appViewSettings:
		updated, cmd := a.settings.Update(msg)
		a.settings = updated
		return a, cmd
	case appViewProps:
		updated, cmd := a.props.Update(msg)
		a.props = updated
		return a, cmd
	case appViewAddon:
		updated, cmd := a.addon.Update(msg)
		a.addon = updated
		return a, cmd
	case appViewDoctor:
		updated, cmd := a.doctor.Update(msg)
		a.doctor = updated
		return a, cmd
	case appViewNirvana:
		updated, cmd := a.nirvana.Update(msg)
		a.nirvana = updated
		return a, cmd
	case appViewLibrary:
		updated, cmd := a.library.Update(msg)
		a.library = updated
		return a, cmd
	case appViewProjects:
		updated, cmd := a.projects.Update(msg)
		a.projects = updated
		return a, cmd
	case appViewAgora:
		updated, cmd := a.agora.Update(msg)
		a.agora = updated
		return a, cmd
	case appViewLogin:
		var cmd tea.Cmd
		a.login, cmd = a.login.Update(msg)
		return a, cmd
	case appViewCodex:
		updated, cmd := a.codex.Update(msg)
		a.codex = updated
		return a, cmd
	case appViewMailbox:
		updated, cmd := a.mailbox.Update(msg)
		a.mailbox = updated
		return a, cmd
	case appViewSystem:
		updated, cmd := a.system.Update(msg)
		a.system = updated
		return a, cmd
	case appViewPresets:
		updated, cmd := a.presetLibrary.Update(msg)
		a.presetLibrary = updated
		return a, cmd
	}

	return a, nil
}

func (a App) handlePaletteCommand(command, args string) (tea.Model, tea.Cmd) {
	addMsg := func(text string) {
		a.mail.AddSystemMessage(text)
	}
	targetDir := a.orchDir
	targetName := a.orchName
	switch command {
	case "sleep":
		if args == "all" {
			agents, _ := fs.DiscoverAgents(a.projectDir)
			count := 0
			for _, agent := range agents {
				if agent.IsHuman {
					continue
				}
				if fs.IsAlive(agent.WorkingDir, 3.0) {
					os.WriteFile(filepath.Join(agent.WorkingDir, ".sleep"), []byte(""), 0o644)
					count++
				}
			}
			addMsg(i18n.TF("mail.sleep_all", count))
		} else if targetDir != "" {
			os.WriteFile(filepath.Join(targetDir, ".sleep"), []byte(""), 0o644)
			addMsg(i18n.T("mail.sleep_sent"))
		}
		return a, nil
	case "suspend":
		if args == "all" {
			agents, _ := fs.DiscoverAgents(a.projectDir)
			count := 0
			for _, agent := range agents {
				if agent.IsHuman {
					continue
				}
				if fs.IsAlive(agent.WorkingDir, 3.0) {
					os.WriteFile(filepath.Join(agent.WorkingDir, ".suspend"), []byte(""), 0o644)
					count++
				}
			}
			addMsg(i18n.TF("mail.suspended_all", count))
		} else if targetDir != "" {
			os.WriteFile(filepath.Join(targetDir, ".suspend"), []byte(""), 0o644)
			addMsg(i18n.TF("mail.suspended", targetName))
		}
		return a, nil
	case "cpr":
		if args == "all" {
			agents, _ := fs.DiscoverAgents(a.projectDir)
			count := 0
			var failures []string
			for _, agent := range agents {
				if agent.IsHuman {
					continue
				}
				if !fs.IsAlive(agent.WorkingDir, 3.0) && a.lingtaiCmd != "" {
					count++
					if err := reviveDir(a.lingtaiCmd, agent.WorkingDir); err != nil {
						failures = append(failures, fmt.Sprintf("%s (%s)", filepath.Base(agent.WorkingDir), firstLine(err)))
					}
				}
			}
			if len(failures) > 0 {
				addMsg(i18n.TF("mail.cpr_all_with_failures", count-len(failures), len(failures), strings.Join(failures, ", ")))
			} else {
				addMsg(i18n.TF("mail.cpr_all", count))
			}
		} else if targetDir != "" && a.lingtaiCmd != "" {
			if !fs.IsAlive(targetDir, 3.0) {
				if err := reviveDir(a.lingtaiCmd, targetDir); err != nil {
					addMsg(i18n.TF("mail.launch_failed", firstLine(err)))
				} else {
					addMsg(i18n.TF("mail.cpr", targetName))
				}
			} else {
				addMsg(i18n.T("mail.cpr_alive"))
			}
		}
		return a, nil
	case "lang":
		// Redirect to /settings — agent language is now configured there
		addMsg(i18n.T("mail.lang_moved"))
		return a, nil
	case "clear":
		if targetDir != "" && a.lingtaiCmd != "" {
			addMsg(i18n.T("mail.clearing"))
			lingtaiCmd := a.lingtaiCmd
			dir := targetDir
			return a, func() tea.Msg {
				// Suspend and wait for process to die
				suspendFile := filepath.Join(dir, ".suspend")
				os.WriteFile(suspendFile, []byte(""), 0o644)
				lockFile := filepath.Join(dir, ".agent.lock")
				for i := 0; i < 40; i++ {
					if tryLock(lockFile) {
						break
					}
					time.Sleep(250 * time.Millisecond)
				}
				os.Remove(suspendFile)
				// Wipe conversation history (token ledger is preserved)
				os.Remove(filepath.Join(dir, "history", "chat_history.jsonl"))
				os.Remove(filepath.Join(dir, "system", "context.md"))
				// Relaunch with clean context
				_, err := process.LaunchAgent(lingtaiCmd, dir)
				return refreshDoneMsg{err: err}
			}
		}
		return a, nil
	case "refresh":
		if args == "all" && a.lingtaiCmd != "" {
			addMsg(i18n.T("mail.refreshing_all"))
			lingtaiCmd := a.lingtaiCmd
			projectDir := a.projectDir
			return a, func() tea.Msg {
				agents, _ := fs.DiscoverAgents(projectDir)
				count := 0
				var failures []string
				for _, agent := range agents {
					if agent.IsHuman {
						continue
					}
					count++
					if err := hardRefreshDir(lingtaiCmd, agent.WorkingDir); err != nil {
						failures = append(failures, fmt.Sprintf("%s (%s)", filepath.Base(agent.WorkingDir), firstLine(err)))
					}
				}
				return refreshAllDoneMsg{count: count, failures: failures}
			}
		} else if args != "" && targetDir != "" && a.lingtaiCmd != "" {
			// `/refresh <preset>` — switch to a named preset and
			// relaunch. Resolve the name against the agent's
			// manifest.preset.allowed list before doing any
			// destructive work; surface a clear error message in
			// the status bar if it doesn't match.
			resolved, err := resolvePresetInAllowed(targetDir, args)
			if err != nil {
				addMsg(firstLine(err))
				return a, nil
			}
			addMsg(fmt.Sprintf(i18n.T("mail.refreshing_to_preset"),
				strings.TrimSuffix(filepath.Base(resolved), ".json")))
			lingtaiCmd := a.lingtaiCmd
			dir := targetDir
			return a, func() tea.Msg {
				return refreshDoneMsg{err: hardRefreshDirWithPreset(lingtaiCmd, dir, resolved)}
			}
		} else if targetDir != "" && a.lingtaiCmd != "" {
			addMsg(i18n.T("mail.refreshing"))
			lingtaiCmd := a.lingtaiCmd
			dir := targetDir
			return a, func() tea.Msg {
				return refreshDoneMsg{err: hardRefreshDir(lingtaiCmd, dir)}
			}
		}
		return a, nil
	case "doctor":
		if targetDir != "" {
			a.currentView = appViewDoctor
			a.doctor = NewDoctorModel(targetDir, a.globalDir)
			return a, tea.Batch(a.doctor.Init(), a.sendSize())
		}
		return a, nil
	case "viz":
		url := a.portalURL()
		if url != "" {
			openBrowser(url)
		} else {
			addMsg("lingtai-portal not found on PATH. Run: brew link --overwrite lingtai-tui")
		}
		return a, nil
	case "addon":
		if a.orchDir != "" {
			a.currentView = appViewAddon
			a.addon = NewAddonModel(a.projectDir)
			return a, tea.Batch(a.addon.Init(), a.sendSize())
		}
		return a, nil
	case "login":
		a.currentView = appViewLogin
		a.login = NewLoginModel(a.orchDir, a.globalDir)
		return a, tea.Batch(a.login.Init(), a.sendSize())
	case "setup":
		a.currentView = appViewFirstRun
		a.firstRun = NewSetupModeModel(a.projectDir, a.globalDir, a.orchDir, a.orchName)
		return a, tea.Batch(a.firstRun.Init(), a.sendSize())
	case "settings":
		a.currentView = appViewSettings
		tuiCfg := config.LoadTUIConfig(a.globalDir)
		a.settings = NewSettingsModel(a.globalDir, a.projectDir, a.orchDir, tuiCfg)
		return a, tea.Batch(a.settings.Init(), a.sendSize())
	case "nirvana":
		a.currentView = appViewNirvana
		a.nirvana = NewNirvanaModel(a.projectDir)
		return a, tea.Batch(a.nirvana.Init(), a.sendSize())
	case "kanban":
		a.currentView = appViewProps
		a.props = NewPropsModel(a.projectDir, a.orchDir, a.globalDir)
		return a, tea.Batch(a.props.Init(), a.sendSize())
	case "skills":
		a.currentView = appViewLibrary
		// Agent-scoped: mirror what the skills capability would inject for
		// this agent. Scans <agent>/.library/ plus every Tier-1 path declared
		// in init.json (manifest.capabilities.skills.paths).
		a.library = NewLibraryModel(a.projectDir, a.orchDir, a.tuiConfig.Language)
		return a, tea.Batch(a.library.Init(), a.sendSize())
	case "projects":
		a.currentView = appViewProjects
		a.projects = NewProjectsModel(a.globalDir, a.projectDir)
		return a, tea.Batch(a.projects.Init(), a.sendSize())
	case "knowledge", "library", "codex":
		a.currentView = appViewCodex
		a.codex = NewCodexModel(a.projectDir, a.orchDir)
		return a, tea.Batch(a.codex.Init(), a.sendSize())
	case "system":
		a.currentView = appViewSystem
		a.system = NewSystemModel(a.projectDir, a.orchDir)
		return a, tea.Batch(a.system.Init(), a.sendSize())
	case "mailbox":
		a.currentView = appViewMailbox
		a.mailbox = NewMailboxModel(a.projectDir)
		return a, tea.Batch(a.mailbox.Init(), a.sendSize())
	case "presets":
		a.currentView = appViewPresets
		// Agent-scoped: shows only the presets in this agent's
		// manifest.preset.allowed list — these are exactly the ones
		// `/refresh <name>` can switch to. The currently-active preset
		// is highlighted in the view. Falls back to the full global
		// registry only when no orchestrator agent is current (e.g.
		// before /setup completes), since there's no allow-list to
		// scope by yet.
		if targetDir != "" {
			allowed := readAllowedPresets(targetDir)
			active := readActivePreset(targetDir)
			a.presetLibrary = NewPresetLibraryModelForAgent(
				a.tuiConfig.Language, a.globalDir, allowed, active,
			)
		} else {
			a.presetLibrary = NewPresetLibraryModel(a.tuiConfig.Language, a.globalDir)
		}
		return a, tea.Batch(a.presetLibrary.Init(), a.sendSize())
	case "agora":
		a.currentView = appViewAgora
		a.agora = NewAgoraModel(a.globalDir, a.projectDir)
		return a, tea.Batch(a.agora.Init(), a.sendSize())
	case "export":
		if args != "" && args != "recipe" {
			addMsg(i18n.T("export.help"))
			return a, nil
		}
		if a.orchDir == "" {
			addMsg(i18n.T("export.no_agent"))
			return a, nil
		}
		if !fs.IsAlive(a.orchDir, 3.0) {
			addMsg(i18n.T("mail.btw_suspended"))
			return a, nil
		}
		fs.WritePrompt(a.orchDir, i18n.T("export.recipe_prompt"))
		addMsg(i18n.T("export.recipe_sent"))
		return a, nil
	case "molt":
		if targetDir == "" {
			return a, nil
		}
		if !fs.IsAlive(targetDir, 3.0) {
			addMsg(i18n.T("mail.btw_suspended"))
			return a, nil
		}
		// Send in agent's language, not TUI language
		lang := "en"
		if manifest, err := fs.ReadInitManifest(targetDir); err == nil {
			if l, ok := manifest["language"].(string); ok && l != "" {
				lang = l
			}
		}
		fs.WritePrompt(targetDir, i18n.TIn(lang, "molt.mandatory_prompt"))
		addMsg(i18n.T("mail.molt_sent"))
		return a, nil
	case "insights":
		if targetDir != "" {
			if !fs.IsAlive(targetDir, 3.0) {
				addMsg(i18n.T("mail.btw_suspended"))
				return a, nil
			}
			question := i18n.T("insight.auto_question")
			fs.WriteInquiry(targetDir, "insight", question)
			addMsg(i18n.T("mail.insight_sent"))
		}
		return a, nil
	case "btw":
		if targetDir != "" && args != "" {
			if !fs.IsAlive(targetDir, 3.0) {
				addMsg(i18n.T("mail.btw_suspended"))
				return a, nil
			}
			fs.WriteInquiry(targetDir, "human", args)
			addMsg(i18n.TF("mail.btw_sent", args))
		} else if args == "" {
			addMsg(i18n.T("mail.btw_usage"))
		}
		return a, nil
	case "quit":
		return a, tea.Quit
	}
	return a, nil
}

// hardRefresh suspends the orchestrator and relaunches it.
// Used by /refresh to force a full reload from init.json.
// Returns the error from process.LaunchAgent if the relaunch fails.
func (a *App) hardRefresh() error {
	if a.orchDir == "" || a.lingtaiCmd == "" {
		return nil
	}
	return hardRefreshDir(a.lingtaiCmd, a.orchDir)
}

// hardRefreshDir force-restarts the agent in the given directory. It is the
// escape hatch behind `/refresh`: rather than refusing when an interpreter is
// still alive, it escalates through suspend → lock-clear poll → SIGTERM/SIGKILL
// → stale-state cleanup → ForceLaunchAgent. Returns the launch error if the
// final relaunch fails; the kill/cleanup steps are best-effort and swallowed.
//
// Sequence:
//  1. Touch `.suspend` so a cooperative agent exits cleanly.
//  2. Wait for `.agent.lock` to free (up to 60s, then forced).
//  3. If `ps` still shows `lingtai run <dir>`, SIGTERM (then SIGKILL) those
//     PIDs — this is what makes /refresh actually forceful rather than a
//     polite request.
//  4. Sweep stale handshake files (.agent.lock, .refresh, .refresh.taken,
//     .suspend) so the fresh interpreter doesn't immediately re-suspend or
//     stall on a leftover lock.
//  5. Reset manifest.preset.active to manifest.preset.default — documented
//     escape hatch when the active preset is misbehaving (rate-limited,
//     broken adapter, etc.).
//  6. ForceLaunchAgent (bypassing the duplicate-protection gate; we've
//     already verified the agent dir is clear above).
func hardRefreshDir(lingtaiCmd, dir string) error {
	suspendFile := filepath.Join(dir, ".suspend")
	os.WriteFile(suspendFile, []byte(""), 0o644)
	waitForLockClear(dir)
	// Escalation: if the agent ignored .suspend (deadlocked, slow shutdown,
	// detached child), kill the lingering interpreter so LaunchAgent's
	// duplicate-protection gate doesn't refuse the relaunch.
	if process.IsAgentRunning(dir) {
		_ = process.TerminateAgentProcesses(dir)
	}
	// Clear lingering handshake files. waitForLockClear may have force-removed
	// .agent.lock; the others (.refresh/.refresh.taken/.suspend) get removed
	// here so the new interpreter doesn't immediately observe a stale signal.
	os.Remove(filepath.Join(dir, ".agent.lock"))
	os.Remove(filepath.Join(dir, ".refresh"))
	os.Remove(filepath.Join(dir, ".refresh.taken"))
	os.Remove(suspendFile)
	resetActivePresetToDefault(dir)
	_, err := process.ForceLaunchAgent(lingtaiCmd, dir)
	// Defensive: ForceLaunchAgent → launchAgentUnsafe calls fs.CleanSignals
	// internally, but a fresh .suspend written by another path between our
	// remove() above and the relaunch would put the new process to sleep.
	// Removing again here is cheap and idempotent.
	os.Remove(suspendFile)
	return err
}

// waitForLockClear polls for .agent.lock to free (force-removing it after
// 60s if the holder is gone). Used by hardRefreshDir between suspend and
// relaunch so we don't stomp a still-running agent's init.json.
func waitForLockClear(dir string) {
	lockFile := filepath.Join(dir, ".agent.lock")
	for i := 0; i < 120; i++ { // 120 × 500ms = 60s max
		if tryLock(lockFile) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	// Process likely died without releasing lock — clean up
	os.Remove(lockFile)
}

// resetActivePresetToDefault rewrites manifest.preset.active to match
// manifest.preset.default in the agent's init.json. Best-effort: any error
// (missing file, malformed JSON, missing preset block) is silently ignored
// so a /refresh still relaunches even if the preset block is in a weird
// state. Both `default` and `active` are guaranteed by validate_init to be
// in `allowed`, so writing active = default is always authorized.
func resetActivePresetToDefault(dir string) {
	initPath := filepath.Join(dir, "init.json")
	data, err := os.ReadFile(initPath)
	if err != nil {
		return
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	manifest, ok := raw["manifest"].(map[string]interface{})
	if !ok {
		return
	}
	pre, ok := manifest["preset"].(map[string]interface{})
	if !ok {
		return
	}
	def, ok := pre["default"].(string)
	if !ok || def == "" {
		return
	}
	if cur, ok := pre["active"].(string); ok && cur == def {
		return // already on default, nothing to write
	}
	pre["active"] = def
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(initPath, out, 0o644)
}

// readAllowedPresets returns the contents of manifest.preset.allowed from
// the agent's init.json — the per-agent allow-list that the kernel
// enforces on runtime preset swaps. Returns nil on any failure (missing
// file, malformed JSON, missing/empty block); callers should treat nil
// as "no allow-list available" and fall back to the global preset
// library rather than fail.
func readAllowedPresets(dir string) []string {
	initPath := filepath.Join(dir, "init.json")
	data, err := os.ReadFile(initPath)
	if err != nil {
		return nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	manifest, ok := raw["manifest"].(map[string]interface{})
	if !ok {
		return nil
	}
	pre, ok := manifest["preset"].(map[string]interface{})
	if !ok {
		return nil
	}
	allowed, ok := pre["allowed"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(allowed))
	for _, v := range allowed {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// readActivePreset returns manifest.preset.active from the agent's
// init.json — the preset currently in force. Returns "" on any failure
// or when the field is missing. Used by /presets to highlight the
// active entry in the agent-scoped view.
func readActivePreset(dir string) string {
	initPath := filepath.Join(dir, "init.json")
	data, err := os.ReadFile(initPath)
	if err != nil {
		return ""
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}
	manifest, ok := raw["manifest"].(map[string]interface{})
	if !ok {
		return ""
	}
	pre, ok := manifest["preset"].(map[string]interface{})
	if !ok {
		return ""
	}
	active, _ := pre["active"].(string)
	return active
}

// resolvePresetInAllowed matches a user-provided query (`/refresh <query>`)
// against the agent's manifest.preset.allowed list. The query may be:
//   - a bare preset name / basename stem ("mimo", "glm-5.1-pro")
//   - a full home-shortened ref ("~/.lingtai-tui/presets/templates/mimo.json")
//   - any path string that exactly matches an entry in allowed (less
//     common, but supports recipe-style paths).
//
// Returns the canonical allowed[] entry on a unique match. Returns an
// error string if no match, multiple matches, or the agent has no
// allowed list. The returned path is what should be written to
// manifest.preset.active; the kernel's _refresh allowed-gate will
// validate it again with `_preset_ref_in` so home-shortened and
// absolute forms compare equal.
func resolvePresetInAllowed(dir, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("preset name is empty")
	}
	allowed := readAllowedPresets(dir)
	if len(allowed) == 0 {
		return "", fmt.Errorf("agent has no manifest.preset.allowed list — cannot switch")
	}
	// Exact-path match first.
	for _, ref := range allowed {
		if ref == query {
			return ref, nil
		}
	}
	// Basename-stem match (drop directory prefix and .json suffix).
	var matches []string
	for _, ref := range allowed {
		stem := strings.TrimSuffix(filepath.Base(ref), ".json")
		if stem == query {
			matches = append(matches, ref)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		// Two presets in the allow-list with the same basename (e.g.
		// a template "mimo.json" and a saved "mimo.json"). Disambiguate.
		return "", fmt.Errorf("preset %q is ambiguous (matches %d entries) — pass the full path",
			query, len(matches))
	}
	// No match. Build a helpful error listing what's actually allowed
	// (basenames only — full paths are noisy in the status bar).
	stems := make([]string, 0, len(allowed))
	for _, ref := range allowed {
		stems = append(stems, strings.TrimSuffix(filepath.Base(ref), ".json"))
	}
	return "", fmt.Errorf("preset %q is not in this agent's allowed list (available: %s)",
		query, strings.Join(stems, ", "))
}

// setActivePreset rewrites manifest.preset.active to the given path.
// Caller is responsible for ensuring the path is in manifest.preset.allowed
// (use resolvePresetInAllowed) — this function is the dumb writer.
// Returns the error from json or filesystem failures; the kernel will
// reject a non-allowed path on relaunch with its own validation error.
func setActivePreset(dir, presetPath string) error {
	initPath := filepath.Join(dir, "init.json")
	data, err := os.ReadFile(initPath)
	if err != nil {
		return err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	manifest, ok := raw["manifest"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("init.json missing 'manifest' object")
	}
	pre, ok := manifest["preset"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("init.json missing 'manifest.preset' object")
	}
	pre["active"] = presetPath
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(initPath, out, 0o644)
}

// hardRefreshDirWithPreset is the `/refresh <preset>` cousin of
// hardRefreshDir. Sequence is identical (suspend → lock-clear → kill →
// signal sweep → relaunch) except that step 5 writes
// manifest.preset.active = presetPath instead of resetting to default.
// The caller is expected to have already validated presetPath via
// resolvePresetInAllowed.
func hardRefreshDirWithPreset(lingtaiCmd, dir, presetPath string) error {
	suspendFile := filepath.Join(dir, ".suspend")
	os.WriteFile(suspendFile, []byte(""), 0o644)
	waitForLockClear(dir)
	if process.IsAgentRunning(dir) {
		_ = process.TerminateAgentProcesses(dir)
	}
	os.Remove(filepath.Join(dir, ".agent.lock"))
	os.Remove(filepath.Join(dir, ".refresh"))
	os.Remove(filepath.Join(dir, ".refresh.taken"))
	os.Remove(suspendFile)
	if err := setActivePreset(dir, presetPath); err != nil {
		// Don't refuse the relaunch — the user asked to refresh.
		// Falling back to whatever active currently is.
	}
	_, err := process.ForceLaunchAgent(lingtaiCmd, dir)
	os.Remove(suspendFile)
	return err
}

// reviveDir waits for .agent.lock to free (force-removing it if the holder
// is gone), then relaunches the agent. Used by /cpr (dead agent, no prior
// suspend) and as the tail of hardRefreshDir (after writing .suspend).
func reviveDir(lingtaiCmd, dir string) error {
	lockFile := filepath.Join(dir, ".agent.lock")
	locked := true
	for i := 0; i < 120; i++ { // 120 × 500ms = 60s max
		if tryLock(lockFile) {
			locked = false
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if locked {
		// Process likely died without releasing lock — clean up
		os.Remove(lockFile)
	}
	_, err := process.LaunchAgent(lingtaiCmd, dir)
	return err
}

// firstLine returns the first line of err.Error(), trimmed of trailing
// whitespace. Used to sanitize errors before they are rendered in the
// single-line status bar — embedded newlines from wrapped subprocess
// stderr (e.g., Python tracebacks captured by EnsureAddons) would
// otherwise corrupt the layout by pushing the status bar across multiple
// rows.
func firstLine(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, " \t\r")
}

// tryLock is defined in lock_unix.go / lock_windows.go

// sendSize returns a tea.Cmd that sends the current terminal dimensions to the
// newly created view so it doesn't render with zero width/height.
func (a App) sendSize() tea.Cmd {
	w, h := a.width, a.height
	return func() tea.Msg { return tea.WindowSizeMsg{Width: w, Height: h} }
}

// RecipeFreshStartMsg is emitted from stepRecipeSwapConfirm when the user
// chooses "Fresh start (wipe .lingtai/ and reconfigure)". The app routes
// this to NirvanaModel and stores the recipe so post-nirvana first-run
// can pre-select it.
type RecipeFreshStartMsg struct {
	Recipe    string
	CustomDir string
}

type refreshDoneMsg struct{ err error }
type refreshAllDoneMsg struct {
	count    int
	failures []string
}

func (a App) switchToView(viewName string) (tea.Model, tea.Cmd) {
	switch viewName {
	case "mail":
		a.currentView = appViewMail
		// Reload config in case settings changed it
		a.tuiConfig = config.LoadTUIConfig(a.globalDir)
		ps := a.tuiConfig.MailPageSize
		if ps <= 0 {
			ps = unlimitedPageSize
		}
		a.mail.pageSize = ps
		a.mail.insightsEnabled = a.tuiConfig.Insights
		// Re-apply theme to textarea (settings may have changed it)
		a.mail.input.ApplyTheme()
		// Restart mail tick + refresh + pulse (ticks die when another view is active)
		return a, tea.Batch(a.mail.refreshMail, tickEvery(a.mail.pollRate), pulseTick(), a.sendSize())
	case "setup":
		a.currentView = appViewFirstRun
		a.firstRun = NewSetupModeModel(a.projectDir, a.globalDir, a.orchDir, a.orchName)
		return a, tea.Batch(a.firstRun.Init(), a.sendSize())
	case "settings":
		a.currentView = appViewSettings
		tuiCfg := config.LoadTUIConfig(a.globalDir)
		a.settings = NewSettingsModel(a.globalDir, a.projectDir, a.orchDir, tuiCfg)
		return a, tea.Batch(a.settings.Init(), a.sendSize())
	case "props", "kanban":
		a.currentView = appViewProps
		a.props = NewPropsModel(a.projectDir, a.orchDir, a.globalDir)
		return a, tea.Batch(a.props.Init(), a.sendSize())
	case "skills":
		a.currentView = appViewLibrary
		// Agent-scoped: mirror what the skills capability would inject for
		// this agent. Scans <agent>/.library/ plus every Tier-1 path declared
		// in init.json (manifest.capabilities.skills.paths).
		a.library = NewLibraryModel(a.projectDir, a.orchDir, a.tuiConfig.Language)
		return a, tea.Batch(a.library.Init(), a.sendSize())
	case "knowledge", "library", "codex":
		a.currentView = appViewCodex
		a.codex = NewCodexModel(a.projectDir, a.orchDir)
		return a, tea.Batch(a.codex.Init(), a.sendSize())
	case "system":
		a.currentView = appViewSystem
		a.system = NewSystemModel(a.projectDir, a.orchDir)
		return a, tea.Batch(a.system.Init(), a.sendSize())
	case "presets":
		a.currentView = appViewPresets
		// Agent-scoped: same view as `/presets`. Shows only the
		// presets in this agent's manifest.preset.allowed list, with
		// the currently-active one highlighted. Falls back to the
		// global registry when no orchestrator is current.
		if a.orchDir != "" {
			allowed := readAllowedPresets(a.orchDir)
			active := readActivePreset(a.orchDir)
			a.presetLibrary = NewPresetLibraryModelForAgent(
				a.tuiConfig.Language, a.globalDir, allowed, active,
			)
		} else {
			a.presetLibrary = NewPresetLibraryModel(a.tuiConfig.Language, a.globalDir)
		}
		return a, tea.Batch(a.presetLibrary.Init(), a.sendSize())
	case "projects":
		a.currentView = appViewProjects
		a.projects = NewProjectsModel(a.globalDir, a.projectDir)
		return a, tea.Batch(a.projects.Init(), a.sendSize())
	case "agora":
		a.currentView = appViewAgora
		a.agora = NewAgoraModel(a.globalDir, a.projectDir)
		return a, tea.Batch(a.agora.Init(), a.sendSize())
	case "addon":
		if a.orchDir != "" {
			a.currentView = appViewAddon
			a.addon = NewAddonModel(a.projectDir)
			return a, tea.Batch(a.addon.Init(), a.sendSize())
		}
		return a, nil
	case "welcome":
		a.currentView = appViewFirstRun
		a.firstRun = NewFirstRunModel(a.projectDir, a.globalDir, true, "")
		a.firstRun.welcomeOnly = true
		return a, tea.Batch(a.firstRun.Init(), a.sendSize())
	}
	return a, nil
}

func (a App) View() tea.View {
	var content string
	switch a.currentView {
	case appViewFirstRun:
		content = a.firstRun.View()
	case appViewMail:
		banner := ""
		if a.startupBanner != "" {
			banner = "  " + lipgloss.NewStyle().Foreground(ColorStuck).Render(a.startupBanner) + "\n"
		}
		content = banner + a.mail.View()
	case appViewSetup:
		content = a.setup.View()
	case appViewSettings:
		content = a.settings.View()
	case appViewProps:
		content = a.props.View()
	case appViewAddon:
		content = a.addon.View()
	case appViewDoctor:
		content = a.doctor.View()
	case appViewNirvana:
		content = a.nirvana.View()
	case appViewLibrary:
		content = a.library.View()
	case appViewProjects:
		content = a.projects.View()
	case appViewAgora:
		content = a.agora.View()
	case appViewLogin:
		content = a.login.View()
	case appViewCodex:
		content = a.codex.View()
	case appViewMailbox:
		content = a.mailbox.View()
	case appViewSystem:
		content = a.system.View()
	case appViewPresets:
		content = a.presetLibrary.View()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	t := ActiveTheme()
	if t.PaintBG {
		v.BackgroundColor = t.BG
		v.ForegroundColor = t.Text
	}
	return v
}

// portalURL kills any existing portal and spawns a fresh one.
// Returns the URL or empty string if lingtai-portal is not on PATH.
func (a *App) portalURL() string {
	portFile := filepath.Join(a.projectDir, ".portal", "port")

	// Kill existing portal so we always get a fresh instance with the latest binary
	exec.Command("pkill", "-f", "lingtai-portal.*--dir.*"+filepath.Dir(a.projectDir)).Run()
	os.Remove(portFile)
	time.Sleep(300 * time.Millisecond)

	// Spawn fresh portal
	portalCmd, _ := exec.LookPath("lingtai-portal")
	if portalCmd == "" {
		return ""
	}
	cmd := exec.Command(portalCmd, "--dir", filepath.Dir(a.projectDir))
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return ""
	}
	// Release the process so it survives TUI exit
	cmd.Process.Release()

	// Wait for port file to appear (up to 3 seconds)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if data, err := os.ReadFile(portFile); err == nil {
			return "http://localhost:" + strings.TrimSpace(string(data))
		}
	}
	return ""
}

func isWSL() bool {
	b, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	s := strings.ToLower(string(b))
	return strings.Contains(s, "microsoft") || strings.Contains(s, "wsl")
}

func openBrowser(url string) {
	if url == "" {
		return
	}
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		if isWSL() {
			// Prefer wslview (wslu) — handles WSL→Windows browser opening natively.
			// Fallback: powershell.exe Start-Process (more reliable than cmd.exe start
			// with URLs containing colons).
			if path, err := exec.LookPath("wslview"); err == nil {
				cmd = path
				args = []string{url}
			} else {
				cmd = "powershell.exe"
				args = []string{"-NoProfile", "-Command", "Start-Process", "'" + url + "'"}
			}
		} else {
			cmd = "xdg-open"
			args = []string{url}
		}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	}
	if cmd != "" {
		exec.Command(cmd, args...).Start()
	}
}

// ValidateCodexAuthOnStartup performs a real validity check on the
// stored Codex OAuth tokens at TUI launch. The local file is treated as
// a structural prerequisite (missing → no-op, no banner); when it is
// present we round-trip the refresh token through OpenAI's token
// endpoint to confirm the grant has not been revoked server-side.
//
// Behavior matrix:
//
//   - file missing                                → return "" (user has no codex login, nothing to test)
//   - file malformed / no refresh_token           → file is junk; return banner pointing at re-login
//   - access token still valid (>5 min until exp) → trust local data, no network call
//   - access token expired/expiring               → refresh against auth.openai.com
//   - 200 OK         → atomic write back, return ""
//   - 401/403        → grant revoked, return banner pointing at re-login
//   - transient err  → return "" (do not penalize the user for being offline)
//
// On success the file is updated atomically (.json.tmp → rename) so any
// later code paths in this launch (firstrun's refreshCodexAuth, the
// agent-launch validateCodexAuthForAgents, the kernel's CodexTokenManager
// inside the agent process) all see the freshest tokens.
func ValidateCodexAuthOnStartup(globalDir string) string {
	authPath := filepath.Join(globalDir, "codex-auth.json")
	raw, err := os.ReadFile(authPath)
	if err != nil {
		return ""
	}

	var tokens CodexTokens
	if err := json.Unmarshal(raw, &tokens); err != nil || tokens.RefreshToken == "" {
		return "⚠ Codex OAuth: codex-auth.json malformed — re-login via /setup"
	}

	const refreshBufferSeconds = 300
	if tokens.ExpiresAt > time.Now().Unix()+refreshBufferSeconds {
		return ""
	}

	fresh, err := refreshCodexTokens(tokens.RefreshToken, tokens)
	if err != nil {
		if err == ErrCodexAuthRevoked {
			return "⚠ Codex OAuth session expired — re-login via /setup → Codex 凭据"
		}
		return ""
	}

	out, err := json.MarshalIndent(fresh, "", "  ")
	if err != nil {
		return ""
	}
	tmpPath := authPath + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0o600); err != nil {
		return ""
	}
	if err := os.Rename(tmpPath, authPath); err != nil {
		os.Remove(tmpPath)
		return ""
	}
	return ""
}

// validateCodexAuthForAgents scans all agent directories under projectDir
// for init.json files whose active/default preset is codex. If any exist
// but ~/.lingtai-tui/codex-auth.json is missing or invalid, returns a
// warning string. Otherwise returns "".
func validateCodexAuthForAgents(globalDir, projectDir string) string {
	// Quick check: is codex-auth.json valid?
	authPath := filepath.Join(globalDir, "codex-auth.json")
	authOK := false
	if data, err := os.ReadFile(authPath); err == nil {
		var tokens CodexTokens
		if json.Unmarshal(data, &tokens) == nil && tokens.RefreshToken != "" {
			authOK = true
		}
	}
	if authOK {
		return ""
	}

	// Check if any agent uses a codex preset
	entries, _ := os.ReadDir(projectDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		initPath := filepath.Join(projectDir, e.Name(), "init.json")
		raw, err := os.ReadFile(initPath)
		if err != nil {
			continue
		}
		var init map[string]interface{}
		if json.Unmarshal(raw, &init) != nil {
			continue
		}
		manifest, _ := init["manifest"].(map[string]interface{})
		if manifest == nil {
			continue
		}
		presetBlock, _ := manifest["preset"].(map[string]interface{})
		if presetBlock == nil {
			continue
		}
		// Check default and active
		for _, key := range []string{"default", "active"} {
			if path, _ := presetBlock[key].(string); path != "" {
				if strings.Contains(path, "codex") {
					return fmt.Sprintf("⚠ Codex OAuth 未验证 — agent %q 使用 codex 预设", e.Name())
				}
			}
		}
	}
	return ""
}
