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
	PopulateBundledLibrary(globalDir)
	utilitiesDir := filepath.Join(globalDir, "utilities", "lingtai-tui-help")
	for _, rel := range assets {
		if _, err := os.Stat(filepath.Join(utilitiesDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected extracted lingtai-tui-help file %s: %v", rel, err)
		}
	}
}

// TestBtwHelpNegativeContract pins the semantic contract of the `/btw` help
// entry across all three locales: it must present /btw as a mirror inquiry that
// does NOT steer the active agent, and must redirect real instructions to a
// normal message. This guards against a well-meaning rewrite reintroducing the
// old "the agent reflects and responds without interrupting its work" phrasing,
// which read as if /btw interjects at the active main agent — the exact
// confusion this contract exists to prevent.
func TestBtwHelpNegativeContract(t *testing.T) {
	locales := []string{"en", "zh", "wen"}
	// Each locale must contain a marker for: (1) the mirror/separate-copy framing,
	// (2) an explicit negation that /btw does not steer/instruct, and (3) the
	// redirect to a normal message for real requests.
	markers := map[string]struct {
		mirror   string
		negate   string
		redirect string
	}{
		"en":  {mirror: "mirror", negate: "not a way to steer", redirect: "send a normal message"},
		"zh":  {mirror: "镜像", negate: "不是指挥", redirect: "改用普通消息"},
		"wen": {mirror: "镜身", negate: "非驭器灵之术", redirect: "改遣常讯"},
	}
	for _, loc := range locales {
		rel := "assets/slash-commands." + loc + ".md"
		body, err := ReadBundledSkillFile("lingtai-tui-help", rel)
		if err != nil {
			t.Fatalf("ReadBundledSkillFile(lingtai-tui-help, %s): %v", rel, err)
		}
		m := markers[loc]
		if !strings.Contains(body, m.mirror) {
			t.Errorf("%s: /btw help missing mirror framing %q", rel, m.mirror)
		}
		if !strings.Contains(body, m.negate) {
			t.Errorf("%s: /btw help missing negative contract %q", rel, m.negate)
		}
		if !strings.Contains(body, m.redirect) {
			t.Errorf("%s: /btw help missing normal-message redirect %q", rel, m.redirect)
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
