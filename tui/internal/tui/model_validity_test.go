package tui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// writeCodexProbeToken writes a token bundle that is not due for refresh
// (expires_at far in the future), matching what a real, currently-valid
// Codex credential looks like on disk — every real writer (OAuth exchange,
// refreshCodexTokens) always sets expires_at, so a token file with none is
// not a state ensureFreshCodexTokens needs to special-case.
func writeCodexProbeToken(t *testing.T, path, access string) {
	t.Helper()
	writeCodexProbeTokenWithExpiry(t, path, access, "refresh", 4102444800) // 2100-01-01
}

// setCodexTokenURLForTest points codexTokenURL (oauth.go) at a test server
// for the duration of the test and returns a restore func. Keeps
// refreshCodexTokens testable without ever contacting auth.openai.com.
func setCodexTokenURLForTest(url string) (restore func()) {
	prev := codexTokenURL
	codexTokenURL = url
	return func() { codexTokenURL = prev }
}

// writeCodexProbeTokenWithExpiry writes a token bundle with an explicit
// expires_at (unix seconds), simulating an on-disk token that is expired or
// about to expire — the state a valid Codex account is in when the TUI has
// been open past the kernel's normal startup refresh.
func writeCodexProbeTokenWithExpiry(t *testing.T, path, access, refresh string, expiresAt int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"access_token":%q,"refresh_token":%q,"expires_at":%d}`, access, refresh, expiresAt)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCodexModelValidityUsesResponsesAndSelectedAccount(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			t.Fatalf("request = %s %s, want POST /responses", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		if strings.Contains(string(data), `"model":"gpt-test"`) {
			gotModel = "gpt-test"
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"response","output":[]}`))
	}))
	defer srv.Close()

	globalDir := t.TempDir()
	writeCodexProbeToken(t, filepath.Join(globalDir, "codex-auth.json"), "selected-access")
	status, detail := probeCodexModel("codex", "gpt-test", srv.URL, globalDir, "")
	if status != probeOK || detail != "" {
		t.Fatalf("probe = %v, %q; want OK", status, detail)
	}
	if gotAuth != "Bearer selected-access" || gotModel != "gpt-test" {
		t.Fatalf("request auth/model = %q/%q", gotAuth, gotModel)
	}
}

// TestCodexModelValidityRefreshesExpiredAccessTokenBeforeProbing exercises
// the proven false-negative code path (not a claim about what the specific
// reported screenshot's token state actually was): a working ChatGPT-backed
// Codex account whose on-disk access_token has expired. Before this fix,
// readCodexTokenFile had no expiry check — unlike the kernel's
// CodexTokenManager.get_access_token, which refreshes before every use — so
// probeCodexModel sent the stale token straight to /responses, got a 401,
// and reported the account/model as ineligible even though the
// refresh_token is valid and a refreshed access_token is accepted.
func TestCodexModelValidityRefreshesExpiredAccessTokenBeforeProbing(t *testing.T) {
	const freshAccess = "fresh-access-token"
	var tokenCalls, responsesCalls int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse refresh form: %v", err)
		}
		if got := r.FormValue("refresh_token"); got != "stale-refresh" {
			t.Fatalf("refresh_token = %q, want stale-refresh", got)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"stale-refresh","expires_in":3600}`, freshAccess)
	}))
	defer tokenSrv.Close()
	restoreTokenURL := setCodexTokenURLForTest(tokenSrv.URL)
	defer restoreTokenURL()

	responsesSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&responsesCalls, 1)
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+freshAccess {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid token"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"response","output":[]}`))
	}))
	defer responsesSrv.Close()

	globalDir := t.TempDir()
	path := filepath.Join(globalDir, "codex-auth.json")
	writeCodexProbeTokenWithExpiry(t, path, "stale-access", "stale-refresh", 1) // already expired

	status, detail := probeCodexModel("codex", "gpt-5.6-sol", responsesSrv.URL, globalDir, "")
	if status != probeOK || detail != "" {
		t.Fatalf("probe = %v, %q; want OK after refreshing the expired token", status, detail)
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("token refresh calls = %d, want 1", tokenCalls)
	}
	if atomic.LoadInt32(&responsesCalls) != 1 {
		t.Fatalf("responses calls = %d, want 1", responsesCalls)
	}

	refreshed, ok := readCodexTokenFile(path)
	if !ok || refreshed.AccessToken != freshAccess {
		t.Fatalf("on-disk token not updated with refreshed access_token: %+v (ok=%v)", refreshed, ok)
	}
}

