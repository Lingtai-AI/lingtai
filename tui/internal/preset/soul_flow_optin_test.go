package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestGenerateInitJSON_SoulFlowDisabled_OmitsSoulAndEnvKey(t *testing.T) {
	// Default disabled agent: no soul block, no LINGTAI_SOUL_FLOW_ENABLED,
	// and (since nothing needed changing) no spurious empty .env.
	opts := DefaultAgentOpts()
	manifest, globalDir := genInit(t, opts)

	if _, ok := manifest["soul"]; ok {
		t.Errorf("default disabled agent should omit manifest.soul, got %v", manifest["soul"])
	}
	if config.SoulFlowEnabled(globalDir) {
		t.Error("default disabled agent enabled soul flow in .env")
	}
	if _, err := os.Stat(config.EnvFilePath(globalDir)); err == nil {
		t.Error("default disabled agent created a .env with nothing to write")
	}
}

func TestGenerateInitJSON_SoulFlowEnabled_WritesEnvAndDefaultCadence(t *testing.T) {
	// Enabling via opts must set LINGTAI_SOUL_FLOW_ENABLED=1 in .env. The
	// cadence in the manifest is the caller's responsibility (the wizard's
	// resolveSoulDelay); here we simulate that by passing an explicit delay.
	delay := DefaultSoulFlowCadence
	opts := DefaultAgentOpts()
	opts.SoulFlowEnabled = true
	opts.SoulDelay = &delay

	manifest, globalDir := genInit(t, opts)

	if !config.SoulFlowEnabled(globalDir) {
		t.Fatal("enabled agent did not write LINGTAI_SOUL_FLOW_ENABLED=1")
	}
	soul, ok := manifest["soul"].(map[string]interface{})
	if !ok {
		t.Fatalf("enabled agent missing manifest.soul, got %v", manifest["soul"])
	}
	got, _ := soul["delay"].(float64)
	if got != DefaultSoulFlowCadence {
		t.Errorf("manifest.soul.delay = %v, want %v", soul["delay"], DefaultSoulFlowCadence)
	}
}

func TestGenerateInitJSON_SoulFlowOptIn_PreservesExistingEnvKeys(t *testing.T) {
	// Enabling soul flow must not clobber an API key already in .env.
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(config.EnvFilePath(globalDir),
		[]byte("MINIMAX_API_KEY=secret\n"), 0o600); err != nil {
		t.Fatalf("seed .env: %v", err)
	}
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	opts := DefaultAgentOpts()
	opts.SoulFlowEnabled = true
	if err := GenerateInitJSONWithOpts(minimaxPreset(), "alice", "alice", lingtaiDir, globalDir, opts); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}

	env, err := os.ReadFile(config.EnvFilePath(globalDir))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if !config.SoulFlowEnabled(globalDir) {
		t.Error("opt-in not written")
	}
	if got := string(env); got != "MINIMAX_API_KEY=secret\nLINGTAI_SOUL_FLOW_ENABLED=1\n" {
		t.Errorf(".env = %q, want API key preserved + opt-in appended", got)
	}
}

func TestGenerateInitJSON_SoulFlowDisable_RemovesExistingEnvKey(t *testing.T) {
	// Re-running with the toggle OFF must remove a previously-set opt-in.
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(config.EnvFilePath(globalDir),
		[]byte("MINIMAX_API_KEY=secret\nLINGTAI_SOUL_FLOW_ENABLED=1\n"), 0o600); err != nil {
		t.Fatalf("seed .env: %v", err)
	}
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	opts := DefaultAgentOpts() // SoulFlowEnabled=false
	if err := GenerateInitJSONWithOpts(minimaxPreset(), "alice", "alice", lingtaiDir, globalDir, opts); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts: %v", err)
	}

	if config.SoulFlowEnabled(globalDir) {
		t.Error("disabling did not remove the opt-in")
	}
	env, _ := os.ReadFile(config.EnvFilePath(globalDir))
	if got := string(env); got != "MINIMAX_API_KEY=secret\n" {
		t.Errorf(".env = %q, want only the preserved API key", got)
	}
}
