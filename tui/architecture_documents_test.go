package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchitectureEntryLinks(t *testing.T) {
	root := filepath.Clean("..")

	for _, pair := range []struct {
		path  string
		links []string
	}{
		{"ANATOMY.md", []string{"CONTRACT.md", "dev-guide-skill/SKILL.md"}},
		{"CONTRACT.md", []string{"ANATOMY.md", "dev-guide-skill/SKILL.md"}},
	} {
		text := readArchitectureFile(t, root, pair.path)
		for _, target := range pair.links {
			if !strings.Contains(text, "\n  - "+target+"\n") {
				t.Errorf("%s related_files must include %s", pair.path, target)
			}
		}
	}

	for _, path := range []string{"README.md", "README.zh.md", "README.wen.md", "CLAUDE.md"} {
		text := readArchitectureFile(t, root, path)
		for _, target := range []string{"ANATOMY.md", "CONTRACT.md", "dev-guide-skill/SKILL.md"} {
			if !hasMarkdownLink(text, target) {
				t.Errorf("%s must link to %s", path, target)
			}
		}
	}
}

func TestRuntimeControlSurfaceAnatomyRoute(t *testing.T) {
	root := filepath.Clean("..")
	for _, tc := range []struct {
		path  string
		wants []string
	}{
		{"ANATOMY.md", []string{
			"Runtime/control-surface boundary",
			"tui/ANATOMY.md",
			"portal/ANATOMY.md",
			"lingtai-kernel-anatomy",
		}},
		{"tui/ANATOMY.md", []string{
			"Runtime/control-surface boundary",
			"tui/internal/tui/app.go:640-670",
			"tui/internal/process/launcher.go:87-135",
			"tui/main.go:1640-1650",
			"tui/internal/tui/app.go:763-906",
			"tui/internal/fs/signal.go:9-55",
			"tui/main.go:737-767",
			"tui/internal/tui/app.go:1834-1883",
		}},
		{"portal/ANATOMY.md", []string{
			"Runtime/control-surface boundary",
			"portal/main.go:91-97",
			"tui/ANATOMY.md",
			"lingtai-kernel-anatomy",
		}},
	} {
		text := readArchitectureFile(t, root, tc.path)
		for _, want := range tc.wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing runtime-boundary route/anchor %q", tc.path, want)
			}
		}
	}
}

func readArchitectureFile(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func hasMarkdownLink(text, target string) bool {
	return strings.Contains(text, "]("+target+")") ||
		strings.Contains(text, "](./"+target+")")
}
