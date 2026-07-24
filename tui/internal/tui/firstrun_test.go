package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// TestSetupViewOmitsMoltPressureField guards the removal of the molt_pressure
// configurable field from the agent setup wizard. molt_pressure is no longer a
// user-facing setup input — it is written to init.json at its DefaultAgentOpts
// value by the preset layer, but the wizard exposes no row for it. The Runtime
// section still renders its other numeric fields (e.g. Max RPM), so the
// assertion proves the molt row was removed, not the whole section.
func TestSetupViewOmitsMoltPressureField(t *testing.T) {
	i18n.SetLang("en")
	baseDir := t.TempDir()
	globalDir := t.TempDir()
	orchDir := filepath.Join(baseDir, "manager")
	if err := os.MkdirAll(orchDir, 0o755); err != nil {
		t.Fatalf("mkdir orchDir: %v", err)
	}
	initJSON := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name":    "岩",
			"language":      "en",
			"molt_pressure": 0.42,
		},
	}
	data, err := json.Marshal(initJSON)
	if err != nil {
		t.Fatalf("marshal init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orchDir, "init.json"), data, 0o644); err != nil {
		t.Fatalf("write init: %v", err)
	}

	m := NewSetupModeModel(baseDir, globalDir, orchDir, "manager")
	m.enterAgentNameDir(m.currentPreset())
	m.step = stepAgentNameDir
	view := m.View()

	// The i18n key is removed, so T() echoes the key back; assert both the
	// key echo and the former human label are absent from the rendered view.
	if strings.Contains(view, "firstrun.molt_pressure") || strings.Contains(view, "Molt pressure") {
		t.Fatalf("setup view should not expose the molt_pressure field; view=%s", view)
	}
	// Sanity: the Runtime section is still present (Max RPM row remains).
	if maxRpm := i18n.T("firstrun.max_rpm"); !strings.Contains(view, maxRpm) {
		t.Fatalf("setup view should still render the Runtime section (%q missing)", maxRpm)
	}
}

func TestFirstRunAndSetupViewsOmitObsoletePrincipleControl(t *testing.T) {
	i18n.SetLang("en")
	for _, tc := range []struct {
		name      string
		setupMode bool
	}{
		{name: "first-run", setupMode: false},
		{name: "setup", setupMode: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseDir := filepath.Join(t.TempDir(), ".lingtai")
			globalDir := t.TempDir()
			var m FirstRunModel
			if tc.setupMode {
				m = NewSetupModeModel(baseDir, globalDir, "", "test")
			} else {
				m = NewFirstRunModel(baseDir, globalDir, true, "")
				m.presets = []preset.Preset{{
					Name:     "test",
					Manifest: map[string]interface{}{"language": "en"},
				}}
			}
			m.width = 120
			m.height = 40
			m.enterAgentNameDir(m.currentPreset())
			view := m.View()

			if strings.Contains(view, i18n.T("firstrun.principle")) || strings.Contains(view, "firstrun.principle") {
				t.Fatalf("%s view exposes the obsolete Principle control: %s", tc.name, view)
			}
			for _, label := range []string{i18n.T("firstrun.covenant"), i18n.T("firstrun.soul_flow"), i18n.T("firstrun.comment")} {
				if !strings.Contains(view, label) {
					t.Fatalf("%s view lost supported prompt %q: %s", tc.name, label, view)
				}
			}
		})
	}
}

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

