package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
)

// genInit runs GenerateInitJSONWithOpts against fresh temp dirs and returns
// the parsed manifest plus the globalDir (for .env assertions).
func genInit(t *testing.T, opts AgentOpts) (manifest map[string]interface{}, globalDir string) {
	t.Helper()
	tmp := t.TempDir()
	globalDir = filepath.Join(tmp, "global")
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := GenerateInitJSONWithOpts(minimaxPreset(), "alice", "alice", lingtaiDir, globalDir, opts); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(lingtaiDir, "alice", "init.json"))
	if err != nil {
		t.Fatalf("read init.json: %v", err)
	}
	var init map[string]interface{}
	if err := json.Unmarshal(data, &init); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}
	manifest, _ = init["manifest"].(map[string]interface{})
	if manifest == nil {
		t.Fatalf("manifest missing in init.json")
	}
	return manifest, globalDir
}

// TestGenerateInitJSON_NeverEmitsSoulConfig pins the retirement of the Soul
// subsystem: a freshly generated agent carries no manifest.soul block, no
// soul_file, and generation never materializes or touches the global .env.
func TestGenerateInitJSON_NeverEmitsSoulConfig(t *testing.T) {
	manifest, globalDir := genInit(t, DefaultAgentOpts())
	if v, ok := manifest["soul"]; ok {
		t.Errorf("generated manifest carries a soul block: %v", v)
	}
	if v, ok := manifest["soul_delay"]; ok {
		t.Errorf("generated manifest carries soul_delay: %v", v)
	}
	if _, err := os.Stat(config.EnvFilePath(globalDir)); !os.IsNotExist(err) {
		t.Errorf("agent generation materialized .env (stat err=%v); want untouched", err)
	}
}

// TestGenerateInitJSON_PreservesRetiredEnvLinesByteForByte proves a user's
// existing .env — including a LINGTAI_SOUL_FLOW_ENABLED line left by the
// retired opt-in — is neither rewritten nor pruned by agent generation.
func TestGenerateInitJSON_PreservesRetiredEnvLinesByteForByte(t *testing.T) {
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir global: %v", err)
	}
	seed := "# keep me\nMINIMAX_API_KEY=secret\nLINGTAI_SOUL_FLOW_ENABLED=1\n"
	if err := os.WriteFile(config.EnvFilePath(globalDir), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed .env: %v", err)
	}
	if err := GenerateInitJSONWithOpts(minimaxPreset(), "alice", "alice", lingtaiDir, globalDir, DefaultAgentOpts()); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}
	got, err := os.ReadFile(config.EnvFilePath(globalDir))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if string(got) != seed {
		t.Errorf(".env changed by agent generation:\n got: %q\nwant: %q", got, seed)
	}
}

// TestGenerateInitJSON_OldAgentKeepsLegacySoulFieldsInert proves that
// re-running the init/setup generation path on an agent created before the
// Soul retirement carries its existing nested manifest.soul object and its
// retired top-level soul_file value through unchanged — neither rewritten,
// extended, nor dropped — while still emitting no new Soul field.
func TestGenerateInitJSON_OldAgentKeepsLegacySoulFieldsInert(t *testing.T) {
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacySoul := map[string]interface{}{"delay": float64(7200), "voice": "inner"}
	const legacySoulFile = "/old/global/soul/en/soul-flow.md"
	old := map[string]interface{}{
		"manifest": map[string]interface{}{
			"agent_name": "alice",
			"language":   "en",
			"soul":       legacySoul,
		},
		"soul_file":     legacySoulFile,
		"covenant_file": "/old/global/covenant/en/covenant.md",
	}
	oldData, err := json.MarshalIndent(old, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy init.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), oldData, 0o644); err != nil {
		t.Fatalf("seed legacy init.json: %v", err)
	}

	if err := GenerateInitJSONWithOpts(minimaxPreset(), "alice", "alice", lingtaiDir, globalDir, DefaultAgentOpts()); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(agentDir, "init.json"))
	if err != nil {
		t.Fatalf("read init.json: %v", err)
	}
	var init map[string]interface{}
	if err := json.Unmarshal(data, &init); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}
	manifest, _ := init["manifest"].(map[string]interface{})
	if manifest == nil {
		t.Fatalf("manifest missing in init.json")
	}
	if got := manifest["soul"]; !reflect.DeepEqual(got, legacySoul) {
		t.Errorf("legacy manifest.soul changed: got %v, want %v", got, legacySoul)
	}
	if got := init["soul_file"]; got != legacySoulFile {
		t.Errorf("legacy soul_file changed: got %v, want %q", got, legacySoulFile)
	}
	if v, ok := manifest["soul_delay"]; ok {
		t.Errorf("regeneration emitted a new soul_delay field: %v", v)
	}
	if _, err := os.Stat(config.EnvFilePath(globalDir)); !os.IsNotExist(err) {
		t.Errorf("regeneration materialized .env (stat err=%v); want untouched", err)
	}
}

// TestBootstrap_DoesNotPopulateSoulFragments proves the retired soul prompt
// fragment is no longer extracted under ~/.lingtai-tui/, while the still
// shipped fragments are.
func TestBootstrap_DoesNotPopulateSoulFragments(t *testing.T) {
	withTempPresets(t, func() {
		globalDir := filepath.Join(t.TempDir(), "global")
		if err := Bootstrap(globalDir); err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		if _, err := os.Stat(filepath.Join(globalDir, "soul")); !os.IsNotExist(err) {
			t.Errorf("Bootstrap populated retired soul/ fragments (stat err=%v)", err)
		}
		for _, lang := range []string{"en", "zh", "wen"} {
			if _, err := os.Stat(CovenantPath(globalDir, lang)); err != nil {
				t.Errorf("covenant for %s missing after Bootstrap: %v", lang, err)
			}
		}
	})
}
