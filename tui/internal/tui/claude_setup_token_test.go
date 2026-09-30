package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// Tests for the claude-code family: local Claude login first, a
// `claude setup-token` token (CLAUDE_CODE_OAUTH_TOKEN) when not logged in or
// when stored (it takes precedence), and an auth-only preset editor.

func claudeTemplateForTest(t *testing.T) preset.Preset {
	t.Helper()
	p := builtinPresetForEditorTest(t, "claude")
	p.Source = preset.SourceTemplate
	return p
}

func claudeSavedPresetForTest(extra map[string]interface{}) preset.Preset {
	llm := map[string]interface{}{
		"provider":    "claude-code",
		"api_key_env": "CLAUDE_CODE_OAUTH_TOKEN",
	}
	for k, v := range extra {
		llm[k] = v
	}
	return preset.Preset{
		Name:        "claude-1",
		Description: preset.PresetDescription{Summary: "claude"},
		Source:      preset.SourceSaved,
		Manifest:    map[string]interface{}{"llm": llm},
	}
}

// The Claude editor shows only the auth row in its LLM section: no provider
// choice, model, endpoint, reasoning, service tier, or wire format.
func TestPresetEditorClaudeShowsOnlyAuthRow(t *testing.T) {
	m := NewPresetEditorModel(claudeTemplateForTest(t), "en", nil, "")
	m, _ = m.Update(claudeLoginStatusMsg{Info: claudeCodeAuthInfo{LoggedIn: true, Email: "user@example.com"}})
	for _, f := range []editorField{feProvider, feModel, feServiceTier, feThinking, feWireAPI, feResponsesTransport, feBaseURL} {
		if m.fieldVisible(f) {
			t.Fatalf("claude-code editor shows field %v", f)
		}
	}
	if !m.fieldVisible(feAPIKey) {
		t.Fatal("claude-code editor must show the auth row")
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
	view := m.View()
	for _, label := range []string{"provider", "model", "base_url", "thinking", "service_tier", "wire_api"} {
		if strings.Contains(view, i18n.T("preset_editor.field_"+label)+" ") {
			t.Fatalf("claude-code editor rendered the %s row:\n%s", label, view)
		}
	}
	for _, opt := range []string{"openai", "anthropic", "codex"} {
		if strings.Contains(view, opt) {
			t.Fatalf("claude-code editor still offers the %q family:\n%s", opt, view)
		}
	}
	line := findLineContaining(t, view, i18n.T("preset_editor.field_claude_auth"))
	if !strings.Contains(line, "using local Claude login (user@example.com)") {
		t.Fatalf("auth row = %q, want the local-login status", line)
	}
	if !strings.Contains(view, "claude-p") {
		t.Fatalf("LLM section header should name the claude-p family:\n%s", view)
	}
}

// Every family preset hides the four-family provider choice; only that
// family's own fields render.
func TestPresetEditorFamilyScopedFields(t *testing.T) {
	anthropic := testOpenAIPresetEditorPreset()
	anthropic.Manifest["llm"].(map[string]interface{})["provider"] = "anthropic"
	cases := []struct {
		name    string
		preset  preset.Preset
		visible []editorField
		hidden  []editorField
	}{
		{"openai", testOpenAIPresetEditorPreset(),
			[]editorField{feModel, feServiceTier, feThinking, feWireAPI, feBaseURL, feAPIKey},
			[]editorField{feProvider}},
		{"anthropic", anthropic,
			[]editorField{feModel, feThinking, feBaseURL, feAPIKey},
			[]editorField{feProvider, feServiceTier, feWireAPI, feResponsesTransport}},
		{"codex", testCodexPresetEditorPreset(nil),
			[]editorField{feModel, feServiceTier, feThinking, feBaseURL, feAPIKey},
			[]editorField{feProvider, feWireAPI, feResponsesTransport}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewPresetEditorModelWithBuiltinFlag(tc.preset, "en", nil, "", false)
			for _, f := range tc.visible {
				if !m.fieldVisible(f) {
					t.Errorf("field %v hidden, want visible", f)
				}
			}
			for _, f := range tc.hidden {
				if m.fieldVisible(f) {
					t.Errorf("field %v visible, want hidden", f)
				}
			}
			m, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 80})
			view := m.View()
			// The four-family radio strip would list claude-code and mark
			// the other families with hollow dots.
			for _, family := range editorProviders {
				if strings.Contains(view, "○ "+family) || strings.Contains(view, "claude-code") {
					t.Fatalf("%s editor renders the provider choice:\n%s", tc.name, view)
				}
			}
		})
	}
}

