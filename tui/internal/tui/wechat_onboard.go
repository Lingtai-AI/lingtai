package tui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// ---------------------------------------------------------------------------
// HTTP layer — WeChat iLink Bot QR onboarding
// ---------------------------------------------------------------------------

const (
	wechatBaseURL             = "https://ilinkai.weixin.qq.com"
	wechatCDNBaseURL          = "https://novac2c.cdn.weixin.qq.com/c2c"
	wechatGetBotQRCodePath    = "/ilink/bot/get_bot_qrcode"
	wechatGetQRCodeStatusPath = "/ilink/bot/get_qrcode_status"
	wechatBotType             = "3"
	wechatAppID               = "bot"
	wechatClientVersion       = "131331"
	wechatQRFetchTimeout      = 15 * time.Second
	wechatPollTimeout         = 60 * time.Second
	wechatDefaultPollInterval = 1
	wechatDefaultQRExpireIn   = 480
	wechatDefaultConfigJSON   = "{\n  \"base_url\": \"https://ilinkai.weixin.qq.com\",\n  \"cdn_base_url\": \"https://novac2c.cdn.weixin.qq.com/c2c\",\n  \"poll_interval\": 1.0,\n  \"allowed_users\": []\n}\n"
)

func normalizeWechatBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return wechatBaseURL
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return strings.TrimRight(raw, "/")
	}
	return "https://" + strings.Trim(strings.TrimSpace(raw), "/")
}

func effectiveWechatBaseURL(raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return normalizeWechatBaseURL(fallback)
	}
	return normalizeWechatBaseURL(raw)
}

func randomWechatUIN() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	value := binary.BigEndian.Uint32(buf[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(value), 10))), nil
}

func wechatRequestHeaders() (http.Header, error) {
	uin, err := randomWechatUIN()
	if err != nil {
		return nil, fmt.Errorf("generate X-WECHAT-UIN: %w", err)
	}

	headers := http.Header{}
	headers.Set("X-WECHAT-UIN", uin)
	headers.Set("iLink-App-Id", wechatAppID)
	headers.Set("iLink-App-ClientVersion", wechatClientVersion)
	return headers, nil
}

type wechatQRResp struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

func (r wechatQRResp) scanData() string {
	if strings.TrimSpace(r.QRCodeImgContent) != "" {
		return r.QRCodeImgContent
	}
	return r.QRCode
}

type wechatPollResp struct {
	Status       string `json:"status"`
	BotToken     string `json:"bot_token"`
	ILinkUserID  string `json:"ilink_user_id"`
	BaseURL      string `json:"baseurl"`
	RedirectHost string `json:"redirect_host"`
}

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

type wechatOnboardStep int

const (
	wechatStepInit wechatOnboardStep = iota
	wechatStepQR
	wechatStepPolling
	wechatStepDone
	wechatStepError
)

type wechatRemainingTickMsg struct{}

// WechatOnboardDoneMsg is emitted after credentials are saved and the addon
// config should be reloaded.
type WechatOnboardDoneMsg struct {
	BotToken string
	UserID   string
	BaseURL  string
	Err      error
}

type wechatQRReadyMsg struct {
	URL      string
	QRCode   string
	BaseURL  string
	Interval int
	ExpireIn int
}

type wechatPollDoneMsg struct {
	BotToken string
	UserID   string
	BaseURL  string
	Err      error
}

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

// WechatOnboardModel drives the WeChat iLink scan-to-connect QR flow.
type WechatOnboardModel struct {
	step         wechatOnboardStep
	qrData       string
	url          string
	qrCode       string
	interval     int
	expireIn     int
	baseURL      string
	message      string
	botToken     string
	userID       string
	pollDeadline time.Time
	ctx          context.Context
	cancelFunc   context.CancelFunc

	lingtaiDir string
	width      int
	height     int

	qrClient   *http.Client
	pollClient *http.Client
}

// NewWechatOnboardModel creates a new WeChat onboard model and starts the
// QR acquisition flow in the background.
func NewWechatOnboardModel(lingtaiDir string) WechatOnboardModel {
	return WechatOnboardModel{
		step:       wechatStepInit,
		lingtaiDir: lingtaiDir,
		baseURL:    wechatBaseURL,
		qrClient:   &http.Client{Timeout: wechatQRFetchTimeout},
		pollClient: &http.Client{Timeout: wechatPollTimeout},
	}
}

