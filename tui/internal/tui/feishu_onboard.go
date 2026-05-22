package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/anthropics/lingtai-tui/i18n"
)

// ---------------------------------------------------------------------------
// HTTP layer — feishu / lark OAuth device-code registration
// ---------------------------------------------------------------------------

const (
	feishuAccountsHost = "accounts.feishu.cn"
	larkAccountsHost   = "accounts.larksuite.com"
	feishuOpenHost     = "open.feishu.cn"
	larkOpenHost       = "open.larksuite.com"
	registrationPath   = "/oauth/v1/app/registration"
	requestTimeout     = 10 * time.Second
)

func accountsHost(domain string) string {
	if domain == "lark" {
		return larkAccountsHost
	}
	return feishuAccountsHost
}

func openHost(domain string) string {
	if domain == "lark" {
		return larkOpenHost
	}
	return feishuOpenHost
}

type beginOnboardResp struct {
	DeviceCode string `json:"device_code"`
	QRURL      string `json:"verification_uri_complete"`
	UserCode   string `json:"user_code"`
	Interval   int    `json:"interval"`
	ExpireIn   int    `json:"expire_in"`
}

type pollOnboardResp struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Error        string `json:"error"`
	UserInfo     struct {
		OpenID      string `json:"open_id"`
		TenantBrand string `json:"tenant_brand"`
	} `json:"user_info"`
}

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

type feishuOnboardStep int

const (
	feishuStepInit    feishuOnboardStep = iota // connecting …
	feishuStepQR                               // showing QR + URL
	feishuStepPolling                          // waiting for scan
	feishuStepDone                             // credentials saved
	feishuStepError                            // error state
)

// feishuRemainingTickMsg triggers a View refresh so the countdown updates.
type feishuRemainingTickMsg struct{}

// FeishuOnboardDoneMsg is emitted when the onboard flow completes (success or error).
// Handled by App.Update and forwarded to AddonModel for state refresh.
type FeishuOnboardDoneMsg struct {
	AppID     string
	AppSecret string
	Domain    string
	BotName   string
	Err       error
}

type feishuQRReadyMsg struct {
	URL        string
	DeviceCode string
	Interval   int
	ExpireIn   int
	Domain     string
}

type feishuPollDoneMsg struct {
	AppID     string
	AppSecret string
	Domain    string
	BotName   string
	Err       error
}

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

// FeishuOnboardModel drives the feishu / lark scan-to-create QR flow.
type FeishuOnboardModel struct {
	step       feishuOnboardStep
	qrData     string // ASCII QR code text
	url        string // launcher URL (terminals with link detection make this Ctrl+clickable)
	deviceCode string
	interval   int
	expireIn   int
	domain     string // "feishu" or "lark"
	message    string // status / error text
	appID         string
	appSecret      string
	botName       string
	pollDeadline  time.Time

	lingtaiDir string // <project>/.lingtai/
	width      int
	height     int
}

// NewFeishuOnboardModel creates a new feishu onboard model and starts the
// init → begin device-code flow in the background.
func NewFeishuOnboardModel(lingtaiDir string) FeishuOnboardModel {
	return FeishuOnboardModel{
		step:       feishuStepInit,
		lingtaiDir: lingtaiDir,
		domain:     "feishu",
	}
}

func (m FeishuOnboardModel) Init() tea.Cmd {
	return m.runInitAndBegin()
}

// ---------------------------------------------------------------------------
// Background goroutines → tea.Msg
// ---------------------------------------------------------------------------

func (m FeishuOnboardModel) runInitAndBegin() tea.Cmd {
	return func() tea.Msg {
		// Init
		if err := callInit(m.domain); err != nil {
			return feishuPollDoneMsg{Err: err}
		}

		// Begin
		begin, err := callBegin(m.domain)
		if err != nil {
			return feishuPollDoneMsg{Err: err}
		}

		// Append lingtai origin params
		qrURL := begin.QRURL
		if strings.Contains(qrURL, "?") {
			qrURL += "&from=lingtai&tp=lingtai"
		} else {
			qrURL += "?from=lingtai&tp=lingtai"
		}
		begin.QRURL = qrURL

		return feishuQRReadyMsg{
			URL:        begin.QRURL,
			DeviceCode: begin.DeviceCode,
			Interval:   begin.Interval,
			ExpireIn:   begin.ExpireIn,
			Domain:     m.domain,
		}
	}
}

