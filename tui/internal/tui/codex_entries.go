package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// codexFile matches the legacy storage schema at codex/codex.json written by
// older agents. New agents use knowledge/<name>/KNOWLEDGE.md folders; the JSON
// reader remains as a display fallback for workdirs that have not booted on the
// migrating kernel yet.
type codexFile struct {
	Version int          `json:"version"`
	Entries []codexEntry `json:"entries"`
}

type codexEntry struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Summary       string `json:"summary"`
	Content       string `json:"content"`
	Supplementary string `json:"supplementary"`
	CreatedAt     string `json:"created_at"`
}

// buildAgentCodexEntries returns the private knowledge entries for a single
// agent. The canonical source is <agent>/knowledge/**/KNOWLEDGE.md. If no
// filesystem-backed entries exist, it falls back to legacy codex/codex.json so
// the TUI can still display older agents before they refresh and migrate.
func buildAgentCodexEntries(agentDir string) []MarkdownEntry {
	if agentDir == "" {
		return nil
	}
	if entries := buildAgentKnowledgeEntries(agentDir); len(entries) > 0 {
		return entries
	}
	return buildLegacyCodexEntries(agentDir)
}

type knowledgeEntry struct {
	Name        string
	Description string
	Path        string
	RelDir      string
}

func buildAgentKnowledgeEntries(agentDir string) []MarkdownEntry {
	knowledgeDir := filepath.Join(agentDir, "knowledge")
	info, err := os.Stat(knowledgeDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	var entries []knowledgeEntry
	_ = filepath.WalkDir(knowledgeDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if isHiddenEntry(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if name != "KNOWLEDGE.md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fm := parseFrontmatter(string(data))
		if fm == nil {
			return nil
		}
		entryName := cleanFrontmatterScalar(fm["name"])
		description := cleanFrontmatterScalar(fm["description"])
		if entryName == "" || description == "" {
			return nil
		}
		rel, err := filepath.Rel(knowledgeDir, filepath.Dir(path))
		if err != nil || rel == "." {
			rel = ""
		}
		entries = append(entries, knowledgeEntry{
			Name:        entryName,
			Description: description,
			Path:        path,
			RelDir:      rel,
		})
		return nil
	})

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].RelDir != entries[j].RelDir {
			return entries[i].RelDir < entries[j].RelDir
		}
		return entries[i].Name < entries[j].Name
	})

	result := make([]MarkdownEntry, 0, len(entries))
	for _, e := range entries {
		label := e.Name
		if len(label) > 30 {
			label = label[:27] + "..."
		}
		group := "Knowledge"
		if e.RelDir != "" {
			group = e.RelDir
		}
		result = append(result, MarkdownEntry{
			Label: label,
			Group: group,
			Path:  e.Path,
		})
	}
	return result
}

func cleanFrontmatterScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	return strings.TrimSpace(s)
}

func buildLegacyCodexEntries(agentDir string) []MarkdownEntry {
	codexPath := filepath.Join(agentDir, "codex", "codex.json")
	data, err := os.ReadFile(codexPath)
	if err != nil {
		return nil
	}
	var cdx codexFile
	if json.Unmarshal(data, &cdx) != nil || len(cdx.Entries) == 0 {
		return nil
	}

	entries := make([]codexEntry, len(cdx.Entries))
	copy(entries, cdx.Entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt > entries[j].CreatedAt
	})

	result := make([]MarkdownEntry, 0, len(entries))
	for _, le := range entries {
		label := le.Title
		if label == "" {
			label = le.ID
		}
		if len(label) > 30 {
			label = label[:27] + "..."
		}

		var md strings.Builder
		md.WriteString("# " + le.Title + "\n\n")
		if le.Summary != "" {
			md.WriteString("> " + le.Summary + "\n\n")
		}
		if le.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, le.CreatedAt); err == nil {
				md.WriteString(fmt.Sprintf("*%s* · `%s`\n\n", t.Format("2006-01-02 15:04"), le.ID))
			} else {
				md.WriteString(fmt.Sprintf("`%s`\n\n", le.ID))
			}
		}
		md.WriteString("---\n\n")
		md.WriteString(le.Content)
		if le.Supplementary != "" {
			md.WriteString("\n\n---\n\n## Supplementary\n\n" + le.Supplementary)
		}

		result = append(result, MarkdownEntry{
			Label:   label,
			Group:   "Legacy codex",
			Content: md.String(),
		})
	}

	return result
}
