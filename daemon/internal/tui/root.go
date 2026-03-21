package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"lingtai-daemon/internal/agent"
	"lingtai-daemon/internal/config"
	"lingtai-daemon/internal/setup"
)

// View identifies the active sub-view.
type View int

const (
	ViewStatus View = iota
	ViewWizard
	ViewChat
)

// RootModel is the top-level bubbletea model that routes between views.
type RootModel struct {
	view   View
	status StatusModel
	wizard setup.WizardModel
	chat   ChatModel

	config     *config.Config
	proc       *agent.Process
	lingtaiDir string
	configPath string
}

// RootOpts configures the RootModel.
type RootOpts struct {
	LingtaiDir  string
	ConfigPath  string
	Config      *config.Config
	Proc        *agent.Process
	InitialView View
}

func NewRoot(opts RootOpts) RootModel {
	m := RootModel{
		view:       opts.InitialView,
		lingtaiDir: opts.LingtaiDir,
		configPath: opts.ConfigPath,
		config:     opts.Config,
		proc:       opts.Proc,
	}
	m.status = NewStatus(opts.LingtaiDir)
	if opts.InitialView == ViewWizard {
		m.wizard = setup.NewWizardModel(opts.LingtaiDir)
	}
	if opts.Config != nil && opts.Proc != nil {
		m.chat = NewChat(opts.Config, opts.Proc)
	}
	return m
}

func (m RootModel) Init() tea.Cmd {
	switch m.view {
	case ViewWizard:
		return m.wizard.Init()
	case ViewChat:
		return m.chat.Init()
	default:
		return m.status.Init()
	}
}

func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case StatusTransitionMsg:
		switch msg.Target {
		case ViewWizard:
			os.MkdirAll(m.lingtaiDir, 0755)
			m.wizard = setup.NewWizardModel(m.lingtaiDir)
			m.view = ViewWizard
			return m, m.wizard.Init()
		case ViewChat:
			// Start agent if not running
			if m.proc == nil {
				cfg, proc, err := m.startAgent()
				if err != nil {
					// Stay on status — agent failed to start
					return m, nil
				}
				m.config = cfg
				m.proc = proc
				m.chat = NewChat(cfg, proc)
			}
			m.view = ViewChat
			return m, m.chat.Init()
		}

	case ChatExitMsg:
		// Return to status, agent keeps running
		m.status.scan()
		m.view = ViewStatus
		return m, nil
	}

	// Route to active sub-view
	switch m.view {
	case ViewWizard:
		updated, cmd := m.wizard.Update(msg)
		m.wizard = updated.(setup.WizardModel)
		// Check if wizard is done
		if m.wizard.Done() {
			if m.wizard.Err() == nil {
				// Wizard completed successfully — reload config and go to status
				cfg, err := config.Load(m.configPath)
				if err == nil {
					m.config = cfg
				}
			}
			m.status.scan()
			m.view = ViewStatus
			return m, nil
		}
		return m, cmd

	case ViewChat:
		updated, cmd := m.chat.Update(msg)
		m.chat = updated.(ChatModel)
		return m, cmd

	case ViewStatus:
		var cmd tea.Cmd
		m.status, cmd = m.status.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m RootModel) View() string {
	switch m.view {
	case ViewWizard:
		return m.wizard.View()
	case ViewChat:
		return m.chat.View()
	default:
		return m.status.View()
	}
}

// startAgent loads config and starts the Python agent process.
func (m *RootModel) startAgent() (*config.Config, *agent.Process, error) {
	cfg, err := config.Load(m.configPath)
	if err != nil {
		return nil, nil, err
	}
	proc, err := agent.Start(agent.StartOptions{
		ConfigPath: m.configPath,
		AgentPort:  cfg.AgentPort,
		WorkingDir: cfg.WorkingDir(),
		Headless:   true,
	})
	if err != nil {
		return nil, nil, err
	}
	return cfg, proc, nil
}

// RunTUI starts the unified TUI.
func RunTUI(opts RootOpts) {
	m := NewRoot(opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		os.Exit(1)
	}
	// If agent was started, stop it
	if m.proc != nil {
		m.proc.Stop()
	}
}
