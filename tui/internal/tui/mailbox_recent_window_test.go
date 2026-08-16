package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// seedMailboxWindowLeaf writes one message under <agentDir>/mailbox/<folder>/,
// naming the leaf with the kernel-shaped sortable mailbox id derived from
// index, so directory order and chronological order agree exactly as they do in
// a real mailbox. It returns the id.
func seedMailboxWindowLeaf(t *testing.T, agentDir, folder string, index int, body string) string {
	t.Helper()
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(index) * time.Minute)
	id := fmt.Sprintf("%s-%04x", stamp.Format("20060102T150405"), index%0x10000)
	dir := filepath.Join(agentDir, "mailbox", folder, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"from":        "libai",
		"to":          []string{"human"},
		"subject":     fmt.Sprintf("subject-%d", index),
		"message":     body,
		"received_at": stamp.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "message.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestMailboxLoadsOnlyNewestWindow is the /mailbox half of the message-window
// contract: the page holds the newest entries only, and everything expensive —
// body reads, markdown materialization, search — happens inside that window.
func TestMailboxLoadsOnlyNewestWindow(t *testing.T) {
	agentDir := t.TempDir()
	const limit, extra = 12, 8
	for i := range limit + extra {
		seedMailboxWindowLeaf(t, agentDir, "inbox", i, fmt.Sprintf("body-%04d", i))
	}

	entries := buildMailboxEntriesWindow(agentDir, limit)

	if len(entries) != limit {
		t.Fatalf("loaded %d entries, want exactly the newest %d", len(entries), limit)
	}
	// The window is observable in what was loaded, not applied at render time
	// over a fully read mailbox: a dropped message has no content to search.
	for i := range extra {
		dropped := fmt.Sprintf("body-%04d", i)
		if got := filterMailboxEntries(entries, dropped); len(got) != 0 {
			t.Fatalf("message %q is older than the window but still searchable (%d matches)", dropped, len(got))
		}
	}
	newest := fmt.Sprintf("body-%04d", limit+extra-1)
	if got := filterMailboxEntries(entries, newest); len(got) != 1 {
		t.Fatalf("newest message %q matched %d entries, want 1", newest, len(got))
	}
}

// TestMailboxWindowSpansFoldersByRecency keeps the cap global rather than
// per-folder: inbox, sent and archive compete for the same newest-N slots, and
// each surviving entry keeps its own group.
func TestMailboxWindowSpansFoldersByRecency(t *testing.T) {
	agentDir := t.TempDir()
	for i := range 6 {
		seedMailboxWindowLeaf(t, agentDir, "archive", i, fmt.Sprintf("archived-%d", i))
	}
	for i := 6; i < 12; i++ {
		seedMailboxWindowLeaf(t, agentDir, "inbox", i, fmt.Sprintf("received-%d", i))
	}
	for i := 12; i < 15; i++ {
		seedMailboxWindowLeaf(t, agentDir, "sent", i, fmt.Sprintf("delivered-%d", i))
	}

	entries := buildMailboxEntriesWindow(agentDir, 5)

	if len(entries) != 5 {
		t.Fatalf("loaded %d entries, want the newest 5 across every folder", len(entries))
	}
	groups := map[string]int{}
	for _, e := range entries {
		groups[e.Group]++
		if strings.Contains(e.Content, "archived-") {
			t.Fatalf("an archived message older than the window survived: %q", e.Label)
		}
	}
	if groups["Sent"] != 3 {
		t.Errorf("Sent group holds %d entries, want the 3 newest sent messages (groups: %v)", groups["Sent"], groups)
	}
	if groups["Inbox"] != 2 {
		t.Errorf("Inbox group holds %d entries, want the 2 newest received messages (groups: %v)", groups["Inbox"], groups)
	}
}

// TestMailboxDefaultWindowIsRecentMessageLimit wires the page to the shared cap
// rather than to a local number, against a mailbox that actually exceeds it.
func TestMailboxDefaultWindowIsRecentMessageLimit(t *testing.T) {
	agentDir := t.TempDir()
	const extra = 5
	for i := range fs.RecentMessageLimit() + extra {
		seedMailboxWindowLeaf(t, agentDir, "inbox", i, fmt.Sprintf("body-%04d", i))
	}

	entries := buildMailboxEntries(agentDir)

	if len(entries) != fs.RecentMessageLimit() {
		t.Fatalf("loaded %d entries, want the shared cap %d", len(entries), fs.RecentMessageLimit())
	}
	if got := filterMailboxEntries(entries, "body-0000"); len(got) != 0 {
		t.Errorf("the oldest message is still loaded (%d matches); the cap is not applied before reading", len(got))
	}
}

// TestMailboxWindowKeepsSmallMailboxesWhole guards the ordinary case: a mailbox
// under the cap is unchanged by it, including a self-addressed message that
// legitimately owns a row in both inbox and sent.
func TestMailboxWindowKeepsSmallMailboxesWhole(t *testing.T) {
	agentDir := t.TempDir()
	id := seedMailboxWindowLeaf(t, agentDir, "inbox", 1, "note to self")
	sent := filepath.Join(agentDir, "mailbox", "sent", id)
	if err := os.MkdirAll(sent, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(agentDir, "mailbox", "inbox", id, "message.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sent, "message.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildMailboxEntries(agentDir)

	if len(entries) != 2 {
		t.Fatalf("loaded %d entries, want both the inbox and sent copies of a self-addressed message", len(entries))
	}
	groups := map[string]bool{}
	for _, e := range entries {
		groups[e.Group] = true
	}
	if !groups["Inbox"] || !groups["Sent"] {
		t.Errorf("groups = %v, want both Inbox and Sent", groups)
	}
}
