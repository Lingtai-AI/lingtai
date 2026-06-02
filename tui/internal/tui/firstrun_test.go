package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

func TestGetPresetProvider(t *testing.T) {
	m := FirstRunModel{}

	tests := []struct {
		name     string
		preset   preset.Preset
		wantProv string
	}{
		{
			name: "minimax preset",
			preset: preset.Preset{
				Name: "minimax",
				Manifest: map[string]interface{}{
					"llm": map[string]interface{}{"provider": "minimax"},
				},
			},
			wantProv: "minimax",
		},
		{
			name: "custom preset",
			preset: preset.Preset{
				Name: "custom",
				Manifest: map[string]interface{}{
					"llm": map[string]interface{}{"provider": "custom"},
				},
			},
			wantProv: "custom",
		},
		{
			name: "missing llm, defaults to minimax",
			preset: preset.Preset{
				Name:     "empty",
				Manifest: map[string]interface{}{},
			},
			wantProv: "minimax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.getPresetProvider(tt.preset)
			if got != tt.wantProv {
				t.Errorf("getPresetProvider() = %q, want %q", got, tt.wantProv)
			}
		})
	}
}

func TestPresetNeedsKey(t *testing.T) {
	m := FirstRunModel{
		// Keys are env-var-keyed now (the field on the preset declares
		// which env var holds its key — see manifest.llm.api_key_env).
		existingKeys: map[string]string{
			"MINIMAX_API_KEY": "my-minimax-key",
		},
	}

	minimaxPreset := preset.Preset{
		Name: "minimax",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "minimax",
				"api_key_env": "MINIMAX_API_KEY",
			},
		},
	}
	customPreset := preset.Preset{
		Name: "custom",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "custom",
				"api_key_env": "LLM_API_KEY",
			},
		},
	}
	// A preset with no api_key_env (e.g. codex OAuth) doesn't need a key.
	codexOAuthPreset := preset.Preset{
		Name: "codex_oauth",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{"provider": "codex"},
		},
	}

	if m.presetNeedsKey(minimaxPreset) {
		t.Error("minimax preset should not need key (MINIMAX_API_KEY is set)")
	}
	if !m.presetNeedsKey(customPreset) {
		t.Error("custom preset should need key (LLM_API_KEY is unset)")
	}
	if m.presetNeedsKey(codexOAuthPreset) {
		t.Error("codex OAuth preset has no api_key_env — should not need key")
	}
}

func TestPresetNeedsKey_distinctEnvVars(t *testing.T) {
	// Two minimax presets with different env vars: one configured,
	// one not. The provider doesn't determine the lookup — the preset's
	// own api_key_env field does.
	m := FirstRunModel{
		existingKeys: map[string]string{
			"MINIMAX_PERSONAL_KEY": "personal-key",
		},
	}
	personal := preset.Preset{
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "minimax",
				"api_key_env": "MINIMAX_PERSONAL_KEY",
			},
		},
	}
	work := preset.Preset{
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "minimax",
				"api_key_env": "MINIMAX_WORK_KEY",
			},
		},
	}
	if m.presetNeedsKey(personal) {
		t.Error("personal preset has key, should not need")
	}
	if !m.presetNeedsKey(work) {
		t.Error("work preset uses a distinct env var that's unset, should need")
	}
}

// writeCodexAuth seeds a codex-auth.json file in dir with a stub token
// bundle. Used by tests that exercise the "valid credential" branches.
func writeCodexAuth(t *testing.T, dir string) string {
	t.Helper()
	tok := CodexTokens{
		AccessToken:  "stub-access",
		RefreshToken: "stub-refresh",
		ExpiresAt:    9999999999,
		Email:        "stub@example.com",
	}
	data, err := json.Marshal(tok)
	if err != nil {
		t.Fatalf("marshal stub tokens: %v", err)
	}
	authPath := filepath.Join(dir, "codex-auth.json")
	if err := os.WriteFile(authPath, data, 0o600); err != nil {
		t.Fatalf("write stub codex-auth.json: %v", err)
	}
	return authPath
}

