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

// RecentMessageLimit is the hard cap on how many of the newest message entries
// the TUI loads and displays. It bounds both the mailbox bodies read to paint
// the page and the merged entry window the chat view renders, so the first
// frame never costs a full-mailbox or full-history scan. There is deliberately
// no exact total-message count anywhere above this cap: this window IS the
// loaded history.
const RecentMessageLimit = 1000

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

// Clone returns a recursively detached copy of the cache. Unlike Refresh's
// ordinary copy-on-refresh behavior, Clone separates every mutable collection
// in the mailbox graph and preserves nil versus non-nil empty shapes.
func (c MailCache) Clone() MailCache {
	out := MailCache{
		humanDir:  c.humanDir,
		inboxDir:  c.inboxDir,
		sentDir:   c.sentDir,
		outboxDir: c.outboxDir,
	}
	if c.seen != nil {
		out.seen = make(map[string]int, len(c.seen))
		for id, index := range c.seen {
			out.seen[id] = index
		}
	}
	if c.Messages != nil {
		out.Messages = make([]MailMessage, len(c.Messages))
		for i, msg := range c.Messages {
			out.Messages[i] = cloneMailMessage(msg)
		}
	}
	return out
}

func cloneMailMessage(msg MailMessage) MailMessage {
	out := msg
	out.To = cloneJSONValue(msg.To)
	out.CC = cloneStrings(msg.CC)
	out.Attachments = cloneStrings(msg.Attachments)
	out.Identity = cloneJSONMap(msg.Identity)
	return out
}

