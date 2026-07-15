package preset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLingtaiUpdateSkillRouterCatalogAndExtraction(t *testing.T) {
	children := []string{"install", "update-tui", "detection", "diagnosis", "homebrew", "mainland-china"}
	parent, err := ReadBundledSkillFile("lingtai-update", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parent, "Nested reference catalog") || !strings.Contains(parent, "Routing table") {
		t.Fatal("lingtai-update router must contain both synchronized routing sections")
	}
	if !strings.Contains(parent, "description:") || !strings.Contains(parent, "Use when") || !strings.Contains(parent, "last_changed_at:") {
		t.Fatal("lingtai-update router is missing required trigger/version frontmatter")
	}
	if !strings.Contains(parent, "system-manual") || strings.Contains(parent, "lingtai-kernel/") {
		t.Fatal("router must route kernel work narratively without inventing a cross-repo path")
	}
	for _, child := range children {
		location := "reference/" + child + "/SKILL.md"
		if strings.Count(parent, location) != 2 {
			t.Errorf("router location %q appears %d times, want once in catalog and once in routing table", location, strings.Count(parent, location))
		}
		body, err := ReadBundledSkillFile("lingtai-update", location)
		if err != nil {
			t.Fatalf("missing nested child %s: %v", location, err)
		}
		if !strings.Contains(body, "Nested `lingtai-update` reference") {
			t.Errorf("%s does not identify itself as nested under lingtai-update", location)
		}
		if !strings.HasPrefix(body, "---\nname: lingtai-update-") {
			t.Errorf("%s has unexpected frontmatter name", location)
		}
		if !strings.Contains(body, "description:") || !strings.Contains(body, "Use when") {
			t.Errorf("%s is missing trigger-oriented description frontmatter", location)
		}
	}

	if !BundledSkillNames()["lingtai-update"] {
		t.Fatal("lingtai-update is not in the embedded skill inventory")
	}
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)
	root := filepath.Join(globalDir, "utilities", "lingtai-update")
	paths := []string{"SKILL.md"}
	for _, child := range children {
		paths = append(paths, filepath.Join("reference", child, "SKILL.md"))
	}
	for _, rel := range paths {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("extracted skill missing %s: %v", rel, err)
		}
	}
}
