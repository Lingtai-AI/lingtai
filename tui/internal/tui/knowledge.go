package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// KnowledgeModel is the top-level /knowledge view. Mirrors LibraryModel: shows one
// agent's private knowledge at a time and swaps agents via Ctrl+T.
type KnowledgeModel struct {
	baseDir     string // .lingtai/ directory (for agent discovery)
	selectedDir string // working dir of the currently-displayed agent

	inner MarkdownViewerModel

	// Drill-in viewer — non-nil when the user pressed Enter on a knowledge
	// entry and is browsing files inside that knowledge folder. Esc pops
	// back to the catalog (clears this pointer).
	drillIn      *MarkdownViewerModel
	drillInDir   string
	drillInTitle string

	pickerOpen bool
	pickerIdx  int
	agentNodes []fs.AgentNode

	width  int
	height int
	ready  bool

	pickerVP viewport.Model
}

type knowledgeLoadMsg struct {
	agentNodes []fs.AgentNode
}

// NewKnowledgeModel constructs the /knowledge view rooted at baseDir with the given
// agent pre-selected.
func NewKnowledgeModel(baseDir, selectedDir string) KnowledgeModel {
	entries := buildAgentKnowledgeCatalogEntries(selectedDir)
	inner := NewMarkdownViewer(entries, knowledgeTitleFor(selectedDir))
	inner.FooterHint = i18n.T("hints.props_select")
	return KnowledgeModel{
		baseDir:     baseDir,
		selectedDir: selectedDir,
		inner:       inner,
	}
}

func knowledgeTitleFor(agentDir string) string {
	base := i18n.T("palette.knowledge")
	if agentDir == "" {
		return base
	}
	name := filepath.Base(agentDir)
	if manifest, err := fs.ReadInitManifest(agentDir); err == nil {
		if v, ok := manifest["nickname"].(string); ok && v != "" {
			name = v
		} else if v, ok := manifest["agent_name"].(string); ok && v != "" {
			name = v
		}
	}
	return fmt.Sprintf("%s — %s", base, name)
}

// reloadCatalog rebuilds the top-level knowledge catalog for the selected agent
// from disk, resetting the markdown viewer to the top.
func (m KnowledgeModel) reloadCatalog() (KnowledgeModel, tea.Cmd) {
	entries := buildAgentKnowledgeCatalogEntries(m.selectedDir)
	m.inner = NewMarkdownViewer(entries, knowledgeTitleFor(m.selectedDir))
	m.inner.FooterHint = i18n.T("hints.props_select")
	var cmd tea.Cmd
	m.inner, cmd = m.resizeViewer(m.inner)
	return m, cmd
}

// reloadVisible rebuilds the currently-visible knowledge layer from disk. At the
// catalog layer it reloads the selected agent's top-level entries; when drilled
// into a knowledge folder, it reloads that folder without requiring a full TUI or
// agent restart.
func (m KnowledgeModel) reloadVisible() (KnowledgeModel, tea.Cmd) {
	if m.drillIn != nil && m.drillInDir != "" {
		title := m.drillInTitle
		if title == "" {
			title = i18n.T("palette.knowledge")
		}
		sub := NewMarkdownViewer(buildKnowledgeFolderEntries(m.drillInDir), title)
		var cmd tea.Cmd
		sub, cmd = m.resizeViewer(sub)
		m.drillIn = &sub
		return m, cmd
	}
	return m.reloadCatalog()
}

func (m KnowledgeModel) resizeViewer(viewer MarkdownViewerModel) (MarkdownViewerModel, tea.Cmd) {
	if m.width > 0 && m.height > 0 {
		return viewer.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	}
	return viewer, nil
}

func (m KnowledgeModel) loadAgents() tea.Msg {
	net, _ := fs.BuildNetwork(m.baseDir)
	var nodes []fs.AgentNode
	for _, n := range net.Nodes {
		if n.IsHuman {
			continue
		}
		if n.WorkingDir == "" {
			continue
		}
		nodes = append(nodes, n)
	}
	return knowledgeLoadMsg{agentNodes: nodes}
}

func (m KnowledgeModel) Init() tea.Cmd {
	return tea.Batch(m.inner.Init(), m.loadAgents)
}

const (
	knowledgeHeaderLines = 2
	knowledgeFooterLines = 2
)

