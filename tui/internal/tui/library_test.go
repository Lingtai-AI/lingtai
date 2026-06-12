package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanLibrary_FollowsSymlinks(t *testing.T) {
	targetDir := t.TempDir()
	os.WriteFile(filepath.Join(targetDir, "SKILL.md"), []byte("---\nname: symlinked-skill\ndescription: A symlinked skill\nversion: 1.0.0\n---\nBody here.\n"), 0o644)

	libraryDir := filepath.Join(t.TempDir(), ".library")
	os.MkdirAll(libraryDir, 0o755)
	os.Symlink(targetDir, filepath.Join(libraryDir, "test-skill-en"))

	regularDir := filepath.Join(libraryDir, "regular-skill")
	os.MkdirAll(regularDir, 0o755)
	os.WriteFile(filepath.Join(regularDir, "SKILL.md"), []byte("---\nname: regular-skill\ndescription: A regular skill\nversion: 1.0.0\n---\nBody.\n"), 0o644)

	skills, problems := scanLibrary(libraryDir)
	if len(problems) != 0 {
		t.Errorf("unexpected problems: %v", problems)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	names := []string{skills[0].Name, skills[1].Name}
	if names[0] != "regular-skill" || names[1] != "symlinked-skill" {
		t.Errorf("skill names = %v, want [regular-skill, symlinked-skill]", names)
	}
}

func TestScanLibrary_SkipsBrokenSymlinks(t *testing.T) {
	libraryDir := filepath.Join(t.TempDir(), ".library")
	os.MkdirAll(libraryDir, 0o755)

	os.Symlink("/nonexistent", filepath.Join(libraryDir, "broken-skill"))

	skills, problems := scanLibrary(libraryDir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
	if len(problems) != 0 {
		t.Errorf("expected 0 problems, got %d", len(problems))
	}
}

// ── readLibraryPaths: resolved-manifest artifact vs raw init.json ──────────

func writeAgentFile(t *testing.T, agentDir, rel, content string) {
	t.Helper()
	path := filepath.Join(agentDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func touchLibraryAgentFile(t *testing.T, agentDir, rel string, mod time.Time) {
	t.Helper()
	path := filepath.Join(agentDir, rel)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestReadLibraryPaths_PrefersResolvedArtifact(t *testing.T) {
	agentDir := t.TempDir()
	// Stale init.json snapshot declares one path...
	writeAgentFile(t, agentDir, "init.json",
		`{"manifest": {"capabilities": {"skills": {"paths": ["~/stale-from-init"]}}}}`)
	// ...but the kernel-resolved artifact carries the effective merge.
	writeAgentFile(t, agentDir, "system/manifest.resolved.json",
		`{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1, "source": "kernel",
		  "manifest": {"capabilities": {"skills": {"paths": ["~/from-preset", "~/from-init-extra"]}}}}`)

	got := readLibraryPaths(agentDir)
	want := []string{"~/from-preset", "~/from-init-extra"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("readLibraryPaths = %v, want %v", got, want)
	}
}

func TestReadLibraryPaths_FallsBackToInitWhenArtifactAbsent(t *testing.T) {
	agentDir := t.TempDir()
	writeAgentFile(t, agentDir, "init.json",
		`{"manifest": {"capabilities": {"skills": {"paths": ["~/from-init"]}}}}`)

	got := readLibraryPaths(agentDir)
	if len(got) != 1 || got[0] != "~/from-init" {
		t.Errorf("readLibraryPaths = %v, want [~/from-init]", got)
	}
}

func TestReadLibraryPaths_FallsBackToInitWhenArtifactMalformed(t *testing.T) {
	agentDir := t.TempDir()
	writeAgentFile(t, agentDir, "init.json",
		`{"manifest": {"capabilities": {"skills": {"paths": ["~/from-init"]}}}}`)

	cases := map[string]string{
		"truncated JSON":          `{"schema": "lingtai.manifest.resolved/v1", "manifest": {`,
		"manifest not object":     `{"schema": "lingtai.manifest.resolved/v1", "manifest": "nope"}`,
		"missing manifest":        `{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1}`,
		"skills paths wrong type": `{"manifest": {"capabilities": {"skills": {"paths": "not-a-list"}}}}`,
	}
	for name, artifact := range cases {
		writeAgentFile(t, agentDir, "system/manifest.resolved.json", artifact)
		got := readLibraryPaths(agentDir)
		if len(got) != 1 || got[0] != "~/from-init" {
			t.Errorf("%s: readLibraryPaths = %v, want [~/from-init]", name, got)
		}
	}
}

func TestReadLibraryPaths_FallsBackToInitWhenArtifactSchemaInvalid(t *testing.T) {
	agentDir := t.TempDir()
	writeAgentFile(t, agentDir, "init.json",
		`{"manifest": {"capabilities": {"skills": {"paths": ["~/from-init"]}}}}`)
	cases := map[string]string{
		"wrong schema":  `{"schema": "other/v1", "schema_version": 1, "source": "kernel", "manifest": {"capabilities": {"skills": {"paths": ["~/bad"]}}}}`,
		"wrong version": `{"schema": "lingtai.manifest.resolved/v1", "schema_version": 2, "source": "kernel", "manifest": {"capabilities": {"skills": {"paths": ["~/bad"]}}}}`,
		"wrong source":  `{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1, "source": "user", "manifest": {"capabilities": {"skills": {"paths": ["~/bad"]}}}}`,
	}
	for name, artifact := range cases {
		writeAgentFile(t, agentDir, "system/manifest.resolved.json", artifact)
		got := readLibraryPaths(agentDir)
		if len(got) != 1 || got[0] != "~/from-init" {
			t.Errorf("%s: readLibraryPaths = %v, want [~/from-init]", name, got)
		}
	}
}

func TestReadLibraryPaths_FallsBackToInitWhenArtifactStale(t *testing.T) {
	agentDir := t.TempDir()
	writeAgentFile(t, agentDir, "system/manifest.resolved.json",
		`{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1, "source": "kernel", "manifest": {"capabilities": {"skills": {"paths": ["~/stale-artifact"]}}}}`)
	writeAgentFile(t, agentDir, "init.json",
		`{"manifest": {"capabilities": {"skills": {"paths": ["~/fresh-init"]}}}}`)
	base := time.Now().Add(-time.Hour)
	touchLibraryAgentFile(t, agentDir, "system/manifest.resolved.json", base)
	touchLibraryAgentFile(t, agentDir, "init.json", base.Add(time.Minute))
	got := readLibraryPaths(agentDir)
	if len(got) != 1 || got[0] != "~/fresh-init" {
		t.Errorf("readLibraryPaths = %v, want [~/fresh-init]", got)
	}
}

func TestReadLibraryPaths_ArtifactWinsEvenWhenInitLacksSkills(t *testing.T) {
	agentDir := t.TempDir()
	// init.json never declared skills (e.g. paths live only in the preset).
	writeAgentFile(t, agentDir, "init.json",
		`{"manifest": {"capabilities": {"bash": {}}}}`)
	writeAgentFile(t, agentDir, "system/manifest.resolved.json",
		`{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1, "source": "kernel",
		  "manifest": {"capabilities": {"skills": {"paths": ["~/preset-only"]}}}}`)

	got := readLibraryPaths(agentDir)
	if len(got) != 1 || got[0] != "~/preset-only" {
		t.Errorf("readLibraryPaths = %v, want [~/preset-only]", got)
	}
}

func TestReadLibraryPaths_ValidArtifactWithoutSkillsIsAuthoritative(t *testing.T) {
	agentDir := t.TempDir()
	// Stale init declares skills, but the resolved truth dropped the capability
	// (e.g. swapped to a preset without skills). The artifact wins — no fallback.
	writeAgentFile(t, agentDir, "init.json",
		`{"manifest": {"capabilities": {"skills": {"paths": ["~/stale-from-init"]}}}}`)
	writeAgentFile(t, agentDir, "system/manifest.resolved.json",
		`{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1, "source": "kernel",
		  "manifest": {"capabilities": {"bash": {}}}}`)

	if got := readLibraryPaths(agentDir); len(got) != 0 {
		t.Errorf("readLibraryPaths = %v, want empty", got)
	}
}

func TestParseFrontmatter_FoldedDescription(t *testing.T) {
	fm := parseFrontmatter("---\nname: knowledge-manual\ndescription: >\n  Concise guide to the knowledge capability\n  and nested folders.\nversion: 1.0.0\n---\n# Body\n")
	if fm == nil {
		t.Fatal("parseFrontmatter returned nil")
	}
	if got := fm["description"]; got != "Concise guide to the knowledge capability and nested folders." {
		t.Errorf("description = %q", got)
	}
}
