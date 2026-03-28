package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// bootstrapDoneMsg signals that background setup (venv + assets) finished.
type bootstrapDoneMsg struct{}

// bootstrapErrMsg signals that background setup failed.
type bootstrapErrMsg struct{ err string }

type firstRunStep int

const (
	stepWelcome firstRunStep = iota
	stepAPIKey
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
	focusOnDir bool // legacy — replaced by fieldIdx
	fieldIdx   int  // 0=name, 1=dir, 2=lang, 3=stamina, 4=context_limit, 5=soul_delay, 6=molt_pressure
	// Agent config text inputs
	agentLangIdx     int // cycle: 0=en, 1=zh, 2=wen
	staminaInput     textinput.Model
	ctxLimitInput    textinput.Model
	soulDelayInput   textinput.Model
	moltPressInput   textinput.Model
	// Welcome page language selector
	langCursor   int
	welcomeOnly  bool // true when opened from /settings (return to mail after language pick)
	// Bootstrap state (venv + assets install)
	setupDone    bool   // true when bootstrap goroutine finishes
	setupErr     string // non-empty if bootstrap failed
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

	si := textinput.New()
	si.CharLimit = 10
	si.Width = 15
	si.Prompt = ""

	ci := textinput.New()
	ci.CharLimit = 10
	ci.Width = 15
	ci.Prompt = ""

	sdi := textinput.New()
	sdi.CharLimit = 10
	sdi.Width = 15
	sdi.Prompt = ""

	mpi := textinput.New()
	mpi.CharLimit = 6
	mpi.Width = 15
	mpi.Prompt = ""

	// Load existing keys from Config.Keys
	cfg, _ := config.LoadConfig(globalDir)
	existingKeys := cfg.Keys
	if existingKeys == nil {
		existingKeys = make(map[string]string)
	}

	// Pre-set language cursor from global config
	langCursor := 0
	langOptions := []string{"en", "zh", "wen"}
	if cfg.Language != "" {
		for i, l := range langOptions {
			if l == cfg.Language {
				langCursor = i
				break
			}
		}
	}

	m := FirstRunModel{
		step:            stepWelcome,
		baseDir:         baseDir,
		globalDir:       globalDir,
		nameInput:       ti,
		dirInput:        di,
		hasPresets:      hasPresets,
		langCursor:      langCursor,
		quickProvider:   qp,
		quickModel:      qm,
		presetKeyInput:  pki,
		existingKeys:    existingKeys,
		staminaInput:    si,
		ctxLimitInput:   ci,
		soulDelayInput:  sdi,
		moltPressInput:  mpi,
	}

	return m
}

func (m FirstRunModel) Init() tea.Cmd {
	if m.welcomeOnly {
		// Already bootstrapped — immediately signal done
		return func() tea.Msg { return bootstrapDoneMsg{} }
	}
	return m.runBootstrap
}

// runBootstrap runs venv creation + asset population in a goroutine.
func (m FirstRunModel) runBootstrap() tea.Msg {
	// Venv (slow — creates venv + pip install). Quiet mode: no stdout/stderr leak.
	if err := config.EnsureVenvQuiet(m.globalDir); err != nil {
		return bootstrapErrMsg{err: err.Error()}
	}
	// Assets + default presets (fast)
	if err := preset.Bootstrap(m.globalDir); err != nil {
		return bootstrapErrMsg{err: err.Error()}
	}
	return bootstrapDoneMsg{}
}

