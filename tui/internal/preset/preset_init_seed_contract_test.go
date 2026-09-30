package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestGenerateInitJSONOmitsSeedCharacterField pins the init contract: the
// generated init.json must not carry a seed-character field at all. The legacy
// `prompt`/`prompt_file` keys are unknown to the kernel (boot warning, never
// honored), and per the project contract the `lingtai`/`lingtai_file` seed is
// *not* written by the TUI either — 灵台 (character) is durable state managed
// after creation via system/lingtai.md / psyche, and the kernel treats a
// missing seed as an empty seed.
func TestGenerateInitJSONOmitsSeedCharacterField(t *testing.T) {
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

	// No legacy seed field — `prompt` is an unknown kernel key (boot warning).
	if _, present := got["prompt"]; present {
		t.Error("init.json must not write legacy `prompt`; it is an unknown kernel field, never honored")
	}
	if _, present := got["prompt_file"]; present {
		t.Error("init.json must not write legacy `prompt_file`")
	}

	// No `lingtai` seed either — character is managed after creation via
	// system/lingtai.md / psyche, not seeded by the TUI.
	if _, present := got["lingtai"]; present {
		t.Error("init.json must not write `lingtai`; 灵台 is managed via system/lingtai.md / psyche, not seeded here")
	}
	if _, present := got["lingtai_file"]; present {
		t.Error("init.json must not write `lingtai_file`")
	}
}
