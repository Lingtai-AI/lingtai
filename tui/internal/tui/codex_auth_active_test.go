package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// withTempCodexHome points $HOME at a fresh temp dir so preset.List/Save (which
// resolve presets/saved under os.UserHomeDir) and codexAuthRefForPath (which
// home-shortens absolute paths) operate against the temp tree, never the real
// user's presets or credentials. Returns the temp home and the matching
// globalDir (~/.lingtai-tui) the production code passes around.
func withTempCodexHome(t *testing.T) (home, globalDir string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	globalDir = filepath.Join(home, ".lingtai-tui")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir globalDir: %v", err)
	}
	return home, globalDir
}

// saveCodexPresetForTest writes a saved-dir Codex preset with the given name and
// codex_auth_path (omitted when ref==""), mirroring what the editor/Save path
// produces. Source is SourceSaved because Save() always lands in saved/.
func saveCodexPresetForTest(t *testing.T, name, ref string) {
	t.Helper()
	llm := map[string]interface{}{"provider": "codex", "model": "gpt-5.6-sol"}
	if ref != "" {
		llm["codex_auth_path"] = ref
	}
	p := preset.Preset{
		Name:        name,
		Description: preset.PresetDescription{Summary: "codex test preset"},
		Manifest:    map[string]interface{}{"llm": llm},
	}
	if err := preset.Save(p); err != nil {
		t.Fatalf("save codex preset %q: %v", name, err)
	}
}

// saveNonCodexPresetForTest writes a saved-dir non-Codex preset so we can prove
// the batch apply never touches presets bound to other providers.
func saveNonCodexPresetForTest(t *testing.T, name string) {
	t.Helper()
	p := preset.Preset{
		Name:        name,
		Description: preset.PresetDescription{Summary: "non-codex test preset"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{"provider": "zhipu", "model": "glm-4"},
		},
	}
	if err := preset.Save(p); err != nil {
		t.Fatalf("save non-codex preset %q: %v", name, err)
	}
}

func reloadSavedRef(t *testing.T, name string) (string, bool) {
	t.Helper()
	p, err := preset.Load(name)
	if err != nil {
		t.Fatalf("reload preset %q: %v", name, err)
	}
	llm, _ := p.Manifest["llm"].(map[string]interface{})
	v, ok := llm["codex_auth_path"]
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return s, true
}

// TestApplyActiveCodexAccount_RewritesOnlySavedCodexPresets verifies the batch
// apply points every saved Codex preset at the chosen account's ref, leaves
// non-Codex saved presets untouched, and reports the count it changed.
func TestApplyActiveCodexAccount_RewritesOnlySavedCodexPresets(t *testing.T) {
	_, globalDir := withTempCodexHome(t)

	saveCodexPresetForTest(t, "codex-a", "") // legacy-bound to start
	saveCodexPresetForTest(t, "codex-b", "~/.lingtai-tui/codex-auth/old.json")
	saveNonCodexPresetForTest(t, "zhipu-1")

	// Target a per-account file under codex-auth/.
	target := filepath.Join(globalDir, codexAuthSubdir, "work.json")
	wantRef := codexAuthRefForPath(globalDir, target)
	if wantRef == "" {
		t.Fatalf("precondition: a per-account target should map to a non-empty ref")
	}

	updated, err := applyActiveCodexAccount(globalDir, target)
	if err != nil {
		t.Fatalf("applyActiveCodexAccount error: %v", err)
	}
	if updated != 2 {
		t.Fatalf("updated = %d, want 2 (two saved codex presets)", updated)
	}

	for _, name := range []string{"codex-a", "codex-b"} {
		ref, ok := reloadSavedRef(t, name)
		if !ok || ref != wantRef {
			t.Errorf("preset %q ref = %q (present=%v), want %q", name, ref, ok, wantRef)
		}
	}
	// Non-codex preset must be unchanged (still no codex_auth_path key).
	if _, ok := reloadSavedRef(t, "zhipu-1"); ok {
		t.Error("non-codex preset must not gain a codex_auth_path")
	}
}

