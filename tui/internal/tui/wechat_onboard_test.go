package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

func TestWechatNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", wechatBaseURL},
		{"https://ilinkai.weixin.qq.com/", wechatBaseURL},
		{"ilinkai.weixin.qq.com", wechatBaseURL},
		{"redirect.weixin.qq.com", "https://redirect.weixin.qq.com"},
	}

	for _, tc := range tests {
		if got := normalizeWechatBaseURL(tc.in); got != tc.want {
			t.Fatalf("normalizeWechatBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWechatRandomUIN(t *testing.T) {
	got, err := randomWechatUIN()
	if err != nil {
		t.Fatalf("randomWechatUIN() error = %v", err)
	}
	if got == "" {
		t.Fatal("randomWechatUIN() returned empty string")
	}

	decoded, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("randomWechatUIN() returned invalid base64: %v", err)
	}
	if len(decoded) == 0 {
		t.Fatal("decoded X-WECHAT-UIN is empty")
	}
	for _, ch := range string(decoded) {
		if ch < '0' || ch > '9' {
			t.Fatalf("decoded X-WECHAT-UIN = %q, want digits only", decoded)
		}
	}
}

func TestWechatRequestHeaders(t *testing.T) {
	headers, err := wechatRequestHeaders()
	if err != nil {
		t.Fatalf("wechatRequestHeaders() error = %v", err)
	}
	if headers.Get("iLink-App-Id") != wechatAppID {
		t.Fatalf("iLink-App-Id = %q, want %q", headers.Get("iLink-App-Id"), wechatAppID)
	}
	if headers.Get("iLink-App-ClientVersion") != wechatClientVersion {
		t.Fatalf("iLink-App-ClientVersion = %q, want %q", headers.Get("iLink-App-ClientVersion"), wechatClientVersion)
	}
	if headers.Get("X-WECHAT-UIN") == "" {
		t.Fatal("X-WECHAT-UIN is empty")
	}
}

func TestWechatQRRespScanData(t *testing.T) {
	withURL := wechatQRResp{QRCode: "raw-token", QRCodeImgContent: "https://example.com/qr"}
	if got := withURL.scanData(); got != "https://example.com/qr" {
		t.Fatalf("scanData() = %q, want qrcode_img_content", got)
	}

	withoutURL := wechatQRResp{QRCode: "raw-token"}
	if got := withoutURL.scanData(); got != "raw-token" {
		t.Fatalf("scanData() = %q, want qrcode token fallback", got)
	}
}

func TestWechatFmtOnboardRemaining(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{23 * time.Second, "23"},
		{2*time.Minute + 5*time.Second, "2m 5"},
	}

	for _, tc := range tests {
		got := fmtOnboardRemaining("wechat.onboard", tc.d)
		if tc.d <= 0 {
			if got != "" {
				t.Fatalf("fmtOnboardRemaining(%v) = %q, want empty", tc.d, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Fatalf("fmtOnboardRemaining(%v) = %q, want substring %q", tc.d, got, tc.want)
		}
	}
}

func TestWechatCallQRUsesHeadersAndParsesResponse(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			if got := r.URL.String(); got != wechatBaseURL+wechatGetBotQRCodePath+"?bot_type=3" {
				t.Fatalf("URL = %q", got)
			}
			if r.Header.Get("iLink-App-Id") != wechatAppID {
				t.Fatalf("iLink-App-Id = %q", r.Header.Get("iLink-App-Id"))
			}
			if r.Header.Get("iLink-App-ClientVersion") != wechatClientVersion {
				t.Fatalf("iLink-App-ClientVersion = %q", r.Header.Get("iLink-App-ClientVersion"))
			}
			if r.Header.Get("X-WECHAT-UIN") == "" {
				t.Fatal("X-WECHAT-UIN missing")
			}
			return jsonHTTPResponse(`{"qrcode":"token-1","qrcode_img_content":"https://qr.example/scan"}`), nil
		}),
	}

	resp, err := callWechatQR(client, wechatBaseURL)
	if err != nil {
		t.Fatalf("callWechatQR() error = %v", err)
	}
	if resp.QRCode != "token-1" {
		t.Fatalf("QRCode = %q, want token-1", resp.QRCode)
	}
	if resp.QRCodeImgContent != "https://qr.example/scan" {
		t.Fatalf("QRCodeImgContent = %q", resp.QRCodeImgContent)
	}
}

