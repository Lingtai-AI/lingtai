package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestRebuildDeduplicatesMailAcrossRestart is a regression test for the
// "every relaunch duplicates all mail" bug caused by loadExisting+mailSeen
// keying on different strings than IngestMail. With rebuild-on-every-launch,
// running RebuildFromSources twice in a row must produce the same output.
func TestRebuildDeduplicatesMailAcrossRestart(t *testing.T) {
	tmp := t.TempDir()
	humanDir := filepath.Join(tmp, "human")
	orchDir := filepath.Join(tmp, "orch")
	inboxDir := filepath.Join(humanDir, "mailbox", "inbox")
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Write 3 mail messages to the inbox with raw From="xiake" and
	// identity.agent_name="徐霞客" — same shape as the real bug.
	mails := []MailMessage{
		{ID: "m1", From: "xiake", To: "human", ReceivedAt: "2026-04-07T15:36:44Z", Subject: "one", Message: "first", Identity: map[string]interface{}{"agent_name": "徐霞客"}},
		{ID: "m2", From: "xiake", To: "human", ReceivedAt: "2026-04-07T15:37:47Z", Subject: "two", Message: "second", Identity: map[string]interface{}{"agent_name": "徐霞客"}},
		{ID: "m3", From: "xiake", To: "human", ReceivedAt: "2026-04-07T15:38:59Z", Subject: "three", Message: "third", Identity: map[string]interface{}{"agent_name": "徐霞客"}},
	}
	for _, m := range mails {
		dir := filepath.Join(inboxDir, m.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(dir, "message.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// First rebuild (simulates fresh launch)
	sc1 := NewSessionCache(humanDir, tmp)
	cache1 := NewMailCache(humanDir).Refresh()
	sc1.RebuildFromSources(cache1, "human", orchDir, "xiake")
	firstLen := sc1.Len()
	if firstLen != len(mails) {
		t.Fatalf("first rebuild: expected %d entries, got %d", len(mails), firstLen)
	}

	// Second rebuild (simulates relaunch — the bug scenario)
	sc2 := NewSessionCache(humanDir, tmp)
	cache2 := NewMailCache(humanDir).Refresh()
	sc2.RebuildFromSources(cache2, "human", orchDir, "xiake")
	secondLen := sc2.Len()
	if secondLen != firstLen {
		t.Fatalf("second rebuild: expected %d entries (same as first), got %d — duplicates indicate the bug regressed", firstLen, secondLen)
	}

	// Verify the on-disk file has exactly the expected number of lines
	data, err := os.ReadFile(filepath.Join(humanDir, "logs", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lineCount := 0
	for _, b := range data {
		if b == '\n' {
			lineCount++
		}
	}
	if lineCount != len(mails) {
		t.Fatalf("session.jsonl: expected %d lines, got %d", len(mails), lineCount)
	}
}

// TestIngestMailWatermarkSkipsOldMail verifies that during a live session
// (not a rebuild), IngestMail skips mail older than the watermark.
func TestIngestMailWatermarkSkipsOldMail(t *testing.T) {
	tmp := t.TempDir()
	humanDir := filepath.Join(tmp, "human")
	inboxDir := filepath.Join(humanDir, "mailbox", "inbox")
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sc := NewSessionCache(humanDir, tmp)
	sc.lastMailTs = "2026-04-10T00:00:00Z" // simulate post-rebuild watermark

	cache := MailCache{
		inboxSeen: map[string]struct{}{},
		sentSeen:  map[string]struct{}{},
		Messages: []MailMessage{
			{From: "human", ReceivedAt: "2026-04-07T00:00:00Z", Message: "old"},   // below watermark — should skip
			{From: "human", ReceivedAt: "2026-04-11T00:00:00Z", Message: "new"},   // above watermark — should ingest
		},
	}

	sc.IngestMail(cache, "human", "", "orch")
	if got := sc.Len(); got != 1 {
		t.Fatalf("expected 1 entry (new only), got %d", got)
	}
	if sc.Entries()[0].Body != "new" {
		t.Fatalf("wrong entry admitted: body=%q", sc.Entries()[0].Body)
	}
	if sc.lastMailTs != "2026-04-11T00:00:00Z" {
		t.Fatalf("watermark not advanced: got %q", sc.lastMailTs)
	}
}
