package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

const (
	daemonsHeaderLines = 2
	daemonsFooterLines = 2
	maxDaemonEvents    = 14
	maxDaemonChats     = 8
)

type daemonPane int

const (
	daemonPaneList daemonPane = iota
	daemonPaneDetail
)

func (p daemonPane) label() string {
	switch p {
	case daemonPaneList:
		return "list"
	default:
		return "detail"
	}
}

// DaemonsModel renders the /daemons view: a read-only browser for one agent's
// daemon folders with Ctrl+T agent switching.
type DaemonsModel struct {
	baseDir     string // .lingtai/ directory (for agent discovery)
	orchDir     string // default agent dir
	selectedDir string // working dir of the currently displayed agent

	agentNodes []fs.AgentNode
	items      []daemonSummary
	selected   int
	loadErr    string

	listVP   viewport.Model
	detailVP viewport.Model
	pickerVP viewport.Model
	ready    bool
	width    int
	height   int
	focused  daemonPane

	pickerOpen bool
	pickerIdx  int
}

type daemonsLoadMsg struct {
	selectedDir string
	agentNodes  []fs.AgentNode
	items       []daemonSummary
	err         string
}

type daemonSummary struct {
	Dir          string
	Handle       string
	GroupID      string
	State        string
	Task         string
	Backend      string
	Preset       string
	StartedAt    string
	UpdatedAt    string
	CompletedAt  string
	FinishedAt   string
	CurrentTool  string
	Turn         int
	MaxTurns     int
	ElapsedS     float64
	Error        string
	Events       []daemonEvent
	Chats        []daemonChat
	Result       string
	EventCount   int
	ToolCount    int
	LastEventAt  string
	Tokens       daemonTokenSummary
	ModifiedTime time.Time
}

type daemonTokenSummary struct {
	Input    int
	Output   int
	Thinking int
	Cached   int
	Calls    int
}

type daemonEvent struct {
	TS      string
	Event   string
	Name    string
	Status  string
	Message string
	Error   string
	Raw     string
}

type daemonChat struct {
	TS   string
	Role string
	Kind string
	Turn string
	Text string
}

// NewDaemonsModel constructs the /daemons browser rooted at baseDir.
func NewDaemonsModel(baseDir, orchDir string) DaemonsModel {
	return DaemonsModel{
		baseDir:     baseDir,
		orchDir:     orchDir,
		selectedDir: orchDir,
	}
}

func (m DaemonsModel) Init() tea.Cmd {
	return m.loadData
}

func (m DaemonsModel) loadData() tea.Msg {
	net, _ := fs.BuildNetwork(m.baseDir)
	var nodes []fs.AgentNode
	for _, n := range net.Nodes {
		if n.IsHuman || n.WorkingDir == "" {
			continue
		}
		nodes = append(nodes, n)
	}

	selected := m.selectedDir
	if selected == "" {
		selected = m.orchDir
	}
	if selected == "" && len(nodes) > 0 {
		selected = nodes[0].WorkingDir
	}
	if selected == "" {
		return daemonsLoadMsg{agentNodes: nodes, err: i18n.T("daemons.no_agent")}
	}

	items, err := loadDaemonSummaries(selected)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	return daemonsLoadMsg{selectedDir: selected, agentNodes: nodes, items: items, err: errText}
}

