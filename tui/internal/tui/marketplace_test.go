package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

func TestDefaultCommandsIncludesMarketplace(t *testing.T) {
	cmd, ok := findCommand("marketplace")
	if !ok {
		t.Fatal("DefaultCommands() missing marketplace command")
	}
	if cmd.Description != "palette.marketplace" || cmd.Detail != "cmd.marketplace" {
		t.Fatalf("marketplace command keys = (%q, %q), want (palette.marketplace, cmd.marketplace)", cmd.Description, cmd.Detail)
	}
}

func TestMarketplaceCommandOpensMarketplaceView(t *testing.T) {
	app := App{globalDir: t.TempDir(), projectDir: t.TempDir()}
	model, _ := app.switchToView("marketplace")
	got := model.(App)
	if got.currentView != appViewMarketplace {
		t.Fatalf("switchToView(%q) currentView = %v, want appViewMarketplace", "marketplace", got.currentView)
	}
}

func TestMarketplaceModelRendersCommunityEntry(t *testing.T) {
	// Isolate HOME so this machine's real ~/lingtai-agora/recipes/ doesn't add
	// local entries that push the curated community example out of the focused
	// detail pane.
	t.Setenv("HOME", t.TempDir())
	// Empty global dir → only curated community entries (incl. roundtable).
	m := NewMarketplaceModel(t.TempDir(), t.TempDir(), "en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := m.View()
	if view == "" {
		t.Fatal("View() returned empty string")
	}
	for _, want := range []string{"Roundtable", "rawpaper123"} {
		if !strings.Contains(view, want) {
			t.Fatalf("marketplace view should mention %q; got:\n%s", want, view)
		}
	}
}

func TestMarketplaceEscReturnsToMail(t *testing.T) {
	m := NewMarketplaceModel(t.TempDir(), t.TempDir(), "en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("pressing q should emit a command")
	}
	msg := cmd()
	vc, ok := msg.(ViewChangeMsg)
	if !ok {
		t.Fatalf("q command produced %T, want ViewChangeMsg", msg)
	}
	if vc.View != "mail" {
		t.Fatalf("q should return to mail, got %q", vc.View)
	}
}

func TestMarketplaceAppLevelQAndEscReturnToMailNotQuit(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{name: "q", msg: tea.KeyPressMsg{Code: 'q', Text: "q"}},
		{name: "esc", msg: tea.KeyPressMsg{Code: tea.KeyEsc}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := App{globalDir: t.TempDir(), projectDir: t.TempDir()}
			model, _ := app.switchToView("marketplace")
			app = model.(App)

			_, cmd := app.Update(tc.msg)
			msg := runCmd(cmd)
			if _, ok := msg.(tea.QuitMsg); ok {
				t.Fatalf("%s in /marketplace quit the app; want return to mail", tc.name)
			}
			vc, ok := msg.(ViewChangeMsg)
			if !ok || vc.View != "mail" {
				t.Fatalf("%s command = %#v, want ViewChangeMsg{View: mail}", tc.name, msg)
			}
		})
	}
}

func TestMarketplaceDetailShowsSkillAndSafety(t *testing.T) {
	// The detail markdown must surface the external-skill half and the
	// safety/preview boundary — the two things that define the marketplace.
	entries := preset.BuiltinMarketplaceEntries()
	if len(entries) == 0 {
		t.Fatal("no marketplace entries to inspect")
	}
	detail := marketplaceEntryDetail(entries[0])
	if !strings.Contains(detail, i18n.T("marketplace.field.skill")) {
		t.Error("detail should label the external skill library")
	}
	if !strings.Contains(detail, i18n.T("marketplace.field.safety")) {
		t.Error("detail should include a preview & safety section")
	}
	if !strings.Contains(detail, entries[0].LibraryName) {
		t.Error("detail should name the external skill library")
	}
	if strings.Contains(detail, "git clone ") {
		t.Error("community preview should not imply an unvalidated one-step git clone install")
	}
	if !strings.Contains(detail, entries[0].SourceURL) {
		t.Error("detail should show the community source URL")
	}
	if !strings.Contains(detail, "~/lingtai-agora/recipes/"+entries[0].Recipe.ID) {
		t.Error("detail should show the expected local recipe bundle path")
	}
}
