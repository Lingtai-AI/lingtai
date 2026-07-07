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

// MailboxModel is the top-level /mailbox view. Mirrors KnowledgeModel: shows one
// agent's (or the human's) mailbox at a time and swaps targets via Ctrl+T.
type MailboxModel struct {
	baseDir     string // .lingtai/ directory (for agent discovery)
	selectedDir string // working dir of the currently-displayed mailbox owner

	inner      MarkdownViewerModel
	allEntries []MarkdownEntry

	searchMode  bool
	searchQuery string

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
	m := MailboxModel{
		baseDir:     baseDir,
		selectedDir: humanDir,
		allEntries:  buildMailboxEntries(humanDir),
	}
	m.rebuildInnerFromEntries()
	return m
}

func (m *MailboxModel) rebuildInnerFromEntries() {
	entries := filterMailboxEntries(m.allEntries, m.searchQuery)
	m.inner = NewMarkdownViewer(entries, mailboxTitleFor(m.selectedDir))
	m.configureInner()
}

func (m *MailboxModel) configureInner() {
	m.inner.FooterHint = m.mailboxFooterHint()
	// /mailbox is primarily an evidence lookup list. Keep list navigation in
	// focus so wheel/page keys move between messages by default; users can press
	// Tab to put focus on the message body and scroll it with the generic viewer.
	m.inner.focus = mdvFocusLeft
	if m.inner.expanded == nil {
		m.inner.expanded = make(map[string]bool)
	}
	for _, group := range m.inner.groupOrder {
		m.inner.expanded[group] = true
	}
	m.ensureMailboxCursorOnEntry()
	m.inner.syncLeft()
	m.inner.syncRight()
}

func (m MailboxModel) mailboxFooterHint() string {
	if m.searchMode {
		query := strings.TrimSpace(m.searchQuery)
		if query == "" {
			return fmt.Sprintf("search (%d/%d): type query · enter done · esc cancel", len(m.inner.entries), len(m.allEntries))
		}
		return fmt.Sprintf("search %q (%d/%d) · enter done · esc clear", truncate(query, 28), len(m.inner.entries), len(m.allEntries))
	}
	if strings.TrimSpace(m.searchQuery) != "" {
		return fmt.Sprintf("filter %q (%d/%d) · / edit · esc clear", truncate(m.searchQuery, 28), len(m.inner.entries), len(m.allEntries))
	}
	return "wheel/pgup/pgdn/home/end navigate · / search · ctrl+t agent · ctrl+r reload"
}

func (m *MailboxModel) syncInnerSize() tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}
	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	return cmd
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

