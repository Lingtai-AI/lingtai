package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// InputModel wraps a textinput with slash-command palette detection.
type InputModel struct {
	textInput   textinput.Model
	showPalette bool
	width       int
}

func NewInputModel() InputModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.CharLimit = 1000
	return InputModel{textInput: ti}
}

func (m InputModel) Init() tea.Cmd {
	return nil
}

func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if m.showPalette {
				m.showPalette = false
				m.textInput.SetValue("")
				return m, nil
			}
		case "enter":
			// Don't handle here — parent handles enter
			return m, nil
		}
		// Forward to textinput
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		// After update, check if slash is first char
		newVal := m.textInput.Value()
		if len(newVal) > 0 && newVal[0] == '/' {
			m.showPalette = true
		} else {
			m.showPalette = false
		}
		return m, cmd
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m InputModel) View() string {
	hint := lipgloss.NewStyle().Foreground(ColorSubtle).Render("[/]")
	input := m.textInput.View()
	// Calculate padding
	usedWidth := lipgloss.Width(input) + lipgloss.Width(hint) + 4 // "  > " prefix
	pad := ""
	if m.width > usedWidth {
		for i := 0; i < m.width-usedWidth; i++ {
			pad += " "
		}
	}
	return "  > " + input + pad + hint
}

func (m InputModel) Value() string {
	return m.textInput.Value()
}

func (m *InputModel) SetValue(s string) {
	m.textInput.SetValue(s)
	if len(s) > 0 && s[0] == '/' {
		m.showPalette = true
	} else {
		m.showPalette = false
	}
}

func (m *InputModel) Reset() {
	m.textInput.SetValue("")
	m.showPalette = false
}

func (m *InputModel) Focus() tea.Cmd {
	return m.textInput.Focus()
}

func (m *InputModel) Blur() {
	m.textInput.Blur()
}

func (m InputModel) Focused() bool {
	return m.textInput.Focused()
}

func (m InputModel) IsPaletteActive() bool {
	return m.showPalette
}

func (m *InputModel) SetWidth(w int) {
	m.width = w
	if w > 10 {
		m.textInput.Width = w - 10 // leave room for "> " prefix and "[/]" hint
	}
}
