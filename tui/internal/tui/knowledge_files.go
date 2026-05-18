package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// buildKnowledgeFolderEntries walks a single knowledge entry folder and returns
// MarkdownEntry items for every readable text file inside. Layout mirrors
// buildSkillFolderEntries:
//   - KNOWLEDGE.md always first (ungrouped).
//   - Any other files at the folder root next (ungrouped), alphabetically.
//   - One group per top-level subdirectory (e.g. "references"), contents
//     recursively flattened and sorted by relative path.
//
// Hidden entries (dot-prefixed) and files whose extension is not in
// readableSkillExts are skipped.
func buildKnowledgeFolderEntries(knowledgeDir string) []MarkdownEntry {
	if knowledgeDir == "" {
		return nil
	}
	dirents, err := os.ReadDir(knowledgeDir)
	if err != nil {
		return nil
	}

	var entries []MarkdownEntry

	// Pass 1: root-level files. KNOWLEDGE.md goes to the very front;
	// everything else is alphabetized after it.
	var knowledgeRoot []string
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
		if name == "KNOWLEDGE.md" {
			knowledgeRoot = append(knowledgeRoot, name)
		} else {
			otherRoot = append(otherRoot, name)
		}
	}
	sort.Strings(otherRoot)
	sort.Strings(subdirs)

	for _, name := range knowledgeRoot {
		entries = append(entries, buildKnowledgeFileEntry(knowledgeDir, "", name))
	}
	for _, name := range otherRoot {
		entries = append(entries, buildKnowledgeFileEntry(knowledgeDir, "", name))
	}

	// Pass 2: each top-level subdirectory becomes its own group header.
	for _, sub := range subdirs {
		subPath := filepath.Join(knowledgeDir, sub)
		var files []string
		_ = filepath.WalkDir(subPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if path == subPath {
				return nil
			}
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
			entry := buildKnowledgeFileEntry(subPath, sub, rel)
			entry.Group = sub
			entries = append(entries, entry)
		}
	}

	return entries
}

// buildKnowledgeFileEntry constructs a single MarkdownEntry for one file in a
// knowledge entry folder. Markdown and plain-text files use Path (lazy-loaded);
// other readable extensions are pre-rendered into a code-fenced block.
func buildKnowledgeFileEntry(root, group, rel string) MarkdownEntry {
	full := filepath.Join(root, rel)
	ext := strings.ToLower(filepath.Ext(rel))
	lang, ok := readableSkillExts[ext]
	label := rel
	if !ok || lang == "" {
		return MarkdownEntry{
			Label: label,
			Path:  full,
		}
	}
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