func (m FirstRunModel) Update(msg tea.Msg) (FirstRunModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case bootstrapDoneMsg:
		m.setupDone = true
		return m, nil

	case bootstrapErrMsg:
		m.setupDone = true
		m.setupErr = msg.err
		return m, nil

	case SetupDoneMsg:
		// API key saved -> move to preset picker (presets already created by Bootstrap)
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
		case stepWelcome:
			langs := []string{"en", "zh", "wen"}
			switch msg.String() {
			case "up":
				if m.langCursor > 0 {
					m.langCursor--
					i18n.SetLang(langs[m.langCursor])
				}
			case "down":
				if m.langCursor < len(langs)-1 {
					m.langCursor++
					i18n.SetLang(langs[m.langCursor])
				}
			case "enter":
				if !m.setupDone {
					return m, nil // blocked — still installing
				}
				lang := langs[m.langCursor]
				// Save language to global config
				cfg, _ := config.LoadConfig(m.globalDir)
				cfg.Language = lang
				config.SaveConfig(m.globalDir, cfg)
				// Opened from /settings — return to mail
				if m.welcomeOnly {
					return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
				}
				// Reload keys after potential config change
				m.existingKeys = cfg.Keys
				if m.existingKeys == nil {
					m.existingKeys = make(map[string]string)
				}
				// Bootstrap created presets — check if API key needed
				m.hasPresets = preset.HasAny()
				if !m.hasPresets {
					m.step = stepAPIKey
					m.setup = NewSetupModel(m.globalDir)
					return m, m.setup.Init()
				}
				m.step = stepPickPreset
				m.presets, _ = preset.List()
				return m, nil
			case "esc":
				if m.welcomeOnly {
					// Restore original language and return
					cfg, _ := config.LoadConfig(m.globalDir)
					if cfg.Language != "" {
						i18n.SetLang(cfg.Language)
					}
					return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
				}
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil

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
					m.enterAgentNameDir(p)
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
					// Save key to Config.Keys (preserve existing config fields)
					m.existingKeys[m.selectedProvider] = key
					cfg, _ := config.LoadConfig(m.globalDir)
					cfg.Keys = m.existingKeys
					config.SaveConfig(m.globalDir, cfg)
				} else if m.existingKeys[m.selectedProvider] == "" {
					// Empty and no existing key, require input
					return m, nil
				}
				// Proceed to name/dir
				p := m.presets[m.cursor]
				m.enterAgentNameDir(p)
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
			langs := []string{"en", "zh", "wen"}
			switch msg.String() {
			case "tab", "down":
				m.fieldIdx = (m.fieldIdx + 1) % agentNameDirFieldCount
				return m, m.focusAgentField()
			case "up":
				m.fieldIdx = (m.fieldIdx - 1 + agentNameDirFieldCount) % agentNameDirFieldCount
				return m, m.focusAgentField()
			case "left":
				if m.fieldIdx == 2 { // language cycle
					m.agentLangIdx = (m.agentLangIdx - 1 + len(langs)) % len(langs)
				}
				return m, nil
			case "right":
				if m.fieldIdx == 2 { // language cycle
					m.agentLangIdx = (m.agentLangIdx + 1) % len(langs)
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
				// Parse numeric fields
				stamina, err := strconv.ParseFloat(m.staminaInput.Value(), 64)
				if err != nil || stamina <= 0 {
					stamina = 36000
				}
				ctxLimit, err := strconv.Atoi(m.ctxLimitInput.Value())
				if err != nil || ctxLimit <= 0 {
					ctxLimit = 200000
				}
				soulDelay, err := strconv.ParseFloat(m.soulDelayInput.Value(), 64)
				if err != nil || soulDelay <= 0 {
					soulDelay = 120
				}
				moltPress, err := strconv.ParseFloat(m.moltPressInput.Value(), 64)
				if err != nil || moltPress <= 0 || moltPress > 1 {
					moltPress = 0.8
				}
				// Generate init.json and launch
				p := m.presets[m.cursor]
				opts := preset.AgentOpts{
					Language:     langs[m.agentLangIdx],
					Stamina:      stamina,
					ContextLimit: ctxLimit,
					SoulDelay:    soulDelay,
					MoltPressure: moltPress,
				}
				if err := preset.GenerateInitJSONWithOpts(p, m.agentName, dirName, m.baseDir, m.globalDir, opts); err != nil {
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
				switch m.fieldIdx {
				case 0:
					m.nameInput, cmd = m.nameInput.Update(msg)
				case 1:
					m.dirInput, cmd = m.dirInput.Update(msg)
				case 3:
					m.staminaInput, cmd = m.staminaInput.Update(msg)
				case 4:
					m.ctxLimitInput, cmd = m.ctxLimitInput.Update(msg)
				case 5:
					m.soulDelayInput, cmd = m.soulDelayInput.Update(msg)
				case 6:
					m.moltPressInput, cmd = m.moltPressInput.Update(msg)
				}
				return m, cmd
			}
		}
	}
	return m, nil
}