func TestPickPreset_CodexAuthRowShowsAccountLabel(t *testing.T) {
	i18n.SetLang("en")
	globalDir := t.TempDir()
	workPath := filepath.Join(codexAuthDir(globalDir), "work-bob.json")
	writeCodexAccountFile(t, workPath, "")

	m := FirstRunModel{
		step:      stepPickPreset,
		setupMode: true,
		globalDir: globalDir,
		cursor:    0,
	}
	m.refreshCodexAuth()
	if !m.codexAuth.valid {
		t.Fatal("per-account Codex auth should make /setup codex auth row valid")
	}
	if m.codexAuth.label != "work-bob" {
		t.Fatalf("expected per-account slug label, got %q", m.codexAuth.label)
	}
	view := m.View()
	if !strings.Contains(view, "work-bob") {
		t.Fatalf("/setup codex auth row should show account label; view=%s", view)
	}
	if strings.Contains(view, i18n.T("preset.codex_credential_authed_badge")) {
		t.Fatalf("/setup codex auth row should not fall back to generic logged-in badge; view=%s", view)
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

// TestPickPreset_EscDuringLoginStaysOnPicker verifies the reported UX-bug
// fix: pressing Esc while a Codex login is mid-flight cancels the login
// and stays on the preset picker — it must NOT emit a ViewChangeMsg that
// would dump the user straight back to the home/mail view, and must not
// leave a stale spinner/URL/code behind.
func TestPickPreset_EscDuringLoginStaysOnPicker(t *testing.T) {
	dir := t.TempDir()
	cancelled := false
	m := FirstRunModel{
		step:           stepPickPreset,
		globalDir:      dir,
		cursor:         0, // Codex row (no saved presets)
		codexLoggingIn: true,
		codexCancel:    func() { cancelled = true },
		codexAuthURL:   "https://auth.openai.com/x",
		codexDeviceURL: "https://auth.openai.com/codex/device",
	}
	startEpoch := m.codexLoginEpoch

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if !cancelled {
		t.Error("Esc during codexLoggingIn must invoke codexCancel")
	}
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if vc, ok := msg.(ViewChangeMsg); ok {
				t.Fatalf("Esc mid-login must not change view; got ViewChangeMsg{View:%q}", vc.View)
			}
		}
	}
	if m.step != stepPickPreset {
		t.Errorf("Esc mid-login should stay on stepPickPreset; got step=%v", m.step)
	}
	if m.codexLoggingIn {
		t.Error("codexLoggingIn should clear after Esc cancel")
	}
	if m.codexLoginEpoch == startEpoch {
		t.Error("codexLoginEpoch should bump on Esc cancel so late callbacks are dropped")
	}
	if m.codexAuthURL != "" || m.codexDeviceURL != "" {
		t.Errorf("Esc cancel must clear transient login fields; url=%q devURL=%q", m.codexAuthURL, m.codexDeviceURL)
	}
}

// TestPickPreset_EscFromMethodChooserStaysOnPicker verifies that Esc while
// the Codex login method chooser is open backs out to the picker (closing
// the chooser) rather than exiting to home.
func TestPickPreset_EscFromMethodChooserStaysOnPicker(t *testing.T) {
	dir := t.TempDir()
	m := FirstRunModel{
		step:                stepPickPreset,
		globalDir:           dir,
		cursor:              0,
		codexChoosingMethod: true,
	}
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if vc, ok := msg.(ViewChangeMsg); ok {
				t.Fatalf("Esc from method chooser must not change view; got ViewChangeMsg{View:%q}", vc.View)
			}
		}
	}
	if m.codexChoosingMethod {
		t.Error("Esc should close the Codex method chooser")
	}
	if m.step != stepPickPreset {
		t.Errorf("Esc from chooser should stay on stepPickPreset; got step=%v", m.step)
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

func TestSetupModePrefillsAgentNameAndCommentFile(t *testing.T) {
	baseDir := t.TempDir()
	globalDir := t.TempDir()
	orchDir := filepath.Join(baseDir, "manager")
	if err := os.MkdirAll(orchDir, 0o755); err != nil {
		t.Fatalf("mkdir orchDir: %v", err)
	}
	commentPath := filepath.Join(t.TempDir(), "comment.md")
	initJSON := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "岩",
			"language":   "zh",
		},
		"comment_file": commentPath,
		"comment":      "legacy-comment-should-not-win",
	}
	data, err := json.Marshal(initJSON)
	if err != nil {
		t.Fatalf("marshal init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orchDir, "init.json"), data, 0o644); err != nil {
		t.Fatalf("write init: %v", err)
	}

	m := NewSetupModeModel(baseDir, globalDir, orchDir, "manager")
	m.enterAgentNameDir(m.currentPreset())

	if got := m.nameInput.Value(); got != "岩" {
		t.Fatalf("setup agent name prefill = %q, want init.json manifest.agent_name", got)
	}
	if got := m.commentInput.Value(); got != commentPath {
		t.Fatalf("setup comment prefill = %q, want comment_file %q", got, commentPath)
	}
}

