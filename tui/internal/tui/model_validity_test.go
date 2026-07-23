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

func writeCodexProbeToken(t *testing.T, path, access string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"access_token":%q,"refresh_token":"refresh"}`, access)), 0o600); err != nil {
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

// codexPoolProbeServer records the Authorization header of every Responses
// call and answers with the per-bearer status code (200 with a valid envelope
// when unlisted), so pool preflight tests can assert exactly which accounts
// were probed, and in what order, against a purely local fake backend.
func codexPoolProbeServer(t *testing.T, statusByBearer map[string]int) (*httptest.Server, *[]string) {
	t.Helper()
	calls := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		*calls = append(*calls, auth)
		code, listed := statusByBearer[auth]
		if !listed {
			code = http.StatusOK
		}
		w.WriteHeader(code)
		if code == http.StatusOK {
			_, _ = w.Write([]byte(`{"object":"response","output":[]}`))
		} else {
			_, _ = w.Write([]byte(`{"error":"probe fixture failure"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

// seedTwoAccountCodexPool writes a two-account flat pool plus a valid legacy
// token that must never be probed while the pool is non-empty. The fake token
// values are secret-shaped so redaction assertions are meaningful.
func seedTwoAccountCodexPool(t *testing.T, dir string) {
	t.Helper()
	writeCodexProbeToken(t, filepath.Join(dir, "codex-auth", "first.json"), "first-access-secret")
	writeCodexProbeToken(t, filepath.Join(dir, "codex-auth", "second.json"), "second-access-secret")
	writeCodexProbeToken(t, filepath.Join(dir, "codex-auth.json"), "legacy-access-secret")
	pool := `{"version":1,"accounts":[{"path":"codex-auth/first.json","weight":1},{"path":"codex-auth/second.json","weight":1}]}`
	if err := os.WriteFile(codexPoolPath(dir), []byte(pool), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCodexPoolModelValidityChecksEveryAccount is the #706 false-green
// regression: the runtime weighted-selects among ALL pool accounts, so one
// passing account must not certify a pool whose later account cannot serve
// the model. Both accounts must be probed and the result must be the
// deterministic failure, with no hidden legacy fallback request.
func TestCodexPoolModelValidityChecksEveryAccount(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	seedTwoAccountCodexPool(t, dir)
	srv, calls := codexPoolProbeServer(t, map[string]int{
		"Bearer second-access-secret": http.StatusForbidden,
	})

	status, detail := probeCodexModel("codex-pool", "gpt-test", srv.URL, dir, "")
	if status != probeAuthError || !strings.Contains(detail, "no eligible Codex pool account") {
		t.Fatalf("probe = %v, %q; want deterministic ineligible-pool failure", status, detail)
	}
	if len(*calls) != 2 || (*calls)[0] != "Bearer first-access-secret" || (*calls)[1] != "Bearer second-access-secret" {
		t.Fatalf("calls = %v; want both pool accounts probed in file order and no legacy fallback", *calls)
	}
}

// TestCodexPoolModelValidityFailFastSkipsLaterAccounts pins the fail-fast
// half of the invariant: once the first account fails deterministically the
// preflight stops — later accounts (which would succeed) are not probed and
// cannot rescue the pool.
func TestCodexPoolModelValidityFailFastSkipsLaterAccounts(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	seedTwoAccountCodexPool(t, dir)
	srv, calls := codexPoolProbeServer(t, map[string]int{
		"Bearer first-access-secret": http.StatusForbidden,
	})

	status, detail := probeCodexModel("codex-pool", "gpt-test", srv.URL, dir, "")
	if status != probeAuthError || !strings.Contains(detail, "no eligible Codex pool account") {
		t.Fatalf("probe = %v, %q; want deterministic ineligible-pool failure", status, detail)
	}
	if len(*calls) != 1 || (*calls)[0] != "Bearer first-access-secret" {
		t.Fatalf("calls = %v; want exactly the first (failing) account and nothing after it", *calls)
	}
}

// TestCodexPoolModelValidityAllAccountsPassIsGreen proves probeOK requires the
// loop to reach the end: every account is probed exactly once and only then is
// the pool certified.
func TestCodexPoolModelValidityAllAccountsPassIsGreen(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	seedTwoAccountCodexPool(t, dir)
	srv, calls := codexPoolProbeServer(t, nil)

	status, detail := probeCodexModel("codex-pool", "gpt-test", srv.URL, dir, "")
	if status != probeOK || detail != "" {
		t.Fatalf("probe = %v, %q; want probeOK for an all-pass pool", status, detail)
	}
	if len(*calls) != 2 || (*calls)[0] != "Bearer first-access-secret" || (*calls)[1] != "Bearer second-access-secret" {
		t.Fatalf("calls = %v; want exactly one probe per pool account, in file order", *calls)
	}
}

// TestCodexPoolModelValidityTransientFailureIsNotGreen proves a transient
// non-OK result on a later account keeps the pool non-green under the
// existing status taxonomy (429 stays retryable, 5xx stays overloaded).
func TestCodexPoolModelValidityTransientFailureIsNotGreen(t *testing.T) {
	cases := []struct {
		name string
		code int
		want probeStatus
	}{
		{"rate-limited", http.StatusTooManyRequests, probeRateLimit},
		{"overloaded", http.StatusInternalServerError, probeOverloaded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LINGTAI_TUI_DIR", "")
			dir := t.TempDir()
			seedTwoAccountCodexPool(t, dir)
			srv, calls := codexPoolProbeServer(t, map[string]int{
				"Bearer second-access-secret": tc.code,
			})

			status, detail := probeCodexModel("codex-pool", "gpt-test", srv.URL, dir, "")
			if status != tc.want || !strings.Contains(detail, "no eligible Codex pool account") {
				t.Fatalf("probe = %v, %q; want transient %v and no green", status, detail, tc.want)
			}
			if len(*calls) != 2 {
				t.Fatalf("calls = %v; want the passing account then the transient one", *calls)
			}
		})
	}
}

// TestCodexPoolModelValidityMissingTokenFailsWithoutRequest pins the local
// deterministic failure: an unreadable/missing token file fails the preflight
// immediately, costs no HTTP request for that entry or any later entry, and
// never falls back to the valid legacy token.
func TestCodexPoolModelValidityMissingTokenFailsWithoutRequest(t *testing.T) {
	cases := []struct {
		name      string
		pool      string
		wantCalls []string
	}{
		{
			name:      "first-account-missing",
			pool:      `{"version":1,"accounts":[{"path":"codex-auth/absent.json","weight":1},{"path":"codex-auth/first.json","weight":1}]}`,
			wantCalls: nil,
		},
		{
			name:      "later-account-missing",
			pool:      `{"version":1,"accounts":[{"path":"codex-auth/first.json","weight":1},{"path":"codex-auth/absent.json","weight":1}]}`,
			wantCalls: []string{"Bearer first-access-secret"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LINGTAI_TUI_DIR", "")
			dir := t.TempDir()
			writeCodexProbeToken(t, filepath.Join(dir, "codex-auth", "first.json"), "first-access-secret")
			writeCodexProbeToken(t, filepath.Join(dir, "codex-auth.json"), "legacy-access-secret")
			if err := os.WriteFile(codexPoolPath(dir), []byte(tc.pool), 0o644); err != nil {
				t.Fatal(err)
			}
			srv, calls := codexPoolProbeServer(t, nil)

			status, detail := probeCodexModel("codex-pool", "gpt-test", srv.URL, dir, "")
			if status != probeAuthError || !strings.Contains(detail, "no eligible Codex pool account") {
				t.Fatalf("probe = %v, %q; want deterministic missing-credential failure", status, detail)
			}
			if len(*calls) != len(tc.wantCalls) {
				t.Fatalf("calls = %v, want %v (no request for the missing entry or anything after it)", *calls, tc.wantCalls)
			}
			for i, want := range tc.wantCalls {
				if (*calls)[i] != want {
					t.Fatalf("calls = %v, want %v", *calls, tc.wantCalls)
				}
			}
		})
	}
}

// TestCodexPoolModelValidityDetailOmitsSecretsAndPaths guards the display
// boundary: a pool preflight failure detail may name the model but must never
// carry token material, absolute paths, or pool refs.
func TestCodexPoolModelValidityDetailOmitsSecretsAndPaths(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	seedTwoAccountCodexPool(t, dir)
	srv, _ := codexPoolProbeServer(t, map[string]int{
		"Bearer second-access-secret": http.StatusForbidden,
	})

	_, detail := probeCodexModel("codex-pool", "gpt-test", srv.URL, dir, "")
	for _, banned := range []string{
		"first-access-secret", "second-access-secret", "legacy-access-secret",
		dir, "codex-auth/first.json", "codex-auth/second.json",
	} {
		if strings.Contains(detail, banned) {
			t.Fatalf("detail leaked %q: %q", banned, detail)
		}
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
