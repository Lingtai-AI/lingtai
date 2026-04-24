package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// migrateRecipeLangSuffix strips legacy "-zh" / "-wen" suffixes from the
// recipe name stored in .lingtai/.tui-asset/.recipe.
//
// Background: before the recipe-redesign, each recipe shipped as three
// sibling directories distinguished by dir-name language suffix (e.g.
// greeter, greeter-zh, greeter-wen). The TUI filtered the recipe picker
// by suffix at display time and stored the picked name verbatim in
// .tui-asset/.recipe. After the redesign, a single recipe bundle carries
// all locale variants internally (under its .recipe/<lang>/ subfolders),
// so the recipe ID is language-agnostic (greeter, not greeter-zh).
//
// Projects created with the old TUI may have stored suffixed names like
// "greeter-zh" in their state file. Without this migration those names
// would no longer resolve to any recipe on disk (the bundled recipes have
// been renamed and the -zh / -wen siblings deleted). This migration
// normalizes them to the bare recipe ID.
//
// Safe no-op if:
//   - .lingtai/.tui-asset/.recipe doesn't exist (pre-m008 or purged project)
//   - the stored recipe field has no -zh / -wen suffix
//   - the file is unreadable / malformed (logged, not errored)
func migrateRecipeLangSuffix(lingtaiDir string) error {
	recipePath := filepath.Join(lingtaiDir, ".tui-asset", ".recipe")
	data, err := os.ReadFile(recipePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// Non-fatal: log and move on.
		fmt.Fprintf(os.Stderr, "warning: m022 could not read %s: %v\n", recipePath, err)
		return nil
	}

	var state struct {
		Recipe    string `json:"recipe"`
		CustomDir string `json:"custom_dir,omitempty"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupt file — log and skip; the TUI's own load path will fall
		// back to a zero state and let the user re-pick on next /setup.
		fmt.Fprintf(os.Stderr, "warning: m022 could not parse %s: %v\n", recipePath, err)
		return nil
	}

	trimmed := stripRecipeLangSuffix(state.Recipe)
	if trimmed == state.Recipe {
		return nil // nothing to do
	}
	state.Recipe = trimmed

	updated, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal updated recipe state: %w", err)
	}
	tmpPath := recipePath + ".tmp"
	if err := os.WriteFile(tmpPath, updated, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, recipePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s: %w", tmpPath, err)
	}
	return nil
}

// stripRecipeLangSuffix removes a trailing "-zh" or "-wen" from a recipe
// ID. Returns the input unchanged if no suffix is present.
func stripRecipeLangSuffix(id string) string {
	for _, suffix := range []string{"-zh", "-wen"} {
		if strings.HasSuffix(id, suffix) {
			return strings.TrimSuffix(id, suffix)
		}
	}
	return id
}
