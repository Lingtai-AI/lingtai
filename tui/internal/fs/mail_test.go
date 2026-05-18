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

func TestReadInbox(t *testing.T) {
	dir := t.TempDir()
	inbox := filepath.Join(dir, "mailbox", "inbox", "msg-001")
	os.MkdirAll(inbox, 0o755)

	msg := MailMessage{
		ID: "msg-001", MailboxID: "msg-001", From: "/agents/alice",
		To: "/agents/human", Subject: "hello", Message: "hi there",
		Type: "normal", ReceivedAt: "2026-03-25T12:00:00.000Z",
	}
	data, _ := json.Marshal(msg)
	os.WriteFile(filepath.Join(inbox, "message.json"), data, 0o644)

	messages, err := ReadInbox(dir)
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("inbox len = %d, want 1", len(messages))
	}
	if messages[0].Subject != "hello" {
		t.Errorf("subject = %q, want %q", messages[0].Subject, "hello")
	}
}

func TestWriteMail(t *testing.T) {
	recipientDir := t.TempDir()
	os.MkdirAll(filepath.Join(recipientDir, "mailbox", "inbox"), 0o755)
	senderDir := t.TempDir()
	os.MkdirAll(filepath.Join(senderDir, "mailbox", "sent"), 0o755)
	writeSenderManifest(t, senderDir, map[string]interface{}{"karma": true})

	err := WriteMail(recipientDir, senderDir, "/sender/human", "/recipient/alice", "test subject", "test body")
	if err != nil {
		t.Fatalf("write mail: %v", err)
	}

	messages, err := ReadInbox(recipientDir)
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("inbox len = %d, want 1", len(messages))
	}
	if messages[0].Message != "test body" {
		t.Errorf("message = %q, want %q", messages[0].Message, "test body")
	}
	if messages[0].From != "/sender/human" {
		t.Errorf("from = %q, want %q", messages[0].From, "/sender/human")
	}

	sent, err := ReadSent(senderDir)
	if err != nil {
		t.Fatalf("read sent: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("sent len = %d, want 1", len(sent))
	}

	// `to` field must be written as []string, not string, so downstream
	// displays (and the kernel reader) see a uniform list shape.
	toSlice, ok := messages[0].To.([]interface{})
	if !ok {
		t.Fatalf("to type = %T, want []interface{} (list[str] on disk)", messages[0].To)
	}
	if len(toSlice) != 1 || toSlice[0] != "/recipient/alice" {
		t.Errorf("to = %#v, want []interface{}{\"/recipient/alice\"}", toSlice)
	}
}

func TestWriteMail_LocalDelivery(t *testing.T) {
	recipientDir := t.TempDir()
	os.MkdirAll(filepath.Join(recipientDir, "mailbox", "inbox"), 0o755)
	senderDir := t.TempDir()
	os.MkdirAll(filepath.Join(senderDir, "mailbox", "sent"), 0o755)
	writeSenderManifest(t, senderDir, map[string]interface{}{"karma": true})

	err := WriteMail(recipientDir, senderDir, "human", "agent_a", "hi", "hello")
	if err != nil {
		t.Fatalf("WriteMail: %v", err)
	}

	msgs, _ := ReadInbox(recipientDir)
	if len(msgs) != 1 {
		t.Fatalf("inbox len = %d, want 1", len(msgs))
	}

	outboxDir := filepath.Join(senderDir, "mailbox", "outbox")
	entries, err := os.ReadDir(outboxDir)
	if err == nil && len(entries) > 0 {
		t.Errorf("outbox should be empty for local delivery, got %d entries", len(entries))
	}
}

func TestWriteMail_RemoteRoutesToOutbox(t *testing.T) {
	senderDir := t.TempDir()
	os.MkdirAll(filepath.Join(senderDir, "mailbox", "sent"), 0o755)
	os.MkdirAll(filepath.Join(senderDir, "mailbox", "outbox"), 0o755)
	writeSenderManifest(t, senderDir, map[string]interface{}{"karma": true})

	remoteAddr := "[2001:db8::1]:/home/user/.lingtai/agent_b"
	err := WriteMail("", senderDir, "human", remoteAddr, "hello", "across the internet")
	if err != nil {
		t.Fatalf("WriteMail: %v", err)
	}

	outboxDir := filepath.Join(senderDir, "mailbox", "outbox")
	entries, err := os.ReadDir(outboxDir)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("outbox len = %d, want 1", len(entries))
	}

	msgPath := filepath.Join(outboxDir, entries[0].Name(), "message.json")
	data, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatalf("read outbox message: %v", err)
	}
	var msg MailMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Message != "across the internet" {
		t.Errorf("message = %q, want %q", msg.Message, "across the internet")
	}
	toSlice, ok := msg.To.([]interface{})
	if !ok {
		t.Fatalf("to type = %T, want []interface{}", msg.To)
	}
	if len(toSlice) != 1 || toSlice[0] != remoteAddr {
		t.Errorf("to = %v, want [%q]", toSlice, remoteAddr)
	}

	sent, err := ReadSent(senderDir)
	if err != nil {
		t.Fatalf("read sent: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("sent len = %d, want 1", len(sent))
	}
}

