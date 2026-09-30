package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

func writeSyntheticAgentPreset(t *testing.T, projectDir, agentName, presetRef, provider, model string, authRef string, active bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(presetRef), 0o700); err != nil {
		t.Fatal(err)
	}
	llm := map[string]interface{}{"provider": provider, "model": model}
	if authRef != "" {
		llm["codex_auth_path"] = authRef
	}
	presetDoc := map[string]interface{}{
		"name":     filepath.Base(presetRef),
		"manifest": map[string]interface{}{"llm": llm},
	}
	data, err := json.Marshal(presetDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(presetRef, data, 0o600); err != nil {
		t.Fatal(err)
	}
	writeSyntheticAgentInit(t, projectDir, agentName, presetRef, active)
}

func writeSyntheticAgentInit(t *testing.T, projectDir, agentName, presetRef string, active bool) {
	t.Helper()
	block := map[string]interface{}{"default": presetRef}
	if active {
		block["active"] = presetRef
	}
	doc := map[string]interface{}{"manifest": map[string]interface{}{"preset": block}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projectDir, agentName, "init.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCodexAuthForAgentsUsesLoadedManifestNotFilename(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	globalDir, projectDir := t.TempDir(), t.TempDir()
	presetRef := filepath.Join(t.TempDir(), "codex-looking-name.txt")
	writeSyntheticAgentPreset(t, projectDir, "agent-one", presetRef, "minimax", "MiniMax-M3", "", false)

	if got := validateCodexAuthForAgents(globalDir, projectDir); got != "" {
		t.Fatalf("non-Codex manifest in Codex-named path produced warning: %q", got)
	}
}

func TestValidateCodexAuthForAgentsUsesCodexBoundAndLegacyFacts(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	tests := []struct {
		name      string
		provider  string
		boundGood bool
		useBound  bool
		legacy    bool
		wantWarn  bool
	}{
		{name: "codex bound token", provider: "codex", boundGood: true, useBound: true, wantWarn: false},
		{name: "codex missing bound ignores legacy", provider: "codex", useBound: true, legacy: true, wantWarn: true},
		{name: "codex oauth bound token", provider: "codex_oauth", boundGood: true, useBound: true, wantWarn: false},
		{name: "codex oauth legacy fallback", provider: "codex_oauth", legacy: true, wantWarn: false},
		{name: "codex oauth missing legacy", provider: "codex_oauth", wantWarn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globalDir, projectDir := t.TempDir(), t.TempDir()
			if tt.legacy {
				writeStubCodexToken(t, legacyCodexAuthPath(globalDir), "legacy@example.test")
			}
			boundRef := filepath.Join(t.TempDir(), "random-bound-token.bin")
			if tt.boundGood {
				writeStubCodexToken(t, boundRef, "bound@example.test")
			}
			presetRef := filepath.Join(t.TempDir(), "preset-with-arbitrary-name.data")
			authRef := ""
			if tt.useBound {
				authRef = boundRef
			}
			writeSyntheticAgentPreset(t, projectDir, "agent-codex", presetRef, tt.provider, "gpt-5.6-sol", authRef, false)
			got := validateCodexAuthForAgents(globalDir, projectDir)
			want := ""
			if tt.wantWarn {
				want = i18n.TF("codex.oauth_unverified_agent", "agent-codex")
			}
			if got != want {
				t.Fatalf("warning = %q, want exact warning %q", got, want)
			}
			if tt.wantWarn && strings.Count(got, "agent-codex") != 1 {
				t.Fatalf("warning named agent-codex %d times, want exactly once: %q", strings.Count(got, "agent-codex"), got)
			}
		})
	}
}

func TestValidateCodexAuthForAgentsMalformedOrUnreadablePresetDoesNotUseFilenameFallback(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	tests := []struct {
		name string
		make func(t *testing.T, path string)
	}{
		{name: "malformed", make: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory (non-file)", make: func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globalDir, projectDir := t.TempDir(), t.TempDir()
			presetRef := filepath.Join(t.TempDir(), "codex-malformed-preset.json")
			tt.make(t, presetRef)
			writeSyntheticAgentInit(t, projectDir, "malformed-agent", presetRef, false)
			if got := validateCodexAuthForAgents(globalDir, projectDir); got != "" {
				t.Fatalf("malformed/unreadable preset emitted auth-specific filename warning: %q", got)
			}
		})
	}
}

