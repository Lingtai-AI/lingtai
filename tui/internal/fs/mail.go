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
// `_new_mailbox_id` in `lingtai.kernel/intrinsics/email/primitives.py` and
// the portal's mirror in `portal/internal/fs/mail.go` so mail written by
// any of the three sides is indistinguishable in `email(check)` output.
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

// ErrRemoteMailUnsupported reports that TUI mail cannot deliver to a remote address.
// Remote delivery is unavailable; rejecting it before any mailbox allocation
// keeps unsupported sends side-effect free.
var ErrRemoteMailUnsupported = errors.New("unsupported remote mail address")

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
	seen      map[string]int // mailbox id → index into Messages
	Messages  []MailMessage  // full sorted merged slice (outbox + inbox + sent)
	humanDir  string
	inboxDir  string
	sentDir   string
	outboxDir string

	// lastFullSentScan records the last time Refresh walked the entire sent/
	// tree. Pending outbox messages still get exact sent/<id>/message.json checks
	// every refresh, but historical sent mail is budgeted so the 1s TUI refresh
	// loop does not repeatedly full-scan large external-disk mailboxes.
	lastFullSentScan time.Time
}

// NewMailCache creates an empty cache for the given human directory.
// mailCacheFullSentScanInterval budgets expensive full sent/ scans in
// MailCache.Refresh. Tests override this package var.
var mailCacheFullSentScanInterval = 10 * time.Second

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
		seen:             make(map[string]int, len(c.seen)+16),
		Messages:         make([]MailMessage, len(c.Messages)),
		humanDir:         c.humanDir,
		inboxDir:         c.inboxDir,
		sentDir:          c.sentDir,
		outboxDir:        c.outboxDir,
		lastFullSentScan: c.lastFullSentScan,
	}
	copy(out.Messages, c.Messages)
	for k, v := range c.seen {
		out.seen[k] = v
	}

	// Order matters: scan outbox first so messages appear immediately after send,
	// then inbox. Pending outbox messages get exact sent/<id>/message.json checks
	// on every refresh so their Delivered flag flips promptly after pickup without
	// forcing a full sent/ scan every second. Historical sent/ discovery is budgeted
	// below because large external-disk sent folders can dominate the TUI loop.
	out.scanFolder(out.outboxDir, false /* delivered */)
	out.scanFolder(out.inboxDir, true)
	out.markDeliveredFromSentIDs()

	now := time.Now()
	if out.lastFullSentScan.IsZero() || now.Sub(out.lastFullSentScan) >= mailCacheFullSentScanInterval {
		out.scanFolder(out.sentDir, true)
		out.lastFullSentScan = now
	}

	// Sort by ReceivedAt (RFC3339 strings sort lexicographically).
	sort.Slice(out.Messages, func(i, j int) bool {
		return out.Messages[i].ReceivedAt < out.Messages[j].ReceivedAt
	})
	// Rebuild the mailbox-id→index map after the sort. Keyed by MailboxID, which is
	// the mailbox directory basename (what scanFolder looks up) — not msg.ID,
	// which could diverge from the directory name if a future kernel rewrote
	// the JSON during pickup. MailboxID is set to the directory name at write
	// time in WriteMail and never mutated.
	out.seen = make(map[string]int, len(out.Messages))
	for i, m := range out.Messages {
		out.seen[m.MailboxID] = i
	}
	return out
}

// scanFolder reads mailbox-id directories in folder. For mailbox ids not yet in seen,
// loads their message.json, stamps Delivered, and appends to Messages.
// For mailbox ids already in seen: if delivered=true, flips the existing entry's
// Delivered flag to true (outbox→sent transition). If delivered=false and
// the mailbox id is already known, skip — we've already loaded it.
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

// markDeliveredFromSentIDs checks only the exact sent/<mailbox-id>/message.json
// paths for messages already visible as pending/undelivered. This preserves the
// immediate outbox→sent delivered-state transition without a full sent/ walk.
func (c *MailCache) markDeliveredFromSentIDs() {
	for i := range c.Messages {
		if c.Messages[i].Delivered {
			continue
		}
		mailboxID := c.Messages[i].MailboxID
		if mailboxID == "" {
			mailboxID = c.Messages[i].ID
		}
		if mailboxID == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(c.sentDir, mailboxID, "message.json")); err == nil {
			c.Messages[i].Delivered = true
		}
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

func WriteMail(recipientDir, senderDir, fromAddr, toAddr, subject, body string) error {
	if IsRemoteAddress(toAddr) {
		return ErrRemoteMailUnsupported
	}

	// Read sender's manifest as identity card (same as Python agents do)
	identity := readManifestAsIdentity(senderDir)

	// Allocate every mailbox directory before writing JSON so the chosen id is
	// unique across all folders this send will touch. Pseudo-agent sends write
	// only outbox; non-pseudo sends also write sender sent/ with the same id.
	var primaryParent string
	pseudo := isPseudoAgent(identity)
	switch {
	case pseudo:
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
