package tui

import (
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// helpLangs are the UI languages /help supports, paired with the slash-command
// asset each should resolve to.
var helpLangs = []struct {
	lang  string
	asset string
}{
	{"en", "assets/slash-commands.en.md"},
	{"zh", "assets/slash-commands.zh.md"},
	{"wen", "assets/slash-commands.wen.md"},
}

// TestSlashCommandAssetForLang verifies the language→asset mapping, including the
// English fallback for unknown locales.
func TestSlashCommandAssetForLang(t *testing.T) {
	for _, c := range helpLangs {
		if got := slashCommandsAsset(c.lang); got != c.asset {
			t.Errorf("slashCommandsAsset(%q) = %q, want %q", c.lang, got, c.asset)
		}
	}
	if got := slashCommandsAsset("fr"); got != "assets/slash-commands.en.md" {
		t.Errorf("slashCommandsAsset(unknown) = %q, want English asset", got)
	}
}

// TestEveryCommandInAllAssets guards the real maintenance risk: a slash command
// is added to DefaultCommands() but never described in the help assets. Every
// command must appear (as "/<name>") in all three language assets.
func TestEveryCommandInAllAssets(t *testing.T) {
	for _, c := range helpLangs {
		content, err := preset.ReadBundledSkillFile(helpSkillName, c.asset)
		if err != nil {
			t.Fatalf("reading %s: %v", c.asset, err)
		}
		if strings.TrimSpace(content) == "" {
			t.Fatalf("%s is empty", c.asset)
		}
		for _, cmd := range DefaultCommands() {
			if !strings.Contains(content, "/"+cmd.Name) {
				t.Errorf("%s does not mention command /%s", c.asset, cmd.Name)
			}
		}
	}
}

// TestLoadSlashCommandsSelectsAsset verifies /help loads the correct asset for
// each UI language by checking the loaded content matches the embedded asset.
func TestLoadSlashCommandsSelectsAsset(t *testing.T) {
	for _, c := range helpLangs {
		want, err := preset.ReadBundledSkillFile(helpSkillName, c.asset)
		if err != nil {
			t.Fatalf("reading %s: %v", c.asset, err)
		}
		if got := loadSlashCommands(c.lang); got != want {
			t.Errorf("loadSlashCommands(%q) did not return the %s asset", c.lang, c.asset)
		}
	}
	// Unknown locale falls back to English.
	wantEN, _ := preset.ReadBundledSkillFile(helpSkillName, "assets/slash-commands.en.md")
	if got := loadSlashCommands("fr"); got != wantEN {
		t.Error("loadSlashCommands(unknown) did not fall back to the English asset")
	}
}

// TestBuildHelpEntriesPerLanguage verifies /help builds a single entry whose
// content is the slash-command guide for the active UI language.
func TestBuildHelpEntriesPerLanguage(t *testing.T) {
	orig := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(orig) })

	for _, c := range helpLangs {
		i18n.SetLang(c.lang)
		entries := buildHelpEntries()
		if len(entries) != 1 {
			t.Fatalf("lang %s: buildHelpEntries() returned %d entries, want 1", c.lang, len(entries))
		}
		want, err := preset.ReadBundledSkillFile(helpSkillName, c.asset)
		if err != nil {
			t.Fatalf("reading %s: %v", c.asset, err)
		}
		if entries[0].Content != want {
			t.Errorf("lang %s: entry content is not the %s asset", c.lang, c.asset)
		}
	}
}

// TestNewHelpModelTitle sanity-checks the wrapper constructs with a title.
func TestNewHelpModelTitle(t *testing.T) {
	m := NewHelpModel()
	if m.inner.title == "" {
		t.Error("HelpModel inner viewer has empty title")
	}
}

func TestAgentRailKeyboardCollapseHelpContract(t *testing.T) {
	optionalF2ByLang := map[string]string{
		"en":  "when the terminal sends F2",
		"zh":  "终端实际发送 F2",
		"wen": "终端实送 F2",
	}
	for _, c := range helpLangs {
		content, err := preset.ReadBundledSkillFile(helpSkillName, c.asset)
		if err != nil {
			t.Fatalf("reading %s: %v", c.asset, err)
		}

		const heading = "### `/agents`"
		start := strings.Index(content, heading)
		if start < 0 {
			t.Fatalf("%s has no %s section", c.asset, heading)
		}
		section := content[start:]
		if next := strings.Index(section[len(heading):], "\n### "); next >= 0 {
			section = section[:len(heading)+next]
		}

		paragraphs := strings.Split(strings.TrimSpace(section), "\n\n")
		if len(paragraphs) < 3 {
			t.Fatalf("%s /agents help has no keyboard-collapse paragraph", c.asset)
		}
		collapseParagraph := paragraphs[len(paragraphs)-1]
		for _, required := range []string{"85", "84", "Ctrl+G", "fn/Globe+F2", "Tab", "/agents"} {
			if !strings.Contains(collapseParagraph, required) {
				t.Errorf("%s /agents keyboard-collapse paragraph does not mention %q", c.asset, required)
			}
		}
		optionalF2, ok := optionalF2ByLang[c.lang]
		if !ok {
			t.Fatalf("%s has no terminal-dependent F2 help contract for language %q", c.asset, c.lang)
		}
		if !strings.Contains(collapseParagraph, optionalF2) {
			t.Errorf("%s /agents keyboard-collapse paragraph does not qualify F2 as terminal-dependent with %q",
				c.asset, optionalF2)
		}
		if primary, optional := strings.Index(collapseParagraph, "Ctrl+G"), strings.Index(collapseParagraph, "fn/Globe+F2"); primary < 0 || optional < 0 || primary > optional {
			t.Errorf("%s /agents keyboard-collapse paragraph does not present Ctrl+G before optional fn/Globe+F2", c.asset)
		}
	}
}

func TestAgentRailVisibleControlsHelpContract(t *testing.T) {
	leftClickByLang := map[string]string{
		"en":  "left-click",
		"zh":  "左键点击",
		"wen": "鼠左击",
	}
	for _, c := range helpLangs {
		content, err := preset.ReadBundledSkillFile(helpSkillName, c.asset)
		if err != nil {
			t.Fatalf("reading %s: %v", c.asset, err)
		}

		const heading = "### `/agents`"
		start := strings.Index(content, heading)
		if start < 0 {
			t.Fatalf("%s has no %s section", c.asset, heading)
		}
		section := content[start:]
		if next := strings.Index(section[len(heading):], "\n### "); next >= 0 {
			section = section[:len(heading)+next]
		}
		paragraphs := strings.Split(strings.TrimSpace(section), "\n\n")
		if len(paragraphs) < 3 {
			t.Fatalf("%s /agents help has no visible-control paragraph", c.asset)
		}
		controlParagraph := paragraphs[len(paragraphs)-1]
		for _, required := range []string{
			"[<]",
			"[>]",
			leftClickByLang[c.lang],
			"85",
			"84",
			"Ctrl+G",
			"fn/Globe+F2",
			"Tab",
			"/agents",
		} {
			if !strings.Contains(controlParagraph, required) {
				t.Errorf("%s /agents visible-control paragraph does not mention %q", c.asset, required)
			}
		}
		for _, obsolete := range []string{"[F2<]", "[F2>]"} {
			if strings.Contains(controlParagraph, obsolete) {
				t.Errorf("%s /agents visible-control paragraph still advertises obsolete control %q", c.asset, obsolete)
			}
		}
	}
}