func (m WechatOnboardModel) Init() tea.Cmd {
	return m.runFetchQR()
}

// ---------------------------------------------------------------------------
// Background goroutines → tea.Msg
// ---------------------------------------------------------------------------

func (m WechatOnboardModel) runFetchQR() tea.Cmd {
	baseURL := effectiveWechatBaseURL(m.baseURL, wechatBaseURL)
	client := m.httpClientForQR()
	return func() tea.Msg {
		qr, err := callWechatQR(client, baseURL)
		if err != nil {
			return wechatPollDoneMsg{Err: err}
		}

		return wechatQRReadyMsg{
			URL:      qr.scanData(),
			QRCode:   qr.QRCode,
			BaseURL:  baseURL,
			Interval: wechatDefaultPollInterval,
			ExpireIn: wechatDefaultQRExpireIn,
		}
	}
}

func (m WechatOnboardModel) runPoll() tea.Cmd {
	qrCode := m.qrCode
	interval := m.interval
	expireIn := m.expireIn
	baseURL := effectiveWechatBaseURL(m.baseURL, wechatBaseURL)
	client := m.httpClientForPoll()
	return func() tea.Msg {
		if m.ctx == nil {
			return wechatPollDoneMsg{Err: fmt.Errorf("wechat: no poll context")}
		}
		if interval <= 0 {
			interval = wechatDefaultPollInterval
		}
		if expireIn <= 0 {
			expireIn = wechatDefaultQRExpireIn
		}

		deadline := time.Now().Add(time.Duration(expireIn) * time.Second)
		currentBaseURL := baseURL

		for time.Now().Before(deadline) {
			select {
			case <-m.ctx.Done():
				return wechatPollDoneMsg{Err: fmt.Errorf("wechat: onboarding cancelled")}
			default:
			}

			poll, err := callWechatPoll(m.ctx, client, qrCode, currentBaseURL)
			if err != nil {
				time.Sleep(time.Duration(interval) * time.Second)
				continue
			}

			switch poll.Status {
			case "", "wait":
				time.Sleep(time.Duration(interval) * time.Second)
			case "scaned":
				time.Sleep(time.Duration(interval) * time.Second)
			case "scaned_but_redirect":
				redirectHost := strings.TrimSpace(poll.RedirectHost)
				if redirectHost == "" {
					time.Sleep(time.Duration(interval) * time.Second)
					continue
				}
				nextBaseURL := normalizeWechatBaseURL(redirectHost)
				if nextBaseURL == currentBaseURL {
					time.Sleep(time.Duration(interval) * time.Second)
					continue
				}
				currentBaseURL = nextBaseURL
			case "confirmed":
				if poll.BotToken == "" {
					return wechatPollDoneMsg{Err: fmt.Errorf("wechat: QR confirmed but bot_token missing")}
				}
				return wechatPollDoneMsg{
					BotToken: poll.BotToken,
					UserID:   poll.ILinkUserID,
					BaseURL:  effectiveWechatBaseURL(poll.BaseURL, currentBaseURL),
				}
			case "expired":
				return wechatPollDoneMsg{Err: fmt.Errorf("wechat: QR code expired")}
			default:
				time.Sleep(time.Duration(interval) * time.Second)
			}
		}

		return wechatPollDoneMsg{Err: fmt.Errorf("wechat: poll timed out after %ds", expireIn)}
	}
}

func (m WechatOnboardModel) httpClientForQR() *http.Client {
	if m.qrClient != nil {
		return m.qrClient
	}
	return &http.Client{Timeout: wechatQRFetchTimeout}
}

func (m WechatOnboardModel) httpClientForPoll() *http.Client {
	if m.pollClient != nil {
		return m.pollClient
	}
	return &http.Client{Timeout: wechatPollTimeout}
}

// ---------------------------------------------------------------------------
// WeChat API HTTP calls (stdlib net/http)
// ---------------------------------------------------------------------------

