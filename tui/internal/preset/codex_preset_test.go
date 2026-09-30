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

// TestCodexPresetUsesRequestedDefault pins the single-account preset's provider,
// requested model default, and endpoint.
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

// TestCodexPoolPresetRetired pins the removal of the built-in codex-pool
// template: account pooling now lives in the external subs-pool project,
// reached through an ordinary custom OpenAI-compatible Responses preset, so the
// TUI ships no pool template and no longer treats the retired provider
// spellings as a Codex credential family.
func TestCodexPoolPresetRetired(t *testing.T) {
	for _, name := range []string{"codex-pool", "codex_pool"} {
		if _, ok := findBuiltin(name); ok {
			t.Errorf("%s must not be a builtin preset", name)
		}
		if IsBuiltin(name) {
			t.Errorf("IsBuiltin(%q) = true, want false", name)
		}
		if got := ClassifyCredentialFamily(name); got != CredentialFamilyOther {
			t.Errorf("ClassifyCredentialFamily(%q) = %q, want %q", name, got, CredentialFamilyOther)
		}
	}
}
