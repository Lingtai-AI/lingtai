package fs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const directUnreadHuman = "project/human"

func directUnreadTarget(project, directory, agentID, address string) DirectTarget {
	return DirectTarget{
		ProjectDirectory: project,
		Directory:        filepath.Join(project, ".lingtai", directory),
		AgentID:          agentID,
		Address:          address,
	}
}

func directUnreadIncoming(target DirectTarget, mailboxID, legacyID, receivedAt string) MailMessage {
	return MailMessage{
		MailboxID:  mailboxID,
		ID:         legacyID,
		From:       target.Address,
		To:         directUnreadHuman,
		ReceivedAt: receivedAt,
		Identity:   map[string]interface{}{"agent_id": target.AgentID},
	}
}

func directUnreadStatePath(project string) string {
	return filepath.Join(project, ".lingtai", ".tui-asset", "direct-unread.json")
}

func writeDirectUnreadState(t *testing.T, project string, contents []byte) string {
	t.Helper()
	path := directUnreadStatePath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll state parent: %v", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("WriteFile state: %v", err)
	}
	return path
}

func mustOpenDirectUnreadStore(t *testing.T, project string, targets []DirectTarget, accepted []MailMessage) *DirectUnreadStore {
	t.Helper()
	store, err := OpenDirectUnreadStore(project, directUnreadHuman, targets, accepted)
	if err != nil {
		t.Fatalf("OpenDirectUnreadStore: %v", err)
	}
	if store == nil {
		t.Fatal("OpenDirectUnreadStore returned nil store")
	}
	return store
}

func assertDirectUnreadCount(t *testing.T, store *DirectUnreadStore, target DirectTarget, accepted []MailMessage, want int) {
	t.Helper()
	got, err := store.UnreadCount(target, accepted)
	if err != nil {
		t.Errorf("UnreadCount: %v", err)
		return
	}
	if got != want {
		t.Errorf("UnreadCount = %d, want %d", got, want)
	}
}

func TestDirectUnreadSameTimestampLateIDAndRestart(t *testing.T) {
	project := t.TempDir()
	target := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
	const instant = "2026-07-22T14:44:00.123456789Z"

	messageA := directUnreadIncoming(target, "mailbox-A", "legacy-A", instant)
	store := mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, []MailMessage{messageA})
	if got, err := store.UnreadCount(target, []MailMessage{messageA}); err != nil || got != 0 {
		t.Fatalf("baseline accepted history UnreadCount = %d, %v; want 0, nil", got, err)
	}

	messageB := directUnreadIncoming(target, "mailbox-B", "legacy-B", instant)
	acceptedAB := []MailMessage{messageA, messageB}
	if got, err := store.UnreadCount(target, acceptedAB); err != nil {
		t.Fatalf("UnreadCount after same-timestamp late ID: %v", err)
	} else if got != 1 {
		t.Fatalf("same-timestamp late ID unread count: got %d want 1", got)
	}

	if err := store.MarkSeen(target, acceptedAB); err != nil {
		t.Fatalf("MarkSeen: %v", err)
	}
	reopened := mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, acceptedAB)
	messageC := directUnreadIncoming(target, "mailbox-C", "legacy-C", instant)
	assertDirectUnreadCount(t, reopened, target, []MailMessage{messageA, messageB, messageC}, 1)
}

