package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// writeTestMailboxMessage drops one internal mailbox message.json into the
// given folder (inbox/sent/archive) under <baseDir>/human/mailbox/<folder>.
// inbox messages use received_at; sent/archive use sent_at.
func writeTestMailboxMessage(t *testing.T, baseDir, folder string, idx int, subject, body string, stamp time.Time) {
	t.Helper()
	dir := filepath.Join(baseDir, "human", "mailbox", folder, fmt.Sprintf("20260707T1200%02d-msg", idx))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	field := "received_at"
	if folder == "sent" || folder == "archive" {
		field = "sent_at"
	}
	raw, err := json.Marshal(map[string]any{
		"from":    "human",
		"to":      []string{"manager"},
		"subject": subject,
		"message": body,
		field:     stamp.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "message.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func textKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// TestFilterMailboxEntries covers the pure search predicate in isolation:
// case-insensitive, multi-term AND, across label/description/group/content.
func TestFilterMailboxEntries(t *testing.T) {
	entries := []MarkdownEntry{
		{Label: "Alpha request", Description: "needs review", Group: "Inbox", Content: "please find alpha notes"},
		{Label: "Beta note", Description: "FYI", Group: "Sent", Content: "different body entirely"},
		{Label: "Gamma", Description: "alpha flavored", Group: "Archive", Content: "gamma body"},
	}

	cases := []struct {
		name string
		q    string
		want int
	}{
		{"empty returns all", "   ", 3},
		{"single term matches label/content/desc", "alpha", 2}, // Alpha request + "alpha flavored" desc
		{"case insensitive", "ALPHA", 2},
		{"multi-term AND narrows", "beta note", 1},
		{"multi-term no full match", "beta gamma", 0},
		{"matches group", "Sent", 1},
		{"no match", "zzz", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := filterMailboxEntries(entries, c.q)
			if len(got) != c.want {
				t.Fatalf("filterMailboxEntries(%q) = %d entries, want %d", c.q, len(got), c.want)
			}
		})
	}
}

// TestIsMailboxSearchKey ensures both documented entry keys open search.
func TestIsMailboxSearchKey(t *testing.T) {
	if !isMailboxSearchKey(tea.KeyPressMsg{Code: '/', Text: "/"}) {
		t.Error("slash should open mailbox search")
	}
	if !isMailboxSearchKey(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}) {
		t.Error("ctrl+f should open mailbox search")
	}
	if isMailboxSearchKey(tea.KeyPressMsg{Code: 'a', Text: "a"}) {
		t.Error("plain 'a' should not open mailbox search")
	}
}

// TestMailboxSearchFiltersCommitsAndClears drives the full mailbox search
// flow: / opens edit mode, typing filters live, Enter commits the filter
// (leaving edit mode but keeping results), and Esc clears it.
func TestMailboxSearchFiltersCommitsAndClears(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	writeTestMailboxMessage(t, base, "inbox", 0, "Alpha request", "please find alpha", now)
	writeTestMailboxMessage(t, base, "inbox", 1, "Beta note", "different body", now.Add(time.Second))

	m := NewMailboxModel(base)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if got := len(m.inner.entries); got != 2 {
		t.Fatalf("initial entries = %d, want 2", got)
	}
	if got := len(m.allEntries); got != 2 {
		t.Fatalf("allEntries = %d, want 2", got)
	}

	// "/" enters search edit mode.
	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.searchMode {
		t.Fatal("slash should enter mailbox search edit mode")
	}

	// Typing filters live.
	for _, r := range "alpha" {
		m, _ = m.Update(textKey(r))
	}
	if got := strings.TrimSpace(m.searchQuery); got != "alpha" {
		t.Fatalf("searchQuery = %q, want alpha", got)
	}
	if got := len(m.inner.entries); got != 1 {
		t.Fatalf("filtered entries = %d, want 1", got)
	}
	if !strings.Contains(m.inner.entries[0].Content, "Alpha request") {
		t.Fatalf("filtered entry content = %q, want the Alpha request mail", m.inner.entries[0].Content)
	}

	// Enter commits: leaves edit mode but keeps the filter applied.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.searchMode {
		t.Fatal("enter should leave search edit mode while keeping the filter")
	}
	if got := len(m.inner.entries); got != 1 {
		t.Fatalf("entries after enter = %d, want the filtered result", got)
	}
	if strings.TrimSpace(m.searchQuery) == "" {
		t.Fatal("enter should keep the committed query")
	}

	// Esc clears the committed filter and restores the full list.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if strings.TrimSpace(m.searchQuery) != "" {
		t.Fatalf("esc should clear the committed query, got %q", m.searchQuery)
	}
	if got := len(m.inner.entries); got != 2 {
		t.Fatalf("entries after clearing = %d, want 2", got)
	}
}

// TestMailboxSearchMultiTermAndPaste exercises whitespace-AND matching and
// Bubble Tea v2 paste delivery into the search query.
func TestMailboxSearchMultiTermAndPaste(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	writeTestMailboxMessage(t, base, "inbox", 0, "Alpha request", "please find alpha", now)
	writeTestMailboxMessage(t, base, "inbox", 1, "Beta note", "different body", now.Add(time.Second))

	m := NewMailboxModel(base)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})

	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m, _ = m.Update(tea.PasteMsg{Content: "alpha request"})
	if got := strings.TrimSpace(m.searchQuery); got != "alpha request" {
		t.Fatalf("pasted multi-word searchQuery = %q, want 'alpha request'", got)
	}
	if got := len(m.inner.entries); got != 1 {
		t.Fatalf("multi-word filtered entries = %d, want 1", got)
	}
}

