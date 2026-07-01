package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
)

// updateTUIState is the /update-tui view's state machine:
//
//	stateTUIChecking ──> stateTUIConfirm ──(confirm)──> stateTUIUpdating ──> stateTUIDone
//	                       │                                                     ▲
//	                       └──(unsupported install)──────────────────────────────┘
//	                       └──(cancel)──> back to mail view
//
// The confirmation in stateTUIConfirm is mandatory before any install command
// runs — /update-tui never mutates on a single keystroke.
type updateTUIState int

const (
	stateTUIChecking updateTUIState = iota
	stateTUIConfirm
	stateTUIUpdating
	stateTUIDone
)

// updateTUICheckedMsg carries the read-only DetectCurrentTUIInstall result back
// to the model.
type updateTUICheckedMsg struct {
	install config.TUIInstallInfo
}

// updateTUIDoneMsg carries the RunManualTUIUpdate result back to the model.
type updateTUIDoneMsg struct {
	report config.TUIUpdateResult
}

// UpdateTUIModel is the /update-tui dedicated view. It mirrors UpdateModel
// conventions: async work runs via tea.Cmd returning a result msg, and esc
// returns to the mail view. Unlike /update it updates ONLY the TUI binary
// (the Python kernel is untouched), and only after explicit confirmation.
type UpdateTUIModel struct {
	globalDir   string
	state       updateTUIState
	install     config.TUIInstallInfo
	confirmIdx  int // 0 = Update now, 1 = Cancel
	resultLines []doctorLine
	failed      bool // true when the TUI update reported an unhealthy result
	width       int
	height      int

	// inspectFn / updateFn are injection seams for tests. Production callers
	// get the real read-only DetectCurrentTUIInstall and mutating
	// RunManualTUIUpdate.
	inspectFn func() config.TUIInstallInfo
	updateFn  func() config.TUIUpdateResult
}

func NewUpdateTUIModel(globalDir string) UpdateTUIModel {
	return UpdateTUIModel{
		globalDir: globalDir,
		state:     stateTUIChecking,
		inspectFn: func() config.TUIInstallInfo { return config.DetectCurrentTUIInstall(globalDir) },
		updateFn: func() config.TUIUpdateResult {
			return config.RunManualTUIUpdate(globalDir, config.ManualTUIUpdateOptions{
				CurrentTUIVersion: tuiVersion,
			})
		},
	}
}

func (m UpdateTUIModel) Init() tea.Cmd {
	return m.checkCmd()
}

// checkCmd runs the read-only install-method detection asynchronously.
func (m UpdateTUIModel) checkCmd() tea.Cmd {
	inspect := m.inspectFn
	return func() tea.Msg {
		return updateTUICheckedMsg{install: inspect()}
	}
}

// runUpdateCmd runs the mutating TUI update asynchronously. The user already
// confirmed in stateTUIConfirm, so this is the forced self-update path.
func (m UpdateTUIModel) runUpdateCmd() tea.Cmd {
	update := m.updateFn
	return func() tea.Msg {
		return updateTUIDoneMsg{report: update()}
	}
}

func (m UpdateTUIModel) Update(msg tea.Msg) (UpdateTUIModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case updateTUICheckedMsg:
		m.install = msg.install
		// Unsupported install methods (unknown/other) cannot self-update; skip
		// the confirm prompt and surface the updater's guidance directly.
		if msg.install.Method == config.TUIInstallMethodUnknown {
			m.state = stateTUIDone
		} else {
			m.state = stateTUIConfirm
			m.confirmIdx = 0
		}
	case updateTUIDoneMsg:
		for _, line := range msg.report.Lines {
			m.resultLines = append(m.resultLines, doctorLineFromConfig(line))
		}
		m.failed = !msg.report.Healthy
		m.state = stateTUIDone
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m UpdateTUIModel) handleKey(msg tea.KeyPressMsg) (UpdateTUIModel, tea.Cmd) {
	// Esc always returns to the mail view, from any state.
	if msg.String() == "esc" {
		return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
	}

	switch m.state {
	case stateTUIConfirm:
		switch msg.String() {
		case "up", "left":
			if m.confirmIdx > 0 {
				m.confirmIdx--
			}
		case "down", "right":
			if m.confirmIdx < 1 {
				m.confirmIdx++
			}
		case "enter":
			switch m.confirmIdx {
			case 0: // Update now — confirmation given, run the install.
				m.state = stateTUIUpdating
				return m, m.runUpdateCmd()
			case 1: // Cancel — no mutation, back to mail.
				return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
			}
		}
	case stateTUIDone:
		// After a failure, 'r' retries: clears the prior result and returns to
		// the confirm prompt so the user can re-trigger the update.
		if m.failed && msg.String() == "r" {
			m.resultLines = nil
			m.failed = false
			m.confirmIdx = 0
			m.state = stateTUIConfirm
		}
	}
	return m, nil
}

