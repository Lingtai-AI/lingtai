package tui

import (
	"os"
	"strings"

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
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return MarkdownViewerCloseMsg{} }
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

func (m MarkdownViewerModel) View() string {
	title := StyleTitle.Render("  "+m.title) + "\n" + strings.Repeat("\u2500", m.width)

	scrollHint := ""
	if m.ready && !m.rightVP.AtBottom() {
		scrollHint = " " + RuneBullet + " pgup/pgdn scroll"
	}
	focusHint := "tab switch"
	footer := strings.Repeat("\u2500", m.width) + "\n" +
		StyleFaint.Render("  ↑↓ "+i18n.T("welcome.select_lang")+"  [Esc] "+i18n.T("firstrun.back")+" "+RuneBullet+" "+focusHint+scrollHint)

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
