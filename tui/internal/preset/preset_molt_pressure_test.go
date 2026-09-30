// Regression guard for the molt_pressure field after Jason #4135/#4137: the
// kernel no longer accepts configurable context.molt thresholds, so new
// TUI-generated init.json files must not include manifest.molt_pressure.
// Existing on-disk init.json files that still carry the key are left as-is
// (no migration/rewrite); the kernel simply ignores the stale key.
package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateInitJSONOmitsMoltPressure(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	globalDir := filepath.Join(tmp, ".lingtai-tui")
	agentDir := filepath.Join(lingtaiDir, "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := GenerateInitJSONWithOpts(codexPreset(), "alice", "alice", lingtaiDir, globalDir, DefaultAgentOpts()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(agentDir, "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	manifest, ok := got["manifest"].(map[string]interface{})
	if !ok {
		t.Fatalf("manifest not a dict: %T", got["manifest"])
	}
	if _, present := manifest["molt_pressure"]; present {
		t.Fatal("manifest.molt_pressure must not be written — the kernel no longer accepts configurable molt thresholds (Jason #4135/#4137)")
	}
}
