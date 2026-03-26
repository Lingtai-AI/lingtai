package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FirstRunDoneMsg is emitted when first-run flow completes.
type FirstRunDoneMsg struct {
	OrchDir  string // full path to orchestrator directory
	OrchName string // agent name
}

type firstRunStep int

const (
	stepCheckPresets firstRunStep = iota
	stepAPIKey
	stepPickPreset
	stepNameAgent
	stepLaunching
)

// FirstRunModel orchestrates the first-run experience.
type FirstRunModel struct {
	step      firstRunStep
	setup     SetupModel
	presets   []preset.Preset
	cursor    int
	nameInput textinput.Model
	message   string
	baseDir   string // .lingtai/ directory
	globalDir string
	width     int
	height    int
}

func NewFirstRunModel(baseDir, globalDir string, hasPresets bool) FirstRunModel {
	ti := textinput.New()
	ti.CharLimit = 64
	ti.Width = 40

	m := FirstRunModel{
		baseDir:   baseDir,
		globalDir: globalDir,
		nameInput: ti,
	}

	if !hasPresets {
		m.step = stepAPIKey
		m.setup = NewSetupModel(globalDir)
	} else {
		m.step = stepPickPreset
		m.presets, _ = preset.List()
	}

	return m
}

func (m FirstRunModel) Init() tea.Cmd {
	switch m.step {
	case stepAPIKey:
		return m.setup.Init()
	case stepPickPreset:
		return nil
	}
	return nil
}

func (m FirstRunModel) Update(msg tea.Msg) (FirstRunModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case SetupDoneMsg:
		// API key saved -> create default preset -> move to preset picker
		preset.EnsureDefault()
		m.presets, _ = preset.List()
		m.step = stepPickPreset
		return m, nil

	case tea.KeyMsg:
		switch m.step {
		case stepAPIKey:
			var cmd tea.Cmd
			m.setup, cmd = m.setup.Update(msg)
			return m, cmd

		case stepPickPreset:
			switch msg.String() {
			case "up":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down":
				if m.cursor < len(m.presets)-1 {
					m.cursor++
				}
			case "enter":
				if m.cursor < len(m.presets) {
					// Move to name prompt
					m.step = stepNameAgent
					defaultName := m.presets[m.cursor].Name
					m.nameInput.SetValue(defaultName)
					m.nameInput.Focus()
					return m, textinput.Blink
				}
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil

		case stepNameAgent:
			switch msg.String() {
			case "enter":
				name := m.nameInput.Value()
				if name == "" {
					name = m.presets[m.cursor].Name
				}
				// Generate init.json and launch
				p := m.presets[m.cursor]
				if err := preset.GenerateInitJSON(p, name, m.baseDir); err != nil {
					m.message = fmt.Sprintf("Error: %v", err)
					return m, nil
				}
				m.step = stepLaunching
				m.message = i18n.TF("firstrun.created", name)

				orchDir := filepath.Join(m.baseDir, name)
				orchName := name
				return m, func() tea.Msg {
					return FirstRunDoneMsg{OrchDir: orchDir, OrchName: orchName}
				}
			case "esc":
				m.step = stepPickPreset
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			default:
				var cmd tea.Cmd
				m.nameInput, cmd = m.nameInput.Update(msg)
				return m, cmd
			}
		}
	}
	return m, nil
}

func (m FirstRunModel) View() string {
	var b strings.Builder

	// Title
	title := StyleTitle.Render("  " + i18n.T("firstrun.welcome"))
	b.WriteString(title + "\n")
	b.WriteString(strings.Repeat("─", m.width) + "\n\n")

	switch m.step {
	case stepAPIKey:
		b.WriteString("  " + i18n.T("firstrun.no_presets") + "\n\n")
		b.WriteString(m.setup.View())

	case stepPickPreset:
		b.WriteString("  " + i18n.T("firstrun.pick_preset") + "\n\n")
		for i, p := range m.presets {
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}
			name := lipgloss.NewStyle().Bold(true).Render(p.Name)
			desc := StyleSubtle.Render("  " + p.Description)
			b.WriteString(cursor + name + desc + "\n")
		}
		b.WriteString("\n" + StyleSubtle.Render("  ↑↓ select  [enter] choose") + "\n")

	case stepNameAgent:
		selectedPreset := m.presets[m.cursor].Name
		b.WriteString("  " + i18n.TF("firstrun.enter_name", selectedPreset) + "\n\n")
		b.WriteString("  " + m.nameInput.View() + "\n\n")
		b.WriteString(StyleSubtle.Render("  [Enter] Create    [Esc] Back") + "\n")

	case stepLaunching:
		b.WriteString("  " + i18n.T("firstrun.launching") + "\n\n")
		if m.message != "" {
			b.WriteString("  " + m.message + "\n")
		}
	}

	return b.String()
}