// The Claude auth row is editable: Enter opens the paste field with the
// setup-token hint, and a pasted token is committed under the shared slot.
func TestPresetEditorClaudeAuthRowIsEditable(t *testing.T) {
	m := NewPresetEditorModel(claudeTemplateForTest(t), "en", nil, "")
	m.cursor = editorFieldOrderIndex(t, feAPIKey)
	m.normalizeCursor()
	if got := m.renderFooter(); !strings.Contains(got, "claude setup-token") {
		t.Fatalf("auth-row footer = %q, want the setup-token hint", got)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != emInline {
		t.Fatalf("mode after Enter on the Claude auth row = %v, want inline paste", m.mode)
	}
	if got := m.renderFooter(); !strings.Contains(got, "claude setup-token") {
		t.Fatalf("inline footer = %q, want the setup-token hint", got)
	}
	m, _ = m.Update(tea.PasteMsg{Content: "sk-ant-oat01-placeholder"})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.apiKeySet || m.apiKey != "sk-ant-oat01-placeholder" {
		t.Fatalf("pasted token not captured: set=%v", m.apiKeySet)
	}
	if got := m.fieldString(feAPIKey); !strings.Contains(got, "setup-token stored") || strings.Contains(got, "placeholder") {
		t.Fatalf("auth row after paste = %q, want the token status without the secret", got)
	}

	updated, cmd := m.commit()
	if cmd == nil {
		t.Fatalf("commit returned no cmd; saveErr=%q", updated.saveErr)
	}
	msg := cmd()
	if _, isClone := msg.(PresetEditorCommitMsg); !isClone {
		// A template with edits routes through the clone prompt first.
		updated.cloneNameInput.SetValue("claude-work")
		updated, cmd = updated.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		msg = cmd()
	}
	commit, ok := msg.(PresetEditorCommitMsg)
	if !ok {
		t.Fatalf("commit msg = %T, want PresetEditorCommitMsg", msg)
	}
	if !commit.APIKeySet || commit.APIKey != "sk-ant-oat01-placeholder" {
		t.Fatal("commit must carry the pasted token")
	}
	stamped := stampAutoEnvVar(commit.Preset, map[string]string{})
	if got := stamped.Manifest["llm"].(map[string]interface{})["api_key_env"]; got != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("claude api_key_env after stamp = %#v, want CLAUDE_CODE_OAUTH_TOKEN", got)
	}
}

// Saving a Claude preset never writes model/thinking/base_url, even when a
// legacy saved preset still carries them.
func TestPresetEditorClaudeCommitDropsModelAndThinking(t *testing.T) {
	p := claudeSavedPresetForTest(map[string]interface{}{
		"model": "opus", "thinking": "high", "base_url": "https://example.test", "service_tier": "fast",
	})
	m := NewPresetEditorModel(p, "en", nil, "")
	_, cmd := m.commit()
	commit := cmd().(PresetEditorCommitMsg)
	llm := commit.Preset.Manifest["llm"].(map[string]interface{})
	for _, key := range []string{"model", "thinking", "base_url", "service_tier"} {
		if v, ok := llm[key]; ok {
			t.Fatalf("committed claude preset kept %s = %#v", key, v)
		}
	}
	if llm["provider"] != "claude-code" || llm["api_key_env"] != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("committed claude llm = %#v", llm)
	}
}

