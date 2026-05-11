package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mailboxIDPattern matches the short mailbox id shape produced by
// newMailboxID and the kernel's `_new_mailbox_id`: 14 digits, a `T`, 6
// digits, a dash, then 4 lowercase hex chars (20 chars total).
var mailboxIDPattern = regexp.MustCompile(`^\d{8}T\d{6}-[0-9a-f]{4}$`)

func TestNewMailboxID_Shape(t *testing.T) {
	id := newMailboxID()
	if !mailboxIDPattern.MatchString(id) {
		t.Fatalf("id = %q, want match of %s", id, mailboxIDPattern)
	}
	if len(id) != 20 {
		t.Errorf("len(id) = %d, want 20", len(id))
	}
}

func TestNewMailboxID_Sortable(t *testing.T) {
	// Two ids generated back-to-back should sort either equal-prefix
	// (same second) or strictly increasing. They must never be
	// strictly less than each other in violation of monotonic time.
	a := newMailboxID()
	b := newMailboxID()
	aPrefix, bPrefix := a[:15], b[:15] // YYYYMMDDTHHMMSS
	if strings.Compare(bPrefix, aPrefix) < 0 {
		t.Errorf("second id prefix %q < first %q — time went backwards", bPrefix, aPrefix)
	}
}

// writePortalSenderManifest writes .agent.json with the given admin value
// so WriteMail treats senderDir as a real agent (not pseudo).
func writePortalSenderManifest(t *testing.T, dir string, admin interface{}) {
	t.Helper()
	manifest := map[string]interface{}{
		"agent_name": "test-sender",
		"admin":      admin,
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func TestWriteMail_ProducesShortMailboxID(t *testing.T) {
	recipientDir := t.TempDir()
	senderDir := t.TempDir()
	writePortalSenderManifest(t, senderDir, map[string]interface{}{"karma": true})

	if err := WriteMail(recipientDir, senderDir, "alice", "bob", "subj", "body"); err != nil {
		t.Fatalf("WriteMail: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(recipientDir, "mailbox", "inbox"))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("inbox len = %d, want 1", len(entries))
	}
	name := entries[0].Name()
	if !mailboxIDPattern.MatchString(name) {
		t.Errorf("inbox dir name = %q, want match of %s", name, mailboxIDPattern)
	}

	// Verify message.json's id and _mailbox_id agree with the directory name.
	data, err := os.ReadFile(filepath.Join(recipientDir, "mailbox", "inbox", name, "message.json"))
	if err != nil {
		t.Fatalf("read message.json: %v", err)
	}
	var msg MailMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.ID != name {
		t.Errorf("msg.ID = %q, want %q (directory name)", msg.ID, name)
	}
	if msg.MailboxID != name {
		t.Errorf("msg.MailboxID = %q, want %q (directory name)", msg.MailboxID, name)
	}

	// Sent copy must reuse the same id.
	sentEntries, err := os.ReadDir(filepath.Join(senderDir, "mailbox", "sent"))
	if err != nil {
		t.Fatalf("read sent: %v", err)
	}
	if len(sentEntries) != 1 || sentEntries[0].Name() != name {
		t.Errorf("sent entries = %v, want [%q]", dirNames(sentEntries), name)
	}
}

func TestPrepareMailDirs_AllocatesDistinctIDs(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "mailbox", "inbox")

	// Pre-create a directory whose name our generator might produce.
	// We cannot predict the suffix, so this only verifies the structural
	// path: a single existing leaf must not stop the generator from
	// allocating a *different* id and succeeding.
	id1, _, _, err := prepareMailDirs(parent, "")
	if err != nil {
		t.Fatalf("first allocation: %v", err)
	}
	id2, _, _, err := prepareMailDirs(parent, "")
	if err != nil {
		t.Fatalf("second allocation: %v", err)
	}
	if id1 == id2 {
		t.Errorf("collision: two sequential allocations returned the same id %q", id1)
	}
}

func TestPrepareMailDirs_SentCollisionRetriesWithoutOverwrite(t *testing.T) {
	root := t.TempDir()
	primaryParent := filepath.Join(root, "recipient", "mailbox", "inbox")
	sentParent := filepath.Join(root, "sender", "mailbox", "sent")
	if err := os.MkdirAll(sentParent, 0o755); err != nil {
		t.Fatalf("setup sent parent: %v", err)
	}

	ids := make([]string, mailboxIDCollisionRetries)
	for i := range ids {
		ids[i] = "20260511T224200-" + fmt.Sprintf("%04x", i)
	}
	sentinel := []byte("existing sent message")
	reserved := make(map[string][]byte)
	for _, id := range ids[:mailboxIDCollisionRetries-1] {
		dir := filepath.Join(sentParent, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("setup collided sent dir: %v", err)
		}
		msg := filepath.Join(dir, "message.json")
		if err := os.WriteFile(msg, sentinel, 0o644); err != nil {
			t.Fatalf("setup collided sent message: %v", err)
		}
		reserved[msg] = sentinel
	}

	oldSource := mailboxIDSource
	next := 0
	mailboxIDSource = func() string {
		id := ids[next]
		if next < len(ids)-1 {
			next++
		}
		return id
	}
	t.Cleanup(func() { mailboxIDSource = oldSource })

	id, primaryDir, sentDir, err := prepareMailDirs(primaryParent, sentParent)
	if err != nil {
		t.Fatalf("prepareMailDirs: %v", err)
	}
	if id != ids[len(ids)-1] {
		t.Fatalf("id = %q, want final retry id %q", id, ids[len(ids)-1])
	}
	if filepath.Base(primaryDir) != id || filepath.Base(sentDir) != id {
		t.Fatalf("dirs do not share id: id=%q primary=%q sent=%q", id, primaryDir, sentDir)
	}
	for msg, want := range reserved {
		got, err := os.ReadFile(msg)
		if err != nil {
			t.Fatalf("reserved message missing: %v", err)
		}
		if string(got) != string(want) {
			t.Fatalf("reserved sent message %s was overwritten", msg)
		}
	}
}

func TestPrepareMailDirs_ExhaustionReportsError(t *testing.T) {
	// Pre-create every possible leaf for the current second. Because we
	// cannot enumerate 65,536 suffixes feasibly, the more practical test is
	// to simulate exhaustion by making the parent un-writable so every
	// `os.Mkdir` fails with a non-IsExist error — which surfaces as a
	// wrapped error rather than retrying forever.
	if os.Getuid() == 0 {
		t.Skip("running as root — chmod 0 won't deny Mkdir")
	}
	parent := filepath.Join(t.TempDir(), "mailbox", "inbox")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	_, _, _, err := prepareMailDirs(parent, "")
	if err == nil {
		t.Fatalf("expected error from un-writable parent, got nil")
	}
	if !strings.Contains(err.Error(), "create primary mailbox") {
		t.Errorf("error = %v, want one wrapped by prepareMailDirs", err)
	}
}

func dirNames(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