func TestDirectUnreadStableAgentIDSurvivesRouteRenameAndSeparatesProjectsAndIDs(t *testing.T) {
	project := t.TempDir()
	original := directUnreadTarget(project, "agent-old", "agent-id-a", "project/agent-old")
	oldMail := directUnreadIncoming(original, "old", "old", "2026-07-22T14:44:00Z")
	store := mustOpenDirectUnreadStore(t, project, []DirectTarget{original}, []MailMessage{oldMail})
	if err := store.MarkSeen(original, []MailMessage{oldMail}); err != nil {
		t.Fatalf("MarkSeen old route: %v", err)
	}

	renamed := directUnreadTarget(project, "agent-new", original.AgentID, "project/agent-new")
	if err := store.SyncTargets([]DirectTarget{renamed}, []MailMessage{oldMail}); err != nil {
		t.Fatalf("SyncTargets renamed route: %v", err)
	}
	newRouteMail := directUnreadIncoming(renamed, "new-route", "new-route", "2026-07-22T14:45:00Z")
	assertDirectUnreadCount(t, store, renamed, []MailMessage{oldMail, newRouteMail}, 1)

	otherID := directUnreadTarget(project, "agent-b", "agent-id-b", "project/agent-b")
	if err := store.SyncTargets([]DirectTarget{renamed, otherID}, nil); err != nil {
		t.Fatalf("SyncTargets other stable ID: %v", err)
	}
	otherIDMail := directUnreadIncoming(otherID, "other-id", "other-id", "2026-07-22T14:46:00Z")
	assertDirectUnreadCount(t, store, otherID, []MailMessage{otherIDMail}, 1)
	assertDirectUnreadCount(t, store, renamed, []MailMessage{otherIDMail}, 0)

	otherProject := t.TempDir()
	otherProjectTarget := directUnreadTarget(otherProject, "agent-new", renamed.AgentID, renamed.Address)
	otherStore := mustOpenDirectUnreadStore(t, otherProject, []DirectTarget{otherProjectTarget}, nil)
	otherProjectMail := directUnreadIncoming(otherProjectTarget, "other-project", "other-project", "2026-07-22T14:47:00Z")
	assertDirectUnreadCount(t, otherStore, otherProjectTarget, []MailMessage{otherProjectMail}, 1)
}

func TestDirectUnreadPersistsExactAgentIDAndRebaselinesMismatchedIntegrity(t *testing.T) {
	project := t.TempDir()
	target := directUnreadTarget(project, "agent-a", " agent-id-a ", "project/agent-a")
	key := DirectThreadKey(target)
	if key == "" {
		t.Fatal("DirectThreadKey rejected a nonblank literal AgentID")
	}

	mismatched := map[string]interface{}{
		"version": 1,
		"threads": map[string]interface{}{
			key: map[string]interface{}{
				"agent_id":           "different-agent-id",
				"received_at":        "2026-07-22T14:44:00Z",
				"ids_at_received_at": []string{"old"},
			},
		},
	}
	encoded, err := json.Marshal(mismatched)
	if err != nil {
		t.Fatalf("Marshal mismatched state: %v", err)
	}
	writeDirectUnreadState(t, project, encoded)

	baseline := directUnreadIncoming(target, "baseline", "baseline", "2026-07-22T14:45:00Z")
	store := mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, []MailMessage{baseline})
	assertDirectUnreadCount(t, store, target, []MailMessage{baseline}, 0)

	persisted, err := os.ReadFile(directUnreadStatePath(project))
	if err != nil {
		t.Fatalf("ReadFile rebased state: %v", err)
	}
	var decoded struct {
		Version int `json:"version"`
		Threads map[string]struct {
			AgentID string `json:"agent_id"`
		} `json:"threads"`
	}
	if err := json.Unmarshal(persisted, &decoded); err != nil {
		t.Fatalf("Unmarshal rebased state: %v", err)
	}
	if decoded.Version != 1 {
		t.Fatalf("persisted version = %d, want 1", decoded.Version)
	}
	entry, ok := decoded.Threads[key]
	if !ok {
		t.Fatalf("persisted threads missing stable key %q: %#v", key, decoded.Threads)
	}
	if entry.AgentID != target.AgentID {
		t.Fatalf("persisted agent_id = %q, want exact literal %q", entry.AgentID, target.AgentID)
	}
}

func TestDirectUnreadOpenDoesNotRebaselineExistingStableThread(t *testing.T) {
	project := t.TempDir()
	target := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
	baseline := directUnreadIncoming(target, "baseline", "baseline", "2026-07-22T14:44:00Z")
	_ = mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, []MailMessage{baseline})

	unresolved := directUnreadIncoming(target, "invalid-time", "invalid-time", "not-rfc3339")
	reopened, err := OpenDirectUnreadStore(project, directUnreadHuman, []DirectTarget{target}, []MailMessage{baseline, unresolved})
	if err != nil {
		t.Fatalf("reopen existing stable thread re-evaluated current accepted mail: %v", err)
	}
	if _, err := reopened.UnreadCount(target, []MailMessage{baseline, unresolved}); err == nil {
		t.Fatal("UnreadCount accepted unresolved current mail after a successful no-rebaseline reopen")
	}
}

