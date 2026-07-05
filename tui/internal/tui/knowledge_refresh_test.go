package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestKnowledgeEntry(t *testing.T, agentDir string, parts []string, name, description string) string {
	t.Helper()
	segments := append([]string{agentDir, "knowledge"}, parts...)
	path := filepath.Join(append(segments, "KNOWLEDGE.md")...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func markdownEntriesHaveLabel(entries []MarkdownEntry, label string) bool {
	for _, entry := range entries {
		if entry.Label == label {
			return true
		}
	}
	return false
}

func markdownEntriesHavePath(entries []MarkdownEntry, path string) bool {
	for _, entry := range entries {
		if entry.Path == path {
			return true
		}
	}
	return false
}

func TestKnowledgeModelCtrlRRefreshesTopLayerFromDisk(t *testing.T) {
	baseDir := t.TempDir()
	agentDir := filepath.Join(baseDir, "agent")
	writeTestKnowledgeEntry(t, agentDir, []string{"alpha"}, "alpha", "first entry")

	m := NewKnowledgeModel(baseDir, agentDir)
	if !markdownEntriesHaveLabel(m.inner.entries, "alpha") {
		t.Fatalf("initial catalog missing alpha: %+v", m.inner.entries)
	}
	writeTestKnowledgeEntry(t, agentDir, []string{"beta"}, "beta", "runtime entry")
	if markdownEntriesHaveLabel(m.inner.entries, "beta") {
		t.Fatalf("precondition failed: stale model saw beta before refresh: %+v", m.inner.entries)
	}

	updated, _ := m.Update(ctrlR())
	m = updated
	if !markdownEntriesHaveLabel(m.inner.entries, "beta") {
		t.Fatalf("catalog ctrl+r did not reload runtime entry beta: %+v", m.inner.entries)
	}
}

func TestKnowledgeModelCtrlRRefreshesDrillInLayerFromDisk(t *testing.T) {
	baseDir := t.TempDir()
	agentDir := filepath.Join(baseDir, "agent")
	writeTestKnowledgeEntry(t, agentDir, []string{"session-journal"}, "session-journal", "journal index")
	firstChild := writeTestKnowledgeEntry(t, agentDir, []string{"session-journal", "first"}, "first", "first child")

	m := NewKnowledgeModel(baseDir, agentDir)
	if len(m.inner.entries) != 1 {
		t.Fatalf("got %d top-level entries, want session-journal only: %+v", len(m.inner.entries), m.inner.entries)
	}
	updated, _ := m.Update(MarkdownViewerSelectMsg{Entry: m.inner.entries[0]})
	m = updated
	if m.drillIn == nil {
		t.Fatal("selecting session-journal did not open drill-in viewer")
	}
	if !markdownEntriesHavePath(m.drillIn.entries, firstChild) {
		t.Fatalf("initial drill-in missing first child %q: %+v", firstChild, m.drillIn.entries)
	}

	secondChild := writeTestKnowledgeEntry(t, agentDir, []string{"session-journal", "second"}, "second", "runtime child")
	if markdownEntriesHavePath(m.drillIn.entries, secondChild) {
		t.Fatalf("precondition failed: stale drill-in saw second child before refresh: %+v", m.drillIn.entries)
	}
	updated, _ = m.Update(ctrlR())
	m = updated
	if m.drillIn == nil {
		t.Fatal("drill-in ctrl+r should keep the drill-in viewer open")
	}
	if !markdownEntriesHavePath(m.drillIn.entries, secondChild) {
		t.Fatalf("drill-in ctrl+r did not reload runtime child %q: %+v", secondChild, m.drillIn.entries)
	}
}

func TestAppRefreshDoneRefreshesOpenKnowledgeView(t *testing.T) {
	baseDir := t.TempDir()
	agentDir := filepath.Join(baseDir, "agent")
	writeTestKnowledgeEntry(t, agentDir, []string{"alpha"}, "alpha", "first entry")

	app := App{
		currentView: appViewKnowledge,
		knowledge:   NewKnowledgeModel(baseDir, agentDir),
	}
	writeTestKnowledgeEntry(t, agentDir, []string{"beta"}, "beta", "runtime entry")

	model, _ := app.Update(refreshDoneMsg{})
	app = model.(App)
	if !markdownEntriesHaveLabel(app.knowledge.inner.entries, "beta") {
		t.Fatalf("refreshDoneMsg did not refresh open /knowledge catalog: %+v", app.knowledge.inner.entries)
	}
}
