package preset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBundledLingtaiTuiHelp verifies the lingtai-tui-help skill ships with the
// binary: it is a recognized bundled skill, its SKILL.md and three localized
// slash-command assets are embedded and readable via ReadBundledSkillFile, and
// they extract to disk under utilities/.
func TestBundledLingtaiTuiHelp(t *testing.T) {
	if !BundledSkillNames()["lingtai-tui-help"] {
		t.Fatal("lingtai-tui-help is not a bundled skill")
	}

	assets := []string{
		"SKILL.md",
		"assets/slash-commands.en.md",
		"assets/slash-commands.zh.md",
		"assets/slash-commands.wen.md",
	}
	for _, rel := range assets {
		body, err := ReadBundledSkillFile("lingtai-tui-help", rel)
		if err != nil {
			t.Fatalf("ReadBundledSkillFile(lingtai-tui-help, %s): %v", rel, err)
		}
		if strings.TrimSpace(body) == "" {
			t.Errorf("bundled lingtai-tui-help/%s is empty", rel)
		}
	}

	// SKILL.md frontmatter must declare the skill name.
	skill, err := ReadBundledSkillFile("lingtai-tui-help", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(skill, "name: lingtai-tui-help") {
		t.Error("lingtai-tui-help SKILL.md missing name frontmatter")
	}

	// The assets extract to disk alongside the other utility skills.
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)
	utilitiesDir := filepath.Join(globalDir, "utilities", "lingtai-tui-help")
	for _, rel := range assets {
		if _, err := os.Stat(filepath.Join(utilitiesDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected extracted lingtai-tui-help file %s: %v", rel, err)
		}
	}
}

// TestBundledLingtaiTuiHelpRuntimeBoundaryAndRouting pins the sanitized
// regression that motivated the umbrella: UI exit is not agent lifecycle,
// ordinary persistence must not default to launchd, and feature questions route
// to their existing canonical skills instead of being duplicated here.
func TestBundledLingtaiTuiHelpRuntimeBoundaryAndRouting(t *testing.T) {
	help, err := ReadBundledSkillFile("lingtai-tui-help", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	flatHelp := strings.Join(strings.Fields(help), " ")
	for _, want := range []string{
		"Anything you need to know about LingTai TUI",
		"关闭 TUI/终端后 agent 会不会停止？",
		"does **not** stop a running agent",
		"launchd` is **not** the default remedy",
		"feature: runtime-lifecycle-source",
		"route: lingtai-tui-anatomy",
		"route: assets/slash-commands.en.md",
		"route: lingtai-preset-skill",
		"route: lingtai-portal-guide",
		"route: tutorial-guide",
		"route: lingtai-update",
		"route: lingtai-dev-guide",
		"route: lingtai-doctor",
		"route: mcp-manual",
		"## Human routing table",
	} {
		normalizedWant := strings.Join(strings.Fields(want), " ")
		if !strings.Contains(flatHelp, normalizedWant) {
			t.Errorf("lingtai-tui-help umbrella missing %q", want)
		}
	}
	for _, private := range []string{"/Users/", "LaunchAgent/", "com.apple."} {
		if strings.Contains(help, private) {
			t.Errorf("lingtai-tui-help leaked private screenshot detail %q", private)
		}
	}

	anatomy, err := ReadBundledSkillFile("lingtai-tui-anatomy", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"repository-root `ANATOMY.md` is both the normative anatomy-of-anatomy",
		"## Runtime-boundary route",
		"tui/ANATOMY.md",
		"portal/ANATOMY.md",
		"lingtai-kernel-anatomy",
	} {
		if !strings.Contains(anatomy, want) {
			t.Errorf("lingtai-tui-anatomy navigator missing %q", want)
		}
	}
}

// TestHelpDoesNotAdvertiseRetiredSoulCommands pins the retirement of the
// Soul subsystem in the shipped help: no locale may document `/btw` or
// `/insights` (both wrote the retired `.inquiry` signal) as a current
// slash command.
func TestHelpDoesNotAdvertiseRetiredSoulCommands(t *testing.T) {
	for _, loc := range []string{"en", "zh", "wen"} {
		rel := "assets/slash-commands." + loc + ".md"
		body, err := ReadBundledSkillFile("lingtai-tui-help", rel)
		if err != nil {
			t.Fatalf("ReadBundledSkillFile(lingtai-tui-help, %s): %v", rel, err)
		}
		for _, retired := range []string{"`/btw`", "`/insights`", "soul flow", "soul-manual"} {
			if strings.Contains(body, retired) {
				t.Errorf("%s: help still advertises retired surface %q", rel, retired)
			}
		}
	}
}

// TestReadBundledSkillFileMissing confirms ReadBundledSkillFile surfaces an
// error for an absent path rather than returning empty content silently.
func TestReadBundledSkillFileMissing(t *testing.T) {
	if _, err := ReadBundledSkillFile("lingtai-tui-help", "assets/nope.md"); err == nil {
		t.Error("expected error reading a missing bundled skill file")
	}
}
