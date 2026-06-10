package tui

import (
	"strings"
	"testing"
)

func TestDefaultCommandsHaveValidUniqueNames(t *testing.T) {
	seen := make(map[string]struct{})

	for _, cmd := range DefaultCommands() {
		if cmd.Name == "" {
			t.Fatal("DefaultCommands() contains an empty command name")
		}
		if strings.ContainsAny(cmd.Name, " /\t\n") {
			t.Errorf("command name %q contains whitespace or slash", cmd.Name)
		}
		if _, ok := seen[cmd.Name]; ok {
			t.Errorf("DefaultCommands() contains duplicate command name %q", cmd.Name)
		}
		seen[cmd.Name] = struct{}{}
	}
}
