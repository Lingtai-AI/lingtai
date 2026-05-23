package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// AddonSavedMsg is sent when addon view is dismissed.
type AddonSavedMsg struct{}

// AddonModel is the /addon view — displays configured addons and allows
// interactive setup for unconfigured addons that support onboarding flows.
// Reads from {lingtaiDir}/.addons/{addon}/config.json, a project-level
// shared location (one config file per addon, multi-account via accounts array).
type AddonModel struct {
	lingtaiDir string // <project>/.lingtai/ directory
	width      int
	height     int
	cursor     int // currently selected addon index
	// addonConfigs maps addon name → JSON file content for successfully read configs.
	addonConfigs map[string]string
	// addonErrors maps addon name → error message (e.g. "not found", "parse error")
	addonErrors map[string]string
}

type addonState struct {
	content    string
	configured bool
	errMsg     string
}

// NewAddonModel constructs the /addon view. lingtaiDir is the project's .lingtai/
// directory (parent of all agent dirs). Addon configs live at
// lingtaiDir/.addons/<addon>/config.json.
func NewAddonModel(lingtaiDir string) AddonModel {
	configs, errs := readAddonConfigs(lingtaiDir)
	return AddonModel{
		lingtaiDir:   lingtaiDir,
		addonConfigs: configs,
		addonErrors:  errs,
	}
}

func (m AddonModel) Init() tea.Cmd { return nil }

func (m AddonModel) Update(msg tea.Msg) (AddonModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case FeishuOnboardDoneMsg:
		// Refresh configs after onboarding completes
		configs, errs := readAddonConfigs(m.lingtaiDir)
		m.addonConfigs = configs
		m.addonErrors = errs
		return m, nil

	case WechatOnboardDoneMsg:
		configs, errs := readAddonConfigs(m.lingtaiDir)
		m.addonConfigs = configs
		m.addonErrors = errs
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down":
			if m.cursor < len(AllAddons)-1 {
				m.cursor++
			}
			return m, nil
		case "enter":
			name := AllAddons[m.cursor]
			if name == "feishu" {
				_, configured := m.addonConfigs[name]
				if !configured && m.addonErrors[name] == "" {
					return m, func() tea.Msg { return ViewChangeMsg{View: "feishu_onboard"} }
				}
			}
			if name == "wechat" {
				_, configured := m.addonConfigs[name]
				if !configured && m.addonErrors[name] == "" {
					return m, func() tea.Msg { return ViewChangeMsg{View: "wechat_onboard"} }
				}
			}
			return m, nil
		case "esc":
			return m, func() tea.Msg { return AddonSavedMsg{} }
		}
	}
	return m, nil
}

func (m AddonModel) View() string {
	var b strings.Builder

	// Title bar
	titleText := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent).Render(i18n.T("welcome.title"))
	titleBar := titleText + " " + StyleAccent.Render(RuneBullet) + " " + StyleTitle.Render(i18n.T("addon.title"))
	escHint := StyleAccent.Render("[esc] ") + StyleSubtle.Render(i18n.T("addon.back"))
	padding := m.width - lipgloss.Width(titleBar) - lipgloss.Width(escHint) - 1
	if padding > 0 {
		b.WriteString(titleBar + strings.Repeat(" ", padding) + escHint + "\n")
	} else {
		b.WriteString(titleBar + "  " + escHint + "\n")
	}
	b.WriteString(strings.Repeat("─", m.width) + "\n\n")

	// Description
	b.WriteString(StyleSubtle.Render("  "+i18n.T("addon.readonly_desc")) + "\n\n")

	// Addon list
	for i, name := range AllAddons {
		label := strings.ToUpper(name[:1]) + name[1:]
		configPath := addonConfigRelPath(name)
		state := addonStateFor(name, m.addonConfigs, m.addonErrors)

		// Cursor highlight
		cursor := "  "
		nameStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent)
		if i == m.cursor {
			cursor = "> "
			nameStyle = nameStyle.Foreground(ColorAccent)
		}
		b.WriteString(cursor + nameStyle.Render(label) + StyleFaint.Render("  "+configPath) + "\n")

		if state.errMsg != "" {
			b.WriteString("    " + StyleFaint.Render(state.errMsg) + "\n\n")
			continue
		}

		if !state.configured || state.content == "" {
			hint := i18n.T("addon.not_configured")
			if (name == "feishu" || name == "wechat") && i == m.cursor {
				hint += "  — " + i18n.T("addon.configure_hint")
			}
			b.WriteString("    " + StyleFaint.Render(hint) + "\n")
			continue
		}

		// Pretty-print the JSON
		pretty := prettyJSON(state.content)
		for _, line := range strings.Split(strings.TrimRight(pretty, "\n"), "\n") {
			b.WriteString("    " + line + "\n")
		}
		b.WriteString("\n")
	}

	// Footer
	b.WriteString(strings.Repeat("─", m.width) + "\n")
	b.WriteString(StyleFaint.Render("  "+i18n.T("addon.footer_hint")) + "\n")

	return b.String()
}

