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
	"github.com/charmbracelet/x/ansi"
)

func writeMailboxViewTestMessage(t *testing.T, baseDir, folder string, idx int, subject, body string, stamp time.Time) {
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

func TestMailboxWheelPageHomeEndNavigateMessageList(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		writeMailboxViewTestMessage(t, base, "inbox", i, fmt.Sprintf("message-%d", i), "body", now.Add(time.Duration(i)*time.Second))
	}

	m := NewMailboxModel(base)
	var cmd tea.Cmd
	m, cmd = m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if cmd != nil {
		_ = cmd()
	}
	if got := m.inner.currentEntryIndex(); got != 0 {
		t.Fatalf("initial currentEntryIndex = %d, want 0", got)
	}

	m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := m.inner.currentEntryIndex(); got != 1 {
		t.Fatalf("after wheel down currentEntryIndex = %d, want 1", got)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if got, want := m.inner.currentEntryIndex(), len(m.inner.entries)-1; got != want {
		t.Fatalf("after end currentEntryIndex = %d, want %d", got, want)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if got := m.inner.currentEntryIndex(); got != 0 {
		t.Fatalf("after home currentEntryIndex = %d, want 0", got)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if got := m.inner.currentEntryIndex(); got <= 0 {
		t.Fatalf("after pgdown currentEntryIndex = %d, want it to advance", got)
	}
}

func TestMailboxSearchFiltersAndEscClears(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	writeMailboxViewTestMessage(t, base, "inbox", 0, "Alpha request", "please find alpha", now)
	writeMailboxViewTestMessage(t, base, "inbox", 1, "Beta note", "different body", now.Add(time.Second))

	m := NewMailboxModel(base)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if got := len(m.inner.entries); got != 2 {
		t.Fatalf("initial entries = %d, want 2", got)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.searchMode {
		t.Fatal("slash should enter mailbox search mode")
	}
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
		t.Fatalf("filtered entry content = %q, want Alpha request", m.inner.entries[0].Content)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.searchMode {
		t.Fatal("enter should leave search edit mode while keeping the filter")
	}
	if got := len(m.inner.entries); got != 1 {
		t.Fatalf("entries after enter = %d, want filtered result", got)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if strings.TrimSpace(m.searchQuery) != "" {
		t.Fatalf("esc should clear searchQuery, got %q", m.searchQuery)
	}
	if got := len(m.inner.entries); got != 2 {
		t.Fatalf("entries after clearing = %d, want 2", got)
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m, _ = m.Update(tea.PasteMsg{Content: "alpha request"})
	if got := strings.TrimSpace(m.searchQuery); got != "alpha request" {
		t.Fatalf("pasted multi-word searchQuery = %q, want alpha request", got)
	}
	if got := len(m.inner.entries); got != 1 {
		t.Fatalf("multi-word filtered entries = %d, want 1", got)
	}
}

func TestMailboxScrollbarDragJumpsMessageList(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		writeMailboxViewTestMessage(t, base, "inbox", i, fmt.Sprintf("message-%02d", i), "body", now.Add(time.Duration(i)*time.Second))
	}

	m := NewMailboxModel(base)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	if visible, _, _, _, _ := m.mailboxScrollbarMetrics(); !visible {
		t.Fatal("expected overflowing mailbox list to render a scrollbar")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "█") {
		t.Fatalf("mailbox view should contain a scrollbar thumb, got:\n%s", view)
	}

	leftW, _ := m.inner.panelWidths()
	bottomY := mdvHeaderLines + m.inner.leftVP.Height() - 1
	m, _ = m.Update(tea.MouseClickMsg{X: leftW - 1, Y: bottomY, Button: tea.MouseLeft})
	if !m.scrollbarDragging {
		t.Fatal("clicking the scrollbar should start a drag")
	}
	if got := m.inner.leftVP.YOffset(); got == 0 {
		t.Fatal("dragging to the bottom should advance the left viewport offset")
	}
	if got := m.inner.currentEntryIndex(); got < 20 {
		t.Fatalf("dragging to bottom currentEntryIndex = %d, want near the end", got)
	}
	m, _ = m.Update(tea.MouseReleaseMsg{})
	if m.scrollbarDragging {
		t.Fatal("mouse release should end scrollbar dragging")
	}
}
