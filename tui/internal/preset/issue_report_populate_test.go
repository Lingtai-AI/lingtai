package preset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPopulateBundledLibrary_IssueReportNestedReferences verifies that the
// embedded utility-library copier preserves the issue-report router and its
// nested reference files on disk, and that the old monolithic content has been
// split out into the nested leaves.
func TestPopulateBundledLibrary_IssueReportNestedReferences(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary("", globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "lingtai-issue-report")
	for _, rel := range []string{
		"SKILL.md",
		"reference/evidence-checklist/SKILL.md",
		"reference/report-template/SKILL.md",
		"reference/filing-flow/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled issue-report file %s to be extracted: %v", rel, err)
		}
	}

	rootBody, err := os.ReadFile(filepath.Join(utilitiesDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Nested reference catalog",
		"location: reference/evidence-checklist/SKILL.md",
		"location: reference/report-template/SKILL.md",
		"location: reference/filing-flow/SKILL.md",
		"Human consent is required, always",
	} {
		if !strings.Contains(string(rootBody), want) {
			t.Errorf("extracted issue-report root missing %q", want)
		}
	}

	// The heavy procedure should live in the leaves, not the router.
	filing, err := os.ReadFile(filepath.Join(utilitiesDir, "reference", "filing-flow", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"gh issue create",
		"--repo Lingtai-AI/lingtai",
		"never include it in the issue body",
	} {
		if !strings.Contains(string(filing), want) {
			t.Errorf("extracted issue-report filing-flow leaf missing %q", want)
		}
	}
}