// TestApplyActiveCodexAccount_LegacyClearsRef verifies selecting the legacy
// account resets saved Codex presets to the implicit fallback by REMOVING the
// codex_auth_path key (empty ref), not leaving an empty string behind.
func TestApplyActiveCodexAccount_LegacyClearsRef(t *testing.T) {
	_, globalDir := withTempCodexHome(t)

	saveCodexPresetForTest(t, "codex-a", "~/.lingtai-tui/codex-auth/work.json")

	legacy := legacyCodexAuthPath(globalDir)
	updated, err := applyActiveCodexAccount(globalDir, legacy)
	if err != nil {
		t.Fatalf("applyActiveCodexAccount(legacy) error: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}
	if ref, ok := reloadSavedRef(t, "codex-a"); ok {
		t.Errorf("legacy selection should remove codex_auth_path; got %q (present=%v)", ref, ok)
	}
}

// TestApplyActiveCodexAccount_NoSavedPresets verifies the graceful path: with no
// saved Codex presets the apply changes nothing and returns count 0 without
// error.
func TestApplyActiveCodexAccount_NoSavedPresets(t *testing.T) {
	_, globalDir := withTempCodexHome(t)
	// Only a non-codex saved preset exists.
	saveNonCodexPresetForTest(t, "zhipu-1")

	target := filepath.Join(globalDir, codexAuthSubdir, "work.json")
	updated, err := applyActiveCodexAccount(globalDir, target)
	if err != nil {
		t.Fatalf("applyActiveCodexAccount error: %v", err)
	}
	if updated != 0 {
		t.Fatalf("updated = %d, want 0 (no saved codex presets)", updated)
	}
}

// TestApplyActiveCodexAccount_IgnoresTemplates verifies templates are never
// rewritten — only user-owned saved presets. A template-named codex preset that
// only exists in templates/ must keep its shipped form.
func TestApplyActiveCodexAccount_IgnoresTemplates(t *testing.T) {
	_, globalDir := withTempCodexHome(t)
	// Materialize the shipped templates (includes the "codex" template).
	if err := preset.RefreshTemplates(); err != nil {
		t.Fatalf("RefreshTemplates: %v", err)
	}

	target := filepath.Join(globalDir, codexAuthSubdir, "work.json")
	updated, err := applyActiveCodexAccount(globalDir, target)
	if err != nil {
		t.Fatalf("applyActiveCodexAccount error: %v", err)
	}
	if updated != 0 {
		t.Fatalf("updated = %d, want 0 (templates must be ignored)", updated)
	}
	// The codex TEMPLATE must remain unbound (no codex_auth_path) — Load
	// prefers saved/ but none was written, so this is the template form.
	p, err := preset.Load("codex")
	if err != nil {
		t.Fatalf("load codex template: %v", err)
	}
	if !preset.IsTemplate(p) {
		t.Fatalf("expected to load the codex TEMPLATE; source=%v", p.Source)
	}
	llm, _ := p.Manifest["llm"].(map[string]interface{})
	if _, ok := llm["codex_auth_path"]; ok {
		t.Error("codex template must not be rewritten with a codex_auth_path")
	}
}

// TestActiveCodexAuthPath_AllAgree verifies the active-account derivation:
// when every saved Codex preset shares one ref, that resolved path is returned.
func TestActiveCodexAuthPath_AllAgree(t *testing.T) {
	_, globalDir := withTempCodexHome(t)

	ref := "~/.lingtai-tui/codex-auth/work.json"
	saveCodexPresetForTest(t, "codex-a", ref)
	saveCodexPresetForTest(t, "codex-b", ref)

	got, ok := activeCodexAuthPath(globalDir)
	if !ok {
		t.Fatal("expected an active path when all saved codex presets agree")
	}
	want := resolveCodexAuthPath(globalDir, ref)
	if got != want {
		t.Fatalf("active path = %q, want %q", got, want)
	}
}

// TestActiveCodexAuthPath_AllLegacy verifies that when all saved Codex presets
// are legacy-bound (no codex_auth_path), the derived active path is the legacy
// file — so the legacy account row gets the (active) marker.
func TestActiveCodexAuthPath_AllLegacy(t *testing.T) {
	_, globalDir := withTempCodexHome(t)

	saveCodexPresetForTest(t, "codex-a", "")
	saveCodexPresetForTest(t, "codex-b", "")

	got, ok := activeCodexAuthPath(globalDir)
	if !ok {
		t.Fatal("expected an active path when all saved codex presets are legacy-bound")
	}
	if got != legacyCodexAuthPath(globalDir) {
		t.Fatalf("active path = %q, want legacy %q", got, legacyCodexAuthPath(globalDir))
	}
}

// TestActiveCodexAuthPath_Mixed verifies that disagreeing saved Codex presets
// yield no active path (nothing gets the marker).
func TestActiveCodexAuthPath_Mixed(t *testing.T) {
	_, globalDir := withTempCodexHome(t)

	saveCodexPresetForTest(t, "codex-a", "~/.lingtai-tui/codex-auth/work.json")
	saveCodexPresetForTest(t, "codex-b", "~/.lingtai-tui/codex-auth/home.json")

	if got, ok := activeCodexAuthPath(globalDir); ok {
		t.Fatalf("mixed presets must yield no active path; got %q", got)
	}
}

// TestActiveCodexAuthPath_NoSavedPresets verifies absence of saved Codex presets
// yields no active path.
func TestActiveCodexAuthPath_NoSavedPresets(t *testing.T) {
	_, globalDir := withTempCodexHome(t)
	saveNonCodexPresetForTest(t, "zhipu-1")

	if got, ok := activeCodexAuthPath(globalDir); ok {
		t.Fatalf("no saved codex presets must yield no active path; got %q", got)
	}
}