func callWechatQR(client *http.Client, baseURL string) (*wechatQRResp, error) {
	u, err := url.Parse(normalizeWechatBaseURL(baseURL) + wechatGetBotQRCodePath)
	if err != nil {
		return nil, fmt.Errorf("parse QR URL: %w", err)
	}

	query := u.Query()
	query.Set("bot_type", wechatBotType)
	u.RawQuery = query.Encode()

	respJSON, err := wechatGetJSON(context.Background(), client, u.String())
	if err != nil {
		return nil, err
	}

	var qr wechatQRResp
	if err := json.Unmarshal(respJSON, &qr); err != nil {
		return nil, fmt.Errorf("parse QR response: %w", err)
	}
	if qr.QRCode == "" {
		return nil, fmt.Errorf("wechat: no qrcode in response")
	}
	return &qr, nil
}

func callWechatPoll(ctx context.Context, client *http.Client, qrCode, baseURL string) (*wechatPollResp, error) {
	u, err := url.Parse(normalizeWechatBaseURL(baseURL) + wechatGetQRCodeStatusPath)
	if err != nil {
		return nil, fmt.Errorf("parse poll URL: %w", err)
	}

	query := u.Query()
	query.Set("qrcode", qrCode)
	u.RawQuery = query.Encode()

	respJSON, err := wechatGetJSON(ctx, client, u.String())
	if err != nil {
		return nil, err
	}

	var poll wechatPollResp
	if err := json.Unmarshal(respJSON, &poll); err != nil {
		return nil, fmt.Errorf("parse poll response: %w", err)
	}
	return &poll, nil
}

