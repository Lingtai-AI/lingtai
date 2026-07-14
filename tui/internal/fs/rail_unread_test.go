package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func incomingMail(id, from, human, ts string) MailMessage {
	return MailMessage{ID: id, MailboxID: id, From: from, To: human, ReceivedAt: ts}
}

func TestRailUnreadSameTimestampLateIDAndRestart(t *testing.T) {
	projectDir := t.TempDir()
	human := "project/human"
	target := DirectTarget{Directory: filepath.Join(projectDir, ".lingtai", "agent-b"), Address: "project/agent-b"}
	first := incomingMail("id-1", target.Address, human, "2026-07-13T10:00:00Z")
	store, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, []MailMessage{first}, human)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.UnreadCount(target, []MailMessage{first}, human); got != 0 {
		t.Fatalf("startup unread = %d, want 0", got)
	}

	late := incomingMail("id-2", target.Address, human, first.ReceivedAt)
	snapshot := []MailMessage{first, late}
	if got := store.UnreadCount(target, snapshot, human); got != 1 {
		t.Fatalf("same-timestamp late-ID unread = %d, want 1", got)
	}
	if err := store.MarkSeen(target, snapshot, human); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, snapshot, human)
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.UnreadCount(target, snapshot, human); got != 0 {
		t.Fatalf("restart unread = %d, want 0", got)
	}
}

