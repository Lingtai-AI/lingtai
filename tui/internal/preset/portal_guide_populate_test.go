package preset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPopulateBundledLibrary_PortalGuideNestedReferences verifies that the
// embedded utility-library copier preserves the portal guide's router and
// nested reference files on disk.
func TestPopulateBundledLibrary_PortalGuideNestedReferences(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "lingtai-portal-guide")
	for _, rel := range []string{
		"SKILL.md",
		"reference/overview/SKILL.md",
		"reference/topology-and-api/SKILL.md",
		"reference/lifecycle-and-recording/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled portal-guide file %s to be extracted: %v", rel, err)
		}
	}

	rootBody, err := os.ReadFile(filepath.Join(utilitiesDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"name: portal-guide-overview",
		"name: portal-guide-topology-and-api",
		"name: portal-guide-lifecycle-and-recording",
		".lingtai/.portal/port",
	} {
		if !strings.Contains(string(rootBody), want) {
			t.Errorf("extracted portal-guide root missing %q", want)
		}
	}
}
