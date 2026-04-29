package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"

	"github.com/anthropics/lingtai-tui/i18n"
)

// MarkdownEntry is a single item in the markdown viewer's left panel.
type MarkdownEntry struct {
	Label   string // display name shown in list
	Group   string // section header (entries with same group are grouped)
	Path    string // absolute path to file (read on selection)
	Content string // pre-built content (used instead of Path if non-empty)
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
}

const (
	mdvHeaderLines = 2
	mdvFooterLines = 2
	mdvFocusLeft   = 0
	mdvFocusRight  = 1
)

// NewMarkdownViewer creates a viewer with the given entries and title.
func NewMarkdownViewer(entries []MarkdownEntry, title string) MarkdownViewerModel {
	return MarkdownViewerModel{
		entries: entries,
		title:   title,
		focus:   mdvFocusRight, // default focus on content
	}
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
			if m.cursor < len(m.entries) {
				idx := m.cursor
				entry := m.entries[idx]
				return m, func() tea.Msg {
					return MarkdownViewerSelectMsg{Index: idx, Entry: entry}
				}
			}
			return m, nil
		case "tab":
			m.focus = 1 - m.focus // toggle
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.syncLeft()
				m.syncRight()
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.entries)-1 {
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

// cursorLineInLeft returns the line number of the cursor in the rendered left panel.
func (m MarkdownViewerModel) cursorLineInLeft() int {
	line := 0
	lastGroup := ""
	for i, e := range m.entries {
		if e.Group != lastGroup {
			if lastGroup != "" {
				line++ // blank line between groups
			}
			line++ // group header
			line++ // blank after header
			lastGroup = e.Group
		}
		if i == m.cursor {
			return line
		}
		line++
	}
	return line
}

func (m MarkdownViewerModel) renderLeft(maxW int) string {
	selectedStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(ColorText)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e5c07b"))

	problemsGroup := i18n.T("library.problems")

	var lines []string
	lastGroup := ""

	for i, e := range m.entries {
		if e.Group != lastGroup {
			if lastGroup != "" {
				lines = append(lines, "")
			}
			gs := sectionStyle
			if e.Group == problemsGroup || e.Group == "Problems" {
				gs = warnStyle
			}
			lines = append(lines, "  "+gs.Render(e.Group))
			lines = append(lines, "")
			lastGroup = e.Group
		}

		marker := "  "
		style := normalStyle
		if e.Group == problemsGroup || e.Group == "Problems" {
			style = warnStyle
		}
		if i == m.cursor {
			marker = "> "
			style = selectedStyle
		}
		label := e.Label
		// Truncate to fit panel width (accounting for marker + padding)
		maxLabel := maxW - 6
		if maxLabel > 0 && len(label) > maxLabel {
			label = label[:maxLabel-3] + "..."
		}
		lines = append(lines, "  "+marker+style.Render(label))
	}

	if len(m.entries) == 0 {
		lines = append(lines, "  "+StyleFaint.Render("(empty)"))
	}

	return strings.Join(lines, "\n")
}

func (m MarkdownViewerModel) renderRight(maxW int) string {
	if len(m.entries) == 0 || m.cursor >= len(m.entries) {
		return "\n  " + StyleFaint.Render("(no content)")
	}

	e := m.entries[m.cursor]

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

	// Strip YAML frontmatter if present
	if loc := fmRe.FindStringIndex(raw); loc != nil {
		raw = raw[loc[1]:]
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
	if len(m.entries) == 0 || m.cursor >= len(m.entries) {
		m.setStatus(i18n.T("mdviewer.export_empty"), true)
		return
	}
	entry := m.entries[m.cursor]

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
