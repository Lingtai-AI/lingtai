package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
