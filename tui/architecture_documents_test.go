package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
			"tui/internal/tui/app.go:763-903",
			"tui/internal/fs/signal.go:9-55",
			"tui/main.go:737-767",
			"tui/internal/tui/app.go:1831-1880",
		}},
		{"portal/ANATOMY.md", []string{
			"Runtime/control-surface boundary",
			"portal/main.go:66-72",
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

// TestArchitectureDocumentsCoverEveryTrackedFile enforces the repository's
// no-orphan rule (root ANATOMY.md, "## Anatomy convention"): every tracked file
// must be reachable from the repo-root ANATOMY.md by descending related_files.
//
// The walk starts at the root anatomy and follows related_files entries; an
// entry that is itself an ANATOMY.md contributes its own list, so a package
// anatomy only counts once its parent links it. That makes three defects fail
// here rather than in review: a file listed nowhere, an anatomy nobody links,
// and a related_files entry that no longer resolves to a tracked file.
func TestArchitectureDocumentsCoverEveryTrackedFile(t *testing.T) {
	root := filepath.Clean("..")

	tracked, err := trackedFiles(root)
	if err != nil {
		t.Skipf("git ls-files unavailable (%v); coverage check needs a git checkout", err)
	}
	if len(tracked) == 0 {
		t.Skip("git ls-files returned no files; not a git checkout")
	}

	anatomies := map[string][]string{}
	for path := range tracked {
		if filepath.Base(path) != "ANATOMY.md" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		anatomies[path] = relatedFiles(string(data))
	}
	if len(anatomies) == 0 {
		t.Fatal("no tracked ANATOMY.md files found")
	}

	const rootAnatomy = "ANATOMY.md"
	if _, ok := anatomies[rootAnatomy]; !ok {
		t.Fatalf("%s is missing", rootAnatomy)
	}

	// Every related_files entry must resolve to a tracked file, and no list may
	// be empty or repeat an entry.
	for _, path := range sortedKeys(anatomies) {
		entries := anatomies[path]
		if len(entries) == 0 {
			t.Errorf("%s: related_files must not be empty", path)
		}
		seen := map[string]bool{}
		for _, entry := range entries {
			if seen[entry] {
				t.Errorf("%s: duplicate related_files entry %s", path, entry)
			}
			seen[entry] = true
			if entry == path {
				t.Errorf("%s: related_files must not list itself", path)
			}
			if !tracked[entry] {
				t.Errorf("%s: related_files entry %s is not a tracked file", path, entry)
			}
		}
	}

	// Breadth-first descent from the root anatomy.
	reached := map[string]bool{}
	visited := map[string]bool{rootAnatomy: true}
	for queue := []string{rootAnatomy}; len(queue) > 0; {
		current := queue[0]
		queue = queue[1:]
		for _, entry := range anatomies[current] {
			reached[entry] = true
			if _, isAnatomy := anatomies[entry]; isAnatomy && !visited[entry] {
				visited[entry] = true
				queue = append(queue, entry)
			}
		}
	}

	for _, path := range sortedKeys(anatomies) {
		if path != rootAnatomy && !visited[path] {
			t.Errorf("%s is not reachable from %s: link it from its parent anatomy", path, rootAnatomy)
		}
	}

	var orphans []string
	for path := range tracked {
		if path != rootAnatomy && !reached[path] {
			orphans = append(orphans, path)
		}
	}
	sort.Strings(orphans)
	if len(orphans) > 0 {
		const show = 25
		listed := orphans
		if len(listed) > show {
			listed = listed[:show]
		}
		t.Errorf("%d tracked file(s) are not reachable from %s via related_files; "+
			"add each to the related_files of the ANATOMY.md that owns it:\n  %s%s",
			len(orphans), rootAnatomy, strings.Join(listed, "\n  "),
			map[bool]string{true: "\n  ..."}[len(orphans) > show])
	}
}

func trackedFiles(root string) (map[string]bool, error) {
	cmd := exec.Command("git", "-c", "core.quotepath=false", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	files := map[string]bool{}
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" {
			files[path] = true
		}
	}
	return files, nil
}

// relatedFiles extracts the YAML frontmatter related_files list. The schema is
// deliberately narrow — a flat block sequence of plain repo-relative paths —
// so this stays a dependency-free reader rather than a YAML parser.
func relatedFiles(text string) []string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	var entries []string
	inList := false
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if strings.HasPrefix(line, "related_files:") {
			inList = true
			continue
		}
		if !inList {
			continue
		}
		if strings.HasPrefix(line, "  - ") {
			entries = append(entries, strings.TrimSpace(strings.TrimPrefix(line, "  - ")))
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		// A new top-level key ends the list.
		if !strings.HasPrefix(line, " ") {
			inList = false
		}
	}
	return entries
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