// TestPickPreset_DelLogoutTwoPressClearsCredential verifies the two-press
// Del-logout gate on the Codex 凭据 row: first press arms; second press
// deletes codex-auth.json and clears the in-memory authed state.
func TestPickPreset_DelLogoutTwoPressClearsCredential(t *testing.T) {
	dir := t.TempDir()
	authPath := writeCodexAuth(t, dir)

	m := FirstRunModel{
		step:      stepPickPreset,
		globalDir: dir,
		// No saved presets; cursor parks on the Codex row directly.
		// visiblePresetCount() == 0, so pickCodexAuthIdx == 0.
		cursor: 0,
	}
	m.refreshCodexAuth()
	if !m.codexAuth.valid {
		t.Fatal("precondition: seeded credential should read as valid")
	}

	// First Del — arms only; file must still exist.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if !m.codexLogoutArmed {
		t.Fatal("first Del should arm codexLogoutArmed")
	}
	if _, err := os.Stat(authPath); err != nil {
		t.Fatalf("first Del must not delete the file: %v", err)
	}

	// Second Del — actually deletes.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.codexLogoutArmed {
		t.Error("logout arm should be cleared after second Del")
	}
	if m.codexAuth.valid {
		t.Error("codexAuth.valid should be false after logout")
	}
	if _, err := os.Stat(authPath); !os.IsNotExist(err) {
		t.Errorf("codex-auth.json should be removed; stat err: %v", err)
	}
}

// TestPickPreset_DelDisarmedByOtherKey verifies that any non-Del key
// disarms the logout-confirm gate so an accidental armed state doesn't
// stick around.
func TestPickPreset_DelDisarmedByOtherKey(t *testing.T) {
	dir := t.TempDir()
	authPath := writeCodexAuth(t, dir)

	m := FirstRunModel{
		step:      stepPickPreset,
		globalDir: dir,
		cursor:    0, // Codex row
	}
	m.refreshCodexAuth()

	// Arm.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if !m.codexLogoutArmed {
		t.Fatal("expected arm after first Del")
	}
	// Press Down — should disarm without deleting.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.codexLogoutArmed {
		t.Error("Down should disarm the logout confirmation")
	}
	if _, err := os.Stat(authPath); err != nil {
		t.Errorf("credential must survive a disarm cycle: %v", err)
	}
}

// TestPickPreset_LateOAuthDoneIgnoredAfterCancel verifies the epoch
// guard in the CodexOAuthDoneMsg handler. If a goroutine delivers
// tokens AFTER the user cancelled, the handler must drop them rather
// than overwrite codex-auth.json.
func TestPickPreset_LateOAuthDoneIgnoredAfterCancel(t *testing.T) {
	dir := t.TempDir()
	m := FirstRunModel{
		step:      stepPickPreset,
		globalDir: dir,
	}
	// Simulate "we started one OAuth flow, then cancelled it" by
	// bumping the epoch twice. The stale msg carries epoch=1; the
	// model is now at epoch=2.
	m.codexLoginEpoch = 2

	stale := CodexOAuthDoneMsg{
		Epoch: 1,
		Tokens: &CodexTokens{
			AccessToken:  "leaked",
			RefreshToken: "leaked-refresh",
			Email:        "leak@example.com",
		},
	}
	m, _ = m.Update(stale)

	authPath := filepath.Join(dir, "codex-auth.json")
	if _, err := os.Stat(authPath); !os.IsNotExist(err) {
		t.Errorf("stale OAuth callback must NOT write codex-auth.json; stat err: %v", err)
	}
	if m.codexAuth.valid {
		t.Error("stale callback must not flip codexAuth.valid")
	}
}

// TestPickPreset_OAuthDoneWritesOnMatchingEpoch is the positive control
// for the epoch guard: a current-epoch message is honoured and writes
// the file.
func TestPickPreset_OAuthDoneWritesOnMatchingEpoch(t *testing.T) {
	dir := t.TempDir()
	m := FirstRunModel{
		step:            stepPickPreset,
		globalDir:       dir,
		codexLoggingIn:  true,
		codexLoginEpoch: 5,
	}
	msg := CodexOAuthDoneMsg{
		Epoch: 5,
		Tokens: &CodexTokens{
			AccessToken:  "good",
			RefreshToken: "good-refresh",
			Email:        "user@example.com",
		},
	}
	m, _ = m.Update(msg)
	if m.codexLoggingIn {
		t.Error("codexLoggingIn should clear after matching OAuth done")
	}
	if !m.codexAuth.valid {
		t.Error("codexAuth should be valid after matching OAuth done")
	}
	authPath := filepath.Join(dir, "codex-auth.json")
	if _, err := os.Stat(authPath); err != nil {
		t.Errorf("matching OAuth done should write codex-auth.json: %v", err)
	}
}

