package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
)

// updateState is the /update view's state machine:
//
//	stateChecking ──> stateConfirm ──(confirm)──> stateUpdating ──> stateDone
//	                      │                                            ▲
//	                      └──(already current / editable dev)──────────┘
//	                      └──(cancel)──> back to mail view
//
// The confirmation in stateConfirm is mandatory before any install command
// runs — /update never mutates on a single keystroke.
type updateState int

const (
	stateChecking updateState = iota
	stateConfirm
	stateUpdating
	stateDone
)

// updateCheckedMsg carries the read-only InspectKernel result back to the model.
type updateCheckedMsg struct {
	status config.KernelStatus
}

// updateDoneMsg carries the RunKernelUpdate result back to the model.
type updateDoneMsg struct {
	report config.DoctorReport
}

// UpdateModel is the /update dedicated view. It mirrors DoctorModel
// conventions: async work runs via tea.Cmd returning a result msg, and esc
// returns to the mail view. Unlike /doctor it updates ONLY the Python kernel,
// and only after explicit confirmation.
type UpdateModel struct {
	orchDir     string
	globalDir   string
	state       updateState
	status      config.KernelStatus
	confirmIdx  int // 0 = Update now, 1 = Cancel
	resultLines []doctorLine
	failed      bool // true when the kernel update reported an unhealthy result
	width       int
	height      int

	// inspectFn / updateFn are injection seams for tests. Production callers
	// get the real read-only InspectKernel and mutating RunKernelUpdate.
	inspectFn func() config.KernelStatus
	updateFn  func(force bool) config.DoctorReport
}

func NewUpdateModel(orchDir, globalDir string) UpdateModel {
	return UpdateModel{
		orchDir:   orchDir,
		globalDir: globalDir,
		state:     stateChecking,
		inspectFn: func() config.KernelStatus { return config.InspectKernel(globalDir) },
		updateFn:  func(force bool) config.DoctorReport { return config.RunKernelUpdate(globalDir, force) },
	}
}

func (m UpdateModel) Init() tea.Cmd {
	return m.checkCmd()
}

// checkCmd runs the read-only kernel inspection asynchronously.
func (m UpdateModel) checkCmd() tea.Cmd {
	inspect := m.inspectFn
	return func() tea.Msg {
		return updateCheckedMsg{status: inspect()}
	}
}

// runUpdateCmd runs the mutating kernel update asynchronously. force=true
// because the user already confirmed in stateConfirm.
func (m UpdateModel) runUpdateCmd() tea.Cmd {
	update := m.updateFn
	return func() tea.Msg {
		return updateDoneMsg{report: update(true)}
	}
}

func (m UpdateModel) Update(msg tea.Msg) (UpdateModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case updateCheckedMsg:
		m.status = msg.status
		// Skip the confirm prompt when there is nothing to update: already
		// current, or an editable dev checkout we must never clobber.
		if m.status.NeedsUpdate && !m.status.Editable {
			m.state = stateConfirm
			m.confirmIdx = 0
		} else {
			m.state = stateDone
		}
	case updateDoneMsg:
		for _, line := range msg.report.Lines {
			m.resultLines = append(m.resultLines, doctorLineFromConfig(line))
		}
		m.failed = !msg.report.Healthy
		m.state = stateDone
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m UpdateModel) handleKey(msg tea.KeyPressMsg) (UpdateModel, tea.Cmd) {
	// Esc always returns to the mail view, from any state.
	if msg.String() == "esc" {
		return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
	}

	if m.state == stateConfirm {
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
				m.state = stateUpdating
				return m, m.runUpdateCmd()
			case 1: // Cancel — no mutation, back to mail.
				return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
			}
		}
	}
	return m, nil
}

func (m UpdateModel) View() string {
	var b strings.Builder

	title := StyleTitle.Render(i18n.T("app.title")) + " " +
		StyleAccent.Render(RuneBullet) + " " +
		StyleTitle.Render(i18n.T("update.title"))
	escHint := StyleAccent.Render("[esc] ") + StyleSubtle.Render(i18n.T("manage.back"))
	padding := m.width - lipgloss.Width(title) - lipgloss.Width(escHint) - 1
	if padding > 0 {
		b.WriteString(title + strings.Repeat(" ", padding) + escHint + "\n")
	} else {
		b.WriteString(title + "  " + escHint + "\n")
	}
	b.WriteString(strings.Repeat("─", m.width) + "\n\n")

	switch m.state {
	case stateChecking:
		b.WriteString("  " + i18n.T("update.checking") + "\n")
	case stateConfirm:
		b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorStuck).Render(
			i18n.TF("update.current_latest", m.status.Installed, m.status.Latest)) + "\n\n")
		b.WriteString("  " + i18n.T("update.prompt") + "\n\n")
		options := []string{i18n.T("update.confirm"), i18n.T("update.cancel")}
		for i, opt := range options {
			cursor := "  "
			style := StyleSubtle
			if i == m.confirmIdx {
				cursor = StyleAccent.Render("▸ ")
				style = lipgloss.NewStyle().Foreground(ColorAgent)
			}
			b.WriteString("  " + cursor + style.Render(opt) + "\n")
		}
	case stateUpdating:
		b.WriteString("  " + i18n.T("update.updating") + "\n")
	case stateDone:
		switch {
		case m.status.Editable:
			b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorAgent).Render(
				i18n.T("update.editable_skip")) + "\n")
		case !m.status.NeedsUpdate:
			b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorAgent).Render(
				i18n.T("update.up_to_date")) + "\n")
		}
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
		if len(m.resultLines) > 0 {
			if m.failed {
				b.WriteString("\n  " + lipgloss.NewStyle().Foreground(ColorStuck).Render(i18n.T("update.failed")) + "\n")
			} else {
				b.WriteString("\n  " + lipgloss.NewStyle().Foreground(ColorAgent).Render(i18n.T("update.done")) + "\n")
			}
		}
	}

	b.WriteString("\n" + strings.Repeat("─", m.width) + "\n")
	b.WriteString(StyleFaint.Render("  [esc] "+i18n.T("manage.back")) + "\n")

	return b.String()
}
