package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

// projectEntry holds a registered project and its loaded details.
type projectEntry struct {
	Path    string
	Name    string     // basename of the project directory
	Network fs.Network // loaded on select
	Current bool       // true if this is the TUI's current project
}

// projectSource determines where the projects list comes from.
type projectSource int

const (
	projectSourceRegistry projectSource = iota // running-agent inventory from the process table
	projectSourceAgora                         // exported networks from ~/lingtai-agora/networks/
)

// ProjectsModel is a two-panel view: project list (left) + agent details (right).
type ProjectsModel struct {
	globalDir    string
	projectDir   string // current TUI project's .lingtai/ directory
	ctx          ProjectsContext
	source       projectSource
	activationID uint64
	requestSeq   uint64
	width        int
	height       int

	projects []projectEntry
	snapshot inventory.Snapshot
	rows     []projectRow
	cursor   int
	loadErr  string
	status   string

	// Right panel viewport
	viewport viewport.Model
	ready    bool
}

type ProjectsContext struct {
	FocusedAgentDir    string
	OriginalProjectDir string
	OriginalAgentDir   string
	Visiting           bool
}

type projectRowKind int

const (
	projectRowGroup projectRowKind = iota
	projectRowAgent
)

type projectRow struct {
	kind    projectRowKind
	project string
	phantom bool
	record  inventory.Record
}

type ProjectsAgentSelectedMsg struct {
	ActivationID uint64
	RequestSeq   uint64
	Record       inventory.Record
}

func NewProjectsModel(globalDir, projectDir string, ctx ProjectsContext) ProjectsModel {
	return NewProjectsModelWithActivation(globalDir, projectDir, ctx, 0)
}

func NewProjectsModelWithActivation(globalDir, projectDir string, ctx ProjectsContext, activationID uint64) ProjectsModel {
	return ProjectsModel{
		globalDir:    globalDir,
		projectDir:   projectDir,
		ctx:          ctx,
		source:       projectSourceRegistry,
		activationID: activationID,
		requestSeq:   1,
	}
}

// NewAgoraProjectsModel creates a ProjectsModel that scans ~/lingtai-agora/networks/.
func NewAgoraProjectsModel(globalDir, projectDir string) ProjectsModel {
	return NewAgoraProjectsModelWithActivation(globalDir, projectDir, 0)
}

func NewAgoraProjectsModelWithActivation(globalDir, projectDir string, activationID uint64) ProjectsModel {
	return ProjectsModel{
		globalDir:    globalDir,
		projectDir:   projectDir,
		source:       projectSourceAgora,
		activationID: activationID,
		requestSeq:   1,
	}
}

// SetSize updates the model's dimensions. Used by parent models
// that relay window size.
func (m *ProjectsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	vpHeight := h - projectsHeaderLines - projectsFooterLines
	if vpHeight < 1 {
		vpHeight = 1
	}
	if !m.ready {
		m.viewport = viewport.New()
		m.viewport.SetWidth(w)
		m.viewport.SetHeight(vpHeight)
		m.ready = true
	} else {
		m.viewport.SetWidth(w)
		m.viewport.SetHeight(vpHeight)
	}
	m.syncViewportContent()
}

// projectsLoadMsg carries the loaded project list.
type projectsLoadMsg struct {
	activationID uint64
	requestSeq   uint64
	projects     []projectEntry
}

type projectsInventoryMsg struct {
	activationID uint64
	requestSeq   uint64
	snapshot     inventory.Snapshot
	err          string
}

type projectsValidationMsg struct {
	activationID uint64
	requestSeq   uint64
	identity     inventory.AgentIdentity
	snapshot     inventory.Snapshot
	record       inventory.Record
	err          string
	valid        bool
}

// agoraDetailMsg is sent when the user presses Enter on a network/recipe in agora mode.
type agoraDetailMsg struct {
	activationID uint64
	name         string // display name
	dir          string // path to recipe directory
}

// agoraTabToggleMsg is sent when the user presses Ctrl+T in agora mode.
type agoraTabToggleMsg struct {
	activationID uint64
}

const (
	projectsHeaderLines  = 2
	projectsFooterLines  = 2
	projectsListTopLines = 3
)

var projectsScanInventory = inventory.Scan

func (m *ProjectsModel) nextRequestSeq() uint64 {
	m.requestSeq++
	if m.requestSeq == 0 {
		m.requestSeq = 1
	}
	return m.requestSeq
}