// TestPickPreset_DelCancelsInFlightLogin verifies that pressing Del
// while codexLoggingIn invokes the stored cancel and bumps the epoch,
// so any late callback is dropped.
func TestPickPreset_DelCancelsInFlightLogin(t *testing.T) {
	dir := t.TempDir()
	cancelled := false
	m := FirstRunModel{
		step:           stepPickPreset,
		globalDir:      dir,
		cursor:         0, // Codex row (no saved presets)
		codexLoggingIn: true,
		codexCancel:    func() { cancelled = true },
	}
	startEpoch := m.codexLoginEpoch

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})

	if !cancelled {
		t.Error("Del during codexLoggingIn must invoke codexCancel")
	}
	if m.codexLoggingIn {
		t.Error("codexLoggingIn should be cleared after cancel")
	}
	if m.codexLoginEpoch == startEpoch {
		t.Error("codexLoginEpoch should bump on cancel so late callbacks are dropped")
	}
	if m.codexCancel != nil {
		t.Error("codexCancel should be cleared after invoking")
	}
}

func TestSetupModeDefaultsToKeepCurrentPreset(t *testing.T) {
	baseDir := t.TempDir()
	globalDir := t.TempDir()
	orchDir := filepath.Join(baseDir, "mimo-1")
	if err := os.MkdirAll(orchDir, 0o755); err != nil {
		t.Fatalf("mkdir orchDir: %v", err)
	}
	initJSON := map[string]interface{}{
		"manifest": map[string]interface{}{
			"language": "zh",
			"llm": map[string]interface{}{
				"provider":    "deepseek",
				"model":       "deepseek-v4-flash",
				"api_key_env": "DEEPSEEK_API_KEY",
			},
			"capabilities": map[string]interface{}{
				"web_search": map[string]interface{}{"provider": "duckduckgo"},
				"vision":     map[string]interface{}{"provider": "inherit"},
			},
		},
	}
	data, err := json.Marshal(initJSON)
	if err != nil {
		t.Fatalf("marshal init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orchDir, "init.json"), data, 0o644); err != nil {
		t.Fatalf("write init: %v", err)
	}

	m := NewSetupModeModel(baseDir, globalDir, orchDir, "mimo-1")
	if !m.setupMode {
		t.Fatalf("expected setupMode=true")
	}
	if m.step != stepPickPreset {
		t.Fatalf("expected setup mode to start at preset picker, got %v", m.step)
	}
	if m.cursor != -1 {
		t.Fatalf("/setup should default to keep-current preset row; cursor=%d", m.cursor)
	}
	if got := m.getPresetProvider(m.currentPreset()); got != "deepseek" {
		t.Fatalf("currentPreset should be synthesized from existing init.json, provider=%q", got)
	}
	caps, ok := m.currentPreset().Manifest["capabilities"].(map[string]interface{})
	if !ok || caps["web_search"] == nil || caps["vision"] == nil {
		t.Fatalf("currentPreset should preserve existing optional capabilities, caps=%#v", caps)
	}
}

func TestSetupModeEnterOnKeepCurrentAdvancesToAgentPresets(t *testing.T) {
	m := FirstRunModel{
		setupMode: true,
		step:      stepPickPreset,
		cursor:    -1,
		presets: []preset.Preset{
			{
				Name:   "saved-one",
				Source: preset.SourceSaved,
				Manifest: map[string]interface{}{
					"llm": map[string]interface{}{"provider": "minimax"},
				},
			},
		},
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.step != stepAgentPresets {
		t.Fatalf("Enter on setup keep-current row should advance to agent presets; got step %v", m.step)
	}
}
