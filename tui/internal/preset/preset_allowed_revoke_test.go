package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSetupRevokesActivePresetWhenDeselected verifies that when /setup
// passes an explicit AllowedPresets list that excludes the agent's
// current `active`, the writer demotes `active` to the new default
// rather than secretly re-adding the deselected preset to `allowed`.
//
// Regression: prior code force-added activeRef to allowed unconditionally,
// so the user could not actually remove the running preset from the
// allowed surface via the /setup wizard.
func TestSetupRevokesActivePresetWhenDeselected(t *testing.T) {
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed an existing init.json where the agent has runtime-swapped
	// to an alternate preset (active != default).
	defaultRef := "~/.lingtai-tui/presets/templates/minimax.json"
	swappedRef := "~/.lingtai-tui/presets/saved/zhipu-1.json"
	seed := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "alice",
			"preset": map[string]interface{}{
				"active":  swappedRef,
				"default": defaultRef,
				"allowed": []interface{}{defaultRef, swappedRef},
			},
		},
	}
	seedData, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), seedData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Re-run /setup with an explicit allowed list that excludes the
	// runtime-swapped preset. PreserveActivePreset=true matches what
	// firstrun.go sets in setupMode.
	opts := DefaultAgentOpts()
	opts.AllowedPresets = []string{defaultRef} // user deselected swappedRef
	opts.PreserveActivePreset = true

	if err := GenerateInitJSONWithOpts(minimaxPreset(), "alice", "alice", lingtaiDir, globalDir, opts); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}

	// Verify post-state.
	data, err := os.ReadFile(filepath.Join(agentDir, "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	pre := got["manifest"].(map[string]interface{})["preset"].(map[string]interface{})

	// active must have been snapped to default — the deselected preset
	// is no longer the running one.
	if active := pre["active"]; active != defaultRef {
		t.Errorf("active = %v, want %s (snapped to default after deselect)", active, defaultRef)
	}
	// default must still be the new default the wizard chose.
	if def := pre["default"]; def != defaultRef {
		t.Errorf("default = %v, want %s", def, defaultRef)
	}
	// allowed must NOT contain the deselected preset.
	allowed := pre["allowed"].([]interface{})
	for _, e := range allowed {
		if e == swappedRef {
			t.Errorf("allowed still contains revoked preset %q: %v", swappedRef, allowed)
		}
	}
	if len(allowed) != 1 {
		t.Errorf("allowed length = %d, want 1; got %v", len(allowed), allowed)
	}
}

// TestSetupKeepsActiveWhenStillAllowed verifies the happy path: when
// the user's new allowed list still includes the current active preset,
// active is preserved (PreserveActivePreset=true semantics) and the
// running agent is not yanked off its current preset.
func TestSetupKeepsActiveWhenStillAllowed(t *testing.T) {
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "bob")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	defaultRef := "~/.lingtai-tui/presets/templates/minimax.json"
	swappedRef := "~/.lingtai-tui/presets/saved/zhipu-1.json"
	seed := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "bob",
			"preset": map[string]interface{}{
				"active":  swappedRef,
				"default": defaultRef,
				"allowed": []interface{}{defaultRef, swappedRef},
			},
		},
	}
	seedData, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), seedData, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := DefaultAgentOpts()
	opts.AllowedPresets = []string{defaultRef, swappedRef} // both still allowed
	opts.PreserveActivePreset = true

	if err := GenerateInitJSONWithOpts(minimaxPreset(), "bob", "bob", lingtaiDir, globalDir, opts); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(agentDir, "init.json"))
	var got map[string]interface{}
	json.Unmarshal(data, &got)
	pre := got["manifest"].(map[string]interface{})["preset"].(map[string]interface{})

	// active stays put — user kept it allowed, so no demotion.
	if active := pre["active"]; active != swappedRef {
		t.Errorf("active = %v, want %s (preserved)", active, swappedRef)
	}
	// default tracks the new wizard choice.
	if def := pre["default"]; def != defaultRef {
		t.Errorf("default = %v, want %s", def, defaultRef)
	}
	// allowed should contain both, no extras.
	allowed := pre["allowed"].([]interface{})
	if len(allowed) != 2 {
		t.Errorf("allowed len = %d, want 2; got %v", len(allowed), allowed)
	}
}