func (m ProjectsModel) loadData(requestSeq uint64) tea.Cmd {
	if requestSeq == 0 {
		requestSeq = 1
	}
	return func() tea.Msg {
		return m.loadDataMsg(requestSeq)
	}
}

func (m ProjectsModel) loadDataMsg(requestSeq uint64) tea.Msg {
	if m.source == projectSourceAgora {
		var paths []string
		paths = scanAgoraNetworks()
		currentProject := filepath.Dir(m.projectDir) // .lingtai/ → parent

		var projects []projectEntry
		for _, p := range paths {
			entry := projectEntry{
				Path:    p,
				Name:    filepath.Base(p),
				Current: p == currentProject,
			}
			// Load network info for each project
			lingtaiDir := filepath.Join(p, ".lingtai")
			net, _ := fs.BuildNetwork(lingtaiDir)
			entry.Network = net
			projects = append(projects, entry)
		}
		return projectsLoadMsg{activationID: m.activationID, requestSeq: requestSeq, projects: projects}
	}
	snap, err := projectsScanInventory(inventory.Options{SelfPID: os.Getpid()})
	if err != nil {
		return projectsInventoryMsg{activationID: m.activationID, requestSeq: requestSeq, err: err.Error()}
	}
	humanizeSnapshotUptime(&snap)
	return projectsInventoryMsg{activationID: m.activationID, requestSeq: requestSeq, snapshot: snap}
}

// scanAgoraNetworks returns paths to all directories under ~/lingtai-agora/networks/
// that contain a .lingtai/ subdirectory. Falls back to ~/lingtai-agora/projects/
// for backward compatibility with pre-export naming.
func scanAgoraNetworks() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	// Try networks/ first, fall back to legacy projects/
	agoraDir := filepath.Join(home, "lingtai-agora", "networks")
	entries, err := os.ReadDir(agoraDir)
	if err != nil {
		// Fallback: try legacy projects/ path
		agoraDir = filepath.Join(home, "lingtai-agora", "projects")
		entries, err = os.ReadDir(agoraDir)
		if err != nil {
			return nil
		}
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(agoraDir, e.Name())
		// Only include if it has .lingtai/ (is a valid published network)
		if info, err := os.Stat(filepath.Join(p, ".lingtai")); err == nil && info.IsDir() {
			paths = append(paths, p)
		}
	}
	return paths
}

func (m ProjectsModel) Init() tea.Cmd { return m.loadData(m.requestSeq) }

