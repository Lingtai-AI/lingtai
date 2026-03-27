package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
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
	stepAPIKey firstRunStep = iota
	stepPickPreset
	stepPresetKey
	stepEditPreset
	stepNewPreset
	stepAgentNameDir
	stepLaunching
)

// stepCount is the total number of wizard steps (for progress display)
const totalSteps = 4

// stepProgress returns the 1-based index and total for progress display
func stepProgress(step firstRunStep, hasPresets bool) (current int, total int) {
	total = totalSteps
	switch {
	case !hasPresets && step == stepAPIKey:
		return 1, total
	case !hasPresets && step == stepPickPreset:
		return 2, total
	case step == stepPickPreset || step == stepPresetKey:
		return 1, total
	case step == stepEditPreset || step == stepNewPreset:
		return 2, total
	case step == stepAgentNameDir:
		return 3, total
	case step == stepLaunching:
		return 4, total
	}
	return 1, total
}

// FirstRunModel orchestrates the first-run experience.
type FirstRunModel struct {
	step       firstRunStep
	setup      SetupModel
	presets    []preset.Preset
	cursor     int
	nameInput  textinput.Model
	dirInput   textinput.Model
	agentName  string
	agentDir   string
	message    string
	baseDir    string // .lingtai/ directory
	globalDir  string
	width      int
	height     int
	hasPresets bool
	// Embedded preset editor
	editPreset  preset.Preset
	editFields  []presetField
	editCursor  int
	// Focus state for combined name+dir step
	focusOnDir bool
	// Quick config (provider/model) in preset selection
	quickProvider textinput.Model
	quickModel    textinput.Model
	quickEditMode bool
	// Embedded key input for preset's provider
	presetKeyInput   textinput.Model
	selectedProvider string // provider of currently selected preset
	existingKeys     map[string]string // loaded from Config.Keys
}