func (m DaemonsModel) Update(msg tea.Msg) (DaemonsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.bodyHeight()
		if !m.ready {
			m.listVP = viewport.New()
			m.detailVP = viewport.New()
			m.pickerVP = viewport.New()
			m.focused = daemonPaneDetail
			m.ready = true
		}
		leftW, rightW := m.paneWidths()
		m.listVP.SetWidth(leftW)
		m.listVP.SetHeight(vpHeight)
		m.detailVP.SetWidth(rightW)
		m.detailVP.SetHeight(vpHeight)
		m.pickerVP.SetWidth(m.width)
		m.pickerVP.SetHeight(vpHeight)
		m.syncContent()
		m.syncPicker()
		return m, nil

	case daemonsLoadMsg:
		m.agentNodes = msg.agentNodes
		m.selectedDir = msg.selectedDir
		m.items = msg.items
		m.loadErr = msg.err
		if m.selected >= len(m.items) {
			m.selected = len(m.items) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		m.detailVP.GotoTop()
		m.syncContent()
		return m, nil

	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		if m.pickerOpen {
			m.pickerVP, cmd = m.pickerVP.Update(msg)
			return m, cmd
		}
		if m.focused == daemonPaneList {
			m.listVP, cmd = m.listVP.Update(msg)
			return m, cmd
		}
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd

	case tea.KeyPressMsg:
		if m.pickerOpen {
			return m.updatePicker(msg)
		}
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
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
		case "ctrl+r", "r":
			return m, m.loadData
		case "tab", "shift+tab":
			m.toggleFocusedPane()
			return m, nil
		case "left":
			m.focused = daemonPaneList
			return m, nil
		case "right":
			m.focused = daemonPaneDetail
			return m, nil
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.detailVP.GotoTop()
				m.syncContent()
			}
			return m, nil
		case "down", "j":
			if m.selected < len(m.items)-1 {
				m.selected++
				m.detailVP.GotoTop()
				m.syncContent()
			}
			return m, nil
		case "home":
			m.selected = 0
			m.detailVP.GotoTop()
			m.syncContent()
			return m, nil
		case "end":
			if len(m.items) > 0 {
				m.selected = len(m.items) - 1
			}
			m.detailVP.GotoTop()
			m.syncContent()
			return m, nil
		case "pgup", "ctrl+u":
			m.scrollFocusedPane(-10)
			return m, nil
		case "pgdown", "ctrl+d":
			m.scrollFocusedPane(10)
			return m, nil
		}
	}
	return m, nil
}

func (m DaemonsModel) updatePicker(msg tea.KeyPressMsg) (DaemonsModel, tea.Cmd) {
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
				m.selected = 0
				m.pickerOpen = false
				m.syncPicker()
				return m, m.loadData
			}
		}
		m.pickerOpen = false
		m.syncPicker()
		return m, nil
	}
	return m, nil
}

func (m *DaemonsModel) syncContent() {
	if !m.ready {
		return
	}
	leftW, rightW := m.paneWidths()
	m.listVP.SetWidth(leftW)
	m.detailVP.SetWidth(rightW)
	m.listVP.SetContent(m.renderList(leftW))
	m.detailVP.SetContent(m.renderDetail(rightW))
	m.ensureSelectedListVisible()
}

func (m DaemonsModel) bodyHeight() int {
	vpHeight := m.height - daemonsHeaderLines - daemonsFooterLines
	if vpHeight < 1 {
		vpHeight = 1
	}
	return vpHeight
}

func (m DaemonsModel) paneWidths() (int, int) {
	leftW := m.width / 3
	if leftW < 30 {
		leftW = 30
	}
	if leftW > 48 {
		leftW = 48
	}
	rightW := m.width - leftW - 3
	if rightW < 30 {
		leftW = m.width/2 - 2
		if leftW < 20 {
			leftW = 20
		}
		rightW = m.width - leftW - 3
	}
	return leftW, rightW
}

func (m *DaemonsModel) toggleFocusedPane() {
	if m.focused == daemonPaneList {
		m.focused = daemonPaneDetail
		return
	}
	m.focused = daemonPaneList
}

func (m *DaemonsModel) scrollFocusedPane(delta int) {
	if m.focused == daemonPaneList {
		m.listVP.SetYOffset(nonNegativeOffset(m.listVP.YOffset() + delta))
		return
	}
	m.detailVP.SetYOffset(nonNegativeOffset(m.detailVP.YOffset() + delta))
}

func nonNegativeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func (m *DaemonsModel) ensureSelectedListVisible() {
	if len(m.items) == 0 || m.listVP.Height() <= 0 {
		return
	}
	row := m.selectedListRow()
	off := m.listVP.YOffset()
	height := m.listVP.Height()
	if row < off {
		m.listVP.SetYOffset(row)
		return
	}
	if row >= off+height {
		m.listVP.SetYOffset(row - height + 1)
	}
}

func (m DaemonsModel) selectedListRow() int {
	row := 3 // title, count, blank
	for i := 0; i < m.selected && i < len(m.items); i++ {
		row++
		if daemonListDescription(m.items[i]) != "" {
			row++
		}
		if i != len(m.items)-1 {
			row++
		}
	}
	return row
}

