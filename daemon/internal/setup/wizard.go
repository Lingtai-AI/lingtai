package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"lingtai-daemon/internal/config"
	"lingtai-daemon/internal/i18n"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Steps in the wizard.
type step int

const (
	StepLang step = iota
	StepModel
	StepVision
	StepWebSearch
	StepIMAP
	StepTelegram
	StepGeneral
	StepReview
)

func (s step) String() string {
	switch s {
	case StepLang:
		return i18n.S("setup_lang")
	case StepModel:
		return i18n.S("setup_model")
	case StepVision:
		return i18n.S("setup_vision") + " (Esc)"
	case StepWebSearch:
		return i18n.S("setup_websearch") + " (Esc)"
	case StepIMAP:
		return "IMAP (Esc)"
	case StepTelegram:
		return "Telegram (Esc)"
	case StepGeneral:
		return i18n.S("setup_general")
	case StepReview:
		return i18n.S("setup_review")
	default:
		return "Unknown"
	}
}

// Styles
var (
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")) // cyan
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))            // green
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))            // red
	dimStyle     = lipgloss.NewStyle().Faint(true)
	promptStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")) // white
)

// providers is the list of supported LLM providers.
var providers = []string{"minimax", "openai", "anthropic", "gemini", "custom"}

// Default endpoints for known providers (empty = provider SDK default).
var providerEndpoints = map[string]string{
	"minimax":   "https://api.minimax.chat/v1",
	"openai":    "https://api.openai.com/v1",
	"anthropic": "https://api.anthropic.com",
	"gemini":    "https://generativelanguage.googleapis.com",
	"custom":    "",
}

// Default model names for known providers.
var providerModels = map[string]string{
	"minimax":   "MiniMax-M2.7-highspeed",
	"openai":    "gpt-5.4",
	"anthropic": "claude-opus-4-6",
	"gemini":    "gemini-3.1-pro",
	"custom":    "",
}

// Vision provider defaults (only minimax and gemini supported).
var visionProviders = []string{"minimax", "gemini"}
var visionModels = map[string]string{
	"minimax": "MiniMax-M2.7-highspeed",
	"gemini":  "gemini-3.1-pro",
}
var visionEndpoints = map[string]string{
	"minimax": "https://api.minimax.chat/v1",
	"gemini":  "https://generativelanguage.googleapis.com",
}

// Web search provider defaults (only minimax and gemini supported).
var webSearchProviders = []string{"minimax", "gemini"}
var webSearchModels = map[string]string{
	"minimax": "MiniMax-M2.7-highspeed",
	"gemini":  "gemini-3.1-pro",
}
var webSearchEndpoints = map[string]string{
	"minimax": "https://api.minimax.chat/v1",
	"gemini":  "https://generativelanguage.googleapis.com",
}

// field is a labeled text input.
type field struct {
	label string
	input textinput.Model
}

// testResultMsg carries the outcome of an async connection test.
type testResultMsg struct {
	step   step
	result TestResult
}

// wizardModel is the Bubble Tea model for the setup wizard.
type wizardModel struct {
	step      step
	fields    map[step][]field
	focus     int // index of focused field within current step
	outputDir string

	// language selector state (step 0)
	langIdx int

	// provider selector state
	providerIdx          int
	visionProviderIdx    int
	webSearchProviderIdx int

	// test results per step
	testResults map[step]*TestResult

	// final status
	done    bool
	err     error
	written []string // files written
}

func newTextInput(placeholder string, defaultVal string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(defaultVal)
	ti.CharLimit = 256
	ti.Width = 50
	return ti
}

