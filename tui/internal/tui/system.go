package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// systemFileOrder is the preferred display order for well-known files in an
// agent's system/ directory. Anything not listed here is appended
// alphabetically.
var systemFileOrder = []string{
	"system.md",
	"covenant.md",
	"principle.md",
	"procedures.md",
	"pad.md",
	"llm.json",
}

// buildAgentSystemEntries lists the files in an agent's system/ directory.
// Well-known markdown files appear first in a canonical order; everything
// else follows alphabetically. JSON files are wrapped in a fenced code block
// for readable rendering; markdown files are shown as-is.
func buildAgentSystemEntries(agentDir string) []MarkdownEntry {
	if agentDir == "" {
		return nil
	}
	sysDir := filepath.Join(agentDir, "system")
	dirents, err := os.ReadDir(sysDir)
	if err != nil {
		return nil
	}

	// Index known filenames for ordering.
	orderIndex := make(map[string]int, len(systemFileOrder))
	for i, n := range systemFileOrder {
		orderIndex[n] = i
	}

	type fileItem struct {
		name string
		path string
	}

	var known []fileItem
	var extras []fileItem
	for _, de := range dirents {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".md" && ext != ".json" {
			continue
		}
		item := fileItem{name: name, path: filepath.Join(sysDir, name)}
		if _, ok := orderIndex[name]; ok {
			known = append(known, item)
		} else {
			extras = append(extras, item)
		}
	}

	sort.Slice(known, func(i, j int) bool {
		return orderIndex[known[i].name] < orderIndex[known[j].name]
	})
	sort.Slice(extras, func(i, j int) bool {
		return extras[i].name < extras[j].name
	})

	files := append(known, extras...)

	result := make([]MarkdownEntry, 0, len(files))
	for _, f := range files {
		label := strings.TrimSuffix(f.name, filepath.Ext(f.name))
		ext := strings.ToLower(filepath.Ext(f.name))

		if ext == ".json" {
			// Fence JSON so glamour renders it as a code block.
			data, err := os.ReadFile(f.path)
			if err != nil {
				continue
			}
			content := "# " + f.name + "\n\n```json\n" + string(data) + "\n```\n"
			result = append(result, MarkdownEntry{
				Label:   label,
				Content: content,
			})
			continue
		}

		// Markdown: pass path so the viewer reads/renders lazily.
		result = append(result, MarkdownEntry{
			Label: label,
			Path:  f.path,
		})
	}

	return result
}

// SystemModel is the top-level /system view. Mirrors LibraryModel/CodexModel:
// shows one agent's system/ directory at a time and swaps agents via Ctrl+T.
type SystemModel struct {
	baseDir     string
	selectedDir string

	inner MarkdownViewerModel

	pickerOpen bool
	pickerIdx  int
	agentNodes []fs.AgentNode

	width  int
	height int
	ready  bool

	pickerVP viewport.Model
}

type systemLoadMsg struct {
	agentNodes []fs.AgentNode
}

func NewSystemModel(baseDir, selectedDir string) SystemModel {
	entries := buildAgentSystemEntries(selectedDir)
	inner := NewMarkdownViewer(entries, systemTitleFor(selectedDir))
	inner.FooterHint = i18n.T("hints.props_select")
	return SystemModel{
		baseDir:     baseDir,
		selectedDir: selectedDir,
		inner:       inner,
	}
}

func systemTitleFor(agentDir string) string {
	base := i18n.T("palette.system")
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

func (m SystemModel) loadAgents() tea.Msg {
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
	return systemLoadMsg{agentNodes: nodes}
}

func (m SystemModel) Init() tea.Cmd {
	return tea.Batch(m.inner.Init(), m.loadAgents)
}

const (
	systemHeaderLines = 2
	systemFooterLines = 2
)

func (m SystemModel) Update(msg tea.Msg) (SystemModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.height - systemHeaderLines - systemFooterLines
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

	case systemLoadMsg:
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

	case tea.KeyPressMsg:
		if m.pickerOpen {
			return m.updatePicker(msg)
		}
		switch msg.String() {
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
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

func (m SystemModel) updatePicker(msg tea.KeyPressMsg) (SystemModel, tea.Cmd) {
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
				entries := buildAgentSystemEntries(m.selectedDir)
				m.inner = NewMarkdownViewer(entries, systemTitleFor(m.selectedDir))
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

func (m *SystemModel) syncPicker() {
	if !m.ready {
		return
	}
	if m.pickerOpen {
		m.pickerVP.SetContent(m.renderPicker())
	}
}

func (m SystemModel) renderPicker() string {
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

func (m SystemModel) View() string {
	if m.pickerOpen {
		header := StyleTitle.Render("  "+systemTitleFor(m.selectedDir)) + "\n" + strings.Repeat("─", m.width)
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