func TestRailUnreadMissingAndCorruptStateBaselineCurrentSnapshot(t *testing.T) {
	projectDir := t.TempDir()
	human := "project/human"
	target := DirectTarget{Directory: filepath.Join(projectDir, ".lingtai", "agent-b"), Address: "project/agent-b"}
	history := []MailMessage{incomingMail("old", target.Address, human, "2026-07-13T09:00:00Z")}

	store, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, history, human)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.UnreadCount(target, history, human); got != 0 {
		t.Fatalf("missing-state baseline unread = %d, want 0", got)
	}
	path := RailUnreadStatePath(projectDir)
	if err := os.WriteFile(path, []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRailUnreadStore(projectDir, []DirectTarget{target}, history, human)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.UnreadCount(target, history, human); got != 0 {
		t.Fatalf("corrupt-state baseline unread = %d, want 0", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil || persisted.Version != RailUnreadStateVersion {
		t.Fatalf("recovered state = version %d, err %v", persisted.Version, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("atomic write left temp path; stat err = %v", err)
	}
}

func TestRailUnreadIdentityChangesRebaselineAndOutgoingDoesNotCount(t *testing.T) {
	projectDir := t.TempDir()
	human := "project/human"
	dir := filepath.Join(projectDir, ".lingtai", "agent-b")
	target := DirectTarget{Directory: dir, Address: "project/agent-b"}
	store, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, nil, human)
	if err != nil {
		t.Fatal(err)
	}
	outgoing := MailMessage{ID: "out", MailboxID: "out", From: human, To: target.Address, ReceivedAt: "2026-07-13T10:00:00Z"}
	if got := store.UnreadCount(target, []MailMessage{outgoing}, human); got != 0 {
		t.Fatalf("outgoing unread = %d, want 0", got)
	}
	incoming := incomingMail("in", target.Address, human, "2026-07-13T10:01:00Z")
	if got := store.UnreadCount(target, []MailMessage{outgoing, incoming}, human); got != 1 {
		t.Fatalf("incoming unread = %d, want 1", got)
	}

	changed := DirectTarget{Directory: dir, Address: "project/agent-b-v2"}
	changedHistory := []MailMessage{incomingMail("new-address-history", changed.Address, human, "2026-07-13T10:02:00Z")}
	if err := store.SyncTargets([]DirectTarget{changed}, changedHistory, human); err != nil {
		t.Fatal(err)
	}
	if got := store.UnreadCount(changed, changedHistory, human); got != 0 {
		t.Fatalf("address-change unread = %d, want re-baselined 0", got)
	}

	if err := store.SyncTargets(nil, changedHistory, human); err != nil {
		t.Fatal(err)
	}
	reusedHistory := append(changedHistory, incomingMail("reused", changed.Address, human, "2026-07-13T10:03:00Z"))
	if err := store.SyncTargets([]DirectTarget{changed}, reusedHistory, human); err != nil {
		t.Fatal(err)
	}
	if got := store.UnreadCount(changed, reusedHistory, human); got != 0 {
		t.Fatalf("directory-reuse unread = %d, want re-baselined 0", got)
	}
}

func TestRailUnreadUnsupportedVersionBaselinesCurrentSnapshot(t *testing.T) {
	projectDir := t.TempDir()
	human := "project/human"
	target := DirectTarget{Directory: filepath.Join(projectDir, ".lingtai", "agent-b"), Address: "project/agent-b"}
	history := []MailMessage{incomingMail("old", target.Address, human, "2026-07-13T09:00:00Z")}
	path := RailUnreadStatePath(projectDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":999,"targets":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, history, human)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.UnreadCount(target, history, human); got != 0 {
		t.Fatalf("unsupported-version baseline unread = %d, want 0", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted railUnreadState
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Version != RailUnreadStateVersion {
		t.Fatalf("recovered version = %d, want %d", persisted.Version, RailUnreadStateVersion)
	}
}

func TestRailUnreadReadErrorIsNotTreatedAsMissingOrCorrupt(t *testing.T) {
	projectDir := t.TempDir()
	path := RailUnreadStatePath(projectDir)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := OpenRailUnreadStore(projectDir, nil, nil, "project/human")
	if err == nil {
		t.Fatal("OpenRailUnreadStore succeeded when the state path was unreadable")
	}
	if !strings.Contains(err.Error(), "read rail unread state") {
		t.Fatalf("error = %q, want read failure context", err)
	}
}

func TestRailUnreadNicknameRenameRetainsBoundary(t *testing.T) {
	projectDir := t.TempDir()
	human := "project/human"
	before := AgentNode{
		WorkingDir: filepath.Join(projectDir, ".lingtai", "agent-b"),
		Address:    "project/agent-b",
		Nickname:   "Before",
	}
	directTarget := func(node AgentNode) DirectTarget {
		return DirectTarget{Directory: node.WorkingDir, Address: node.Address}
	}
	target := directTarget(before)
	store, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, nil, human)
	if err != nil {
		t.Fatal(err)
	}
	incoming := incomingMail("in", target.Address, human, "2026-07-13T10:00:00Z")
	snapshot := []MailMessage{incoming}
	if got := store.UnreadCount(target, snapshot, human); got != 1 {
		t.Fatalf("pre-rename unread = %d, want 1", got)
	}

	after := before
	after.Nickname = "After"
	renamedTarget := directTarget(after)
	if err := store.SyncTargets([]DirectTarget{renamedTarget}, snapshot, human); err != nil {
		t.Fatal(err)
	}
	if got := store.UnreadCount(renamedTarget, snapshot, human); got != 1 {
		t.Fatalf("nickname-only rename unread = %d, want retained 1", got)
	}
}

func TestRailUnreadMarkSeenRequiresSyncedIdentity(t *testing.T) {
	projectDir := t.TempDir()
	human := "project/human"
	target := DirectTarget{Directory: filepath.Join(projectDir, ".lingtai", "agent-b"), Address: "project/agent-b"}
	store, err := OpenRailUnreadStore(projectDir, []DirectTarget{target}, nil, human)
	if err != nil {
		t.Fatal(err)
	}

	changed := DirectTarget{Directory: target.Directory, Address: "project/agent-b-v2"}
	if err := store.MarkSeen(changed, nil, human); err == nil {
		t.Fatal("MarkSeen accepted an address identity that had not been synchronized")
	}
	if err := store.SyncTargets([]DirectTarget{changed}, nil, human); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSeen(changed, nil, human); err != nil {
		t.Fatalf("MarkSeen rejected synchronized identity: %v", err)
	}

	unsynced := DirectTarget{Directory: filepath.Join(projectDir, ".lingtai", "agent-c"), Address: "project/agent-c"}
	if err := store.MarkSeen(unsynced, nil, human); err == nil {
		t.Fatal("MarkSeen accepted a target that had not been synchronized")
	}
}

func TestRailUnreadNicknameIndependentAddressFingerprint(t *testing.T) {
	if AddressFingerprint(" project/agent-b ") != AddressFingerprint("project/agent-b") {
		t.Fatal("address fingerprint should normalize surrounding whitespace")
	}
	if AddressFingerprint("project/agent-b") == AddressFingerprint("project/agent-c") {
		t.Fatal("different addresses must have different fingerprints")
	}
}
