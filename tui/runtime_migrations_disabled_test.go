package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProductionHasNoProjectMigrationCallers mechanically guards the option-2
// composition boundary. The retained migration package is historical/test-only;
// production TUI packages must not import it, run it, stamp it, or persist its
// notification/progress field.
func TestProductionHasNoProjectMigrationCallers(t *testing.T) {
	banned := []string{
		"internal/migrate",
		"migrate.Run(",
		"migrate.StampCurrent(",
		"IsAddonCommentNotified(",
		"CheckAddonComment(",
		"MarkAddonCommentNotified(",
		"addon_comment_cleanup_notified",
	}
	var violations []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == "internal/migrate" || path == "internal/globalmigrate" || strings.Contains(path, string(filepath.Separator)+".git") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		for _, token := range banned {
			if token == "migrate.Run(" && strings.Contains(text, "globalmigrate.Run(") {
				// The separate per-machine globalmigrate package is outside the
				// retired per-project registry boundary.
				continue
			}
			if strings.Contains(text, token) {
				violations = append(violations, path+": "+token)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("production project migration callers/imports remain:\n%s", strings.Join(violations, "\n"))
	}
}

// TestRuntimeMigrationRetirementArtifacts proves that the protected historical
// package/document remain while exactly the six authorized m040/preflight
// paths are absent. It intentionally checks paths, not Git status, so it is
// useful in the source tree and does not mutate evidence.
func TestRuntimeMigrationRetirementArtifacts(t *testing.T) {
	deleted := []string{
		"tui/internal/migrate/alias_conflict_preflight.go",
		"tui/internal/migrate/m040_shell_capability.go",
		"tui/internal/migrate/m040_shell_capability_test.go",
		"portal/internal/migrate/alias_conflict_preflight.go",
		"portal/internal/migrate/m040_shell_capability.go",
		"portal/internal/migrate/m040_shell_capability_test.go",
	}
	for _, rel := range deleted {
		if _, err := os.Stat(filepath.Join("..", rel)); !os.IsNotExist(err) {
			t.Errorf("authorized retired path %s still exists (err=%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join("..", "migration", "migration.md")); err != nil {
		t.Fatalf("protected migration/migration.md missing: %v", err)
	}
}
