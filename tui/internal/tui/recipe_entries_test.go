package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildRecipeEntriesUsesCanonicalRecipeResolvers(t *testing.T) {
	dir := t.TempDir()
	writeRecipeEntryFile(t, filepath.Join(dir, ".recipe", "recipe.json"), `{
		"id": "preview",
		"name": "Preview",
		"description": "Preview recipe",
		"library_name": "recipe-lib"
	}`)
	writeRecipeEntryFile(t, filepath.Join(dir, ".recipe", "greet", "greet.md"), "root greet")
	writeRecipeEntryFile(t, filepath.Join(dir, ".recipe", "greet", "zh", "greet.md"), "zh greet")
	writeRecipeEntryFile(t, filepath.Join(dir, ".recipe", "comment", "comment.md"), "comment")
	writeRecipeEntryFile(t, filepath.Join(dir, ".recipe", "covenant", "covenant.md"), "covenant")
	writeRecipeEntryFile(t, filepath.Join(dir, ".recipe", "procedures", "zh", "procedures.md"), "zh procedures")
	writeRecipeEntryFile(t, filepath.Join(dir, "recipe-lib", "planner", "SKILL.md"), "# Planner")
	writeRecipeEntryFile(t, filepath.Join(dir, "recipe-lib", "localized", "zh", "SKILL.md"), "# Localized")

	// These are from the old preview layout and should not be surfaced once
	// the bundle has a canonical .recipe/ manifest.
	writeRecipeEntryFile(t, filepath.Join(dir, "greet.md"), "legacy root greet")
	writeRecipeEntryFile(t, filepath.Join(dir, "skills", "legacy", "SKILL.md"), "# Legacy")

	entries := buildRecipeEntries(dir, "zh")
	byLabel := make(map[string]MarkdownEntry)
	for _, entry := range entries {
		byLabel[entry.Label] = entry
	}

	wantPaths := map[string]string{
		"greet.md (zh)":           filepath.Join(dir, ".recipe", "greet", "zh", "greet.md"),
		"comment.md":              filepath.Join(dir, ".recipe", "comment", "comment.md"),
		"recipe.json":             filepath.Join(dir, ".recipe", "recipe.json"),
		"planner/SKILL.md":        filepath.Join(dir, "recipe-lib", "planner", "SKILL.md"),
		"localized/SKILL.md (zh)": filepath.Join(dir, "recipe-lib", "localized", "zh", "SKILL.md"),
		"covenant.md":             filepath.Join(dir, ".recipe", "covenant", "covenant.md"),
		"procedures.md (zh)":      filepath.Join(dir, ".recipe", "procedures", "zh", "procedures.md"),
	}
	for label, wantPath := range wantPaths {
		entry, ok := byLabel[label]
		if !ok {
			t.Fatalf("missing entry %q in %#v", label, entries)
		}
		if entry.Path != wantPath {
			t.Errorf("%s path = %q, want %q", label, entry.Path, wantPath)
		}
	}
	if _, ok := byLabel["legacy/SKILL.md"]; ok {
		t.Fatalf("legacy top-level skills/ entry should not be included")
	}
	for _, entry := range entries {
		if entry.Path == filepath.Join(dir, "greet.md") {
			t.Fatalf("legacy top-level greet.md should not be included")
		}
	}
}

func TestBuildRecipeEntriesRejectsLegacyOnlyDirectory(t *testing.T) {
	dir := t.TempDir()
	writeRecipeEntryFile(t, filepath.Join(dir, "greet.md"), "legacy greet")
	writeRecipeEntryFile(t, filepath.Join(dir, "skills", "legacy", "SKILL.md"), "# Legacy")

	entries := buildRecipeEntries(dir, "en")
	if len(entries) != 0 {
		t.Fatalf("buildRecipeEntries legacy-only entries = %#v, want none", entries)
	}
}

func writeRecipeEntryFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