func (m ProjectsModel) Update(msg tea.Msg) (ProjectsModel, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.height - projectsHeaderLines - projectsFooterLines
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !m.ready {
			m.viewport = viewport.New()
			m.viewport.SetWidth(m.width)
			m.viewport.SetHeight(vpHeight)
			m.ready = true
		} else {
			m.viewport.SetWidth(m.width)
			m.viewport.SetHeight(vpHeight)
		}
		m.syncViewportContent()

	case projectsLoadMsg:
		if !m.acceptsRequest(msg.activationID, msg.requestSeq) {
			return m, nil
		}
		m.projects = msg.projects
		if m.cursor >= len(m.projects) {
			m.cursor = max(0, len(m.projects)-1)
		}
		m.syncViewportContent()

	case projectsInventoryMsg:
		if !m.acceptsRequest(msg.activationID, msg.requestSeq) {
			return m, nil
		}
		m.applyInventoryResult(msg.snapshot, msg.err)
		m.syncViewportContent()

	case projectsValidationMsg:
		if !m.acceptsRequest(msg.activationID, msg.requestSeq) {
			return m, nil
		}
		if msg.err == "" {
			m.applyInventoryResult(msg.snapshot, "")
		}
		if msg.valid {
			rec := msg.record
			activationID := m.activationID
			requestSeq := msg.requestSeq
			return m, func() tea.Msg {
				return ProjectsAgentSelectedMsg{ActivationID: activationID, RequestSeq: requestSeq, Record: rec}
			}
		}
		if msg.err != "" {
			m.status = i18n.T("projects.scan_error")
		} else if msg.record.AgentDir != "" && !msg.record.Enterable {
			m.status = i18n.T("projects.target_changed") + ": " + enterabilityText(msg.record)
		} else {
			m.status = i18n.T("projects.target_changed")
		}
		m.syncViewportContent()

	case tea.MouseWheelMsg:
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
		case "up", "k":
			if m.source == projectSourceAgora {
				if m.cursor > 0 {
					m.cursor--
					m.syncViewportContent()
				}
			} else if m.moveCursor(-1) {
				m.syncViewportContent()
			}
			return m, nil
		case "down", "j":
			if m.source == projectSourceAgora {
				if m.cursor < len(m.projects)-1 {
					m.cursor++
					m.syncViewportContent()
				}
			} else if m.moveCursor(1) {
				m.syncViewportContent()
			}
			return m, nil
		case "enter":
			if m.source == projectSourceAgora && m.cursor < len(m.projects) {
				proj := m.projects[m.cursor]
				recipeDir := filepath.Join(proj.Path, ".recipe")
				activationID := m.activationID
				return m, func() tea.Msg {
					return agoraDetailMsg{activationID: activationID, name: proj.Name, dir: recipeDir}
				}
			}
			if m.source == projectSourceRegistry {
				row, ok := m.selectedAgentRow()
				if !ok {
					return m, nil
				}
				if !row.record.Enterable {
					m.status = enterabilityText(row.record)
					m.syncViewportContent()
					return m, nil
				}
				rec := row.record
				seq := m.nextRequestSeq()
				m.status = i18n.T("projects.validating")
				m.syncViewportContent()
				return m, m.validateSelection(rec, seq)
			}
			return m, nil
		case "ctrl+t":
			if m.source == projectSourceAgora {
				activationID := m.activationID
				return m, func() tea.Msg { return agoraTabToggleMsg{activationID: activationID} }
			}
			return m, nil
		case "ctrl+r", "r":
			// ctrl+r is the canonical refresh across views; bare r is kept
			// as a pre-existing alias for this list-only view.
			seq := m.nextRequestSeq()
			return m, m.loadData(seq)
		default:
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *ProjectsModel) syncViewportContent() {
	if !m.ready {
		return
	}
	m.viewport.SetContent(m.renderBody())
	m.ensureCursorVisible()
}

func (m ProjectsModel) acceptsRequest(activationID, requestSeq uint64) bool {
	return activationID == m.activationID && requestSeq == m.requestSeq
}

func (m *ProjectsModel) applyInventoryResult(snapshot inventory.Snapshot, err string) {
	m.snapshot = snapshot
	m.loadErr = err
	m.rows = rowsFromSnapshot(snapshot)
	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}
	m.cursor = m.nearestSelectableCursor(m.cursor)
	if err == "" {
		m.status = ""
	}
}

func (m ProjectsModel) validateSelection(selected inventory.Record, requestSeq uint64) tea.Cmd {
	activationID := m.activationID
	identity := selected.Identity()
	return func() tea.Msg {
		snap, err := projectsScanInventory(inventory.Options{SelfPID: os.Getpid()})
		if err != nil {
			return projectsValidationMsg{
				activationID: activationID,
				requestSeq:   requestSeq,
				identity:     identity,
				err:          err.Error(),
			}
		}
		humanizeSnapshotUptime(&snap)
		rec, ok := recordByIdentity(snap, identity)
		valid := ok && rec.Enterable
		return projectsValidationMsg{
			activationID: activationID,
			requestSeq:   requestSeq,
			identity:     identity,
			snapshot:     snap,
			record:       rec,
			valid:        valid,
		}
	}
}

func recordByIdentity(snap inventory.Snapshot, identity inventory.AgentIdentity) (inventory.Record, bool) {
	for _, r := range snap.Records {
		if r.Identity() == identity {
			return r, true
		}
	}
	return inventory.Record{}, false
}

func humanizeSnapshotUptime(snap *inventory.Snapshot) {
	for i := range snap.Records {
		snap.Records[i].Uptime = inventory.HumanUptimeFromEtime(snap.Records[i].Uptime)
	}
	for gi := range snap.Groups {
		for ri := range snap.Groups[gi].Records {
			snap.Groups[gi].Records[ri].Uptime = inventory.HumanUptimeFromEtime(snap.Groups[gi].Records[ri].Uptime)
		}
	}
}

