package preset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBundledLingtaiTuiHelp verifies the lingtai-tui-help skill ships with the
// binary: it is a recognized bundled skill, its SKILL.md and three localized
// slash-command assets are embedded and readable via ReadBundledSkillFile, and
// they extract to disk under utilities/.
func TestBundledLingtaiTuiHelp(t *testing.T) {
	if !BundledSkillNames()["lingtai-tui-help"] {
		t.Fatal("lingtai-tui-help is not a bundled skill")
	}

	assets := []string{
		"SKILL.md",
		"assets/slash-commands.en.md",
		"assets/slash-commands.zh.md",
		"assets/slash-commands.wen.md",
	}
	for _, rel := range assets {
		body, err := ReadBundledSkillFile("lingtai-tui-help", rel)
		if err != nil {
			t.Fatalf("ReadBundledSkillFile(lingtai-tui-help, %s): %v", rel, err)
		}
		if strings.TrimSpace(body) == "" {
			t.Errorf("bundled lingtai-tui-help/%s is empty", rel)
		}
	}

	// SKILL.md frontmatter must declare the skill name.
	skill, err := ReadBundledSkillFile("lingtai-tui-help", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(skill, "name: lingtai-tui-help") {
		t.Error("lingtai-tui-help SKILL.md missing name frontmatter")
	}

	// The assets extract to disk alongside the other utility skills.
	globalDir := t.TempDir()
	PopulateBundledLibrary("", globalDir)
	utilitiesDir := filepath.Join(globalDir, "utilities", "lingtai-tui-help")
	for _, rel := range assets {
		if _, err := os.Stat(filepath.Join(utilitiesDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected extracted lingtai-tui-help file %s: %v", rel, err)
		}
	}
}

// TestReadBundledSkillFileMissing confirms ReadBundledSkillFile surfaces an
// error for an absent path rather than returning empty content silently.
func TestReadBundledSkillFileMissing(t *testing.T) {
	if _, err := ReadBundledSkillFile("lingtai-tui-help", "assets/nope.md"); err == nil {
		t.Error("expected error reading a missing bundled skill file")
	}
}
