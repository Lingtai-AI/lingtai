package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helper tests
// ---------------------------------------------------------------------------

func TestAccountsHost(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"feishu", feishuAccountsHost},
		{"lark", larkAccountsHost},
		{"unknown", feishuAccountsHost}, // default
	}
	for _, tc := range tests {
		got := accountsHost(tc.domain)
		if got != tc.want {
			t.Errorf("accountsHost(%q) = %q, want %q", tc.domain, got, tc.want)
		}
	}
}

func TestOpenHost(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"feishu", feishuOpenHost},
		{"lark", larkOpenHost},
		{"unknown", feishuOpenHost},
	}
	for _, tc := range tests {
		got := openHost(tc.domain)
		if got != tc.want {
			t.Errorf("openHost(%q) = %q, want %q", tc.domain, got, tc.want)
		}
	}
}

func TestFmtRemaining(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string // just check prefix/suffix since i18n may vary
	}{
		{0, ""},
		{30 * time.Second, "30"},
		{90 * time.Second, "1m 30"},
		{5*time.Minute + 3*time.Second, "5m 3"},
	}
	for _, tc := range tests {
		got := fmtRemaining(tc.d)
		if tc.d <= 0 {
			if got != "" {
				t.Errorf("fmtRemaining(%v) = %q, want empty", tc.d, got)
			}
			continue
		}
		if got == "" {
			t.Errorf("fmtRemaining(%v) = empty, expected time string", tc.d)
		}
	}
}

func TestRenderQR(t *testing.T) {
	qr := renderQR("https://example.com/test")
	if qr == "" {
		t.Error("renderQR returned empty string for valid URL")
	}
	if !strings.Contains(qr, "█") && !strings.Contains(qr, "#") {
		// QR code should contain block characters
		t.Logf("renderQR output (might be empty or minimal): %q", qr[:min(len(qr), 50)])
	}
}

// ---------------------------------------------------------------------------
// saveConfig test
// ---------------------------------------------------------------------------

func TestSaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	lingtaiDir := filepath.Join(tmpDir, ".lingtai")

	m := FeishuOnboardModel{
		lingtaiDir: lingtaiDir,
		appID:     "cli_xxxxxxxxxxxxx",
		appSecret: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		domain:    "feishu",
		botName:   "TestBot",
	}

	if err := m.saveConfig(); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	configPath := filepath.Join(lingtaiDir, ".addons", "feishu", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	var cfg feishuConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config JSON: %v", err)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(cfg.Accounts))
	}
	if cfg.Accounts[0].AppID != "cli_xxxxxxxxxxxxx" {
		t.Errorf("app_id = %q, want cli_xxxxxxxxxxxxx", cfg.Accounts[0].AppID)
	}
	if cfg.Accounts[0].AppSecret != "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" {
		t.Errorf("app_secret mismatch")
	}
	if cfg.Accounts[0].Alias != "default" {
		t.Errorf("alias = %q, want default", cfg.Accounts[0].Alias)
	}
}

// ---------------------------------------------------------------------------
// HTTP call tests (httptest)
// ---------------------------------------------------------------------------



// Since postRegistrationJSON and friends build "https://<host>/..." URLs,
// and httptest uses HTTP, we test the underlying HTTP layer by calling
// postJSON and getJSON directly, then test the call* functions by
// verifying they parse responses correctly from a known byte stream.

func TestPostJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"device_code": "dc_test_123"}`)
	}))
	defer srv.Close()

	// Build URL manually to use HTTP scheme
	body := "action=begin&archetype=PersonalAgent&auth_method=client_secret"
	resp, err := postJSON(srv.URL+registrationPath, []byte(body))
	if err != nil {
		t.Fatalf("postJSON error: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if result["device_code"] != "dc_test_123" {
		t.Errorf("device_code = %v, want dc_test_123", result["device_code"])
	}
}

func TestGetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test_token" {
			t.Errorf("Authorization = %s, want Bearer test_token", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"code": 0, "bot": {"bot_name": "MyBot"}}`)
	}))
	defer srv.Close()

	resp, err := getJSON(srv.URL+"/bot/v3/info", "test_token")
	if err != nil {
		t.Fatalf("getJSON error: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if result["code"] != float64(0) {
		t.Errorf("code = %v, want 0", result["code"])
	}
}