// TestCodexModelValidityRevokedRefreshStaysAuthError proves a genuinely
// revoked account (refresh rejected with 401/403) still fails closed as a
// deterministic auth error, not silently passed.
func TestCodexModelValidityRevokedRefreshStaysAuthError(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenSrv.Close()
	restoreTokenURL := setCodexTokenURLForTest(tokenSrv.URL)
	defer restoreTokenURL()

	responsesSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("must not reach the Responses endpoint with a token known to be unrefreshable")
	}))
	defer responsesSrv.Close()

	globalDir := t.TempDir()
	path := filepath.Join(globalDir, "codex-auth.json")
	writeCodexProbeTokenWithExpiry(t, path, "stale-access", "revoked-refresh", 1)

	status, detail := probeCodexModel("codex", "gpt-5.6-sol", responsesSrv.URL, globalDir, "")
	if status != probeAuthError {
		t.Fatalf("probe = %v, %q; want probeAuthError for a revoked refresh grant", status, detail)
	}
}

// TestCodexModelValidityTransientRefreshFailureIsRetryableNotAuthError
// proves the other half of the false-negative fix: when a token is due for
// refresh but the refresh itself fails for a reason unrelated to the grant
// (network error here), the probe must NOT fall back to sending the
// known-stale access token to /responses and let its 401 be misread as
// deterministic ineligibility. It must report an operational, retryable
// outcome (probeOverloaded, which checkCodexModelValidityCmdForSource maps
// to validityRetryable) so Save can proceed with a warning instead of
// blocking on a problem that says nothing about the account or model.
func TestCodexModelValidityTransientRefreshFailureIsRetryableNotAuthError(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a network-adjacent failure: close the connection without
		// a response, which surfaces to refreshCodexTokens as a non-nil,
		// non-HTTP-status error (client.PostForm returns an error), the same
		// class of failure as a DNS/timeout/connection-reset.
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		conn.Close()
	}))
	defer tokenSrv.Close()
	restoreTokenURL := setCodexTokenURLForTest(tokenSrv.URL)
	defer restoreTokenURL()

	responsesSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("must not reach the Responses endpoint with a token known to be stale after a failed refresh")
	}))
	defer responsesSrv.Close()

	globalDir := t.TempDir()
	path := filepath.Join(globalDir, "codex-auth.json")
	writeCodexProbeTokenWithExpiry(t, path, "stale-access", "still-good-refresh", 1)

	status, detail := probeCodexModel("codex", "gpt-5.6-sol", responsesSrv.URL, globalDir, "")
	if status != probeOverloaded {
		t.Fatalf("probe = %v, %q; want probeOverloaded (retryable) for a transient refresh failure, not a hard auth error", status, detail)
	}

	// The on-disk token must be untouched: a transient failure must not
	// overwrite a possibly-still-valid refresh_token with anything.
	unchanged, ok := readCodexTokenFile(path)
	if !ok || unchanged.AccessToken != "stale-access" {
		t.Fatalf("on-disk token was modified by a failed refresh: %+v (ok=%v)", unchanged, ok)
	}
}