func TestWechatCallPollParsesResponse(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if got := r.URL.String(); got != wechatBaseURL+wechatGetQRCodeStatusPath+"?qrcode=token-1" {
				t.Fatalf("URL = %q", got)
			}
			return jsonHTTPResponse(`{"status":"confirmed","bot_token":"bot-123","ilink_user_id":"wxid_1","baseurl":"https://redir.weixin.qq.com"}`), nil
		}),
	}

	resp, err := callWechatPoll(context.Background(), client, "token-1", wechatBaseURL)
	if err != nil {
		t.Fatalf("callWechatPoll() error = %v", err)
	}
	if resp.Status != "confirmed" || resp.BotToken != "bot-123" || resp.ILinkUserID != "wxid_1" {
		t.Fatalf("unexpected poll response: %+v", resp)
	}
}

func TestWechatCallPollReturnsHTTPStatusError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(strings.NewReader("gateway exploded and returned too much detail")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	_, err := callWechatPoll(context.Background(), client, "token-1", wechatBaseURL)
	if err == nil {
		t.Fatal("callWechatPoll() error = nil, want HTTP status error")
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("error = %v, want status code", err)
	}
	if !strings.Contains(err.Error(), "gateway exploded") {
		t.Fatalf("error = %v, want body prefix", err)
	}
}

func TestWechatRunPollRedirectAndConfirm(t *testing.T) {
	callCount := 0
	pollClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			callCount++
			switch callCount {
			case 1:
				if r.URL.Host != "ilinkai.weixin.qq.com" {
					t.Fatalf("first host = %q, want ilinkai.weixin.qq.com", r.URL.Host)
				}
				return jsonHTTPResponse(`{"status":"scaned_but_redirect","redirect_host":"redirect.weixin.qq.com"}`), nil
			case 2:
				if r.URL.Host != "redirect.weixin.qq.com" {
					t.Fatalf("second host = %q, want redirect.weixin.qq.com", r.URL.Host)
				}
				return jsonHTTPResponse(`{"status":"confirmed","bot_token":"bot-123","ilink_user_id":"wxid_1","baseurl":"https://redirect.weixin.qq.com"}`), nil
			default:
				t.Fatalf("unexpected call count %d", callCount)
				return nil, nil
			}
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := WechatOnboardModel{
		ctx:        ctx,
		qrCode:     "token-1",
		interval:   0,
		expireIn:   5,
		baseURL:    wechatBaseURL,
		pollClient: pollClient,
	}

	msg := m.runPoll()()
	done, ok := msg.(wechatPollDoneMsg)
	if !ok {
		t.Fatalf("runPoll() = %T, want wechatPollDoneMsg", msg)
	}
	if done.Err != nil {
		t.Fatalf("runPoll() err = %v", done.Err)
	}
	if done.BotToken != "bot-123" || done.UserID != "wxid_1" || done.BaseURL != "https://redirect.weixin.qq.com" {
		t.Fatalf("runPoll() = %+v", done)
	}
}

func TestWechatRunPollExpired(t *testing.T) {
	pollClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return jsonHTTPResponse(`{"status":"expired"}`), nil
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := WechatOnboardModel{
		ctx:        ctx,
		qrCode:     "token-1",
		interval:   0,
		expireIn:   5,
		baseURL:    wechatBaseURL,
		pollClient: pollClient,
	}

	msg := m.runPoll()()
	done, ok := msg.(wechatPollDoneMsg)
	if !ok {
		t.Fatalf("runPoll() = %T, want wechatPollDoneMsg", msg)
	}
	if done.Err == nil || !strings.Contains(done.Err.Error(), "expired") {
		t.Fatalf("runPoll() err = %v, want expired error", done.Err)
	}
}

