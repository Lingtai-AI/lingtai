package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBuildSkillFolderEntries_PortalGuideNestedReferences verifies that the
// shipped portal guide is a router with nested reference SKILL.md files and
// that the TUI drill-in view exposes those files under the reference group.
func TestBuildSkillFolderEntries_PortalGuideNestedReferences(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "lingtai-portal-guide")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is lingtai-portal-guide missing?")
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
		"name: portal-guide-overview",
		"reference/overview/SKILL.md",
		"name: portal-guide-topology-and-api",
		"reference/topology-and-api/SKILL.md",
		"name: portal-guide-lifecycle-and-recording",
		"reference/lifecycle-and-recording/SKILL.md",
		".lingtai/.portal/port",
		"Routing table",
	} {
		if !strings.Contains(rootBody, want) {
			t.Errorf("portal-guide root missing %q", want)
		}
	}

	labels := make(map[string]MarkdownEntry)
	for _, e := range entries {
		labels[e.Label] = e
	}
	for _, want := range []string{
		"overview/SKILL.md",
		"topology-and-api/SKILL.md",
		"lifecycle-and-recording/SKILL.md",
	} {
		e, ok := labels[want]
		if !ok {
			t.Fatalf("missing nested portal-guide entry %q", want)
		}
		if e.Group != "reference" {
			t.Errorf("entry %q group = %q, want reference", want, e.Group)
		}
	}

	for _, rel := range []string{
		filepath.Join("reference", "overview", "SKILL.md"),
		filepath.Join("reference", "topology-and-api", "SKILL.md"),
		filepath.Join("reference", "lifecycle-and-recording", "SKILL.md"),
	} {
		childBodyBytes, err := os.ReadFile(filepath.Join(skillDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(childBodyBytes), "nested `lingtai-portal-guide` reference") {
			t.Errorf("%s should identify itself as a nested lingtai-portal-guide reference", rel)
		}
	}
}
