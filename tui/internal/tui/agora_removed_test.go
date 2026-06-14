package tui

import (
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// The /agora command and its AgoraModel browser were removed. These tests guard
// against the command (or its help/docs) creeping back in. The underlying
// network/recipe import infrastructure (NewAgoraProjectsModel, ScanAgoraRecipes,
// the firstrun recipe-import flow) is intentionally NOT covered here — only the
// removed slash command is.

// TestDefaultCommandsDoesNotKeepAgora ensures /agora is no longer a slash
// command in the palette.
func TestDefaultCommandsDoesNotKeepAgora(t *testing.T) {
	if _, ok := findCommand("agora"); ok {
		t.Fatal("DefaultCommands() should not keep agora as a command")
	}
}

// TestSlashCommandAssetsDoNotMentionAgora ensures the /help guides no longer
// document a removed command in any language.
func TestSlashCommandAssetsDoNotMentionAgora(t *testing.T) {
	for _, c := range helpLangs {
		content, err := preset.ReadBundledSkillFile(helpSkillName, c.asset)
		if err != nil {
			t.Fatalf("reading %s: %v", c.asset, err)
		}
		if strings.Contains(content, "/agora") {
			t.Errorf("%s still mentions the removed /agora command", c.asset)
		}
	}
}

// TestSwitchToAgoraDoesNotNavigate ensures the removed command name is inert:
// switchToView("agora") must not change the current view.
func TestSwitchToAgoraDoesNotNavigate(t *testing.T) {
	app := App{currentView: appViewMail, orchDir: t.TempDir(), projectDir: t.TempDir()}
	model, _ := app.switchToView("agora")
	if got := model.(App).currentView; got != appViewMail {
		t.Fatalf("switchToView(%q) currentView = %v, want unchanged (appViewMail)", "agora", got)
	}
}