func newWizardModel(outputDir string) wizardModel {
	// Detect initial language index
	initialLangIdx := 0
	for idx, code := range i18n.Languages {
		if code == i18n.Lang {
			initialLangIdx = idx
			break
		}
	}

	m := wizardModel{
		step:        StepLang,
		outputDir:   outputDir,
		langIdx:     initialLangIdx,
		providerIdx: 0,
		testResults: make(map[step]*TestResult),
		fields:      make(map[step][]field),
	}

	// Step: Lang has no text fields (uses left/right selector)

	// Step: Model
	defaultProvider := providers[0]
	apiKeyInput := newTextInput("sk-...", "")
	apiKeyInput.EchoMode = textinput.EchoPassword
	apiKeyInput.EchoCharacter = '•'
	m.fields[StepModel] = []field{
		{label: "Provider", input: newTextInput(defaultProvider, defaultProvider)},
		{label: "Model", input: newTextInput("model name", providerModels[defaultProvider])},
		{label: "API key", input: apiKeyInput},
		{label: "Endpoint", input: newTextInput("https://...", providerEndpoints[defaultProvider])},
	}

	// Step: Vision
	defaultVisionProvider := visionProviders[0]
	visionKeyInput := newTextInput(i18n.S("setup_same_key"), "")
	visionKeyInput.EchoMode = textinput.EchoPassword
	visionKeyInput.EchoCharacter = '•'
	m.fields[StepVision] = []field{
		{label: "Provider", input: newTextInput(defaultVisionProvider, defaultVisionProvider)},
		{label: "Model", input: newTextInput("model name", visionModels[defaultVisionProvider])},
		{label: "API key", input: visionKeyInput},
		{label: "Endpoint", input: newTextInput("https://...", visionEndpoints[defaultVisionProvider])},
	}

	// Step: Web Search
	defaultWSProvider := webSearchProviders[0]
	wsKeyInput := newTextInput(i18n.S("setup_same_key"), "")
	wsKeyInput.EchoMode = textinput.EchoPassword
	wsKeyInput.EchoCharacter = '•'
	m.fields[StepWebSearch] = []field{
		{label: "Provider", input: newTextInput(defaultWSProvider, defaultWSProvider)},
		{label: "Model", input: newTextInput("model name", webSearchModels[defaultWSProvider])},
		{label: "API key", input: wsKeyInput},
		{label: "Endpoint", input: newTextInput("https://...", webSearchEndpoints[defaultWSProvider])},
	}

	// Step: IMAP
	imapPassInput := newTextInput("password", "")
	imapPassInput.EchoMode = textinput.EchoPassword
	imapPassInput.EchoCharacter = '•'
	m.fields[StepIMAP] = []field{
		{label: "Email address", input: newTextInput("you@example.com", "")},
		{label: "Password", input: imapPassInput},
		{label: "IMAP host", input: newTextInput("imap.example.com", "")},
		{label: "IMAP port", input: newTextInput("993", "993")},
		{label: "SMTP host", input: newTextInput("smtp.example.com", "")},
		{label: "SMTP port", input: newTextInput("587", "587")},
	}

	// Step: Telegram
	telegramInput := newTextInput("bot token", "")
	telegramInput.EchoMode = textinput.EchoPassword
	telegramInput.EchoCharacter = '•'
	m.fields[StepTelegram] = []field{
		{label: "Bot token", input: telegramInput},
	}

	// Step: General
	home, _ := os.UserHomeDir()
	defaultBase := filepath.Join(home, ".lingtai")
	m.fields[StepGeneral] = []field{
		{label: "Agent name", input: newTextInput("orchestrator", "orchestrator")},
		{label: "Base directory", input: newTextInput(defaultBase, defaultBase)},
		{label: "Agent port", input: newTextInput("8501", "8501")},
		{label: "Bash policy file", input: newTextInput("(optional)", "")},
		{label: "Covenant", input: newTextInput("(optional)", "")},
	}

	// Step: Review has no fields

	// Pre-fill from existing config if available
	m.loadExisting()

	// Focus the first field
	if len(m.fields[StepModel]) > 0 {
		m.fields[StepModel][0].input.Focus()
	}

	return m
}

