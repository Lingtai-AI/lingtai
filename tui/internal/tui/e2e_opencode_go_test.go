package tui

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// TestE2EOpenCodeGoOptionOnZhipuMinimax drives the real preset editor model
// (same code a user types in) for zhipu and minimax: it loads the builtin
// preset, cycles the base_url field forward until it lands on the OpenCode Go
// option, commits, and verifies the saved manifest carries the OpenCode Go
// endpoint and OPENCODE_GO_API_KEY credential slot. This is the e2e contract
// Jason asked for (2026-08-09 4081): the option must actually produce a
// working preset, not just render.
func TestE2EOpenCodeGoOptionOnZhipuMinimax(t *testing.T) {
	const openCodeGoURL = "https://opencode.ai/zen/go/v1"

	for _, provider := range []string{"zhipu", "minimax"} {
		regions := preset.ProviderRegionURLs[provider]
		// Locate the OpenCode Go row in the table.
		ocgIdx := -1
		for i, r := range regions {
			if r.URL == openCodeGoURL {
				ocgIdx = i
				break
			}
		}
		if ocgIdx < 0 {
			t.Fatalf("[%s] OpenCode Go row missing from ProviderRegionURLs", provider)
		}
		if regions[ocgIdx].Env != "OPENCODE_GO_API_KEY" {
			t.Fatalf("[%s] OpenCode Go row Env = %q, want OPENCODE_GO_API_KEY", provider, regions[ocgIdx].Env)
		}

		m := NewPresetEditorModelWithBuiltinFlag(builtinPresetForEditorTest(t, provider), "en", nil, "", false)
		llm := m.llmMap()

		// Start at the default first region (CN).
		if got, _ := llm["base_url"].(string); got != regions[0].URL {
			t.Fatalf("[%s] initial base_url = %q, want %q", provider, got, regions[0].URL)
		}

		// Put the cursor on the base_url field and cycle forward until we land
		// on the OpenCode Go URL (bounded by table length).
		m.cursor = editorFieldOrderIndex(t, feBaseURL)
		cycles := 0
		for {
			if got, _ := llm["base_url"].(string); got == openCodeGoURL {
				break
			}
			if cycles > len(regions) {
				t.Fatalf("[%s] never landed on OpenCode Go; base_url = %v", provider, llm["base_url"])
			}
			m.cycleFocused(1)
			cycles++
		}

		// Verify the working manifest now points at OpenCode Go with its slot.
		if got, _ := llm["base_url"].(string); got != openCodeGoURL {
			t.Fatalf("[%s] base_url = %q, want %q", provider, got, openCodeGoURL)
		}
		if got, _ := llm["api_key_env"].(string); got != "OPENCODE_GO_API_KEY" {
			t.Fatalf("[%s] api_key_env = %q, want OPENCODE_GO_API_KEY", provider, got)
		}

		// Validate the working manifest (must pass the editor save gate).
		if errs := m.working.Validate(); len(errs) != 0 {
			t.Fatalf("[%s] working preset failed Validate: %v", provider, errs)
		}

		// Commit and inspect the saved manifest JSON.
		committed := clonePresetForEditor(m.working)
		raw, err := json.Marshal(committed.Manifest)
		if err != nil {
			t.Fatalf("[%s] marshal manifest: %v", provider, err)
		}
		var mm map[string]interface{}
		if err := json.Unmarshal(raw, &mm); err != nil {
			t.Fatalf("[%s] unmarshal manifest: %v", provider, err)
		}
		llm2, _ := mm["llm"].(map[string]interface{})
		if got, _ := llm2["base_url"].(string); got != openCodeGoURL {
			t.Fatalf("[%s] committed base_url = %q, want %q", provider, got, openCodeGoURL)
		}
		if got, _ := llm2["api_key_env"].(string); got != "OPENCODE_GO_API_KEY" {
			t.Fatalf("[%s] committed api_key_env = %q, want OPENCODE_GO_API_KEY", provider, got)
		}

		t.Logf("[%s] e2e OK: base_url=%s api_key_env=%s", provider, llm2["base_url"], llm2["api_key_env"])
	}
}
