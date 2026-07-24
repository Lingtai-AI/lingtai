package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// agentSelectorRow is one canonical conversation row: the synthetic Main row or
// one safe same-project direct target. Every selection surface consumes these
// rows; nothing else discovers targets.
type agentSelectorRow struct {
	Label  string
	Target fs.DirectTarget
	Main   bool
}

// agentSelectorState is the one Mail-owned canonical conversation catalog and
// selection: accepted safe rows, the mutable selector cursor, the stable
// current thread key, and the /agents overlay open state. It owns no rail
// geometry, focus, scroll window, or badge state; the visible rail is only a
// presentation over this canonical state.
type agentSelectorState struct {
	rows              []agentSelectorRow
	cursor            int
	selectorOpen      bool
	selectedThreadKey string
}

// discoverAgentSelectorRows enumerates safe direct-conversation rows beneath
// the current project's .lingtai directory. It intentionally reads only
// immediate manifest directories and does not consult process or inventory
// state. Human, blank-identity, blank-route, and duplicate stable-identity
// candidates fail closed: an ambiguous duplicate omits the entire group.
func discoverAgentSelectorRows(projectDir string) []agentSelectorRow {
	projectRoot := filepath.Clean(filepath.Dir(projectDir))

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return []agentSelectorRow{{Label: i18n.T("agent_selector.main"), Main: true}}
	}

	type candidate struct {
		row       agentSelectorRow
		threadKey string
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		workingDir := filepath.Join(projectDir, entry.Name())
		if _, err := os.Lstat(filepath.Join(workingDir, ".agent.json")); err != nil {
			continue
		}

		agent, err := fs.ReadAgent(workingDir)
		if err != nil {
			continue
		}
		if agent.IsHuman {
			continue
		}

		address := strings.TrimSpace(agent.Address)
		target := fs.DirectTarget{
			ProjectDirectory: projectRoot,
			Directory:        filepath.Clean(agent.WorkingDir),
			AgentID:          agent.AgentID,
			Address:          address,
		}
		if strings.TrimSpace(target.AgentID) == "" ||
			strings.TrimSpace(target.Directory) == "" ||
			strings.TrimSpace(target.ProjectDirectory) == "" ||
			target.Address == "" {
			continue
		}

		threadKey := fs.DirectThreadKey(target)
		if threadKey == "" {
			continue
		}

		label := strings.TrimSpace(agent.Nickname)
		if label == "" {
			label = strings.TrimSpace(agent.AgentName)
		}
		if label == "" {
			label = target.Address
		}
		candidates = append(candidates, candidate{
			row:       agentSelectorRow{Label: label, Target: target},
			threadKey: threadKey,
		})
	}

	keyCounts := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		keyCounts[candidate.threadKey]++
	}

	ordinary := make([]agentSelectorRow, 0, len(candidates))
	for _, candidate := range candidates {
		if keyCounts[candidate.threadKey] > 1 {
			continue
		}
		ordinary = append(ordinary, candidate.row)
	}
	sort.Slice(ordinary, func(i, j int) bool {
		left, right := ordinary[i], ordinary[j]
		if foldedLeft, foldedRight := strings.ToLower(left.Label), strings.ToLower(right.Label); foldedLeft != foldedRight {
			return foldedLeft < foldedRight
		}
		if left.Label != right.Label {
			return left.Label < right.Label
		}
		if left.Target.AgentID != right.Target.AgentID {
			return left.Target.AgentID < right.Target.AgentID
		}
		return left.Target.Directory < right.Target.Directory
	})

	rows := make([]agentSelectorRow, 0, len(ordinary)+1)
	rows = append(rows, agentSelectorRow{Label: i18n.T("agent_selector.main"), Main: true})
	rows = append(rows, ordinary...)
	return rows
}

// installSelectorRows publishes a command-prepared canonical catalog without
// manifest discovery. The mutable cursor survives the row replacement by
// stable identity — the synthetic Main row or the row's fs.DirectThreadKey —
// independently of the current selection.
func (m MailModel) installSelectorRows(rows []agentSelectorRow) MailModel {
	cursorKey := m.agentSelector.cursorThreadKey()
	m.agentSelector.rows = rows
	m.agentSelector.restoreCursorByThreadKey(cursorKey)
	return m
}

func directTargetsForRows(rows []agentSelectorRow) []fs.DirectTarget {
	targets := make([]fs.DirectTarget, 0, len(rows))
	for _, row := range rows {
		if !row.Main {
			targets = append(targets, row.Target)
		}
	}
	return targets
}

