package fs

import (
	"bytes"
	"os"
	"testing"
)

func TestDirectUnreadMarkSeenIsMonotonicAndUnionsSameTimestampIDs(t *testing.T) {
	project := t.TempDir()
	target := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
	store := mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, nil)

	latest := directUnreadIncoming(target, "latest", "latest", "2026-07-22T14:46:00Z")
	if err := store.MarkSeen(target, []MailMessage{latest}); err != nil {
		t.Fatalf("MarkSeen latest: %v", err)
	}
	older := directUnreadIncoming(target, "older", "older", "2026-07-22T14:45:00Z")
	if err := store.MarkSeen(target, []MailMessage{older}); err != nil {
		t.Fatalf("MarkSeen older: %v", err)
	}
	assertDirectUnreadCount(t, store, target, []MailMessage{latest}, 0)

	const instant = "2026-07-22T14:47:00.123456789Z"
	messageA := directUnreadIncoming(target, "mailbox-A", "legacy-A", instant)
	if err := store.MarkSeen(target, []MailMessage{messageA}); err != nil {
		t.Fatalf("MarkSeen first same-time ID: %v", err)
	}
	messageB := directUnreadIncoming(target, "mailbox-B", "legacy-B", instant)
	if err := store.MarkSeen(target, []MailMessage{messageA, messageB}); err != nil {
		t.Fatalf("MarkSeen second same-time ID: %v", err)
	}
	messageC := directUnreadIncoming(target, "mailbox-C", "legacy-C", instant)
	assertDirectUnreadCount(t, store, target, []MailMessage{messageA, messageB, messageC}, 1)
}

func TestDirectUnreadCopyOnWriteSaveFailureKeepsMemoryAndBytes(t *testing.T) {
	project := t.TempDir()
	writeDirectUnreadState(t, project, []byte(`{"version":1,"threads":{}}`))
	target := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
	baseline := directUnreadIncoming(target, "baseline", "baseline", "2026-07-22T14:44:00Z")
	store := mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, []MailMessage{baseline})

	late := directUnreadIncoming(target, "late", "late", "2026-07-22T14:45:00Z")
	assertDirectUnreadCount(t, store, target, []MailMessage{baseline, late}, 1)
	statePath := directUnreadStatePath(project)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile state before failed save: %v", err)
	}

	// writeJSONAtomic, the established JSON helper, writes statePath+".tmp" in
	// the same directory. A directory at that path makes each attempted save fail
	// without modifying the current durable file.
	if err := os.Mkdir(statePath+".tmp", 0o755); err != nil {
		t.Fatalf("Mkdir failing temp path: %v", err)
	}
	if err := store.MarkSeen(target, []MailMessage{baseline, late}); err == nil {
		t.Error("MarkSeen succeeded despite deterministic state save failure")
	}
	afterMark, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile state after failed MarkSeen: %v", err)
	}
	if !bytes.Equal(afterMark, before) {
		t.Fatalf("failed MarkSeen changed persisted bytes:\nbefore=%q\nafter=%q", before, afterMark)
	}
	assertDirectUnreadCount(t, store, target, []MailMessage{baseline, late}, 1)

	newTarget := directUnreadTarget(project, "agent-b", "agent-id-b", "project/agent-b")
	if err := store.SyncTargets([]DirectTarget{target, newTarget}, []MailMessage{baseline, late}); err == nil {
		t.Error("SyncTargets succeeded despite deterministic state save failure")
	}
	afterSync, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile state after failed SyncTargets: %v", err)
	}
	if !bytes.Equal(afterSync, before) {
		t.Fatalf("failed SyncTargets changed persisted bytes:\nbefore=%q\nafter=%q", before, afterSync)
	}

	if err := os.Remove(statePath + ".tmp"); err != nil {
		t.Fatalf("Remove failing temp path: %v", err)
	}
	newMail := directUnreadIncoming(newTarget, "new", "new", "2026-07-22T14:46:00Z")
	if _, err := store.UnreadCount(newTarget, []MailMessage{newMail}); err == nil {
		t.Error("failed SyncTargets published the new target in memory")
	}
}
