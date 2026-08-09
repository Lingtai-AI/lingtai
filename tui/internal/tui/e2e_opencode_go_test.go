package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

const openCodeGoURL = "https://opencode.ai/zen/go/v1"

// openEditorAsHostDoes builds the editor exactly the way a host does:
// NewPresetEditorModel derives isBuiltin from preset.IsTemplate, and templates
// reach the host with Source == SourceTemplate because they are loaded off
// disk from presets/templates/ (preset.BuiltinPresets returns them in-memory
// with a zero Source, which is *not* what the user's editor sees). Passing the
// flag explicitly as false — as the first draft of this test did — skips the
// whole `if m.isBuiltin && …` branch in commit(), which is where the credential
// slot is decided.
func openEditorAsHostDoes(t *testing.T, provider string) PresetEditorModel {
	t.Helper()
	p := builtinPresetForEditorTest(t, provider)
	p.Source = preset.SourceTemplate
	if !preset.IsTemplate(p) {
		t.Fatalf("[%s] test setup wrong: preset is not a template", provider)
	}
	m := NewPresetEditorModel(p, "en", nil, "")
	if !m.isBuiltin {
		t.Fatalf("[%s] NewPresetEditorModel derived isBuiltin=false; the production commit() branch would not run", provider)
	}
	return m
}

// pressKey feeds one real key event through Update, the same entry point the
// Bubble Tea runtime uses.
func pressKey(t *testing.T, m PresetEditorModel, code rune) (PresetEditorModel, tea.Cmd) {
	t.Helper()
	return m.Update(tea.KeyPressMsg{Code: code})
}

// walkCursorTo presses ↓ until the focused row is `want`, using real key
// events so the test also proves the row is reachable by keyboard (hidden
// rows are skipped by moveCursor, so a plain index assignment would not).
func walkCursorTo(t *testing.T, m PresetEditorModel, want editorField) PresetEditorModel {
	t.Helper()
	target := editorFieldOrderIndex(t, want)
	for i := 0; m.cursor != target; i++ {
		if i > len(editorFieldOrder) {
			t.Fatalf("cursor never reached field %v (stuck at %d)", want, m.cursor)
		}
		m, _ = pressKey(t, m, tea.KeyDown)
	}
	return m
}

// saveViaKeyboard drives the real save gate: Tab to the [Save] row, Enter to
// commit. Returns the PresetEditorCommitMsg the host receives.
func saveViaKeyboard(t *testing.T, m PresetEditorModel) PresetEditorCommitMsg {
	t.Helper()
	m, _ = pressKey(t, m, tea.KeyTab)
	if m.cursor != saveFieldIndex {
		t.Fatalf("Tab did not land on [Save] (cursor=%d, want %d)", m.cursor, saveFieldIndex)
	}
	m, cmd := pressKey(t, m, tea.KeyEnter)
	if m.saveErr != "" {
		t.Fatalf("save rejected by the editor gate: %s", m.saveErr)
	}
	if m.mode != emBrowse {
		t.Fatalf("save left the editor in mode %v, not emBrowse — an overlay intervened and this test must drive it", m.mode)
	}
	if cmd == nil {
		t.Fatal("Enter on [Save] produced no command")
	}
	msg, ok := cmd().(PresetEditorCommitMsg)
	if !ok {
		t.Fatalf("save produced %T, want PresetEditorCommitMsg", cmd())
	}
	return msg
}

func committedLLM(t *testing.T, p preset.Preset) map[string]interface{} {
	t.Helper()
	llm, ok := p.Manifest["llm"].(map[string]interface{})
	if !ok {
		t.Fatalf("committed manifest has no llm map: %#v", p.Manifest)
	}
	return llm
}

