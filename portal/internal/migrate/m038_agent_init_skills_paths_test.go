package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeAgentInit(t *testing.T, lingtaiDir, agent, content string) string {
	t.Helper()
	agentDir := filepath.Join(lingtaiDir, agent)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agentDir, "init.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readAgentSkills(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("patched init.json is not valid json: %v\n%s", err, data)
	}
	manifest, ok := doc["manifest"].(map[string]interface{})
	if !ok {
		t.Fatalf("manifest missing or not a map: %s", data)
	}
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("capabilities missing or not a map: %s", data)
	}
	skills, ok := caps["skills"].(map[string]interface{})
	if !ok {
		t.Fatalf("skills missing or not a map: %s", data)
	}
	return skills
}

// TestRunFrom37PatchesAgentInitAndAdvancesTo38 pins the portal-opened-first
// scenario: a project the TUI last touched at version 37 must get the agent
// init.json skills.paths repair from the portal too, not just a version
// stamp. A no-op stub here would mark the project as migrated and the TUI's
// m038 would never run.
func TestRunFrom37PatchesAgentInitAndAdvancesTo38(t *testing.T) {
	lingtaiDir := filepath.Join(t.TempDir(), ".lingtai")
	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMeta(t, lingtaiDir, 37)

	// skills entry present but paths lost
	orch := writeAgentInit(t, lingtaiDir, "orch",
		`{"manifest":{"capabilities":{"skills":{"library_limit":42},"web_search":{"provider":"zhipu"}}}}`)
	// whole capabilities map lost
	scout := writeAgentInit(t, lingtaiDir, "scout", `{"manifest":{"llm":{"provider":"custom"}}}`)

	if err := Run(lingtaiDir); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	if meta := readMeta(t, lingtaiDir); meta.Version != 38 {
		t.Fatalf("expected meta version 38, got %d", meta.Version)
	}

	orchSkills := readAgentSkills(t, orch)
	if !reflect.DeepEqual(orchSkills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("orch skills.paths = %#v, want %#v", orchSkills["paths"], defaultPresetSkillsPaths)
	}
	if got := orchSkills["library_limit"]; got != float64(42) {
		t.Fatalf("orch existing skills config overwritten: %#v", orchSkills)
	}

	scoutSkills := readAgentSkills(t, scout)
	if !reflect.DeepEqual(scoutSkills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("scout skills.paths = %#v, want %#v", scoutSkills["paths"], defaultPresetSkillsPaths)
	}

	// sibling capability entries survive
	data, _ := os.ReadFile(orch)
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	caps := doc["manifest"].(map[string]interface{})["capabilities"].(map[string]interface{})
	ws, ok := caps["web_search"].(map[string]interface{})
	if !ok || ws["provider"] != "zhipu" {
		t.Fatalf("web_search entry damaged: %#v", caps["web_search"])
	}
}

func TestMigrateAgentInitSkillsPathsPreservesCustomPaths(t *testing.T) {
	lingtaiDir := t.TempDir()
	content := `{"manifest":{"capabilities":{"skills":{"paths":["./custom-skills"]}}}}`
	path := writeAgentInit(t, lingtaiDir, "orch", content)

	if err := migrateAgentInitSkillsPaths(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	// File must be byte-identical: nothing to patch means no rewrite.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("init.json with custom paths was rewritten: %s", data)
	}
}

func TestMigrateAgentInitSkillsPathsSkipsMalformedAndNonMapCases(t *testing.T) {
	lingtaiDir := t.TempDir()
	cases := map[string]string{
		"broken":         `{"manifest":`,
		"weird-caps":     `{"manifest":{"capabilities":["skills"]}}`,
		"weird-manifest": `{"manifest":"nope"}`,
	}
	paths := map[string]string{}
	for agent, content := range cases {
		paths[agent] = writeAgentInit(t, lingtaiDir, agent, content)
	}
	// a valid sibling proves the migration continues past the broken ones
	good := writeAgentInit(t, lingtaiDir, "zz-good", `{"manifest":{"capabilities":{}}}`)

	if err := migrateAgentInitSkillsPaths(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	for agent, content := range cases {
		data, err := os.ReadFile(paths[agent])
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != content {
			t.Fatalf("%s init.json should be unchanged, got: %s", agent, data)
		}
	}

	skills := readAgentSkills(t, good)
	if !reflect.DeepEqual(skills["paths"], defaultPresetSkillsPaths) {
		t.Fatalf("valid sibling not patched: %#v", skills["paths"])
	}
}