func (m *ProjectsModel) ensureCursorVisible() {
	if !m.ready {
		return
	}
	m.viewport.SetYOffset(m.viewport.YOffset())
	line, ok := m.selectedRenderedLine()
	if !ok {
		return
	}
	height := m.viewport.Height()
	if height < 1 {
		return
	}
	offset := m.viewport.YOffset()
	switch {
	case line < offset:
		m.viewport.SetYOffset(line)
	case line >= offset+height:
		m.viewport.SetYOffset(line - height + 1)
	}
}

func (m ProjectsModel) selectedRenderedLine() (int, bool) {
	if m.source == projectSourceAgora {
		if len(m.projects) == 0 {
			return 0, false
		}
		cursor := max(0, min(m.cursor, len(m.projects)-1))
		return projectsListTopLines + cursor, true
	}
	if !m.rowSelectable(m.cursor) {
		return 0, false
	}
	return projectsListTopLines + m.cursor, true
}

func rowsFromSnapshot(s inventory.Snapshot) []projectRow {
	var rows []projectRow
	for _, g := range s.Groups {
		rows = append(rows, projectRow{kind: projectRowGroup, project: g.Project, phantom: g.Phantom})
		for _, r := range g.Records {
			rows = append(rows, projectRow{kind: projectRowAgent, project: g.Project, phantom: g.Phantom, record: r})
		}
	}
	return rows
}

func (m ProjectsModel) rowSelectable(idx int) bool {
	return idx >= 0 && idx < len(m.rows) && m.rows[idx].kind == projectRowAgent
}

func (m ProjectsModel) nearestSelectableCursor(start int) int {
	if len(m.rows) == 0 {
		return 0
	}
	if m.rowSelectable(start) {
		return start
	}
	for i := start + 1; i < len(m.rows); i++ {
		if m.rowSelectable(i) {
			return i
		}
	}
	for i := start - 1; i >= 0; i-- {
		if m.rowSelectable(i) {
			return i
		}
	}
	return max(0, min(start, len(m.rows)-1))
}

func (m *ProjectsModel) moveCursor(delta int) bool {
	if len(m.rows) == 0 {
		return false
	}
	next := m.cursor + delta
	for next >= 0 && next < len(m.rows) {
		if m.rowSelectable(next) {
			m.cursor = next
			m.status = ""
			return true
		}
		next += delta
	}
	return false
}

func (m ProjectsModel) selectedAgentRow() (projectRow, bool) {
	if !m.rowSelectable(m.cursor) {
		return projectRow{}, false
	}
	return m.rows[m.cursor], true
}