func TestReadInbox_Empty(t *testing.T) {
	dir := t.TempDir()
	messages, err := ReadInbox(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("expected empty inbox, got %d", len(messages))
	}
}

func TestWriteMail_PseudoAgentSenderWritesOnlyToOutbox(t *testing.T) {
	senderDir := t.TempDir()
	recipientDir := t.TempDir()

	// Pseudo-agent sender: .agent.json has admin: null.
	manifest := map[string]interface{}{
		"agent_name": "human",
		"admin":      nil,
	}
	manifestBytes, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(senderDir, ".agent.json"), manifestBytes, 0o644)

	err := WriteMail(recipientDir, senderDir, "localhost:"+senderDir, "localhost:"+recipientDir, "hi", "hello")
	if err != nil {
		t.Fatalf("WriteMail: %v", err)
	}

	// Outbox MUST contain the message.
	outboxEntries, err := os.ReadDir(filepath.Join(senderDir, "mailbox", "outbox"))
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if len(outboxEntries) != 1 {
		t.Fatalf("outbox len = %d, want 1", len(outboxEntries))
	}

	// Inbox MUST be empty (no direct-delivery).
	inboxDir := filepath.Join(recipientDir, "mailbox", "inbox")
	inboxEntries, _ := os.ReadDir(inboxDir)
	if len(inboxEntries) != 0 {
		t.Errorf("recipient inbox len = %d, want 0 (pseudo-agent sends skip direct delivery)", len(inboxEntries))
	}

	// Sent MUST be empty (no sent-at-send-time copy for pseudo-agents).
	sentDir := filepath.Join(senderDir, "mailbox", "sent")
	sentEntries, _ := os.ReadDir(sentDir)
	if len(sentEntries) != 0 {
		t.Errorf("sender sent len = %d, want 0 (recipient produces sent on pickup)", len(sentEntries))
	}
}

func TestWriteMail_RealAgentSenderUnchanged(t *testing.T) {
	senderDir := t.TempDir()
	recipientDir := t.TempDir()

	// Real agent sender: admin is a non-nil map.
	manifest := map[string]interface{}{
		"agent_name": "alice",
		"admin":      map[string]interface{}{"karma": true},
	}
	manifestBytes, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(senderDir, ".agent.json"), manifestBytes, 0o644)

	err := WriteMail(recipientDir, senderDir, "alice", "bob", "hi", "hello")
	if err != nil {
		t.Fatalf("WriteMail: %v", err)
	}

	// Inbox MUST contain the message (local-delivery path unchanged).
	inboxEntries, err := os.ReadDir(filepath.Join(recipientDir, "mailbox", "inbox"))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(inboxEntries) != 1 {
		t.Fatalf("inbox len = %d, want 1", len(inboxEntries))
	}

	// Sent MUST contain the copy (sent-at-send-time behavior unchanged for real agents).
	sentEntries, err := os.ReadDir(filepath.Join(senderDir, "mailbox", "sent"))
	if err != nil {
		t.Fatalf("read sent: %v", err)
	}
	if len(sentEntries) != 1 {
		t.Errorf("sent len = %d, want 1 (real agents still write sent on send)", len(sentEntries))
	}
}

func TestMailCache_ScansOutboxWithUndelivered(t *testing.T) {
	humanDir := t.TempDir()
	outboxDir := filepath.Join(humanDir, "mailbox", "outbox", "msg-out-1")
	os.MkdirAll(outboxDir, 0o755)

	msg := MailMessage{
		ID: "msg-out-1", MailboxID: "msg-out-1",
		From: "human", To: []string{"alice"}, Subject: "pending", Message: "hi",
		Type: "normal", ReceivedAt: "2026-04-21T10:00:00.000Z",
	}
	data, _ := json.Marshal(msg)
	os.WriteFile(filepath.Join(outboxDir, "message.json"), data, 0o644)

	cache := NewMailCache(humanDir).Refresh()
	if len(cache.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1 (outbox not scanned?)", len(cache.Messages))
	}
	if cache.Messages[0].Delivered {
		t.Errorf("Delivered = true, want false (message is in outbox, not yet picked up)")
	}
}

