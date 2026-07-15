// Verifies that GenerateInitJSONWithOpts writes the new list-shape addons +
// matching mcp activation entries pointing at the local venv Python.
//
// This is the wire that satisfies "default include five addons; activation
// points to local installation, not remote". When this test passes, brand-new
// agents created by the TUI wizard (or rehydrated from setup mode) ship with
// all five curated MCPs registered + activated against the local venv where
// `pip install lingtai` placed the curated lingtai.mcp_servers modules.
package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateInitJSONWritesNewShapeWithLocalVenv(t *testing.T) {
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

	p := DefaultPreset()
	opts := AgentOpts{
		Addons: []string{"imap", "telegram", "feishu", "wechat", "whatsapp"},
	}
	if err := GenerateInitJSONWithOpts(p, "alice", "alice", lingtaiDir, globalDir, opts); err != nil {
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

	// addons must be the new list shape.
	addons, ok := got["addons"].([]interface{})
	if !ok {
		t.Fatalf("addons not a list: %T (%v)", got["addons"], got["addons"])
	}
	wantNames := map[string]bool{"imap": true, "telegram": true, "feishu": true, "wechat": true, "whatsapp": true}
	if len(addons) != len(wantNames) {
		t.Errorf("addons len = %d, want %d (%v)", len(addons), len(wantNames), addons)
	}
	for _, raw := range addons {
		if name, ok := raw.(string); ok {
			delete(wantNames, name)
		}
	}
	if len(wantNames) > 0 {
		t.Errorf("missing addon names: %v", wantNames)
	}

	// mcp section must exist with one entry per addon and exact module/env
	// wiring. In particular, generated args must never regress to the removed
	// top-level compatibility wrappers.
	mcp, ok := got["mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcp not a dict: %T (%v)", got["mcp"], got["mcp"])
	}
	wantSpecs := map[string]struct {
		module string
		envVar string
		config string
	}{
		"imap":     {"lingtai.mcp_servers.imap", "LINGTAI_IMAP_CONFIG", filepath.Join(".secrets", "imap.json")},
		"telegram": {"lingtai.mcp_servers.telegram", "LINGTAI_TELEGRAM_CONFIG", filepath.Join(".secrets", "telegram.json")},
		"feishu":   {"lingtai.mcp_servers.feishu", "LINGTAI_FEISHU_CONFIG", filepath.Join(".secrets", "feishu.json")},
		"wechat":   {"lingtai.mcp_servers.wechat", "LINGTAI_WECHAT_CONFIG", filepath.Join(".secrets", "wechat", "config.json")},
		"whatsapp": {"lingtai.mcp_servers.whatsapp", "LINGTAI_WHATSAPP_CONFIG", filepath.Join(".secrets", "whatsapp.json")},
	}
	for name, want := range wantSpecs {
		entry, ok := mcp[name].(map[string]interface{})
		if !ok {
			t.Errorf("mcp.%s missing or wrong type: %T", name, mcp[name])
			continue
		}
		if entry["type"] != "stdio" {
			t.Errorf("mcp.%s.type = %v, want stdio", name, entry["type"])
		}
		// command must point inside the venv we passed (the "local install" wire).
		expectedVenvFragment := filepath.Join(globalDir, "runtime", "venv")
		cmd, _ := entry["command"].(string)
		if !strings.HasPrefix(cmd, expectedVenvFragment) {
			t.Errorf("mcp.%s.command = %q, want path under %q", name, cmd, expectedVenvFragment)
		}
		args, _ := entry["args"].([]interface{})
		if len(args) != 2 || args[0] != "-m" || args[1] != want.module {
			t.Errorf("mcp.%s.args = %v, want [-m %s]", name, args, want.module)
		}
		env, _ := entry["env"].(map[string]interface{})
		if env[want.envVar] != want.config {
			t.Errorf("mcp.%s.env[%s] = %v, want %q", name, want.envVar, env[want.envVar], want.config)
		}
	}

	// venv_path top-level field must also point at the same local venv.
	venvPath, _ := got["venv_path"].(string)
	if venvPath != filepath.Join(globalDir, "runtime", "venv") {
		t.Errorf("venv_path = %q, want %q", venvPath, filepath.Join(globalDir, "runtime", "venv"))
	}
}

