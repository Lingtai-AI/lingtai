package migrate

import (
	"fmt"
	"os"
	"path/filepath"
)

// migrateProceduresEnglishOnly removes stale per-language procedures
// overrides from ~/.lingtai-tui/procedures/<lang>/.
//
// Background: older TUI builds shipped localized procedures.md files
// (en/, zh/, wen/). Those were seeded into ~/.lingtai-tui/procedures/
// on launch and never cleaned up. The canonical procedures file is now
// English-only and lives at ~/.lingtai-tui/procedures/procedures.md,
// which the root populate() call refreshes on every launch. But
// ProceduresPath() still checks <lang>/procedures.md first, so any
// leftover wen/zh/en subdirs on disk shadow the fresh root file with
// stale content — including pre-rename vocabulary like
// psyche(memory, edit, ...) and library(submit, ...).
//
// This migration deletes every <lang>/ subdirectory under
// ~/.lingtai-tui/procedures/ unconditionally. The root procedures.md
// is left alone (it is rewritten on every launch by Bootstrap's
// populate() call — no action needed here). Next time an agent's
// refresh or fresh launch resolves ProceduresPath(), the root file
// will be picked up for every language.
//
// Takes the project-level lingtaiDir as input (per migration contract)
// but ignores it — the stale files live in the global ~/.lingtai-tui/
// dir, not under the project.
func migrateProceduresEnglishOnly(_ string) error {
	globalDir := globalTUIDir()
	if globalDir == "" {
		return nil // can't resolve; skip silently
	}
	proceduresDir := filepath.Join(globalDir, "procedures")
	entries, err := os.ReadDir(proceduresDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", proceduresDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(proceduresDir, e.Name())
		if err := os.RemoveAll(sub); err != nil {
			fmt.Printf("warning: failed to remove stale procedures subdir %s: %v\n", sub, err)
		}
	}
	return nil
}
