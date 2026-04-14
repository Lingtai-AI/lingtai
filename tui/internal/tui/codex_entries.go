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

// codexFile matches the JSON schema of codex/codex.json written by agents.
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

// buildCodexEntries scans all agent directories under lingtaiDir for
// codex/codex.json files and returns MarkdownEntry items grouped by
// agent name. Each codex entry becomes one sidebar item whose content
// is the entry's markdown content (with metadata header).
func buildCodexEntries(lingtaiDir string) []MarkdownEntry {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		return nil
	}

	var result []MarkdownEntry

	// Collect agents that have codex archives, sorted by name
	type agentCodex struct {
		name    string
		entries []codexEntry
	}
	var agents []agentCodex

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		codexPath := filepath.Join(lingtaiDir, entry.Name(), "codex", "codex.json")
		data, err := os.ReadFile(codexPath)
		if err != nil {
			continue
		}
		var cdx codexFile
		if json.Unmarshal(data, &cdx) != nil || len(cdx.Entries) == 0 {
			continue
		}
		agents = append(agents, agentCodex{name: entry.Name(), entries: cdx.Entries})
	}

	sort.Slice(agents, func(i, j int) bool {
		return agents[i].name < agents[j].name
	})

	for _, ag := range agents {
		// Sort entries by created_at descending (newest first)
		sort.Slice(ag.entries, func(i, j int) bool {
			return ag.entries[i].CreatedAt > ag.entries[j].CreatedAt
		})
		for _, le := range ag.entries {
			label := le.Title
			if label == "" {
				label = le.ID
			}
			// Truncate long titles for sidebar
			if len(label) > 30 {
				label = label[:27] + "..."
			}

			// Build the right-panel content as markdown
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
				Group:   ag.name,
				Content: md.String(),
			})
		}
	}

	return result
}
