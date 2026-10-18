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
			"llm": map[string]interface{}{"provider": "openai", "model": "test-model"},
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

// Explicit output drops legacy tools instead of canonicalizing them into a preset.
func TestSaveCanonicalizesLegacyCapabilities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Save(legacyShellPreset("legacy-save")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(SavedDir(), "legacy-save.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := DecodeJSONUseNumber(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc) != 3 || doc["llm"] == nil || doc["manifest"] != nil {
		t.Fatalf("noncanonical preset output: %s", data)
	}
}

// Conflicting legacy tool declarations are irrelevant to the LLM-only view;
// the standalone CanonicalizeCapabilities conflict oracle above remains intact.
func TestListFailsClosedOnConflictingCapabilities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(SavedDir(), "conflict.json")
	if err := os.MkdirAll(SavedDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"name":"conflict","description":{"summary":"conflict"},"manifest":{"llm":{"provider":"openai","model":"test"},"capabilities":{"bash":{"yolo":true},"shell":{"yolo":false}}}}`)
	if err := os.WriteFile(path, input, 0o644); err != nil {
		t.Fatal(err)
	}
	listed, err := List()
	if err != nil || len(listed) != 1 || len(listed[0].Manifest) != 1 {
		t.Fatalf("legacy LLM read: %#v err=%v", listed, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(input) {
		t.Fatal("ordinary list changed legacy bytes")
	}
}

func TestGenerateInitJSONWritesCanonicalShellCapability(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	if err := fixtureGenerateInitJSONWithOpts(t, legacyShellPreset("legacy-init"), "alice", "alice", lingtaiDir, filepath.Join(tmp, "global"), DefaultAgentOpts()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(lingtaiDir, "alice", "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Fresh init tools come from the kernel, not the selected legacy preset.
	if strings.Contains(string(data), `"source": "user"`) {
		t.Fatalf("preset tools leaked into fresh init: %s", data)
	}
}

func TestLoadCanonicalizesLegacyCapabilities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(SavedDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(SavedDir(), "legacy-load.json")
	input := []byte(`{"name":"legacy-load","description":{"summary":"legacy"},"manifest":{"llm":{"provider":"openai","model":"test"},"capabilities":{"bash":{"metadata":{"source":"user"}}}}}`)
	if err := os.WriteFile(path, input, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load("legacy-load")
	if err != nil || len(loaded.Manifest) != 1 || loaded.Manifest["llm"] == nil {
		t.Fatalf("legacy load exposed tools: %#v err=%v", loaded, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(input) {
		t.Fatal("ordinary load changed legacy bytes")
	}
}

func TestLoadSaveAndGeneratePreserveLargeLegacyCapabilityNumber(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(SavedDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(SavedDir(), "legacy-number.json")
	input := []byte(`{"name":"legacy-number","description":{"summary":"large number"},"manifest":{"llm":{"provider":"openai","model":"test","route_id":9007199254740993},"capabilities":{"bash":{"tenant_id":9007199254740993}}}}`)
	if err := os.WriteFile(path, input, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load("legacy-number")
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Manifest["llm"].(map[string]interface{})["route_id"]; got != json.Number("9007199254740993") {
		t.Fatalf("loaded route_id lost precision: %v", got)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(input) {
		t.Fatal("ordinary load changed legacy number bytes")
	}
	if err := Save(loaded); err != nil {
		t.Fatal(err)
	}
	savedData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Preset
	if err := DecodeJSONUseNumber(savedData, &saved); err != nil {
		t.Fatal(err)
	}
	if got := saved.Manifest["llm"].(map[string]interface{})["route_id"]; got != json.Number("9007199254740993") {
		t.Fatalf("saved route_id lost precision: %v", got)
	}
	if strings.Contains(string(savedData), "tenant_id") || strings.Contains(string(savedData), "capabilities") {
		t.Fatalf("save re-exported legacy tools: %s", savedData)
	}
	projectDir := t.TempDir()
	if err := fixtureGenerateInitJSONWithOpts(t, loaded, "alice", "alice", filepath.Join(projectDir, ".lingtai"), filepath.Join(projectDir, "global"), DefaultAgentOpts()); err != nil {
		t.Fatal(err)
	}
	initData, err := os.ReadFile(filepath.Join(projectDir, ".lingtai", "alice", "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(initData), "tenant_id") {
		t.Fatalf("preset tool kwargs leaked into init: %s", initData)
	}
}
