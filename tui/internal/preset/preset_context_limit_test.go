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
	if got := DefaultAgentOpts().ContextLimit; got != 500000 {
		t.Errorf("DefaultAgentOpts().ContextLimit = %d, want 500000", got)
	}
}

// The public legacy option remains accepted/projected, but System policy is
// not serialized into new init. Existing explicit System owner bytes survive.
func TestGenerateInitJSON_WritesContextLimit(t *testing.T) {
	withTempPresets(t, func() {
		root, global := t.TempDir(), t.TempDir()
		lingtaiDir := filepath.Join(root, ".lingtai")
		for _, tc := range []struct {
			name      string
			input     int
			projected int
		}{
			{"default", 500000, 500000},
			{"override", 1000000, 1000000},
		} {
			t.Run(tc.name, func(t *testing.T) {
				opts := DefaultAgentOpts()
				opts.ContextLimit = tc.input
				dir := filepath.Join(lingtaiDir, tc.name)
				if err := os.MkdirAll(filepath.Join(dir, "settings"), 0755); err != nil {
					t.Fatal(err)
				}
				owner := filepath.Join(dir, "settings", "system.json")
				// Deliberately differs from wizard value: generation must not reset owner.
				seed := []byte(`{"schema_version":2,"context_limit":23}`)
				if err := os.WriteFile(owner, seed, 0600); err != nil {
					t.Fatal(err)
				}
				request, err := ProjectConfigurationRequest(codexPreset(), tc.name, tc.name, lingtaiDir, global, opts)
				if err != nil || request.Options.ContextLimit != tc.projected {
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
				for _, key := range []string{"context_limit", "max_rpm"} {
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