// loadExisting reads config.json, model.json, and .env from outputDir
// and pre-fills the wizard fields so returning users see their saved values.
func (m *wizardModel) loadExisting() {
	configPath := filepath.Join(m.outputDir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return // no existing config
	}

	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return
	}

	// Load .env secrets
	config.LoadDotenv(m.outputDir)

	// Language
	var lang string
	if v, ok := raw["language"]; ok {
		json.Unmarshal(v, &lang)
		for idx, code := range i18n.Languages {
			if code == lang {
				m.langIdx = idx
				i18n.Lang = lang
				break
			}
		}
	}

	// Model — resolve from model.json or inline (use raw map to capture sub-objects)
	var modelRaw map[string]json.RawMessage
	if v, ok := raw["model"]; ok {
		var modelPath string
		if json.Unmarshal(v, &modelPath) == nil {
			// It's a file path
			modelData, err := os.ReadFile(filepath.Join(m.outputDir, modelPath))
			if err == nil {
				json.Unmarshal(modelData, &modelRaw)
			}
		} else {
			json.Unmarshal(v, &modelRaw)
		}
	}
	if modelRaw == nil {
		modelRaw = make(map[string]json.RawMessage)
	}

	// Parse main model fields
	var modelCfg struct {
		Provider  string `json:"provider"`
		Model     string `json:"model"`
		APIKeyEnv string `json:"api_key_env"`
		BaseURL   string `json:"base_url"`
	}
	// Re-marshal modelRaw back to parse the struct fields
	if b, err := json.Marshal(modelRaw); err == nil {
		json.Unmarshal(b, &modelCfg)
	}

	if modelCfg.Provider != "" {
		// Set provider index
		for idx, p := range providers {
			if p == modelCfg.Provider {
				m.providerIdx = idx
				break
			}
		}
		m.fields[StepModel][0].input.SetValue(modelCfg.Provider)
	}
	if modelCfg.Model != "" {
		m.fields[StepModel][1].input.SetValue(modelCfg.Model)
	}
	if modelCfg.APIKeyEnv != "" {
		if key := os.Getenv(modelCfg.APIKeyEnv); key != "" {
			m.fields[StepModel][2].input.SetValue(key)
		}
	}
	if modelCfg.BaseURL != "" {
		m.fields[StepModel][3].input.SetValue(modelCfg.BaseURL)
	}

	// Vision sub-config
	if vRaw, ok := modelRaw["vision"]; ok {
		var vCfg struct {
			Provider  string `json:"provider"`
			Model     string `json:"model"`
			APIKeyEnv string `json:"api_key_env"`
			BaseURL   string `json:"base_url"`
		}
		if json.Unmarshal(vRaw, &vCfg) == nil {
			if vCfg.Provider != "" {
				for idx, p := range visionProviders {
					if p == vCfg.Provider {
						m.visionProviderIdx = idx
						break
					}
				}
				m.fields[StepVision][0].input.SetValue(vCfg.Provider)
			}
			if vCfg.Model != "" {
				m.fields[StepVision][1].input.SetValue(vCfg.Model)
			}
			if vCfg.APIKeyEnv != "" {
				if key := os.Getenv(vCfg.APIKeyEnv); key != "" {
					m.fields[StepVision][2].input.SetValue(key)
				}
			}
			if vCfg.BaseURL != "" {
				m.fields[StepVision][3].input.SetValue(vCfg.BaseURL)
			}
		}
	}

	// Web search sub-config
	if wsRaw, ok := modelRaw["web_search"]; ok {
		var wsCfg struct {
			Provider  string `json:"provider"`
			Model     string `json:"model"`
			APIKeyEnv string `json:"api_key_env"`
			BaseURL   string `json:"base_url"`
		}
		if json.Unmarshal(wsRaw, &wsCfg) == nil {
			if wsCfg.Provider != "" {
				for idx, p := range webSearchProviders {
					if p == wsCfg.Provider {
						m.webSearchProviderIdx = idx
						break
					}
				}
				m.fields[StepWebSearch][0].input.SetValue(wsCfg.Provider)
			}
			if wsCfg.Model != "" {
				m.fields[StepWebSearch][1].input.SetValue(wsCfg.Model)
			}
			if wsCfg.APIKeyEnv != "" {
				if key := os.Getenv(wsCfg.APIKeyEnv); key != "" {
					m.fields[StepWebSearch][2].input.SetValue(key)
				}
			}
			if wsCfg.BaseURL != "" {
				m.fields[StepWebSearch][3].input.SetValue(wsCfg.BaseURL)
			}
		}
	}

	// IMAP
	if v, ok := raw["imap"]; ok {
		var imap struct {
			Email    string `json:"email_address"`
			PassEnv  string `json:"password_env"`
			IMAPHost string `json:"imap_host"`
			IMAPPort int    `json:"imap_port"`
			SMTPHost string `json:"smtp_host"`
			SMTPPort int    `json:"smtp_port"`
		}
		if json.Unmarshal(v, &imap) == nil {
			m.fields[StepIMAP][0].input.SetValue(imap.Email)
			if imap.PassEnv != "" {
				if pass := os.Getenv(imap.PassEnv); pass != "" {
					m.fields[StepIMAP][1].input.SetValue(pass)
				}
			}
			m.fields[StepIMAP][2].input.SetValue(imap.IMAPHost)
			if imap.IMAPPort > 0 {
				m.fields[StepIMAP][3].input.SetValue(strconv.Itoa(imap.IMAPPort))
			}
			m.fields[StepIMAP][4].input.SetValue(imap.SMTPHost)
			if imap.SMTPPort > 0 {
				m.fields[StepIMAP][5].input.SetValue(strconv.Itoa(imap.SMTPPort))
			}
		}
	}

	// Telegram
	if v, ok := raw["telegram"]; ok {
		var tg struct {
			TokenEnv string `json:"bot_token_env"`
		}
		if json.Unmarshal(v, &tg) == nil && tg.TokenEnv != "" {
			if token := os.Getenv(tg.TokenEnv); token != "" {
				m.fields[StepTelegram][0].input.SetValue(token)
			}
		}
	}

	// General
	var agentName, baseDir, bashPolicy, covenant string
	var agentPort int
	if v, ok := raw["agent_name"]; ok {
		json.Unmarshal(v, &agentName)
	}
	if v, ok := raw["base_dir"]; ok {
		json.Unmarshal(v, &baseDir)
	}
	if v, ok := raw["agent_port"]; ok {
		json.Unmarshal(v, &agentPort)
	}
	if v, ok := raw["bash_policy"]; ok {
		json.Unmarshal(v, &bashPolicy)
	}
	if v, ok := raw["covenant"]; ok {
		json.Unmarshal(v, &covenant)
	}
	if agentName != "" {
		m.fields[StepGeneral][0].input.SetValue(agentName)
	}
	if baseDir != "" {
		m.fields[StepGeneral][1].input.SetValue(baseDir)
	}
	if agentPort > 0 {
		m.fields[StepGeneral][2].input.SetValue(strconv.Itoa(agentPort))
	}
	if bashPolicy != "" {
		m.fields[StepGeneral][3].input.SetValue(bashPolicy)
	}
	if covenant != "" {
		m.fields[StepGeneral][4].input.SetValue(covenant)
	}
}

