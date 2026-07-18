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

func TestGovernedProcessContractGraph(t *testing.T) {
	root := filepath.Clean("..")
	rootContract := readArchitectureFile(t, root, "CONTRACT.md")
	tuiAnatomy := readArchitectureFile(t, root, "tui/ANATOMY.md")

	children := []struct {
		name         string
		contractPath string
		anatomyPath  string
		relatedFiles []string
		anatomyLinks []string
	}{
		{
			name:         "agent-process-lifecycle",
			contractPath: "tui/internal/process/CONTRACT.md",
			anatomyPath:  "tui/internal/process/ANATOMY.md",
			relatedFiles: []string{
				"tui/internal/process/ANATOMY.md",
				"tui/internal/process/launcher.go",
				"tui/internal/process/launcher_test.go",
				"tui/internal/process/check.go",
				"tui/internal/process/check_test.go",
				"tui/internal/process/kill_unix.go",
				"tui/internal/process/kill_windows.go",
				"tui/internal/processscan/CONTRACT.md",
			},
			anatomyLinks: []string{"tui/ANATOMY.md", "tui/internal/processscan/ANATOMY.md"},
		},
		{
			name:         "agent-process-observation",
			contractPath: "tui/internal/processscan/CONTRACT.md",
			anatomyPath:  "tui/internal/processscan/ANATOMY.md",
			relatedFiles: []string{
				"tui/internal/processscan/ANATOMY.md",
				"tui/internal/processscan/check.go",
				"tui/internal/processscan/check_test.go",
				"tui/internal/process/CONTRACT.md",
			},
			anatomyLinks: []string{"tui/ANATOMY.md", "tui/internal/process/ANATOMY.md"},
		},
	}

	headings := []string{"Purpose", "Behavior", "Port", "Adapters", "Contract rules", "Contract tests", "Maintenance"}
	for _, child := range children {
		t.Run(child.name, func(t *testing.T) {
			rootEntry := "\n  - " + child.contractPath + "\n"
			if got := strings.Count(rootContract, rootEntry); got != 1 {
				t.Fatalf("root CONTRACT.md must list %s exactly once; got %d", child.contractPath, got)
			}

			contract := readArchitectureFile(t, root, child.contractPath)
			prefix := "---\nname: " + child.name + "\ncontract_version: 1\nroot_contract: CONTRACT.md\nrelated_files:\n"
			if !strings.HasPrefix(contract, prefix) {
				t.Errorf("%s must use the governed-child frontmatter prefix", child.contractPath)
			}
			if !strings.Contains(contract, "maintenance: |\n  This component contract is governed by the root CONTRACT.md. Keep\n") {
				t.Errorf("%s must use the canonical child maintenance contract", child.contractPath)
			}

			for _, target := range child.relatedFiles {
				requireRelatedFile(t, root, contract, child.contractPath, target)
			}

			anatomy := readArchitectureFile(t, root, child.anatomyPath)
			requireRelatedFile(t, root, anatomy, child.anatomyPath, child.contractPath)
			for _, target := range child.anatomyLinks {
				requireRelatedFile(t, root, anatomy, child.anatomyPath, target)
			}
			requireRelatedFile(t, root, tuiAnatomy, "tui/ANATOMY.md", child.anatomyPath)

			cursor := 0
			for _, heading := range headings {
				marker := "\n## " + heading + "\n"
				if got := strings.Count(contract, marker); got != 1 {
					t.Fatalf("%s must contain heading %q exactly once; got %d", child.contractPath, heading, got)
				}
				next := strings.Index(contract[cursor:], marker)
				if next < 0 {
					t.Fatalf("%s heading %q is out of order", child.contractPath, heading)
				}
				cursor += next + len(marker)
			}
		})
	}
}

func requireRelatedFile(t *testing.T, root, text, owner, target string) {
	t.Helper()
	if !strings.Contains(text, "\n  - "+target+"\n") {
		t.Errorf("%s related_files must include %s", owner, target)
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(target)))
	if err != nil {
		t.Errorf("%s related target %s: %v", owner, target, err)
		return
	}
	if !info.Mode().IsRegular() {
		t.Errorf("%s related target %s must be a regular file", owner, target)
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
