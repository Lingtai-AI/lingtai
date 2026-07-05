package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// legacyKnowledgeFile matches the legacy storage schema at codex/codex.json written by
// older agents. New agents use knowledge/<name>/KNOWLEDGE.md folders. The TUI
// now performs a one-time migration from this JSON store into the folder
// layout on first scan, mirroring the kernel's migration semantics.
type legacyKnowledgeFile struct {
	Version int                    `json:"version"`
	Entries []legacyKnowledgeEntry `json:"entries"`
}

type legacyKnowledgeEntry struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Summary       string `json:"summary"`
	Content       string `json:"content"`
	Supplementary string `json:"supplementary"`
	CreatedAt     string `json:"created_at"`
}

// buildAgentKnowledgeCatalogEntries returns the private knowledge entries for a single
// agent. Before scanning the canonical knowledge/ folder it runs a one-time
// migration from legacy JSON stores (codex/codex.json and knowledge/knowledge.json)
// so /knowledge consistently shows filesystem-backed entries instead of a
// legacy fallback group.
func buildAgentKnowledgeCatalogEntries(agentDir string) []MarkdownEntry {
	if agentDir == "" {
		return nil
	}
	migrateLegacyJSONStores(agentDir)
	return buildAgentKnowledgeEntries(agentDir)
}

type knowledgeEntry struct {
	Name        string
	Description string
	Path        string
	RelDir      string
}