func (m wizardModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case testResultMsg:
		r := msg.result
		m.testResults[msg.step] = &r
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "esc":
			// Skip optional steps
			if m.step == StepVision || m.step == StepWebSearch || m.step == StepIMAP || m.step == StepTelegram {
				m.advanceStep()
				return m, nil
			}

		case "tab", "down":
			if m.step == StepLang {
				m.langIdx = (m.langIdx + 1) % len(i18n.Languages)
				i18n.Lang = i18n.Languages[m.langIdx]
				return m, nil
			}
			if m.step == StepReview {
				break
			}
			fields := m.fields[m.step]
			if m.focus < len(fields)-1 {
				fields[m.focus].input.Blur()
				m.focus++
				fields[m.focus].input.Focus()
				m.fields[m.step] = fields
			}
			return m, nil

		case "shift+tab", "up":
			if m.step == StepLang {
				m.langIdx = (m.langIdx - 1 + len(i18n.Languages)) % len(i18n.Languages)
				i18n.Lang = i18n.Languages[m.langIdx]
				return m, nil
			}
			if m.step == StepReview {
				break
			}
			fields := m.fields[m.step]
			if m.focus > 0 {
				fields[m.focus].input.Blur()
				m.focus--
				fields[m.focus].input.Focus()
				m.fields[m.step] = fields
			}
			return m, nil

		case "left":
			if m.step == StepLang {
				break
			}
			if m.step == StepModel && m.focus == 0 {
				m.providerIdx = (m.providerIdx - 1 + len(providers)) % len(providers)
				m.syncProviderDefaults()
				return m, nil
			}
			if m.step == StepVision && m.focus == 0 {
				m.visionProviderIdx = (m.visionProviderIdx - 1 + len(visionProviders)) % len(visionProviders)
				m.syncVisionDefaults()
				return m, nil
			}
			if m.step == StepWebSearch && m.focus == 0 {
				m.webSearchProviderIdx = (m.webSearchProviderIdx - 1 + len(webSearchProviders)) % len(webSearchProviders)
				m.syncWebSearchDefaults()
				return m, nil
			}

		case "right":
			if m.step == StepLang {
				break
			}
			if m.step == StepModel && m.focus == 0 {
				m.providerIdx = (m.providerIdx + 1) % len(providers)
				m.syncProviderDefaults()
				return m, nil
			}
			if m.step == StepVision && m.focus == 0 {
				m.visionProviderIdx = (m.visionProviderIdx + 1) % len(visionProviders)
				m.syncVisionDefaults()
				return m, nil
			}
			if m.step == StepWebSearch && m.focus == 0 {
				m.webSearchProviderIdx = (m.webSearchProviderIdx + 1) % len(webSearchProviders)
				m.syncWebSearchDefaults()
				return m, nil
			}

		case "ctrl+t":
			// Run connection test
			return m, m.runTest()

		case "enter":
			if m.step == StepReview {
				m.written, m.err = m.writeConfig()
				m.done = true
				return m, tea.Quit
			}
			// On last field of current step, advance
			fields := m.fields[m.step]
			if fields == nil || m.focus >= len(fields)-1 {
				m.advanceStep()
				return m, nil
			}
			// Otherwise move to next field
			fields[m.focus].input.Blur()
			m.focus++
			fields[m.focus].input.Focus()
			m.fields[m.step] = fields
			return m, nil
		}
	}

	// Update the focused text input
	if m.step != StepReview && m.step != StepLang {
		fields := m.fields[m.step]
		if m.focus < len(fields) {
			var cmd tea.Cmd
			fields[m.focus].input, cmd = fields[m.focus].input.Update(msg)
			m.fields[m.step] = fields
			return m, cmd
		}
	}

	return m, nil
}

func (m *wizardModel) syncProviderDefaults() {
	p := providers[m.providerIdx]
	m.fields[StepModel][0].input.SetValue(p)
	m.fields[StepModel][1].input.SetValue(providerModels[p])
	m.fields[StepModel][3].input.SetValue(providerEndpoints[p])
}

func (m *wizardModel) syncVisionDefaults() {
	p := visionProviders[m.visionProviderIdx]
	m.fields[StepVision][0].input.SetValue(p)
	m.fields[StepVision][1].input.SetValue(visionModels[p])
	m.fields[StepVision][3].input.SetValue(visionEndpoints[p])
}