func (m UpdateTUIModel) View() string {
	var b strings.Builder

	title := StyleTitle.Render(i18n.T("app.title")) + " " +
		StyleAccent.Render(RuneBullet) + " " +
		StyleTitle.Render(i18n.T("update_tui.title"))
	escHint := StyleAccent.Render("[esc] ") + StyleSubtle.Render(i18n.T("manage.back"))
	padding := m.width - lipgloss.Width(title) - lipgloss.Width(escHint) - 1
	if padding > 0 {
		b.WriteString(title + strings.Repeat(" ", padding) + escHint + "\n")
	} else {
		b.WriteString(title + "  " + escHint + "\n")
	}
	b.WriteString(strings.Repeat("─", m.width) + "\n\n")

	switch m.state {
	case stateTUIChecking:
		b.WriteString("  " + i18n.T("update_tui.checking") + "\n")
	case stateTUIConfirm:
		b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorStuck).Render(
			i18n.TF("update_tui.method", tuiInstallLabel(m.install))) + "\n\n")
		b.WriteString("  " + i18n.T("update_tui.prompt") + "\n\n")
		options := []string{i18n.T("update_tui.confirm"), i18n.T("update_tui.cancel")}
		for i, opt := range options {
			cursor := "  "
			style := StyleSubtle
			if i == m.confirmIdx {
				cursor = StyleAccent.Render("▸ ")
				style = lipgloss.NewStyle().Foreground(ColorAgent)
			}
			b.WriteString("  " + cursor + style.Render(opt) + "\n")
		}
	case stateTUIUpdating:
		b.WriteString("  " + i18n.T("update_tui.updating") + "\n")
	case stateTUIDone:
		for _, line := range m.resultLines {
			switch {
			case line.Warn:
				b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorStuck).Render(line.Text) + "\n")
			case line.OK:
				b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorAgent).Render(line.Text) + "\n")
			default:
				b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorSuspended).Render(line.Text) + "\n")
			}
		}
		if m.failed {
			b.WriteString("\n  " + lipgloss.NewStyle().Foreground(ColorStuck).Render(i18n.T("update_tui.failed")) + "\n")
		} else {
			b.WriteString("\n  " + lipgloss.NewStyle().Foreground(ColorAgent).Render(i18n.T("update_tui.done")) + "\n")
		}
	}

	b.WriteString("\n" + strings.Repeat("─", m.width) + "\n")
	b.WriteString(StyleFaint.Render("  [esc] "+i18n.T("manage.back")) + "\n")

	return b.String()
}

// tuiInstallLabel renders a human-readable label for a detected TUI install,
// mirroring config.TUIInstallInfo.summary() (which is unexported).
func tuiInstallLabel(install config.TUIInstallInfo) string {
	label := string(install.Method)
	switch install.Method {
	case config.TUIInstallMethodHomebrew:
		label = "homebrew"
	case config.TUIInstallMethodSource:
		label = "source/user-local"
	case config.TUIInstallMethodUnknown:
		label = "unknown/other"
	}
	if install.Detail != "" {
		return label + " (" + install.Detail + ")"
	}
	return label
}