func TestWechatRunPollUsesInjectedPollClient(t *testing.T) {
	qrCalls := 0
	pollCalls := 0

	qrClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			qrCalls++
			return jsonHTTPResponse(`{"qrcode":"token-1","qrcode_img_content":"https://qr.example/scan"}`), nil
		}),
	}
	pollClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			pollCalls++
			return jsonHTTPResponse(`{"status":"confirmed","bot_token":"bot-123","ilink_user_id":"wxid_1","baseurl":"https://redirect.weixin.qq.com"}`), nil
		}),
	}

	m := WechatOnboardModel{
		baseURL:    wechatBaseURL,
		qrClient:   qrClient,
		pollClient: pollClient,
	}

	qrMsg := m.runFetchQR()()
	ready, ok := qrMsg.(wechatQRReadyMsg)
	if !ok {
		t.Fatalf("runFetchQR() = %T, want wechatQRReadyMsg", qrMsg)
	}
	if qrCalls != 1 {
		t.Fatalf("qrCalls = %d, want 1", qrCalls)
	}
	if pollCalls != 0 {
		t.Fatalf("pollCalls after runFetchQR = %d, want 0", pollCalls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.ctx = ctx
	m.qrCode = ready.QRCode
	m.interval = ready.Interval
	m.expireIn = ready.ExpireIn

	pollMsg := m.runPoll()()
	done, ok := pollMsg.(wechatPollDoneMsg)
	if !ok {
		t.Fatalf("runPoll() = %T, want wechatPollDoneMsg", pollMsg)
	}
	if done.Err != nil {
		t.Fatalf("runPoll() err = %v", done.Err)
	}
	if qrCalls != 1 {
		t.Fatalf("qrCalls after runPoll = %d, want 1", qrCalls)
	}
	if pollCalls != 1 {
		t.Fatalf("pollCalls = %d, want 1", pollCalls)
	}
}

func TestNewWechatOnboardModelUsesDistinctTimeouts(t *testing.T) {
	m := NewWechatOnboardModel(t.TempDir())
	if m.qrClient == nil || m.pollClient == nil {
		t.Fatal("expected both QR and poll clients to be initialized")
	}
	if m.qrClient == m.pollClient {
		t.Fatal("expected distinct HTTP clients for QR and poll")
	}
	if m.qrClient.Timeout != wechatQRFetchTimeout {
		t.Fatalf("qr timeout = %v, want %v", m.qrClient.Timeout, wechatQRFetchTimeout)
	}
	if m.pollClient.Timeout != wechatPollTimeout {
		t.Fatalf("poll timeout = %v, want %v", m.pollClient.Timeout, wechatPollTimeout)
	}
}

func TestWechatSaveConfigCreatesDefaultAndCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	lingtaiDir := filepath.Join(tmpDir, ".lingtai")

	m := WechatOnboardModel{
		lingtaiDir: lingtaiDir,
		botToken:   "bot-123",
		userID:     "wxid_1",
		baseURL:    "https://redirect.weixin.qq.com",
	}

	if err := m.saveConfig(); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	configPath := filepath.Join(lingtaiDir, ".addons", "wechat", "config.json")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(config.json) error = %v", err)
	}
	if string(configData) != wechatDefaultConfigJSON {
		t.Fatalf("config.json = %q, want exact template JSON", string(configData))
	}

	credentialsPath := filepath.Join(lingtaiDir, ".addons", "wechat", "credentials.json")
	assertWechatCredentials(t, credentialsPath, "bot-123", "wxid_1", "https://redirect.weixin.qq.com")
}

func TestWechatSaveConfigPreservesExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	lingtaiDir := filepath.Join(tmpDir, ".lingtai")
	addonDir := filepath.Join(lingtaiDir, ".addons", "wechat")
	if err := os.MkdirAll(addonDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	configPath := filepath.Join(addonDir, "config.json")
	originalConfig := "{\n  \"base_url\": \"https://custom.example\",\n  \"allowed_users\": [\"wxid_keep\"]\n}\n"
	if err := os.WriteFile(configPath, []byte(originalConfig), 0o600); err != nil {
		t.Fatalf("WriteFile(config.json) error = %v", err)
	}

	m := WechatOnboardModel{
		lingtaiDir: lingtaiDir,
		botToken:   "bot-456",
		userID:     "wxid_2",
		baseURL:    "https://redirect.weixin.qq.com",
	}
	if err := m.saveConfig(); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(config.json) error = %v", err)
	}
	if string(configData) != originalConfig {
		t.Fatalf("config.json = %q, want preserved content %q", string(configData), originalConfig)
	}

	credentialsPath := filepath.Join(addonDir, "credentials.json")
	assertWechatCredentials(t, credentialsPath, "bot-456", "wxid_2", "https://redirect.weixin.qq.com")
}