func (m *wizardModel) syncWebSearchDefaults() {
	p := webSearchProviders[m.webSearchProviderIdx]
	m.fields[StepWebSearch][0].input.SetValue(p)
	m.fields[StepWebSearch][1].input.SetValue(webSearchModels[p])
	m.fields[StepWebSearch][3].input.SetValue(webSearchEndpoints[p])
}

func (m *wizardModel) advanceStep() {
	// Blur current fields
	if fields, ok := m.fields[m.step]; ok {
		for i := range fields {
			fields[i].input.Blur()
		}
		m.fields[m.step] = fields
	}

	m.step++
	m.focus = 0

	// Focus first field of new step
	if fields, ok := m.fields[m.step]; ok && len(fields) > 0 {
		fields[0].input.Focus()
		m.fields[m.step] = fields
	}
}

func (m wizardModel) View() string {
	if m.done {
		if m.err != nil {
			return errorStyle.Render(fmt.Sprintf("Error: %v\n", m.err))
		}
		var b strings.Builder
		b.WriteString(successStyle.Render(i18n.S("setup_saved")) + "\n\n")
		b.WriteString(i18n.S("setup_files") + "\n")
		for _, f := range m.written {
			b.WriteString(fmt.Sprintf("  %s %s\n", successStyle.Render("\u2713"), f))
		}
		return b.String()
	}

	var b strings.Builder

	// Banner
	if m.langIdx == 0 { // English
		b.WriteString(headerStyle.Render("LingTai AI") + "\n")
		b.WriteString(dimStyle.Render("Heard the Way beneath the Bodhi;") + "\n")
		b.WriteString(dimStyle.Render("one body, ten thousand avatars.") + "\n\n")
	} else {
		b.WriteString(headerStyle.Render("灵台AI") + "\n")
		b.WriteString(dimStyle.Render("灵台方寸山  斜月三星洞") + "\n")
		b.WriteString(dimStyle.Render("闻道菩提下  一身化万相") + "\n\n")
	}

	// Progress bar
	allSteps := []step{StepLang, StepModel, StepVision, StepWebSearch, StepIMAP, StepTelegram, StepGeneral, StepReview}
	for i, s := range allSteps {
		name := s.String()
		if s == m.step {
			b.WriteString(promptStyle.Render(fmt.Sprintf("[%s]", name)))
		} else if s < m.step {
			b.WriteString(successStyle.Render(fmt.Sprintf(" %s ", name)))
		} else {
			b.WriteString(dimStyle.Render(fmt.Sprintf(" %s ", name)))
		}
		if i < len(allSteps)-1 {
			b.WriteString(dimStyle.Render(" > "))
		}
	}
	b.WriteString("\n\n")

	// Section header
	b.WriteString(headerStyle.Render(m.step.String()) + "\n\n")

	// Language selector (no text fields)
	if m.step == StepLang {
		for idx, code := range i18n.Languages {
			label := i18n.LanguageLabels[code]
			if idx == m.langIdx {
				b.WriteString(fmt.Sprintf("  %s  %s\n", promptStyle.Render(">"), promptStyle.Render(label)))
			} else {
				b.WriteString(fmt.Sprintf("     %s\n", dimStyle.Render(label)))
			}
		}
		b.WriteString("\n" + dimStyle.Render(i18n.S("setup_lang_hint")) + "\n")
		return b.String()
	}

	if m.step == StepReview {
		b.WriteString(m.renderReview())
		b.WriteString("\n" + dimStyle.Render("Enter → save, Ctrl+C → abort") + "\n")
		return b.String()
	}

	// Render fields
	fields := m.fields[m.step]
	for i, f := range fields {
		// Skip base_url field unless provider is custom
		if m.step == StepModel && i == 3 {
			provider := m.fields[StepModel][0].input.Value()
			if provider != "custom" {
				continue
			}
		}

		cursor := "  "
		if i == m.focus {
			cursor = promptStyle.Render("> ")
		}

		label := f.label
		if (m.step == StepModel || m.step == StepVision || m.step == StepWebSearch) && i == 0 {
			label = fmt.Sprintf("%s (left/right to cycle)", label)
		}

		b.WriteString(fmt.Sprintf("%s%s\n", cursor, promptStyle.Render(label)))
		b.WriteString(fmt.Sprintf("  %s\n", f.input.View()))
	}

	// Show test result if any
	if tr, ok := m.testResults[m.step]; ok {
		b.WriteString("\n")
		if tr.OK {
			b.WriteString(fmt.Sprintf("  %s %s\n", successStyle.Render("\u2713"), tr.Message))
		} else {
			b.WriteString(fmt.Sprintf("  %s %s\n", errorStyle.Render("\u2717"), tr.Message))
		}
	}

	// Hints
	b.WriteString("\n")
	hints := []string{"Tab/Down: next field", "Shift+Tab/Up: prev field", "Enter: next step"}
	if m.step == StepVision || m.step == StepWebSearch || m.step == StepIMAP || m.step == StepTelegram {
		hints = append(hints, "Esc: skip")
	}
	if m.step == StepIMAP || m.step == StepTelegram {
		hints = append(hints, "Ctrl+T: test connection")
	}
	b.WriteString(dimStyle.Render(strings.Join(hints, " | ")) + "\n")

	return b.String()
}

