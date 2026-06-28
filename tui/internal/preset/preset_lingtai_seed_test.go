package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateInitJSONWritesLingtaiSeedNotLegacyPrompt(t *testing.T) {
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

	if err := GenerateInitJSONWithOpts(DefaultPreset(), "alice", "alice", lingtaiDir, globalDir, DefaultAgentOpts()); err != nil {
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

	if _, present := got["prompt"]; present {
		t.Fatal("init.json must not write legacy prompt; kernel no longer treats it as lingtai")
	}
	if seed, present := got["lingtai"]; !present {
		t.Fatal("init.json should write the current lingtai seed field")
	} else if seed != "" {
		t.Fatalf("lingtai seed = %#v, want empty string", seed)
	}
}