func daemonListDescription(d daemonSummary) string {
	if d.Task != "" {
		return d.Task
	}
	return d.Backend
}

func (m *DaemonsModel) syncPicker() {
	if !m.ready || !m.pickerOpen {
		return
	}
	m.pickerVP.SetContent(m.renderPicker())
}

func (m DaemonsModel) View() string {
	title := fmt.Sprintf("%s — %s", i18n.T("daemons.title"), daemonAgentName(m.selectedDir))
	header := StyleTitle.Render("  "+title) + "\n" + strings.Repeat("─", m.width)
	scrollHint := ""
	if m.ready && !m.pickerOpen && m.focusedPaneCanScroll() {
		scrollHint = " " + RuneBullet + " pg scroll " + m.focused.label()
	}
	footerLine := "  ↑↓/jk " + i18n.T("manage.select") + " " + RuneBullet + " tab/←→ focus " + m.focused.label() + " " + RuneBullet + " pg scroll " + RuneBullet + " " + i18n.T("daemons.refresh") + " " + RuneBullet + " " + i18n.T("hints.props_select") + " " + RuneBullet + " esc " + i18n.T("manage.back") + scrollHint
	footer := strings.Repeat("─", m.width) + "\n" + StyleFaint.Render(footerLine)
	body := ""
	if m.ready {
		if m.pickerOpen {
			body = m.pickerVP.View()
		} else {
			body = m.renderPanes()
		}
	}
	return header + "\n" + PaintViewportBG(body, m.width) + "\n" + footer
}

func (m DaemonsModel) focusedPaneCanScroll() bool {
	if m.focused == daemonPaneList {
		return !m.listVP.AtBottom()
	}
	return !m.detailVP.AtBottom()
}

