package preset

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptSectionDefinitionsAreBundled(t *testing.T) {
	tests := []struct {
		name string
		fsys embed.FS
		path string
		want []string
	}{
		{
			name: "covenant",
			fsys: covenantFS,
			path: "covenant/section.yaml",
			want: []string{
				"section: covenant",
				"metadata_only: true",
				"en/covenant.md",
				"zh/covenant.md",
				"wen/covenant.md",
				"tui/internal/preset/ANATOMY.md",
				"tui/internal/preset/principle/section.yaml",
			},
		},
		{
			name: "principle",
			fsys: principleFS,
			path: "principle/section.yaml",
			want: []string{
				"section: principle",
				"metadata_only: true",
				"en/principle.md",
				"zh/principle.md",
				"wen/principle.md",
				"tui/internal/preset/ANATOMY.md",
				"tui/internal/preset/covenant/section.yaml",
			},
		},
		{
			name: "procedures",
			fsys: proceduresFS,
			path: "procedures/section.yaml",
			want: []string{
				"section: procedures",
				"metadata_only: true",
				"procedures.md",
				"detailed GitHub issue filing or consent policy",
				"tui/internal/preset/ANATOMY.md",
				"tui/internal/preset/principle/section.yaml",
			},
		},
		{
			name: "soul",
			fsys: soulFS,
			path: "soul/section.yaml",
			want: []string{
				"section: soul",
				"metadata_only: true",
				"en/soul-flow.md",
				"zh/soul-flow.md",
				"wen/soul-flow.md",
				"tui/internal/preset/ANATOMY.md",
				"tui/internal/preset/procedures/section.yaml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.fsys.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read bundled %s: %v", tt.path, err)
			}
			body := string(data)
			if strings.Contains(body, "\nlinks:") || strings.HasPrefix(body, "links:") {
				t.Fatalf("%s should use anatomy-style related_files, not a links field", tt.path)
			}
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing %q\n%s", tt.path, want, body)
				}
			}
			for _, want := range []string{
				"Keep related_files as repo-relative paths to real files",
				"must not be injected into the agent system prompt",
				"at least one peer section.yaml",
				"copy this maintenance field",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing shared maintenance rule %q\n%s", tt.path, want, body)
				}
			}
		})
	}
}

func TestPresetAnatomyLinksPromptSectionDefinitions(t *testing.T) {
	data, err := os.ReadFile("ANATOMY.md")
	if err != nil {
		t.Fatalf("read ANATOMY.md: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		"Prompt section definitions",
		"tui/internal/preset/covenant/section.yaml",
		"tui/internal/preset/principle/section.yaml",
		"tui/internal/preset/procedures/section.yaml",
		"tui/internal/preset/soul/section.yaml",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("ANATOMY.md missing %q", want)
		}
	}
}

func TestRelatedManualsLinkBackToPromptSectionDefinitions(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{
			path: "skills/lingtai-tui-anatomy/SKILL.md",
			want: []string{
				"related_files:",
				"tui/internal/preset/ANATOMY.md",
				"tui/internal/preset/covenant/section.yaml",
				"tui/internal/preset/principle/section.yaml",
				"tui/internal/preset/procedures/section.yaml",
				"tui/internal/preset/soul/section.yaml",
			},
		},
		{
			path: "skills/lingtai-issue-report/SKILL.md",
			want: []string{"related_files:", "tui/internal/preset/procedures/section.yaml"},
		},
		{
			path: "skills/lingtai-dev-guide/SKILL.md",
			want: []string{"related_files:", "tui/internal/preset/procedures/section.yaml"},
		},
		{
			path: "skills/lingtai-tui-help/SKILL.md",
			want: []string{"related_files:", "tui/internal/preset/procedures/section.yaml"},
		},
		{
			path: "skills/lingtai-tutorial-guide/SKILL.md",
			want: []string{
				"related_files:",
				"tui/internal/preset/covenant/section.yaml",
				"tui/internal/preset/principle/section.yaml",
			},
		},
		{
			path: "skills/lingtai-recipe/SKILL.md",
			want: []string{
				"related_files:",
				"tui/internal/preset/covenant/section.yaml",
				"tui/internal/preset/principle/section.yaml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			data, err := os.ReadFile(filepath.FromSlash(tt.path))
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			body := string(data)
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing bidirectional related_files entry %q", tt.path, want)
				}
			}
		})
	}
}

func TestRelatedFileEntriesResolve(t *testing.T) {
	repoRoot := filepath.Clean("../../..")
	for _, rel := range []string{
		"covenant/section.yaml",
		"principle/section.yaml",
		"procedures/section.yaml",
		"soul/section.yaml",
		"skills/lingtai-tui-anatomy/SKILL.md",
		"skills/lingtai-issue-report/SKILL.md",
		"skills/lingtai-dev-guide/SKILL.md",
		"skills/lingtai-tui-help/SKILL.md",
		"skills/lingtai-tutorial-guide/SKILL.md",
		"skills/lingtai-recipe/SKILL.md",
	} {
		t.Run(rel, func(t *testing.T) {
			data, err := os.ReadFile(filepath.FromSlash(rel))
			if err != nil {
				t.Fatalf("read %s: %v", rel, err)
			}
			text := string(data)
			if strings.HasSuffix(rel, ".md") {
				text = yamlFrontmatter(t, rel, text)
			}
			for _, related := range relatedFiles(text) {
				if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(related))); err != nil {
					t.Fatalf("%s related_files entry %q does not resolve from repo root: %v", rel, related, err)
				}
			}
		})
	}
}

func yamlFrontmatter(t *testing.T, rel, text string) string {
	t.Helper()
	if !strings.HasPrefix(text, "---\n") {
		t.Fatalf("%s has no YAML frontmatter", rel)
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		t.Fatalf("%s has unclosed YAML frontmatter", rel)
	}
	return text[4 : 4+end]
}

func relatedFiles(yamlText string) []string {
	var out []string
	inRelatedFiles := false
	for _, line := range strings.Split(yamlText, "\n") {
		if line == "related_files:" {
			inRelatedFiles = true
			continue
		}
		if inRelatedFiles {
			if strings.HasPrefix(line, "  - ") {
				out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "  - ")))
				continue
			}
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ") {
				inRelatedFiles = false
			}
		}
	}
	return out
}

func TestBootstrapPopulatesPromptSectionDefinitions(t *testing.T) {
	dir := t.TempDir()
	if err := Bootstrap(dir); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	for _, rel := range []string{
		"covenant/section.yaml",
		"principle/section.yaml",
		"procedures/section.yaml",
		"soul/section.yaml",
	} {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatalf("expected Bootstrap to populate %s: %v", rel, err)
		}
		if !strings.Contains(string(data), "metadata_only: true") {
			t.Fatalf("%s should declare metadata_only: true", rel)
		}
	}

	if got := ProceduresPath(dir, "en"); strings.HasSuffix(got, "section.yaml") {
		t.Fatalf("ProceduresPath should resolve rendered markdown, got metadata sidecar %q", got)
	}
}