func TestSetupModePrefillsLegacyCommentWhenCommentFileMissing(t *testing.T) {
	baseDir := t.TempDir()
	globalDir := t.TempDir()
	orchDir := filepath.Join(baseDir, "manager")
	if err := os.MkdirAll(orchDir, 0o755); err != nil {
		t.Fatalf("mkdir orchDir: %v", err)
	}
	initJSON := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "岩",
			"language":   "zh",
		},
		"comment": "legacy-comment.md",
	}
	data, err := json.Marshal(initJSON)
	if err != nil {
		t.Fatalf("marshal init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orchDir, "init.json"), data, 0o644); err != nil {
		t.Fatalf("write init: %v", err)
	}

	m := NewSetupModeModel(baseDir, globalDir, orchDir, "manager")
	m.enterAgentNameDir(m.currentPreset())

	if got := m.commentInput.Value(); got != "legacy-comment.md" {
		t.Fatalf("setup legacy comment prefill = %q", got)
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

func TestEnterAgentNameDirLanguageFollowsTUIConfig(t *testing.T) {
	tests := []struct {
		name        string
		tuiLang     string
		presetLang  string
		wantIdx     int
		wantPathBit string
	}{
		{
			name:        "english UI overrides chinese preset",
			tuiLang:     "en",
			presetLang:  "zh",
			wantIdx:     0,
			wantPathBit: filepath.Join("covenant", "en", "covenant.md"),
		},
		{
			name:        "chinese UI overrides english preset",
			tuiLang:     "zh",
			presetLang:  "en",
			wantIdx:     1,
			wantPathBit: filepath.Join("covenant", "zh", "covenant.md"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globalDir := t.TempDir()
			if err := config.SaveTUIConfig(globalDir, config.TUIConfig{Language: tt.tuiLang, MailPageSize: 200}); err != nil {
				t.Fatalf("save TUI config: %v", err)
			}
			m := NewFirstRunModel(t.TempDir(), globalDir, true, "")
			m.enterAgentNameDir(preset.Preset{
				Name: "tutorial-test",
				Manifest: map[string]interface{}{
					"language": tt.presetLang,
				},
			})

			if m.agentLangIdx != tt.wantIdx {
				t.Fatalf("agentLangIdx = %d, want %d", m.agentLangIdx, tt.wantIdx)
			}
			if got := m.covenantInput.Value(); !strings.Contains(got, tt.wantPathBit) {
				t.Fatalf("covenantInput = %q, want path containing %q", got, tt.wantPathBit)
			}
		})
	}
}

func TestEnterAgentNameDirLanguageFallsBackToPresetWhenTUIConfigInvalid(t *testing.T) {
	globalDir := t.TempDir()
	if err := config.SaveTUIConfig(globalDir, config.TUIConfig{Language: "bogus", MailPageSize: 200}); err != nil {
		t.Fatalf("save TUI config: %v", err)
	}
	m := NewFirstRunModel(t.TempDir(), globalDir, true, "")
	m.enterAgentNameDir(preset.Preset{
		Name: "fallback-test",
		Manifest: map[string]interface{}{
			"language": "wen",
		},
	})

	if m.agentLangIdx != 2 {
		t.Fatalf("agentLangIdx = %d, want wen index 2", m.agentLangIdx)
	}
	want := filepath.Join("covenant", "wen", "covenant.md")
	if got := m.covenantInput.Value(); !strings.Contains(got, want) {
		t.Fatalf("covenantInput = %q, want path containing %q", got, want)
	}
}

func TestEnterAgentNameDirSetupModeSurfacesExistingInitLanguage(t *testing.T) {
	globalDir := t.TempDir()
	if err := config.SaveTUIConfig(globalDir, config.TUIConfig{Language: "en", MailPageSize: 200}); err != nil {
		t.Fatalf("save TUI config: %v", err)
	}
	m := NewFirstRunModel(t.TempDir(), globalDir, true, "")
	m.setupMode = true
	m.setupKeepInitJSON = map[string]interface{}{
		"manifest": map[string]interface{}{
			"language": "wen",
		},
	}
	m.enterAgentNameDir(preset.Preset{
		Name: "keep-current",
		Manifest: map[string]interface{}{
			"language": "zh",
		},
	})

	if m.agentLangIdx != 2 {
		t.Fatalf("agentLangIdx = %d, want existing init wen index 2", m.agentLangIdx)
	}
	want := filepath.Join("covenant", "wen", "covenant.md")
	if got := m.covenantInput.Value(); !strings.Contains(got, want) {
		t.Fatalf("covenantInput = %q, want path containing %q", got, want)
	}
}

func TestPickPreset_CodexEnterShowsMethodChooser(t *testing.T) {
	dir := t.TempDir()
	m := FirstRunModel{
		step:      stepPickPreset,
		globalDir: dir,
		cursor:    0, // Codex credential row when there are no visible presets.
	}

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("opening the Codex method chooser must not start a network command")
	}
	if !m.codexChoosingMethod {
		t.Fatal("Enter on Codex credential row should show method chooser")
	}
	if m.codexLoggingIn {
		t.Fatal("method chooser should not start login yet")
	}
	if m.codexMethodCursor != 0 {
		t.Fatalf("default method cursor = %d, want browser OAuth (0)", m.codexMethodCursor)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.codexMethodCursor != 1 {
		t.Fatalf("Down should select device code; cursor=%d", m.codexMethodCursor)
	}
	view := m.View()
	if !strings.Contains(view, "Device code") || !strings.Contains(view, "remote") {
		t.Fatalf("chooser view should mention remote-friendly device code; view=%s", view)
	}
}

// TestPickPreset_SetupModeCodexEnterRoutesToCredentials verifies that in
// /setup mode, pressing Enter on the Codex credential row emits
// ViewChangeMsg{View:"login"} — routing to the Setup → Credentials subpage —
// instead of opening the inline browser/device-code chooser (which is the
// first-run/non-setupMode path tested above).
func TestPickPreset_SetupModeCodexEnterRoutesToCredentials(t *testing.T) {
	dir := t.TempDir()
	m := FirstRunModel{
		step:      stepPickPreset,
		globalDir: dir,
		setupMode: true,
		cursor:    0, // Codex credential row: no visible presets, so pickCodexAuthIdx == 0.
	}

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.codexChoosingMethod {
		t.Fatal("setup mode Codex row should NOT open the inline method chooser")
	}
	if cmd == nil {
		t.Fatal("setup mode Codex row must return a command (ViewChangeMsg)")
	}
	msg := cmd()
	vc, ok := msg.(ViewChangeMsg)
	if !ok {
		t.Fatalf("expected ViewChangeMsg; got %T: %v", msg, msg)
	}
	if vc.View != "login" {
		t.Fatalf("expected ViewChangeMsg{View:\"login\"}; got View=%q", vc.View)
	}
}

// failingGlobalDir returns a globalDir where config.SaveConfig is guaranteed
// to fail: it writes config.json fine but then WriteEnvFile chokes because the
// .env path is a directory (os.ReadFile → EISDIR). Deterministic, no chmod.
func failingGlobalDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(config.EnvFilePath(dir), 0o755); err != nil {
		t.Fatalf("mkdir .env-as-dir: %v", err)
	}
	// Sanity: SaveConfig really does fail for this dir.
	if err := config.SaveConfig(dir, config.Config{Keys: map[string]string{"MINIMAX_API_KEY": "x"}}); err == nil {
		t.Fatalf("expected SaveConfig to fail when .env is a directory")
	}
	return dir
}

// TestPresetKeyNext_AdvancesWithoutLiveProbeForAPIKeyProvider is the
// regression test for the reported (Jason, 2026-07-23) bug where every
// provider — Codex and API-key providers such as DeepSeek alike — was
// rejected by a save-time live-availability check even though the
// configured provider worked. Pressing Next on stepPresetKey must save
// the key and advance immediately: no pending/checking state, and no
// live HTTP call (base_url points at a reserved, unreachable port, so a
// probe would fail/hang and this test would not pass step advancement).
func TestPresetKeyNext_AdvancesWithoutLiveProbeForAPIKeyProvider(t *testing.T) {
	dir := t.TempDir()
	keyInput := textarea.New()
	keyInput.SetValue("sk-deepseek-test")
	m := FirstRunModel{
		step:         stepPresetKey,
		globalDir:    dir,
		existingKeys: map[string]string{},
		keyFieldIdx:  2, // Next button
		cursor:       0,
		presets: []preset.Preset{
			{
				Name: "deepseek-test",
				Manifest: map[string]interface{}{
					"llm": map[string]interface{}{
						"provider":    "deepseek",
						"model":       "deepseek-v4-pro",
						"api_compat":  "openai",
						"base_url":    "http://127.0.0.1:1", // reserved port; would fail/hang if ever dialed
						"api_key_env": "DEEPSEEK_API_KEY",
					},
				},
			},
		},
		presetKeyInput: keyInput,
	}

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.step == stepPresetKey {
		t.Fatalf("expected the wizard to advance past stepPresetKey immediately, with no live probe; message=%q", m.message)
	}
	if cmd == nil {
		t.Fatalf("expected enterCapabilities' cmd on the first Next press")
	}
	if got := m.existingKeys["DEEPSEEK_API_KEY"]; got != "sk-deepseek-test" {
		t.Fatalf("expected the pasted key to be saved; got %q", got)
	}
}

// TestFirstRunAgentPresetsNext_AdvancesWithoutLiveCodexProbe is the Codex
// half of the same regression: selecting a Codex preset as default on
// stepAgentPresets and pressing Next must advance immediately with no
// live Responses-endpoint call and no pending/checking state.
func TestFirstRunAgentPresetsNext_AdvancesWithoutLiveCodexProbe(t *testing.T) {
	m := FirstRunModel{
		step: stepAgentPresets,
		presets: []preset.Preset{{
			Name: "codex-test",
			Manifest: map[string]interface{}{
				"llm": map[string]interface{}{
					"provider": "codex",
					"model":    "gpt-5.6-sol",
					"base_url": "http://127.0.0.1:1", // reserved port; would fail/hang if ever dialed
				},
			},
		}},
		savedPresetIdx:   []int{0},
		presetAllowed:    []bool{true},
		presetDefaultIdx: 0,
		presetCfgCursor:  2, // row 0, Back 1, Next 2
		cursor:           0,
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if updated.step == stepAgentPresets {
		t.Fatalf("expected the wizard to advance past stepAgentPresets immediately, with no live Codex probe; message=%q", updated.presetCfgMessage)
	}
	if cmd == nil {
		t.Fatalf("expected a cmd once the wizard advances past stepAgentPresets")
	}
}

// TestPresetKeyNext_SaveConfigErrorSurfacesAndDoesNotAdvance covers the
// stepPresetKey "Next" path (issue #509): when persisting the pasted API key
// fails, the wizard must surface the error and stay on stepPresetKey rather
// than silently advancing into the capabilities/runtime page as though the
// key was saved.
func TestPresetKeyNext_SaveConfigErrorSurfacesAndDoesNotAdvance(t *testing.T) {
	dir := failingGlobalDir(t)
	keyInput := textarea.New()
	keyInput.SetValue("sk-test-key")
	m := FirstRunModel{
		step:         stepPresetKey,
		globalDir:    dir,
		existingKeys: map[string]string{},
		keyFieldIdx:  2, // Next button
		cursor:       0,
		presets: []preset.Preset{
			{
				Name: "minimax",
				Manifest: map[string]interface{}{
					"llm": map[string]interface{}{
						"provider":    "minimax",
						"api_key_env": "MINIMAX_API_KEY",
					},
				},
			},
		},
		presetKeyInput: keyInput,
	}

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.step != stepPresetKey {
		t.Fatalf("save failure must keep the wizard on stepPresetKey; got step %v", m.step)
	}
	if cmd != nil {
		t.Fatalf("save failure should return nil cmd, not advance into capabilities")
	}
	if !strings.Contains(m.message, "Failed to save API key:") {
		t.Fatalf("expected a visible API key save error; got message %q", m.message)
	}
}

// TestPresetEditorCommit_KeySaveErrorSurfacesAndDoesNotSavePreset covers the
// PresetEditorCommitMsg path (issue #509): when the edited preset carries a new
// API key but persisting it fails, the wizard must surface the error and must
// not proceed into the preset-save/advance path as though the key was stored.
func TestPresetEditorCommit_KeySaveErrorSurfacesAndDoesNotSavePreset(t *testing.T) {
	dir := failingGlobalDir(t)
	// Isolate HOME: if the regression returns and the handler falls through
	// to preset.Save, it would write under ~/.lingtai-tui/presets/saved.
	// Point HOME at a temp dir so we can assert nothing was written there.
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := FirstRunModel{
		step:         stepPickPreset,
		globalDir:    dir,
		existingKeys: map[string]string{},
	}

	commit := PresetEditorCommitMsg{
		Preset: preset.Preset{
			Name: "minimax",
			Manifest: map[string]interface{}{
				"llm": map[string]interface{}{
					"provider":    "minimax",
					"api_key_env": "MINIMAX_API_KEY",
				},
			},
		},
		APIKeySet: true,
		APIKey:    "sk-edited-key",
	}

	m, _ = m.Update(commit)

	if !strings.Contains(m.message, "Failed to save API key:") {
		t.Fatalf("expected a visible API key save error; got message %q", m.message)
	}
	savedPath := filepath.Join(home, ".lingtai-tui", "presets", "saved", "minimax.json")
	if _, err := os.Stat(savedPath); !os.IsNotExist(err) {
		t.Fatalf("key save failure must not persist the preset; stat %q err = %v", savedPath, err)
	}
}