// TestMailboxViewRendersDuringSearch locks in the single shared render path:
// the mailbox still renders through MarkdownViewerModel.View() while a search
// is active, with no second render path and no panic, and the footer echoes
// the live query via the existing FooterHint seam.
func TestMailboxViewRendersDuringSearch(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	writeTestMailboxMessage(t, base, "inbox", 0, "Alpha request", "please find alpha", now)

	m := NewMailboxModel(base)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "alpha" {
		m, _ = m.Update(textKey(r))
	}

	view := m.View() // must not panic; single render path preserved
	if view == "" {
		t.Fatal("mailbox View() returned empty while searching")
	}
	if !strings.Contains(view, "alpha") {
		t.Fatalf("mailbox footer should echo the live search query, got:\n%s", view)
	}
}

// expandedGroupCount returns how many groups in the viewer are currently open.
func expandedGroupCount(viewer MarkdownViewerModel) int {
	n := 0
	for _, g := range viewer.groupOrder {
		if viewer.expanded[g] {
			n++
		}
	}
	return n
}

// visibleNodeHasGroup reports whether an expanded entry row of the given group is
// currently visible in the sidebar (i.e. not hidden under a collapsed header).
func visibleNodeHasGroup(viewer MarkdownViewerModel, group string) bool {
	for _, n := range viewer.visibleNodes() {
		if !n.isGroup && n.group == group {
			return true
		}
	}
	return false
}

// TestMailboxGroupExpansionEmptyVsFiltered locks in the group-expansion
// contract that the shared viewer's default (first group expanded, rest
// collapsed) is preserved whenever the query is empty, while a non-empty filter
// expands every group so a match in any group stays visible.
//
// Regression for the parent-found behavior: rebuildInner once expanded every
// group unconditionally, which changed normal idle/reload/agent-switch mailview
// behavior (sidebar could overflow with many groups) for no benefit when no
// filter was active.
func TestMailboxGroupExpansionEmptyVsFiltered(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	// inbox is newest so it is the first group (expanded by the shared default).
	writeTestMailboxMessage(t, base, "inbox", 0, "Inbox update", "shared keyword release notes", now.Add(2*time.Second))
	writeTestMailboxMessage(t, base, "sent", 1, "Sent note", "sent standalone content", now.Add(time.Second))
	writeTestMailboxMessage(t, base, "archive", 2, "Archive report", "shared keyword final archive", now)

	m := NewMailboxModel(base)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// (1) Initial load, empty query: shared viewer default — exactly one group
	//     (the first, Inbox) expanded; Sent and Archive collapsed.
	if got := expandedGroupCount(m.inner); got != 1 {
		t.Fatalf("initial load: %d groups expanded, want 1 (first-only default)", got)
	}
	if !m.inner.expanded["Inbox"] {
		t.Error("initial load: first group Inbox should be expanded")
	}
	if m.inner.expanded["Archive"] || m.inner.expanded["Sent"] {
		t.Error("initial load: non-first groups should be collapsed by default")
	}

	// (2) Non-empty filter spanning Inbox + Archive: every group expands so the
	//     Archive match is not hidden under a collapsed header.
	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "shared keyword" {
		m, _ = m.Update(textKey(r))
	}
	if got := len(m.inner.entries); got != 2 {
		t.Fatalf("filter matched %d entries, want 2 (Inbox + Archive)", got)
	}
	if !m.inner.expanded["Archive"] {
		t.Error("filtered: Archive must expand so its match stays visible")
	}
	if !visibleNodeHasGroup(m.inner, "Archive") {
		t.Error("filtered: Archive match is hidden under a collapsed header (not in visibleNodes)")
	}

	// (3) Esc clears the filter: the first-group-only default is restored.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if strings.TrimSpace(m.searchQuery) != "" {
		t.Fatalf("after esc: query = %q, want empty", m.searchQuery)
	}
	if got := expandedGroupCount(m.inner); got != 1 {
		t.Fatalf("after clear: %d groups expanded, want 1 (first-only restored)", got)
	}
	if !m.inner.expanded["Inbox"] {
		t.Error("after clear: first group Inbox should still be expanded")
	}
	if m.inner.expanded["Archive"] {
		t.Error("after clear: Archive should collapse back to the default")
	}
}

// TestMailboxGroupExpansionCommittedFilterClear covers the non-edit-mode clear
// path: a committed filter (Enter) expands all groups, and a single Esc from the
// normal mailbox view clears it back to the first-group-only default.
func TestMailboxGroupExpansionCommittedFilterClear(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	writeTestMailboxMessage(t, base, "inbox", 0, "Inbox update", "shared keyword release", now.Add(2*time.Second))
	writeTestMailboxMessage(t, base, "archive", 1, "Archive report", "shared keyword final", now)

	m := NewMailboxModel(base)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Open, type, then Enter to commit the filter (leaves edit mode).
	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "shared keyword" {
		m, _ = m.Update(textKey(r))
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.searchMode {
		t.Fatal("enter should leave search edit mode")
	}
	if !m.inner.expanded["Archive"] {
		t.Error("committed filter: Archive must stay expanded so its match is visible")
	}

	// A single Esc from the normal view clears the committed filter and restores
	// the first-group-only default.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if strings.TrimSpace(m.searchQuery) != "" {
		t.Fatalf("after esc: query = %q, want empty", m.searchQuery)
	}
	if got := expandedGroupCount(m.inner); got != 1 {
		t.Fatalf("after clearing committed filter: %d groups expanded, want 1", got)
	}
	if m.inner.expanded["Archive"] {
		t.Error("after clearing committed filter: Archive should collapse back to the default")
	}
}