func (m ProjectsModel) renderBody() string {
	if m.source == projectSourceRegistry {
		return m.renderInventoryBody()
	}
	leftW := m.width / 3
	if leftW < 25 {
		leftW = 25
	}
	if leftW > 40 {
		leftW = 40
	}
	rightW := m.width - leftW - 1
	if rightW < 20 {
		rightW = 20
	}
	if leftW+1+rightW > m.width && m.width > 1 {
		rightW = m.width - leftW - 1
		if rightW < 0 {
			rightW = 0
		}
	}

	leftContent := m.renderLeft(leftW)
	rightContent := m.renderRight(rightW)

	leftLines := strings.Split(leftContent, "\n")
	rightLines := strings.Split(rightContent, "\n")

	vpHeight := m.height - projectsHeaderLines - projectsFooterLines
	if vpHeight < 1 {
		vpHeight = 1
	}
	for len(leftLines) < vpHeight {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < vpHeight {
		rightLines = append(rightLines, "")
	}
	for len(leftLines) < len(rightLines) {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < len(leftLines) {
		rightLines = append(rightLines, "")
	}

	sep := lipgloss.NewStyle().Foreground(ColorTextFaint).Render("│")

	var body strings.Builder
	for i := 0; i < len(leftLines); i++ {
		l := padToWidth(leftLines[i], leftW)
		body.WriteString(l + sep + rightLines[i] + "\n")
	}
	return strings.TrimRight(body.String(), "\n")
}

func (m ProjectsModel) renderInventoryBody() string {
	leftW := m.width / 2
	if leftW < 42 {
		leftW = 42
	}
	if leftW > 72 {
		leftW = 72
	}
	rightW := m.width - leftW - 1
	if rightW < 24 {
		rightW = 24
	}
	if leftW+1+rightW > m.width && m.width > 1 {
		rightW = m.width - leftW - 1
		if rightW < 0 {
			rightW = 0
		}
	}

	leftLines := strings.Split(m.renderInventoryLeft(leftW), "\n")
	rightLines := strings.Split(m.renderInventoryRight(rightW), "\n")

	vpHeight := m.height - projectsHeaderLines - projectsFooterLines
	if vpHeight < 1 {
		vpHeight = 1
	}
	for len(leftLines) < vpHeight {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < vpHeight {
		rightLines = append(rightLines, "")
	}
	for len(leftLines) < len(rightLines) {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < len(leftLines) {
		rightLines = append(rightLines, "")
	}

	sep := lipgloss.NewStyle().Foreground(ColorTextFaint).Render("│")
	var body strings.Builder
	for i := 0; i < len(leftLines); i++ {
		body.WriteString(padToWidth(leftLines[i], leftW) + sep + rightLines[i] + "\n")
	}
	return strings.TrimRight(body.String(), "\n")
}

func (m ProjectsModel) renderInventoryLeft(maxW int) string {
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	selectedStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	disabledStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	markerStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	errorStyle := lipgloss.NewStyle().Foreground(ColorStuck)

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("projects.running_agents")))
	lines = append(lines, "")

	if m.loadErr != "" {
		lines = append(lines, "  "+errorStyle.Render(i18n.T("projects.scan_error")))
		lines = append(lines, "  "+StyleFaint.Render(m.loadErr))
		return strings.Join(lines, "\n")
	}
	if len(m.rows) == 0 {
		lines = append(lines, "  "+StyleFaint.Render(i18n.T("projects.none_running")))
		return strings.Join(lines, "\n")
	}

	for i, row := range m.rows {
		if row.kind == projectRowGroup {
			name := projectLabel(row.project)
			var tags []string
			currentProject := filepath.Dir(m.projectDir)
			if row.project == currentProject {
				tags = append(tags, i18n.T("projects.current_project"))
			}
			if m.ctx.OriginalProjectDir != "" && row.project == filepath.Dir(m.ctx.OriginalProjectDir) {
				tags = append(tags, i18n.T("projects.original_project"))
			}
			if row.phantom {
				tags = append(tags, i18n.T("projects.phantom"))
			}
			if len(tags) > 0 {
				name += " " + markerStyle.Render(strings.Join(tags, " "))
			}
			lines = append(lines, "  "+sectionStyle.Render(name))
			continue
		}

		r := row.record
		prefix := "    "
		style := nameStyle
		if i == m.cursor {
			prefix = "  > "
			style = selectedStyle
		} else if !r.Enterable {
			style = disabledStyle
		}
		display := firstNonEmpty(r.Nickname, r.AgentName, r.Address, r.Agent)
		heartbeat := inventory.HeartbeatLabel(r.Heartbeat)
		summary := fmt.Sprintf("%s  %s  pid %d  %s", r.Role, firstNonEmpty(r.State, "unknown"), r.PID, heartbeat)
		if r.Uptime != "" {
			summary += "  " + r.Uptime
		}
		if !r.Enterable {
			summary += "  !"
		}
		var tags []string
		if r.AgentDir == m.ctx.FocusedAgentDir {
			if m.ctx.Visiting {
				tags = append(tags, i18n.T("projects.visiting"))
			} else {
				tags = append(tags, i18n.T("projects.current"))
			}
		}
		if m.ctx.OriginalAgentDir != "" && r.AgentDir == m.ctx.OriginalAgentDir {
			tags = append(tags, i18n.T("projects.original"))
		}
		if len(tags) > 0 {
			display += " " + markerStyle.Render(strings.Join(tags, " "))
		}
		line := prefix + style.Render(display) + markerStyle.Render("  "+summary)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m ProjectsModel) renderInventoryRight(maxW int) string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	errorStyle := lipgloss.NewStyle().Foreground(ColorStuck)

	if m.loadErr != "" {
		return "\n  " + errorStyle.Render(i18n.T("projects.scan_error")) + "\n\n  " + StyleFaint.Render(m.loadErr)
	}
	if len(m.rows) == 0 {
		return "\n  " + StyleFaint.Render(i18n.T("projects.none_running"))
	}
	row, ok := m.selectedAgentRow()
	if !ok {
		return ""
	}
	r := row.record
	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(firstNonEmpty(r.Nickname, r.AgentName, r.Agent)))
	lines = append(lines, "")
	lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.role")+": ")+valueStyle.Render(string(r.Role)))
	lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.address")+": ")+valueStyle.Render(firstNonEmpty(r.Address, r.Agent)))
	lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.state")+": ")+valueStyle.Render(firstNonEmpty(r.State, "unknown")))
	lines = append(lines, "  "+labelStyle.Render("PID: ")+valueStyle.Render(fmt.Sprint(r.PID)))
	if r.Uptime != "" {
		lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.uptime")+": ")+valueStyle.Render(r.Uptime))
	}
	lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.heartbeat")+": ")+valueStyle.Render(heartbeatDetail(r.Heartbeat)))
	lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.admin")+": ")+valueStyle.Render(firstNonEmpty(r.AdminSummary, "unknown")))
	if r.IMHandles != "" {
		lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.im")+": ")+valueStyle.Render(r.IMHandles))
	}
	lines = append(lines, "")
	lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.path")+": ")+valueStyle.Render(r.Project))
	lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.agent_dir")+": ")+valueStyle.Render(r.AgentDir))
	if r.LockExists {
		lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.lock")+": ")+valueStyle.Render(i18n.T("projects.lock_present")))
	}
	if r.Phantom {
		lines = append(lines, "  "+errorStyle.Render(i18n.T("projects.phantom_detail")))
	}
	if !r.Enterable {
		lines = append(lines, "")
		lines = append(lines, "  "+errorStyle.Render(i18n.T("projects.not_enterable")+": "+enterabilityText(r)))
	} else {
		lines = append(lines, "")
		lines = append(lines, "  "+StyleFaint.Render(i18n.T("projects.enter_hint")))
	}
	if m.status != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+errorStyle.Render(m.status))
	}
	return strings.Join(lines, "\n")
}

