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

// The public legacy option remains accepted/projected, but System policy is
// not serialized into new init. Existing explicit System owner bytes survive.
func TestGenerateInitJSON_WritesMaxAedAttempts(t *testing.T) {
	withTempPresets(t, func() {
		root, global := t.TempDir(), t.TempDir()
		lingtaiDir := filepath.Join(root, ".lingtai")
		for _, tc := range []struct {
			name      string
			input     int
			projected int
		}{
			{"explicit", 7, 7},
			{"zero", 0, 5},
			{"above-max", 5000, 100},
		} {
			t.Run(tc.name, func(t *testing.T) {
				opts := DefaultAgentOpts()
				opts.MaxAedAttempts = tc.input
				dir := filepath.Join(lingtaiDir, tc.name)
				if err := os.MkdirAll(filepath.Join(dir, "settings"), 0755); err != nil {
					t.Fatal(err)
				}
				owner := filepath.Join(dir, "settings", "system.json")
				// Deliberately differs from wizard value: generation must not reset owner.
				seed := []byte(`{"schema_version":2,"max_aed_attempts":23}`)
				if err := os.WriteFile(owner, seed, 0600); err != nil {
					t.Fatal(err)
				}
				request, err := ProjectConfigurationRequest(codexPreset(), tc.name, tc.name, lingtaiDir, global, opts)
				if err != nil || request.Options.MaxAEDAttempts != tc.projected {
					t.Fatalf("legacy option projection: %+v %v", request.Options, err)
				}
				if err := fixtureGenerateInitJSONWithOpts(t, codexPreset(), tc.name, tc.name, lingtaiDir, global, opts); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(dir, "init.json"))
				if err != nil {
					t.Fatal(err)
				}
				var document map[string]interface{}
				if json.Unmarshal(data, &document) != nil || document["llm"] == nil || document["manifest"] != nil {
					t.Fatal("wrong fresh owners")
				}
				for _, key := range []string{"max_aed_attempts", "max_rpm"} {
					if _, found := document[key]; found {
						t.Fatalf("runtime knob %s exported into init", key)
					}
				}
				got, err := os.ReadFile(owner)
				if err != nil || string(got) != string(seed) {
					t.Fatal("generation rewrote explicit System owner policy")
				}
			})
		}
	})
}
