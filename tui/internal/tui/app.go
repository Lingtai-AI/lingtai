package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
	tea "github.com/charmbracelet/bubbletea"
)

type appView int

const (
	appViewFirstRun appView = iota
	appViewMail
	appViewManage
	appViewSetup
	appViewSettings
	appViewPresets
)

// App is the root Bubble Tea model. Routes between views via slash commands.
type App struct {
	currentView appView
	mail        MailModel
	manage      ManageModel
	setup       SetupModel
	settings    SettingsModel
	presets     PresetsModel
	firstRun    FirstRunModel

	globalDir  string
	projectDir string // .lingtai/ directory
	vizURL     string
	orchDir    string // full path to orchestrator dir
	orchName   string
	lingtaiCmd string
	width      int
	height     int
}

func humanAddr(projectDir string) string {
	humanDir := filepath.Join(projectDir, "human")
	node, err := fs.ReadAgent(humanDir)
	if err != nil {
		return humanDir
	}
	if node.Address != "" {
		return node.Address
	}
	return humanDir
}

// NewApp creates the root app model.
func NewApp(globalDir, projectDir, vizURL string, needsFirstRun bool, orchestrators []string, settings Settings) App {
	lingtaiCmd := config.LingtaiCmd(globalDir)

	app := App{
		globalDir:  globalDir,
		projectDir: projectDir,
		vizURL:     vizURL,
		lingtaiCmd: lingtaiCmd,
	}

	if needsFirstRun {
		app.currentView = appViewFirstRun
		hasPresets := preset.HasAny()
		app.firstRun = NewFirstRunModel(projectDir, globalDir, hasPresets)
	} else {
		// Determine orchestrator
		if len(orchestrators) == 1 {
			app.orchName = orchestrators[0]
			app.orchDir = filepath.Join(projectDir, orchestrators[0])
		} else if len(orchestrators) > 1 {
			// Check saved setting
			if settings.Orchestrator != "" {
				// Verify it still exists
				found := false
				for _, o := range orchestrators {
					if o == settings.Orchestrator {
						found = true
						break
					}
				}
				if found {
					app.orchName = settings.Orchestrator
					app.orchDir = filepath.Join(projectDir, settings.Orchestrator)
				}
			}
			// If no saved or stale, use first (app could prompt, but keep simple for now)
			if app.orchName == "" {
				app.orchName = orchestrators[0]
				app.orchDir = filepath.Join(projectDir, orchestrators[0])
				settings.Orchestrator = orchestrators[0]
				SaveSettings(projectDir, settings)
			}
		}

		app.currentView = appViewMail
		humanDir := filepath.Join(projectDir, "human")
		addr := humanAddr(projectDir)
		app.mail = NewMailModel(humanDir, addr, projectDir, app.orchDir, app.orchName)
		app.manage = NewManageModel(projectDir, lingtaiCmd)
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
		// Forward to all views so they can resize
		switch a.currentView {
		case appViewMail:
			a.mail, _ = a.mail.Update(msg)
		case appViewManage:
			a.manage, _ = a.manage.Update(msg)
		case appViewSetup:
			a.setup, _ = a.setup.Update(msg)
		case appViewSettings:
			a.settings, _ = a.settings.Update(msg)
		case appViewPresets:
			a.presets, _ = a.presets.Update(msg)
		case appViewFirstRun:
			a.firstRun, _ = a.firstRun.Update(msg)
		}
		return a, nil

	// === Cross-view messages ===

	case ViewChangeMsg:
		return a.switchToView(msg.View)

	case PaletteSelectMsg:
		return a.handlePaletteCommand(msg.Command)

	case FirstRunDoneMsg:
		// First-run complete: launch agent and switch to mail
		a.orchDir = msg.OrchDir
		a.orchName = msg.OrchName
		// Launch the agent
		var launchErr string
		if a.lingtaiCmd != "" {
			if _, err := process.LaunchAgent(a.lingtaiCmd, a.orchDir); err != nil {
				launchErr = fmt.Sprintf("Failed to launch agent: %v", err)
			}
		}
		// Initialize mail view
		a.currentView = appViewMail
		humanDir := filepath.Join(a.projectDir, "human")
		addr := humanAddr(a.projectDir)
		a.mail = NewMailModel(humanDir, addr, a.projectDir, a.orchDir, a.orchName)
		a.manage = NewManageModel(a.projectDir, a.lingtaiCmd)
		if launchErr != "" {
			a.mail.messages = append(a.mail.messages, ChatMessage{From: "system", Body: launchErr, Type: "mail"})
		}
		return a, a.mail.Init()

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
		p, err := preset.Load(msg.Name)
		if err != nil {
			return a, nil
		}
		agentName := p.Name
		if err := preset.GenerateInitJSON(p, agentName, a.projectDir); err != nil {
			return a, nil
		}
		orchDir := filepath.Join(a.projectDir, agentName)
		var launchErr string
		if a.lingtaiCmd != "" {
			if _, err := process.LaunchAgent(a.lingtaiCmd, orchDir); err != nil {
				launchErr = fmt.Sprintf("Failed to launch agent: %v", err)
			}
		}
		a.orchDir = orchDir
		a.orchName = agentName
		a.currentView = appViewMail
		humanDir := filepath.Join(a.projectDir, "human")
		addr := humanAddr(a.projectDir)
		a.mail = NewMailModel(humanDir, addr, a.projectDir, a.orchDir, a.orchName)
		a.manage = NewManageModel(a.projectDir, a.lingtaiCmd)
		if launchErr != "" {
			a.mail.messages = append(a.mail.messages, ChatMessage{From: "system", Body: launchErr, Type: "mail"})
		}
		return a, a.mail.Init()

	// === Global keys ===

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "q":
			// Only quit if not in a text input context
			if a.currentView != appViewSetup && a.currentView != appViewFirstRun && a.currentView != appViewMail {
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
	case appViewManage:
		updated, cmd := a.manage.Update(msg)
		a.manage = updated
		return a, cmd
	case appViewSetup:
		var cmd tea.Cmd
		a.setup, cmd = a.setup.Update(msg)
		return a, cmd
	case appViewSettings:
		updated, cmd := a.settings.Update(msg)
		a.settings = updated
		return a, cmd
	case appViewPresets:
		updated, cmd := a.presets.Update(msg)
		a.presets = updated
		return a, cmd
	}

	return a, nil
}

func (a App) handlePaletteCommand(command string) (tea.Model, tea.Cmd) {
	switch command {
	case "manage":
		a.currentView = appViewManage
		a.manage = NewManageModel(a.projectDir, a.lingtaiCmd)
		return a, tea.Batch(a.manage.Init(), a.sendSize())
	case "viz":
		// Open browser, stay on mail
		openBrowser(a.vizURL)
		return a, nil
	case "setup":
		a.currentView = appViewSetup
		a.setup = NewSetupModel(a.globalDir)
		return a, tea.Batch(a.setup.Init(), a.sendSize())
	case "settings":
		a.currentView = appViewSettings
		settings := LoadSettings(a.projectDir)
		a.settings = NewSettingsModel(a.projectDir, settings)
		return a, tea.Batch(a.settings.Init(), a.sendSize())
	case "presets":
		a.currentView = appViewPresets
		a.presets = NewPresetsModel()
		return a, tea.Batch(a.presets.Init(), a.sendSize())
	case "lang":
		// Cycle language: en → zh → wen → en
		langs := []string{"en", "zh", "wen"}
		current := i18n.Lang()
		next := langs[0]
		for idx, l := range langs {
			if l == current && idx+1 < len(langs) {
				next = langs[idx+1]
				break
			}
		}
		i18n.SetLang(next)
		settings := LoadSettings(a.projectDir)
		settings.Language = next
		SaveSettings(a.projectDir, settings)
		// Show confirmation inline
		a.mail.messages = append(a.mail.messages, ChatMessage{
			From: "system",
			Body: i18n.TF("settings.language") + ": " + next,
			Type: "mail",
		})
		if a.mail.ready {
			a.mail.viewport.SetContent(a.mail.renderMessages())
			a.mail.viewport.GotoBottom()
		}
		return a, nil
	case "help":
		// Render help inline as a system message in the chat stream
		helpText := i18n.T("help.title") + "\n" +
			i18n.T("help.manage") + "\n" +
			i18n.T("help.viz") + "\n" +
			i18n.T("help.setup") + "\n" +
			i18n.T("help.settings") + "\n" +
			i18n.T("help.presets") + "\n" +
			i18n.T("help.lang") + "\n" +
			i18n.T("help.help") + "\n" +
			i18n.T("help.verbose")
		a.mail.messages = append(a.mail.messages, ChatMessage{
			From: "system",
			Body: helpText,
			Type: "mail",
		})
		if a.mail.ready {
			a.mail.viewport.SetContent(a.mail.renderMessages())
			a.mail.viewport.GotoBottom()
		}
		return a, nil
	case "quit":
		return a, tea.Quit
	}
	return a, nil
}

// sendSize returns a tea.Cmd that sends the current terminal dimensions to the
// newly created view so it doesn't render with zero width/height.
func (a App) sendSize() tea.Cmd {
	w, h := a.width, a.height
	return func() tea.Msg { return tea.WindowSizeMsg{Width: w, Height: h} }
}

func (a App) switchToView(viewName string) (tea.Model, tea.Cmd) {
	switch viewName {
	case "mail":
		a.currentView = appViewMail
		return a, nil
	case "manage":
		a.currentView = appViewManage
		a.manage = NewManageModel(a.projectDir, a.lingtaiCmd)
		return a, tea.Batch(a.manage.Init(), a.sendSize())
	case "setup":
		a.currentView = appViewSetup
		a.setup = NewSetupModel(a.globalDir)
		return a, tea.Batch(a.setup.Init(), a.sendSize())
	case "settings":
		a.currentView = appViewSettings
		settings := LoadSettings(a.projectDir)
		a.settings = NewSettingsModel(a.projectDir, settings)
		return a, tea.Batch(a.settings.Init(), a.sendSize())
	case "presets":
		a.currentView = appViewPresets
		a.presets = NewPresetsModel()
		return a, tea.Batch(a.presets.Init(), a.sendSize())
	}
	return a, nil
}

func (a App) View() string {
	switch a.currentView {
	case appViewFirstRun:
		return a.firstRun.View()
	case appViewMail:
		return a.mail.View()
	case appViewManage:
		return a.manage.View()
	case appViewSetup:
		return a.setup.View()
	case appViewSettings:
		return a.settings.View()
	case appViewPresets:
		return a.presets.View()
	}
	return ""
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
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	}
	if cmd != "" {
		exec.Command(cmd, args...).Start()
	}
}