func projectLabel(project string) string {
	if project == "" {
		return "(unknown project)"
	}
	return filepath.Base(project)
}

func enterabilityText(r inventory.Record) string {
	text := enterabilityReasonText(r.EnterReason)
	if text == "" {
		text = i18n.T("projects.not_enterable")
	}
	if detail := sanitizedProjectDetail(r.EnterDetail); detail != "" {
		return text + ": " + detail
	}
	return text
}

func enterabilityReasonText(reason inventory.EnterabilityReason) string {
	switch reason {
	case inventory.EnterReasonPathOutsideProject:
		return i18n.T("projects.enter_reason_path")
	case inventory.EnterReasonPhantomProject:
		return i18n.T("projects.enter_reason_phantom")
	case inventory.EnterReasonManifestUnreadable:
		return i18n.T("projects.enter_reason_manifest")
	case inventory.EnterReasonHuman:
		return i18n.T("projects.enter_reason_human")
	case inventory.EnterReasonAgentDirMissing:
		return i18n.T("projects.enter_reason_agent_dir")
	default:
		return ""
	}
}

func sanitizedProjectDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	if i := strings.IndexAny(detail, "\r\n"); i >= 0 {
		detail = detail[:i]
	}
	if len(detail) > 160 {
		detail = detail[:160] + "..."
	}
	return detail
}

func heartbeatDetail(h fs.HeartbeatStatus) string {
	label := inventory.HeartbeatLabel(h)
	if h.AgeSeconds > 0 {
		return fmt.Sprintf("%s (%.0fs)", label, h.AgeSeconds)
	}
	if h.Error != "" && !h.Exists {
		return label + " (" + h.Error + ")"
	}
	return label
}

func (m ProjectsModel) renderLeft(maxW int) string {
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	selectedStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	currentStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string
	lines = append(lines, "")
	sectionKey := "projects.registered"
	emptyKey := "projects.none"
	if m.source == projectSourceAgora {
		sectionKey = "agora.published"
		emptyKey = "agora.none"
	}
	lines = append(lines, "  "+sectionStyle.Render(i18n.T(sectionKey)))
	lines = append(lines, "")

	if len(m.projects) == 0 {
		lines = append(lines, "  "+StyleFaint.Render(i18n.T(emptyKey)))
	}

	for i, proj := range m.projects {
		marker := "  "
		style := nameStyle
		if i == m.cursor {
			marker = "> "
			style = selectedStyle
		}
		name := proj.Name
		suffix := ""
		if proj.Current {
			suffix = " " + currentStyle.Render(i18n.T("projects.current"))
		}
		lines = append(lines, "  "+marker+style.Render(name)+suffix)
	}

	return strings.Join(lines, "\n")
}