func wechatGetJSON(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	if client == nil {
		client = &http.Client{}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	headers, err := wechatRequestHeaders()
	if err != nil {
		return nil, err
	}
	httpReq.Header = headers

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyPrefix, readErr := io.ReadAll(io.LimitReader(resp.Body, 200))
		if readErr != nil {
			return nil, readErr
		}
		return nil, fmt.Errorf(
			"wechat: GET %s returned HTTP %d: %s",
			rawURL,
			resp.StatusCode,
			strings.TrimSpace(string(bodyPrefix)),
		)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

// ---------------------------------------------------------------------------
// Config saving
// ---------------------------------------------------------------------------

type wechatCredentials struct {
	BotToken string `json:"bot_token"`
	UserID   string `json:"user_id"`
	BaseURL  string `json:"base_url"`
	SavedAt  string `json:"saved_at"`
}

func (m WechatOnboardModel) saveConfig() error {
	addonDir := filepath.Join(m.lingtaiDir, ".addons", "wechat")
	if err := os.MkdirAll(addonDir, 0o700); err != nil {
		return err
	}

	configPath := filepath.Join(addonDir, "config.json")
	if _, err := os.Stat(configPath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(configPath, []byte(wechatDefaultConfigJSON), 0o600); err != nil {
			return err
		}
	}

	credentialsPath := filepath.Join(addonDir, "credentials.json")
	credentials := wechatCredentials{
		BotToken: m.botToken,
		UserID:   m.userID,
		BaseURL:  effectiveWechatBaseURL(m.baseURL, wechatBaseURL),
		SavedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(credentialsPath, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(credentialsPath, 0o600)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m WechatOnboardModel) Update(msg tea.Msg) (WechatOnboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case wechatQRReadyMsg:
		m.ctx, m.cancelFunc = context.WithCancel(context.Background())
		m.url = msg.URL
		m.qrCode = msg.QRCode
		m.baseURL = effectiveWechatBaseURL(msg.BaseURL, wechatBaseURL)
		m.interval = msg.Interval
		m.expireIn = msg.ExpireIn
		if m.interval <= 0 {
			m.interval = wechatDefaultPollInterval
		}
		if m.expireIn <= 0 {
			m.expireIn = wechatDefaultQRExpireIn
		}
		m.qrData = renderQR(msg.URL)
		m.step = wechatStepPolling
		m.pollDeadline = time.Now().Add(time.Duration(m.expireIn) * time.Second)
		m.message = ""
		return m, tea.Batch(
			m.runPoll(),
			tea.Tick(time.Second, func(t time.Time) tea.Msg {
				return wechatRemainingTickMsg{}
			}),
		)

	case wechatRemainingTickMsg:
		if m.step != wechatStepPolling {
			return m, nil
		}
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return wechatRemainingTickMsg{}
		})

	case wechatPollDoneMsg:
		if msg.Err != nil {
			m.step = wechatStepError
			m.message = msg.Err.Error()
			return m, nil
		}

		m.botToken = msg.BotToken
		m.userID = msg.UserID
		m.baseURL = effectiveWechatBaseURL(msg.BaseURL, m.baseURL)
		if err := m.saveConfig(); err != nil {
			m.step = wechatStepError
			m.message = fmt.Sprintf("save config: %v", err)
			return m, nil
		}

		m.step = wechatStepDone
		if m.userID != "" {
			m.message = i18n.TF("wechat.onboard.connected_as_refresh", m.userID)
		} else {
			m.message = i18n.T("wechat.onboard.config_saved")
		}
		return m, func() tea.Msg {
			return WechatOnboardDoneMsg{
				BotToken: m.botToken,
				UserID:   m.userID,
				BaseURL:  m.baseURL,
			}
		}

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			if m.cancelFunc != nil {
				m.cancelFunc()
			}
			return m, func() tea.Msg { return ViewChangeMsg{View: "addon"} }
		case "enter":
			if m.step == wechatStepError {
				m.step = wechatStepInit
				m.message = ""
				m.qrData = ""
				m.url = ""
				m.qrCode = ""
				m.pollDeadline = time.Time{}
				return m, m.runFetchQR()
			}
			if m.step == wechatStepDone {
				return m, func() tea.Msg { return ViewChangeMsg{View: "addon"} }
			}
		case "ctrl+c":
			return m, func() tea.Msg { return tea.QuitMsg{} }
		}
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m WechatOnboardModel) View() string {
	var b strings.Builder

	titleBar := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent).Render("LingTai") + " " +
		StyleAccent.Render(RuneBullet) + " " +
		StyleTitle.Render(i18n.T("wechat.onboard.title"))
	escHint := StyleAccent.Render("[esc] ") + StyleSubtle.Render(i18n.T("common.back"))
	padding := m.width - lipgloss.Width(titleBar) - lipgloss.Width(escHint) - 1
	if padding > 0 {
		b.WriteString(titleBar + strings.Repeat(" ", padding) + escHint + "\n")
	} else {
		b.WriteString(titleBar + "  " + escHint + "\n")
	}
	b.WriteString(strings.Repeat("─", m.width) + "\n\n")

	switch m.step {
	case wechatStepInit:
		b.WriteString("  " + StyleSubtle.Render(i18n.T("wechat.onboard.connecting")) + "\n")

	case wechatStepQR, wechatStepPolling:
		b.WriteString("  " + StyleSubtle.Render(i18n.T("wechat.onboard.admin_warning")) + "\n\n")
		b.WriteString("  " + StyleSubtle.Render(i18n.T("wechat.onboard.url_hint")) + "\n")
		urlStyle := lipgloss.NewStyle().Foreground(ColorAccent).Underline(true)
		b.WriteString(urlStyle.Render("  "+m.url) + "\n\n")

		scanHint := i18n.T("wechat.onboard.scan_hint")
		if !m.pollDeadline.IsZero() {
			remaining := time.Until(m.pollDeadline)
			if remaining > 0 {
				scanHint += " " + fmtOnboardRemaining("wechat.onboard", remaining)
			}
		}
		b.WriteString("  " + StyleSubtle.Render(scanHint) + "\n\n")

		if m.qrData != "" {
			for _, line := range strings.Split(m.qrData, "\n") {
				b.WriteString("  " + line + "\n")
			}
			b.WriteString("\n")
		}

	case wechatStepDone:
		b.WriteString("  " + StyleAccent.Render(m.message) + "\n")
		b.WriteString("\n  " + StyleSubtle.Render(i18n.T("wechat.onboard.done_hint")) + "\n")

	case wechatStepError:
		errStyle := lipgloss.NewStyle().Foreground(ColorSuspended)
		b.WriteString("  " + errStyle.Render(m.message) + "\n")
		b.WriteString("\n  " + StyleSubtle.Render(i18n.T("wechat.onboard.retry_hint")) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width) + "\n")
	switch m.step {
	case wechatStepQR, wechatStepPolling:
		b.WriteString(StyleFaint.Render("  [esc] "+i18n.T("common.cancel")) + "\n")
	case wechatStepError:
		b.WriteString(StyleFaint.Render("  [enter] "+i18n.T("common.retry")+"    [esc] "+i18n.T("common.back")) + "\n")
	case wechatStepDone:
		b.WriteString(StyleFaint.Render("  [enter] "+i18n.T("common.back")) + "\n")
	}

	return b.String()
}
