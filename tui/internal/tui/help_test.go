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
