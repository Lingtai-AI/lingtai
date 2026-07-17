package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPortalProductionHasNoProjectMigrationCallers is kept beside the
// historical registry so it can run without the Portal web embed artifact. It
// scans production Go sources only; this package's retained tests are allowed
// to invoke the historical registry directly.
func TestPortalProductionHasNoProjectMigrationCallers(t *testing.T) {
	banned := []string{
		"internal/migrate",
		"migrate.Run(",
		"migrate.StampCurrent(",
		"addon_comment_cleanup_notified",
	}
	root := filepath.Join("..", "..")
	var violations []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if filepath.Clean(path) == filepath.Join("..", "..", "internal", "migrate") {
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
