package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// ProjectsContext carries the App's authoritative current identity into the
// Projects view. FocusedAgentDir and CurrentAgentName are the app's current
// agent (a.orchDir / a.orchName) — never inferred from process state, role,
// PID, row order, or cursor. When Visiting is true they name the visited agent
// (see enterVisitedAgent); OriginalAgentDir/OriginalProjectDir then name the
// context the visit came from. The Projects view resolves live runtime state by
// matching FocusedAgentDir against the process-visible snapshot; when no record
// matches, it shows an honest unavailable status rather than inventing one.
type ProjectsContext struct {
	FocusedAgentDir    string
	CurrentAgentName   string
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
	return m.registryListTopLines() + m.cursor, true
}

// registryListTopLines is the number of rendered lines before agent row 0 in
// the registry overview pane: the (variable-height) current-agent block followed
// by the three-line running-agents header (blank, section title, blank) that
// projectsListTopLines counts. renderInventoryLeft appends exactly those three
// lines after the block, so row 0 begins at len(block) + projectsListTopLines;
// there is no fourth separator line to count. Keeping this in lockstep with
// renderInventoryLeft makes scroll-to-cursor land on the right row.
func (m ProjectsModel) registryListTopLines() int {
	block := m.renderCurrentAgentBlock()
	if len(block) == 0 {
		return projectsListTopLines
	}
	return len(block) + projectsListTopLines
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
	if m.width < 90 {
		return m.renderInventoryLeft(max(0, m.width))
	}
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
	currentStyle := lipgloss.NewStyle().Foreground(ColorAccent)
	errorStyle := lipgloss.NewStyle().Foreground(ColorStuck)

	var lines []string
	// The dedicated current-agent block anchors the authoritative current
	// identity and its live state/heartbeat at the top of the pane, so it stays
	// visible and unambiguous even when the cursor roams onto other agents. It is
	// keyed on the App context (FocusedAgentDir/CurrentAgentName), never on
	// cursor, role, PID, or row order.
	lines = append(lines, m.renderCurrentAgentBlock()...)
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
		// The row prefix carries two independent signals in fixed columns so
		// cursor selection and current-agent identity never collapse into one:
		// column 0 is the current-agent marker, column 2 is the cursor marker.
		// A row can be current, selected, both, or neither — each reads distinctly.
		isCurrent := m.isCurrentAgentRow(r)
		currentCol := " "
		if isCurrent {
			currentCol = projectsCurrentMarker
		}
		cursorCol := " "
		style := nameStyle
		if i == m.cursor {
			cursorCol = ">"
			style = selectedStyle
		} else if isCurrent {
			style = currentStyle
		} else if !r.Enterable {
			style = disabledStyle
		}
		prefix := currentCol + " " + cursorCol + " "
		display := firstNonEmpty(r.Nickname, r.AgentName, r.Address, r.Agent)
		heartbeat := localizedHeartbeatLabel(r.Heartbeat)
		// The overview row answers who/role/live-state/heartbeat and, when
		// authoritative context exists and there is room, compact context
		// pressure. Operational details (PID, process uptime) live in Details.
		summary := fmt.Sprintf("%s  %s  %s", r.Role, valueOrDash(r.State), heartbeat)
		if r.ContextAvailable && maxW >= projectsOverviewContextMinWidth {
			summary += fmt.Sprintf("  %.0f%%", r.ContextUsagePct)
		}
		if !r.Enterable {
			summary += "  !"
		}
		// The current/visiting identity is carried by the column-0 marker and the
		// dedicated block above, not an inline tag. Only the [visiting] refinement
		// and the [original] origin marker remain as inline tags.
		var tags []string
		if isCurrent && m.ctx.Visiting {
			tags = append(tags, i18n.T("projects.visiting"))
		}
		if m.ctx.OriginalAgentDir != "" && m.agentDirMatches(r.AgentDir, m.ctx.OriginalAgentDir) {
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

// projectsCurrentMarker flags the authoritative current agent in the overview
// list and block. It is deliberately distinct from the ">" cursor marker so the
// current-agent identity and the keyboard selection never read as the same
// concept (product contract: identity ≠ cursor).
const projectsCurrentMarker = "◆"

// agentDirMatches compares two agent-directory strings by normalized absolute
// path (inventory.NormalizePath: filepath.Clean then filepath.Abs), so an
// un-cleaned or relative context dir still matches the snapshot's cleaned
// AgentDir. It does not expand "~/" — callers pass already-resolved paths
// (a.orchDir and inventory AgentDir come from the same conventions). Empty
// inputs never match.
func (m ProjectsModel) agentDirMatches(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return inventory.NormalizePath(a) == inventory.NormalizePath(b)
}

// isCurrentAgentRow reports whether a record is the App's authoritative current
// agent, matched by directory identity from context — never by role, PID, state,
// or row order.
func (m ProjectsModel) isCurrentAgentRow(r inventory.Record) bool {
	return m.agentDirMatches(r.AgentDir, m.ctx.FocusedAgentDir)
}

// currentAgentRecord returns the process-visible record for the App's current
// agent, if any. The second result is false when the current agent has no record
// in the snapshot (stopped, never-booted, or otherwise not process-visible) — in
// which case the caller degrades to an honest unavailable status rather than
// inventing a lifecycle state.
func (m ProjectsModel) currentAgentRecord() (inventory.Record, bool) {
	if m.ctx.FocusedAgentDir == "" {
		return inventory.Record{}, false
	}
	for _, row := range m.rows {
		if row.kind != projectRowAgent {
			continue
		}
		if m.isCurrentAgentRow(row.record) {
			return row.record, true
		}
	}
	return inventory.Record{}, false
}

// renderCurrentAgentBlock renders the dedicated current-agent summary at the top
// of the overview pane. It names the App's authoritative current agent and, when
// that agent is process-visible, surfaces its live runtime state (colored by
// StateColor) and heartbeat as prominent, separately-labeled fields. When the
// current agent is absent from the snapshot it shows a localized unavailable
// status instead — honest degradation, never a fabricated lifecycle state. When
// there is no current agent at all (no App context), the block renders nothing.
func (m ProjectsModel) renderCurrentAgentBlock() []string {
	if m.ctx.FocusedAgentDir == "" && strings.TrimSpace(m.ctx.CurrentAgentName) == "" {
		return nil
	}
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	markerStyle := lipgloss.NewStyle().Foreground(ColorAccent)

	header := i18n.T("projects.current_agent")
	if m.ctx.Visiting {
		header += " " + markerStyle.Render(i18n.T("projects.visiting"))
	}

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(header))

	rec, visible := m.currentAgentRecord()
	// Prefer the process-visible display name; fall back to the App-context name
	// so a not-visible current agent is still identified rather than blank.
	name := ""
	if visible {
		name = firstNonEmpty(rec.Nickname, rec.AgentName, rec.Address, rec.Agent)
	}
	name = firstNonEmpty(name, m.ctx.CurrentAgentName)
	if name == "" {
		name = projectsUnavailable
	}
	lines = append(lines, "  "+projectsCurrentMarker+" "+nameStyle.Render(name))

	if !visible {
		// Honest degradation: no process record → no lifecycle state. Say so.
		lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.current_agent_unavailable")))
		return lines
	}

	// Live runtime state and heartbeat are the prominent, separately-labeled core
	// of the block — colored by StateColor so ACTIVE/IDLE/ASLEEP read at a glance.
	state := valueOrDash(rec.State)
	stateStyled := lipgloss.NewStyle().Foreground(StateColor(strings.ToUpper(rec.State))).Bold(true).Render(state)
	heartbeat := heartbeatStyled(rec.Heartbeat)
	lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.state")+": ")+stateStyled+
		labelStyle.Render("   "+i18n.T("projects.heartbeat")+": ")+heartbeat)
	return lines
}

