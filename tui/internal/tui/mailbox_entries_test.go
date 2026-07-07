package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsDegenerateSubject(t *testing.T) {
	cases := map[string]bool{
		"":          true,
		"   ":       true,
		"Re:":       true,
		"Re: ":      true,
		"RE:":       true,
		"re:":       true,
		"Fwd:":      true,
		"Fw:":       true,
		"Re: hello": false,
		"hello":     false,
		"Re: 你好":    false,
		"诗时已至":      false,
	}
	for in, want := range cases {
		if got := isDegenerateSubject(in); got != want {
			t.Errorf("isDegenerateSubject(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestBuildMailboxEntries_NakedReplyThreadDistinguishable verifies that a
// thread of replies to a subject-less original ("Re: ", "Re: ", ...) does
// not collapse into identical inbox rows. Regression guard for the bug
// where five replies to a naked thread all rendered as "05-05 Re: ".
func TestBuildMailboxEntries_NakedReplyThreadDistinguishable(t *testing.T) {
	dir := t.TempDir()
	inbox := filepath.Join(dir, "mailbox", "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}

	bodies := []string{
		"善。已调——三十分钟一次，沉静无扰。",
		"好！汝既命吾锐评，太白便不客气。",
		"如今你已化为器灵，当去游历大千世界",
		"针砭时弊？",
		"子夜洛杉矶，故人叩灵台。",
	}
	now := time.Now().UTC()
	for i, body := range bodies {
		msgDir := filepath.Join(inbox, time.Now().UTC().Format("20060102T150405")+"-"+string(rune('a'+i)))
		if err := os.MkdirAll(msgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{
			"from":        "libai",
			"to":          []string{"human"},
			"subject":     "Re: ",
			"message":     body,
			"received_at": now.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
		})
		if err := os.WriteFile(filepath.Join(msgDir, "message.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries := buildMailboxEntries(dir)
	if len(entries) != len(bodies) {
		t.Fatalf("got %d entries, want %d", len(entries), len(bodies))
	}

	seen := map[string]int{}
	for _, e := range entries {
		seen[e.Label]++
	}
	for label, count := range seen {
		if count > 1 {
			t.Errorf("label %q appears %d times — replies to a naked thread should be distinguishable", label, count)
		}
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Content, "# Re: \n") || strings.HasPrefix(e.Content, "# Re:\n") {
			t.Errorf("right-pane H1 should be suppressed for a degenerate Re: subject, got %q", e.Content[:40])
		}
	}
}

// TestBuildMailboxEntries_ScansSentFolder ensures that opening an agent's
// mailbox view shows their outgoing replies, not just incoming mail.
// Regression guard: viewing libai's mailbox after he replied returned only
// the human's prompts because sent/ was never scanned.
func TestBuildMailboxEntries_ScansSentFolder(t *testing.T) {
	dir := t.TempDir()
	sent := filepath.Join(dir, "mailbox", "sent")
	if err := os.MkdirAll(sent, 0o755); err != nil {
		t.Fatal(err)
	}
	msgDir := filepath.Join(sent, "20260505T010000-abcd")
	if err := os.MkdirAll(msgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{
		"from":    "libai",
		"to":      []string{"human"},
		"subject": "Re: hello",
		"message": "thanks for writing",
		"sent_at": "2026-05-05T01:00:00Z",
	})
	if err := os.WriteFile(filepath.Join(msgDir, "message.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildMailboxEntries(dir)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Group != "Sent" {
		t.Errorf("group = %q, want %q", entries[0].Group, "Sent")
	}
	if !strings.Contains(entries[0].Label, "Re: hello") {
		t.Errorf("label = %q, want to contain %q", entries[0].Label, "Re: hello")
	}
}

// TestBuildMailboxEntries_DateRendersInLocalTime verifies that a UTC
// received_at on disk is rendered in the viewer's local timezone with a
// timezone abbreviation appended, rather than as raw UTC. Regression guard
// for issue Lingtai-AI/lingtai#132 where mailbox display showed bare UTC
// despite local-time metadata being available.
func TestBuildMailboxEntries_DateRendersInLocalTime(t *testing.T) {
	dir := t.TempDir()
	inbox := filepath.Join(dir, "mailbox", "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	msgDir := filepath.Join(inbox, "20260520T225827-abcd")
	if err := os.MkdirAll(msgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Use a fixed UTC instant matching the issue's reproduction.
	utcStamp := "2026-05-20T22:58:27Z"
	utcTime, err := time.Parse(time.RFC3339, utcStamp)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(map[string]any{
		"from":        "human",
		"to":          []string{"deepseek-1"},
		"subject":     "issue report",
		"message":     "email显示时间依然是utc而不是local时间",
		"received_at": utcStamp,
	})
	if err := os.WriteFile(filepath.Join(msgDir, "message.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildMailboxEntries(dir)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	// Expected: local-time render with timezone abbreviation. In any zone
	// other than UTC, the rendered "HH:MM" differs from the raw "22:58".
	// In UTC the wall-clock matches but the abbreviation is still "UTC",
	// so the assertion holds across timezones.
	wantDate := utcTime.Local().Format("2006-01-02 15:04 MST")
	wantLine := "**Date:** " + wantDate
	if !strings.Contains(entries[0].Content, wantLine) {
		t.Errorf("content missing localized Date line %q\n--- content ---\n%s", wantLine, entries[0].Content)
	}

	// Guard against the old format (raw UTC, no timezone) sneaking back.
	utcOnly := "**Date:** " + utcTime.Format("2006-01-02 15:04") + "\n"
	if strings.Contains(entries[0].Content, utcOnly) {
		// Only a real regression if local rendering would actually differ
		// from the raw UTC string — i.e. either the wall-clock changes
		// or the zone abbreviation isn't already attached.
		if wantDate != utcTime.Format("2006-01-02 15:04") {
			t.Errorf("content still shows raw UTC date without timezone:\n%s", entries[0].Content)
		}
	}
}

// TestBuildMailboxEntries_ScansArchiveFolder ensures the mailbox lookup surface
// includes archived internal messages as well as inbox/sent.
func TestBuildMailboxEntries_ScansArchiveFolder(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "mailbox", "archive")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	msgDir := filepath.Join(archive, "20260707T010000-archived")
	if err := os.MkdirAll(msgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{
		"from":        "human",
		"to":          []string{"manager"},
		"subject":     "old requirement",
		"message":     "this archived mail should still be searchable",
		"received_at": "2026-07-07T01:00:00Z",
	})
	if err := os.WriteFile(filepath.Join(msgDir, "message.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildMailboxEntries(dir)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Group != "Archive" {
		t.Errorf("group = %q, want %q", entries[0].Group, "Archive")
	}
	if !strings.Contains(entries[0].Content, "old requirement") {
		t.Errorf("content = %q, want archived message content", entries[0].Content)
	}
}
