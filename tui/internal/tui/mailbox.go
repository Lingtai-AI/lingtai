package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// MailboxModel is the top-level /mailbox view. Mirrors CodexModel: shows one
// agent's (or the human's) mailbox at a time and swaps targets via Ctrl+T.
type MailboxModel struct {
	baseDir     string // .lingtai/ directory (for agent discovery)
	selectedDir string // working dir of the currently-displayed mailbox owner

	inner MarkdownViewerModel

	pickerOpen bool
	pickerIdx  int
	agentNodes []fs.AgentNode // includes the human node

	width  int
	height int
	ready  bool

	pickerVP viewport.Model
}

type mailboxLoadMsg struct {
	agentNodes []fs.AgentNode
}

const (
	mailboxHeaderLines = 2
	mailboxFooterLines = 2
)

// NewMailboxModel constructs the /mailbox view rooted at baseDir with the
// human's mailbox pre-selected.
func NewMailboxModel(baseDir string) MailboxModel {
	humanDir := filepath.Join(baseDir, "human")
	entries := buildMailboxEntries(humanDir)
	inner := NewMarkdownViewer(entries, mailboxTitleFor(humanDir))
	inner.FooterHint = i18n.T("hints.props_select")
	return MailboxModel{
		baseDir:     baseDir,
		selectedDir: humanDir,
		inner:       inner,
	}
}

// mailboxTitleFor returns "<palette.mailbox> — <name>" for the given agent dir.
// For the human directory, the name is the localized "human" label.
func mailboxTitleFor(agentDir string) string {
	base := i18n.T("palette.mailbox")
	if agentDir == "" {
		return base
	}
	name := mailboxOwnerName(agentDir)
	return fmt.Sprintf("%s — %s", base, name)
}

func mailboxOwnerName(agentDir string) string {
	if filepath.Base(agentDir) == "human" {
		return "human"
	}
	name := filepath.Base(agentDir)
	if node, err := fs.ReadAgent(agentDir); err == nil {
		if node.Nickname != "" {
			name = node.Nickname
		} else if node.AgentName != "" {
			name = node.AgentName
		}
	}
	return name
}

func (m MailboxModel) loadAgents() tea.Msg {
	net, _ := fs.BuildNetwork(m.baseDir)
	var nodes []fs.AgentNode
	// Place the human first so it remains the conventional default.
	for _, n := range net.Nodes {
		if n.IsHuman && n.WorkingDir != "" {
			nodes = append(nodes, n)
		}
	}
	for _, n := range net.Nodes {
		if n.IsHuman {
			continue
		}
		if n.WorkingDir == "" {
			continue
		}
		nodes = append(nodes, n)
	}
	return mailboxLoadMsg{agentNodes: nodes}
}

func (m MailboxModel) Init() tea.Cmd {
	return tea.Batch(m.inner.Init(), m.loadAgents)
}

func (m MailboxModel) Update(msg tea.Msg) (MailboxModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.height - mailboxHeaderLines - mailboxFooterLines
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !m.ready {
			m.pickerVP = viewport.New()
			m.ready = true
		}
		m.pickerVP.SetWidth(m.width)
		m.pickerVP.SetHeight(vpHeight)
		m.syncPicker()
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd

	case mailboxLoadMsg:
		m.agentNodes = msg.agentNodes
		return m, nil

	case tea.KeyPressMsg:
		if m.pickerOpen {
			return m.updatePicker(msg)
		}
		switch msg.String() {
		case "ctrl+t":
			if len(m.agentNodes) == 0 {
				return m, nil
			}
			m.pickerOpen = true
			m.pickerIdx = 0
			for i, n := range m.agentNodes {
				if n.WorkingDir == m.selectedDir {
					m.pickerIdx = i
					break
				}
			}
			m.syncPicker()
			return m, nil
		}
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd

	case tea.MouseWheelMsg:
		if m.pickerOpen {
			var cmd tea.Cmd
			m.pickerVP, cmd = m.pickerVP.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

func (m MailboxModel) updatePicker(msg tea.KeyPressMsg) (MailboxModel, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+t":
		m.pickerOpen = false
		m.syncPicker()
		return m, nil
	case "up", "k":
		if m.pickerIdx > 0 {
			m.pickerIdx--
			m.syncPicker()
		}
		return m, nil
	case "down", "j":
		if m.pickerIdx < len(m.agentNodes)-1 {
			m.pickerIdx++
			m.syncPicker()
		}
		return m, nil
	case "enter":
		if m.pickerIdx < len(m.agentNodes) {
			newDir := m.agentNodes[m.pickerIdx].WorkingDir
			if newDir != "" && newDir != m.selectedDir {
				m.selectedDir = newDir
				entries := buildMailboxEntries(m.selectedDir)
				m.inner = NewMarkdownViewer(entries, mailboxTitleFor(m.selectedDir))
				m.inner.FooterHint = i18n.T("hints.props_select")
				if m.width > 0 && m.height > 0 {
					var cmd tea.Cmd
					m.inner, cmd = m.inner.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
					m.pickerOpen = false
					m.syncPicker()
					return m, cmd
				}
			}
		}
		m.pickerOpen = false
		m.syncPicker()
		return m, nil
	}
	return m, nil
}

func (m *MailboxModel) syncPicker() {
	if !m.ready {
		return
	}
	if m.pickerOpen {
		m.pickerVP.SetContent(m.renderPicker())
	}
}

func (m MailboxModel) renderPicker() string {
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	selectedStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.select_agent")))
	lines = append(lines, "")

	if len(m.agentNodes) == 0 {
		lines = append(lines, "  "+StyleFaint.Render("(no agents)"))
		lines = append(lines, "")
		lines = append(lines, "  "+StyleFaint.Render("[esc/ctrl+t] "+i18n.T("manage.back")))
		return strings.Join(lines, "\n")
	}

	for i, n := range m.agentNodes {
		name := n.AgentName
		if n.Nickname != "" {
			name = n.Nickname
		}
		if n.IsHuman {
			name = "human"
		}
		if name == "" {
			name = "(unknown)"
		}

		state := n.State
		if n.IsHuman {
			state = "──"
		} else if state == "" {
			state = "──"
		}
		stateRendered := lipgloss.NewStyle().Foreground(StateColor(strings.ToUpper(state))).Render(state)

		marker := "  "
		style := nameStyle
		if n.WorkingDir == m.selectedDir {
			marker = "● "
		}
		if i == m.pickerIdx {
			style = selectedStyle
			marker = "> "
			if n.WorkingDir == m.selectedDir {
				marker = ">●"
			}
		}

		lines = append(lines, fmt.Sprintf("  %s%-18s %s", marker, style.Render(name), stateRendered))
	}

	lines = append(lines, "")
	lines = append(lines, "  "+StyleFaint.Render("↑↓ "+i18n.T("manage.select")+"  [enter]  [esc/ctrl+t] "+i18n.T("manage.back")))

	return strings.Join(lines, "\n")
}

func (m MailboxModel) View() string {
	if m.pickerOpen {
		header := StyleTitle.Render("  "+mailboxTitleFor(m.selectedDir)) + "\n" + strings.Repeat("─", m.width)
		footer := strings.Repeat("─", m.width) + "\n" +
			StyleFaint.Render("  "+i18n.T("hints.props_select"))
		body := ""
		if m.ready {
			body = m.pickerVP.View()
		}
		return header + "\n" + PaintViewportBG(body, m.width) + "\n" + footer
	}
	return m.inner.View()
}
