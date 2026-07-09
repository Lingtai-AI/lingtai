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

	inner MarkdownViewerModel

	// allEntries is the unfiltered set for the current owner; the viewer is
	// rebuilt from filterMailboxEntries(allEntries, searchQuery). Keeping the
	// source of truth here (not inside the viewer) lets search restore the full
	// list on Esc without rescanning disk.
	allEntries []MarkdownEntry

	// searchMode is true only while the query is being edited; searchQuery
	// persists after Enter so a committed filter survives list navigation.
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
	m.rebuildInner()
	return m
}

// rebuildInner rebuilds the markdown viewer from allEntries filtered by the
// current searchQuery and refreshes the mailbox footer hint. While a non-empty
// filter is active it expands every group so a match in any group stays visible;
// when the query is empty it leaves the shared viewer's default (first group
// expanded, the rest collapsed) intact, preserving normal idle/reload/
// agent-switch behavior and keeping a many-group sidebar to one screen.
// Callers must re-send WindowSizeMsg afterwards (see syncInnerSize) to size
// the freshly constructed viewer's viewports.
func (m *MailboxModel) rebuildInner() {
	entries := filterMailboxEntries(m.allEntries, m.searchQuery)
	m.inner = NewMarkdownViewer(entries, mailboxTitleFor(m.selectedDir))
	m.inner.FooterHint = m.mailboxFooterHint()
	// Only expand every group while a non-empty filter needs to expose matches
	// across groups; an empty query keeps the shared viewer's first-group-only
	// default. This is same-package field access rather than a new viewer-level
	// option: the only caller is here, so an exported ExpandAllGroups seam
	// would not earn its surface.
	if strings.TrimSpace(m.searchQuery) != "" {
		for _, g := range m.inner.groupOrder {
			m.inner.expanded[g] = true
		}
	}
}

// syncInnerSize re-applies the last known window size to the inner viewer,
// returning any command it produces. No-op until the first WindowSizeMsg.
func (m *MailboxModel) syncInnerSize() tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}
	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	return cmd
}

// mailboxFooterHint drives the trailing hint segment the shared viewer renders
// via its FooterHint seam. It advertises the search key when idle and echoes
// the live query and match counts while editing or while a filter is applied.
func (m MailboxModel) mailboxFooterHint() string {
	query := strings.TrimSpace(m.searchQuery)
	shown := truncate(query, 28)
	matched := len(m.inner.entries)
	total := len(m.allEntries)
	if m.searchMode {
		return fmt.Sprintf(i18n.T("mailbox.search_hint"), shown, matched, total)
	}
	if query != "" {
		return fmt.Sprintf(i18n.T("mailbox.filter_hint"), shown, matched, total)
	}
	return i18n.T("hints.mailbox")
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
	m.rebuildInner()
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
		}
		// Esc clears an applied filter first; a second Esc then exits /mailbox
		// via the shared viewer's MarkdownViewerCloseMsg, preserving the old
		// contract when no filter is active.
		if msg.String() == "esc" && strings.TrimSpace(m.searchQuery) != "" {
			m.searchQuery = ""
			m.rebuildInner()
			return m, m.syncInnerSize()
		}
		// "/" or Ctrl+F opens the mailbox-local search editor.
		if isMailboxSearchKey(msg) {
			m.searchMode = true
			m.inner.FooterHint = m.mailboxFooterHint()
			return m, nil
		}
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd

	case tea.PasteMsg:
		if m.searchMode {
			m.searchQuery += normalizeMailboxSearchText(msg.Content)
			m.rebuildInner()
			return m, m.syncInnerSize()
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

// updateSearch handles keys while the mailbox search editor is open. The query
// filters live; Enter commits the current filter (leaving edit mode but keeping
// the result set), Esc cancels (drops the query and clears the filter).
func (m MailboxModel) updateSearch(msg tea.KeyPressMsg) (MailboxModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchQuery = ""
		m.searchMode = false
		m.rebuildInner()
		return m, m.syncInnerSize()
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
		m.rebuildInner()
		return m, m.syncInnerSize()
	case "ctrl+u":
		if m.searchQuery == "" {
			return m, nil
		}
		m.searchQuery = ""
		m.rebuildInner()
		return m, m.syncInnerSize()
	}
	if msg.Text != "" {
		m.searchQuery += normalizeMailboxSearchText(msg.Text)
		m.rebuildInner()
		return m, m.syncInnerSize()
	}
	return m, nil
}

// isMailboxSearchKey reports whether a keypress should open the mailbox search
// editor: "/" (the vim/mail-style search prefix) or Ctrl+F.
func isMailboxSearchKey(msg tea.KeyPressMsg) bool {
	if msg.Text == "/" {
		return true
	}
	k := msg.Key()
	return k.Code == 'f' && k.Mod == tea.ModCtrl
}

// normalizeMailboxSearchText collapses newlines/tabs to spaces and drops other
// control bytes so a pasted block can't smuggle non-printable chars into the
// query or the footer hint.
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

// filterMailboxEntries returns the entries whose label, description, group,
// path, or rendered content contain every whitespace-separated term (case
// insensitive, AND semantics). An empty query returns the input unchanged.
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
				// Switching owner invalidates any active search: rebuild from a
				// fresh, unfiltered scan of the new agent's mailbox.
				m.searchQuery = ""
				m.searchMode = false
				m.allEntries = buildMailboxEntries(m.selectedDir)
				m.rebuildInner()
				m.pickerOpen = false
				m.syncPicker()
				return m, m.syncInnerSize()
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