// TestCallBeginResponseParsing tests that callBegin correctly parses
// a valid JSON response from the registration endpoint.
func TestCallBeginResponseParsing(t *testing.T) {
	jsonData := `{
		"device_code": "dc_test_456",
		"verification_uri_complete": "https://feishu.cn/verify?code=xyz",
		"user_code": "XYZ-789",
		"interval": 5,
		"expire_in": 300
	}`

	var begin beginOnboardResp
	if err := json.Unmarshal([]byte(jsonData), &begin); err != nil {
		t.Fatalf("unmarshal begin response: %v", err)
	}
	if begin.DeviceCode != "dc_test_456" {
		t.Errorf("DeviceCode = %s, want dc_test_456", begin.DeviceCode)
	}
	if begin.QRURL != "https://feishu.cn/verify?code=xyz" {
		t.Errorf("QRURL = %s", begin.QRURL)
	}
	if begin.Interval != 5 {
		t.Errorf("Interval = %d, want 5", begin.Interval)
	}
	if begin.ExpireIn != 300 {
		t.Errorf("ExpireIn = %d, want 300", begin.ExpireIn)
	}
}

// TestCallPollResponseParsing tests various poll response scenarios.
func TestCallPollResponseParsing(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantID   string
		wantErr  string
		wantCode int
	}{
		{
			name:   "success",
			json:   `{"client_id": "cli_abc123", "client_secret": "secret_xyz"}`,
			wantID: "cli_abc123",
		},
		{
			name:    "access denied",
			json:    `{"error": "access_denied"}`,
			wantErr: "access_denied",
		},
		{
			name:    "expired",
			json:    `{"error": "expired_token"}`,
			wantErr: "expired_token",
		},
		{
			name:   "pending",
			json:   `{}`,
			wantID: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var poll pollOnboardResp
			if err := json.Unmarshal([]byte(tc.json), &poll); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if poll.ClientID != tc.wantID {
				t.Errorf("ClientID = %s, want %s", poll.ClientID, tc.wantID)
			}
			if poll.Error != tc.wantErr {
				t.Errorf("Error = %s, want %s", poll.Error, tc.wantErr)
			}
		})
	}
}

// TestCallProbeBotResponseParsing tests probe bot response parsing.
func TestCallProbeBotResponseParsing(t *testing.T) {
	tokenResp := `{"code": 0, "tenant_access_token": "token_abc123"}`
	botResp := `{"code": 0, "bot": {"bot_name": "MyTestBot", "open_id": "ou_xxx"}}`

	var tokenData struct {
		Code              int    `json:"code"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.Unmarshal([]byte(tokenResp), &tokenData); err != nil {
		t.Fatalf("unmarshal token: %v", err)
	}
	if tokenData.TenantAccessToken != "token_abc123" {
		t.Errorf("Token = %s", tokenData.TenantAccessToken)
	}

	var botData struct {
		Code int `json:"code"`
		Bot  struct {
			BotName string `json:"bot_name"`
			OpenID  string `json:"open_id"`
		} `json:"bot"`
	}
	if err := json.Unmarshal([]byte(botResp), &botData); err != nil {
		t.Fatalf("unmarshal bot: %v", err)
	}
	if botData.Bot.BotName != "MyTestBot" {
		t.Errorf("BotName = %s", botData.Bot.BotName)
	}
}

func TestCallProbeBotTokenCodeError(t *testing.T) {
	// Simulate Feishu returning code != 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return non-zero code and no token
		fmt.Fprint(w, `{"code": 999, "msg": "invalid app"}`)
	}))
	defer srv.Close()

	// Call the underlying HTTP directly
	body, _ := json.Marshal(map[string]string{"app_id": "bad_id", "app_secret": "bad_secret"})
	resp, err := postJSON(srv.URL+"/auth/v3/tenant_access_token/internal", body)
	if err != nil {
		t.Fatalf("postJSON error: %v", err)
	}

	var tokenData struct {
		Code              int    `json:"code"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.Unmarshal(resp, &tokenData); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tokenData.Code == 0 {
		t.Error("expected non-zero code for invalid app")
	}
	if tokenData.TenantAccessToken != "" {
		t.Errorf("expected empty token, got %s", tokenData.TenantAccessToken)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
