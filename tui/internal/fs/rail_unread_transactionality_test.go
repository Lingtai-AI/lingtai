package fs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRailUnreadMarkSeenWriteFailureKeepsMemoryUnadvanced(t *testing.T) {
	projectDir := t.TempDir()
	human := "project/human"
	target := DirectTarget{
		Directory: filepath.Join(projectDir, ".lingtai", "agent-b"),
		Address:   "project/agent-b",
	}
	store, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, nil, human)
	if err != nil {
		t.Fatal(err)
	}

	message := incomingMail("unread", target.Address, human, "2026-07-15T01:00:00Z")
	snapshot := []MailMessage{message}
	if got := store.UnreadCount(target, snapshot, human); got != 1 {
		t.Fatalf("precondition unread = %d, want 1", got)
	}

	persistedPath := store.path
	blockingFile := filepath.Join(projectDir, "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("block nested state write"), 0o644); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(blockingFile, "rail-last-seen.json")
	if err := store.MarkSeen(target, snapshot, human); err == nil {
		t.Fatal("MarkSeen succeeded through a non-directory parent, want a persistence error")
	}

	if got := store.UnreadCount(target, snapshot, human); got != 1 {
		t.Fatalf("in-memory unread after failed MarkSeen = %d, want 1", got)
	}

	store.path = persistedPath
	restarted, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, snapshot, human)
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.UnreadCount(target, snapshot, human); got != 1 {
		t.Fatalf("persisted unread after failed MarkSeen = %d, want 1", got)
	}
}

func TestRailUnreadSyncTargetsWriteFailureKeepsMemoryUnchanged(t *testing.T) {
	projectDir := t.TempDir()
	human := "project/human"
	target := DirectTarget{
		Directory: filepath.Join(projectDir, ".lingtai", "agent-b"),
		Address:   "project/agent-b",
	}
	store, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, nil, human)
	if err != nil {
		t.Fatal(err)
	}

	oldMessage := incomingMail("old-unread", target.Address, human, "2026-07-15T02:00:00Z")
	oldSnapshot := []MailMessage{oldMessage}
	if got := store.UnreadCount(target, oldSnapshot, human); got != 1 {
		t.Fatalf("precondition old-target unread = %d, want 1", got)
	}
	before := railUnreadState{
		Version: store.state.Version,
		Targets: make(map[string]unreadTargetState, len(store.state.Targets)),
	}
	for key, state := range store.state.Targets {
		before.Targets[key] = state
	}

	changed := DirectTarget{Directory: target.Directory, Address: "project/agent-b-v2"}
	changedHistory := append(oldSnapshot,
		incomingMail("changed-history", changed.Address, human, "2026-07-15T02:01:00Z"),
	)
	persistedPath := store.path
	blockingFile := filepath.Join(projectDir, "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("block nested state write"), 0o644); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(blockingFile, "rail-last-seen.json")
	if err := store.SyncTargets([]DirectTarget{changed}, changedHistory, human); err == nil {
		t.Fatal("SyncTargets succeeded through a non-directory parent, want a persistence error")
	}

	if got := store.UnreadCount(target, oldSnapshot, human); got != 1 {
		t.Fatalf("in-memory old-target unread after failed SyncTargets = %d, want 1", got)
	}
	if !reflect.DeepEqual(store.state, before) {
		t.Fatalf("in-memory state changed after failed SyncTargets:\n got: %#v\nwant: %#v", store.state, before)
	}

	store.path = persistedPath
	restarted, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, oldSnapshot, human)
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.UnreadCount(target, oldSnapshot, human); got != 1 {
		t.Fatalf("persisted old-target unread after failed SyncTargets = %d, want 1", got)
	}
}