func (m wizardModel) renderReview() string {
	var b strings.Builder

	// Language
	langCode := i18n.Languages[m.langIdx]
	langLabel := i18n.LanguageLabels[langCode]
	b.WriteString(promptStyle.Render(i18n.S("setup_lang")+":") + fmt.Sprintf(" %s (%s)\n", langLabel, langCode))

	// Model
	provider := m.fieldVal(StepModel, 0)
	b.WriteString("\n" + promptStyle.Render("Model:") + "\n")
	b.WriteString(fmt.Sprintf("  Provider:    %s\n", provider))
	b.WriteString(fmt.Sprintf("  Model:       %s\n", m.fieldVal(StepModel, 1)))
	if m.fieldVal(StepModel, 2) != "" {
		b.WriteString(fmt.Sprintf("  API key:     %s\n", "••••••••"))
	}
	if endpoint := m.fieldVal(StepModel, 3); endpoint != "" {
		b.WriteString(fmt.Sprintf("  Endpoint:    %s\n", endpoint))
	}

	// Vision
	if visionProvider := m.fieldVal(StepVision, 0); visionProvider != "" && m.fieldVal(StepVision, 1) != "" {
		b.WriteString("\n" + promptStyle.Render("Vision:") + "\n")
		b.WriteString(fmt.Sprintf("  Provider:    %s\n", visionProvider))
		b.WriteString(fmt.Sprintf("  Model:       %s\n", m.fieldVal(StepVision, 1)))
		if m.fieldVal(StepVision, 2) != "" {
			b.WriteString(fmt.Sprintf("  API key:     %s\n", "••••••••"))
		} else {
			b.WriteString(fmt.Sprintf("  API key:     %s\n", dimStyle.Render("reusing main key")))
		}
		if endpoint := m.fieldVal(StepVision, 3); endpoint != "" {
			b.WriteString(fmt.Sprintf("  Endpoint:    %s\n", endpoint))
		}
	} else {
		b.WriteString("\n" + dimStyle.Render("Vision: skipped") + "\n")
	}

	// Web Search
	if wsProvider := m.fieldVal(StepWebSearch, 0); wsProvider != "" && m.fieldVal(StepWebSearch, 1) != "" {
		b.WriteString("\n" + promptStyle.Render("Web Search:") + "\n")
		b.WriteString(fmt.Sprintf("  Provider:    %s\n", wsProvider))
		b.WriteString(fmt.Sprintf("  Model:       %s\n", m.fieldVal(StepWebSearch, 1)))
		if m.fieldVal(StepWebSearch, 2) != "" {
			b.WriteString(fmt.Sprintf("  API key:     %s\n", "••••••••"))
		} else {
			b.WriteString(fmt.Sprintf("  API key:     %s\n", dimStyle.Render("reusing main key")))
		}
		if endpoint := m.fieldVal(StepWebSearch, 3); endpoint != "" {
			b.WriteString(fmt.Sprintf("  Endpoint:    %s\n", endpoint))
		}
	} else {
		b.WriteString("\n" + dimStyle.Render("Web Search: skipped") + "\n")
	}

	// IMAP
	if m.fieldVal(StepIMAP, 0) != "" {
		b.WriteString("\n" + promptStyle.Render("IMAP/SMTP:") + "\n")
		b.WriteString(fmt.Sprintf("  Email:     %s\n", m.fieldVal(StepIMAP, 0)))
		b.WriteString(fmt.Sprintf("  Password:  %s\n", "••••••••"))
		b.WriteString(fmt.Sprintf("  IMAP:      %s:%s\n", m.fieldVal(StepIMAP, 2), m.fieldVal(StepIMAP, 3)))
		b.WriteString(fmt.Sprintf("  SMTP:      %s:%s\n", m.fieldVal(StepIMAP, 4), m.fieldVal(StepIMAP, 5)))
		m.renderTestResult(&b, StepIMAP)
	} else {
		b.WriteString("\n" + dimStyle.Render("IMAP/SMTP: skipped") + "\n")
	}

	// Telegram
	if m.fieldVal(StepTelegram, 0) != "" {
		b.WriteString("\n" + promptStyle.Render("Telegram:") + "\n")
		b.WriteString(fmt.Sprintf("  Token:     %s\n", "••••••••"))
		m.renderTestResult(&b, StepTelegram)
	} else {
		b.WriteString("\n" + dimStyle.Render("Telegram: skipped") + "\n")
	}

	// General
	b.WriteString("\n" + promptStyle.Render("General:") + "\n")
	b.WriteString(fmt.Sprintf("  Agent Name: %s\n", m.fieldVal(StepGeneral, 0)))
	b.WriteString(fmt.Sprintf("  Base Dir:   %s\n", m.fieldVal(StepGeneral, 1)))
	b.WriteString(fmt.Sprintf("  Port:       %s\n", m.fieldVal(StepGeneral, 2)))
	if v := m.fieldVal(StepGeneral, 3); v != "" {
		b.WriteString(fmt.Sprintf("  Bash Policy: %s\n", v))
	}
	if v := m.fieldVal(StepGeneral, 4); v != "" {
		b.WriteString(fmt.Sprintf("  Covenant:    %s\n", v))
	}

	// Save location
	b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("Config → %s/config.json", m.outputDir)) + "\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("Secrets → %s/.env", m.outputDir)) + "\n")

	return b.String()
}