// The editor probes the local login only for a Claude preset without a
// token; a stored token (shared slot, shown even on the template) wins.
func TestPresetEditorClaudeLoginProbeOnlyWithoutToken(t *testing.T) {
	calls := stubClaudeLogin(t, claudeCodeAuthInfo{LoggedIn: true})

	m := NewPresetEditorModel(claudeTemplateForTest(t), "en", nil, "")
	if got := m.fieldString(feAPIKey); got != i18n.T("claude.auth_checking") {
		t.Fatalf("auth row before probe = %q, want checking", got)
	}
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Claude preset without token must probe the local login")
	}
	m, _ = m.Update(cmd())
	if *calls != 1 {
		t.Fatalf("probe calls = %d, want 1", *calls)
	}
	if got := m.fieldString(feAPIKey); got != i18n.T("claude.auth_login") {
		t.Fatalf("auth row after probe = %q, want local login", got)
	}

	withToken := NewPresetEditorModel(claudeTemplateForTest(t), "en", map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "tok"}, "")
	if cmd := withToken.Init(); cmd != nil {
		t.Fatal("a stored token takes precedence; no login probe")
	}
	if got := withToken.fieldString(feAPIKey); got != i18n.TF("claude.auth_token", "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Fatalf("auth row with stored token = %q", got)
	}

	openai := NewPresetEditorModelWithBuiltinFlag(testOpenAIPresetEditorPreset(), "en", nil, "", false)
	if cmd := openai.Init(); cmd != nil {
		t.Fatal("non-Claude presets must not probe the Claude CLI")
	}
}

// First-run: a logged-in Claude CLI skips the paste step; a stored token
// skips it too; neither routes to the setup-token paste step. The probe is
// lazy and runs once.
func TestFirstRunClaudePresetNeedsTokenOnlyWithoutLogin(t *testing.T) {
	p := claudeSavedPresetForTest(nil)

	calls := stubClaudeLogin(t, claudeCodeAuthInfo{LoggedIn: true, Email: "user@example.com"})
	m := FirstRunModel{existingKeys: map[string]string{}}
	m.ensureClaudeLoginFor(p)
	m.ensureClaudeLoginFor(p)
	if *calls != 1 {
		t.Fatalf("lazy login probe ran %d times, want once", *calls)
	}
	if m.presetNeedsKey(p) {
		t.Fatal("logged-in Claude CLI must skip the token step")
	}
	if st, _ := m.claudeAuthStatusFor(p); st.Label() != "✓ using local Claude login (user@example.com)" {
		t.Fatalf("status = %q", st.Label())
	}

	stubClaudeLogin(t, claudeCodeAuthInfo{})
	m = FirstRunModel{existingKeys: map[string]string{}}
	m.ensureClaudeLoginFor(p)
	if !m.presetNeedsKey(p) {
		t.Fatal("no login and no token must ask for the setup-token")
	}
	m.existingKeys["CLAUDE_CODE_OAUTH_TOKEN"] = "tok"
	if m.presetNeedsKey(p) {
		t.Fatal("a stored setup-token must skip the token step")
	}

	// A legacy Claude preset with no api_key_env reads the default slot.
	legacy := claudeSavedPresetForTest(map[string]interface{}{"api_key_env": ""})
	if m.presetNeedsKey(legacy) {
		t.Fatal("legacy Claude preset must read CLAUDE_CODE_OAUTH_TOKEN")
	}
}

// Constructing the wizard never execs `claude`: the probe waits until a
// Claude preset is being set up (draft zero-write safety).
func TestFirstRunConstructorDoesNotProbeClaude(t *testing.T) {
	calls := stubClaudeLogin(t, claudeCodeAuthInfo{LoggedIn: true})
	dir := t.TempDir()
	_ = NewFirstRunModel(dir, dir, true)
	_ = NewDraftFirstRunModel(dir, dir, true, &ProjectDraft{})
	if *calls != 0 {
		t.Fatalf("constructors ran the Claude login probe %d times, want 0", *calls)
	}
}