// Verifies that running GenerateInitJSONWithOpts twice — once for a fresh
// agent, then again with extra addon selections — produces the right
// preserve-vs-add behavior: pre-existing addons are kept verbatim from the
// previous init.json, so user edits are never clobbered by /setup.
func TestGenerateInitJSONPreservesExistingAddons(t *testing.T) {
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

	p := DefaultPreset()

	// First creation: only imap.
	if err := GenerateInitJSONWithOpts(p, "alice", "alice", lingtaiDir, globalDir, AgentOpts{
		Addons: []string{"imap"},
	}); err != nil {
		t.Fatal(err)
	}

	// User then hand-edits init.json to add a custom mcp entry the wizard
	// doesn't know about (e.g. a third-party MCP they registered themselves).
	initPath := filepath.Join(agentDir, "init.json")
	data, _ := os.ReadFile(initPath)
	var doc map[string]interface{}
	json.Unmarshal(data, &doc)
	mcp, _ := doc["mcp"].(map[string]interface{})
	mcp["custom-mcp"] = map[string]interface{}{
		"type":    "stdio",
		"command": "/opt/custom/python",
		"args":    []interface{}{"-m", "my_custom"},
	}
	doc["mcp"] = mcp
	updated, _ := json.MarshalIndent(doc, "", "  ")
	os.WriteFile(initPath, updated, 0o644)

	// Re-run with a different addon selection (telegram).
	if err := GenerateInitJSONWithOpts(p, "alice", "alice", lingtaiDir, globalDir, AgentOpts{
		Addons: []string{"telegram"},
	}); err != nil {
		t.Fatal(err)
	}

	// Pre-existing addons (imap) should be kept; opts.Addons (telegram) ignored
	// because the existing list takes precedence — user edits win.
	updatedData, _ := os.ReadFile(initPath)
	var got map[string]interface{}
	json.Unmarshal(updatedData, &got)
	addons, _ := got["addons"].([]interface{})
	if len(addons) != 1 || addons[0] != "imap" {
		t.Errorf("addons should be preserved as [imap], got %v", addons)
	}

	// Custom mcp entry must survive.
	gotMCP, _ := got["mcp"].(map[string]interface{})
	custom, _ := gotMCP["custom-mcp"].(map[string]interface{})
	if custom["command"] != "/opt/custom/python" {
		t.Errorf("custom-mcp clobbered: %v", gotMCP["custom-mcp"])
	}
	// imap mcp entry must still exist (it was in the original write, and the
	// new pass shouldn't drop it since imap is still in the addons list).
	if _, ok := gotMCP["imap"]; !ok {
		t.Errorf("imap mcp activation lost; got mcp keys = %v", keysOf(gotMCP))
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Verifies legacy dict-shape addons in an existing init.json get normalized
// to the new list shape on the next regen — the m028 migration handles the
// explicit case, but if a user somehow ends up with the legacy shape (e.g.
// edited init.json by hand), GenerateInitJSONWithOpts should normalize on
// the next /setup pass without losing the addon names.
func TestGenerateInitJSONNormalizesLegacyDictShape(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	globalDir := filepath.Join(tmp, ".lingtai-tui")
	agentDir := filepath.Join(lingtaiDir, "alice")
	os.MkdirAll(agentDir, 0o755)
	os.MkdirAll(globalDir, 0o755)

	// Seed a legacy dict-shape init.json.
	legacy := map[string]interface{}{
		"manifest": map[string]interface{}{},
		"addons": map[string]interface{}{
			"imap":     map[string]interface{}{"config": ".secrets/imap.json"},
			"telegram": map[string]interface{}{"config": ".secrets/telegram.json"},
		},
	}
	data, _ := json.Marshal(legacy)
	os.WriteFile(filepath.Join(agentDir, "init.json"), data, 0o644)

	p := DefaultPreset()
	if err := GenerateInitJSONWithOpts(p, "alice", "alice", lingtaiDir, globalDir, AgentOpts{
		Addons: []string{"feishu"}, // should be ignored — legacy names take precedence
	}); err != nil {
		t.Fatal(err)
	}

	updatedData, _ := os.ReadFile(filepath.Join(agentDir, "init.json"))
	var got map[string]interface{}
	json.Unmarshal(updatedData, &got)
	addons, ok := got["addons"].([]interface{})
	if !ok {
		t.Fatalf("addons should now be a list, got %T (%v)", got["addons"], got["addons"])
	}
	names := map[string]bool{}
	for _, raw := range addons {
		if s, ok := raw.(string); ok {
			names[s] = true
		}
	}
	if !names["imap"] || !names["telegram"] {
		t.Errorf("expected imap+telegram preserved from legacy dict, got %v", addons)
	}
	if names["feishu"] {
		t.Errorf("opts.Addons should NOT have been merged; got %v", addons)
	}
}

// Cosmetic: confirm runtime.GOOS-aware venv python path resolution
// produces a sensible string. Not a behavior test; just makes sure the
// fragment matching in TestGenerateInitJSONWritesNewShapeWithLocalVenv
// uses the right separator on Windows.
func TestVenvPythonPathFragment(t *testing.T) {
	if runtime.GOOS == "windows" && filepath.Separator != '\\' {
		t.Errorf("unexpected separator on windows: %q", filepath.Separator)
	}
}
