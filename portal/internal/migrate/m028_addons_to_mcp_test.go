package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestM028AddonSpecsUseBundledModules(t *testing.T) {
	want := map[string]addonSpec{
		"imap":     {module: "lingtai.mcp_servers.imap", envVarName: "LINGTAI_IMAP_CONFIG", defaultRel: ".secrets/imap.json"},
		"telegram": {module: "lingtai.mcp_servers.telegram", envVarName: "LINGTAI_TELEGRAM_CONFIG", defaultRel: ".secrets/telegram.json"},
		"feishu":   {module: "lingtai.mcp_servers.feishu", envVarName: "LINGTAI_FEISHU_CONFIG", defaultRel: ".secrets/feishu.json"},
		"wechat":   {module: "lingtai.mcp_servers.wechat", envVarName: "LINGTAI_WECHAT_CONFIG", defaultRel: ".secrets/wechat/config.json"},
		"whatsapp": {module: "lingtai.mcp_servers.whatsapp", envVarName: "LINGTAI_WHATSAPP_CONFIG", defaultRel: ".secrets/whatsapp.json"},
	}

	if len(addonSpecs) != len(want) {
		t.Fatalf("addonSpecs has %d entries, want %d: %#v", len(addonSpecs), len(want), addonSpecs)
	}
	for name, wantSpec := range want {
		got, ok := addonSpecs[name]
		if !ok {
			t.Errorf("addonSpecs missing %q", name)
			continue
		}
		if got != wantSpec {
			t.Errorf("addonSpecs[%q] = %#v, want %#v", name, got, wantSpec)
		}
	}
}

// TestM028AddonKeyReMatchesTUIModuleIdentifierContract mirrors the TUI's
// legacy addon key validation contract (tui/internal/config.ValidateAddonKey):
// keys containing semicolons or any other source-boundary/non-identifier
// characters are not valid addon/module identifiers and must be rejected.
func TestM028AddonKeyReMatchesTUIModuleIdentifierContract(t *testing.T) {
	valid := []string{"imap", "telegram", "_private", "a1", "x_y", "imap.telegram"}
	invalid := []string{
		"", "imap;rm", ";", "imap\nprint(1)", "imap#comment", "imap x",
		"1imap", "-imap", ".imap", "imap.", "imap..x", "imap\"x",
		"imap'x", "imap\\x", "imap/x", "__import__('os')",
		"import os; os.system('id')",
	}
	for _, name := range valid {
		if !addonKeyRe.MatchString(name) {
			t.Errorf("addonKeyRe should accept %q", name)
		}
	}
	for _, name := range invalid {
		if addonKeyRe.MatchString(name) {
			t.Errorf("addonKeyRe must reject %q", name)
		}
	}
}

// TestM028SkipsInvalidAddonKey proves the same contract applies when the
// portal reads legacy addon objects: a key with a source-boundary character
// is skipped and never carried into the converted file, while valid keys are
// still converted.
func TestM028SkipsInvalidAddonKey(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initPath := filepath.Join(agentDir, "init.json")
	doc := map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons": map[string]interface{}{
			"imap":    map[string]interface{}{"config": ".secrets/imap.json"},
			"imap;rm": map[string]interface{}{"config": ".secrets/evil.json"},
		},
	}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(initPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := convertAddonsInInitFile(initPath, agentDir, ""); err != nil {
		t.Fatal(err)
	}

	updated, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(updated, &got); err != nil {
		t.Fatal(err)
	}
	addons, _ := got["addons"].([]interface{})
	if len(addons) != 1 || addons[0] != "imap" {
		t.Errorf("addons = %v, want [\"imap\"]", addons)
	}
	mcp, _ := got["mcp"].(map[string]interface{})
	if _, ok := mcp["imap;rm"]; ok {
		t.Error("invalid addon key should not have an mcp entry")
	}
}