func (m MailboxModel) reloadInner() (MailboxModel, tea.Cmd) {
	m.allEntries = buildMailboxEntries(m.selectedDir)
	m.rebuildInnerFromEntries()
	return m, m.syncInnerSize()
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
		if m.searchMode {
			return m.updateSearch(msg)
		}
		switch msg.String() {
		case "ctrl+r":
			return m.reloadInner()
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
		case "esc":
			if strings.TrimSpace(m.searchQuery) != "" {
				m.searchQuery = ""
				m.searchMode = false
				return m.applyMailboxFilter()
			}
		}
		if isMailboxSearchKey(msg) {
			m.searchMode = true
			m.inner.FooterHint = m.mailboxFooterHint()
			return m, nil
		}
		if m.handleMailboxNavKey(msg) {
			return m, nil
		}
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd

	case tea.PasteMsg:
		if m.searchMode {
			m.searchQuery += normalizeMailboxSearchText(msg.Content)
			return m.applyMailboxFilter()
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
		if m.handleMailboxWheel(msg) {
			return m, nil
		}
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

func (m MailboxModel) applyMailboxFilter() (MailboxModel, tea.Cmd) {
	m.rebuildInnerFromEntries()
	return m, m.syncInnerSize()
}

func isMailboxSearchKey(msg tea.KeyPressMsg) bool {
	if msg.Text == "/" {
		return true
	}
	key := msg.Key()
	return msg.String() == "ctrl+f" || (key.Code == 'f' && key.Mod == tea.ModCtrl)
}

func (m MailboxModel) updateSearch(msg tea.KeyPressMsg) (MailboxModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if strings.TrimSpace(m.searchQuery) == "" {
			m.searchMode = false
			m.inner.FooterHint = m.mailboxFooterHint()
			return m, nil
		}
		m.searchQuery = ""
		m.searchMode = false
		return m.applyMailboxFilter()
	case "enter":
		m.searchMode = false
		m.inner.FooterHint = m.mailboxFooterHint()
		return m, nil
	case "backspace", "ctrl+h":
		if m.searchQuery == "" {
			return m, nil
		}
		runes := []rune(m.searchQuery)
		m.searchQuery = string(runes[:len(runes)-1])
		return m.applyMailboxFilter()
	case "ctrl+u":
		if m.searchQuery == "" {
			return m, nil
		}
		m.searchQuery = ""
		return m.applyMailboxFilter()
	}
	if msg.Text != "" {
		m.searchQuery += normalizeMailboxSearchText(msg.Text)
		return m.applyMailboxFilter()
	}
	return m, nil
}

func (m *MailboxModel) handleMailboxWheel(msg tea.MouseWheelMsg) bool {
	if m.inner.focus != mdvFocusLeft {
		return false
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		return m.moveMailboxCursor(-1)
	case tea.MouseWheelDown:
		return m.moveMailboxCursor(1)
	}
	return false
}

func (m *MailboxModel) handleMailboxNavKey(msg tea.KeyPressMsg) bool {
	if m.inner.focus != mdvFocusLeft {
		return false
	}
	switch msg.Key().Code {
	case tea.KeyPgUp:
		return m.moveMailboxCursor(-m.mailboxPageStep())
	case tea.KeyPgDown:
		return m.moveMailboxCursor(m.mailboxPageStep())
	case tea.KeyHome:
		return m.jumpMailboxCursor(false)
	case tea.KeyEnd:
		return m.jumpMailboxCursor(true)
	}
	return false
}

func (m MailboxModel) mailboxPageStep() int {
	if h := m.inner.leftVP.Height(); h > 2 {
		return h - 2
	}
	return 5
}

func (m *MailboxModel) entryNodePositions() []int {
	nodes := m.inner.visibleNodes()
	positions := make([]int, 0, len(nodes))
	for i, n := range nodes {
		if !n.isGroup {
			positions = append(positions, i)
		}
	}
	return positions
}

func (m *MailboxModel) ensureMailboxCursorOnEntry() {
	positions := m.entryNodePositions()
	if len(positions) == 0 {
		m.inner.cursor = 0
		return
	}
	for _, pos := range positions {
		if pos == m.inner.cursor {
			return
		}
	}
	m.inner.cursor = positions[0]
}

func (m *MailboxModel) moveMailboxCursor(delta int) bool {
	if delta == 0 {
		return false
	}
	positions := m.entryNodePositions()
	if len(positions) == 0 {
		return false
	}
	current := 0
	for i, pos := range positions {
		if pos <= m.inner.cursor {
			current = i
		}
	}
	target := current + delta
	if target < 0 {
		target = 0
	}
	if target >= len(positions) {
		target = len(positions) - 1
	}
	if m.inner.cursor == positions[target] {
		return true
	}
	m.inner.cursor = positions[target]
	m.inner.syncLeft()
	m.inner.syncRight()
	return true
}

func (m *MailboxModel) jumpMailboxCursor(last bool) bool {
	positions := m.entryNodePositions()
	if len(positions) == 0 {
		return false
	}
	target := positions[0]
	if last {
		target = positions[len(positions)-1]
	}
	if m.inner.cursor == target {
		return true
	}
	m.inner.cursor = target
	m.inner.syncLeft()
	m.inner.syncRight()
	return true
}

func normalizeMailboxSearchText(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, s)
}

func filterMailboxEntries(entries []MarkdownEntry, query string) []MarkdownEntry {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	if len(terms) == 0 {
		return entries
	}
	filtered := make([]MarkdownEntry, 0, len(entries))
	for _, entry := range entries {
		haystack := strings.ToLower(strings.Join([]string{
			entry.Label,
			entry.Description,
			entry.Group,
			entry.Content,
			entry.Path,
		}, "\n"))
		matched := true
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				matched = false
				break
			}
		}
		if matched {
			filtered = append(filtered, entry)
		}
	}
	return filtered
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