func (m DaemonsModel) renderPanes() string {
	if m.width <= 0 {
		return ""
	}
	if m.loadErr != "" {
		return "\n  " + StyleFaint.Render(m.loadErr)
	}
	if len(m.items) == 0 {
		return "\n  " + StyleFaint.Render(i18n.T("daemons.no_daemons"))
	}

	leftW, _ := m.paneWidths()
	sep := StyleFaint.Render(" │ ")
	leftLines := strings.Split(m.listVP.View(), "\n")
	rightLines := strings.Split(m.detailVP.View(), "\n")
	for len(leftLines) < len(rightLines) {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < len(leftLines) {
		rightLines = append(rightLines, "")
	}
	var b strings.Builder
	for i := range leftLines {
		b.WriteString(padToWidth(leftLines[i], leftW) + sep + rightLines[i])
		if i != len(leftLines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m DaemonsModel) renderList(maxW int) string {
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	muted := StyleFaint
	selectedStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	var lines []string
	lines = append(lines, sectionStyle.Render(i18n.T("daemons.list_title")))
	lines = append(lines, muted.Render(fmt.Sprintf(i18n.T("daemons.count"), len(m.items))))
	lines = append(lines, "")
	for i, d := range m.items {
		marker := "  "
		style := lipgloss.NewStyle().Foreground(ColorText)
		if i == m.selected {
			marker = "> "
			style = selectedStyle
		}
		state := d.State
		if state == "" {
			state = "──"
		}
		stateRendered := lipgloss.NewStyle().Foreground(StateColor(strings.ToUpper(state))).Render(state)
		line := fmt.Sprintf("%s%-8s %s", marker, d.Handle, stateRendered)
		lines = append(lines, style.Render(truncateForPanel(line, maxW)))
		desc := d.Task
		if d.GroupID != "" {
			if desc != "" {
				desc = d.GroupID + " · " + desc
			} else {
				desc = d.GroupID
			}
		}
		if desc == "" {
			desc = d.Backend
		}
		if desc != "" {
			lines = append(lines, "  "+muted.Render(truncateForPanel(desc, maxW-2)))
		}
		if i != len(m.items)-1 {
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

func (m DaemonsModel) renderDetail(maxW int) string {
	if len(m.items) == 0 || m.selected >= len(m.items) {
		return ""
	}
	d := m.items[m.selected]
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)
	muted := StyleFaint
	var lines []string
	lines = append(lines, sectionStyle.Render(i18n.T("daemons.detail_title")))
	lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("dir:"), valueStyle.Render(filepath.Base(d.Dir))))
	lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("state:"), lipgloss.NewStyle().Foreground(StateColor(strings.ToUpper(d.State))).Render(nonEmpty(d.State, "──"))))
	if d.Error != "" {
		lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render(i18n.T("daemons.error")+":"), valueStyle.Render(d.Error)))
	}
	lines = append(lines, "")
	lines = append(lines, sectionStyle.Render(i18n.T("daemons.metadata")))
	meta := []struct{ label, value string }{
		{i18n.T("daemons.group_id"), d.GroupID},
		{i18n.T("daemons.backend"), d.Backend},
		{i18n.T("daemons.preset"), d.Preset},
		{i18n.T("daemons.current_tool"), d.CurrentTool},
		{i18n.T("daemons.turn"), turnText(d.Turn, d.MaxTurns)},
		{i18n.T("daemons.duration"), daemonDuration(d)},
		{i18n.T("daemons.started"), d.StartedAt},
		{i18n.T("daemons.updated"), d.UpdatedAt},
		{i18n.T("daemons.completed"), d.CompletedAt},
		{i18n.T("daemons.finished"), d.FinishedAt},
		{i18n.T("daemons.last_event"), d.LastEventAt},
		{i18n.T("daemons.tokens"), daemonTokenText(d.Tokens)},
	}
	for _, row := range meta {
		if strings.TrimSpace(row.value) == "" || row.value == "0" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render(row.label+":"), valueStyle.Render(row.value)))
	}
	lines = append(lines, fmt.Sprintf("%s %s", labelStyle.Render("events:"), valueStyle.Render(fmt.Sprintf("%d (%d tools)", d.EventCount, d.ToolCount))))

	// Important information first: task → interactions → result. The raw
	// event/trajectory log goes last so it never buries the summary above it.
	lines = append(lines, "")
	lines = append(lines, sectionStyle.Render(i18n.T("daemons.task")))
	lines = appendWrappedFull(lines, d.Task, maxW, "  ")
	if len(d.Chats) > 0 {
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render(i18n.T("daemons.interactions")))
		for _, c := range d.Chats {
			who := c.Role
			if c.Kind != "" {
				who += "/" + c.Kind
			}
			headerParts := []string{shortTime(c.TS), who}
			if c.Turn != "" {
				headerParts = append(headerParts, "turn="+c.Turn)
			}
			lines = append(lines, muted.Render("  "+strings.TrimSpace(strings.Join(headerParts, " "))))
			lines = appendWrappedFull(lines, c.Text, maxW, "    ")
		}
	}
	if d.Result != "" {
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render(i18n.T("daemons.result")))
		lines = appendWrappedFull(lines, d.Result, maxW, "  ")
	}
	if len(d.Events) > 0 {
		lines = append(lines, "")
		lines = append(lines, sectionStyle.Render(i18n.T("daemons.events")))
		for _, ev := range d.Events {
			label := strings.TrimSpace(strings.Join([]string{shortTime(ev.TS), ev.Event, ev.Name, ev.Status}, " "))
			if label == "" {
				label = "event"
			}
			lines = append(lines, muted.Render("  "+label))
			body := ev.Raw
			if body == "" {
				body = firstNonEmpty(ev.Message, ev.Error)
			}
			lines = appendWrappedFull(lines, body, maxW, "    ")
		}
	}
	return strings.Join(lines, "\n")
}