func (m wizardModel) renderTestResult(b *strings.Builder, s step) {
	if tr, ok := m.testResults[s]; ok {
		if tr.OK {
			b.WriteString(fmt.Sprintf("  %s %s\n", successStyle.Render("\u2713"), tr.Message))
		} else {
			b.WriteString(fmt.Sprintf("  %s %s\n", errorStyle.Render("\u2717"), tr.Message))
		}
	}
}

func (m wizardModel) fieldVal(s step, idx int) string {
	fields, ok := m.fields[s]
	if !ok || idx >= len(fields) {
		return ""
	}
	return fields[idx].input.Value()
}

func (m wizardModel) runTest() tea.Cmd {
	switch m.step {
	case StepIMAP:
		return func() tea.Msg {
			email := m.fieldVal(StepIMAP, 0)
			passEnv := m.fieldVal(StepIMAP, 1)
			imapHost := m.fieldVal(StepIMAP, 2)
			imapPortStr := m.fieldVal(StepIMAP, 3)

			pass := os.Getenv(passEnv)
			if pass == "" {
				return testResultMsg{step: StepIMAP, result: TestResult{OK: false, Message: fmt.Sprintf("env var %s is not set", passEnv)}}
			}

			imapPort, _ := strconv.Atoi(imapPortStr)
			if imapPort == 0 {
				imapPort = 993
			}

			r := TestIMAP(imapHost, imapPort, email, pass)
			return testResultMsg{step: StepIMAP, result: r}
		}

	case StepTelegram:
		return func() tea.Msg {
			tokenEnv := m.fieldVal(StepTelegram, 0)
			token := os.Getenv(tokenEnv)
			if token == "" {
				return testResultMsg{step: StepTelegram, result: TestResult{OK: false, Message: fmt.Sprintf("env var %s is not set", tokenEnv)}}
			}
			r := TestTelegram(token)
			return testResultMsg{step: StepTelegram, result: r}
		}

	default:
		return nil
	}
}

