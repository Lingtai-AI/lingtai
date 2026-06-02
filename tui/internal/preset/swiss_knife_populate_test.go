package preset

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPopulateBundledLibrary_SwissKnifeNestedReferences verifies that the
// embedded utility-library copier preserves swiss-knife's nested reference tree
// on disk. This protects the runtime paths documented in swiss-knife's router
// and child references, such as
// ~/.lingtai-tui/utilities/swiss-knife/reference/<name>/SKILL.md.
func TestPopulateBundledLibrary_SwissKnifeNestedReferences(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary("", globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "swiss-knife")
	for _, rel := range []string{
		"SKILL.md",
		"reference/claude-code/SKILL.md",
		"reference/openai-codex/SKILL.md",
		"reference/opencode/SKILL.md",
		"reference/minimax-cli/SKILL.md",
		"reference/token-usage/SKILL.md",
		"reference/token-usage/scripts/cost_report.py",
		"reference/token-usage/scripts/custom_pricing.json",
		"reference/html-report/SKILL.md",
		"reference/html-report/assets/template.html",
		"reference/headless-bot/SKILL.md",
		"reference/headless-bot/scripts/create_telegram_bot_project.py",
		"reference/xiaomi-mimo/SKILL.md",
		"reference/zhipu-coding-plan/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled swiss-knife file %s to be extracted: %v", rel, err)
		}
	}

	for _, old := range []string{
		"claude-code/SKILL.md",
		"openai-codex/SKILL.md",
		"opencode/SKILL.md",
		"token-usage/SKILL.md",
		"html-report/SKILL.md",
		"headless-telegram-bot/SKILL.md",
		"reference/headless-telegram-bot/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, old)); !os.IsNotExist(err) {
			t.Fatalf("old swiss-knife child path %s should not be extracted outside reference/ (err=%v)", old, err)
		}
	}
}

// TestPopulateBundledLibrary_WebBrowsingNestedReferences verifies that the
// embedded utility-library copier preserves web-browsing's nested reference
// router files alongside the existing deep-dive references and scripts.
func TestPopulateBundledLibrary_WebBrowsingNestedReferences(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary("", globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "web-browsing")
	for _, rel := range []string{
		"SKILL.md",
		"scripts/extract_page.py",
		"scripts/cached_get.py",
		"reference/tier-quick-refs/SKILL.md",
		"reference/routing-and-sites/SKILL.md",
		"reference/maintenance-bundles/SKILL.md",
		"reference/tier-0-pdf.md",
		"assets/search-providers.json",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled web-browsing file %s to be extracted: %v", rel, err)
		}
	}
}

// TestPopulateBundledLibrary_DailyReflectionNestedReferences verifies that the
// embedded utility-library copier preserves daily-reflection's nested reference
// tree on disk.
func TestPopulateBundledLibrary_DailyReflectionNestedReferences(t *testing.T) {
	globalDir := t.TempDir()
	PopulateBundledLibrary("", globalDir)

	utilitiesDir := filepath.Join(globalDir, "utilities", "daily-reflection")
	for _, rel := range []string{
		"SKILL.md",
		"reference/data-collection/SKILL.md",
		"reference/analysis-reporting/SKILL.md",
		"reference/operations/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(utilitiesDir, rel)); err != nil {
			t.Fatalf("expected bundled daily-reflection file %s to be extracted: %v", rel, err)
		}
	}
}