func (m DaemonsModel) renderPicker() string {
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	selectedStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.select_agent")))
	lines = append(lines, "")
	if len(m.agentNodes) == 0 {
		lines = append(lines, "  "+StyleFaint.Render(i18n.T("daemons.no_agent")))
		lines = append(lines, "")
		lines = append(lines, "  "+StyleFaint.Render("[esc/ctrl+t] "+i18n.T("manage.back")))
		return strings.Join(lines, "\n")
	}
	for i, n := range m.agentNodes {
		name := agentDisplayName(n)
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

func loadDaemonSummaries(agentDir string) ([]daemonSummary, error) {
	daemonsDir := filepath.Join(agentDir, "daemons")
	entries, err := os.ReadDir(daemonsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var items []daemonSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(daemonsDir, entry.Name())
		item, err := readDaemonSummary(dir)
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ModifiedTime.After(items[j].ModifiedTime)
	})
	return items, nil
}

func readDaemonSummary(dir string) (daemonSummary, error) {
	info, _ := os.Stat(dir)
	item := daemonSummary{Dir: dir, Handle: daemonHandle(filepath.Base(dir))}
	if info != nil {
		item.ModifiedTime = info.ModTime()
	}
	data, err := os.ReadFile(filepath.Join(dir, "daemon.json"))
	if err != nil {
		return item, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return item, err
	}
	item.Task = stringField(raw, "task")
	item.GroupID = stringField(raw, "group_id")
	item.State = stringField(raw, "state")
	item.Backend = stringField(raw, "backend")
	item.Preset = daemonPreset(raw)
	item.StartedAt = stringField(raw, "started_at")
	item.UpdatedAt = stringField(raw, "updated_at")
	item.CompletedAt = stringField(raw, "completed_at")
	item.FinishedAt = stringField(raw, "finished_at")
	if item.CompletedAt == "" {
		item.CompletedAt = item.FinishedAt
	}
	item.CurrentTool = stringField(raw, "current_tool")
	item.Error = stringField(raw, "error")
	item.Turn = intField(raw, "turn")
	item.MaxTurns = intField(raw, "max_turns")
	item.ElapsedS = floatField(raw, "elapsed_s")
	item.Events, item.EventCount, item.ToolCount, item.LastEventAt = readDaemonEvents(filepath.Join(dir, "logs", "events.jsonl"))
	item.Chats = readDaemonChats(filepath.Join(dir, "history", "chat_history.jsonl"))
	item.Result = readDaemonResult(filepath.Join(dir, "result.txt"))
	item.Tokens = readDaemonTokens(filepath.Join(dir, "logs", "token_ledger.jsonl"))
	if item.ModifiedTime.IsZero() {
		item.ModifiedTime = newestTimestamp(item.StartedAt, item.UpdatedAt, item.CompletedAt, item.FinishedAt, item.LastEventAt)
	}
	return item, nil
}

func readDaemonEvents(path string) ([]daemonEvent, int, int, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, ""
	}
	lines := splitLines(string(data))
	toolCount := 0
	lastTS := ""
	var events []daemonEvent
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var raw map[string]any
		_ = json.Unmarshal([]byte(trimmed), &raw)
		event := daemonEvent{
			TS:      firstNonEmpty(stringField(raw, "ts"), stringField(raw, "timestamp")),
			Event:   firstNonEmpty(stringField(raw, "event"), stringField(raw, "type")),
			Name:    firstNonEmpty(stringField(raw, "name"), stringField(raw, "tool")),
			Status:  stringField(raw, "status"),
			Message: firstNonEmpty(stringField(raw, "message"), stringField(raw, "text")),
			Error:   stringField(raw, "error"),
			Raw:     trimmed,
		}
		if event.Event == "tool_call" || event.Event == "tool_result" {
			toolCount++
		}
		if event.TS != "" {
			lastTS = event.TS
		}
		events = append(events, event)
	}
	return events, len(events), toolCount, lastTS
}

func readDaemonChats(path string) []daemonChat {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var chats []daemonChat
	for _, line := range splitLines(string(data)) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			chats = append(chats, daemonChat{Text: trimmed})
			continue
		}
		text := stringField(raw, "text")
		if text == "" {
			text = trimmed
		}
		chats = append(chats, daemonChat{
			TS:   stringField(raw, "ts"),
			Role: stringField(raw, "role"),
			Kind: stringField(raw, "kind"),
			Turn: stringField(raw, "turn"),
			Text: text,
		})
	}
	return chats
}

func readDaemonResult(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func readDaemonTokens(path string) daemonTokenSummary {
	data, err := os.ReadFile(path)
	if err != nil {
		return daemonTokenSummary{}
	}
	var total daemonTokenSummary
	for _, line := range splitLines(string(data)) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			continue
		}
		total.Input += intField(raw, "input")
		total.Output += intField(raw, "output")
		total.Thinking += intField(raw, "thinking")
		total.Cached += intField(raw, "cached")
		total.Calls++
	}
	return total
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func daemonHandle(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) >= 2 && parts[0] == "em" {
		return parts[0] + "-" + parts[1]
	}
	return name
}