func TestDirectUnreadBaselinesMissingMalformedAndUnsupportedStateButReturnsReadFailures(t *testing.T) {
	stateCases := []struct {
		name     string
		contents []byte
	}{
		{name: "missing"},
		{name: "malformed JSON", contents: []byte("{not JSON")},
		{name: "unsupported version", contents: []byte(`{"version":999,"threads":{}}`)},
	}

	for _, tc := range stateCases {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			target := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
			if tc.contents != nil {
				writeDirectUnreadState(t, project, tc.contents)
			}
			history := directUnreadIncoming(target, "history", "history", "2026-07-22T14:44:00Z")
			store := mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, []MailMessage{history})
			assertDirectUnreadCount(t, store, target, []MailMessage{history}, 0)
			late := directUnreadIncoming(target, "late", "late", "2026-07-22T14:45:00Z")
			assertDirectUnreadCount(t, store, target, []MailMessage{history, late}, 1)
		})
	}

	t.Run("non IsNotExist state read failure is returned", func(t *testing.T) {
		project := t.TempDir()
		if err := os.MkdirAll(directUnreadStatePath(project), 0o755); err != nil {
			t.Fatalf("MkdirAll state-path directory blocker: %v", err)
		}
		target := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
		store, err := OpenDirectUnreadStore(project, directUnreadHuman, []DirectTarget{target}, nil)
		if err == nil {
			t.Fatalf("OpenDirectUnreadStore returned store %#v without surfacing a state-path directory read failure", store)
		}
	})
}

func TestDirectUnreadCountsOnlyStrictIncomingDirectMail(t *testing.T) {
	project := t.TempDir()
	target := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
	store := mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, nil)
	const instant = "2026-07-22T14:44:00Z"

	valid := directUnreadIncoming(target, "valid", "valid", instant)
	outgoing := valid
	outgoing.MailboxID, outgoing.ID = "outgoing", "outgoing"
	outgoing.From, outgoing.To = directUnreadHuman, target.Address
	group := valid
	group.MailboxID, group.ID = "group", "group"
	group.To = []string{directUnreadHuman, "project/other"}
	cc := valid
	cc.MailboxID, cc.ID = "cc", "cc"
	cc.CC = []string{"project/other"}
	identityConflict := valid
	identityConflict.MailboxID, identityConflict.ID = "conflict", "conflict"
	identityConflict.Identity = map[string]interface{}{"agent_id": "other-agent-id"}

	for _, tc := range []struct {
		name string
		mail MailMessage
		want int
	}{
		{name: "valid strict incoming", mail: valid, want: 1},
		{name: "outgoing", mail: outgoing, want: 0},
		{name: "group", mail: group, want: 0},
		{name: "CC", mail: cc, want: 0},
		{name: "identity conflict", mail: identityConflict, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertDirectUnreadCount(t, store, target, []MailMessage{tc.mail}, tc.want)
		})
	}
}

func TestDirectUnreadRejectsUnresolvedIncomingWithoutChangingBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		mail func(DirectTarget) MailMessage
	}{
		{
			name: "invalid timestamp",
			mail: func(target DirectTarget) MailMessage {
				return directUnreadIncoming(target, "invalid-time", "invalid-time", "not-rfc3339")
			},
		},
		{
			name: "blank mailbox and legacy IDs",
			mail: func(target DirectTarget) MailMessage {
				return directUnreadIncoming(target, " ", "", "2026-07-22T14:45:00Z")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			writeDirectUnreadState(t, project, []byte(`{"version":1,"threads":{}}`))
			target := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
			baseline := directUnreadIncoming(target, "baseline", "baseline", "2026-07-22T14:44:00Z")
			store := mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, []MailMessage{baseline})
			before, err := os.ReadFile(directUnreadStatePath(project))
			if err != nil {
				t.Fatalf("ReadFile before MarkSeen: %v", err)
			}
			unresolved := tc.mail(target)
			if _, err := store.UnreadCount(target, []MailMessage{baseline, unresolved}); err == nil {
				t.Error("UnreadCount accepted unresolved incoming mail instead of returning an error")
			}
			if err := store.MarkSeen(target, []MailMessage{baseline, unresolved}); err == nil {
				t.Error("MarkSeen accepted unresolved incoming mail instead of returning an error")
			}
			after, err := os.ReadFile(directUnreadStatePath(project))
			if err != nil {
				t.Fatalf("ReadFile after MarkSeen: %v", err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("MarkSeen changed state bytes for unresolved mail:\nbefore=%q\nafter=%q", before, after)
			}
		})
	}
}

