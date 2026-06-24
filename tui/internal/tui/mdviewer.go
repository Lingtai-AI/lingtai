package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"

	"github.com/anthropics/lingtai-tui/i18n"
)

// MarkdownEntry is a single item in the markdown viewer's left panel.
type MarkdownEntry struct {
	Label       string // display name shown in list
	Description string // optional subtitle (rendered faint under Label)
	Group       string // section header (entries with same group are grouped)
	Path        string // absolute path to file (read on selection)
	Content     string // pre-built content (used instead of Path if non-empty)
	Remote      string // configured git remote URL for repo-backed skills; "" otherwise (issue #172)
}

// MarkdownViewerCloseMsg is sent when the user exits the viewer.
type MarkdownViewerCloseMsg struct{}

// MarkdownViewerSelectMsg is sent when the user presses Enter on an entry.
// Wrappers that want drill-in behavior handle this message; wrappers that
// don't care can ignore it.
type MarkdownViewerSelectMsg struct {
	Index int
	Entry MarkdownEntry
}

// MarkdownViewerModel is a two-panel view with independent scrolling:
// left panel (entry list) and right panel (rendered markdown content).
type MarkdownViewerModel struct {
	entries []MarkdownEntry
	title   string
	width   int
	height  int
	cursor  int

	leftVP  viewport.Model
	rightVP viewport.Model
	ready   bool

	// focus tracks which panel receives scroll input
	focus int // 0 = left, 1 = right

	// FooterHint, if non-empty, is appended to the footer as an extra shortcut
	// hint (e.g., "ctrl+t select agent"). Wrappers set this to advertise keys
	// they handle at a higher level.
	FooterHint string

	// status is a transient message (e.g. last export path) shown in the footer
	// in place of the standard hint line. Cleared on the next keypress.
	status    string
	statusErr bool

	// expanded tracks which group folders are open. Groups not present in the
	// map are treated as collapsed. Groups are keyed by their Group string.
	expanded map[string]bool
	// groupOrder is the de-duplicated list of groups in entry-order; used to
	// render group nodes even when none of their entries are visible.
	groupOrder []string
}

const (
	mdvHeaderLines = 2
	mdvFooterLines = 2
	mdvFocusLeft   = 0
	mdvFocusRight  = 1
)

// NewMarkdownViewer creates a viewer with the given entries and title.
// The first group is expanded by default; all others start collapsed so the
// sidebar fits in one screen for agents with many groups.
func NewMarkdownViewer(entries []MarkdownEntry, title string) MarkdownViewerModel {
	expanded := make(map[string]bool)
	var order []string
	seen := make(map[string]bool)
	for _, e := range entries {
		if !seen[e.Group] {
			seen[e.Group] = true
			order = append(order, e.Group)
		}
	}
	if len(order) > 0 {
		expanded[order[0]] = true
	}
	m := MarkdownViewerModel{
		entries:    entries,
		title:      title,
		focus:      mdvFocusRight, // default focus on content
		expanded:   expanded,
		groupOrder: order,
	}
	// Land the cursor on the first entry (skip past the group header) so the
	// right panel renders immediately and matches the pre-tree behavior.
	nodes := m.visibleNodes()
	for i, n := range nodes {
		if !n.isGroup {
			m.cursor = i
			break
		}
	}
	return m
}

func (m MarkdownViewerModel) Init() tea.Cmd { return nil }