// heartbeatStyled renders the localized heartbeat label colored by freshness:
// active green when fresh, suspended amber when stale, dim when missing — so the
// current-agent block's health reads without cross-referencing the label text.
func heartbeatStyled(h fs.HeartbeatStatus) string {
	label := localizedHeartbeatLabel(h)
	switch {
	case h.Fresh:
		return lipgloss.NewStyle().Foreground(ColorActive).Render(label)
	case h.Exists:
		return lipgloss.NewStyle().Foreground(ColorSuspended).Render(label)
	default:
		return lipgloss.NewStyle().Foreground(ColorTextDim).Render(label)
	}
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
	topology := topologyForProject(m.snapshot, r.Project)
	appendField := func(lines []string, label, value string) []string {
		return append(lines, "  "+labelStyle.Render(label+": ")+valueStyle.Render(value))
	}
	appendSection := func(lines []string, key string) []string {
		return append(lines, "", "  "+sectionStyle.Render(i18n.T(key)))
	}

	var lines []string

	// Identity header: display name, then the address/agent identity as a
	// dim line — never a labeled Address: row, and dropped when it would only
	// echo the header. The header fallback order matches the left overview row
	// (Nickname, AgentName, Address, Agent) so address-only records never fall
	// through to a blank or internal-agent name.
	header := firstNonEmpty(r.Nickname, r.AgentName, r.Address, r.Agent)
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(header))
	identity := firstNonEmpty(r.Address, r.Agent)
	if identity != "" && identity != header {
		lines = append(lines, "  "+labelStyle.Render(identity))
	}

	// Validation state sits directly under the header so warnings/disabled/
	// phantom reasons are the first thing read, not buried below sections.
	if r.Phantom {
		lines = append(lines, "", "  "+errorStyle.Render(i18n.T("projects.phantom_detail")))
	}
	if !r.Enterable {
		lines = append(lines, "", "  "+errorStyle.Render(i18n.T("projects.not_enterable")+": "+enterabilityText(r)))
	}
	if m.status != "" {
		lines = append(lines, "", "  "+errorStyle.Render(m.status))
	}

	// Lifecycle: created + lifetime fold onto one line; uptime and molt count
	// stay distinct.
	moltCount := projectsUnavailable
	if r.MoltCountAvailable {
		moltCount = fmt.Sprint(r.MoltCount)
	}
	lines = appendSection(lines, "projects.section_lifecycle")
	lines = appendField(lines, i18n.T("projects.created_at"), lifecycleCreatedLine(r))
	lines = appendField(lines, i18n.T("projects.process_uptime"), valueOrDash(r.Uptime))
	lines = appendField(lines, i18n.T("projects.molt_count"), moltCount)

	// Network: project and orchestrator on their own lines; the two live
	// counts fold into one readable line.
	lines = appendSection(lines, "projects.section_network")
	lines = appendField(lines, i18n.T("projects.project"), valueOrDash(projectLabel(r.Project)))
	lines = appendField(lines, i18n.T("projects.orchestrator"), valueOrDash(topology.orchestrator))
	lines = append(lines, "  "+labelStyle.Render(i18n.T("projects.live_members")+": ")+valueStyle.Render(fmt.Sprint(topology.members))+
		labelStyle.Render(" · "+i18n.T("projects.live_admins")+": ")+valueStyle.Render(fmt.Sprint(topology.admins)))

	// Runtime: PID plus exact authoritative context (with a small meter when
	// legible), and optional IM/lock connections.
	lines = appendSection(lines, "projects.section_runtime")
	lines = appendField(lines, "PID", fmt.Sprint(r.PID))
	if r.ContextAvailable {
		usage := fmt.Sprintf("%s / %s (%.1f%%)", formatComma(int64(r.ContextTotalTokens)), formatComma(int64(r.ContextWindowSize)), r.ContextUsagePct)
		lines = appendField(lines, i18n.T("projects.context_usage"), usage)
		if meter := contextMeter(r.ContextUsagePct, maxW); meter != "" {
			lines = append(lines, "  "+meter)
		}
	}
	if r.IMHandles != "" {
		lines = appendField(lines, i18n.T("projects.im"), r.IMHandles)
	}
	if r.LockExists {
		lines = appendField(lines, i18n.T("projects.lock"), i18n.T("projects.lock_present"))
	}

	if r.Enterable {
		lines = append(lines, "")
		lines = append(lines, "  "+StyleFaint.Render(i18n.T("projects.enter_hint")))
	}
	return strings.Join(lines, "\n")
}

