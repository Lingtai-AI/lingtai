package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBuildSkillFolderEntries_RecipeSkill verifies the drill-in catalog
// against the real lingtai-recipe skill folder, which has SKILL.md at root
// plus reference/, scripts/, and assets/ subdirectories.
func TestBuildSkillFolderEntries_RecipeSkill(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "lingtai-recipe")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is lingtai-recipe missing?")
	}

	// SKILL.md must be first.
	if entries[0].Label != "SKILL.md" {
		t.Errorf("first entry = %q, want SKILL.md", entries[0].Label)
	}

	// Group labels should include references, scripts, assets.
	groups := make(map[string]int)
	for _, e := range entries {
		if e.Group != "" {
			groups[e.Group]++
		}
	}
	for _, want := range []string{"reference", "scripts", "assets"} {
		if groups[want] == 0 {
			t.Errorf("expected non-empty group %q, groups=%v", want, groups)
		}
	}

	// No hidden entries (.pytest_cache) should leak in.
	for _, e := range entries {
		if strings.Contains(e.Label, ".pytest_cache") {
			t.Errorf("hidden entry leaked: %s", e.Label)
		}
	}

	// Python scripts should be pre-rendered as fenced python blocks.
	foundPy := false
	for _, e := range entries {
		if strings.HasSuffix(e.Label, ".py") {
			foundPy = true
			if !strings.Contains(e.Content, "```python") {
				t.Errorf("python entry %q not fenced as python: %q", e.Label, firstLineOf(e.Content))
			}
		}
	}
	if !foundPy {
		t.Error("no python scripts found in lingtai-recipe — did the fixture change?")
	}
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestBuildSkillFolderEntries_SwissKnifeNestedReferences verifies that the
// shipped swiss-knife utility skill is a router with nested reference SKILL.md
// files, and that the TUI drill-in view exposes those nested files under the
// reference group without promoting them to sibling top-level utility folders.
func TestBuildSkillFolderEntries_SwissKnifeNestedReferences(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "swiss-knife")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is swiss-knife missing?")
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
		"reference/claude-code/SKILL.md",
		"reference/openai-codex/SKILL.md",
		"reference/opencode/SKILL.md",
		"reference/minimax-cli/SKILL.md",
		"reference/token-usage/SKILL.md",
		"reference/html-report/SKILL.md",
		"reference/xiaomi-mimo/SKILL.md",
		"reference/zhipu-coding-plan/SKILL.md",
	} {
		if !strings.Contains(rootBody, want) {
			t.Errorf("swiss-knife root missing %q", want)
		}
	}

	labels := make(map[string]MarkdownEntry)
	for _, e := range entries {
		labels[e.Label] = e
	}
	for _, want := range []string{
		"claude-code/SKILL.md",
		"openai-codex/SKILL.md",
		"opencode/SKILL.md",
		"html-report/SKILL.md",
		"html-report/assets/template.html",
		"token-usage/SKILL.md",
		"token-usage/scripts/cost_report.py",
	} {
		e, ok := labels[want]
		if !ok {
			t.Fatalf("missing nested swiss-knife entry %q", want)
		}
		if e.Group != "reference" {
			t.Errorf("entry %q group = %q, want reference", want, e.Group)
		}
	}

	childBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "claude-code", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(childBodyBytes), "Nested swiss-knife reference for Claude Code CLI") {
		t.Error("nested claude-code child should identify itself as a nested swiss-knife reference for Claude Code CLI")
	}
}

func TestBuildSkillFolderEntries_WebBrowsingNestedReferences(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "web-browsing")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is web-browsing missing?")
	}

	rootBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootBody := string(rootBodyBytes)
	for _, want := range []string{
		"Nested reference catalog",
		"reference/tier-quick-refs/SKILL.md",
		"reference/routing-and-sites/SKILL.md",
		"reference/maintenance-bundles/SKILL.md",
	} {
		if !strings.Contains(rootBody, want) {
			t.Errorf("web-browsing root missing %q", want)
		}
	}

	labels := make(map[string]MarkdownEntry)
	for _, e := range entries {
		labels[e.Label] = e
	}
	for _, want := range []string{
		"tier-quick-refs/SKILL.md",
		"routing-and-sites/SKILL.md",
		"maintenance-bundles/SKILL.md",
		"tier-0-pdf.md",
	} {
		e, ok := labels[want]
		if !ok {
			t.Fatalf("missing nested web-browsing entry %q", want)
		}
		if e.Group != "reference" {
			t.Errorf("entry %q group = %q, want reference", want, e.Group)
		}
	}

	childBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "tier-quick-refs", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(childBodyBytes), "Nested web-browsing reference") {
		t.Error("nested tier quick refs child should identify itself as a nested web-browsing reference")
	}
}

func TestBuildSkillFolderEntries_DailyReflectionNestedReferences(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "daily-reflection")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is daily-reflection missing?")
	}

	rootBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootBody := string(rootBodyBytes)
	for _, want := range []string{
		"Nested reference catalog",
		"reference/data-collection/SKILL.md",
		"reference/analysis-reporting/SKILL.md",
		"reference/operations/SKILL.md",
		"system-manual/reference/sqlite-log-query/SKILL.md",
	} {
		if !strings.Contains(rootBody, want) {
			t.Errorf("daily-reflection root missing %q", want)
		}
	}

	labels := make(map[string]MarkdownEntry)
	for _, e := range entries {
		labels[e.Label] = e
	}
	for _, want := range []string{
		"data-collection/SKILL.md",
		"analysis-reporting/SKILL.md",
		"operations/SKILL.md",
	} {
		e, ok := labels[want]
		if !ok {
			t.Fatalf("missing nested daily-reflection entry %q", want)
		}
		if e.Group != "reference" {
			t.Errorf("entry %q group = %q, want reference", want, e.Group)
		}
	}

	childBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "data-collection", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(childBodyBytes), "Nested daily-reflection reference") {
		t.Error("nested data collection child should identify itself as a nested daily-reflection reference")
	}
}