func (m MarkdownViewerModel) Update(msg tea.Msg) (MarkdownViewerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.height - mdvHeaderLines - mdvFooterLines
		if vpHeight < 1 {
			vpHeight = 1
		}
		leftW, _ := m.panelWidths()
		if !m.ready {
			m.leftVP = viewport.New()
			m.rightVP = viewport.New()
			m.ready = true
		}
		m.leftVP.SetWidth(leftW)
		m.leftVP.SetHeight(vpHeight)
		m.rightVP.SetWidth(m.width - leftW - 1) // -1 for separator
		m.rightVP.SetHeight(vpHeight)
		m.syncLeft()
		m.syncRight()

	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		if m.focus == mdvFocusRight {
			m.rightVP, cmd = m.rightVP.Update(msg)
		} else {
			m.leftVP, cmd = m.leftVP.Update(msg)
		}
		return m, cmd

	case tea.KeyPressMsg:
		key := msg.String()
		// ctrl+e exports the current entry to ~/Downloads. Handle it before
		// clearing the status so the export result is the new status.
		if key == "ctrl+e" {
			m.exportCurrent()
			return m, nil
		}
		// Any other keypress dismisses a stale status banner.
		if m.status != "" {
			m.status = ""
			m.statusErr = false
		}
		switch key {
		case "esc", "q":
			return m, func() tea.Msg { return MarkdownViewerCloseMsg{} }
		case "enter":
			nodes := m.visibleNodes()
			if m.cursor < len(nodes) {
				n := nodes[m.cursor]
				if n.isGroup {
					m.toggleGroup(n.group)
					m.syncLeft()
					return m, nil
				}
				idx := n.entryIdx
				entry := m.entries[idx]
				return m, func() tea.Msg {
					return MarkdownViewerSelectMsg{Index: idx, Entry: entry}
				}
			}
			return m, nil
		case "tab":
			m.focus = 1 - m.focus // toggle
			return m, nil
		case "left", "h":
			// Collapse the group containing the cursor, or, if already on a
			// collapsed group header, jump to the previous group header.
			nodes := m.visibleNodes()
			if m.cursor < len(nodes) {
				n := nodes[m.cursor]
				if n.isGroup && m.expanded[n.group] {
					m.expanded[n.group] = false
					m.syncLeft()
					return m, nil
				}
				if !n.isGroup {
					// Move cursor to the group header and collapse it.
					for i := m.cursor - 1; i >= 0; i-- {
						if nodes[i].isGroup && nodes[i].group == n.group {
							m.cursor = i
							m.expanded[n.group] = false
							m.syncLeft()
							m.syncRight()
							return m, nil
						}
					}
				}
			}
			return m, nil
		case "right", "l":
			// Expand the group under the cursor.
			nodes := m.visibleNodes()
			if m.cursor < len(nodes) {
				n := nodes[m.cursor]
				if n.isGroup && !m.expanded[n.group] {
					m.expanded[n.group] = true
					m.syncLeft()
					return m, nil
				}
			}
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.syncLeft()
				m.syncRight()
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.visibleNodes())-1 {
				m.cursor++
				m.syncLeft()
				m.syncRight()
			}
			return m, nil
		default:
			// pgup/pgdn/home/end go to right panel
			var cmd tea.Cmd
			if m.focus == mdvFocusRight {
				m.rightVP, cmd = m.rightVP.Update(msg)
			} else {
				m.leftVP, cmd = m.leftVP.Update(msg)
			}
			return m, cmd
		}
	}
	return m, nil
}

func (m MarkdownViewerModel) panelWidths() (int, int) {
	leftW := m.width / 3
	if leftW < 25 {
		leftW = 25
	}
	if leftW > 40 {
		leftW = 40
	}
	rightW := m.width - leftW - 1 // -1 for separator column
	if rightW < 20 {
		rightW = 20
	}
	return leftW, rightW
}

func (m *MarkdownViewerModel) syncLeft() {
	if !m.ready {
		return
	}
	leftW, _ := m.panelWidths()
	m.leftVP.SetContent(m.renderLeft(leftW))

	// Scroll to keep cursor visible
	vpH := m.leftVP.Height()
	if vpH <= 0 {
		return
	}
	cursorLine := m.cursorLineInLeft()
	top := m.leftVP.YOffset()
	if cursorLine < top {
		m.leftVP.SetYOffset(cursorLine)
	} else if cursorLine >= top+vpH {
		m.leftVP.SetYOffset(cursorLine - vpH + 1)
	}
}

