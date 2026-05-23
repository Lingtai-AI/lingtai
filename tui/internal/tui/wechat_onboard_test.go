package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/i18n"
)

func TestNormalizeWechatBaseURL(t *testing.T) {
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

func TestRandomWechatUIN(t *testing.T) {
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

func TestCallWechatQRUsesHeadersAndParsesResponse(t *testing.T) {
	oldTransport := wechatHTTPClient.Transport
	t.Cleanup(func() {
		wechatHTTPClient.Transport = oldTransport
	})

	wechatHTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
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
	})

	resp, err := callWechatQR(wechatBaseURL)
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

func TestCallWechatPollParsesResponse(t *testing.T) {
	oldTransport := wechatHTTPClient.Transport
	t.Cleanup(func() {
		wechatHTTPClient.Transport = oldTransport
	})

	wechatHTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.String(); got != wechatBaseURL+wechatGetQRCodeStatusPath+"?qrcode=token-1" {
			t.Fatalf("URL = %q", got)
		}
		return jsonHTTPResponse(`{"status":"confirmed","bot_token":"bot-123","ilink_user_id":"wxid_1","baseurl":"https://redir.weixin.qq.com"}`), nil
	})

	resp, err := callWechatPoll("token-1", wechatBaseURL)
	if err != nil {
		t.Fatalf("callWechatPoll() error = %v", err)
	}
	if resp.Status != "confirmed" || resp.BotToken != "bot-123" || resp.ILinkUserID != "wxid_1" {
		t.Fatalf("unexpected poll response: %+v", resp)
	}
}

func TestWechatRunPollRedirectAndConfirm(t *testing.T) {
	oldTransport := wechatHTTPClient.Transport
	t.Cleanup(func() {
		wechatHTTPClient.Transport = oldTransport
	})

	callCount := 0
	wechatHTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
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
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := WechatOnboardModel{
		ctx:      ctx,
		qrCode:   "token-1",
		interval: 0,
		expireIn: 5,
		baseURL:  wechatBaseURL,
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
	oldTransport := wechatHTTPClient.Transport
	t.Cleanup(func() {
		wechatHTTPClient.Transport = oldTransport
	})

	wechatHTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(`{"status":"expired"}`), nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := WechatOnboardModel{
		ctx:      ctx,
		qrCode:   "token-1",
		interval: 0,
		expireIn: 5,
		baseURL:  wechatBaseURL,
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

func TestWechatSaveConfig(t *testing.T) {
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
	credentialsData, err := os.ReadFile(credentialsPath)
	if err != nil {
		t.Fatalf("ReadFile(credentials.json) error = %v", err)
	}

	var creds wechatCredentials
	if err := json.Unmarshal(credentialsData, &creds); err != nil {
		t.Fatalf("credentials.json unmarshal error = %v", err)
	}
	if creds.BotToken != "bot-123" || creds.UserID != "wxid_1" || creds.BaseURL != "https://redirect.weixin.qq.com" {
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

func TestAddonUpdateRefreshesOnWechatDone(t *testing.T) {
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

func TestAppUpdateForwardsWechatOnboardDoneMsg(t *testing.T) {
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
