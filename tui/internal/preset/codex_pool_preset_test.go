package preset

import "testing"

// findBuiltin returns the builtin preset with the given name, or a zero Preset
// and false when absent.
func findBuiltin(name string) (Preset, bool) {
	for _, p := range BuiltinPresets() {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

// llmOf returns the manifest.llm map for a preset (nil when absent).
func llmOf(p Preset) map[string]interface{} {
	llm, _ := p.Manifest["llm"].(map[string]interface{})
	return llm
}

// TestCodexPoolPresetExists verifies the new codex-pool template is a builtin
// bound to the codex-pool provider, mirroring the codex preset's model/endpoint.
func TestCodexPoolPresetExists(t *testing.T) {
	p, ok := findBuiltin("codex-pool")
	if !ok {
		t.Fatal("codex-pool preset should be a builtin")
	}
	llm := llmOf(p)
	if llm == nil {
		t.Fatal("codex-pool preset must have an llm map")
	}
	if prov, _ := llm["provider"].(string); prov != "codex-pool" {
		t.Errorf("codex-pool provider = %q, want %q", prov, "codex-pool")
	}
	if model, _ := llm["model"].(string); model != "gpt-5.6-sol" {
		t.Errorf("codex-pool model = %q, want gpt-5.6-sol", model)
	}
	// It must pass the same validation gauntlet as any other preset.
	if errs := p.Validate(); len(errs) != 0 {
		t.Errorf("codex-pool preset failed validation: %v", errs)
	}
}

// TestCodexPresetUsesRequestedDefault pins the single-account preset's provider,
// requested model default, and endpoint independently from codex-pool.
func TestCodexPresetUsesRequestedDefault(t *testing.T) {
	p, ok := findBuiltin("codex")
	if !ok {
		t.Fatal("codex preset should still be a builtin")
	}
	llm := llmOf(p)
	if prov, _ := llm["provider"].(string); prov != "codex" {
		t.Errorf("codex provider = %q, want %q", prov, "codex")
	}
	if model, _ := llm["model"].(string); model != "gpt-5.6-sol" {
		t.Errorf("codex model = %q, want gpt-5.6-sol", model)
	}
	if base, _ := llm["base_url"].(string); base != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("codex base_url changed: %q", base)
	}
}

// TestCodexPoolProviderCredentialValidity verifies that global OAuth existence
// is not confused with pool membership or validated-empty fallback readiness.
func TestCodexPoolProviderCredentialValidity(t *testing.T) {
	// Point HOME at a temp dir so Save() and the "~/"-prefixed ref below resolve
	// against the test tree, never the real user's presets.
	t.Setenv("HOME", t.TempDir())

	p := codexPoolPreset()
	if err := Save(p); err != nil {
		// Save lands in saved/; that's fine for resolving the ref below.
		t.Fatalf("save codex-pool preset: %v", err)
	}
	ref := "~/.lingtai-tui/presets/saved/codex-pool.json"

	// Without OAuth configured → not valid.
	withoutAuth := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexOAuthConfigured: false})
	if len(withoutAuth) == 1 && withoutAuth[0].Exists && withoutAuth[0].HasKey {
		t.Error("codex-pool must not be credential-valid when no Codex OAuth is configured")
	}

	withAuth := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexOAuthConfigured: true})
	if len(withAuth) != 1 || !withAuth[0].Exists {
		t.Fatalf("expected 1 existing resolved ref; got %#v", withAuth)
	}
	if withAuth[0].HasKey {
		t.Error("global Codex OAuth alone must not make codex-pool valid")
	}

	withFlat := ResolveRefsWithAuth([]string{ref}, nil, AuthState{CodexPoolEligible: true})
	if !withFlat[0].HasKey {
		t.Error("flat pool eligibility must make codex-pool valid")
	}
	withClassified := ResolveRefsWithAuth([]string{ref}, nil, AuthState{
		CodexPoolEligibleModels: map[string]bool{"gpt-5.6-sol": true},
	})
	if !withClassified[0].HasKey {
		t.Error("exact classified-model eligibility must make codex-pool valid")
	}
	withWrongModel := ResolveRefsWithAuth([]string{ref}, nil, AuthState{
		CodexPoolEligibleModels: map[string]bool{"other-model": true},
	})
	if withWrongModel[0].HasKey {
		t.Error("a different classified model must not make codex-pool valid")
	}
}