func (m wizardModel) writeConfig() ([]string, error) {
	if err := os.MkdirAll(m.outputDir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create output directory: %w", err)
	}

	var written []string

	// Derive env var name from provider
	provider := m.fieldVal(StepModel, 0)
	apiKeyEnv := strings.ToUpper(provider) + "_API_KEY"

	// 1. model.json
	modelCfg := map[string]interface{}{
		"provider":    provider,
		"model":       m.fieldVal(StepModel, 1),
		"api_key_env": apiKeyEnv,
	}
	if endpoint := m.fieldVal(StepModel, 3); endpoint != "" {
		modelCfg["base_url"] = endpoint
	}

	// Vision config (if not skipped)
	if visionProvider := m.fieldVal(StepVision, 0); visionProvider != "" && m.fieldVal(StepVision, 1) != "" {
		visionKeyEnv := apiKeyEnv // reuse main key by default
		if visionKey := m.fieldVal(StepVision, 2); visionKey != "" {
			visionKeyEnv = strings.ToUpper(visionProvider) + "_API_KEY"
			if visionKeyEnv == apiKeyEnv {
				visionKeyEnv = strings.ToUpper(visionProvider) + "_VISION_API_KEY"
			}
		}
		visionCfg := map[string]interface{}{
			"provider":    visionProvider,
			"model":       m.fieldVal(StepVision, 1),
			"api_key_env": visionKeyEnv,
		}
		if endpoint := m.fieldVal(StepVision, 3); endpoint != "" {
			visionCfg["base_url"] = endpoint
		}
		modelCfg["vision"] = visionCfg
	}

	// Web search config (if not skipped)
	if wsProvider := m.fieldVal(StepWebSearch, 0); wsProvider != "" && m.fieldVal(StepWebSearch, 1) != "" {
		wsKeyEnv := apiKeyEnv // reuse main key by default
		if wsKey := m.fieldVal(StepWebSearch, 2); wsKey != "" {
			wsKeyEnv = strings.ToUpper(wsProvider) + "_API_KEY"
			if wsKeyEnv == apiKeyEnv {
				wsKeyEnv = strings.ToUpper(wsProvider) + "_WEB_SEARCH_API_KEY"
			}
		}
		wsCfg := map[string]interface{}{
			"provider":    wsProvider,
			"model":       m.fieldVal(StepWebSearch, 1),
			"api_key_env": wsKeyEnv,
		}
		if endpoint := m.fieldVal(StepWebSearch, 3); endpoint != "" {
			wsCfg["base_url"] = endpoint
		}
		modelCfg["web_search"] = wsCfg
	}

	modelPath := filepath.Join(m.outputDir, "model.json")
	if err := writeJSON(modelPath, modelCfg); err != nil {
		return written, fmt.Errorf("writing model.json: %w", err)
	}
	written = append(written, modelPath)

	// 2. config.json
	port, _ := strconv.Atoi(m.fieldVal(StepGeneral, 2))
	if port == 0 {
		port = 8501
	}

	cfg := map[string]interface{}{
		"model":      "model.json",
		"language":   i18n.Languages[m.langIdx],
		"agent_name": m.fieldVal(StepGeneral, 0),
		"base_dir":   m.fieldVal(StepGeneral, 1),
		"agent_port": port,
	}

	if v := m.fieldVal(StepGeneral, 3); v != "" {
		cfg["bash_policy"] = v
	}
	if v := m.fieldVal(StepGeneral, 4); v != "" {
		cfg["covenant"] = v
	}

	// IMAP config
	if email := m.fieldVal(StepIMAP, 0); email != "" {
		imapPort, _ := strconv.Atoi(m.fieldVal(StepIMAP, 3))
		smtpPort, _ := strconv.Atoi(m.fieldVal(StepIMAP, 5))
		cfg["imap"] = map[string]interface{}{
			"email_address": email,
			"password_env":  "IMAP_PASSWORD",
			"imap_host":     m.fieldVal(StepIMAP, 2),
			"imap_port":     imapPort,
			"smtp_host":     m.fieldVal(StepIMAP, 4),
			"smtp_port":     smtpPort,
		}
	}

	// Telegram config
	if token := m.fieldVal(StepTelegram, 0); token != "" {
		cfg["telegram"] = map[string]interface{}{
			"bot_token_env": "TELEGRAM_BOT_TOKEN",
		}
	}

	configPath := filepath.Join(m.outputDir, "config.json")
	if err := writeJSON(configPath, cfg); err != nil {
		return written, fmt.Errorf("writing config.json: %w", err)
	}
	written = append(written, configPath)

	// 3. .env (save actual secrets)
	var envLines []string
	if apiKey := m.fieldVal(StepModel, 2); apiKey != "" {
		envLines = append(envLines, fmt.Sprintf("%s=%s", apiKeyEnv, apiKey))
	}
	if visionKey := m.fieldVal(StepVision, 2); visionKey != "" && visionKey != m.fieldVal(StepModel, 2) {
		visionProvider := m.fieldVal(StepVision, 0)
		visionKeyEnv := strings.ToUpper(visionProvider) + "_API_KEY"
		if visionKeyEnv == apiKeyEnv {
			visionKeyEnv = strings.ToUpper(visionProvider) + "_VISION_API_KEY"
		}
		envLines = append(envLines, fmt.Sprintf("%s=%s", visionKeyEnv, visionKey))
	}
	if wsKey := m.fieldVal(StepWebSearch, 2); wsKey != "" && wsKey != m.fieldVal(StepModel, 2) {
		wsProvider := m.fieldVal(StepWebSearch, 0)
		wsKeyEnv := strings.ToUpper(wsProvider) + "_API_KEY"
		if wsKeyEnv == apiKeyEnv {
			wsKeyEnv = strings.ToUpper(wsProvider) + "_WEB_SEARCH_API_KEY"
		}
		envLines = append(envLines, fmt.Sprintf("%s=%s", wsKeyEnv, wsKey))
	}
	if password := m.fieldVal(StepIMAP, 1); password != "" {
		envLines = append(envLines, fmt.Sprintf("IMAP_PASSWORD=%s", password))
	}
	if token := m.fieldVal(StepTelegram, 0); token != "" {
		envLines = append(envLines, fmt.Sprintf("TELEGRAM_BOT_TOKEN=%s", token))
	}
	if len(envLines) > 0 {
		envPath := filepath.Join(m.outputDir, ".env")
		content := "# LingTai secrets — do not commit this file\n\n" + strings.Join(envLines, "\n") + "\n"
		if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
			return written, fmt.Errorf("writing .env: %w", err)
		}
		written = append(written, envPath)
	}

	return written, nil
}

func writeJSON(path string, data interface{}) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}

// Run starts the interactive setup wizard, writing config to outputDir.
func Run(outputDir string) error {
	m := newWizardModel(outputDir)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("wizard error: %w", err)
	}
	if wm, ok := finalModel.(wizardModel); ok && wm.err != nil {
		return wm.err
	}
	return nil
}