func TestCodexPoolModelValidityFailsClosedForIneligibleNonemptyPool(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"model not eligible"}`))
	}))
	defer srv.Close()

	globalDir := t.TempDir()
	writeCodexProbeToken(t, filepath.Join(globalDir, "codex-auth", "member.json"), "pool-access")
	pool := fmt.Sprintf(`{"version":1,"accounts":[{"path":"codex-auth/member.json","weight":1}]}`)
	if err := os.WriteFile(codexPoolPath(globalDir), []byte(pool), 0o600); err != nil {
		t.Fatal(err)
	}
	// A valid legacy token must not become a hidden fallback when the pool is
	// non-empty and its selected member is ineligible.
	writeCodexProbeToken(t, filepath.Join(globalDir, "codex-auth.json"), "legacy-access")

	status, detail := probeCodexModel("codex-pool", "gpt-test", srv.URL, globalDir, "")
	if status != probeAuthError || !strings.Contains(detail, "no eligible Codex pool account") {
		t.Fatalf("probe = %v, %q; want loud ineligible-pool failure", status, detail)
	}
	if calls != 1 {
		t.Fatalf("Responses calls = %d, want exactly the selected pool member", calls)
	}
}

func TestCodexPoolModelValidity_ZeroDisabledAndBlankUseLegacyProbe(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"response","output":[]}`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	writeCodexProbeToken(t, filepath.Join(dir, "codex-auth.json"), "legacy")
	raw := []byte(`{"version":1,"accounts":[{"path":"","weight":1},{"path":"codex-auth/disabled.json","weight":1,"enabled":false},{"path":"codex-auth/zero.json","weight":0}]}`)
	if err := os.WriteFile(codexPoolPath(dir), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	status, detail := probeCodexModel("codex-pool", "gpt-test", srv.URL, dir, "")
	if status != probeOK || detail != "" || calls != 1 {
		t.Fatalf("probe = %v, %q, calls=%d; want legacy fallback", status, detail, calls)
	}
}

func TestCodexModelValidityRequiresSelectedModel(t *testing.T) {
	status, detail := probeCodexModel("codex", "", "", t.TempDir(), "")
	if status != probeUnknown || !strings.Contains(detail, "selected Codex model is missing") {
		t.Fatalf("probe = %v, %q; want explicit missing-model state", status, detail)
	}
}

// newPresetKeyTestInput builds a textarea pre-filled with val, matching
// the shape FirstRunModel.presetKeyInput expects in production.
func newPresetKeyTestInput(val string) textarea.Model {
	ta := textarea.New()
	ta.SetValue(val)
	return ta
}

// testValidityPreset builds a "custom" provider preset pointed at an
// httptest server so probeLLM's real HTTP calls hit a fake, deterministic
// backend instead of a live provider — no live credentials needed.
func testValidityPreset(baseURL string) preset.Preset {
	return preset.Preset{
		Name:        "validity-test",
		Description: preset.PresetDescription{Summary: "A preset used by validity-gate tests"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "custom",
				"model":       "test-model",
				"api_compat":  "anthropic",
				"base_url":    baseURL,
				"api_key_env": "CUSTOM_API_KEY",
			},
		},
	}
}

// anthropicOKServer answers both probeLLM's stage-1 GET /v1/models and
// stage-2 POST /v1/messages with a real-looking, non-empty envelope.
func anthropicOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// anthropicAuthErrorServer answers every request with 401, simulating an
// invalid credential.

