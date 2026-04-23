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

// buildAgentCodexEntries returns the codex entries for a single agent,
// sorted newest-first, flat (no group header). Each entry becomes one
// sidebar item whose content is the entry's markdown content with a
// metadata header. Returns an empty slice if the agent has no codex
// archive or it is empty.
func buildAgentCodexEntries(agentDir string) []MarkdownEntry {
	if agentDir == "" {
		return nil
	}
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
			Content: md.String(),
		})
	}

	return result
}
