package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// readableSkillExts maps file extensions to the glamour code-fence language.
// Files whose extension is not in this map are hidden from the drill-in view.
// "" means "render raw markdown, no fencing".
var readableSkillExts = map[string]string{
	".md":       "",
	".markdown": "",
	".txt":      "",
	".json":     "json",
	".jsonc":    "json",
	".yaml":     "yaml",
	".yml":      "yaml",
	".toml":     "toml",
	".py":       "python",
	".sh":       "bash",
	".bash":     "bash",
	".zsh":      "bash",
	".js":       "javascript",
	".mjs":      "javascript",
	".cjs":      "javascript",
	".ts":       "typescript",
	".tsx":      "typescript",
	".go":       "go",
	".rs":       "rust",
	".rb":       "ruby",
	".html":     "html",
	".css":      "css",
	".sql":      "sql",
	".template": "",
}

// buildSkillFolderEntries walks a single skill folder and returns
// MarkdownEntry items for every readable text file inside. Layout:
//   - SKILL.md always first (ungrouped).
//   - Any other files at the folder root next (ungrouped), alphabetically.
//   - One group per top-level subdirectory (e.g. "references", "scripts",
//     "assets"), contents recursively flattened and sorted by relative path.
//
// Hidden entries (dot-prefixed) are skipped at every level so pytest caches
// and editor metadata don't pollute the view. Files whose extension is not
// in readableSkillExts are skipped as well.
func buildSkillFolderEntries(skillDir string) []MarkdownEntry {
	if skillDir == "" {
		return nil
	}
	dirents, err := os.ReadDir(skillDir)
	if err != nil {
		return nil
	}

	var entries []MarkdownEntry

	// Pass 1: root-level files. SKILL.md goes to the very front; everything
	// else is alphabetized after it.
	var skillRoot []string
	var otherRoot []string
	var subdirs []string
	for _, de := range dirents {
		name := de.Name()
		if isHiddenEntry(name) {
			continue
		}
		if de.IsDir() {
			subdirs = append(subdirs, name)
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := readableSkillExts[ext]; !ok {
			continue
		}
		if name == "SKILL.md" {
			skillRoot = append(skillRoot, name)
		} else {
			otherRoot = append(otherRoot, name)
		}
	}
	sort.Strings(otherRoot)
	sort.Strings(subdirs)

	for _, name := range skillRoot {
		entries = append(entries, buildSkillFileEntry(skillDir, "", name))
	}
	for _, name := range otherRoot {
		entries = append(entries, buildSkillFileEntry(skillDir, "", name))
	}

	// Pass 2: each top-level subdirectory becomes its own group header.
	// Sub-subdirectories are flattened — label carries the relative path so
	// the structure is still visible.
	for _, sub := range subdirs {
		subPath := filepath.Join(skillDir, sub)
		var files []string
		_ = filepath.WalkDir(subPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if path == subPath {
				// Root of the subtree — never "hidden", walk into it.
				return nil
			}
			// Skip hidden dirs (e.g. .pytest_cache) wholesale and hidden
			// files individually. A leading-dot segment in the relative
			// path means something along the way is hidden.
			rel, _ := filepath.Rel(subPath, path)
			for _, seg := range strings.Split(rel, string(filepath.Separator)) {
				if isHiddenEntry(seg) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if _, ok := readableSkillExts[ext]; !ok {
				return nil
			}
			files = append(files, rel)
			return nil
		})
		sort.Strings(files)
		for _, rel := range files {
			entry := buildSkillFileEntry(subPath, sub, rel)
			entry.Group = sub
			entries = append(entries, entry)
		}
	}

	return entries
}

// buildSkillFileEntry constructs a single MarkdownEntry for one file.
//   - root: directory the relative path is rooted at.
//   - group: optional group header (empty means ungrouped root).
//   - rel:  relative path from root to the file.
//
// Markdown and plain-text files are passed through as Path (lazy-loaded by
// the viewer). Other readable extensions are pre-rendered into a code-fenced
// block with a heading so glamour picks up syntax highlighting.
func buildSkillFileEntry(root, group, rel string) MarkdownEntry {
	_ = group // group is set by the caller; kept here for call-site clarity
	full := filepath.Join(root, rel)
	ext := strings.ToLower(filepath.Ext(rel))
	lang, ok := readableSkillExts[ext]
	label := rel
	if !ok || lang == "" {
		// Markdown / plain text — let the viewer read it lazily from disk.
		return MarkdownEntry{
			Label: label,
			Path:  full,
		}
	}
	// Source / structured data — pre-render into a fenced code block so
	// glamour highlights it. The file may be large but the viewer's right
	// pane scrolls independently.
	data, err := os.ReadFile(full)
	if err != nil {
		return MarkdownEntry{
			Label:   label,
			Content: "(could not read file: " + err.Error() + ")",
		}
	}
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(rel)
	b.WriteString("\n\n```")
	b.WriteString(lang)
	b.WriteString("\n")
	b.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		b.WriteString("\n")
	}
	b.WriteString("```\n")
	return MarkdownEntry{
		Label:   label,
		Content: b.String(),
	}
}