// addonConfigRelPath returns the canonical path (relative to project root) for
// an addon's config file. This is the only place the convention is defined —
// all other code uses this helper.
func addonConfigRelPath(addon string) string {
	return filepath.Join(".lingtai", ".addons", addon, "config.json")
}

// AddonConfigPath returns the absolute path to an addon's config file, given
// the project's .lingtai/ directory. Exported for use by other packages.
func AddonConfigPath(lingtaiDir, addon string) string {
	return filepath.Join(lingtaiDir, ".addons", addon, "config.json")
}

// readAddonConfigs reads {lingtaiDir}/.addons/{addon}/config.json for each
// known addon. Returns (configs, errors): configs holds addon→JSON-content
// for successful reads, errors holds addon→error-message for files that
// exist but couldn't be parsed. Addons with no file at all appear in neither map.
func readAddonConfigs(lingtaiDir string) (map[string]string, map[string]string) {
	configs := make(map[string]string)
	errs := make(map[string]string)
	if lingtaiDir == "" {
		return configs, errs
	}

	for _, addon := range AllAddons {
		if addon == "wechat" {
			state := readWechatAddonState(lingtaiDir)
			if state.configured {
				configs[addon] = state.content
			}
			if state.errMsg != "" {
				errs[addon] = state.errMsg
			}
			continue
		}

		configPath := AddonConfigPath(lingtaiDir, addon)
		data, err := os.ReadFile(configPath)
		if err != nil {
			// File missing or unreadable — not an error, just "not configured"
			continue
		}
		// Validate it parses as JSON; if not, report as an error
		var probe any
		if jerr := json.Unmarshal(data, &probe); jerr != nil {
			errs[addon] = i18n.TF("addon.parse_error", jerr.Error())
			continue
		}
		configs[addon] = string(data)
	}
	return configs, errs
}

func addonStateFor(name string, configs, errs map[string]string) addonState {
	state := addonState{}
	if content, ok := configs[name]; ok {
		state.content = content
		state.configured = content != ""
	}
	if errMsg, ok := errs[name]; ok {
		state.errMsg = errMsg
		state.configured = false
	}
	return state
}

func readWechatAddonState(lingtaiDir string) addonState {
	state := addonState{}
	configPath := AddonConfigPath(lingtaiDir, "wechat")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return state
	}

	var probe any
	if jerr := json.Unmarshal(configData, &probe); jerr != nil {
		state.errMsg = i18n.TF("addon.parse_error", jerr.Error())
		return state
	}

	credentialsPath := filepath.Join(filepath.Dir(configPath), "credentials.json")
	credentialsData, err := os.ReadFile(credentialsPath)
	if err != nil {
		if os.IsNotExist(err) {
			state.content = string(configData)
			return state
		}
		state.errMsg = i18n.TF("addon.parse_error", err.Error())
		return state
	}
	if len(strings.TrimSpace(string(credentialsData))) == 0 {
		state.content = string(configData)
		return state
	}

	var creds struct {
		BotToken string `json:"bot_token"`
		UserID   string `json:"user_id"`
	}
	if jerr := json.Unmarshal(credentialsData, &creds); jerr != nil {
		state.errMsg = i18n.TF("addon.credential_error", jerr.Error())
		return state
	}
	if strings.TrimSpace(creds.BotToken) == "" || strings.TrimSpace(creds.UserID) == "" {
		state.content = string(configData)
		return state
	}

	state.content = string(configData)
	state.configured = true
	return state
}

// prettyJSON returns a formatted (indented) JSON string, or the original on error.
func prettyJSON(data string) string {
	var v any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return data
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return data
	}
	return string(out)
}
