package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildKnowledgeFolderEntries verifies drill-in catalog for a knowledge
// entry folder containing KNOWLEDGE.md, extra root files, and a subdirectory.
func TestBuildKnowledgeFolderEntries(t *testing.T) {
	dir := t.TempDir()

	// KNOWLEDGE.md with valid frontmatter
	knowledgeBody := "---\nname: test\n\ndescription: desc\n---\n# Title\nBody text\n"
	if err := os.WriteFile(filepath.Join(dir, "KNOWLEDGE.md"), []byte(knowledgeBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// Extra root markdown file
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes\nContent\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Subdirectory with a file
	refsDir := filepath.Join(dir, "references")
	if err := os.MkdirAll(refsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refsDir, "source.md"), []byte("# Source\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildKnowledgeFolderEntries(dir)
	if len(entries) == 0 {
		t.Fatal("no entries returned")
	}

	// KNOWLEDGE.md must be first.
	if entries[0].Label != "KNOWLEDGE.md" {
		t.Errorf("first entry = %q, want KNOWLEDGE.md", entries[0].Label)
	}

	// Root files should come before subdirectory entries.
	var rootLabels, subLabels []string
	for _, e := range entries {
		if e.Group == "" {
			rootLabels = append(rootLabels, e.Label)
		} else {
			subLabels = append(subLabels, e.Label)
		}
	}
	if len(rootLabels) != 2 {
		t.Errorf("expected 2 root entries (KNOWLEDGE.md, notes.md), got %d: %v", len(rootLabels), rootLabels)
	}
	if len(subLabels) != 1 {
		t.Errorf("expected 1 subdirectory entry (source.md), got %d: %v", len(subLabels), subLabels)
	}

	// Subdirectory group name should be "references".
	if len(subLabels) > 0 {
		found := false
		for _, e := range entries {
			if e.Group == "references" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected group \"references\" in entries")
		}
	}

	// Markdown files should use Path (lazy-loaded), not Content.
	for _, e := range entries {
		if strings.HasSuffix(e.Label, ".md") && e.Path == "" && e.Content == "" {
			t.Errorf("entry %q has neither Path nor Content", e.Label)
		}
	}
}

// TestBuildKnowledgeFolderEntries_EmptyDir verifies an empty dir returns nil.
func TestBuildKnowledgeFolderEntries_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	entries := buildKnowledgeFolderEntries(dir)
	if entries != nil {
		t.Errorf("expected nil for empty dir, got %d entries", len(entries))
	}
}

// TestBuildKnowledgeFolderEntries_HiddenSkipped verifies dot-files are excluded.
func TestBuildKnowledgeFolderEntries_HiddenSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "KNOWLEDGE.md"), []byte("---\nname: test\n\ndescription: d\n---\n# T\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := buildKnowledgeFolderEntries(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Label, ".") {
			t.Errorf("hidden entry leaked: %s", e.Label)
		}
	}
}

// TestBuildKnowledgeFolderEntries_NilForMissing verifies missing dir returns nil.
func TestBuildKnowledgeFolderEntries_NilForMissing(t *testing.T) {
	entries := buildKnowledgeFolderEntries("/nonexistent/path")
	if entries != nil {
		t.Errorf("expected nil for missing dir, got %d entries", len(entries))
	}
}
