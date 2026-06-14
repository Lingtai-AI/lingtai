package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInitWithRemote creates a git repo at dir and configures the given remote
// URL under the given remote name. Skips the test if git is unavailable.
func gitInitWithRemote(t *testing.T, dir, remoteName, url string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	if remoteName != "" && url != "" {
		run("remote", "add", remoteName, url)
	}
}

func TestGitRemoteForDir_PrefersOrigin(t *testing.T) {
	repo := t.TempDir()
	gitInitWithRemote(t, repo, "origin", "https://github.com/Lingtai-AI/example-skill.git")

	got := gitRemoteForDir(repo)
	want := "https://github.com/Lingtai-AI/example-skill.git"
	if got != want {
		t.Errorf("gitRemoteForDir = %q, want %q", got, want)
	}
}

func TestGitRemoteForDir_NoRemote(t *testing.T) {
	repo := t.TempDir()
	gitInitWithRemote(t, repo, "", "")

	if got := gitRemoteForDir(repo); got != "" {
		t.Errorf("gitRemoteForDir = %q, want empty (no remote configured)", got)
	}
}

func TestGitRemoteForDir_NotARepo(t *testing.T) {
	dir := t.TempDir()
	if got := gitRemoteForDir(dir); got != "" {
		t.Errorf("gitRemoteForDir = %q, want empty (not a git repo)", got)
	}
}

func TestGitRemoteForDir_OriginWinsOverOthers(t *testing.T) {
	repo := t.TempDir()
	gitInitWithRemote(t, repo, "upstream", "https://github.com/other/upstream.git")
	// Add origin after upstream; origin must still win.
	cmd := exec.Command("git", "remote", "add", "origin", "https://github.com/Lingtai-AI/wins.git")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}

	if got := gitRemoteForDir(repo); got != "https://github.com/Lingtai-AI/wins.git" {
		t.Errorf("gitRemoteForDir = %q, want origin URL", got)
	}
}

func TestGitRemoteForDir_FallsBackToFirstRemote(t *testing.T) {
	repo := t.TempDir()
	gitInitWithRemote(t, repo, "upstream", "https://github.com/other/upstream.git")

	if got := gitRemoteForDir(repo); got != "https://github.com/other/upstream.git" {
		t.Errorf("gitRemoteForDir = %q, want fallback to the single non-origin remote", got)
	}
}


func TestGitRemoteForDir_DiscoversParentWorktree(t *testing.T) {
	repo := t.TempDir()
	gitInitWithRemote(t, repo, "origin", "https://github.com/Lingtai-AI/parent.git")
	nested := filepath.Join(repo, "skills", "nested-skill")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := gitRemoteForDir(nested); got != "https://github.com/Lingtai-AI/parent.git" {
		t.Errorf("gitRemoteForDir(nested) = %q, want parent origin URL", got)
	}
}

func TestGitRemoteForDir_FallsBackToSortedFirstRemote(t *testing.T) {
	repo := t.TempDir()
	gitInitWithRemote(t, repo, "zeta", "https://github.com/other/zeta.git")
	cmd := exec.Command("git", "remote", "add", "alpha", "https://github.com/other/alpha.git")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add alpha: %v\n%s", err, out)
	}

	if got := gitRemoteForDir(repo); got != "https://github.com/other/alpha.git" {
		t.Errorf("gitRemoteForDir = %q, want sorted first non-origin remote", got)
	}
}

func TestScanLibrary_PopulatesRemoteForRepoBackedSkill(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// A library dir that is itself a git repo with an origin remote.
	libraryDir := filepath.Join(t.TempDir(), ".library")
	if err := os.MkdirAll(libraryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInitWithRemote(t, libraryDir, "origin", "https://github.com/Lingtai-AI/repo-backed.git")

	skillDir := filepath.Join(libraryDir, "repo-backed-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: repo-backed-skill\ndescription: A repo-backed skill\nversion: 1.0.0\n---\nBody.\n"), 0o644)

	skills, problems := scanLibrary(libraryDir)
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Remote != "https://github.com/Lingtai-AI/repo-backed.git" {
		t.Errorf("Remote = %q, want origin URL", skills[0].Remote)
	}
}

func TestScanLibrary_OmitsRemoteWhenNoRepo(t *testing.T) {
	libraryDir := filepath.Join(t.TempDir(), ".library")
	skillDir := filepath.Join(libraryDir, "loose-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: loose-skill\ndescription: Not repo-backed\nversion: 1.0.0\n---\nBody.\n"), 0o644)

	skills, _ := scanLibrary(libraryDir)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Remote != "" {
		t.Errorf("Remote = %q, want empty (no git repo)", skills[0].Remote)
	}
}

func TestBuildLibraryEntries_CarriesRemoteOntoCatalogEntry(t *testing.T) {
	skills := []skillEntry{
		{
			Name:        "repo-backed-skill",
			Description: "A repo-backed skill",
			Path:        "/tmp/repo-backed-skill/SKILL.md",
			Group:       "custom",
			Remote:      "https://github.com/Lingtai-AI/example-skill.git",
		},
		{
			Name:        "loose-skill",
			Description: "Not repo-backed",
			Path:        "/tmp/loose-skill/SKILL.md",
			Group:       "custom",
		},
	}

	entries := buildLibraryEntries("", "en", skills, nil)

	byLabel := map[string]MarkdownEntry{}
	for _, e := range entries {
		byLabel[e.Label] = e
	}
	if got := byLabel["repo-backed-skill"].Remote; got != "https://github.com/Lingtai-AI/example-skill.git" {
		t.Errorf("repo-backed entry Remote = %q, want origin URL", got)
	}
	if got := byLabel["loose-skill"].Remote; got != "" {
		t.Errorf("loose entry Remote = %q, want empty", got)
	}
}
