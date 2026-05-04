package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// seedAgent writes a minimal init.json with the given preset block to
// <lingtaiDir>/<name>/init.json. Test helper.
func seedAgent(t *testing.T, lingtaiDir, name string, presetBlock map[string]interface{}) {
	t.Helper()
	dir := filepath.Join(lingtaiDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	init := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": name,
			"preset":     presetBlock,
		},
	}
	data, _ := json.MarshalIndent(init, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "init.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readPresetBlock(t *testing.T, lingtaiDir, name string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(lingtaiDir, name, "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return raw["manifest"].(map[string]interface{})["preset"].(map[string]interface{})
}

// TestPropagatePresetPolicy_RewritesAllAgents verifies the helper writes
// the new {default, allowed} to every agent and demotes active when
// the agent's old active is no longer in the allowed list.
func TestPropagatePresetPolicy_RewritesAllAgents(t *testing.T) {
	tmp := t.TempDir()

	mimo := "~/.lingtai-tui/presets/saved/mimo-pro.json"
	codex := "~/.lingtai-tui/presets/saved/codex-gpt5.5.json"
	deepseek := "~/.lingtai-tui/presets/saved/deepseek_flash.json"
	zhipu := "~/.lingtai-tui/presets/templates/zhipu.json"

	// Three agents, each on different active presets:
	// - alice: active=codex (will be revoked)
	// - bob: active=deepseek (still allowed)
	// - carol: active=zhipu (will be revoked)
	seedAgent(t, tmp, "alice", map[string]interface{}{
		"active": codex, "default": mimo,
		"allowed": []interface{}{mimo, codex, zhipu},
	})
	seedAgent(t, tmp, "bob", map[string]interface{}{
		"active": deepseek, "default": codex,
		"allowed": []interface{}{deepseek, codex},
	})
	seedAgent(t, tmp, "carol", map[string]interface{}{
		"active": zhipu, "default": zhipu,
		"allowed": []interface{}{zhipu, mimo},
	})
	// human pseudo-agent: must be skipped (no init.json typically, but
	// even if one existed we don't propagate to it).
	if err := os.MkdirAll(filepath.Join(tmp, "human"), 0o755); err != nil {
		t.Fatal(err)
	}

	// New network policy: only mimo and deepseek are allowed; mimo is default.
	count, err := PropagatePresetPolicy(tmp, "" /* no skip */, mimo,
		[]string{mimo, deepseek})
	if err != nil {
		t.Fatalf("PropagatePresetPolicy: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}

	// alice: active codex revoked → demoted to mimo (new default).
	a := readPresetBlock(t, tmp, "alice")
	if a["active"] != mimo {
		t.Errorf("alice active = %v, want %s (demoted from revoked codex)", a["active"], mimo)
	}
	if a["default"] != mimo {
		t.Errorf("alice default = %v, want %s", a["default"], mimo)
	}

	// bob: active deepseek still allowed → preserved. Default rewritten to mimo.
	b := readPresetBlock(t, tmp, "bob")
	if b["active"] != deepseek {
		t.Errorf("bob active = %v, want %s (preserved)", b["active"], deepseek)
	}
	if b["default"] != mimo {
		t.Errorf("bob default = %v, want %s (network policy reset)", b["default"], mimo)
	}

	// carol: active zhipu revoked → demoted. Default also rewritten to mimo
	// even though her old default (zhipu) was hers — /setup is a network
	// policy reset.
	c := readPresetBlock(t, tmp, "carol")
	if c["active"] != mimo {
		t.Errorf("carol active = %v, want %s", c["active"], mimo)
	}
	if c["default"] != mimo {
		t.Errorf("carol default = %v, want %s", c["default"], mimo)
	}

	// All three should have the same allowed list verbatim.
	for _, name := range []string{"alice", "bob", "carol"} {
		got := readPresetBlock(t, tmp, name)
		al := got["allowed"].([]interface{})
		if len(al) != 2 {
			t.Errorf("%s allowed len = %d, want 2; got %v", name, len(al), al)
			continue
		}
		if al[0] != mimo || al[1] != deepseek {
			t.Errorf("%s allowed = %v, want [%s %s]", name, al, mimo, deepseek)
		}
	}
}

// TestPropagatePresetPolicy_SkipsTargetAndHuman verifies that the agent
// passed via skipDir (the wizard's primary target — already saved) and
// the human pseudo-agent are not rewritten.
func TestPropagatePresetPolicy_SkipsTargetAndHuman(t *testing.T) {
	tmp := t.TempDir()
	mimo := "~/.lingtai-tui/presets/saved/mimo-pro.json"
	codex := "~/.lingtai-tui/presets/saved/codex-gpt5.5.json"

	// Wizard's primary target — should NOT be touched by propagation.
	seedAgent(t, tmp, "alice", map[string]interface{}{
		"active": codex, "default": codex,
		"allowed": []interface{}{codex},
	})
	seedAgent(t, tmp, "bob", map[string]interface{}{
		"active": codex, "default": codex,
		"allowed": []interface{}{codex},
	})

	count, _ := PropagatePresetPolicy(tmp, "alice", mimo, []string{mimo})
	if count != 1 {
		t.Errorf("count = %d, want 1 (only bob propagated)", count)
	}

	// alice is unchanged.
	a := readPresetBlock(t, tmp, "alice")
	if a["default"] != codex {
		t.Errorf("alice default mutated despite skip: %v", a["default"])
	}
	if a["active"] != codex {
		t.Errorf("alice active mutated despite skip: %v", a["active"])
	}

	// bob got the network policy.
	b := readPresetBlock(t, tmp, "bob")
	if b["default"] != mimo {
		t.Errorf("bob default = %v, want %s", b["default"], mimo)
	}
	if b["active"] != mimo {
		t.Errorf("bob active = %v, want %s (demoted)", b["active"], mimo)
	}
}

// TestPropagatePresetPolicy_SkipsNonAgentDirs verifies that directories
// without init.json are silently skipped (e.g. anatomy_* sub-projects,
// .secrets, etc).
func TestPropagatePresetPolicy_SkipsNonAgentDirs(t *testing.T) {
	tmp := t.TempDir()
	mimo := "~/.lingtai-tui/presets/saved/mimo-pro.json"
	codex := "~/.lingtai-tui/presets/saved/codex.json"

	seedAgent(t, tmp, "alice", map[string]interface{}{
		"active": codex, "default": codex, "allowed": []interface{}{codex},
	})
	// Non-agent dirs: must not crash, must not be counted.
	for _, name := range []string{"anatomy_intrinsics", ".secrets", "logs"} {
		if err := os.MkdirAll(filepath.Join(tmp, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	count, err := PropagatePresetPolicy(tmp, "", mimo, []string{mimo})
	if err != nil {
		t.Fatalf("PropagatePresetPolicy: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

// TestPropagatePresetPolicy_MalformedAgentDoesntBlockOthers verifies
// that one bad init.json doesn't prevent the rest of the network from
// being rewritten.
func TestPropagatePresetPolicy_MalformedAgentDoesntBlockOthers(t *testing.T) {
	tmp := t.TempDir()
	mimo := "~/.lingtai-tui/presets/saved/mimo-pro.json"
	codex := "~/.lingtai-tui/presets/saved/codex.json"

	seedAgent(t, tmp, "alice", map[string]interface{}{
		"active": codex, "default": codex, "allowed": []interface{}{codex},
	})
	seedAgent(t, tmp, "carol", map[string]interface{}{
		"active": codex, "default": codex, "allowed": []interface{}{codex},
	})
	// bob has malformed init.json
	bobDir := filepath.Join(tmp, "bob")
	os.MkdirAll(bobDir, 0o755)
	os.WriteFile(filepath.Join(bobDir, "init.json"), []byte("not json"), 0o644)

	count, err := PropagatePresetPolicy(tmp, "", mimo, []string{mimo})
	// One agent failed (bob); error should be reported, count should be 2.
	if err == nil {
		t.Errorf("expected first-error reported for malformed agent, got nil")
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (alice and carol succeed despite bob failing)", count)
	}
	// alice and carol are still rewritten.
	a := readPresetBlock(t, tmp, "alice")
	if a["default"] != mimo {
		t.Errorf("alice default = %v, want %s (should propagate even though bob failed)", a["default"], mimo)
	}
}