func (m *MarkdownViewerModel) syncRight() {
	if !m.ready {
		return
	}
	_, rightW := m.panelWidths()
	m.rightVP.SetContent(m.renderRight(rightW))
	m.rightVP.SetYOffset(0) // reset scroll on selection change
}

// visibleNode is one row in the tree sidebar — either a group header or an
// expanded entry. The cursor indexes into the slice returned by
// visibleNodes(), so collapsed groups are automatically skipped.
type visibleNode struct {
	isGroup  bool
	group    string
	entryIdx int // -1 when isGroup
}

// visibleNodes returns the ordered list of group headers and (when their
// group is expanded) entry rows that make up the current sidebar tree.
func (m MarkdownViewerModel) visibleNodes() []visibleNode {
	var nodes []visibleNode
	if len(m.entries) == 0 {
		return nodes
	}
	// Group entries while preserving entry order.
	byGroup := make(map[string][]int)
	for i, e := range m.entries {
		byGroup[e.Group] = append(byGroup[e.Group], i)
	}
	for _, g := range m.groupOrder {
		nodes = append(nodes, visibleNode{isGroup: true, group: g, entryIdx: -1})
		if m.expanded[g] {
			for _, idx := range byGroup[g] {
				nodes = append(nodes, visibleNode{isGroup: false, group: g, entryIdx: idx})
			}
		}
	}
	return nodes
}

// currentEntryIndex returns the entry index under the cursor, or -1 when the
// cursor is on a group header.
func (m MarkdownViewerModel) currentEntryIndex() int {
	nodes := m.visibleNodes()
	if m.cursor >= len(nodes) {
		return -1
	}
	n := nodes[m.cursor]
	if n.isGroup {
		return -1
	}
	return n.entryIdx
}

func (m *MarkdownViewerModel) toggleGroup(group string) {
	if m.expanded == nil {
		m.expanded = make(map[string]bool)
	}
	m.expanded[group] = !m.expanded[group]
}

// cursorLineInLeft returns the line number of the cursor row in the rendered
// left panel. Each visible node renders as exactly one line plus an optional
// description line for non-group entries that carry a Description.
func (m MarkdownViewerModel) cursorLineInLeft() int {
	line := 0
	nodes := m.visibleNodes()
	for i, n := range nodes {
		if i == m.cursor {
			return line
		}
		line++ // the node's own row
		if !n.isGroup {
			if d := strings.TrimSpace(m.entries[n.entryIdx].Description); d != "" {
				line++ // subtitle line
			}
		}
	}
	return line
}

func (m MarkdownViewerModel) renderLeft(maxW int) string {
	selectedStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(ColorText)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e5c07b"))

	problemsGroup := i18n.T("skills.problems")

	nodes := m.visibleNodes()
	if len(nodes) == 0 {
		return "  " + StyleFaint.Render("(empty)")
	}

	var lines []string
	for i, n := range nodes {
		isCursor := i == m.cursor
		if n.isGroup {
			arrow := "▶"
			if m.expanded[n.group] {
				arrow = "▼"
			}
			marker := "  "
			gs := sectionStyle
			if n.group == problemsGroup || n.group == "Problems" {
				gs = warnStyle
			}
			if isCursor {
				marker = "> "
				gs = selectedStyle
			}
			label := truncateForPanel(n.group, maxW-6)
			lines = append(lines, "  "+marker+gs.Render(arrow+" "+label))
			continue
		}

		e := m.entries[n.entryIdx]
		marker := "    " // indented under the group
		style := normalStyle
		if e.Group == problemsGroup || e.Group == "Problems" {
			style = warnStyle
		}
		if isCursor {
			marker = "  > "
			style = selectedStyle
		}
		label := truncateForPanel(e.Label, maxW-8)
		lines = append(lines, "  "+marker+style.Render(label))
		if d := strings.TrimSpace(e.Description); d != "" {
			desc := truncateForPanel(d, maxW-8)
			lines = append(lines, "      "+StyleFaint.Render(desc))
		}
	}

	return strings.Join(lines, "\n")
}

