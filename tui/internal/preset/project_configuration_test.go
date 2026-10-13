package preset

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/process"
)

// Fixture prerequisites for the real configure consumer. These helpers only
// persist input documents and select the parent's cold-installed interpreter;
// they do not reconstruct reconciliation or manufacture a success receipt.
func fixtureProjectPython(t *testing.T) string {
	t.Helper()
	python := os.Getenv("LINGTAI_PROJECT_CONFIGURATION_TEST_PYTHON")
	if python == "" || !filepath.IsAbs(python) {
		t.Fatal("set LINGTAI_PROJECT_CONFIGURATION_TEST_PYTHON to the parent-verified installed interpreter; no global runtime fallback")
	}
	if _, err := os.Stat(python); err != nil {
		t.Fatalf("installed fixture interpreter: %v", err)
	}
	return python
}

func fixturePresetDocument(t *testing.T, p Preset) {
	t.Helper()
	if isSyntheticPreset(p) {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	fixturePresetReference(t, filepath.Join(home, RefFor(p)[2:]), p)
}

func fixturePresetReference(t *testing.T, ref string, p Preset) {
	t.Helper()
	path := ref
	if len(ref) > 2 && ref[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		path = filepath.Join(home, ref[2:])
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	// Raw fixture serialization deliberately leaves legacy aliases/exact numbers
	// for the actual kernel reader to handle, not the Go Save normalizer.
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func fixtureGenerateInitJSONWithOpts(t *testing.T, p Preset, agentName, dirName, lingtaiDir, globalDir string, opts AgentOpts) error {
	t.Helper()
	python := fixtureProjectPython(t)
	fixturePresetDocument(t, p)
	if err := os.MkdirAll(filepath.Dir(lingtaiDir), 0755); err != nil {
		t.Fatal(err)
	}
	if opts.EnvFile == "" {
		// Supply an external readable owner prerequisite. In particular do NOT
		// materialize globalDir/.env: no-global-write assertions still apply.
		opts.EnvFile = filepath.Join(t.TempDir(), "fixture.env")
		if err := os.WriteFile(opts.EnvFile, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return GenerateInitJSONWithOpts(p, agentName, dirName, lingtaiDir, globalDir, opts, python)
}

func fixtureGenerateInitJSON(t *testing.T, p Preset, agentName, dirName, lingtaiDir, globalDir string) error {
	t.Helper()
	opts := DefaultAgentOpts()
	if language, ok := p.Manifest["language"].(string); ok && language != "" {
		opts.Language = language
	}
	return fixtureGenerateInitJSONWithOpts(t, p, agentName, dirName, lingtaiDir, globalDir, opts)
}

func TestProjectConfigurationPublicProjection(t *testing.T) {
	root, global := t.TempDir(), t.TempDir()
	opts := DefaultAgentOpts()
	opts.VenvPath = filepath.Join(root, "explicit-venv")
	opts.EnvFile = filepath.Join(root, "public.env")
	p := Preset{Name: "keep_current", Source: SourceUnknown, Manifest: map[string]interface{}{"credential": "must-not-cross", "llm": map[string]interface{}{"model": "display-only"}}}
	request, err := ProjectConfigurationRequest(p, "Display", "orch-dir", filepath.Join(root, ".lingtai"), global, opts)
	if err != nil {
		t.Fatal(err)
	}
	if request.ProjectDir != root || request.PresetRef != "" || !request.KeepCurrent || !request.Options.PreserveActive || request.Prompts != nil || request.Options.Pad != nil {
		t.Fatalf("wrong projection %+v", request)
	}
	if request.Options.EnvFile != opts.EnvFile || request.Options.VenvPath != opts.VenvPath {
		t.Fatal("explicit public fallbacks lost")
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]interface{}
	if json.Unmarshal(data, &public) != nil {
		t.Fatal("invalid public projection")
	}
	if _, ok := public["manifest"]; ok {
		t.Fatal("whole preset projected")
	}
	if _, ok := public["credential"]; ok {
		t.Fatal("credential projected")
	}
	if count, err := PropagatePresetPolicy(filepath.Join(root, ".lingtai"), "orch-dir", "", nil, "missing-python"); err != nil || count != 0 {
		t.Fatal("empty allowed must not invoke kernel")
	}
}

// Shared byte-identical kernel/Go matrix exercises the actual installed CLI,
// not a Go reconstruction of Core's reconciliation algorithm. Parent supplies
// this TEST-ONLY selector explicitly after cold-package proof; no production
// setting or install/retry mechanism is introduced.
func TestProjectConfigurationInstalledSharedMatrix(t *testing.T) {
	python := os.Getenv("LINGTAI_PROJECT_CONFIGURATION_TEST_PYTHON")
	if python == "" {
		t.Skip("parent-owned actual installed kernel acceptance requires explicit test interpreter")
	}
	bytes, err := os.ReadFile("testdata/project_configuration.json")
	if err != nil {
		t.Fatal(err)
	}
	var matrix struct {
		ExistingInit      map[string]interface{} `json:"existing_init"`
		ExistingBlueprint map[string]interface{} `json:"existing_blueprint"`
		Cases             []struct {
			ID              string   `json:"id"`
			KeepCurrent     bool     `json:"keep_current"`
			Allowed         []string `json:"allowed"`
			ExpectedActive  string   `json:"expected_active"`
			ExpectedDefault string   `json:"expected_default"`
		} `json:"cases"`
	}
	if DecodeJSONUseNumber(bytes, &matrix) != nil {
		t.Fatal("shared fixture invalid")
	}
	for _, test := range matrix.Cases {
		t.Run(test.ID, func(t *testing.T) {
			root := t.TempDir()
			write := func(path string, value interface{}) {
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			refs := map[string]string{}
			for _, name := range []string{"old.json", "active.json", "selected.json"} {
				refs[name] = filepath.Join(root, name)
				write(refs[name], map[string]interface{}{"description": map[string]string{"summary": "public fixture"}, "manifest": map[string]interface{}{"llm": map[string]string{"provider": "openai", "model": "selected-model"}, "capabilities": map[string]interface{}{"shell": map[string]bool{"yolo": false}}}})
			}
			// Re-decode for each row; unknown numeric fields must remain exact tokens.
			var previous struct {
				ExistingInit      map[string]interface{} `json:"existing_init"`
				ExistingBlueprint map[string]interface{} `json:"existing_blueprint"`
			}
			if DecodeJSONUseNumber(bytes, &previous) != nil {
				t.Fatal("fixture invalid")
			}
			manifest := previous.ExistingInit["manifest"].(map[string]interface{})
			policy := manifest["preset"].(map[string]interface{})
			policy["default"], policy["active"], policy["allowed"] = refs["old.json"], refs["active.json"], []string{refs["old.json"], refs["active.json"]}
			selected := filepath.Join(root, ".lingtai", "orch-dir")
			write(filepath.Join(selected, "init.json"), previous.ExistingInit)
			write(filepath.Join(selected, ".agent.json"), previous.ExistingBlueprint)
			request := process.ProjectConfigureRequest{ProjectDir: root, DirectoryName: "orch-dir", AgentName: "New Display", PresetRef: refs["selected.json"], KeepCurrent: test.KeepCurrent,
				Options: process.ProjectConfigurationOptions{EnvFile: "fallback.env", VenvPath: "fallback/venv", Language: "en", Karma: true, PreserveActive: true, ContextLimit: 500000, MaxRPM: 60, MaxAEDAttempts: 5}}
			for _, name := range test.Allowed {
				request.Options.AllowedPresets = append(request.Options.AllowedPresets, refs[name])
			}
			if test.KeepCurrent {
				request.PresetRef = ""
			}
			result, err := process.ConfigureProject(context.Background(), python, request)
			if err != nil || !result.Source.InitWritten || result.Updated != 1 {
				t.Fatalf("actual receipt %+v err=%v", result, err)
			}
			data, err := os.ReadFile(filepath.Join(selected, "init.json"))
			if err != nil {
				t.Fatal(err)
			}
			var saved map[string]interface{}
			if DecodeJSONUseNumber(data, &saved) != nil {
				t.Fatal("saved invalid")
			}
			savedPolicy := saved["manifest"].(map[string]interface{})["preset"].(map[string]interface{})
			if savedPolicy["default"] != refs[test.ExpectedDefault] || savedPolicy["active"] != refs[test.ExpectedActive] {
				t.Fatalf("owner policy %v", savedPolicy)
			}
			if saved["pad"] != "durable Pad" || saved["env_file"] != "custom.env" || saved["venv_path"] != "custom/venv" {
				t.Fatal("kernel lost existing custom values")
			}
			if saved["custom_root"].(map[string]interface{})["integer"] != json.Number("9007199254740993123456789") {
				t.Fatal("exact metadata rounded")
			}
			data, err = os.ReadFile(filepath.Join(selected, ".agent.json"))
			if err != nil {
				t.Fatal(err)
			}
			var blueprint map[string]interface{}
			if DecodeJSONUseNumber(data, &blueprint) != nil {
				t.Fatal("saved blueprint invalid")
			}
			if blueprint["agent_id"] != "stable-id" || blueprint["molt_count"] != json.Number("9007199254740993123456789") || blueprint["nickname"] != "durable nick" {
				t.Fatal("kernel-owned identity/history lost")
			}
		})
	}
}
