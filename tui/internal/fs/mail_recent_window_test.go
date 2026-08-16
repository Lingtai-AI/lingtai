package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeRecentWindowMessage writes one mailbox leaf under folder with a
// kernel-shaped sortable mailbox id derived from index, so directory order and
// chronological order agree exactly as they do in a real mailbox.
func writeRecentWindowMessage(t *testing.T, humanDir, folder string, index int) string {
	t.Helper()
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(index) * time.Minute)
	id := fmt.Sprintf("%s-%04x", stamp.Format("20060102T150405"), index%0x10000)
	parent := filepath.Join(humanDir, "mailbox", folder)
	if _, err := os.Stat(parent); err != nil {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(parent, id)
	if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	msg := MailMessage{
		ID:         id,
		MailboxID:  id,
		From:       "alice",
		To:         []string{"human"},
		Subject:    fmt.Sprintf("subject-%d", index),
		Message:    fmt.Sprintf("body-%d", index),
		Type:       "normal",
		ReceivedAt: stamp.Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "message.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestRefreshRecentRetainsOnlyNewestLimitInChronologicalOrder pins the hard
// contract at the fs boundary: a mailbox holding more than the cap loads only
// the newest RecentMessageLimit entries, the cap is visible in the returned
// snapshot itself (not hidden at render time), and what survives is still in
// chronological order with the newest entry last.
func TestRefreshRecentRetainsOnlyNewestLimitInChronologicalOrder(t *testing.T) {
	humanDir := t.TempDir()
	const extra = 25
	total := RecentMessageLimit + extra
	ids := make([]string, 0, total)
	for i := range total {
		ids = append(ids, writeRecentWindowMessage(t, humanDir, "inbox", i))
	}

	cache := NewMailCache(humanDir).RefreshRecent(RecentMessageLimit)

	if len(cache.Messages) != RecentMessageLimit {
		t.Fatalf("loaded %d messages, want exactly the newest %d", len(cache.Messages), RecentMessageLimit)
	}
	if got, want := cache.Messages[0].MailboxID, ids[extra]; got != want {
		t.Errorf("oldest retained message = %q, want %q (the window must start after the %d dropped entries)", got, want, extra)
	}
	if got, want := cache.Messages[len(cache.Messages)-1].MailboxID, ids[total-1]; got != want {
		t.Errorf("newest retained message = %q, want %q", got, want)
	}
	for i := 1; i < len(cache.Messages); i++ {
		if !mailMessageBefore(cache.Messages[i-1], cache.Messages[i]) {
			t.Fatalf("window is not chronological at index %d: %q then %q",
				i, cache.Messages[i-1].ReceivedAt, cache.Messages[i].ReceivedAt)
		}
	}
	// Bodies outside the window are absent from the snapshot entirely.
	for _, msg := range cache.Messages {
		if msg.Message == "body-0" {
			t.Fatal("an entry older than the window survived into the loaded snapshot")
		}
	}
	if len(cache.seen) != RecentMessageLimit {
		t.Errorf("seen index holds %d ids, want %d — the index must track the window, not the mailbox", len(cache.seen), RecentMessageLimit)
	}
}

// TestRefreshRecentKeepsNewMessagesArrivingAndDropsTheOldest is the
// steady-state half: ordinary refresh still picks up newly delivered mail, and
// the window slides so the snapshot never grows past the cap.
func TestRefreshRecentKeepsNewMessagesArrivingAndDropsTheOldest(t *testing.T) {
	humanDir := t.TempDir()
	const limit = 5
	for i := range limit {
		writeRecentWindowMessage(t, humanDir, "inbox", i)
	}
	cache := NewMailCache(humanDir).RefreshRecent(limit)
	if len(cache.Messages) != limit {
		t.Fatalf("baseline loaded %d, want %d", len(cache.Messages), limit)
	}
	oldest := cache.Messages[0].MailboxID

	newest := writeRecentWindowMessage(t, humanDir, "inbox", limit)
	cache = cache.RefreshRecent(limit)

	if len(cache.Messages) != limit {
		t.Fatalf("after a new message the snapshot holds %d, want the cap %d", len(cache.Messages), limit)
	}
	if got := cache.Messages[len(cache.Messages)-1].MailboxID; got != newest {
		t.Errorf("newest message %q did not land in the refreshed window (last = %q)", newest, got)
	}
	if _, stillThere := cache.seen[oldest]; stillThere {
		t.Errorf("message %q should have slid out of the window", oldest)
	}
}

// TestRefreshRecentFlipsDeliveredAndDeduplicatesAcrossFolders keeps the bounded
// refresh behaviorally identical to Refresh for the outbox→sent transition: one
// entry, no duplicate, Delivered flipped in place.
func TestRefreshRecentFlipsDeliveredAndDeduplicatesAcrossFolders(t *testing.T) {
	humanDir := t.TempDir()
	id := writeRecentWindowMessage(t, humanDir, "outbox", 1)

	cache := NewMailCache(humanDir).RefreshRecent(RecentMessageLimit)
	if len(cache.Messages) != 1 || cache.Messages[0].Delivered {
		t.Fatalf("outbox message = %+v, want exactly one undelivered entry", cache.Messages)
	}

	sentParent := filepath.Join(humanDir, "mailbox", "sent")
	if err := os.MkdirAll(sentParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(humanDir, "mailbox", "outbox", id), filepath.Join(sentParent, id)); err != nil {
		t.Fatal(err)
	}

	cache = cache.RefreshRecent(RecentMessageLimit)
	if len(cache.Messages) != 1 {
		t.Fatalf("after pickup: %d entries, want 1 (no duplicate across folders)", len(cache.Messages))
	}
	if !cache.Messages[0].Delivered {
		t.Error("after pickup: Delivered = false, want true")
	}
}

// TestRecentMailboxIDsWindowsByNameAlone pins the primitive readers outside this
// package bound themselves with. The window is decided from the directory
// listing only, so a folder whose leaves hold no message.json at all still
// yields exactly the newest ids: nothing here can be paying for a body read.
func TestRecentMailboxIDsWindowsByNameAlone(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "inbox")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	const total, limit = 25, 10
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids := make([]string, 0, total)
	for i := range total {
		stamp := base.Add(time.Duration(i) * time.Minute)
		id := fmt.Sprintf("%s-%04x", stamp.Format("20060102T150405"), i)
		if err := os.Mkdir(filepath.Join(folder, id), 0o755); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	// A stray non-directory entry is not a mailbox leaf and must not occupy a
	// slot in the window.
	if err := os.WriteFile(filepath.Join(folder, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := RecentMailboxIDs(folder, limit)
	if len(got) != limit {
		t.Fatalf("got %d ids, want the newest %d", len(got), limit)
	}
	for i, want := range ids[total-limit:] {
		if got[i] != want {
			t.Fatalf("id %d = %q, want %q (window must be the newest ids, oldest-first)", i, got[i], want)
		}
	}
	if all := RecentMailboxIDs(folder, 0); len(all) != total {
		t.Errorf("limit<=0 returned %d ids, want every one of the %d leaves", len(all), total)
	}
	if missing := RecentMailboxIDs(filepath.Join(folder, "absent"), limit); len(missing) != 0 {
		t.Errorf("missing folder returned %d ids, want none", len(missing))
	}
}

// TestRefreshRecentUnboundedLimitMatchesRefresh keeps the compatibility escape
// hatch honest for callers that still want the whole mailbox.
func TestRefreshRecentUnboundedLimitMatchesRefresh(t *testing.T) {
	humanDir := t.TempDir()
	for i := range 7 {
		writeRecentWindowMessage(t, humanDir, "inbox", i)
	}
	bounded := NewMailCache(humanDir).RefreshRecent(0)
	full := NewMailCache(humanDir).Refresh()
	if len(bounded.Messages) != len(full.Messages) {
		t.Fatalf("limit<=0 loaded %d, want the unbounded %d", len(bounded.Messages), len(full.Messages))
	}
	for i := range full.Messages {
		if bounded.Messages[i].MailboxID != full.Messages[i].MailboxID {
			t.Fatalf("entry %d = %q, want %q", i, bounded.Messages[i].MailboxID, full.Messages[i].MailboxID)
		}
	}
}