// activateConversationRow is the one canonical activation path shared by every
// selection surface. Main restores the exact stored Main compose and clears the
// current direct state; an ordinary row installs a fresh current-only direct
// conversation and returns its deferred visibility command.
func (m MailModel) activateConversationRow(index int) (MailModel, tea.Cmd) {
	if index < 0 || index >= len(m.agentSelector.rows) {
		return m, nil
	}
	row := m.agentSelector.rows[index]
	m.agentSelector.cursor = index
	if row.Main {
		m.agentSelector.selectedThreadKey = ""
		m = m.restoreMainCompose()
		m.directChat = clearedDirectChat(m.directChat)
		m.lastInputLines = -1
		m.syncViewportHeight()
		return m, nil
	}
	m = m.installFreshDirectCompose()
	return m.activateDirectTarget(row.Target)
}

// openAgentSelector exposes the existing canonical rows as the /agents overlay
// at every terminal width. It changes no selection or direct projection.
func (m MailModel) openAgentSelector() MailModel {
	m.agentSelector.selectorOpen = true
	m.input.Blur()
	return m
}

// updateAgentSelector owns keys while the /agents overlay is open. Cursor
// movement never activates; Enter/Space go through the one canonical
// activation function; Esc cancels without changing the current conversation.
func (m MailModel) updateAgentSelector(msg tea.KeyPressMsg) (MailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.agentSelector.selectorOpen = false
		_ = m.input.Focus()
		return m, m.currentDirectVisibilityCmd()
	case "up", "k":
		return m.moveSelectorCursor(-1), nil
	case "down", "j":
		return m.moveSelectorCursor(1), nil
	case "home":
		return m.setSelectorCursor(0), nil
	case "end":
		return m.setSelectorCursor(len(m.agentSelector.rows) - 1), nil
	case "enter":
		m.agentSelector.selectorOpen = false
		_ = m.input.Focus()
		return m.activateConversationRow(m.agentSelector.cursor)
	}
	if msg.Code == ' ' {
		m.agentSelector.selectorOpen = false
		_ = m.input.Focus()
		return m.activateConversationRow(m.agentSelector.cursor)
	}
	return m, nil
}

// renderAgentSelector is the compact Mail-owned overlay over the stored
// canonical rows and cursor. It performs no discovery or I/O.
func (m MailModel) renderAgentSelector() string {
	width := m.width - 4
	if width < 16 {
		width = m.width
	}
	if width < 1 {
		return ""
	}
	lines := []string{i18n.T("agent_selector.title")}
	for index, row := range m.agentSelector.rows {
		current := row.Main && m.agentSelector.selectedThreadKey == "" ||
			!row.Main && fs.DirectThreadKey(row.Target) == m.agentSelector.selectedThreadKey
		prefix := selectorRowPrefix(current, index == m.agentSelector.cursor)
		lines = append(lines, prefix+ansi.Truncate(row.Label, width-2, ""))
	}
	lines = append(lines, i18n.T("agent_selector.hint"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(width).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// cursorThreadKey names the stable identity currently under the mutable
// cursor: "" for the synthetic Main row (or an out-of-range cursor), otherwise
// the row's stable thread key.
func (s agentSelectorState) cursorThreadKey() string {
	if s.cursor < 0 || s.cursor >= len(s.rows) || s.rows[s.cursor].Main {
		return ""
	}
	return fs.DirectThreadKey(s.rows[s.cursor].Target)
}

// restoreCursorByThreadKey moves the cursor to the row carrying the given
// stable identity ("" is the synthetic Main row). A vanished identity falls
// back deterministically to the leading Main row.
func (s *agentSelectorState) restoreCursorByThreadKey(key string) {
	for index, row := range s.rows {
		if row.Main && key == "" || !row.Main && fs.DirectThreadKey(row.Target) == key {
			s.cursor = index
			return
		}
	}
	s.cursor = 0
	s.normalizeCursor()
}

func (s *agentSelectorState) normalizeCursor() {
	if len(s.rows) == 0 {
		s.cursor = 0
		return
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor >= len(s.rows) {
		s.cursor = len(s.rows) - 1
	}
}

func (m MailModel) moveSelectorCursor(delta int) MailModel {
	if len(m.agentSelector.rows) == 0 {
		return m
	}
	m.agentSelector.cursor = (m.agentSelector.cursor + delta) % len(m.agentSelector.rows)
	if m.agentSelector.cursor < 0 {
		m.agentSelector.cursor += len(m.agentSelector.rows)
	}
	return m
}

func (m MailModel) setSelectorCursor(index int) MailModel {
	if len(m.agentSelector.rows) == 0 {
		return m
	}
	if index < 0 {
		index = 0
	}
	if index >= len(m.agentSelector.rows) {
		index = len(m.agentSelector.rows) - 1
	}
	m.agentSelector.cursor = index
	return m
}

// selectorRowPrefix marks the current conversation and the mutable cursor as
// independent visible signals.
func selectorRowPrefix(current, cursor bool) string {
	switch {
	case current && cursor:
		return ">•"
	case cursor:
		return "> "
	case current:
		return "• "
	default:
		return "  "
	}
}