func TestWechatUpdateEmitsDoneMsgAfterSave(t *testing.T) {
	tmpDir := t.TempDir()
	lingtaiDir := filepath.Join(tmpDir, ".lingtai")

	m := WechatOnboardModel{
		lingtaiDir: lingtaiDir,
		baseURL:    wechatBaseURL,
	}

	updated, cmd := m.Update(wechatPollDoneMsg{
		BotToken: "bot-123",
		UserID:   "wxid_1",
		BaseURL:  "https://redirect.weixin.qq.com",
	})
	if updated.step != wechatStepDone {
		t.Fatalf("step = %v, want %v", updated.step, wechatStepDone)
	}
	if cmd == nil {
		t.Fatal("cmd = nil, want WechatOnboardDoneMsg emitter")
	}

	msg := cmd()
	done, ok := msg.(WechatOnboardDoneMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want WechatOnboardDoneMsg", msg)
	}
	if done.BotToken != "bot-123" || done.UserID != "wxid_1" || done.BaseURL != "https://redirect.weixin.qq.com" {
		t.Fatalf("done msg = %+v", done)
	}
}

func TestWechatAddonUpdateRefreshesOnDone(t *testing.T) {
	tmpDir := t.TempDir()
	lingtaiDir := filepath.Join(tmpDir, ".lingtai")
	m := NewAddonModel(lingtaiDir)

	if _, ok := m.addonConfigs["wechat"]; ok {
		t.Fatal("expected wechat config to be absent before refresh")
	}

	if err := (WechatOnboardModel{
		lingtaiDir: lingtaiDir,
		botToken:   "bot-123",
		userID:     "wxid_1",
		baseURL:    "https://redirect.weixin.qq.com",
	}).saveConfig(); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	updated, cmd := m.Update(WechatOnboardDoneMsg{BotToken: "bot-123", UserID: "wxid_1"})
	if cmd != nil {
		t.Fatalf("Update() cmd = %v, want nil", cmd)
	}
	got, ok := updated.addonConfigs["wechat"]
	if !ok {
		t.Fatal("expected wechat config to be refreshed")
	}
	if !strings.Contains(got, "\"base_url\": \"https://ilinkai.weixin.qq.com\"") {
		t.Fatalf("addon config = %q, want saved base_url", got)
	}
}

func TestWechatAddonModelTreatsMissingCredentialsAsUnconfigured(t *testing.T) {
	tmpDir := t.TempDir()
	lingtaiDir := filepath.Join(tmpDir, ".lingtai")
	addonDir := filepath.Join(lingtaiDir, ".addons", "wechat")
	if err := os.MkdirAll(addonDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(addonDir, "config.json"), []byte(wechatDefaultConfigJSON), 0o600); err != nil {
		t.Fatalf("WriteFile(config.json) error = %v", err)
	}

	m := NewAddonModel(lingtaiDir)
	if _, ok := m.addonConfigs["wechat"]; ok {
		t.Fatal("expected wechat to remain unconfigured without credentials.json")
	}
	if errMsg := m.addonErrors["wechat"]; errMsg != "" {
		t.Fatalf("unexpected wechat error = %q", errMsg)
	}

	m.cursor = indexOfAddon("wechat")
	if AllAddons[m.cursor] != "wechat" {
		t.Fatalf("cursor addon = %q, want wechat", AllAddons[m.cursor])
	}

	updated, cmd := m.Update(teaKey("enter"))
	if cmd == nil {
		t.Fatal("enter cmd = nil, want onboarding view change")
	}
	msg := cmd()
	change, ok := msg.(ViewChangeMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want ViewChangeMsg", msg)
	}
	if change.View != "wechat_onboard" {
		t.Fatalf("change.View = %q, want wechat_onboard", change.View)
	}
	if updated.cursor != m.cursor {
		t.Fatalf("cursor changed = %d, want %d", updated.cursor, m.cursor)
	}
}

