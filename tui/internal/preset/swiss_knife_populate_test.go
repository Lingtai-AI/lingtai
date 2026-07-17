package preset

import (
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
		"reference/vision/SKILL.md",
		"reference/vision/scripts/describe.py",
		"reference/vision/reference/local-models.md",
		"reference/listen/SKILL.md",
		"reference/listen/scripts/transcribe.py",
		"reference/listen/scripts/appreciate.py",
		"reference/academic-research/SKILL.md",
		"reference/academic-research/scripts/fetch_paper.py",
		"reference/academic-research/reference/api-arxiv.md",
		"reference/academic-research/reference/pipeline-latex-writing.md",
		"reference/academic-research/reference/anti-pattern-text-consistency-vs-data-correspondence.md",
		"reference/academic-research/reference/evidence-verification-gate.md",
		"reference/dj/SKILL.md",
		"reference/token-usage/SKILL.md",
		"reference/token-usage/scripts/cost_report.py",
		"reference/token-usage/scripts/tool_calls_per_api_call_trend.py",
		"reference/token-usage/scripts/custom_pricing.json",
		"reference/html-report/SKILL.md",
		"reference/html-report/assets/template.html",
		"reference/headless-bot/SKILL.md",
		"reference/headless-bot/scripts/create_telegram_bot_project.py",
		"reference/xiaomi-mimo/SKILL.md",
		"reference/zhipu-coding-plan/SKILL.md",
		"reference/find-something-to-do/SKILL.md",
		"reference/preset-health/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled swiss-knife file %s to be extracted: %v", rel, err)
		}
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

func TestPopulateBundledLibrary_HeadlessBotTelegramModule(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "swiss-knife", "reference", "headless-bot")
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
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled lingtai-dev-guide file %s to be extracted: %v", rel, err)
		}
	}

	rootBody, err := os.ReadFile(filepath.Join(utilitiesDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"```yaml",
		"- name: dev-guide-architecture",
		"location: reference/architecture/SKILL.md",
		"Routing table",
	} {
		if !strings.Contains(string(rootBody), want) {
			t.Errorf("lingtai-dev-guide root missing nested metadata %q", want)
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