// truncateForPanel shortens s to fit within max display columns, appending
// "..." when truncation occurs. Uses lipgloss.Width so multi-byte glyphs (CJK)
// are accounted for correctly.
func truncateForPanel(s string, max int) string {
	if max <= 0 {
		return s
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	// Walk runes, accumulating width, stop when we'd exceed max-3.
	target := max - 3
	if target < 1 {
		target = 1
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > target {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "..."
}

func (m MarkdownViewerModel) renderRight(maxW int) string {
	idx := m.currentEntryIndex()
	if idx < 0 {
		return "\n  " + StyleFaint.Render("(no content)")
	}

	e := m.entries[idx]

	var raw string
	if e.Content != "" {
		raw = e.Content
	} else if e.Path != "" {
		data, err := os.ReadFile(e.Path)
		if err != nil {
			return "\n  " + StyleFaint.Render("(file not found)")
		}
		raw = string(data)
	} else {
		return "\n  " + StyleFaint.Render("(no content)")
	}

	// Surface YAML frontmatter (fields like `description`) instead of
	// discarding it: render the block as a fenced code section above the
	// body so glamour displays it verbatim and readably, while the body
	// below renders as normal markdown.
	if sub := fmRe.FindStringSubmatchIndex(raw); sub != nil {
		fmBody := strings.TrimRight(raw[sub[2]:sub[3]], "\n")
		body := raw[sub[1]:]
		raw = "```yaml\n" + fmBody + "\n```\n\n" + body
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "\n  " + StyleFaint.Render("(empty)")
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(ActiveTheme().GlamourStyle),
		glamour.WithWordWrap(maxW-2),
	)
	if err == nil {
		if rendered, rerr := r.Render(raw); rerr == nil {
			return "\n" + rendered
		}
	}

	wrapped := lipgloss.NewStyle().Width(maxW - 2).Render(raw)
	var lines []string
	lines = append(lines, "")
	for _, line := range strings.Split(wrapped, "\n") {
		lines = append(lines, " "+line)
	}
	return strings.Join(lines, "\n")
}

var exportSanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9._\-\p{Han}]+`)

// exportCurrent writes the current entry to ~/Downloads. For Path-backed
// entries the original file is copied verbatim; for Content-backed entries
// the rendered markdown is written with a synthesized filename. The result
// (or an error) is stored in m.status for the footer to display.
func (m *MarkdownViewerModel) exportCurrent() {
	idx := m.currentEntryIndex()
	if idx < 0 {
		m.setStatus(i18n.T("mdviewer.export_empty"), true)
		return
	}
	entry := m.entries[idx]

	dir, err := exportTargetDir()
	if err != nil {
		m.setStatus(fmt.Sprintf("%s: %v", i18n.T("mdviewer.export_failed"), err), true)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.setStatus(fmt.Sprintf("%s: %v", i18n.T("mdviewer.export_failed"), err), true)
		return
	}

	var data []byte
	var baseName string
	switch {
	case entry.Content != "":
		data = []byte(entry.Content)
		baseName = synthExportName(entry, m.title)
	case entry.Path != "":
		raw, err := os.ReadFile(entry.Path)
		if err != nil {
			m.setStatus(fmt.Sprintf("%s: %v", i18n.T("mdviewer.export_failed"), err), true)
			return
		}
		data = raw
		baseName = filepath.Base(entry.Path)
	default:
		m.setStatus(i18n.T("mdviewer.export_empty"), true)
		return
	}

	dest := uniquePath(filepath.Join(dir, baseName))
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		m.setStatus(fmt.Sprintf("%s: %v", i18n.T("mdviewer.export_failed"), err), true)
		return
	}
	m.setStatus(fmt.Sprintf("%s %s", i18n.T("mdviewer.export_saved"), prettyPath(dest)), false)
}

func (m *MarkdownViewerModel) setStatus(msg string, isErr bool) {
	m.status = msg
	m.statusErr = isErr
}

// exportTargetDir returns the user's Downloads directory, falling back to the
// home directory if Downloads is not writable.
func exportTargetDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

// synthExportName builds a safe filename from the entry label/group plus a
// timestamp, defaulting to a generic name when the label is empty.
func synthExportName(entry MarkdownEntry, viewTitle string) string {
	parts := []string{}
	if t := strings.TrimSpace(viewTitle); t != "" {
		parts = append(parts, t)
	}
	if g := strings.TrimSpace(entry.Group); g != "" {
		parts = append(parts, g)
	}
	if l := strings.TrimSpace(entry.Label); l != "" {
		parts = append(parts, l)
	}
	stem := "lingtai-export"
	if len(parts) > 0 {
		stem = strings.Join(parts, "-")
	}
	stem = exportSanitizeRe.ReplaceAllString(stem, "-")
	stem = strings.Trim(stem, "-_.")
	if stem == "" {
		stem = "lingtai-export"
	}
	if len(stem) > 80 {
		stem = stem[:80]
	}
	stamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s-%s.md", stem, stamp)
}

// uniquePath appends a numeric suffix if path already exists.
func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return path // give up; let WriteFile overwrite as a last resort
}

// prettyPath replaces $HOME with ~ for compact display.
func prettyPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func (m MarkdownViewerModel) View() string {
	title := StyleTitle.Render("  "+m.title) + "\n" + strings.Repeat("\u2500", m.width)

	scrollHint := ""
	if m.ready && !m.rightVP.AtBottom() {
		scrollHint = " " + RuneBullet + " pgup/pgdn scroll"
	}
	focusHint := "tab switch"
	exportHint := " " + RuneBullet + " " + i18n.T("mdviewer.export_hint")
	extraHint := ""
	if m.FooterHint != "" {
		extraHint = " " + RuneBullet + " " + m.FooterHint
	}
	hintLine := StyleFaint.Render("  ↑↓ " + i18n.T("welcome.select_lang") + "  [Esc] " + i18n.T("firstrun.back") + " " + RuneBullet + " " + focusHint + scrollHint + exportHint + extraHint)
	if m.status != "" {
		statusStyle := lipgloss.NewStyle().Foreground(ColorAccent)
		if m.statusErr {
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75"))
		}
		hintLine = statusStyle.Render("  " + m.status)
	}
	footer := strings.Repeat("\u2500", m.width) + "\n" + hintLine

	if !m.ready {
		return title + "\n\n  " + i18n.T("app.loading") + "\n\n" + footer
	}

	// Render both viewports and merge side by side
	leftW, _ := m.panelWidths()
	leftContent := m.leftVP.View()
	rightContent := m.rightVP.View()

	leftLines := strings.Split(leftContent, "\n")
	rightLines := strings.Split(rightContent, "\n")

	vpHeight := m.height - mdvHeaderLines - mdvFooterLines
	if vpHeight < 1 {
		vpHeight = 1
	}

	// Pad to equal length
	for len(leftLines) < vpHeight {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < vpHeight {
		rightLines = append(rightLines, "")
	}

	sep := lipgloss.NewStyle().Foreground(ColorTextFaint).Render("│")
	var body strings.Builder
	for i := 0; i < vpHeight; i++ {
		l := ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		}
		l = padToWidth(l, leftW)
		body.WriteString(l + sep + r + "\n")
	}
	merged := strings.TrimRight(body.String(), "\n")

	return title + "\n" + PaintViewportBG(merged, m.width) + "\n" + footer
}
