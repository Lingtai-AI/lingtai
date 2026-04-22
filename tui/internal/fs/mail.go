package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
)

func ReadInbox(dir string) ([]MailMessage, error) {
	return readMailFolder(filepath.Join(dir, "mailbox", "inbox"))
}

func ReadSent(dir string) ([]MailMessage, error) {
	return readMailFolder(filepath.Join(dir, "mailbox", "sent"))
}

// MailCache tracks already-loaded messages for incremental refresh.
// Each Refresh call reads new messages from disk. Messages transitioning
// from outbox/ to sent/ have their Delivered flag flipped in place.
type MailCache struct {
	seen      map[string]int // UUID → index into Messages
	Messages  []MailMessage  // full sorted merged slice (outbox + inbox + sent)
	humanDir  string
	inboxDir  string
	sentDir   string
	outboxDir string
}

// NewMailCache creates an empty cache for the given human directory.
func NewMailCache(humanDir string) MailCache {
	return MailCache{
		seen:      make(map[string]int),
		humanDir:  humanDir,
		inboxDir:  filepath.Join(humanDir, "mailbox", "inbox"),
		sentDir:   filepath.Join(humanDir, "mailbox", "sent"),
		outboxDir: filepath.Join(humanDir, "mailbox", "outbox"),
	}
}

// Refresh scans outbox, inbox, and sent folders for new messages, returning
// an updated cache. The receiver is not mutated — safe to call from a goroutine.
// A message that transitions from outbox/ to sent/ between refreshes has its
// Delivered flag flipped from false to true in place (no duplicate entry).
func (c MailCache) Refresh() MailCache {
	out := MailCache{
		seen:      make(map[string]int, len(c.seen)+16),
		Messages:  make([]MailMessage, len(c.Messages)),
		humanDir:  c.humanDir,
		inboxDir:  c.inboxDir,
		sentDir:   c.sentDir,
		outboxDir: c.outboxDir,
	}
	copy(out.Messages, c.Messages)
	for k, v := range c.seen {
		out.seen[k] = v
	}

	// Order matters: scan outbox first so messages appear immediately after send.
	// Then inbox and sent — for any UUID previously seen in outbox that now appears
	// in sent, the scan flips Delivered to true in place.
	out.scanFolder(out.outboxDir, false /* delivered */)
	out.scanFolder(out.inboxDir, true)
	out.scanFolder(out.sentDir, true)

	// Sort by ReceivedAt (RFC3339 strings sort lexicographically).
	sort.Slice(out.Messages, func(i, j int) bool {
		return out.Messages[i].ReceivedAt < out.Messages[j].ReceivedAt
	})
	// Rebuild the UUID→index map after the sort. Keyed by MailboxID, which is
	// the mailbox directory basename (what scanFolder looks up) — not msg.ID,
	// which could diverge from the directory name if a future kernel rewrote
	// the JSON during pickup. MailboxID is set to the directory name at write
	// time in WriteMail and never mutated.
	for i, m := range out.Messages {
		out.seen[m.MailboxID] = i
	}
	return out
}

// scanFolder reads UUID directories in folder. For UUIDs not yet in seen,
// loads their message.json, stamps Delivered, and appends to Messages.
// For UUIDs already in seen: if delivered=true, flips the existing entry's
// Delivered flag to true (outbox→sent transition). If delivered=false and
// the UUID is already known, skip — we've already loaded it.
func (c *MailCache) scanFolder(folder string, delivered bool) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if idx, ok := c.seen[name]; ok {
			// Already loaded. If this folder marks the message as delivered
			// and the current entry isn't, flip in place (outbox→sent).
			if delivered && !c.Messages[idx].Delivered {
				c.Messages[idx].Delivered = true
			}
			continue
		}
		msgPath := filepath.Join(folder, name, "message.json")
		data, err := os.ReadFile(msgPath)
		if err != nil {
			continue
		}
		var msg MailMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		msg.Delivered = delivered
		c.seen[name] = len(c.Messages)
		c.Messages = append(c.Messages, msg)
	}
}

func readMailFolder(folder string) ([]MailMessage, error) {
	entries, err := os.ReadDir(folder)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read folder: %w", err)
	}
	var messages []MailMessage
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		msgPath := filepath.Join(folder, entry.Name(), "message.json")
		data, err := os.ReadFile(msgPath)
		if err != nil {
			continue
		}
		var msg MailMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// readManifestAsIdentity reads .agent.json from dir and returns it as the identity card.
func readManifestAsIdentity(dir string) map[string]interface{} {
	data, err := os.ReadFile(filepath.Join(dir, ".agent.json"))
	if err != nil {
		return map[string]interface{}{"agent_name": "human", "admin": nil}
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return map[string]interface{}{"agent_name": "human", "admin": nil}
	}
	return manifest
}

func WriteMail(recipientDir, senderDir, fromAddr, toAddr, subject, body string) error {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Read sender's manifest as identity card (same as Python agents do)
	identity := readManifestAsIdentity(senderDir)

	msg := MailMessage{
		ID:         id,
		MailboxID:  id,
		From:       fromAddr,
		To:         []string{toAddr},
		CC:         []string{},
		Subject:    subject,
		Message:    body,
		Type:       "normal",
		ReceivedAt: now,
		Identity:   identity,
	}

	data, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	// Pseudo-agent branch: if sender's .agent.json has admin: null (or no
	// manifest), write only to the sender's outbox. Subscribed real agents
	// poll the outbox and produce the sent entry via atomic rename on pickup.
	if isPseudoAgent(identity) {
		outboxDir := filepath.Join(senderDir, "mailbox", "outbox", id)
		if err := os.MkdirAll(outboxDir, 0o755); err != nil {
			return fmt.Errorf("create outbox dir: %w", err)
		}
		if err := os.WriteFile(filepath.Join(outboxDir, "message.json"), data, 0o644); err != nil {
			return fmt.Errorf("write outbox message: %w", err)
		}
		return nil
	}

	if IsRemoteAddress(toAddr) {
		outboxDir := filepath.Join(senderDir, "mailbox", "outbox", id)
		if err := os.MkdirAll(outboxDir, 0o755); err != nil {
			return fmt.Errorf("create outbox dir: %w", err)
		}
		if err := os.WriteFile(filepath.Join(outboxDir, "message.json"), data, 0o644); err != nil {
			return fmt.Errorf("write outbox message: %w", err)
		}
	} else {
		inboxDir := filepath.Join(recipientDir, "mailbox", "inbox", id)
		if err := os.MkdirAll(inboxDir, 0o755); err != nil {
			return fmt.Errorf("create inbox dir: %w", err)
		}
		if err := os.WriteFile(filepath.Join(inboxDir, "message.json"), data, 0o644); err != nil {
			return fmt.Errorf("write inbox message: %w", err)
		}
	}

	sentDir := filepath.Join(senderDir, "mailbox", "sent", id)
	if err := os.MkdirAll(sentDir, 0o755); err != nil {
		return fmt.Errorf("create sent dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(sentDir, "message.json"), data, 0o644); err != nil {
		return fmt.Errorf("write sent message: %w", err)
	}

	return nil
}

// isPseudoAgent returns true if the identity manifest indicates a pseudo-agent
// (no running agent process). The admin field being nil — including when
// .agent.json is missing entirely, which readManifestAsIdentity falls back to —
// is the pseudo-agent signal.
func isPseudoAgent(identity map[string]interface{}) bool {
	if identity == nil {
		return true
	}
	admin, present := identity["admin"]
	if !present {
		return true
	}
	return admin == nil
}
