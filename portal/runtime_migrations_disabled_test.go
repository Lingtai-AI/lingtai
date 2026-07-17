package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPortalProductionHasNoProjectMigrationCallers keeps the Portal startup
// boundary mechanical: the retained migration package is historical/test-only
// and Portal production must not import, run, stamp, or persist it.
func TestPortalProductionHasNoProjectMigrationCallers(t *testing.T) {
	banned := []string{
		"internal/migrate",
		"migrate.Run(",
		"migrate.StampCurrent(",
		"addon_comment_cleanup_notified",
	}
	var violations []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == "internal/migrate" || strings.Contains(path, string(filepath.Separator)+".git") {
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
		t.Fatalf("Portal production project migration callers/imports remain:\n%s", strings.Join(violations, "\n"))
	}
}
