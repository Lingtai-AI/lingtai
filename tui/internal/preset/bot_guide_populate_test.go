package preset

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This is the real embed/materializer boundary, not a hand-built test fixture.
func TestPopulateBundledLibrary_BotGuide(t *testing.T) {
	const root = "skills/lingtai-bot-guide"
	want := map[string]bool{}
	for _, child := range []string{"initiation", "headless-bot", "channels", "operations", "avatars", "coverage"} {
		want["reference/"+child+"/SKILL.md"] = true
	}
	parent, err := skillsFS.ReadFile(root + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	catalog := map[string]bool{}
	for _, entry := range catalogEntryRE.FindAllStringSubmatch(string(parent), -1) {
		if catalog[entry[2]] {
			t.Errorf("duplicate catalog location %s", entry[2])
		}
		catalog[entry[2]] = true
	}
	assertSameNames(t, "bot guide catalog", want, catalog)
	if !BundledSkillNames()["lingtai-bot-guide"] {
		t.Fatal("bot guide missing from top-level catalog")
	}
	if len(BundledSkillNames()) != 11 {
		t.Errorf("top-level bundled skills = %d, want 11", len(BundledSkillNames()))
	}
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)
	extracted := filepath.Join(globalDir, "utilities", "lingtai-bot-guide")
	children := map[string]bool{}
	err = fs.WalkDir(skillsFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, root+"/")
		if rel != "SKILL.md" && strings.HasSuffix(rel, "/SKILL.md") {
			children[rel] = true
		}
		data, err := skillsFS.ReadFile(path)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(extracted, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if !bytes.Equal(data, got) {
			t.Errorf("extraction changed %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSameNames(t, "embedded bot references", want, children)
	if _, err := os.Stat(filepath.Join(globalDir, "utilities", "swiss-knife", "reference", "headless-bot")); !os.IsNotExist(err) {
		t.Fatalf("retired Swiss Knife bot subtree extracted: %v", err)
	}
	if _, err := fs.Stat(skillsFS, "skills/swiss-knife/reference/headless-bot"); !os.IsNotExist(err) {
		t.Fatalf("retired Swiss Knife bot subtree embedded: %v", err)
	}
}

func TestPopulateBundledLibrary_HeadlessBotTelegramModule(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "lingtai-bot-guide", "reference", "headless-bot")
	wantArgs := `"args": ["-m", "lingtai.mcp_servers.telegram"]`
	for _, rel := range []string{
		"SKILL.md",
		"scripts/create_telegram_bot_project.py",
	} {
		body, err := os.ReadFile(filepath.Join(utilitiesDir, rel))
		if err != nil {
			t.Fatalf("read bundled headless-bot file %s: %v", rel, err)
		}
		text := string(body)
		if strings.Contains(text, "lingtai_telegram") {
			t.Errorf("bundled headless-bot file %s contains stale Telegram module path", rel)
		}
		if !strings.Contains(text, wantArgs) {
			t.Errorf("bundled headless-bot file %s missing mcp.telegram args %q", rel, wantArgs)
		}
	}
}