func daemonAgentName(agentDir string) string {
	if agentDir == "" {
		return ""
	}
	if node, err := fs.ReadAgent(agentDir); err == nil {
		return agentDisplayName(node)
	}
	return filepath.Base(agentDir)
}

func agentDisplayName(n fs.AgentNode) string {
	if n.Nickname != "" {
		return n.Nickname
	}
	if n.AgentName != "" {
		return n.AgentName
	}
	if n.Address != "" {
		return n.Address
	}
	return filepath.Base(n.WorkingDir)
}

func stringField(raw map[string]any, key string) string {
	v, ok := raw[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return fmt.Sprint(v)
}

func intField(raw map[string]any, key string) int {
	v, ok := raw[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(x)
		return n
	default:
		return 0
	}
}

func floatField(raw map[string]any, key string) float64 {
	v, ok := raw[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	default:
		return 0
	}
}

// daemonPreset derives an operator-visible preset label from daemon.json.
// preset_name is authoritative when present; otherwise fall back through the
// preset's provider/model, then the run's model, so the row is still useful
// for older daemons that predate preset_name (where it is null/absent).
func daemonPreset(raw map[string]any) string {
	if name := stringField(raw, "preset_name"); name != "" {
		return name
	}
	provider := stringField(raw, "preset_provider")
	model := stringField(raw, "preset_model")
	switch {
	case provider != "" && model != "":
		return provider + ":" + model
	case model != "":
		return model
	case provider != "":
		return provider
	}
	return stringField(raw, "model")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func turnText(turn, maxTurns int) string {
	if turn == 0 && maxTurns == 0 {
		return ""
	}
	if maxTurns == 0 {
		return fmt.Sprintf("%d", turn)
	}
	return fmt.Sprintf("%d/%d", turn, maxTurns)
}

func daemonDuration(d daemonSummary) string {
	if d.ElapsedS > 0 {
		return formatSeconds(d.ElapsedS)
	}
	start, ok := parseDaemonTime(d.StartedAt)
	if !ok {
		return ""
	}
	endText := firstNonEmpty(d.CompletedAt, d.FinishedAt, d.UpdatedAt, d.LastEventAt)
	end, ok := parseDaemonTime(endText)
	if !ok || end.Before(start) {
		return ""
	}
	return formatSeconds(end.Sub(start).Seconds())
}

func daemonTokenText(t daemonTokenSummary) string {
	if t.Calls == 0 {
		return ""
	}
	total := t.Input + t.Output + t.Thinking
	return fmt.Sprintf("%d calls / %d tokens (in %d, out %d, think %d, cached %d)", t.Calls, total, t.Input, t.Output, t.Thinking, t.Cached)
}

func parseDaemonTime(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func formatSeconds(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	d := time.Duration(seconds * float64(time.Second)).Round(time.Second)
	if d < time.Second {
		return "<1s"
	}
	return d.String()
}

func shortTime(ts string) string {
	if len(ts) >= 19 {
		return strings.ReplaceAll(ts[:19], "T", " ")
	}
	return ts
}

func appendWrappedFull(lines []string, text string, maxW int, prefix string) []string {
	if text == "" {
		return append(lines, prefix+StyleFaint.Render("—"))
	}
	width := maxW - lipgloss.Width(prefix)
	if width < 20 {
		width = 20
	}
	for _, rawLine := range splitLines(text) {
		if rawLine == "" {
			lines = append(lines, prefix)
			continue
		}
		for rawLine != "" {
			chunk, rest := splitByDisplayWidth(rawLine, width)
			lines = append(lines, prefix+chunk)
			rawLine = rest
		}
	}
	return lines
}

func splitByDisplayWidth(s string, width int) (string, string) {
	if width <= 0 {
		return "", s
	}
	var b strings.Builder
	used := 0
	for idx, r := range s {
		rw := lipgloss.Width(string(r))
		if used > 0 && used+rw > width {
			return b.String(), s[idx:]
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String(), ""
}

func newestTimestamp(values ...string) time.Time {
	var newest time.Time
	for _, v := range values {
		if v == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil && t.After(newest) {
			newest = t
		}
	}
	return newest
}