func (m FirstRunModel) View() string {
	var b strings.Builder

	switch m.step {
	case stepWelcome:
		return m.viewWelcome()
	default:
		// non-welcome steps: show standard title bar
	}

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

		langs := []string{"en", "zh", "wen"}

		// Helper: cursor prefix for field index
		cur := func(idx int) string {
			if idx == m.fieldIdx {
				return "> "
			}
			return "  "
		}

		// 0: Name
		b.WriteString(cur(0) + i18n.T("firstrun.agent_name") + ": " + m.nameInput.View() + "\n")

		// 1: Dir
		b.WriteString(cur(1) + i18n.T("firstrun.agent_dir") + ": " + m.dirInput.View() + "\n")

		// 2: Language (cycle selector)
		langVal := langs[m.agentLangIdx]
		if m.fieldIdx == 2 {
			langVal = lipgloss.NewStyle().Bold(true).Foreground(ColorActive).Render("< " + langVal + " >")
		}
		b.WriteString(cur(2) + i18n.T("firstrun.language") + ": " + langVal + "\n")

		// 3-6: Numeric text inputs with hints
		type numField struct {
			idx   int
			label string
			hint  string
			view  string
		}
		numFields := []numField{
			{3, i18n.T("firstrun.stamina"), i18n.T("firstrun.stamina_hint"), m.staminaInput.View()},
			{4, i18n.T("firstrun.context_limit"), i18n.T("firstrun.context_limit_hint"), m.ctxLimitInput.View()},
			{5, i18n.T("firstrun.soul_delay"), i18n.T("firstrun.soul_delay_hint"), m.soulDelayInput.View()},
			{6, i18n.T("firstrun.molt_pressure"), i18n.T("firstrun.molt_pressure_hint"), m.moltPressInput.View()},
		}
		for _, nf := range numFields {
			hint := StyleFaint.Render(" (" + nf.hint + ")")
			b.WriteString(cur(nf.idx) + nf.label + ": " + nf.view + hint + "\n")
		}

		if m.message != "" {
			errStyle := lipgloss.NewStyle().Foreground(ColorSuspended)
			b.WriteString("\n  " + errStyle.Render(m.message) + "\n")
		}
		b.WriteString("\n" + StyleFaint.Render("  ↑↓ "+i18n.T("firstrun.toggle_field")+
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

// viewWelcome renders the welcome/language selection page.
func (m FirstRunModel) viewWelcome() string {
	langLabels := []string{"English", "现代汉语", "文言"}

	// Build content lines (without vertical centering first)
	var content strings.Builder

	// Braille logo (𢘐 — U+22610)
	logoLines := []string{
		"⠀⠀⠀⠀⠀⣶⣤⠀⠀⠀⠀⠀⠀⠀⣰⣶⡤⠀⠀⠀⠀⠀⠀⠀⠀",
		"⠀⠀⠀⠀⠀⣿⡇⠀⠀⠀⠀⠀⠀⢰⣿⠏⢷⡀⠀⠀⠀⠀⠀⠀⠀",
		"⠀⠀⠀⡀⠀⣿⡇⠰⣄⠀⠀⠀⣠⣿⠋⠀⠈⢻⣦⡀⠀⠀⠀⠀⠀",
		"⠀⠀⢸⡇⠀⣿⡇⠀⢹⣷⡄⣴⠟⠁⠀⠀⠀⠀⠙⢿⣦⣄⠀⠀⠀",
		"⠀⣠⣿⠇⠀⣿⡇⠀⠈⢛⡿⠁⠀⠀⠀⠀⠀⠀⠀⠀⠙⠿⣿⣶⡄",
		"⠘⠿⠋⠀⠀⣿⡇⠠⠖⠋⣤⣤⠤⠤⠤⣤⣤⠤⠤⠴⠾⠷⠄⠁⠀",
		"⠀⠀⠀⠀⠀⣿⡇⠀⠀⠀⠀⠀⠀⠀⠀⣿⣿⠀⠀⠀⠀⠀⠀⠀⠀",
		"⠀⠀⠀⠀⠀⣿⡇⠀⠀⠀⠀⠀⠀⠀⠀⣿⣿⠀⠀⠀⠀⠀⠀⠀⠀",
		"⠀⠀⠀⠀⠀⣿⡇⠀⠀⠀⠀⠀⠀⠀⠀⣿⣿⠀⠀⠀⠀⣠⣤⡀⠀",
		"⠀⠀⠀⠀⠀⠿⠃⠀⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠉⠁",
	}
	logoStyle := lipgloss.NewStyle().Foreground(ColorAgent)
	for _, line := range logoLines {
		content.WriteString(centerText(logoStyle.Render(line), m.width) + "\n")
	}
	content.WriteString("\n")

	// Product name
	titleText := i18n.T("welcome.title")
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent)
	content.WriteString(centerText(titleStyle.Render(titleText), m.width) + "\n\n")

	// Poem (two lines)
	poemStyle := StyleSubtle
	content.WriteString(centerText(poemStyle.Render(i18n.T("welcome.poem_line1")), m.width) + "\n")
	content.WriteString(centerText(poemStyle.Render(i18n.T("welcome.poem_line2")), m.width) + "\n\n\n")

	// Language selector
	for i, label := range langLabels {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(ColorText)
		if i == m.langCursor {
			cursor = "> "
			style = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
		}
		line := cursor + style.Render(label)
		content.WriteString(centerText(line, m.width) + "\n")
	}

	// Bootstrap status
	if !m.welcomeOnly {
		content.WriteString("\n")
		if m.setupErr != "" {
			errStyle := lipgloss.NewStyle().Foreground(ColorSuspended)
			content.WriteString(centerText(errStyle.Render(i18n.TF("welcome.setup_failed", m.setupErr)), m.width) + "\n")
		} else if m.setupDone {
			doneStyle := lipgloss.NewStyle().Foreground(ColorAgent)
			content.WriteString(centerText(doneStyle.Render(i18n.T("welcome.ready")), m.width) + "\n")
		} else {
			content.WriteString(centerText(StyleFaint.Render(i18n.T("welcome.installing")), m.width) + "\n")
		}
	}

	// Footer hints
	content.WriteString("\n")
	var hints string
	if m.setupDone || m.welcomeOnly {
		hints = StyleFaint.Render("↑↓ " + i18n.T("welcome.select_lang") + "  [Enter] " + i18n.T("welcome.confirm"))
	} else {
		hints = StyleFaint.Render("↑↓ " + i18n.T("welcome.select_lang") + "  (" + i18n.T("welcome.installing") + ")")
	}
	content.WriteString(centerText(hints, m.width) + "\n")

	// Vertical centering: pad top to center the content block
	contentStr := content.String()
	contentLines := strings.Count(contentStr, "\n")
	topPad := (m.height - contentLines) / 2
	if topPad < 1 {
		topPad = 1
	}

	return strings.Repeat("\n", topPad) + contentStr
}

// centerText centers a string within the given width.
func centerText(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	pad := (width - w) / 2
	return strings.Repeat(" ", pad) + s
}

// agentNameDirFieldCount is the number of fields in stepAgentNameDir.
const agentNameDirFieldCount = 7 // name, dir, lang, stamina, ctx_limit, soul_delay, molt_pressure

// enterAgentNameDir initialises all fields and transitions to stepAgentNameDir.
func (m *FirstRunModel) enterAgentNameDir(p preset.Preset) {
	defaultName := p.Name
	m.agentName = defaultName
	m.agentDir = defaultName
	m.nameInput.SetValue(defaultName)
	m.dirInput.SetValue(defaultName)
	m.fieldIdx = 0
	m.focusOnDir = false
	m.nameInput.Focus()
	m.dirInput.Blur()

	// Language — inherit from preset, fallback "en"
	m.agentLangIdx = 0
	if l, ok := p.Manifest["language"].(string); ok {
		for i, lang := range []string{"en", "zh", "wen"} {
			if lang == l {
				m.agentLangIdx = i
				break
			}
		}
	}

	// Numeric defaults
	m.staminaInput.SetValue("36000")
	m.ctxLimitInput.SetValue("200000")
	m.soulDelayInput.SetValue("120")
	m.moltPressInput.SetValue("0.8")
	m.staminaInput.Blur()
	m.ctxLimitInput.Blur()
	m.soulDelayInput.Blur()
	m.moltPressInput.Blur()

	m.step = stepAgentNameDir
}

// focusAgentField focuses the input at m.fieldIdx and blurs all others.
// Returns the blink command for the newly focused input.
func (m *FirstRunModel) focusAgentField() tea.Cmd {
	m.nameInput.Blur()
	m.dirInput.Blur()
	m.staminaInput.Blur()
	m.ctxLimitInput.Blur()
	m.soulDelayInput.Blur()
	m.moltPressInput.Blur()
	m.focusOnDir = false

	switch m.fieldIdx {
	case 0:
		return m.nameInput.Focus()
	case 1:
		m.focusOnDir = true
		return m.dirInput.Focus()
	case 2:
		return nil // language — cycle selector, no text input
	case 3:
		return m.staminaInput.Focus()
	case 4:
		return m.ctxLimitInput.Focus()
	case 5:
		return m.soulDelayInput.Focus()
	case 6:
		return m.moltPressInput.Focus()
	}
	return nil
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