// anthropicRateLimitServer proves the provider/model endpoint was reached,
// then returns the retryable plan-credits shape that prompted this behavior.
func anthropicRateLimitServer(t *testing.T, echoedSecret string, messageCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			messageCalls.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprintf(w, `{"error":{"type":"rate_limit_error","message":"Token Plan usage limit reached; purchase Credits (2056); x-api-key=%s"}}`, echoedSecret)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func anthropicAuthErrorServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid x-api-key"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// drainValidityResult pumps m through the tea.Cmd returned by commit()
// (or by startModelValidityCheck) until the resulting modelValidityResultMsg
// has been applied, mirroring what the real Bubble Tea runtime does with
// the returned cmd — commit() itself never blocks on network I/O.
func drainValidityResult(t *testing.T, m PresetEditorModel, cmd tea.Cmd) PresetEditorModel {
	t.Helper()
	if cmd == nil {
		t.Fatalf("expected a pending validity-check cmd, got nil")
	}
	msg := cmd()
	result, ok := msg.(modelValidityResultMsg)
	if !ok {
		t.Fatalf("expected modelValidityResultMsg, got %T", msg)
	}
	updated, _ := m.Update(result)
	return updated
}

func TestPresetEditorCommitBlocksUntilModelValidated(t *testing.T) {
	srv := anthropicOKServer(t)
	p := testValidityPreset(srv.URL)
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m.apiKey = "sk-test"

	// First Save: no check has ever run for this tuple, so commit()
	// starts one and does NOT emit PresetEditorCommitMsg yet.
	updated, cmd := m.commit()
	if updated.saveErr == "" {
		t.Fatalf("expected a pending message while validity check is in flight")
	}
	if updated.modelValidity != validityChecking {
		t.Fatalf("expected validityChecking, got %v", updated.modelValidity)
	}

	updated = drainValidityResult(t, updated, cmd)
	if updated.modelValidity != validityValid {
		t.Fatalf("expected validityValid after a 2xx probe, got %v (%s)", updated.modelValidity, updated.modelValidityDetail)
	}
	if got := updated.modelValidityLine(); got == "" {
		t.Fatalf("expected a non-empty valid status line")
	}

	// Second Save: tuple unchanged, prior check succeeded — commits now.
	final, cmd2 := updated.commit()
	if cmd2 == nil {
		t.Fatalf("expected commit cmd after successful validation")
	}
	msg := cmd2()
	if _, ok := msg.(PresetEditorCommitMsg); !ok {
		t.Fatalf("expected PresetEditorCommitMsg once validated, got %T", msg)
	}
	if final.saveErr != "" {
		t.Fatalf("unexpected saveErr after successful validated commit: %q", final.saveErr)
	}
}

func TestPresetEditorRetryableRateLimitSavesWithWarningAndReprobes(t *testing.T) {
	const apiKey = "sk-test-secret"
	var messageCalls atomic.Int32
	srv := anthropicRateLimitServer(t, apiKey, &messageCalls)
	p := testValidityPreset(srv.URL)
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m.apiKey = apiKey
	checking, cmd := m.commit()
	retryable := drainValidityResult(t, checking, cmd)
	if retryable.modelValidity != validityRetryable {
		t.Fatalf("expected validityRetryable after 429, got %v (%s)", retryable.modelValidity, retryable.modelValidityDetail)
	}
	if !strings.Contains(retryable.modelValidityDetail, "2056") {
		t.Fatalf("expected provider evidence, got %q", retryable.modelValidityDetail)
	}
	if strings.Contains(retryable.modelValidityDetail, apiKey) {
		t.Fatalf("validity detail leaked API key: %q", retryable.modelValidityDetail)
	}
	saved, saveCmd := retryable.commit()
	if saveCmd == nil {
		t.Fatalf("retryable failure should save with warning")
	}
	raw := saveCmd()
	msg, ok := raw.(PresetEditorCommitMsg)
	if !ok {
		t.Fatalf("expected PresetEditorCommitMsg, got %T", raw)
	}
	for _, evidence := range []string{"custom", "test-model", "2056", "Preset saved", "runtime calls may fail"} {
		if !strings.Contains(msg.Warning, evidence) {
			t.Fatalf("warning missing %q: %q", evidence, msg.Warning)
		}
	}
	if strings.Contains(msg.Warning, apiKey) {
		t.Fatalf("warning leaked API key: %q", msg.Warning)
	}
	if saved.modelValidity != validityUnknown {
		t.Fatalf("retryable result must reset after save; got %v", saved.modelValidity)
	}
	rechecking, retryCmd := saved.commit()
	if retryCmd == nil || rechecking.modelValidity != validityChecking {
		t.Fatalf("same-tuple re-save must re-probe; status=%v cmd=%v", rechecking.modelValidity, retryCmd != nil)
	}
	rechecked := drainValidityResult(t, rechecking, retryCmd)
	if rechecked.modelValidity != validityRetryable {
		t.Fatalf("expected fresh retryable result, got %v", rechecked.modelValidity)
	}
	if got := messageCalls.Load(); got != 2 {
		t.Fatalf("expected two real message probes, got %d", got)
	}
}

func TestPresetEditorCommitBlocksOnInvalidModel(t *testing.T) {
	srv := anthropicAuthErrorServer(t)
	p := testValidityPreset(srv.URL)
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m.apiKey = "sk-bad-key"

	updated, cmd := m.commit()
	updated = drainValidityResult(t, updated, cmd)
	if updated.modelValidity != validityInvalid {
		t.Fatalf("expected validityInvalid after a 401 probe, got %v", updated.modelValidity)
	}
	if updated.modelValidityDetail == "" {
		t.Fatalf("expected a non-empty invalid detail")
	}

	// Save must still refuse to commit.
	final, cmd2 := updated.commit()
	if cmd2 != nil {
		if _, ok := cmd2().(PresetEditorCommitMsg); ok {
			t.Fatalf("commit must not succeed while the model is marked invalid")
		}
	}
	if final.saveErr == "" {
		t.Fatalf("expected saveErr to explain why Save is blocked")
	}
}

func TestPresetEditorCommitBlocksWhileChecking(t *testing.T) {
	// Server that never responds within the test's lifetime is
	// unnecessary — we only need commit() to observe modelValidity ==
	// validityChecking (already set by an earlier commit()) and refuse
	// to emit PresetEditorCommitMsg a second time before the result
	// lands.
	srv := anthropicOKServer(t)
	p := testValidityPreset(srv.URL)
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m.apiKey = "sk-test"

	updated, cmd := m.commit() // starts the check; now validityChecking
	if updated.modelValidity != validityChecking {
		t.Fatalf("expected validityChecking immediately after starting a check, got %v", updated.modelValidity)
	}

	// A second Save attempt before the result arrives must also refuse,
	// and must NOT start a duplicate check (same tuple, already checking).
	again, cmd2 := updated.commit()
	if cmd2 != nil {
		if _, ok := cmd2().(PresetEditorCommitMsg); ok {
			t.Fatalf("commit must not succeed while a check is still pending")
		}
	}
	if again.modelValidityGen != updated.modelValidityGen {
		t.Fatalf("a second Save on the same pending tuple must not start a duplicate check")
	}

	_ = drainValidityResult(t, again, cmd)
}

func TestPresetEditorEditingTupleInvalidatesPriorSuccessAndIgnoresStaleResult(t *testing.T) {
	srv := anthropicOKServer(t)
	p := testValidityPreset(srv.URL)
	m := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	m.apiKey = "sk-test"

	updated, cmd := m.commit()
	updated = drainValidityResult(t, updated, cmd)
	if updated.modelValidity != validityValid {
		t.Fatalf("setup: expected validityValid, got %v", updated.modelValidity)
	}

	// Edit the model — the tuple fingerprint changes, so the prior
	// "valid" result must no longer be recognized as covering it.
	llm := updated.llmMap()
	llm["model"] = "a-different-model"

	if updated.currentValidityKey() == updated.modelValidityKey {
		t.Fatalf("editing the model must change the validity fingerprint")
	}
	if line := updated.modelValidityLine(); line != "" {
		t.Fatalf("stale valid status must not render for a changed tuple, got %q", line)
	}

	// Save on the edited tuple must re-check, not silently reuse the
	// earlier "valid" result.
	afterEdit, editCmd := updated.commit()
	if afterEdit.modelValidity != validityChecking {
		t.Fatalf("expected a fresh check after editing the model, got %v", afterEdit.modelValidity)
	}
	staleGen := updated.modelValidityGen // the generation from BEFORE this edit's check
	if afterEdit.modelValidityGen == staleGen {
		t.Fatalf("expected a new generation for the re-check")
	}

	// A late result carrying the OLD generation must be dropped.
	stale := modelValidityResultMsg{Generation: staleGen, Status: validityInvalid, Detail: "stale"}
	afterStale, _ := afterEdit.Update(stale)
	if afterStale.modelValidity != validityChecking {
		t.Fatalf("a stale-generation result must be ignored, got status %v", afterStale.modelValidity)
	}

	// The fresh check's own result still applies normally.
	final := drainValidityResult(t, afterStale, editCmd)
	if final.modelValidity != validityValid {
		t.Fatalf("expected the fresh check's own result to apply, got %v (%s)", final.modelValidity, final.modelValidityDetail)
	}
}

// TestPresetLibrarySharesEditorValidityGate confirms the standalone
// /presets flow (PresetLibraryModel) inherits the same real-availability
// gate as the first-run wizard, since both host the same
// PresetEditorModel and only PresetEditorModel.commit() decides when
// PresetEditorCommitMsg fires — see PresetEditorModel.commit's doc
// comment and firstrun.go's stepEditPreset case for the wizard side of
// this same-code-path guarantee.
func TestPresetLibrarySharesEditorValidityGate(t *testing.T) {
	srv := anthropicOKServer(t)
	p := testValidityPreset(srv.URL)
	editor := NewPresetEditorModelWithBuiltinFlag(p, "en", nil, "", false)
	editor.apiKey = "sk-test"

	m := PresetLibraryModel{
		focus:  presetLibFocusEditor,
		editor: editor,
		lang:   "en",
	}

	// Ctrl+S while unvalidated must not emit PresetEditorCommitMsg — the
	// library's Update forwards it into m.editor.Update, which must
	// refuse exactly like the wizard's stepEditPreset does.
	m, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatalf("expected the pending validity-check cmd")
	}
	msg := cmd()
	if _, ok := msg.(PresetEditorCommitMsg); ok {
		t.Fatalf("preset library must not save before the model is validated")
	}
	result, ok := msg.(modelValidityResultMsg)
	if !ok {
		t.Fatalf("expected modelValidityResultMsg, got %T", msg)
	}

	// Deliver the result the same way the real program would: the
	// library is in presetLibFocusEditor, so Update's default branch
	// forwards it straight into m.editor.
	m, _ = m.Update(result)
	if m.editor.modelValidity != validityValid {
		t.Fatalf("expected the embedded editor to record validityValid, got %v", m.editor.modelValidity)
	}

	// Ctrl+S now succeeds and the library handles the commit (saves,
	// returns focus to the list).
	m, cmd = m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatalf("expected a commit cmd once validated")
	}
	if _, ok := cmd().(PresetEditorCommitMsg); !ok {
		t.Fatalf("expected PresetEditorCommitMsg once the model is validated")
	}
}