func (m FeishuOnboardModel) runPoll() tea.Cmd {
	deviceCode := m.deviceCode
	interval := m.interval
	expireIn := m.expireIn
	domain := m.domain
	return func() tea.Msg {
		if interval <= 0 {
			interval = 5
		}
		if expireIn <= 0 {
			expireIn = 600
		}
		deadline := time.Now().Add(time.Duration(expireIn) * time.Second)
		currentDomain := domain
		switched := false

		for time.Now().Before(deadline) {
			poll, err := callPoll(deviceCode, currentDomain)
			if err != nil {
				time.Sleep(time.Duration(interval) * time.Second)
				continue
			}

			// Domain auto-detection
			if poll.UserInfo.TenantBrand == "lark" && !switched {
				currentDomain = "lark"
				switched = true
			}

			// Success
			if poll.ClientID != "" && poll.ClientSecret != "" {
				return feishuPollDoneMsg{
					AppID:     poll.ClientID,
					AppSecret: poll.ClientSecret,
					Domain:    currentDomain,
				}
			}

			// Terminal errors
			switch poll.Error {
			case "access_denied", "expired_token":
				return feishuPollDoneMsg{Err: fmt.Errorf("feishu: %s", poll.Error)}
			}

			time.Sleep(time.Duration(interval) * time.Second)
		}
		return feishuPollDoneMsg{Err: fmt.Errorf("feishu: poll timed out after %ds", expireIn)}
	}
}

func (m FeishuOnboardModel) runProbeBot() tea.Cmd {
	appID := m.appID
	appSecret := m.appSecret
	domain := m.domain
	return func() tea.Msg {
		name, err := callProbeBot(appID, appSecret, domain)
		return feishuPollDoneMsg{
			AppID:     appID,
			AppSecret: appSecret,
			Domain:    domain,
			BotName:   name,
			Err:       err,
		}
	}
}

// ---------------------------------------------------------------------------
// Feishu API HTTP calls (stdlib net/http)
// ---------------------------------------------------------------------------

func callInit(domain string) error {
	body := url.Values{"action": {"init"}}.Encode()
	return postRegistration(domain, body)
}

func callBegin(domain string) (*beginOnboardResp, error) {
	body := url.Values{
		"action":             {"begin"},
		"archetype":          {"PersonalAgent"},
		"auth_method":        {"client_secret"},
		"request_user_info":  {"open_id"},
	}.Encode()

	resp, err := postRegistrationJSON(domain, body)
	if err != nil {
		return nil, err
	}

	var begin beginOnboardResp
	if err := json.Unmarshal(resp, &begin); err != nil {
		return nil, fmt.Errorf("parse begin response: %w", err)
	}
	if begin.DeviceCode == "" {
		return nil, fmt.Errorf("feishu: no device_code in begin response")
	}
	return &begin, nil
}

func callPoll(deviceCode, domain string) (*pollOnboardResp, error) {
	body := url.Values{
		"action":      {"poll"},
		"device_code": {deviceCode},
		"tp":          {"ob_app"},
	}.Encode()

	respJSON, err := postRegistrationJSON(domain, body)
	if err != nil {
		return nil, err
	}

	var poll pollOnboardResp
	if err := json.Unmarshal(respJSON, &poll); err != nil {
		return nil, fmt.Errorf("parse poll response: %w", err)
	}
	return &poll, nil
}

func callProbeBot(appID, appSecret, domain string) (string, error) {
	// Get tenant access token
	tokenBody, _ := json.Marshal(map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	})

	tokenResp, err := postJSON(
		fmt.Sprintf("https://%s/open-apis/auth/v3/tenant_access_token/internal", openHost(domain)),
		tokenBody,
	)
	if err != nil {
		return "", fmt.Errorf("get tenant token: %w", err)
	}

	var tokenData struct {
		Code            int    `json:"code"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.Unmarshal(tokenResp, &tokenData); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if tokenData.TenantAccessToken == "" {
		return "", fmt.Errorf("no tenant_access_token")
	}

	// Get bot info
	botResp, err := getJSON(
		fmt.Sprintf("https://%s/open-apis/bot/v3/info", openHost(domain)),
		tokenData.TenantAccessToken,
	)
	if err != nil {
		return "", fmt.Errorf("get bot info: %w", err)
	}

	var botData struct {
		Code int `json:"code"`
		Bot  struct {
			BotName string `json:"bot_name"`
			OpenID  string `json:"open_id"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(botResp, &botData); err != nil {
		return "", fmt.Errorf("parse bot response: %w", err)
	}
	if botData.Code != 0 {
		return "", fmt.Errorf("bot info returned code %d", botData.Code)
	}
	return botData.Bot.BotName, nil
}