// TestE2EOpenCodeGoOptionSurvivesSave is the end-to-end contract for the
// OpenCode Go base_url option on all three providers that offer it: opening
// the shipped template, walking to the base_url row with ↓, pressing → until
// OpenCode Go is selected, and saving with Tab+Enter must produce a preset
// that actually resolves through OPENCODE_GO_API_KEY.
//
// Every step goes through the production path — NewPresetEditorModel (so
// isBuiltin is real), tea.KeyPressMsg through Update (so the →/l binding is
// exercised), and m.commit() via the [Save] row (so normalizeLLMForCommit,
// AutoSavedName and the api_key_env gate all run). The assertion is on the
// state the *user* ends up with: the committed manifest after the host's
// stampAutoEnvVar, not a pre-commit clone of the working copy.
func TestE2EOpenCodeGoOptionSurvivesSave(t *testing.T) {
	tempTestHome(t)

	for _, provider := range []string{"deepseek", "zhipu", "minimax"} {
		t.Run(provider, func(t *testing.T) {
			regions := preset.ProviderRegionURLs[provider]
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

			m := openEditorAsHostDoes(t, provider)
			if got := asString(m.llmMap()["base_url"]); got != regions[0].URL {
				t.Fatalf("[%s] template base_url = %q, want the first region %q", provider, got, regions[0].URL)
			}

			// ↓ to the base_url row, then → until OpenCode Go is selected.
			m = walkCursorTo(t, m, feBaseURL)
			for i := 0; asString(m.llmMap()["base_url"]) != openCodeGoURL; i++ {
				if i > len(regions) {
					t.Fatalf("[%s] → never landed on OpenCode Go; base_url = %q", provider, asString(m.llmMap()["base_url"]))
				}
				m, _ = pressKey(t, m, tea.KeyRight)
			}
			if got := asString(m.llmMap()["api_key_env"]); got != "OPENCODE_GO_API_KEY" {
				t.Fatalf("[%s] working api_key_env = %q, want OPENCODE_GO_API_KEY", provider, got)
			}

			// Tab + Enter: the real save gate.
			msg := saveViaKeyboard(t, m)
			if msg.Preset.Name == provider {
				t.Fatalf("[%s] commit kept the template name; the built-in should have been auto-renamed", provider)
			}
			if msg.Preset.Source != preset.SourceSaved {
				t.Fatalf("[%s] committed Source = %v, want SourceSaved", provider, msg.Preset.Source)
			}
			llm := committedLLM(t, msg.Preset)
			if got := asString(llm["base_url"]); got != openCodeGoURL {
				t.Fatalf("[%s] committed base_url = %q, want %q", provider, got, openCodeGoURL)
			}
			if got := asString(llm["api_key_env"]); got != "OPENCODE_GO_API_KEY" {
				t.Fatalf("[%s] committed api_key_env = %q, want OPENCODE_GO_API_KEY (the option's whole point)", provider, got)
			}

			// The host stamps a gap-filled slot into presets that still have
			// an empty api_key_env. A preset bound to the OpenCode Go account
			// must come out the other side untouched — no PROVIDER_N slot,
			// and in particular no fabricated CN/INTL region suffix.
			stamped := stampAutoEnvVar(msg.Preset, map[string]string{"OPENCODE_GO_API_KEY": "sk-live"})
			if got := asString(committedLLM(t, stamped)["api_key_env"]); got != "OPENCODE_GO_API_KEY" {
				t.Fatalf("[%s] after stampAutoEnvVar api_key_env = %q, want OPENCODE_GO_API_KEY", provider, got)
			}
		})
	}
}

// TestE2EEditedBuiltinWithoutRegionEnvStillGetsFreshSlot is the other half of
// the api_key_env gate in commit(): narrowing the delete to "keep only a
// cross-provider account a region row declares" must not weaken the original
// rule. Editing a built-in in any other way still drops the template's shared
// slot so stampAutoEnvVar can allocate a private one, and the stamped name
// still carries the CN/INTL region suffix for a genuine regional endpoint.
//
// The deepseek case is the counter-case that matters most: its DEFAULT region
// row declares an Env (DeepSeek API -> DEEPSEEK_API_KEY), so an edit that never
// touches base_url would keep the template's shared slot if the exception were
// keyed on "the row declares an Env" alone — and the next deepseek preset the
// user saves would overwrite the first one's key in cfg.Keys.
func TestE2EEditedBuiltinWithoutRegionEnvStillGetsFreshSlot(t *testing.T) {
	tempTestHome(t)

	for _, tc := range []struct {
		provider string
		editRow  editorField // the one row this case cycles with →
		wantBase int         // region index base_url must sit on afterwards
		want     string
	}{
		{"zhipu", feBaseURL, 1, "ZHIPU_INTL_1_API_KEY"},     // CN -> INTL: an Env-less region row
		{"minimax", feBaseURL, 1, "MINIMAX_INTL_1_API_KEY"}, // CN -> INTL: an Env-less region row
		{"deepseek", feModel, 0, "DEEPSEEK_1_API_KEY"},      // model only; base_url stays on DeepSeek API
	} {
		t.Run(tc.provider, func(t *testing.T) {
			regions := preset.ProviderRegionURLs[tc.provider]
			m := openEditorAsHostDoes(t, tc.provider)
			m = walkCursorTo(t, m, tc.editRow)
			m, _ = pressKey(t, m, tea.KeyRight)

			if !m.hasSemanticEdits() {
				t.Fatalf("[%s] → on %v changed nothing; commit()'s isBuiltin branch would not run", tc.provider, tc.editRow)
			}
			if got := asString(m.llmMap()["base_url"]); got != regions[tc.wantBase].URL {
				t.Fatalf("[%s] base_url = %q, want %q (%s)", tc.provider, got, regions[tc.wantBase].URL, regions[tc.wantBase].Label)
			}

			msg := saveViaKeyboard(t, m)
			llm := committedLLM(t, msg.Preset)
			if _, present := llm["api_key_env"]; present {
				t.Fatalf("[%s] committed api_key_env = %q, want it dropped so the template's shared slot stays clean", tc.provider, asString(llm["api_key_env"]))
			}

			stamped := stampAutoEnvVar(msg.Preset, nil)
			if got := asString(committedLLM(t, stamped)["api_key_env"]); got != tc.want {
				t.Fatalf("[%s] after stampAutoEnvVar api_key_env = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}