func TestWechatAddonModelBlocksOnboardingWhenConfigHasError(t *testing.T) {
	tmpDir := t.TempDir()
	lingtaiDir := filepath.Join(tmpDir, ".lingtai")
	addonDir := filepath.Join(lingtaiDir, ".addons", "wechat")
	if err := os.MkdirAll(addonDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(addonDir, "config.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile(config.json) error = %v", err)
	}

	m := NewAddonModel(lingtaiDir)
	if _, ok := m.addonErrors["wechat"]; !ok {
		t.Fatal("expected wechat parse error")
	}

	m.cursor = indexOfAddon("wechat")
	if AllAddons[m.cursor] != "wechat" {
		t.Fatalf("cursor addon = %q, want wechat", AllAddons[m.cursor])
	}

	updated, cmd := m.Update(teaKey("enter"))
	if cmd != nil {
		t.Fatalf("enter cmd = %v, want nil when addon has error", cmd)
	}
	if updated.cursor != m.cursor {
		t.Fatalf("cursor changed = %d, want %d", updated.cursor, m.cursor)
	}
}

func TestWechatAddonViewShowsConfigureHint(t *testing.T) {
	m := AddonModel{
		width:        120,
		cursor:       indexOfAddon("wechat"),
		addonConfigs: map[string]string{},
		addonErrors:  map[string]string{},
	}

	view := m.View()
	if !strings.Contains(view, i18n.T("addon.configure_hint")) {
		t.Fatalf("view missing configure hint: %q", view)
	}
}

func TestWechatAppUpdateForwardsOnboardDoneMsg(t *testing.T) {
	tmpDir := t.TempDir()
	lingtaiDir := filepath.Join(tmpDir, ".lingtai")
	app := App{
		projectDir:    lingtaiDir,
		currentView:   appViewWechatOnboard,
		addon:         NewAddonModel(lingtaiDir),
		wechatOnboard: NewWechatOnboardModel(lingtaiDir),
	}

	if err := (WechatOnboardModel{
		lingtaiDir: lingtaiDir,
		botToken:   "bot-123",
		userID:     "wxid_1",
		baseURL:    "https://redirect.weixin.qq.com",
	}).saveConfig(); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	model, cmd := app.Update(WechatOnboardDoneMsg{BotToken: "bot-123", UserID: "wxid_1", BaseURL: "https://redirect.weixin.qq.com"})
	if cmd != nil {
		t.Fatalf("Update() cmd = %v, want nil", cmd)
	}

	updated, ok := model.(App)
	if !ok {
		t.Fatalf("Update() model = %T, want App", model)
	}
	got, ok := updated.addon.addonConfigs["wechat"]
	if !ok {
		t.Fatal("expected addon config to be refreshed after WechatOnboardDoneMsg")
	}
	if !strings.Contains(got, "\"poll_interval\": 1.0") {
		t.Fatalf("addon config = %q, want poll_interval", got)
	}
}

func TestWechatViewIncludesAdminWarningAndCountdown(t *testing.T) {
	m := WechatOnboardModel{
		step:         wechatStepPolling,
		url:          "https://qr.example/scan",
		qrData:       "██\n██",
		pollDeadline: time.Now().Add(90 * time.Second),
		width:        120,
	}

	view := m.View()
	if !strings.Contains(view, i18n.T("wechat.onboard.admin_warning")) {
		t.Fatalf("view missing admin warning: %q", view)
	}
	if !strings.Contains(view, i18n.T("wechat.onboard.scan_hint")) {
		t.Fatalf("view missing scan hint: %q", view)
	}
}

func assertWechatCredentials(t *testing.T, credentialsPath, wantToken, wantUserID, wantBaseURL string) {
	t.Helper()

	credentialsData, err := os.ReadFile(credentialsPath)
	if err != nil {
		t.Fatalf("ReadFile(credentials.json) error = %v", err)
	}

	var creds wechatCredentials
	if err := json.Unmarshal(credentialsData, &creds); err != nil {
		t.Fatalf("credentials.json unmarshal error = %v", err)
	}
	if creds.BotToken != wantToken || creds.UserID != wantUserID || creds.BaseURL != wantBaseURL {
		t.Fatalf("credentials = %+v", creds)
	}
	if creds.SavedAt == "" {
		t.Fatal("SavedAt is empty")
	}

	info, err := os.Stat(credentialsPath)
	if err != nil {
		t.Fatalf("Stat(credentials.json) error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode = %#o, want 0600", info.Mode().Perm())
	}
}

func teaKey(key string) tea.KeyPressMsg {
	switch key {
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		r := []rune(key)
		if len(r) == 0 {
			return tea.KeyPressMsg{}
		}
		return tea.KeyPressMsg{Code: r[0], Text: key}
	}
}

