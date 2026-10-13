package preset

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPopulateBundledLibrary_SwissKnifeNestedReferences verifies that the
// embedded utility-library copier preserves swiss-knife's nested reference tree
// on disk. This protects the runtime paths documented in swiss-knife's router
// and child references, such as
// ~/.lingtai-tui/utilities/swiss-knife/reference/<name>/SKILL.md.
func TestPopulateBundledLibrary_SwissKnifeNestedReferences(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "swiss-knife")
	for _, rel := range []string{
		"SKILL.md",
		"reference/minimax-cli/SKILL.md",
		"reference/academic-research/SKILL.md",
		"reference/academic-research/reference/anti-pattern-text-consistency-vs-data-correspondence.md",
		"reference/academic-research/reference/evidence-verification-gate.md",
		"reference/token-usage/SKILL.md",
		"reference/token-usage/scripts/cost_report.py",
		"reference/token-usage/scripts/tool_calls_per_api_call_trend.py",
		"reference/token-usage/scripts/custom_pricing.json",
		"reference/html-report/SKILL.md",
		"reference/html-report/assets/template.html",
		"reference/xiaomi-mimo/SKILL.md",
		"reference/zhipu-coding-plan/SKILL.md",
		"reference/preset-health/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled swiss-knife file %s to be extracted: %v", rel, err)
		}
	}

	// Retired optional toolkits must not return through embedding or extraction.
	for _, retired := range []string{
		"reference/headless-bot",
		"reference/vision",
		"reference/listen",
		"reference/dj",
		"reference/find-something-to-do",
		"reference/academic-research/scripts",
		"reference/academic-research/reference/api-arxiv.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, retired)); !os.IsNotExist(err) {
			t.Fatalf("retired bundled utility %s should not be extracted (err=%v)", retired, err)
		}
	}
	if _, err := os.Stat(filepath.Join(globalDir, "utilities", "textbook-distillation")); !os.IsNotExist(err) {
		t.Fatalf("retired textbook-distillation should not be extracted (err=%v)", err)
	}
	if BundledSkillNames()["textbook-distillation"] {
		t.Fatal("retired textbook-distillation should not be a bundled skill name")
	}

	for _, old := range []string{
		"claude-code/SKILL.md",
		"openai-codex/SKILL.md",
		"opencode/SKILL.md",
		"reference/claude-code/SKILL.md",
		"reference/openai-codex/SKILL.md",
		"reference/opencode/SKILL.md",
		"token-usage/SKILL.md",
		"html-report/SKILL.md",
		"headless-telegram-bot/SKILL.md",
		"reference/headless-telegram-bot/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, old)); !os.IsNotExist(err) {
			t.Fatalf("old swiss-knife child path %s should not be extracted outside reference/ (err=%v)", old, err)
		}
	}

	for _, oldTopLevel := range []string{
		filepath.Join(globalDir, "utilities", "dj", "SKILL.md"),
		filepath.Join(globalDir, "utilities", "find-something-to-do", "SKILL.md"),
		filepath.Join(globalDir, "utilities", "vision", "SKILL.md"),
		filepath.Join(globalDir, "utilities", "vision", "scripts", "describe.py"),
		filepath.Join(globalDir, "utilities", "listen", "SKILL.md"),
		filepath.Join(globalDir, "utilities", "listen", "scripts", "transcribe.py"),
		filepath.Join(globalDir, "utilities", "academic-research", "SKILL.md"),
		filepath.Join(globalDir, "utilities", "academic-research", "scripts", "fetch_paper.py"),
	} {
		if _, err := os.Stat(oldTopLevel); !os.IsNotExist(err) {
			t.Fatalf("old top-level utility skill %s should not be extracted after moving under swiss-knife (err=%v)", oldTopLevel, err)
		}
	}
}

// TestBundledAcademicEvidenceGates protects the retained safeguards while
// ensuring none of the retired fetching/API/pipeline toolkit is embedded.
func TestBundledAcademicEvidenceGates(t *testing.T) {
	root := "skills/swiss-knife/reference/academic-research"
	wantFiles := map[string]bool{
		root + "/SKILL.md": true,
		root + "/reference/evidence-verification-gate.md":                           true,
		root + "/reference/anti-pattern-text-consistency-vs-data-correspondence.md": true,
	}
	err := fs.WalkDir(skillsFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !wantFiles[path] {
			t.Errorf("retired academic toolkit file is still embedded: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for path := range wantFiles {
		if _, err := skillsFS.ReadFile(path); err != nil {
			t.Errorf("retained academic gate missing: %s: %v", path, err)
		}
	}
	for _, check := range []struct {
		path string
		want string
	}{
		{root + "/SKILL.md", "do not bypass"},
		{root + "/reference/evidence-verification-gate.md", "verified literature matrix"},
		{root + "/reference/anti-pattern-text-consistency-vs-data-correspondence.md", "data correspondence"},
		{"skills/swiss-knife/reference/html-report/SKILL.md", "Confirm authorized access"},
		{"skills/swiss-knife/reference/html-report/SKILL.md", "Distill, do not reproduce"},
	} {
		body, err := skillsFS.ReadFile(check.path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), check.want) {
			t.Errorf("retained safety reference %s missing %q", check.path, check.want)
		}
	}
}

func TestPopulateBundledLibrary_MinimaxCliCanonicalReference(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)

	// The top-level minimax-cli skill was removed; minimax-cli now lives only as
	// a swiss-knife nested reference. Guard that the redundant top-level copy does
	// not come back.
	if _, err := os.Stat(filepath.Join(globalDir, "utilities", "minimax-cli")); err == nil {
		t.Error("top-level minimax-cli utility should no longer be bundled; it lives under swiss-knife/reference/minimax-cli")
	}

	canonicalPath := filepath.Join(globalDir, "utilities", "swiss-knife", "reference", "minimax-cli", "SKILL.md")
	canonicalBodyBytes, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalBody := string(canonicalBodyBytes)
	for _, want := range []string{
		"Nested swiss-knife reference for the MiniMax `mmx` CLI",
		"~/.lingtai-tui/presets/saved/",
		"Do **not** hardcode an unverified host",
	} {
		if !strings.Contains(canonicalBody, want) {
			t.Errorf("canonical minimax-cli reference missing %q", want)
		}
	}
}