// lifecycleCreatedLine folds the creation timestamp and derived lifetime into a
// single readable value ("<ts> · <lifetime>"), degrading honestly to a dash
// when the creation time is unknown.
func lifecycleCreatedLine(r inventory.Record) string {
	if r.CreatedAt == "" {
		return projectsUnavailable
	}
	created := formatKanbanTimestamp(r.CreatedAt)
	lifetime := formatAgentLifetime(r.CreatedAt, time.Now())
	if lifetime == projectsUnavailable {
		return created
	}
	return created + " · " + lifetime
}

// contextMeter renders a small text progress meter for authoritative context
// pressure. It is dropped when the Details pane is too narrow to keep the exact
// usage line legible alongside it, and — to avoid contradicting the exact
// numeric text — also when the quantized bar would show zero filled cells (a
// low nonzero percentage that a 12-cell bar cannot represent). The exact
// "total / window (pct)" line always renders; only the meter is suppressed.
func contextMeter(pct float64, maxW int) string {
	if maxW < projectsOverviewContextMinWidth {
		return ""
	}
	if shareBarFilledCells(pct, projectsContextMeterCells) == 0 {
		return ""
	}
	return renderShareBar(pct, projectsContextMeterCells)
}

// shareBarFilledCells reports how many cells renderShareBar would fill for pct
// over width cells, using the same truncating quantization (never rounding up),
// so the Projects meter and its suppression decision stay in lockstep with the
// shared bar renderer without duplicating its styling.
func shareBarFilledCells(pct float64, width int) int {
	if width < 1 {
		width = 1
	}
	filled := int((pct / 100.0) * float64(width))
	if filled < 0 {
		return 0
	}
	if filled > width {
		return width
	}
	return filled
}