func TestWechatAddonModelReportsMalformedCredentials(t *testing.T) {
	t.Run("invalid JSON in credentials.json", func(t *testing.T) {
		tmpDir := t.TempDir()
		lingtaiDir := filepath.Join(tmpDir, ".lingtai")
		addonDir := filepath.Join(lingtaiDir, ".addons", "wechat")
		if err := os.MkdirAll(addonDir, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(addonDir, "config.json"), []byte(wechatDefaultConfigJSON), 0o600); err != nil {
			t.Fatalf("WriteFile(config.json) error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(addonDir, "credentials.json"), []byte("{not-json"), 0o600); err != nil {
			t.Fatalf("WriteFile(credentials.json) error = %v", err)
		}

		m := NewAddonModel(lingtaiDir)

		// config.json is valid, so it should be in addonConfigs
		if _, ok := m.addonConfigs["wechat"]; ok {
			t.Fatal("expected wechat NOT to be configured with malformed credentials")
		}

		// Should report an error for malformed credentials
		errMsg, hasErr := m.addonErrors["wechat"]
		if !hasErr {
			t.Fatal("expected credential error for malformed credentials.json")
		}
		if !strings.Contains(errMsg, "invalid character") && !strings.Contains(errMsg, "credentials") {
			t.Fatalf("credential error = %q, want error message about credentials.json parse failure", errMsg)
		}

		// [Enter] should NOT open onboarding when credentials has error
		m.cursor = indexOfAddon("wechat")
		_, cmd := m.Update(teaKey("enter"))
		if cmd != nil {
			t.Fatalf("enter cmd = %v, want nil when credentials have error", cmd)
		}
	})

	t.Run("empty credentials.json", func(t *testing.T) {
		tmpDir := t.TempDir()
		lingtaiDir := filepath.Join(tmpDir, ".lingtai")
		addonDir := filepath.Join(lingtaiDir, ".addons", "wechat")
		if err := os.MkdirAll(addonDir, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(addonDir, "config.json"), []byte(wechatDefaultConfigJSON), 0o600); err != nil {
			t.Fatalf("WriteFile(config.json) error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(addonDir, "credentials.json"), []byte("{}"), 0o600); err != nil {
			t.Fatalf("WriteFile(credentials.json) error = %v", err)
		}

		m := NewAddonModel(lingtaiDir)

		// Empty credentials object (no bot_token/user_id) should be treated as unconfigured
		if _, ok := m.addonConfigs["wechat"]; ok {
			t.Fatal("expected wechat NOT to be configured with empty credentials")
		}
		if errMsg := m.addonErrors["wechat"]; errMsg != "" {
			t.Fatalf("unexpected error for empty credentials = %q", errMsg)
		}

		// Should still be able to open onboarding
		m.cursor = indexOfAddon("wechat")
		_, cmd := m.Update(teaKey("enter"))
		if cmd == nil {
			t.Fatal("enter cmd = nil, want onboarding view change when credentials are empty")
		}
	})

	t.Run("missing bot_token and user_id", func(t *testing.T) {
		tmpDir := t.TempDir()
		lingtaiDir := filepath.Join(tmpDir, ".lingtai")
		addonDir := filepath.Join(lingtaiDir, ".addons", "wechat")
		if err := os.MkdirAll(addonDir, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(addonDir, "config.json"), []byte(wechatDefaultConfigJSON), 0o600); err != nil {
			t.Fatalf("WriteFile(config.json) error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(addonDir, "credentials.json"), []byte(`{"bot_token":"","user_id":""}`), 0o600); err != nil {
			t.Fatalf("WriteFile(credentials.json) error = %v", err)
		}

		m := NewAddonModel(lingtaiDir)
		if _, ok := m.addonConfigs["wechat"]; ok {
			t.Fatal("expected wechat NOT to be configured with empty bot_token/user_id")
		}
		if errMsg := m.addonErrors["wechat"]; errMsg != "" {
			t.Fatalf("unexpected error for empty bot_token/user_id = %q", errMsg)
		}
	})
}

func indexOfAddon(name string) int {
	for i, addon := range AllAddons {
		if addon == name {
			return i
		}
	}
	return 0
}
