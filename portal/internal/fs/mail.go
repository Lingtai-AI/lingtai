package fs

import (
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
)

// newMailboxID builds a sortable, human-scannable mailbox id. The format
// (`YYYYMMDDTHHMMSS-xxxx`, 20 chars, UTC) matches the kernel helper
// `_new_mailbox_id` in `lingtai_kernel/intrinsics/email/primitives.py` so
// mail written by either side is indistinguishable in `email(check)` output.
// The 4-hex suffix is drawn from `uuid.New` (a v4 UUID); 16 bits of
// entropy per second is enough for human-paced sends and the WriteMail
// collision-retry loop covers the rare burst case.
var mailboxIDSource = func() string {
	ts := time.Now().UTC().Format("20060102T150405")
	// `uuid.New().String()` returns `xxxxxxxx-xxxx-...` — the first 4 hex
	// chars come before any dash, so this mirrors the kernel's
	// `uuid.uuid4().hex[:4]` slicing.
	suffix := uuid.New().String()[:4]
	return ts + "-" + suffix
}

func newMailboxID() string {
	return mailboxIDSource()
}

func ReadInbox(dir string) ([]MailMessage, error) {
	return readMailFolder(filepath.Join(dir, "mailbox", "inbox"))
}

func ReadArchive(dir string) ([]MailMessage, error) {
	return readMailFolder(filepath.Join(dir, "mailbox", "archive"))
}

// MailCache tracks already-loaded messages for incremental refresh.
// Each Refresh call reads only new messages from disk.
type MailCache struct {
	inboxSeen map[string]struct{} // mailbox id dirs already loaded from inbox
	sentSeen  map[string]struct{} // mailbox id dirs already loaded from sent
	Messages  []MailMessage       // full sorted merged slice (inbox + sent)
	inboxDir  string
	sentDir   string
}

// NewMailCache creates an empty cache for the given human directory.
func NewMailCache(humanDir string) MailCache {
	return MailCache{
		inboxSeen: make(map[string]struct{}),
		sentSeen:  make(map[string]struct{}),
		inboxDir:  filepath.Join(humanDir, "mailbox", "inbox"),
		sentDir:   filepath.Join(humanDir, "mailbox", "sent"),
	}
}

// Refresh scans inbox and sent folders for new messages, returning an updated
// cache. The receiver is not mutated — safe to call from a goroutine.
func (c MailCache) Refresh() MailCache {
	out := MailCache{
		inboxSeen: make(map[string]struct{}, len(c.inboxSeen)+16),
		sentSeen:  make(map[string]struct{}, len(c.sentSeen)+16),
		Messages:  make([]MailMessage, len(c.Messages)),
		inboxDir:  c.inboxDir,
		sentDir:   c.sentDir,
	}
	copy(out.Messages, c.Messages)
	for k := range c.inboxSeen {
		out.inboxSeen[k] = struct{}{}
	}
	for k := range c.sentSeen {
		out.sentSeen[k] = struct{}{}
	}

	// Scan inbox for new entries
	out.scanFolder(out.inboxDir, out.inboxSeen)
	// Scan sent for new entries
	out.scanFolder(out.sentDir, out.sentSeen)

	// Sort by ReceivedAt (RFC3339 strings sort lexicographically)
	sort.Slice(out.Messages, func(i, j int) bool {
		return out.Messages[i].ReceivedAt < out.Messages[j].ReceivedAt
	})
	return out
}

// scanFolder reads mailbox-id directories not yet in seen, loads their message.json,
// and appends to Messages.
func (c *MailCache) scanFolder(folder string, seen map[string]struct{}) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, ok := seen[name]; ok {
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
		seen[name] = struct{}{}
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

func writeJSONAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// mailboxIDCollisionRetries is the per-folder attempt budget for
// `prepareMailDirs`. The short ID has 16 bits of entropy in the
// suffix, so a same-second send has a 1/65536 chance of colliding;
// 8 retries reduces the practical failure probability to negligible while
// still terminating quickly when the filesystem is genuinely failing.
const mailboxIDCollisionRetries = 8

// prepareMailDirs allocates an id and creates every mailbox leaf that will
// receive this message. Non-pseudo sends write both the primary folder and the
// sender's sent/ folder, so the id must be free in both places; otherwise a
// same-second suffix collision in sent/ could overwrite an existing sent
// record. On a collision in any target folder the partial leaves from that
// attempt are removed and a fresh id is generated.
func prepareMailDirs(primaryParent string, sentParent string) (string, string, string, error) {
	if err := os.MkdirAll(primaryParent, 0o755); err != nil {
		return "", "", "", fmt.Errorf("create primary mailbox parent: %w", err)
	}
	if sentParent != "" {
		if err := os.MkdirAll(sentParent, 0o755); err != nil {
			return "", "", "", fmt.Errorf("create sent mailbox parent: %w", err)
		}
	}
	var lastErr error
	for i := 0; i < mailboxIDCollisionRetries; i++ {
		id := newMailboxID()
		primaryDir := filepath.Join(primaryParent, id)
		if err := os.Mkdir(primaryDir, 0o755); err != nil {
			if !errors.Is(err, iofs.ErrExist) {
				return "", "", "", fmt.Errorf("create primary mailbox leaf: %w", err)
			}
			lastErr = err
			continue
		}
		if sentParent == "" {
			return id, primaryDir, "", nil
		}
		sentDir := filepath.Join(sentParent, id)
		if err := os.Mkdir(sentDir, 0o755); err != nil {
			_ = os.Remove(primaryDir)
			if !errors.Is(err, iofs.ErrExist) {
				return "", "", "", fmt.Errorf("create sent mailbox leaf: %w", err)
			}
			lastErr = err
			continue
		}
		return id, primaryDir, sentDir, nil
	}
	return "", "", "", fmt.Errorf("create mailbox leaves: exhausted %d retries: %w",
		mailboxIDCollisionRetries, lastErr)
}

func WriteMail(recipientDir, senderDir, fromAddr, toAddr, subject, body string) error {
	// Read sender's manifest as identity card (same as Python agents do)
	identity := readManifestAsIdentity(senderDir)

	// Allocate every mailbox directory before writing JSON so the chosen id is
	// unique across all folders this send will touch. Pseudo-agent sends write
	// only outbox; non-pseudo sends also write sender sent/ with the same id.
	var primaryParent string
	pseudo := isPseudoAgent(identity)
	switch {
	case pseudo, IsRemoteAddress(toAddr):
		primaryParent = filepath.Join(senderDir, "mailbox", "outbox")
	default:
		primaryParent = filepath.Join(recipientDir, "mailbox", "inbox")
	}
	sentParent := ""
	if !pseudo {
		sentParent = filepath.Join(senderDir, "mailbox", "sent")
	}

	id, primaryDir, sentDir, err := prepareMailDirs(primaryParent, sentParent)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
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

	if err := writeJSONAtomic(filepath.Join(primaryDir, "message.json"), data); err != nil {
		return fmt.Errorf("write primary message: %w", err)
	}

	// Pseudo-agent branch: no sent/ copy at send time. The subscribed real
	// agent's pickup will produce the sent entry via atomic rename.
	if pseudo {
		return nil
	}

	if err := writeJSONAtomic(filepath.Join(sentDir, "message.json"), data); err != nil {
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