func (m KnowledgeModel) Update(msg tea.Msg) (KnowledgeModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.height - knowledgeHeaderLines - knowledgeFooterLines
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
		if m.drillIn != nil {
			inner := *m.drillIn
			var dcmd tea.Cmd
			inner, dcmd = inner.Update(msg)
			m.drillIn = &inner
			if dcmd != nil {
				cmd = tea.Batch(cmd, dcmd)
			}
		}
		return m, cmd

	case knowledgeLoadMsg:
		m.agentNodes = msg.agentNodes
		found := false
		for _, n := range m.agentNodes {
			if n.WorkingDir == m.selectedDir {
				found = true
				break
			}
		}
		if !found && len(m.agentNodes) > 0 {
			m.pickerIdx = 0
		}
		return m, nil

	case MarkdownViewerSelectMsg:
		// Drill in to the knowledge entry's folder.
		if m.drillIn != nil {
			// Already drilled in — files are leaves, Enter is a no-op.
			return m, nil
		}
		if msg.Entry.Path == "" {
			return m, nil
		}
		knowledgeDir := filepath.Dir(msg.Entry.Path)
		files := buildKnowledgeFolderEntries(knowledgeDir)
		if len(files) == 0 {
			return m, nil
		}
		title := i18n.T("palette.knowledge") + " \u2014 " + msg.Entry.Label
		sub := NewMarkdownViewer(files, title)
		m.drillIn = &sub
		m.drillInDir = knowledgeDir
		m.drillInTitle = title
		if m.width > 0 && m.height > 0 {
			inner := *m.drillIn
			var cmd tea.Cmd
			inner, cmd = inner.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			m.drillIn = &inner
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.pickerOpen {
			return m.updatePicker(msg)
		}
		// Drill-in active: keys go to the drill-in viewer instead of
		// the catalog. Esc/q pops back to the catalog; Ctrl+R reloads
		// the current knowledge folder; Ctrl+T is ignored so the user
		// must Esc first to swap agents.
		if m.drillIn != nil {
			switch msg.String() {
			case "esc", "q":
				m.drillIn = nil
				m.drillInDir = ""
				m.drillInTitle = ""
				return m, nil
			case "ctrl+r":
				return m.reloadVisible()
			case "ctrl+t":
				return m, nil
			}
			inner := *m.drillIn
			var cmd tea.Cmd
			inner, cmd = inner.Update(msg)
			m.drillIn = &inner
			return m, cmd
		}
		switch msg.String() {
		case "ctrl+r":
			return m.reloadVisible()
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
		if m.drillIn != nil {
			inner := *m.drillIn
			var cmd tea.Cmd
			inner, cmd = inner.Update(msg)
			m.drillIn = &inner
			return m, cmd
		}
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd
	}

	// Default: forward to whichever viewer is currently visible.
	if m.drillIn != nil {
		inner := *m.drillIn
		var cmd tea.Cmd
		inner, cmd = inner.Update(msg)
		m.drillIn = &inner
		return m, cmd
	}
	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

func (m KnowledgeModel) updatePicker(msg tea.KeyPressMsg) (KnowledgeModel, tea.Cmd) {
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
				m.drillIn = nil
				m.drillInDir = ""
				m.drillInTitle = ""
				var cmd tea.Cmd
				m, cmd = m.reloadCatalog()
				m.pickerOpen = false
				m.syncPicker()
				return m, cmd
			}
		}
		m.pickerOpen = false
		m.syncPicker()
		return m, nil
	}
	return m, nil
}

func (m *KnowledgeModel) syncPicker() {
	if !m.ready {
		return
	}
	if m.pickerOpen {
		m.pickerVP.SetContent(m.renderPicker())
	}
}

func (m KnowledgeModel) renderPicker() string {
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
		if name == "" {
			name = "(unknown)"
		}

		state := n.State
		if state == "" {
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

func (m KnowledgeModel) View() string {
	if m.pickerOpen {
		header := StyleTitle.Render("  "+knowledgeTitleFor(m.selectedDir)) + "\n" + strings.Repeat("─", m.width)
		footer := strings.Repeat("─", m.width) + "\n" +
			StyleFaint.Render("  "+i18n.T("hints.props_select"))
		body := ""
		if m.ready {
			body = m.pickerVP.View()
		}
		return header + "\n" + PaintViewportBG(body, m.width) + "\n" + footer
	}
	if m.drillIn != nil {
		return m.drillIn.View()
	}
	return m.inner.View()
}