func (m ProjectsModel) renderRight(maxW int) string {
	if len(m.projects) == 0 {
		return "\n  " + StyleFaint.Render(i18n.T("projects.select_hint"))
	}
	if m.cursor >= len(m.projects) {
		return ""
	}

	proj := m.projects[m.cursor]

	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string

	// Path
	lines = append(lines, "")
	lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.path")+": ")+valueStyle.Render(proj.Path))
	lines = append(lines, "")

	// Agent list
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("projects.section_agents")))
	lines = append(lines, "")

	net := proj.Network
	if len(net.Nodes) == 0 {
		lines = append(lines, "  "+StyleFaint.Render("  ──"))
	} else {
		for _, n := range net.Nodes {
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
			if n.IsHuman {
				name = "human"
				stateRendered = lipgloss.NewStyle().Foreground(StateColor("ACTIVE")).Render("ACTIVE")
			}
			lines = append(lines, fmt.Sprintf("  %-20s %s", valueStyle.Render(name), stateRendered))
		}
	}

	// Network stats
	stats := net.Stats
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("projects.section_network")))
	lines = append(lines, "")

	var stateParts []string
	if stats.Active > 0 {
		c := lipgloss.NewStyle().Foreground(StateColor("ACTIVE"))
		stateParts = append(stateParts, c.Render(fmt.Sprintf("%s: %d", i18n.T("state.active"), stats.Active)))
	}
	if stats.Idle > 0 {
		c := lipgloss.NewStyle().Foreground(StateColor("IDLE"))
		stateParts = append(stateParts, c.Render(fmt.Sprintf("%s: %d", i18n.T("state.idle"), stats.Idle)))
	}
	if stats.Stuck > 0 {
		c := lipgloss.NewStyle().Foreground(StateColor("STUCK"))
		stateParts = append(stateParts, c.Render(fmt.Sprintf("%s: %d", i18n.T("state.stuck"), stats.Stuck)))
	}
	if stats.Asleep > 0 {
		c := lipgloss.NewStyle().Foreground(StateColor("ASLEEP"))
		stateParts = append(stateParts, c.Render(fmt.Sprintf("%s: %d", i18n.T("state.asleep"), stats.Asleep)))
	}
	if stats.Suspended > 0 {
		c := lipgloss.NewStyle().Foreground(StateColor("SUSPENDED"))
		stateParts = append(stateParts, c.Render(fmt.Sprintf("%s: %d", i18n.T("state.suspended"), stats.Suspended)))
	}
	if len(stateParts) > 0 {
		lines = append(lines, "  "+strings.Join(stateParts, "  "))
	} else {
		lines = append(lines, "  "+StyleFaint.Render("──"))
	}
	if net.Activity.Status != "" {
		c := lipgloss.NewStyle().Foreground(NetworkActivityColor(net.Activity.Status))
		lines = append(lines, "  "+labelStyle.Render(networkActivityLabel()+": ")+c.Render(networkActivityStatusLabel(net.Activity.Status)))
	}

	// Mail count
	if stats.TotalMails > 0 {
		lines = append(lines, "")
		lines = append(lines, "  "+labelStyle.Render(i18n.T("props.total_mails")+": ")+valueStyle.Render(fmt.Sprintf("%d", stats.TotalMails)))
	}

	return strings.Join(lines, "\n")
}

func (m ProjectsModel) View() string {
	titleKey := "projects.title"
	footerHintKey := "hints.projects_nav"
	if m.source == projectSourceAgora {
		titleKey = "agora.title"
		footerHintKey = "hints.agora_networks"
	}
	title := StyleTitle.Render("  "+i18n.T(titleKey)) + "\n" + strings.Repeat("\u2500", m.width)

	scrollHint := ""
	if m.ready && !m.viewport.AtBottom() {
		scrollHint = " " + RuneBullet + " pgup/pgdn scroll"
	}
	status := ""
	if m.source == projectSourceRegistry && m.status != "" {
		status = " " + RuneBullet + " " + m.status
	}
	footer := strings.Repeat("\u2500", m.width) + "\n" +
		StyleFaint.Render("  "+i18n.T(footerHintKey)+scrollHint+status)

	return title + "\n" + PaintViewportBG(m.viewport.View(), m.width) + "\n" + footer
}
