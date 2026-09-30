// Regression guard for the molt_prompt field after Jason #4140: the kernel no
// longer accepts or reads a configurable molt_prompt (context.molt messages are
// hardcoded kernel-side), so the TUI config generator must not write it into
// new init.json files. Existing on-disk init.json files that still carry the
// key are left as-is (no migration/rewrite); the kernel simply ignores the
// stale key.
package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateInitJSONOmitsMoltPrompt(t *testing.T) {
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

	opts := DefaultAgentOpts()
	if err := GenerateInitJSONWithOpts(codexPreset(), "alice", "alice", lingtaiDir, globalDir, opts); err != nil {
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
	if _, present := manifest["molt_prompt"]; present {
		t.Fatal("manifest.molt_prompt must not be written — the kernel no longer accepts a configurable molt_prompt (Jason #4140)")
	}
}