func TestDirectUnreadUsesMailboxIDThenLegacyIDAndParsedTime(t *testing.T) {
	t.Run("same timestamp uses the effective ID set", func(t *testing.T) {
		project := t.TempDir()
		target := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
		const instant = "2026-07-22T14:44:00.123456789Z"
		baseline := directUnreadIncoming(target, "mailbox-A", "legacy-A", instant)
		store := mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, []MailMessage{baseline})

		duplicateMailbox := directUnreadIncoming(target, "mailbox-A", "different-legacy", instant)
		mailboxPreferred := directUnreadIncoming(target, "mailbox-B", "legacy-A", instant)
		legacyFallback := directUnreadIncoming(target, "", "legacy-only", instant)
		assertDirectUnreadCount(t, store, target, []MailMessage{baseline, duplicateMailbox, mailboxPreferred, legacyFallback}, 2)
	})

	t.Run("timestamps are parsed rather than lexicographically compared", func(t *testing.T) {
		project := t.TempDir()
		target := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
		// The baseline's local date sorts after the later message's local date,
		// but 23:45Z is fifteen minutes after 00:30+01:00 (23:30Z).
		baseline := directUnreadIncoming(target, "early", "early", "2026-01-01T00:30:00+01:00")
		store := mustOpenDirectUnreadStore(t, project, []DirectTarget{target}, []MailMessage{baseline})
		later := directUnreadIncoming(target, "later", "later", "2025-12-31T23:45:00Z")
		assertDirectUnreadCount(t, store, target, []MailMessage{baseline, later}, 1)
	})
}

func TestDirectUnreadSyncRejectsInvalidKeysAtomicallyAddsAndDoesNotPrune(t *testing.T) {
	project := t.TempDir()
	writeDirectUnreadState(t, project, []byte(`{"version":1,"threads":{}}`))
	targetA := directUnreadTarget(project, "agent-a", "agent-id-a", "project/agent-a")
	store := mustOpenDirectUnreadStore(t, project, []DirectTarget{targetA}, nil)
	before, err := os.ReadFile(directUnreadStatePath(project))
	if err != nil {
		t.Fatalf("ReadFile before invalid sync: %v", err)
	}

	blank := targetA
	blank.AgentID = " \t"
	if err := store.SyncTargets([]DirectTarget{targetA, blank}, nil); err == nil {
		t.Error("SyncTargets accepted a blank stable AgentID")
	}
	afterBlank, err := os.ReadFile(directUnreadStatePath(project))
	if err != nil {
		t.Fatalf("ReadFile after blank sync: %v", err)
	}
	if !bytes.Equal(afterBlank, before) {
		t.Error("blank-key SyncTargets changed persisted bytes")
	}

	duplicate := targetA
	duplicate.Address = "project/agent-a-renamed"
	if err := store.SyncTargets([]DirectTarget{targetA, duplicate}, nil); err == nil {
		t.Error("SyncTargets accepted duplicate stable thread keys")
	}
	afterDuplicate, err := os.ReadFile(directUnreadStatePath(project))
	if err != nil {
		t.Fatalf("ReadFile after duplicate sync: %v", err)
	}
	if !bytes.Equal(afterDuplicate, before) {
		t.Error("duplicate-key SyncTargets changed persisted bytes")
	}

	targetB := directUnreadTarget(project, "agent-b", "agent-id-b", "project/agent-b")
	if err := store.SyncTargets([]DirectTarget{targetB}, nil); err != nil {
		t.Fatalf("SyncTargets new stable key: %v", err)
	}
	mailForA := directUnreadIncoming(targetA, "after-absence", "after-absence", "2026-07-22T14:45:00Z")
	mailForB := directUnreadIncoming(targetB, "after-add", "after-add", "2026-07-22T14:46:00Z")
	assertDirectUnreadCount(t, store, targetA, []MailMessage{mailForA}, 1)
	assertDirectUnreadCount(t, store, targetB, []MailMessage{mailForB}, 1)
}