// postRegistration POSTs form-encoded data to the registration endpoint.
func postRegistration(domain, formBody string) error {
	_, err := postRegistrationJSON(domain, formBody)
	return err
}

// postRegistrationJSON POSTs form-encoded data to the registration endpoint.
// The feishu registration endpoint returns JSON even on 4xx — always parse the body.
func postRegistrationJSON(domain, formBody string) ([]byte, error) {
	u := fmt.Sprintf("https://%s%s", accountsHost(domain), registrationPath)
	httpReq, err := http.NewRequest("POST", u, strings.NewReader(formBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// postJSON sends a JSON-encoded POST and returns the raw response body.
func postJSON(url string, body []byte) ([]byte, error) {
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// getJSON sends a GET with bearer auth and returns the raw response body.
func getJSON(url, token string) ([]byte, error) {
	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// ---------------------------------------------------------------------------
// Config saving
// ---------------------------------------------------------------------------

// feishuConfig represents a feishu/lark addon config file.
type feishuConfig struct {
	Accounts []feishuAccount `json:"accounts"`
}

type feishuAccount struct {
	Alias    string `json:"alias"`
	AppID    string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

func (m FeishuOnboardModel) saveConfig() error {
	cfg := feishuConfig{
		Accounts: []feishuAccount{
			{
				Alias:    "default",
				AppID:    m.appID,
				AppSecret: m.appSecret,
			},
		},
	}
	configPath := filepath.Join(m.lingtaiDir, ".addons", "feishu", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0o600)
}

// ---------------------------------------------------------------------------
// QR rendering
// ---------------------------------------------------------------------------

func renderQR(data string) string {
	qr, err := qrcode.New(data, qrcode.Low)
	if err != nil {
		return ""
	}
	return qr.ToSmallString(false)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m FeishuOnboardModel) Update(msg tea.Msg) (FeishuOnboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case feishuQRReadyMsg:
		m.url = msg.URL
		m.deviceCode = msg.DeviceCode
		m.interval = msg.Interval
		m.expireIn = msg.ExpireIn
		if m.interval <= 0 {
			m.interval = 5
		}
		if m.expireIn <= 0 {
			m.expireIn = 600
		}
		m.domain = msg.Domain
		m.qrData = renderQR(msg.URL)
		m.step = feishuStepPolling
		m.pollDeadline = time.Now().Add(time.Duration(m.expireIn) * time.Second)
		m.message = ""
		return m, tea.Batch(
			m.runPoll(),
			tea.Tick(time.Second, func(t time.Time) tea.Msg {
				return feishuRemainingTickMsg{}
			}),
		)

	case feishuRemainingTickMsg:
		if m.step != feishuStepPolling {
			return m, nil
		}
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return feishuRemainingTickMsg{}
		})

	case feishuPollDoneMsg:
		if msg.Err != nil {
			// If we already have credentials, the error is from probe — save anyway.
			if m.appID != "" {
				m.botName = msg.BotName
				m.step = feishuStepDone
				m.message = i18n.T("feishu.onboard.success")
				if m.botName != "" {
					m.message = fmt.Sprintf("Connected as %s", m.botName)
				}
				return m, nil
			}
			m.step = feishuStepError
			m.message = msg.Err.Error()
			return m, nil
		}

		// Poll succeeded — got credentials
		if m.appID == "" {
			m.appID = msg.AppID
			m.appSecret = msg.AppSecret
			m.domain = msg.Domain
			// Save config immediately, then probe bot (best-effort)
			if err := m.saveConfig(); err != nil {
				m.step = feishuStepError
				m.message = fmt.Sprintf("save config: %v", err)
				return m, nil
			}
			if msg.BotName != "" {
				m.botName = msg.BotName
				m.step = feishuStepDone
				m.message = fmt.Sprintf("Connected as %s — /refresh to apply", m.botName)
				return m, func() tea.Msg { return FeishuOnboardDoneMsg{AppID: m.appID, AppSecret: m.appSecret, Domain: m.domain, BotName: m.botName} }
			}
			// Probe bot for display name
			return m, m.runProbeBot()
		}

		// Probe result (has appID already)
		m.botName = msg.BotName
		m.step = feishuStepDone
		if msg.Err != nil {
			m.message = "Config saved — /refresh to apply"
		} else {
			m.message = fmt.Sprintf("Connected as %s — /refresh to apply", m.botName)
		}
		return m, func() tea.Msg {
			return FeishuOnboardDoneMsg{AppID: m.appID, AppSecret: m.appSecret, Domain: m.domain, BotName: m.botName}
		}

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			if m.step == feishuStepDone || m.step == feishuStepError {
				return m, func() tea.Msg { return ViewChangeMsg{View: "addon"} }
			}
			return m, func() tea.Msg { return ViewChangeMsg{View: "addon"} }
		case "enter":
			if m.step == feishuStepError {
				// Retry
				m.step = feishuStepInit
				m.message = ""
				return m, m.runInitAndBegin()
			}
			if m.step == feishuStepDone {
				return m, func() tea.Msg { return ViewChangeMsg{View: "addon"} }
			}
		case "ctrl+c":
			return m, func() tea.Msg { return tea.QuitMsg{} }
		}
	}

	return m, nil
}


// fmtDuration formats a time.Duration as a human-readable countdown.
func fmtDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m > 0 {
		return fmt.Sprintf("(%dm %ds remaining)", m, s)
	}
	return fmt.Sprintf("(%ds remaining)", s)
}
// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m FeishuOnboardModel) View() string {
	var b strings.Builder

	// Title bar
	titleBar := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent).Render("LingTai") + " " +
		StyleAccent.Render(RuneBullet) + " " +
		StyleTitle.Render(i18n.T("feishu.onboard.title"))
	escHint := StyleAccent.Render("[esc] ") + StyleSubtle.Render(i18n.T("common.back"))
	padding := m.width - lipgloss.Width(titleBar) - lipgloss.Width(escHint) - 1
	if padding > 0 {
		b.WriteString(titleBar + strings.Repeat(" ", padding) + escHint + "\n")
	} else {
		b.WriteString(titleBar + "  " + escHint + "\n")
	}
	b.WriteString(strings.Repeat("─", m.width) + "\n\n")

	switch m.step {
	case feishuStepInit:
		b.WriteString("  " + StyleSubtle.Render(i18n.T("feishu.onboard.connecting")) + "\n")

	case feishuStepQR, feishuStepPolling:
		// QR code
		if m.qrData != "" {
			qrStyle := lipgloss.NewStyle().Foreground(ColorAgent).Align(lipgloss.Center)
			// Indent each line of the QR
			for _, line := range strings.Split(m.qrData, "\n") {
				b.WriteString(qrStyle.Render("  " + line) + "\n")
			}
			b.WriteString("\n")
		}

		// URL (Ctrl+clickable in most terminals)
		b.WriteString("  " + StyleSubtle.Render(i18n.T("feishu.onboard.url_hint")) + "\n")
		urlStyle := lipgloss.NewStyle().Foreground(ColorAccent).Underline(true)
		b.WriteString(urlStyle.Render("  " + m.url) + "\n\n")

		// Status with countdown
		scanning := i18n.T("feishu.onboard.scanning")
		if !m.pollDeadline.IsZero() {
			remaining := time.Until(m.pollDeadline)
			if remaining > 0 {
				scanning += "  " + fmtDuration(remaining)
			}
		}
		b.WriteString("  " + StyleSubtle.Render(scanning) + "\n")

	case feishuStepDone:
		b.WriteString("  " + StyleAccent.Render(m.message) + "\n")
		b.WriteString("\n  " + StyleSubtle.Render(i18n.T("feishu.onboard.done_hint")) + "\n")

	case feishuStepError:
		errStyle := lipgloss.NewStyle().Foreground(ColorSuspended)
		b.WriteString("  " + errStyle.Render(m.message) + "\n")
		b.WriteString("\n  " + StyleSubtle.Render(i18n.T("feishu.onboard.retry_hint")) + "\n")
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width) + "\n")
	switch m.step {
	case feishuStepQR, feishuStepPolling:
		b.WriteString(StyleFaint.Render("  [esc] "+i18n.T("common.cancel")) + "\n")
	case feishuStepError:
		b.WriteString(StyleFaint.Render("  [enter] "+i18n.T("common.retry")+"    [esc] "+i18n.T("common.back")) + "\n")
	case feishuStepDone:
		b.WriteString(StyleFaint.Render("  [enter] "+i18n.T("common.back")) + "\n")
	}

	return b.String()
}