const projectsUnavailable = "—"

// projectsOverviewContextMinWidth is the left-pane width below which the compact
// context percentage is dropped from an overview row so the who/role/state/
// heartbeat core never clips.
const projectsOverviewContextMinWidth = 40

// projectsContextMeterCells is the width of the Runtime context meter. Kept
// small so the meter plus the exact "total / window (pct)" line stays legible
// at the 100-column supported width.
const projectsContextMeterCells = 12

type projectTopology struct {
	orchestrator string
	members      int
	admins       int
}

func topologyForProject(snapshot inventory.Snapshot, project string) projectTopology {
	var topology projectTopology
	for _, record := range snapshot.Records {
		if record.Project != project {
			continue
		}
		topology.members++
		if record.IsOrchestrator {
			topology.admins++
			if topology.orchestrator == "" {
				topology.orchestrator = firstNonEmpty(record.Nickname, record.AgentName, record.Address, record.Agent)
			}
		}
	}
	return topology
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return projectsUnavailable
	}
	return value
}

func formatAgentLifetime(createdAt string, now time.Time) string {
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil || now.Before(created) {
		return projectsUnavailable
	}
	return formatDuration(now.Sub(created))
}

func projectLabel(project string) string {
	if project == "" {
		return projectsUnavailable
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
	case inventory.EnterReasonNonAdmin:
		return i18n.T("projects.enter_reason_non_admin")
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

func localizedHeartbeatLabel(h fs.HeartbeatStatus) string {
	switch {
	case h.Fresh:
		return i18n.T("projects.heartbeat_fresh")
	case h.Exists:
		return i18n.T("projects.heartbeat_stale")
	default:
		return i18n.T("projects.heartbeat_missing")
	}
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
