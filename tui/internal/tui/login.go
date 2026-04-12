package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type loginStatus int

const (
	loginChecking loginStatus = iota
	loginValid
	loginInvalid
	loginError
)

type loginEntry struct {
	Provider string
	Display  string      // masked key or "OAuth — email"
	Status   loginStatus
	Detail   string // error detail
	IsOAuth  bool
	BaseURL  string
	Key      string // raw key or access token
}

type loginHealthMsg struct {
	Provider string
	Status   loginStatus
	Detail   string
}

// LoginModel is the /login dedicated view showing saved credentials with
// live health checks.
type LoginModel struct {
	entries       []loginEntry
	cursor        int
	activePreset  string
	activeModel   string
	orchDir       string
	globalDir     string
	width, height int
	message       string
	reenteringKey bool
	keyInput      textarea.Model
	codexLogging  bool
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// providerBaseURL returns the default API base URL for known providers.
func providerBaseURL(provider string) string {
	switch provider {
	case "minimax":
		return "https://api.minimaxi.com"
	case "zhipu":
		return "https://open.bigmodel.cn/api/coding/paas/v4"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewLoginModel builds a LoginModel populated from the orchestrator config
// and globally saved credentials.
func NewLoginModel(orchDir, globalDir string) LoginModel {
	m := LoginModel{
		orchDir:   orchDir,
		globalDir: globalDir,
	}

	// 1. Read orchestrator's active provider/model.
	provider, model, _, _, _ := readLLMConfig(orchDir)
	m.activePreset = provider
	m.activeModel = model

	// 2. Check for codex-auth.json (OAuth entry).
	codexAuthPath := filepath.Join(globalDir, "codex-auth.json")
	if data, err := os.ReadFile(codexAuthPath); err == nil {
		var tokens CodexTokens
		if json.Unmarshal(data, &tokens) == nil && tokens.RefreshToken != "" {
			display := "OAuth"
			if tokens.Email != "" {
				display = "OAuth — " + tokens.Email
			}
			m.entries = append(m.entries, loginEntry{
				Provider: "codex",
				Display:  display,
				Status:   loginChecking,
				IsOAuth:  true,
				BaseURL:  "https://chatgpt.com/backend-api",
				Key:      tokens.AccessToken,
			})
		}
	}

	// 3. Read config.Keys for API-key-based providers.
	cfg, _ := config.LoadConfig(globalDir)
	for prov, key := range cfg.Keys {
		if key == "" || prov == "codex" {
			continue
		}
		base := providerBaseURL(prov)
		m.entries = append(m.entries, loginEntry{
			Provider: prov,
			Display:  maskKey(key),
			Status:   loginChecking,
			IsOAuth:  false,
			BaseURL:  base,
			Key:      key,
		})
	}

	// 4. Prepare textarea for key re-entry.
	ta := textarea.New()
	ta.SetHeight(1)
	ta.CharLimit = 512
	ta.Placeholder = "paste API key..."
	m.keyInput = ta

	return m
}

// ---------------------------------------------------------------------------
// Health check
// ---------------------------------------------------------------------------

func checkHealth(e loginEntry) loginHealthMsg {
	if e.BaseURL == "" || e.Key == "" {
		return loginHealthMsg{
			Provider: e.Provider,
			Status:   loginInvalid,
			Detail:   "no endpoint",
		}
	}

	var url string
	if e.IsOAuth {
		url = strings.TrimRight(e.BaseURL, "/") + "/codex/models?client_version=1.0.0"
	} else {
		url = strings.TrimRight(e.BaseURL, "/") + "/models"
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return loginHealthMsg{Provider: e.Provider, Status: loginError, Detail: "connection error"}
	}
	req.Header.Set("Authorization", "Bearer "+e.Key)

	resp, err := client.Do(req)
	if err != nil {
		return loginHealthMsg{Provider: e.Provider, Status: loginError, Detail: "connection error"}
	}
	defer resp.Body.Close()
	io.ReadAll(io.LimitReader(resp.Body, 1024)) // drain body

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return loginHealthMsg{Provider: e.Provider, Status: loginValid}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return loginHealthMsg{Provider: e.Provider, Status: loginInvalid, Detail: "invalid credentials"}
	default:
		return loginHealthMsg{Provider: e.Provider, Status: loginError, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
}

// ---------------------------------------------------------------------------
// Bubble Tea interface
// ---------------------------------------------------------------------------

func (m LoginModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, e := range m.entries {
		entry := e // capture
		cmds = append(cmds, func() tea.Msg {
			return checkHealth(entry)
		})
	}
	return tea.Batch(cmds...)
}

func (m LoginModel) Update(msg tea.Msg) (LoginModel, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case loginHealthMsg:
		for idx := range m.entries {
			if m.entries[idx].Provider == msg.Provider {
				m.entries[idx].Status = msg.Status
				m.entries[idx].Detail = msg.Detail
				break
			}
		}

	case CodexOAuthDoneMsg:
		m.codexLogging = false
		if msg.Err != nil {
			m.message = "OAuth error: " + msg.Err.Error()
			return m, nil
		}
		if msg.Tokens == nil {
			m.message = "OAuth returned no tokens"
			return m, nil
		}

		// Save tokens to codex-auth.json.
		data, err := json.MarshalIndent(msg.Tokens, "", "  ")
		if err != nil {
			m.message = "failed to marshal tokens: " + err.Error()
			return m, nil
		}
		authPath := filepath.Join(m.globalDir, "codex-auth.json")
		if err := os.WriteFile(authPath, data, 0o600); err != nil {
			m.message = "failed to save codex-auth.json: " + err.Error()
			return m, nil
		}

		// Update or add codex entry.
		display := "OAuth"
		if msg.Tokens.Email != "" {
			display = "OAuth — " + msg.Tokens.Email
		}
		found := false
		for idx := range m.entries {
			if m.entries[idx].Provider == "codex" {
				m.entries[idx].Display = display
				m.entries[idx].Key = msg.Tokens.AccessToken
				m.entries[idx].Status = loginChecking
				m.entries[idx].Detail = ""
				found = true
				break
			}
		}
		if !found {
			m.entries = append(m.entries, loginEntry{
				Provider: "codex",
				Display:  display,
				Status:   loginChecking,
				IsOAuth:  true,
				BaseURL:  "https://chatgpt.com/backend-api",
				Key:      msg.Tokens.AccessToken,
			})
		}

		// Re-run health check for codex.
		entry := m.entryByProvider("codex")
		if entry != nil {
			e := *entry
			return m, func() tea.Msg { return checkHealth(e) }
		}

	case tea.KeyPressMsg:
		if m.reenteringKey {
			return m.updateKeyReentry(msg)
		}
		return m.updateNormal(msg)
	}

	return m, nil
}

func (m *LoginModel) entryByProvider(provider string) *loginEntry {
	for idx := range m.entries {
		if m.entries[idx].Provider == provider {
			return &m.entries[idx]
		}
	}
	return nil
}

func (m LoginModel) updateNormal(msg tea.KeyPressMsg) (LoginModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
	case "enter":
		if m.cursor >= 0 && m.cursor < len(m.entries) {
			entry := m.entries[m.cursor]
			if entry.IsOAuth {
				m.codexLogging = true
				ch := startOAuthFlow()
				return m, func() tea.Msg {
					return <-ch
				}
			}
			// API key entry — show re-entry textarea.
			m.reenteringKey = true
			m.keyInput.Reset()
			m.keyInput.Focus()
			return m, nil
		}
	}
	return m, nil
}

func (m LoginModel) updateKeyReentry(msg tea.KeyPressMsg) (LoginModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.reenteringKey = false
		m.keyInput.Blur()
		return m, nil
	case "enter":
		newKey := strings.TrimSpace(m.keyInput.Value())
		if newKey == "" {
			m.reenteringKey = false
			m.keyInput.Blur()
			return m, nil
		}
		m.reenteringKey = false
		m.keyInput.Blur()

		// Save key to config.
		cfg, err := config.LoadConfig(m.globalDir)
		if err != nil {
			m.message = "failed to load config: " + err.Error()
			return m, nil
		}
		if cfg.Keys == nil {
			cfg.Keys = make(map[string]string)
		}
		provider := m.entries[m.cursor].Provider
		cfg.Keys[provider] = newKey
		if err := config.SaveConfig(m.globalDir, cfg); err != nil {
			m.message = "failed to save config: " + err.Error()
			return m, nil
		}

		// Update entry.
		m.entries[m.cursor].Key = newKey
		m.entries[m.cursor].Display = maskKey(newKey)
		m.entries[m.cursor].Status = loginChecking
		m.entries[m.cursor].Detail = ""

		// Fire health check.
		entry := m.entries[m.cursor]
		return m, func() tea.Msg { return checkHealth(entry) }
	default:
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd
	}
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m LoginModel) View() string {
	var b strings.Builder

	// Title bar: app.title • login.title   [esc] back
	title := StyleTitle.Render(i18n.T("app.title")) + " " +
		StyleAccent.Render(RuneBullet) + " " +
		StyleTitle.Render(i18n.T("login.title"))
	escHint := StyleAccent.Render("[esc] ") + StyleSubtle.Render(i18n.T("manage.back"))
	padding := m.width - lipgloss.Width(title) - lipgloss.Width(escHint) - 1
	if padding > 0 {
		b.WriteString(title + strings.Repeat(" ", padding) + escHint + "\n")
	} else {
		b.WriteString(title + "  " + escHint + "\n")
	}
	b.WriteString(strings.Repeat("─", m.width) + "\n\n")

	// Active provider line.
	if m.activePreset != "" {
		active := fmt.Sprintf("  Active: %s", m.activePreset)
		if m.activeModel != "" {
			active += fmt.Sprintf(" (%s)", m.activeModel)
		}
		b.WriteString(active + "\n\n")
	}

	// Entries.
	if len(m.entries) == 0 {
		b.WriteString("  " + StyleFaint.Render("No saved credentials.") + "\n")
	} else {
		b.WriteString("  Saved credentials:\n\n")
		for idx, entry := range m.entries {
			cursor := "  "
			if idx == m.cursor {
				cursor = StyleAccent.Render("> ")
			}

			// Status icon.
			var icon string
			switch entry.Status {
			case loginChecking:
				icon = StyleSubtle.Render("⋯")
			case loginValid:
				icon = lipgloss.NewStyle().Foreground(ColorActive).Render("✓")
			case loginInvalid:
				icon = lipgloss.NewStyle().Foreground(ColorSuspended).Render("✗")
			case loginError:
				icon = lipgloss.NewStyle().Foreground(ColorStuck).Render("✗")
			}

			// Provider name padded to 10 chars.
			name := entry.Provider
			if len(name) < 10 {
				name += strings.Repeat(" ", 10-len(name))
			}

			line := fmt.Sprintf("%s %s %s %s", cursor, icon, name, entry.Display)
			if entry.Detail != "" {
				var detailStyle lipgloss.Style
				switch entry.Status {
				case loginInvalid:
					detailStyle = lipgloss.NewStyle().Foreground(ColorSuspended)
				case loginError:
					detailStyle = lipgloss.NewStyle().Foreground(ColorStuck)
				default:
					detailStyle = lipgloss.NewStyle().Foreground(ColorStuck)
				}
				line += "  " + detailStyle.Render("("+entry.Detail+")")
			}
			b.WriteString(line + "\n")
		}
	}

	// Key re-entry area.
	if m.reenteringKey && m.cursor >= 0 && m.cursor < len(m.entries) {
		b.WriteString("\n  Enter new API key for " + m.entries[m.cursor].Provider + ":\n")
		b.WriteString("  " + m.keyInput.View() + "\n")
		b.WriteString("  " + StyleFaint.Render("[Enter] save  [Esc] cancel") + "\n")
	}

	// Codex logging state.
	if m.codexLogging {
		b.WriteString("\n  " + StyleAccent.Render("Waiting for browser authentication...") + "\n")
	}

	// Transient message.
	if m.message != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(ColorStuck).Render(m.message) + "\n")
	}

	// Bottom hints.
	b.WriteString("\n" + strings.Repeat("─", m.width) + "\n")
	b.WriteString(StyleFaint.Render("  [Enter] re-authenticate  [Esc] back") + "\n")

	return b.String()
}