func TestValidateCodexAuthForAgentsDeduplicatesDefaultAndActiveRef(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	globalDir, projectDir := t.TempDir(), t.TempDir()
	presetRef := filepath.Join(t.TempDir(), "arbitrary-invalid-codex-ref.json")
	writeStubCodexToken(t, legacyCodexAuthPath(globalDir), "legacy@example.test")
	if err := os.WriteFile(presetRef, []byte(`{"name":"bad","manifest":{"llm":{"provider":"codex","model":"gpt-5.6-sol","codex_auth_path":"/missing/token.json"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSyntheticAgentInit(t, projectDir, "duplicate-agent", presetRef, true)
	got := validateCodexAuthForAgents(globalDir, projectDir)
	want := i18n.TF("codex.oauth_unverified_agent", "duplicate-agent")
	if got != want {
		t.Fatalf("warning = %q, want exact warning %q", got, want)
	}
	if n := strings.Count(got, "duplicate-agent"); n != 1 {
		t.Fatalf("duplicate default/active ref appeared %d times in warning %q", n, got)
	}
}

func syntheticPickerPreset(provider, model string) preset.Preset {
	return preset.Preset{
		Name: "synthetic-" + provider,
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": provider,
				"model":    model,
			},
		},
	}
}

func firstRunPickerModel(t *testing.T, globalDir string, p preset.Preset) FirstRunModel {
	t.Helper()
	m := NewFirstRunModel(t.TempDir(), globalDir, true)
	m.step = stepPickPreset
	m.presets = []preset.Preset{p}
	m.cursor = 0
	m.width = 100
	m.height = 30
	return m
}

// TestValidateCodexAuthForAgentsIgnoresRetiredCodexPoolProvider pins that the
// retired codex-pool provider spellings are no longer a Codex credential
// family: an agent still naming one is not reported as missing Codex OAuth
// (pooling now lives in the external subs-pool project).
func TestValidateCodexAuthForAgentsIgnoresRetiredCodexPoolProvider(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	for _, provider := range []string{"codex-pool", "codex_pool"} {
		t.Run(provider, func(t *testing.T) {
			globalDir, projectDir := t.TempDir(), t.TempDir()
			presetRef := filepath.Join(t.TempDir(), "retired-pool-preset.json")
			writeSyntheticAgentPreset(t, projectDir, "pool-agent", presetRef, provider, "gpt-5.6-sol", "", false)
			if got := validateCodexAuthForAgents(globalDir, projectDir); got != "" {
				t.Fatalf("retired %s provider produced Codex OAuth warning: %q", provider, got)
			}
		})
	}
}

func TestFirstRunPickerGuardsUnauthenticatedCodex(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	i18n.SetLang("en")
	globalDir := t.TempDir()

	m := firstRunPickerModel(t, globalDir, syntheticPickerPreset("codex", "gpt-5.6-sol"))
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.step != stepPickPreset || m.cursor != 0 {
		t.Fatalf("unauthenticated Codex selection moved: step=%v cursor=%d", m.step, m.cursor)
	}
	hint := i18n.T("firstrun.preset_pick.codex_needs_oauth_hint")
	if m.message != hint {
		t.Fatalf("picker message = %q, want %q", m.message, hint)
	}
	if !strings.Contains(m.View(), hint) {
		t.Fatalf("picker view did not visibly mark unauthenticated Codex; view=%s", m.View())
	}
}

func TestFirstRunPickerAllowsAuthenticatedCodex(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	i18n.SetLang("en")
	globalDir := t.TempDir()
	writeStubCodexToken(t, legacyCodexAuthPath(globalDir), "legacy@example.test")

	m := firstRunPickerModel(t, globalDir, syntheticPickerPreset("codex", "gpt-5.6-sol"))
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.step != stepEditPreset || m.message != "" {
		t.Fatalf("authenticated Codex did not advance cleanly: step=%v message=%q", m.step, m.message)
	}
}
