package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultAgentOpts_MaxAedAttempts pins the TUI first-run/setup default,
// so generated init.json manifests use the intended AED retry count.
func TestDefaultAgentOpts_MaxAedAttempts(t *testing.T) {
	if got := DefaultAgentOpts().MaxAedAttempts; got != DefaultMaxAedAttempts {
		t.Errorf("DefaultAgentOpts().MaxAedAttempts = %d, want %d", got, DefaultMaxAedAttempts)
	}
	if DefaultMaxAedAttempts != 5 {
		t.Errorf("DefaultMaxAedAttempts = %d, want 5", DefaultMaxAedAttempts)
	}
}

func TestClampAedAttempts(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"zero falls back to default", 0, DefaultMaxAedAttempts},
		{"negative falls back to default", -5, DefaultMaxAedAttempts},
		{"min boundary kept", 1, 1},
		{"typical kept", 25, 25},
		{"max boundary kept", 100, 100},
		{"above max clamped down", 101, MaxMaxAedAttempts},
		{"way above max clamped down", 99999, MaxMaxAedAttempts},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClampAedAttempts(c.in); got != c.want {
				t.Errorf("ClampAedAttempts(%d) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// TestGenerateInitJSON_WritesMaxAedAttempts verifies the manifest carries the
// chosen value, that it round-trips through JSON, and that a zero-value opt is
// normalized to the default rather than written as 0.
func TestGenerateInitJSON_WritesMaxAedAttempts(t *testing.T) {
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

		// Explicit value round-trips.
		opts := DefaultAgentOpts()
		opts.MaxAedAttempts = 7
		if err := GenerateInitJSONWithOpts(DefaultPreset(), "alice", "alice", lingtaiDir, globalDir, opts); err != nil {
			t.Fatalf("GenerateInitJSONWithOpts: %v", err)
		}
		// JSON numbers decode as float64.
		if v, ok := readManifest("alice")["max_aed_attempts"].(float64); !ok || int(v) != 7 {
			t.Errorf("alice max_aed_attempts = %v (ok=%v), want 7", readManifest("alice")["max_aed_attempts"], ok)
		}

		// Zero-value opt normalizes to the default (never written as 0).
		zeroOpts := DefaultAgentOpts()
		zeroOpts.MaxAedAttempts = 0
		if err := GenerateInitJSONWithOpts(DefaultPreset(), "bob", "bob", lingtaiDir, globalDir, zeroOpts); err != nil {
			t.Fatalf("GenerateInitJSONWithOpts: %v", err)
		}
		if v, ok := readManifest("bob")["max_aed_attempts"].(float64); !ok || int(v) != DefaultMaxAedAttempts {
			t.Errorf("bob max_aed_attempts = %v (ok=%v), want %d", readManifest("bob")["max_aed_attempts"], ok, DefaultMaxAedAttempts)
		}

		// Out-of-range opt is clamped to the ceiling.
		bigOpts := DefaultAgentOpts()
		bigOpts.MaxAedAttempts = 5000
		if err := GenerateInitJSONWithOpts(DefaultPreset(), "carol", "carol", lingtaiDir, globalDir, bigOpts); err != nil {
			t.Fatalf("GenerateInitJSONWithOpts: %v", err)
		}
		if v, ok := readManifest("carol")["max_aed_attempts"].(float64); !ok || int(v) != MaxMaxAedAttempts {
			t.Errorf("carol max_aed_attempts = %v (ok=%v), want %d", readManifest("carol")["max_aed_attempts"], ok, MaxMaxAedAttempts)
		}
	})
}