// The paste step for a Claude preset asks for the setup-token with the
// `claude setup-token` hint and writes it under CLAUDE_CODE_OAUTH_TOKEN.
func TestFirstRunClaudePasteStepHintAndSave(t *testing.T) {
	dir := t.TempDir()
	keyInput := textarea.New()
	m := FirstRunModel{
		globalDir:      dir,
		existingKeys:   map[string]string{},
		presets:        []preset.Preset{claudeSavedPresetForTest(nil)},
		presetKeyInput: keyInput,
		nameInput:      textinput.New(),
		dirInput:       textinput.New(),
		width:          120,
		height:         40,
	}
	m, _ = m.enterPresetKeyFor(m.presets[0])
	view := m.View()
	for _, want := range []string{i18n.T("firstrun.enter_claude_token"), "Run `claude setup-token` in a terminal and paste the token here", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Claude paste step missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "API Key") {
		t.Fatalf("Claude paste step should not ask for an API key:\n%s", view)
	}

	m.presetKeyInput.SetValue("sk-ant-oat01-placeholder")
	m.keyFieldIdx = 2 // Next
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.step == stepPresetKey {
		t.Fatalf("Next did not advance; message=%q", m.message)
	}
	if got := m.existingKeys["CLAUDE_CODE_OAUTH_TOKEN"]; got != "sk-ant-oat01-placeholder" {
		t.Fatal("token not stored under CLAUDE_CODE_OAUTH_TOKEN")
	}
	keys, _ := config.ResolveKeys(dir)
	if keys["CLAUDE_CODE_OAUTH_TOKEN"] != "sk-ant-oat01-placeholder" {
		t.Fatal("token not persisted to the key store")
	}
}

// Setup → Credentials lists a stored Claude token by presence only (no
// network) and says which Claude credential is active.
func TestLoginModelClaudeTokenPresenceAndActivePath(t *testing.T) {
	stubClaudeLogin(t, claudeCodeAuthInfo{LoggedIn: true, Email: "user@example.com"})
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := config.Config{Keys: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "sk-ant-oat01-placeholder-token"}}
	if err := config.SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	m := NewLoginModel("", dir)
	m.width = 100
	var entry *loginEntry
	for i := range m.entries {
		if m.entries[i].Provider == "CLAUDE_CODE_OAUTH_TOKEN" {
			entry = &m.entries[i]
		}
	}
	if entry == nil {
		t.Fatalf("credentials page does not list the Claude token: %#v", m.entries)
	}
	if entry.Family != preset.ProviderClaudeCode {
		t.Fatalf("Claude token family = %q, want claude-code", entry.Family)
	}
	health := checkHealth(*entry) // must not dial anything
	if health.Status != loginUnverified || health.Detail != i18n.T("login.claude_token_stored") {
		t.Fatalf("Claude token health = %#v, want stored-not-verified", health)
	}
	if !m.claudeRelevant {
		t.Fatal("a stored Claude token makes the Claude line relevant")
	}
	view := m.View()
	if !strings.Contains(view, i18n.TF("claude.auth_token", "CLAUDE_CODE_OAUTH_TOKEN")) {
		t.Fatalf("Claude line should report the token path:\n%s", view)
	}
	if strings.Contains(view, "placeholder-token") {
		t.Fatalf("credentials page leaked the token:\n%s", view)
	}

	// Without a token: the local login path, then neither → hint.
	noToken := LoginModel{claudeRelevant: true, claudeEnv: "CLAUDE_CODE_OAUTH_TOKEN", width: 100}
	noToken, _ = noToken.Update(claudeLoginStatusMsg{Info: claudeCodeAuthInfo{LoggedIn: true, Email: "user@example.com"}})
	if view := noToken.View(); !strings.Contains(view, "using local Claude login (user@example.com)") {
		t.Fatalf("Claude line should report the local login:\n%s", view)
	}
	noToken, _ = noToken.Update(claudeLoginStatusMsg{Info: claudeCodeAuthInfo{}})
	view = noToken.View()
	if !strings.Contains(view, i18n.T("claude.auth_none")) || !strings.Contains(view, i18n.T("login.claude_setup_token_hint")) {
		t.Fatalf("Claude line should report neither with the setup-token hint:\n%s", view)
	}
}

func TestLoginModelClaudeLineHiddenWhenNoClaudeInUse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "presets"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewLoginModel("", dir)
	m.width = 100
	if m.claudeRelevant || strings.Contains(m.View(), i18n.T("login.claude_line")) {
		t.Fatal("Claude line must stay hidden when no Claude preset, agent, or token is in use")
	}
}
