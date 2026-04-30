package tui

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBuildSkillFolderEntries_RecipeSkill verifies the drill-in catalog
// against the real lingtai-recipe skill folder, which has SKILL.md at root
// plus reference/, scripts/, and assets/ subdirectories.
func TestBuildSkillFolderEntries_RecipeSkill(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "lingtai-recipe")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is lingtai-recipe missing?")
	}

	// SKILL.md must be first.
	if entries[0].Label != "SKILL.md" {
		t.Errorf("first entry = %q, want SKILL.md", entries[0].Label)
	}

	// Group labels should include references, scripts, assets.
	groups := make(map[string]int)
	for _, e := range entries {
		if e.Group != "" {
			groups[e.Group]++
		}
	}
	for _, want := range []string{"reference", "scripts", "assets"} {
		if groups[want] == 0 {
			t.Errorf("expected non-empty group %q, groups=%v", want, groups)
		}
	}

	// No hidden entries (.pytest_cache) should leak in.
	for _, e := range entries {
		if strings.Contains(e.Label, ".pytest_cache") {
			t.Errorf("hidden entry leaked: %s", e.Label)
		}
	}

	// Python scripts should be pre-rendered as fenced python blocks.
	foundPy := false
	for _, e := range entries {
		if strings.HasSuffix(e.Label, ".py") {
			foundPy = true
			if !strings.Contains(e.Content, "```python") {
				t.Errorf("python entry %q not fenced as python: %q", e.Label, firstLineOf(e.Content))
			}
		}
	}
	if !foundPy {
		t.Error("no python scripts found in lingtai-recipe — did the fixture change?")
	}
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