func buildAgentKnowledgeEntries(agentDir string) []MarkdownEntry {
	knowledgeDir := filepath.Join(agentDir, "knowledge")
	dirents, err := os.ReadDir(knowledgeDir)
	if err != nil {
		return nil
	}

	// The top-level /knowledge catalog mirrors the kernel prompt catalog: only
	// immediate knowledge/<name>/KNOWLEDGE.md entries appear here. Nested
	// sub-knowledge entries are second-layer detail owned by their routing parent
	// and remain reachable after pressing Enter to drill into that parent folder.
	var entries []knowledgeEntry
	for _, de := range dirents {
		name := de.Name()
		if !de.IsDir() || isHiddenEntry(name) {
			continue
		}
		path := filepath.Join(knowledgeDir, name, "KNOWLEDGE.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fm := parseFrontmatter(string(data))
		if fm == nil {
			continue
		}
		entryName := cleanFrontmatterScalar(fm["name"])
		description := cleanFrontmatterScalar(fm["description"])
		if entryName == "" || description == "" {
			continue
		}
		entries = append(entries, knowledgeEntry{
			Name:        entryName,
			Description: description,
			Path:        path,
			RelDir:      name,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	result := make([]MarkdownEntry, 0, len(entries))
	for _, e := range entries {
		label := e.Name
		if len(label) > 30 {
			label = label[:27] + "..."
		}
		result = append(result, MarkdownEntry{
			Label:       label,
			Description: e.Description,
			Group:       "Knowledge",
			Path:        e.Path,
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

// migrateLegacyJSONStores performs a one-time migration from legacy JSON
// stores into knowledge/<slug>/KNOWLEDGE.md folders. After a successful
// migration the source JSON is renamed to <name>.migrated (with a numeric
// suffix if that already exists) so the migration does not repeat.
func migrateLegacyJSONStores(agentDir string) {
	knowledgeDir := filepath.Join(agentDir, "knowledge")
	migrateOneLegacyJSON(knowledgeDir, filepath.Join(knowledgeDir, "knowledge.json"), "migrated-knowledge-json")
	migrateOneLegacyJSON(knowledgeDir, filepath.Join(agentDir, "codex", "codex.json"), "migrated-codex-json")
}

func migrateOneLegacyJSON(knowledgeDir, legacyPath, origin string) {
	info, err := os.Stat(legacyPath)
	if err != nil || info.IsDir() {
		return
	}
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return
	}
	var cdx legacyKnowledgeFile
	if err := json.Unmarshal(data, &cdx); err != nil {
		return
	}
	if len(cdx.Entries) == 0 {
		return
	}

	migrated := 0
	for idx, le := range cdx.Entries {
		legacyID := strings.TrimSpace(le.ID)
		if legacyID == "" {
			legacyID = fmt.Sprintf("%d", idx+1)
		}
		title := strings.TrimSpace(le.Title)
		if title == "" {
			title = strings.TrimSpace(le.Content)
		}
		if title == "" {
			title = fmt.Sprintf("Entry %d", idx+1)
		}
		summary := strings.TrimSpace(le.Summary)
		if summary == "" {
			summary = title
		}
		if summary == "" {
			summary = strings.TrimSpace(le.Content)
		}
		if summary == "" {
			summary = "Migrated knowledge entry"
		}
		content := strings.TrimSpace(le.Content)
		supplementary := strings.TrimSpace(le.Supplementary)
		createdAt := strings.TrimSpace(le.CreatedAt)

		entryDir, name := uniqueEntryDir(knowledgeDir, title, legacyID)
		md := formatKnowledgeMD(name, title, summary, content, supplementary, legacyID, createdAt, origin)
		if err := os.MkdirAll(entryDir, 0o755); err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(entryDir, "KNOWLEDGE.md"), []byte(md), 0o644); err != nil {
			continue
		}
		if supplementary != "" {
			refsDir := filepath.Join(entryDir, "references")
			if err := os.MkdirAll(refsDir, 0o755); err == nil {
				_ = os.WriteFile(filepath.Join(refsDir, "supplementary.md"), []byte(supplementary+"\n"), 0o644)
			}
		}
		migrated++
	}

	if migrated == 0 {
		return
	}
	backup := legacyPath + ".migrated"
	if _, err := os.Stat(backup); err == nil {
		n := 2
		for {
			candidate := fmt.Sprintf("%s.migrated.%d", legacyPath, n)
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				backup = candidate
				break
			}
			n++
		}
	}
	_ = os.Rename(legacyPath, backup)
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(value, fallback string) string {
	base := strings.ToLower(strings.TrimSpace(value))
	if base == "" {
		base = fallback
	}
	base = slugRE.ReplaceAllString(base, "-")
	base = strings.Trim(base, ".-_")
	if base == "" {
		base = fallback
	}
	if len(base) > 64 {
		base = base[:64]
	}
	base = strings.Trim(base, ".-_")
	if base == "" {
		base = fallback
	}
	return base
}

func uniqueEntryDir(root, preferred, legacyID string) (string, string) {
	slug := slugify(preferred, slugify(legacyID, "entry"))
	candidate := filepath.Join(root, slug)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, slug
	}
	suffixBase := slugify(legacyID, "entry")
	combined := slug + "-" + suffixBase
	candidate = filepath.Join(root, combined)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, combined
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s-%s-%d", slug, suffixBase, i)
		candidate = filepath.Join(root, name)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, name
		}
	}
}

func yamlQuote(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func formatKnowledgeMD(name, title, description, content, supplementary, legacyID, createdAt, origin string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + yamlQuote(name) + "\n")
	b.WriteString("description: " + yamlQuote(description) + "\n")
	b.WriteString("version: \"1.0.0\"\n")
	b.WriteString("origin: " + yamlQuote(origin) + "\n")
	if legacyID != "" {
		b.WriteString("legacy_id: " + yamlQuote(legacyID) + "\n")
	}
	if title != "" {
		b.WriteString("title: " + yamlQuote(title) + "\n")
	}
	if createdAt != "" {
		b.WriteString("created_at: " + yamlQuote(createdAt) + "\n")
	}
	b.WriteString("---\n\n")
	if title != "" {
		b.WriteString("# " + title + "\n\n")
	}
	if content != "" {
		b.WriteString(strings.TrimRight(content, "\n") + "\n\n")
	} else {
		b.WriteString(description + "\n\n")
	}
	if supplementary != "" {
		b.WriteString("## References\n\n")
		b.WriteString("- [Migrated supplementary material](references/supplementary.md)\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