func TestPresetKeyNext_BlocksUntilModelValidated(t *testing.T) {
	srv := anthropicOKServer(t)
	dir := t.TempDir()
	keyInput := newPresetKeyTestInput("sk-test")
	m := FirstRunModel{
		step:           stepPresetKey,
		globalDir:      dir,
		existingKeys:   map[string]string{},
		keyFieldIdx:    2, // Next button
		cursor:         0,
		nameInput:      textinput.New(),
		dirInput:       textinput.New(),
		ctxLimitInput:  textinput.New(),
		soulDelayInput: textinput.New(),
		maxRpmInput:    textinput.New(),
		maxAedInput:    textinput.New(),
		covenantInput:  textinput.New(),
		soulFlowInput:  textinput.New(),
		commentInput:   textinput.New(),
		presets: []preset.Preset{
			{
				Name: "custom-test",
				Manifest: map[string]interface{}{
					"llm": map[string]interface{}{
						"provider":    "custom",
						"model":       "test-model",
						"api_compat":  "anthropic",
						"base_url":    srv.URL,
						"api_key_env": "CUSTOM_API_KEY",
					},
				},
			},
		},
		presetKeyInput: keyInput,
	}

	// First Enter: no check has run for this tuple yet. Must NOT advance.
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.step != stepPresetKey {
		t.Fatalf("must not advance past stepPresetKey before validation completes; step=%v", m.step)
	}
	if m.presetKeyValidity != validityChecking {
		t.Fatalf("expected validityChecking, got %v", m.presetKeyValidity)
	}
	if cmd == nil {
		t.Fatalf("expected a pending validity-check cmd")
	}

	// Deliver the async result.
	msg := cmd()
	result, ok := msg.(modelValidityResultMsg)
	if !ok {
		t.Fatalf("expected modelValidityResultMsg, got %T", msg)
	}
	m, _ = m.Update(result)
	if m.presetKeyValidity != validityValid {
		t.Fatalf("expected validityValid, got %v (%s)", m.presetKeyValidity, m.presetKeyValidityDetail)
	}

	// Second Enter: tuple unchanged, check already succeeded — advances.
	m, cmd2 := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd2 == nil {
		t.Fatalf("expected enterCapabilities' cmd once validated")
	}
	if m.step == stepPresetKey {
		t.Fatalf("expected the wizard to advance past stepPresetKey once validated")
	}
}

