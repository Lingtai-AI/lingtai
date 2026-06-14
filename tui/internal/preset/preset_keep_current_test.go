package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupModeKeepCurrentPresetPolicyDoesNotWriteSyntheticPreset is the
// regression test for issue #316.
//
// Scenario: agent has codex as active/default/allowed. User runs /setup,
// picks "Keep current preset" (cursor==-1 → synthetic preset Name="keep_current"),
// then on the allowed page also authorises zhipu-2. On save, the writer must:
//   - NOT write keep_current.json anywhere in allowed or default
//   - preserve codex as the policy default
//   - preserve codex as active (PreserveActivePreset semantics)
//   - include both codex and zhipu-2 in allowed
func TestSetupModeKeepCurrentPresetPolicyDoesNotWriteSyntheticPreset(t *testing.T) {
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "codex-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	codexRef := "~/.lingtai-tui/presets/templates/codex.json"
	zhipuRef := "~/.lingtai-tui/presets/saved/zhipu-2.json"

	// Seed an existing init.json where active/default/allowed all point at
	// the codex template preset.
	seed := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "codex-agent",
			"preset": map[string]interface{}{
				"active":  codexRef,
				"default": codexRef,
				"allowed": []interface{}{codexRef},
			},
		},
	}
	seedData, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), seedData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate /setup keep-current: synthetic preset with Name="keep_current",
	// zero Source (not a real on-disk file).  This is exactly what firstrun.go
	// builds in NewSetupModeModel when cursor==-1.
	synthetic := Preset{
		Name: "keep_current",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "codex",
				"model":    "gpt-4o",
			},
		},
	}

	// User also allowed zhipu-2 on the preset-policy page.
	opts := DefaultAgentOpts()
	opts.AllowedPresets = []string{codexRef, zhipuRef}
	opts.PreserveActivePreset = true

	if err := GenerateInitJSONWithOpts(synthetic, "codex-agent", "codex-agent", lingtaiDir, globalDir, opts); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(agentDir, "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	pre := got["manifest"].(map[string]interface{})["preset"].(map[string]interface{})

	// default must remain the real codex ref, not keep_current.json
	if def := pre["default"]; def != codexRef {
		t.Errorf("default = %q, want %q (synthetic keep_current must never be written as default)", def, codexRef)
	}

	// active must remain codex (PreserveActivePreset)
	if active := pre["active"]; active != codexRef {
		t.Errorf("active = %q, want %q", active, codexRef)
	}

	// allowed must contain both real presets and must NOT contain keep_current
	allowed, _ := pre["allowed"].([]interface{})
	allowedStrs := make([]string, 0, len(allowed))
	for _, e := range allowed {
		if s, ok := e.(string); ok {
			allowedStrs = append(allowedStrs, s)
		}
	}

	for _, s := range allowedStrs {
		if strings.Contains(s, "keep_current") {
			t.Errorf("allowed contains synthetic keep_current ref %q; full allowed: %v", s, allowedStrs)
		}
	}

	hasCodex := false
	hasZhipu := false
	for _, s := range allowedStrs {
		if s == codexRef {
			hasCodex = true
		}
		if s == zhipuRef {
			hasZhipu = true
		}
	}
	if !hasCodex {
		t.Errorf("allowed missing codex ref %q; got %v", codexRef, allowedStrs)
	}
	if !hasZhipu {
		t.Errorf("allowed missing zhipu-2 ref %q; got %v", zhipuRef, allowedStrs)
	}
	if len(allowedStrs) != 2 {
		t.Errorf("allowed has %d entries, want 2; got %v", len(allowedStrs), allowedStrs)
	}
}

// TestSetupModeKeepCurrentNoExtraAllowed verifies that when "Keep current
// preset" is chosen and the user does not add any extra presets to the
// allowed list (opts.AllowedPresets is nil), the result preserves the
// existing allowed list from init.json unchanged and never writes
// keep_current.json as the default.
func TestSetupModeKeepCurrentNoExtraAllowed(t *testing.T) {
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	codexRef := "~/.lingtai-tui/presets/templates/codex.json"
	zhipuRef := "~/.lingtai-tui/presets/saved/zhipu-1.json"

	seed := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "alice",
			"preset": map[string]interface{}{
				"active":  codexRef,
				"default": codexRef,
				"allowed": []interface{}{codexRef, zhipuRef},
			},
		},
	}
	seedData, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), seedData, 0o644); err != nil {
		t.Fatal(err)
	}

	synthetic := Preset{
		Name: "keep_current",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{"provider": "codex"},
		},
	}

	opts := DefaultAgentOpts()
	opts.PreserveActivePreset = true
	// AllowedPresets deliberately not set — user skipped the preset-policy page

	if err := GenerateInitJSONWithOpts(synthetic, "alice", "alice", lingtaiDir, globalDir, opts); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(agentDir, "init.json"))
	var got map[string]interface{}
	json.Unmarshal(data, &got)
	pre := got["manifest"].(map[string]interface{})["preset"].(map[string]interface{})

	if def := pre["default"].(string); strings.Contains(def, "keep_current") {
		t.Errorf("default = %q must not reference keep_current", def)
	}
	if def := pre["default"]; def != codexRef {
		t.Errorf("default = %q, want %q", def, codexRef)
	}
	if active := pre["active"]; active != codexRef {
		t.Errorf("active = %q, want %q", active, codexRef)
	}
}