func TestMailCache_FlipsDeliveredOnOutboxToSentTransition(t *testing.T) {
	humanDir := t.TempDir()
	outboxMsgDir := filepath.Join(humanDir, "mailbox", "outbox", "msg-transit-1")
	os.MkdirAll(outboxMsgDir, 0o755)

	msg := MailMessage{
		ID: "msg-transit-1", MailboxID: "msg-transit-1",
		From: "human", To: []string{"alice"}, Subject: "in-transit", Message: "hi",
		Type: "normal", ReceivedAt: "2026-04-21T10:00:00.000Z",
	}
	data, _ := json.Marshal(msg)
	os.WriteFile(filepath.Join(outboxMsgDir, "message.json"), data, 0o644)

	// First refresh: message is in outbox, Delivered=false.
	cache := NewMailCache(humanDir).Refresh()
	if len(cache.Messages) != 1 {
		t.Fatalf("first refresh: len = %d, want 1", len(cache.Messages))
	}
	if cache.Messages[0].Delivered {
		t.Fatalf("first refresh: Delivered = true, want false")
	}

	// Simulate recipient pickup: atomic move from outbox to sent.
	sentMsgDir := filepath.Join(humanDir, "mailbox", "sent", "msg-transit-1")
	os.MkdirAll(filepath.Join(humanDir, "mailbox", "sent"), 0o755)
	if err := os.Rename(outboxMsgDir, sentMsgDir); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Second refresh: same UUID now in sent, Delivered must flip to true,
	// and there must NOT be a duplicate entry.
	cache = cache.Refresh()
	if len(cache.Messages) != 1 {
		t.Fatalf("after transition: len = %d, want 1 (no duplicate)", len(cache.Messages))
	}
	if !cache.Messages[0].Delivered {
		t.Errorf("after transition: Delivered = false, want true")
	}
}

func TestMailCache_InboxAndSentDeliveredTrue(t *testing.T) {
	humanDir := t.TempDir()

	inboxDir := filepath.Join(humanDir, "mailbox", "inbox", "in-1")
	os.MkdirAll(inboxDir, 0o755)
	inMsg := MailMessage{ID: "in-1", MailboxID: "in-1", From: "alice", To: []string{"human"}, ReceivedAt: "2026-04-21T09:00:00.000Z"}
	inData, _ := json.Marshal(inMsg)
	os.WriteFile(filepath.Join(inboxDir, "message.json"), inData, 0o644)

	sentDir := filepath.Join(humanDir, "mailbox", "sent", "sent-1")
	os.MkdirAll(sentDir, 0o755)
	sentMsg := MailMessage{ID: "sent-1", MailboxID: "sent-1", From: "human", To: []string{"alice"}, ReceivedAt: "2026-04-21T09:30:00.000Z"}
	sentData, _ := json.Marshal(sentMsg)
	os.WriteFile(filepath.Join(sentDir, "message.json"), sentData, 0o644)

	cache := NewMailCache(humanDir).Refresh()
	if len(cache.Messages) != 2 {
		t.Fatalf("len = %d, want 2", len(cache.Messages))
	}
	for _, m := range cache.Messages {
		if !m.Delivered {
			t.Errorf("msg %s: Delivered = false, want true (inbox/sent messages are always delivered)", m.ID)
		}
	}
}

// writeSenderManifest writes .agent.json with the given admin value so
// WriteMail treats senderDir as a real agent (not pseudo).
func writeSenderManifest(t *testing.T, dir string, admin interface{}) {
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
	a := newMailboxID()
	b := newMailboxID()
	aPrefix, bPrefix := a[:15], b[:15]
	if strings.Compare(bPrefix, aPrefix) < 0 {
		t.Errorf("second id prefix %q < first %q — time went backwards", bPrefix, aPrefix)
	}
}

func TestWriteMail_ProducesShortMailboxID(t *testing.T) {
	recipientDir := t.TempDir()
	senderDir := t.TempDir()
	writeSenderManifest(t, senderDir, map[string]interface{}{"karma": true})

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

	data, err := os.ReadFile(filepath.Join(recipientDir, "mailbox", "inbox", name, "message.json"))
	if err != nil {
		t.Fatalf("read message.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(recipientDir, "mailbox", "inbox", name, "message.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary primary message file leaked: %v", err)
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

	sentEntries, err := os.ReadDir(filepath.Join(senderDir, "mailbox", "sent"))
	if err != nil {
		t.Fatalf("read sent: %v", err)
	}
	if len(sentEntries) != 1 || sentEntries[0].Name() != name {
		got := make([]string, 0, len(sentEntries))
		for _, e := range sentEntries {
			got = append(got, e.Name())
		}
		t.Errorf("sent entries = %v, want [%q]", got, name)
	}
	if _, err := os.Stat(filepath.Join(senderDir, "mailbox", "sent", name, "message.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary sent message file leaked: %v", err)
	}
}

func TestPrepareMailDirs_AllocatesDistinctIDs(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "mailbox", "inbox")

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