func TestFirstRunValidityStreamsDoNotCrossTalk(t *testing.T) {
	m := FirstRunModel{step: stepAgentPresets, presetCfgValidityGen: 1, presetCfgValidity: validityChecking, presetKeyValidityGen: 1, presetKeyValidity: validityChecking}
	keyResult := modelValidityResultMsg{Generation: 1, Source: "preset-key", Status: validityValid}
	m, _ = m.Update(keyResult)
	if m.presetCfgValidity != validityChecking {
		t.Fatalf("key-stream result satisfied config stream: %v", m.presetCfgValidity)
	}
	if m.presetKeyValidity != validityChecking {
		t.Fatalf("key-stream result should not route while config step is active")
	}
	configInvalid := modelValidityResultMsg{Generation: 1, Source: "codex-config", Status: validityInvalid, Detail: "probe failed"}
	m, _ = m.Update(configInvalid)
	if m.presetCfgValidity != validityInvalid || m.presetCfgMessage != "probe failed" {
		t.Fatalf("matching config result not applied: status=%v message=%q", m.presetCfgValidity, m.presetCfgMessage)
	}
	configValid := modelValidityResultMsg{Generation: 1, Source: "codex-config", Status: validityValid}
	m, _ = m.Update(configValid)
	if m.presetCfgValidity != validityValid {
		t.Fatalf("matching valid result not applied: %v", m.presetCfgValidity)
	}
}

