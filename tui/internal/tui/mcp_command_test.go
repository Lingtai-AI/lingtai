package tui

import "testing"

// findCommand returns the Command with the given name from DefaultCommands,
// or (Command{}, false) if absent.
func findCommand(name string) (Command, bool) {
	for _, cmd := range DefaultCommands() {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return Command{}, false
}

// /mcp is the primary, human-facing command for the MCP control panel.
func TestDefaultCommandsIncludesMCP(t *testing.T) {
	cmd, ok := findCommand("mcp")
	if !ok {
		t.Fatal("DefaultCommands() missing mcp command")
	}
	if cmd.Description != "palette.mcp" || cmd.Detail != "cmd.mcp" {
		t.Fatalf("mcp command keys = (%q, %q), want (palette.mcp, cmd.mcp)", cmd.Description, cmd.Detail)
	}
}

func TestDefaultCommandsIncludesContext(t *testing.T) {
	cmd, ok := findCommand("context")
	if !ok {
		t.Fatal("DefaultCommands() missing context command")
	}
	if cmd.Description != "palette.context" || cmd.Detail != "cmd.context" {
		t.Fatalf("context command keys = (%q, %q), want (palette.context, cmd.context)", cmd.Description, cmd.Detail)
	}
}

// /addon should not remain as a compatibility alias; /mcp is the only
// human-facing command for this control panel.
func TestDefaultCommandsDoesNotKeepAddonAlias(t *testing.T) {
	if _, ok := findCommand("addon"); ok {
		t.Fatal("DefaultCommands() should not keep addon as a command alias")
	}
}

func TestMCPCommandOpensControlPanelView(t *testing.T) {
	app := App{orchDir: t.TempDir(), projectDir: t.TempDir()}
	model, _ := app.switchToView("mcp")
	got := model.(App)
	if got.currentView != appViewAddon {
		t.Fatalf("switchToView(%q) currentView = %v, want appViewAddon", "mcp", got.currentView)
	}
}

func TestContextCommandOpensDetailView(t *testing.T) {
	dir := t.TempDir()
	app := App{orchDir: dir, projectDir: dir}

	model, _ := app.handlePaletteCommand("context", "")
	got := model.(App)

	if got.currentView != appViewProps {
		t.Fatalf("context command currentView = %v, want appViewProps", got.currentView)
	}
	if !got.props.detailOpen {
		t.Fatal("context command should open the props detail view")
	}
}
