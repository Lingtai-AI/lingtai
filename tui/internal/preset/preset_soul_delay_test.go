package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestGenerateInitJSON_SoulDelayInheritsKernelDefault verifies the TUI does not
// encode a second soul-delay default into generated init.json manifests. When
// the user leaves the field unset, the runtime kernel's AgentConfig default is
// allowed to apply; explicit user overrides are still preserved.
func TestGenerateInitJSON_SoulDelayInheritsKernelDefault(t *testing.T) {
	withTempPresets(t, func() {
		tmpDir := t.TempDir()
		lingtaiDir := filepath.Join(tmpDir, ".lingtai")
		if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
			t.Fatalf("mkdir lingtai dir: %v", err)
		}
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
			manifest, ok := initJSON["manifest"].(map[string]interface{})
			if !ok {
				t.Fatal("manifest not a map")
			}
			return manifest
		}

		if err := GenerateInitJSONWithOpts(DefaultPreset(), "alice", "alice", lingtaiDir, globalDir, DefaultAgentOpts()); err != nil {
			t.Fatalf("GenerateInitJSONWithOpts default: %v", err)
		}
		if _, ok := readManifest("alice")["soul"]; ok {
			t.Fatal("default manifest unexpectedly encoded soul.delay; want kernel default inheritance")
		}

		explicitDelay := 123.0
		opts := DefaultAgentOpts()
		opts.SoulDelay = &explicitDelay
		if err := GenerateInitJSONWithOpts(DefaultPreset(), "bob", "bob", lingtaiDir, globalDir, opts); err != nil {
			t.Fatalf("GenerateInitJSONWithOpts explicit: %v", err)
		}
		soul, ok := readManifest("bob")["soul"].(map[string]interface{})
		if !ok {
			t.Fatal("explicit manifest missing soul map")
		}
		if got, ok := soul["delay"].(float64); !ok || got != explicitDelay {
			t.Fatalf("explicit soul.delay = %v (ok=%v), want %v", soul["delay"], ok, explicitDelay)
		}
	})
}