func NewFirstRunModel(baseDir, globalDir string, hasPresets bool) FirstRunModel {
	ti := textinput.New()
	ti.CharLimit = 64
	ti.Width = 40

	di := textinput.New()
	di.CharLimit = 64
	di.Width = 40

	qp := textinput.New()
	qp.CharLimit = 32
	qp.Width = 25
	qp.Prompt = ""

	qm := textinput.New()
	qm.CharLimit = 64
	qm.Width = 25
	qm.Prompt = ""

	pki := textinput.New()
	pki.CharLimit = 128
	pki.Width = 50

	// Load existing keys from Config.Keys
	cfg, _ := config.LoadConfig(globalDir)
	existingKeys := cfg.Keys
	if existingKeys == nil {
		existingKeys = make(map[string]string)
	}

	m := FirstRunModel{
		baseDir:         baseDir,
		globalDir:       globalDir,
		nameInput:       ti,
		dirInput:        di,
		hasPresets:      hasPresets,
		quickProvider:   qp,
		quickModel:      qm,
		presetKeyInput:  pki,
		existingKeys:    existingKeys,
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
		// Reload keys after setup saves
		cfg, _ := config.LoadConfig(m.globalDir)
		m.existingKeys = cfg.Keys
		if m.existingKeys == nil {
			m.existingKeys = make(map[string]string)
		}
		m.step = stepPickPreset
		return m, nil

	case tea.KeyMsg:
		switch m.step {
		case stepAPIKey:
			var cmd tea.Cmd
			m.setup, cmd = m.setup.Update(msg)
			return m, cmd

		case stepPickPreset:
			// Handle quick config mode (provider/model editing)
			if m.quickEditMode {
				switch msg.String() {
				case "esc":
					m.quickEditMode = false
					m.quickProvider.Blur()
					m.quickModel.Blur()
					return m, nil
				case "enter":
					// Apply quick config to selected preset
					if m.cursor < len(m.presets) {
						m.editPreset = m.presets[m.cursor]
						if llm, ok := m.editPreset.Manifest["llm"].(map[string]interface{}); ok {
							if m.quickProvider.Value() != "" {
								llm["provider"] = m.quickProvider.Value()
							}
							if m.quickModel.Value() != "" {
								llm["model"] = m.quickModel.Value()
							}
						}
						preset.Save(m.editPreset)
						m.presets, _ = preset.List()
					}
					m.quickEditMode = false
					m.quickProvider.Blur()
					m.quickModel.Blur()
					return m, nil
				default:
					var cmd tea.Cmd
					m.quickProvider, _ = m.quickProvider.Update(msg)
					m.quickModel, cmd = m.quickModel.Update(msg)
					return m, cmd
				}
			}
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
					p := m.presets[m.cursor]
					provider := m.getPresetProvider(p)
					m.selectedProvider = provider
					// Check if key is needed and missing
					if m.needsKey(provider) {
						m.step = stepPresetKey
						m.presetKeyInput.Reset()
						m.presetKeyInput.Focus()
						return m, textinput.Blink
					}
					// Key exists, proceed to name/dir
					m.step = stepAgentNameDir
					defaultName := p.Name
					m.agentName = defaultName
					m.agentDir = defaultName
					m.nameInput.SetValue(defaultName)
					m.dirInput.SetValue(defaultName)
					m.focusOnDir = false
					m.nameInput.Focus()
					return m, textinput.Blink
				}
			case "e":
				if m.cursor < len(m.presets) {
					m.editPreset = m.presets[m.cursor]
					m.editFields = buildEditFields(m.editPreset)
					m.editCursor = 0
					m.step = stepEditPreset
				}
			case "p":
				// Quick config: edit provider/model without entering full editor
				if m.cursor < len(m.presets) {
					m.quickEditMode = true
					p := m.presets[m.cursor]
					var provider, modelVal string
					if llm, ok := p.Manifest["llm"].(map[string]interface{}); ok {
						if v, ok := llm["provider"].(string); ok {
							provider = v
						}
						if v, ok := llm["model"].(string); ok {
							modelVal = v
						}
					}
					m.quickProvider.SetValue(provider)
					m.quickModel.SetValue(modelVal)
					m.quickProvider.Focus()
				}
			case "n":
				m.nameInput.SetValue("")
				m.nameInput.Focus()
				m.step = stepNewPreset
				return m, textinput.Blink
			case "esc":
				// Already at start, ctrl+c to quit
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil

		case stepPresetKey:
			// Embedded key input for selected preset's provider
			switch msg.String() {
			case "esc":
				m.step = stepPickPreset
				return m, nil
			case "enter":
				key := m.presetKeyInput.Value()
				if key != "" {
					// Save key to Config.Keys
					m.existingKeys[m.selectedProvider] = key
					cfg := config.Config{Keys: m.existingKeys}
					config.SaveConfig(m.globalDir, cfg)
				} else if m.existingKeys[m.selectedProvider] == "" {
					// Empty and no existing key, require input
					return m, nil
				}
				// Proceed to name/dir
				m.step = stepAgentNameDir
				p := m.presets[m.cursor]
				defaultName := p.Name
				m.agentName = defaultName
				m.agentDir = defaultName
				m.nameInput.SetValue(defaultName)
				m.dirInput.SetValue(defaultName)
				m.focusOnDir = false
				m.nameInput.Focus()
				return m, textinput.Blink
			case "ctrl+c":
				return m, tea.Quit
			default:
				var cmd tea.Cmd
				m.presetKeyInput, cmd = m.presetKeyInput.Update(msg)
				return m, cmd
			}

		case stepEditPreset:
			switch msg.String() {
			case "esc":
				preset.Save(m.editPreset)
				m.presets, _ = preset.List()
				m.step = stepPickPreset
			case "up":
				if m.editCursor > 0 {
					m.editCursor--
				}
			case "down":
				if m.editCursor < len(m.editFields)-1 {
					m.editCursor++
				}
			case "left":
				f := &m.editFields[m.editCursor]
				if !f.IsBool && f.Current > 0 {
					f.Current--
					applyEditField(&m.editPreset, f)
				}
			case "right":
				f := &m.editFields[m.editCursor]
				if !f.IsBool && f.Current < len(f.Options)-1 {
					f.Current++
					applyEditField(&m.editPreset, f)
				}
			case " ":
				// Space toggles capability on/off for boolean fields
				f := &m.editFields[m.editCursor]
				if f.IsBool {
					if f.Current == 0 {
						f.Current = 1
					} else {
						f.Current = 0
					}
					applyEditField(&m.editPreset, f)
				}
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil

		case stepNewPreset:
			switch msg.String() {
			case "esc":
				m.step = stepPickPreset
			case "enter":
				name := m.nameInput.Value()
				if name == "" {
					return m, nil
				}
				p := preset.Preset{
					Name: name,
					Manifest: map[string]interface{}{
						"llm": map[string]interface{}{
							"provider":    "minimax",
							"model":       "MiniMax-M2.7-highspeed",
							"api_key":     nil,
							"api_key_env": "MINIMAX_API_KEY",
						},
						"capabilities": map[string]interface{}{"file": map[string]interface{}{}},
						"admin":        map[string]interface{}{"karma": true},
					},
				}
				preset.Save(p)
				m.presets, _ = preset.List()
				m.editPreset = p
				m.editFields = buildEditFields(p)
				m.editCursor = 0
				m.step = stepEditPreset
			case "ctrl+c":
				return m, tea.Quit
			default:
				var cmd tea.Cmd
				m.nameInput, cmd = m.nameInput.Update(msg)
				return m, cmd
			}
			return m, nil

		case stepAgentNameDir:
			switch msg.String() {
			case "tab":
				// Toggle focus between name and dir
				m.focusOnDir = !m.focusOnDir
				if m.focusOnDir {
					m.dirInput.Focus()
				} else {
					m.nameInput.Focus()
				}
				return m, nil
			case "enter":
				name := m.nameInput.Value()
				if name == "" {
					name = m.presets[m.cursor].Name
				}
				dirName := m.dirInput.Value()
				if dirName == "" {
					dirName = name
				}
				m.agentName = name
				m.agentDir = dirName
				// Check collision
				orchDir := filepath.Join(m.baseDir, dirName)
				if _, err := os.Stat(orchDir); err == nil {
					m.message = i18n.TF("firstrun.dir_exists", dirName)
					return m, nil
				}
				// Generate init.json and launch
				p := m.presets[m.cursor]
				if err := preset.GenerateInitJSON(p, m.agentName, dirName, m.baseDir, m.globalDir); err != nil {
					m.message = i18n.TF("firstrun.error", err)
					return m, nil
				}
				m.step = stepLaunching
				m.message = i18n.TF("firstrun.created", m.agentName)
				return m, func() tea.Msg {
					return FirstRunDoneMsg{OrchDir: orchDir, OrchName: m.agentName}
				}
			case "esc":
				m.step = stepPickPreset
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			default:
				var cmd tea.Cmd
				if !m.focusOnDir {
					m.nameInput, cmd = m.nameInput.Update(msg)
				} else {
					m.dirInput, cmd = m.dirInput.Update(msg)
				}
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
		stepNum, total := stepProgress(m.step, m.hasPresets)
		b.WriteString("\n  " + StyleSubtle.Render(fmt.Sprintf("Step %d/%d", stepNum, total)) + "\n\n")
		b.WriteString("  " + i18n.T("firstrun.no_presets") + "\n\n")
		b.WriteString(m.setup.View())

	case stepPickPreset:
		stepNum, total := stepProgress(m.step, m.hasPresets)
		b.WriteString("\n  " + StyleSubtle.Render(fmt.Sprintf("Step %d/%d: "+i18n.T("firstrun.pick_preset"), stepNum, total)) + "\n\n")
		for i, p := range m.presets {
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}
			name := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent).Render(p.Name)
			desc := StyleSubtle.Render("  " + p.Description)
			b.WriteString(cursor + name + desc + "\n")
		}
		// Quick config panel
		if m.quickEditMode {
			b.WriteString("\n  " + StyleTitle.Render(i18n.T("presets.quick_config")) + "\n\n")
			b.WriteString("  Provider: " + m.quickProvider.View() + "\n")
			b.WriteString("  Model:    " + m.quickModel.View() + "\n")
			b.WriteString("\n" + StyleFaint.Render("  [Enter] "+i18n.T("presets.apply")+
				"  [Esc] "+i18n.T("presets.cancel")) + "\n")
		} else {
			b.WriteString("\n" + StyleFaint.Render("  "+i18n.T("firstrun.select_hint")+
				"  [e] "+i18n.T("presets.edit")+
				"  [p] "+i18n.T("presets.quick_config")+
				"  [n] "+i18n.T("presets.new")) + "\n")
			b.WriteString(StyleFaint.Render("  [Ctrl+C] "+i18n.T("common.quit")) + "\n")
		}

	case stepPresetKey:
		providerName := m.selectedProvider
		if providerName == "minimax" {
			providerName = "MiniMax"
		} else if providerName == "gemini" {
			providerName = "Gemini"
		} else {
			providerName = "Custom"
		}
		b.WriteString("  " + i18n.TF("firstrun.enter_provider_key", providerName) + "\n\n")
		b.WriteString("  " + i18n.T("setup.api_key_label") + " " + m.presetKeyInput.View() + "\n\n")
		b.WriteString(StyleFaint.Render("  [Enter] "+i18n.T("setup.save")+
			"  [Esc] "+i18n.T("setup.back")) + "\n")

	case stepEditPreset:
		stepNum, total := stepProgress(m.step, m.hasPresets)
		b.WriteString("\n  " + StyleSubtle.Render(fmt.Sprintf("Step %d/%d: "+i18n.TF("presets.editor_title", m.editPreset.Name), stepNum, total)) + "\n\n")
		capStarted := false
		for idx, f := range m.editFields {
			if strings.HasPrefix(f.Key, "cap:") && !capStarted {
				capStarted = true
				b.WriteString("\n  " + i18n.T("presets.capabilities") + ":\n")
			}
			cursor := "  "
			if idx == m.editCursor {
				cursor = "> "
			}
			var label string
			if strings.HasPrefix(f.Key, "cap:") {
				label = f.Label
			} else {
				label = i18n.T(f.Label)
			}
			displayVal := f.Options[f.Current]
			if f.IsBool {
				if displayVal == "true" {
					displayVal = "[x]"
				} else {
					displayVal = "[ ]"
				}
			}
			if idx == m.editCursor {
				if f.IsBool {
					displayVal = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render(displayVal)
				} else {
					displayVal = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render("< "+displayVal+" >")
				}
			}
			if strings.HasPrefix(f.Key, "cap:") {
				b.WriteString(cursor + displayVal + " " + label + "\n")
			} else {
				b.WriteString(cursor + label + ": " + displayVal + "\n")
			}
		}
		b.WriteString("\n" + StyleFaint.Render("  ↑↓ "+i18n.T("settings.select")+
			"  ←→/space "+i18n.T("settings.change")+
			"  [esc] "+i18n.T("presets.back")) + "\n")

	case stepNewPreset:
		stepNum, total := stepProgress(m.step, m.hasPresets)
		b.WriteString("\n  " + StyleSubtle.Render(fmt.Sprintf("Step %d/%d: "+i18n.T("presets.enter_name"), stepNum, total)) + "\n\n")
		b.WriteString("  " + m.nameInput.View() + "\n\n")
		b.WriteString(StyleFaint.Render("  [Enter] "+i18n.T("presets.create")+
			"  [Esc] "+i18n.T("presets.cancel")) + "\n")

	case stepAgentNameDir:
		stepNum, total := stepProgress(m.step, m.hasPresets)
		b.WriteString("\n  " + StyleSubtle.Render(fmt.Sprintf("Step %d/%d: "+i18n.T("firstrun.enter_name_dir"), stepNum, total)) + "\n\n")

		// Name field
		nameCursor := "  "
		if !m.focusOnDir {
			nameCursor = "> "
		}
		b.WriteString(nameCursor + i18n.T("firstrun.agent_name") + ": " + m.nameInput.View() + "\n")

		// Dir field
		dirCursor := "  "
		if m.focusOnDir {
			dirCursor = "> "
		}
		b.WriteString(dirCursor + i18n.T("firstrun.agent_dir") + ": " + m.dirInput.View() + "\n")

		if m.message != "" {
			errStyle := lipgloss.NewStyle().Foreground(ColorSuspended)
			b.WriteString("\n  " + errStyle.Render(m.message) + "\n")
		}
		b.WriteString("\n" + StyleFaint.Render("  [Tab] "+i18n.T("firstrun.toggle_field")+
			"  [Enter] "+i18n.T("firstrun.create_agent")+
			"  [Esc] "+i18n.T("firstrun.back")) + "\n")

	case stepLaunching:
		stepNum, total := stepProgress(m.step, m.hasPresets)
		b.WriteString("\n  " + StyleSubtle.Render(fmt.Sprintf("Step %d/%d: ", stepNum, total)) + i18n.T("firstrun.launching") + "\n\n")
		if m.message != "" {
			b.WriteString("  " + m.message + "\n")
		}
	}

	return b.String()
}

// getPresetProvider extracts provider name from a preset
func (m FirstRunModel) getPresetProvider(p preset.Preset) string {
	if llm, ok := p.Manifest["llm"].(map[string]interface{}); ok {
		if provider, ok := llm["provider"].(string); ok {
			return provider
		}
	}
	return "minimax" // default
}

// needsKey returns true if the provider's key is not configured
func (m FirstRunModel) needsKey(provider string) bool {
	_, hasKey := m.existingKeys[provider]
	return !hasKey
}

// presetNeedsKey returns true if the preset's provider key is missing (for warning display)
func (m FirstRunModel) presetNeedsKey(p preset.Preset) bool {
	provider := m.getPresetProvider(p)
	return m.needsKey(provider)
}
