package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBuildSkillFolderEntries_IssueReportNestedReferences verifies that the
// shipped issue-report skill is a router with nested reference SKILL.md files
// and that the TUI drill-in view exposes those files under the reference group.
func TestBuildSkillFolderEntries_IssueReportNestedReferences(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "lingtai-issue-report")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is lingtai-issue-report missing?")
	}
	if entries[0].Label != "SKILL.md" {
		t.Errorf("first entry = %q, want SKILL.md", entries[0].Label)
	}

	rootBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootBody := string(rootBodyBytes)
	for _, want := range []string{
		"Nested reference catalog",
		"name: issue-report-evidence-checklist",
		"reference/evidence-checklist/SKILL.md",
		"name: issue-report-report-template",
		"reference/report-template/SKILL.md",
		"name: issue-report-filing-flow",
		"reference/filing-flow/SKILL.md",
		"Human consent is required, always",
		"Secrets never enter a report",
		"Routing table",
	} {
		if !strings.Contains(rootBody, want) {
			t.Errorf("issue-report root missing %q", want)
		}
	}

	labels := make(map[string]MarkdownEntry)
	for _, e := range entries {
		labels[e.Label] = e
	}
	for _, want := range []string{
		"evidence-checklist/SKILL.md",
		"report-template/SKILL.md",
		"filing-flow/SKILL.md",
	} {
		e, ok := labels[want]
		if !ok {
			t.Fatalf("missing nested issue-report entry %q", want)
		}
		if e.Group != "reference" {
			t.Errorf("entry %q group = %q, want reference", want, e.Group)
		}
	}

	for _, rel := range []string{
		filepath.Join("reference", "evidence-checklist", "SKILL.md"),
		filepath.Join("reference", "report-template", "SKILL.md"),
		filepath.Join("reference", "filing-flow", "SKILL.md"),
	} {
		childBodyBytes, err := os.ReadFile(filepath.Join(skillDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(childBodyBytes), "nested `lingtai-issue-report` reference") {
			t.Errorf("%s should identify itself as a nested lingtai-issue-report reference", rel)
		}
	}
}