func TestFirstRunCodexConfigCheckingBlocksNext(t *testing.T) {
	m := FirstRunModel{
		step:              stepAgentPresets,
		presets:           []preset.Preset{{Manifest: map[string]interface{}{"llm": map[string]interface{}{"provider": "codex", "model": "gpt-test"}}}},
		savedPresetIdx:    []int{0},
		presetAllowed:     []bool{true},
		presetDefaultIdx:  0,
		presetCfgCursor:   2, // row 0, Back 1, Next 2
		presetCfgValidity: validityChecking,
		cursor:            0,
	}
	m.presetCfgValidityKey = m.presetCfgValidityKeyFor(m.presets[0])
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || updated.step != stepAgentPresets {
		t.Fatalf("checking Codex config must keep Next blocked: step=%v cmd=%v", updated.step, cmd != nil)
	}
}

func TestPresetKeyNext_InvalidModelBlocksAdvance(t *testing.T) {
	srv := anthropicAuthErrorServer(t)
	dir := t.TempDir()
	keyInput := newPresetKeyTestInput("sk-bad")
	m := FirstRunModel{
		step:         stepPresetKey,
		globalDir:    dir,
		existingKeys: map[string]string{},
		keyFieldIdx:  2,
		cursor:       0,
		presets: []preset.Preset{
			{
				Name: "custom-test",
				Manifest: map[string]interface{}{
					"llm": map[string]interface{}{
						"provider":    "custom",
						"model":       "test-model",
						"api_compat":  "anthropic",
						"base_url":    srv.URL,
						"api_key_env": "CUSTOM_API_KEY",
					},
				},
			},
		},
		presetKeyInput: keyInput,
	}

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := cmd().(modelValidityResultMsg)
	m, _ = m.Update(msg)
	if m.presetKeyValidity != validityInvalid {
		t.Fatalf("expected validityInvalid after a 401 probe, got %v", m.presetKeyValidity)
	}

	m, cmd2 := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.step != stepPresetKey {
		t.Fatalf("an invalid model must keep the wizard on stepPresetKey; got step %v", m.step)
	}
	if cmd2 != nil {
		if _, ok := cmd2().(modelValidityResultMsg); !ok {
			t.Fatalf("re-pressing Next on an invalid tuple must not dispatch capabilities")
		}
	}
	if m.message == "" {
		t.Fatalf("expected a visible error message explaining the block")
	}
}

func TestCheckModelValidityCmdClaudeCodeUsesOAuth(t *testing.T) {
	msg := checkModelValidityCmd(17, "claude-code", "fable", "", "", "")()
	got, ok := msg.(modelValidityResultMsg)
	if !ok {
		t.Fatalf("message type = %T, want modelValidityResultMsg", msg)
	}
	if got.Generation != 17 || got.Status != validityValid {
		t.Fatalf("result = %#v, want generation 17 and validityValid", got)
	}
}