func cloneStrings(src []string) []string {
	if src == nil {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

func cloneJSONMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for key, value := range src {
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneJSONValue(src interface{}) interface{} {
	switch value := src.(type) {
	case map[string]interface{}:
		return cloneJSONMap(value)
	case []interface{}:
		if value == nil {
			return []interface{}(nil)
		}
		out := make([]interface{}, len(value))
		for i, item := range value {
			out[i] = cloneJSONValue(item)
		}
		return out
	case []string:
		return cloneStrings(value)
	default:
		return value
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
	// Then inbox and sent — for any mailbox id previously seen in outbox that now
	// appears in sent, the scan flips Delivered to true in place.
	out.scanFolder(out.outboxDir, false /* delivered */)
	out.scanFolder(out.inboxDir, true)
	out.scanFolder(out.sentDir, true)

	// Sort by the parsed instant; RFC3339Nano's variable-width fractional
	// seconds do not sort chronologically as strings.
	sort.Slice(out.Messages, func(i, j int) bool {
		return mailMessageBefore(out.Messages[i], out.Messages[j])
	})
	// Rebuild the mailbox-id→index map after the sort. Keyed by MailboxID, which is
	// the mailbox directory basename (what scanFolder looks up) — not msg.ID,
	// which could diverge from the directory name if a future kernel rewrote
	// the JSON during pickup. MailboxID is set to the directory name at write
	// time in WriteMail and never mutated.
	for i, m := range out.Messages {
		out.seen[m.MailboxID] = i
	}
	return out
}

// RefreshRecent is the bounded refresh used to paint the page. It lists the
// mailbox directories, keeps only the newest `limit` mailbox ids, and reads
// message.json only for those — so the cost of a refresh is O(limit) bodies
// plus one directory listing per folder, never one read per message ever sent.
//
// Selecting the window by mailbox id is exact rather than approximate: every
// producer (TUI, portal, kernel) names a mailbox leaf with the sortable UTC
// stamp `YYYYMMDDTHHMMSS-xxxx`, so lexical order over directory names is
// chronological order without opening a single body.
//
// Retention matches Refresh: an already-loaded message is reused rather than
// re-read, and is kept even if its folder no longer lists it. What differs is
// the bound — anything older than the newest `limit` ids is dropped, so the
// returned cache holds at most `limit` entries. The cap is therefore observable
// in the snapshot itself, not applied at render time over a fully loaded set.
// A limit <= 0 falls back to the unbounded Refresh. As with Refresh, the
// receiver is not mutated, so this is safe to call from a goroutine.
func (c MailCache) RefreshRecent(limit int) MailCache {
	if limit <= 0 {
		return c.Refresh()
	}
	out := MailCache{
		humanDir:  c.humanDir,
		inboxDir:  c.inboxDir,
		sentDir:   c.sentDir,
		outboxDir: c.outboxDir,
	}

	retained := make(map[string]MailMessage, len(c.Messages))
	for _, msg := range c.Messages {
		id := mailCacheKey(msg)
		if _, known := retained[id]; !known {
			retained[id] = msg
		}
	}

	// List every mailbox leaf — names only, no bodies. Folder order matches
	// Refresh's scan order: outbox first, so a message is visible immediately
	// after send, then inbox and sent, which mark it delivered. The first folder
	// holding an id owns its body.
	sources := []struct {
		folder    string
		delivered bool
	}{
		{out.outboxDir, false},
		{out.inboxDir, true},
		{out.sentDir, true},
	}
	folderFor := make(map[string]string)
	delivered := make(map[string]bool)
	var onDisk []string
	for _, source := range sources {
		for _, id := range mailboxIDsIn(source.folder) {
			if _, known := folderFor[id]; !known {
				folderFor[id] = source.folder
				onDisk = append(onDisk, id)
			}
			if source.delivered {
				delivered[id] = true
			}
		}
	}

	// Narrow the on-disk candidates to the newest window BEFORE any body is
	// read: this is what keeps a refresh independent of total mailbox size, and
	// it is why a steady-state tick only reads what actually arrived.
	sort.Strings(onDisk)
	if len(onDisk) > limit {
		onDisk = onDisk[len(onDisk)-limit:]
	}

	messages := make([]MailMessage, 0, len(retained)+len(onDisk))
	for _, id := range onDisk {
		if _, loaded := retained[id]; loaded {
			continue
		}
		msg, ok := readMailboxMessage(folderFor[id], id)
		if !ok {
			continue
		}
		msg.Delivered = delivered[id]
		messages = append(messages, msg)
	}
	for id, msg := range retained {
		// Already loaded — never re-read. If a delivered folder now holds it and
		// the retained entry says otherwise, flip in place (outbox→sent).
		if delivered[id] && !msg.Delivered {
			msg.Delivered = true
		}
		messages = append(messages, msg)
	}

	// Sort by the parsed instant, exactly like Refresh, then keep the newest
	// window. Trimming here rather than by id makes "most recent" mean the
	// timestamp for everything already in hand, while the id ordering above is
	// only ever used to decide which unread bodies are worth opening.
	sort.Slice(messages, func(i, j int) bool {
		return mailMessageBefore(messages[i], messages[j])
	})
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}

	out.Messages = messages
	out.seen = make(map[string]int, len(messages))
	for i, m := range messages {
		out.seen[m.MailboxID] = i
	}
	return out
}

// mailCacheKey is the stable per-message identity used to window and deduplicate
// a cache: the mailbox directory name, which WriteMail stamps into MailboxID.
// The ID fallback covers messages assembled in memory rather than read from a
// mailbox leaf.
func mailCacheKey(msg MailMessage) string {
	if msg.MailboxID != "" {
		return msg.MailboxID
	}
	return msg.ID
}

// mailboxIDsIn lists the mailbox-id directory names in folder. It reads only
// the directory entry list and never opens a message body, so it stays cheap on
// mailboxes holding tens of thousands of messages.
func mailboxIDsIn(folder string) []string {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ids = append(ids, entry.Name())
	}
	return ids
}

// RecentMailboxIDs returns at most the newest `limit` mailbox-leaf ids in
// folder, chosen from the directory listing alone — no message body is opened,
// so the caller decides what to read while still paying only one ReadDir.
//
// It is the shared primitive for readers outside this package that need the
// same window RefreshRecent applies internally: every producer names a mailbox
// leaf with the sortable UTC stamp `YYYYMMDDTHHMMSS-xxxx`, so lexical order
// over directory names is chronological order. A limit <= 0 returns every id.
// The result is sorted oldest-first, matching the order a mailbox is displayed
// in.
func RecentMailboxIDs(folder string, limit int) []string {
	ids := mailboxIDsIn(folder)
	sort.Strings(ids)
	if limit > 0 && len(ids) > limit {
		ids = ids[len(ids)-limit:]
	}
	return ids
}

// readMailboxMessage reads and decodes one mailbox leaf's message.json.
// A missing or malformed body reports false and is skipped by every caller.
func readMailboxMessage(folder, id string) (MailMessage, bool) {
	data, err := os.ReadFile(filepath.Join(folder, id, "message.json"))
	if err != nil {
		return MailMessage{}, false
	}
	var msg MailMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return MailMessage{}, false
	}
	return msg, true
}

func mailMessageBefore(a, b MailMessage) bool {
	at, aErr := time.Parse(time.RFC3339Nano, a.ReceivedAt)
	bt, bErr := time.Parse(time.RFC3339Nano, b.ReceivedAt)
	if aErr == nil && bErr == nil {
		if !at.Equal(bt) {
			return at.Before(bt)
		}
	} else if a.ReceivedAt != b.ReceivedAt {
		return a.ReceivedAt < b.ReceivedAt
	}
	if a.MailboxID != b.MailboxID {
		return a.MailboxID < b.MailboxID
	}
	return a.ID < b.ID
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
		msg, ok := readMailboxMessage(folder, name)
		if !ok {
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
		msg, ok := readMailboxMessage(folder, entry.Name())
		if !ok {
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
	return writeAtomicBytes(path, data, 0o644)
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
