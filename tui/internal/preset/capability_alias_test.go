package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func legacyShellPreset(name string) Preset {
	return Preset{
		Name:        name,
		Description: PresetDescription{Summary: "legacy shell capability"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{"provider": "custom", "model": "test-model"},
			"capabilities": map[string]interface{}{
				"bash": map[string]interface{}{
					"yolo":     true,
					"paths":    []interface{}{"./scripts"},
					"metadata": map[string]interface{}{"source": "user"},
				},
			},
		},
	}
}

func TestCanonicalizeCapabilitiesPreservesLegacyConfiguration(t *testing.T) {
	cfg := map[string]interface{}{
		"yolo":     true,
		"paths":    []interface{}{"./scripts", "~/bin"},
		"metadata": map[string]interface{}{"source": "user"},
	}
	caps := map[string]interface{}{"bash": cfg}

	changed, err := CanonicalizeCapabilities(caps)
	if err != nil {
		t.Fatalf("CanonicalizeCapabilities: %v", err)
	}
	if !changed {
		t.Fatal("legacy capability was not canonicalized")
	}
	if _, ok := caps["bash"]; ok {
		t.Fatalf("legacy bash key survived: %#v", caps)
	}
	if got := caps["shell"]; !reflect.DeepEqual(got, cfg) {
		t.Fatalf("shell config = %#v, want %#v", got, cfg)
	}
}

func TestCanonicalizeCapabilitiesIdenticalKeysKeepCanonicalValue(t *testing.T) {
	legacy := map[string]interface{}{"yolo": true}
	canonical := map[string]interface{}{"yolo": true}
	caps := map[string]interface{}{"bash": legacy, "shell": canonical}

	changed, err := CanonicalizeCapabilities(caps)
	if err != nil {
		t.Fatalf("CanonicalizeCapabilities: %v", err)
	}
	if !changed || len(caps) != 1 {
		t.Fatalf("canonicalization changed=%v caps=%#v, want only shell", changed, caps)
	}
	if got := caps["shell"]; !reflect.DeepEqual(got, canonical) {
		t.Fatalf("shell config = %#v, want canonical value %#v", got, canonical)
	}
}

func TestCanonicalizeCapabilitiesConflictFailsClosed(t *testing.T) {
	legacy := map[string]interface{}{"yolo": true}
	canonical := map[string]interface{}{"yolo": false}
	caps := map[string]interface{}{"bash": legacy, "shell": canonical}
	before := map[string]interface{}{"bash": legacy, "shell": canonical}

	if _, err := CanonicalizeCapabilities(caps); err == nil {
		t.Fatal("conflicting bash and shell configurations returned nil error")
	} else if !strings.Contains(err.Error(), "bash") || !strings.Contains(err.Error(), "shell") {
		t.Fatalf("conflict error = %q, want both capability names", err)
	}
	if !reflect.DeepEqual(caps, before) {
		t.Fatalf("conflict mutated capabilities: got %#v, want %#v", caps, before)
	}
}

func TestSaveCanonicalizesLegacyCapabilities(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := Save(legacyShellPreset("legacy-save")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".lingtai-tui", "presets", "saved", "legacy-save.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved Preset
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	caps := saved.Manifest["capabilities"].(map[string]interface{})
	if _, ok := caps["bash"]; ok {
		t.Fatalf("saved preset still contains legacy bash key: %s", data)
	}
	if got := caps["shell"].(map[string]interface{})["yolo"]; got != true {
		t.Fatalf("saved shell config lost yolo value: %#v", caps["shell"])
	}
}

func TestListFailsClosedOnConflictingCapabilities(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".lingtai-tui", "presets", "saved", "conflict.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"name":"conflict","description":{"summary":"conflict"},"manifest":{"llm":{"provider":"custom","model":"test"},"capabilities":{"bash":{"yolo":true},"shell":{"yolo":false}}}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := List(); err == nil || !strings.Contains(err.Error(), "bash") || !strings.Contains(err.Error(), "shell") {
		t.Fatalf("List conflict error = %v, want explicit bash/shell error", err)
	}
}

func TestGenerateInitJSONWritesCanonicalShellCapability(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	globalDir := filepath.Join(tmp, "global")
	if err := GenerateInitJSONWithOpts(legacyShellPreset("legacy-init"), "alice", "alice", lingtaiDir, globalDir, DefaultAgentOpts()); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(lingtaiDir, "alice", "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	caps := doc["manifest"].(map[string]interface{})["capabilities"].(map[string]interface{})
	if _, ok := caps["bash"]; ok {
		t.Fatalf("generated init.json still contains legacy bash key: %s", data)
	}
	if got := caps["shell"].(map[string]interface{})["metadata"].(map[string]interface{})["source"]; got != "user" {
		t.Fatalf("generated init.json lost shell configuration: %#v", caps["shell"])
	}
}

func TestLoadCanonicalizesLegacyCapabilities(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".lingtai-tui", "presets", "saved", "legacy-load.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(legacyShellPreset("legacy-load"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load("legacy-load")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	caps := loaded.Manifest["capabilities"].(map[string]interface{})
	if _, ok := caps["bash"]; ok {
		t.Fatalf("loaded preset still contains legacy bash key: %#v", caps)
	}
	if got := caps["shell"].(map[string]interface{})["metadata"].(map[string]interface{})["source"]; got != "user" {
		t.Fatalf("loaded shell config lost metadata: %#v", caps["shell"])
	}
}

func TestLoadSaveAndGeneratePreserveLargeLegacyCapabilityNumber(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".lingtai-tui", "presets", "saved", "legacy-number.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"name":"legacy-number","description":{"summary":"large number"},"manifest":{"llm":{"provider":"custom","model":"test"},"capabilities":{"bash":{"tenant_id":9007199254740993}}}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load("legacy-number")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	caps := loaded.Manifest["capabilities"].(map[string]interface{})
	shell := caps["shell"].(map[string]interface{})
	if got := shell["tenant_id"].(json.Number); got != json.Number("9007199254740993") {
		t.Fatalf("loaded tenant_id = %q, want exact token 9007199254740993", got)
	}

	if err := Save(loaded); err != nil {
		t.Fatalf("Save: %v", err)
	}
	savedData, err := os.ReadFile(filepath.Join(home, ".lingtai-tui", "presets", "saved", "legacy-number.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved Preset
	if err := DecodeJSONUseNumber(savedData, &saved); err != nil {
		t.Fatalf("decode saved preset: %v", err)
	}
	savedShell := saved.Manifest["capabilities"].(map[string]interface{})["shell"].(map[string]interface{})
	if got := savedShell["tenant_id"].(json.Number); got != json.Number("9007199254740993") {
		t.Fatalf("saved tenant_id = %q, want exact token 9007199254740993", got)
	}

	projectDir := t.TempDir()
	if err := GenerateInitJSONWithOpts(loaded, "alice", "alice", filepath.Join(projectDir, ".lingtai"), filepath.Join(projectDir, "global"), DefaultAgentOpts()); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}
	initData, err := os.ReadFile(filepath.Join(projectDir, ".lingtai", "alice", "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	var initDoc map[string]interface{}
	if err := DecodeJSONUseNumber(initData, &initDoc); err != nil {
		t.Fatalf("decode generated init.json: %v", err)
	}
	initShell := initDoc["manifest"].(map[string]interface{})["capabilities"].(map[string]interface{})["shell"].(map[string]interface{})
	if got := initShell["tenant_id"].(json.Number); got != json.Number("9007199254740993") {
		t.Fatalf("generated tenant_id = %q, want exact token 9007199254740993", got)
	}
}