func TestPopulateBundledLibrary_DevGuideNestedReferences(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "lingtai-dev-guide")
	for _, rel := range []string{
		"SKILL.md",
		"reference/architecture/SKILL.md",
		"reference/setup/SKILL.md",
		"reference/contributing/SKILL.md",
		"reference/gotchas/SKILL.md",
		"reference/debug-troubleshoot/SKILL.md",
		"reference/security-audit/SKILL.md",
		"reference/runtime-self-check/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled lingtai-dev-guide file %s to be extracted: %v", rel, err)
		}
	}

	rootBody, err := os.ReadFile(filepath.Join(utilitiesDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootText := string(rootBody)
	for _, want := range []string{
		"```yaml",
		"- name: dev-guide-architecture",
		"location: reference/architecture/SKILL.md",
		"Routing table",
	} {
		if !strings.Contains(rootText, want) {
			t.Errorf("lingtai-dev-guide root missing nested metadata %q", want)
		}
	}

	runtimeBody, err := os.ReadFile(filepath.Join(utilitiesDir, "reference", "runtime-self-check", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	runtimeText := string(runtimeBody)
	for _, doc := range []struct {
		name string
		text string
	}{
		{name: "root", text: rootText},
		{name: "runtime-self-check", text: runtimeText},
	} {
		for _, owner := range []string{"system-manual", "refresh-precheck"} {
			if !strings.Contains(doc.text, owner) {
				t.Errorf("lingtai-dev-guide %s missing refresh owner %q", doc.name, owner)
			}
		}
	}
	for _, trigger := range []string{
		"Ordinary same-runtime reload does not use this deep checklist",
		"Source/venv cutover",
		"suspected mismatch",
	} {
		if !strings.Contains(runtimeText, trigger) {
			t.Errorf("runtime-self-check missing stable trigger/limit %q", trigger)
		}
	}
	for _, stale := range []string{
		"Right after a `refresh`",
		"Post-refresh diagnostics checklist",
	} {
		if strings.Contains(runtimeText, stale) {
			t.Errorf("runtime-self-check retains stale universal guidance %q", stale)
		}
	}

	for _, old := range []string{
		"reference/architecture.md",
		"reference/setup.md",
		"reference/contributing.md",
		"reference/gotchas.md",
		"reference/debug-troubleshoot.md",
		"reference/security-audit.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, old)); !os.IsNotExist(err) {
			t.Fatalf("old lingtai-dev-guide flat reference path %s should not be extracted (err=%v)", old, err)
		}
	}
}

// TestPopulateBundledLibrary_RecipeNestedReferences verifies that the embedded
// utility-library copier preserves lingtai-recipe's nested reference files and
// assets after the export procedure moved out of assets/.
func TestPopulateBundledLibrary_RecipeNestedReferences(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "lingtai-recipe")
	for _, rel := range []string{
		"SKILL.md",
		"reference/recipe-format/SKILL.md",
		"reference/export-recipe/SKILL.md",
		"assets/gitignore.template",
		"scripts/validate_recipe.py",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled lingtai-recipe file %s to be extracted: %v", rel, err)
		}
	}

	rootBody, err := os.ReadFile(filepath.Join(utilitiesDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"```yaml",
		"- name: recipe-format-reference",
		"location: reference/recipe-format/SKILL.md",
		"- name: recipe-export-flow",
		"Routing table",
	} {
		if !strings.Contains(string(rootBody), want) {
			t.Errorf("lingtai-recipe root missing nested metadata %q", want)
		}
	}

	for _, old := range []string{
		"reference/recipe-format.md",
		"assets/export-recipe.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, old)); !os.IsNotExist(err) {
			t.Fatalf("old lingtai-recipe path %s should not be extracted (err=%v)", old, err)
		}
	}
}

// TestPopulateBundledLibrary_TutorialGuideNestedReferences verifies that the
// embedded utility-library copier preserves tutorial-guide's nested lesson
// reference tree on disk.
func TestPopulateBundledLibrary_TutorialGuideNestedReferences(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "lingtai-tutorial-guide")
	for _, rel := range []string{
		"SKILL.md",
		"reference/orientation/SKILL.md",
		"reference/agent-runtime/SKILL.md",
		"reference/communication/SKILL.md",
		"reference/memory-and-molt/SKILL.md",
		"reference/capabilities/SKILL.md",
		"reference/operations-and-graduation/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled tutorial-guide file %s to be extracted: %v", rel, err)
		}
	}

	for _, old := range []string{
		"reference/communication-and-capabilities/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, old)); !os.IsNotExist(err) {
			t.Fatalf("old tutorial-guide path %s should not be extracted (err=%v)", old, err)
		}
	}
}
