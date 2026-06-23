package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultAgentOpts_ContextLimit pins the TUI first-run/setup default token
// (context) upper limit, so newly generated init.json manifests use the
// intended budget. This is the canonical default; the firstrun wizard's
// zero/invalid-input fallbacks mirror it.
func TestDefaultAgentOpts_ContextLimit(t *testing.T) {
	if got := DefaultAgentOpts().ContextLimit; got != 250000 {
		t.Errorf("DefaultAgentOpts().ContextLimit = %d, want 250000", got)
	}
}

// TestGenerateInitJSON_WritesContextLimit verifies the default flows into the
// generated manifest as 250000, and — crucially — that an explicit user
// override is preserved verbatim rather than being clamped back to the default.
func TestGenerateInitJSON_WritesContextLimit(t *testing.T) {
	withTempPresets(t, func() {
		tmpDir := t.TempDir()
		lingtaiDir := filepath.Join(tmpDir, ".lingtai")
		os.MkdirAll(lingtaiDir, 0o755)
		globalDir := filepath.Join(tmpDir, ".lingtai-global")
		Bootstrap(globalDir)

		readManifest := func(agent string) map[string]interface{} {
			t.Helper()
			initPath := filepath.Join(lingtaiDir, agent, "init.json")
			data, err := os.ReadFile(initPath)
			if err != nil {
				t.Fatalf("read init.json: %v", err)
			}
			var initJSON map[string]interface{}
			if err := json.Unmarshal(data, &initJSON); err != nil {
				t.Fatalf("parse init.json: %v", err)
			}
			m, ok := initJSON["manifest"].(map[string]interface{})
			if !ok {
				t.Fatal("manifest not a map")
			}
			return m
		}

		// Default flows through to the manifest as 250000.
		if err := GenerateInitJSONWithOpts(DefaultPreset(), "alice", "alice", lingtaiDir, globalDir, DefaultAgentOpts()); err != nil {
			t.Fatalf("GenerateInitJSONWithOpts: %v", err)
		}
		// JSON numbers decode as float64.
		if v, ok := readManifest("alice")["context_limit"].(float64); !ok || int(v) != 250000 {
			t.Errorf("alice context_limit = %v (ok=%v), want 250000", readManifest("alice")["context_limit"], ok)
		}

		// Explicit override is preserved, not reset to the default.
		opts := DefaultAgentOpts()
		opts.ContextLimit = 1000000
		if err := GenerateInitJSONWithOpts(DefaultPreset(), "bob", "bob", lingtaiDir, globalDir, opts); err != nil {
			t.Fatalf("GenerateInitJSONWithOpts: %v", err)
		}
		if v, ok := readManifest("bob")["context_limit"].(float64); !ok || int(v) != 1000000 {
			t.Errorf("bob context_limit = %v (ok=%v), want preserved 1000000", readManifest("bob")["context_limit"], ok)
		}
	})
}
