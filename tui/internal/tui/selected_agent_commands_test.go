package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPaletteAgentCommandsFollowCurrentConversation(t *testing.T) {
	fixture := newDirectAffinityFixture(t, false)
	app, _ := directAffinityActivate(t, fixture.app, fixture.targetA.AgentID)
	app.orchDir = t.TempDir()
	app.orchName = "Main"

	for _, command := range []string{"sleep", "suspend"} {
		t.Run(command, func(t *testing.T) {
			signal := "." + command
			targetPath := filepath.Join(fixture.targetA.Directory, signal)
			mainPath := filepath.Join(app.orchDir, signal)
			_ = os.Remove(targetPath)
			_ = os.Remove(mainPath)
			t.Cleanup(func() {
				_ = os.Remove(targetPath)
				_ = os.Remove(mainPath)
			})

			updated, _ := app.handlePaletteCommand(command, "")
			app = updated.(App)

			if _, err := os.Stat(targetPath); err != nil {
				t.Errorf("/%s did not target selected agent %q: %v", command, fixture.targetA.Directory, err)
			}
			if _, err := os.Stat(mainPath); !os.IsNotExist(err) {
				t.Errorf("/%s still targeted Main %q while direct agent was current", command, app.orchDir)
			}
		})
	}
}

func TestPaletteAgentCommandsKeepMainFallback(t *testing.T) {
	fixture := newDirectAffinityFixture(t, false)
	app := fixture.app
	app.orchDir = t.TempDir()
	app.orchName = "Main"
	mainPath := filepath.Join(app.orchDir, ".sleep")
	_ = os.Remove(mainPath)
	t.Cleanup(func() { _ = os.Remove(mainPath) })

	updated, _ := app.handlePaletteCommand("sleep", "")
	app = updated.(App)

	if _, err := os.Stat(mainPath); err != nil {
		t.Fatalf("/sleep with Main current did not retain Main target %q: %v", app.orchDir, err)
	}
}

func TestPaletteAgentOwnedViewsFollowCurrentConversation(t *testing.T) {
	fixture := newDirectAffinityFixture(t, false)
	base, _ := directAffinityActivate(t, fixture.app, fixture.targetA.AgentID)
	base.orchDir = t.TempDir()
	base.orchName = "Main"

	tests := []struct {
		command     string
		selectedDir func(App) string
	}{
		{command: "settings", selectedDir: func(a App) string { return a.settings.orchDir }},
		{command: "kanban", selectedDir: func(a App) string { return a.props.orchDir }},
		{command: "daemons", selectedDir: func(a App) string { return a.daemons.orchDir }},
		{command: "notification", selectedDir: func(a App) string { return a.notification.agentDir }},
		{command: "skills", selectedDir: func(a App) string { return a.library.selectedDir }},
		{command: "knowledge", selectedDir: func(a App) string { return a.knowledge.selectedDir }},
		{command: "library", selectedDir: func(a App) string { return a.knowledge.selectedDir }},
		{command: "codex", selectedDir: func(a App) string { return a.knowledge.selectedDir }},
		{command: "system", selectedDir: func(a App) string { return a.system.selectedDir }},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			updated, _ := base.handlePaletteCommand(tt.command, "")
			app := updated.(App)
			if got := tt.selectedDir(app); got != fixture.targetA.Directory {
				t.Fatalf("/%s targeted %q, want selected agent %q", tt.command, got, fixture.targetA.Directory)
			}
		})
	}
}
