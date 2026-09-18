package fs

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DirectMailPublication is an immutable, owner-neutral index over one accepted
// mailbox snapshot and its canonical direct-target catalog. Construction walks
// accepted mail once and evaluates IsDirectMail only for targets whose route
// (bare address or absolute directory) is the message's strict peer. Callers
// receive detached, page-bounded results; the publication never exposes its
// retained message graph.
type DirectMailPublication struct {
	humanAddress string
	threads      map[string]directThreadPublication
}

type directThreadPublication struct {
	target       DirectTarget
	messages     []MailMessage
	incoming     []directUnreadMessage
	latest       directUnreadCursor
	incomingByID map[string]time.Time
	incomingErr  error
}

// NewDirectMailPublication builds one immutable direct index. Invalid or
// duplicate stable target keys are omitted fail-closed; canonical callers have
// already removed such targets. Duplicate current routes remain separate
// candidates, preserving IsDirectMail's legacy routing semantics.
func NewDirectMailPublication(humanAddress string, targets []DirectTarget, accepted []MailMessage) *DirectMailPublication {
	humanAddress = strings.TrimSpace(humanAddress)
	publication := &DirectMailPublication{
		humanAddress: humanAddress,
		threads:      make(map[string]directThreadPublication, len(targets)),
	}
	if humanAddress == "" {
		return publication
	}

	keyCounts := make(map[string]int, len(targets))
	for _, target := range targets {
		keyCounts[DirectThreadKey(target)]++
	}
	humanSpelling := directSpelling(humanAddress)
	humans := map[string]struct{}{humanSpelling: {}}
	byRoute := make(map[string][]string, len(targets))
	for _, target := range targets {
		key := DirectThreadKey(target)
		if key == "" || keyCounts[key] != 1 {
			continue
		}
		publication.threads[key] = directThreadPublication{
			target:       target,
			latest:       directUnreadCursor{ids: []string{}},
			incomingByID: make(map[string]time.Time),
		}
		if directory := directHumanEndpoint(humanAddress, target).directory; directory != "" {
			humans[directory] = struct{}{}
		}
		route := directTargetEndpoint(target)
		if route.address != "" && route.address != humanSpelling {
			byRoute[route.address] = append(byRoute[route.address], key)
			if route.directory != "" && route.directory != route.address {
				byRoute[route.directory] = append(byRoute[route.directory], key)
			}
		}
	}

	for _, message := range accepted {
		peer, ok := directMailPeerAddress(message, humans)
		if !ok {
			continue
		}
		for _, key := range byRoute[peer] {
			thread := publication.threads[key]
			if !IsDirectMail(message, humanAddress, thread.target) {
				continue
			}
			// Detach only matching mail so the fs-owned publication stays immutable
			// even for legacy callers that supplied a mutable accepted slice.
			thread.messages = append(thread.messages, cloneMailMessage(message))
			// IsDirectMail tries the human as sender first and rejects any spelling the
			// human and target share, so accepted mail from anyone else is the target's.
			if !directHumanEndpoint(humanAddress, thread.target).names(message.From) {
				thread.appendIncoming(message)
			}
			publication.threads[key] = thread
		}
	}
	return publication
}

// directMailPeerAddress performs only the strict envelope geometry needed to
// select same-route candidates: the spelling opposite any spelling of the human
// (humans holds every one across the targets' projects). IsDirectMail remains
// the authoritative final predicate for direction, identity.agent_id, CC, and
// normalization behavior.
func directMailPeerAddress(message MailMessage, humans map[string]struct{}) (string, bool) {
	if len(message.CC) != 0 {
		return "", false
	}
	recipient, ok := strictMailRecipient(message.To)
	if !ok {
		return "", false
	}
	from, to := directSpelling(message.From), directSpelling(recipient)
	_, fromHuman := humans[from]
	_, toHuman := humans[to]
	switch {
	case fromHuman:
		return to, true
	case toHuman:
		return from, from != ""
	default:
		return "", false
	}
}

func (thread *directThreadPublication) appendIncoming(message MailMessage) {
	if thread.incomingErr != nil {
		return
	}
	receivedAt, err := time.Parse(time.RFC3339Nano, message.ReceivedAt)
	if err != nil {
		thread.incomingErr = fmt.Errorf("resolve direct unread message: invalid received_at %q: %w", message.ReceivedAt, err)
		return
	}
	id := message.MailboxID
	if strings.TrimSpace(id) == "" {
		id = message.ID
	}
	if strings.TrimSpace(id) == "" {
		thread.incomingErr = fmt.Errorf("resolve direct unread message: missing stable message ID")
		return
	}
	if prior, exists := thread.incomingByID[id]; exists {
		if !prior.Equal(receivedAt) {
			thread.incomingErr = fmt.Errorf("resolve direct unread message: stable message ID %q has conflicting received_at", id)
		}
		return
	}
	thread.incomingByID[id] = receivedAt
	thread.incoming = append(thread.incoming, directUnreadMessage{id: id, at: receivedAt})
	switch {
	case receivedAt.After(thread.latest.receivedAt):
		thread.latest = directUnreadCursor{receivedAt: receivedAt, ids: []string{id}}
	case receivedAt.Equal(thread.latest.receivedAt):
		thread.latest.ids = append(thread.latest.ids, id)
		sort.Strings(thread.latest.ids)
	}
}

// DirectPage returns the newest horizon messages for exactly the supplied
// stable route, in chronological order, plus whether the thread has older mail.
// Work and allocation are O(page) and independent of unrelated accepted mail.
func (p *DirectMailPublication) DirectPage(target DirectTarget, horizon int) ([]MailMessage, bool) {
	if horizon < 1 {
		return nil, false
	}
	thread, ok := p.threadForTarget(target)
	if !ok {
		return nil, false
	}
	start := len(thread.messages) - horizon
	hasOlder := start > 0
	if start < 0 {
		start = 0
	}
	page := make([]MailMessage, len(thread.messages)-start)
	for index, message := range thread.messages[start:] {
		page[index] = cloneMailMessage(message)
	}
	return page, hasOlder
}

func (p *DirectMailPublication) threadForTarget(target DirectTarget) (directThreadPublication, bool) {
	if p == nil {
		return directThreadPublication{}, false
	}
	key := DirectThreadKey(target)
	thread, ok := p.threads[key]
	if !ok || thread.target.ProjectDirectory != target.ProjectDirectory ||
		thread.target.AgentID != target.AgentID ||
		strings.TrimSpace(thread.target.Address) != strings.TrimSpace(target.Address) {
		return directThreadPublication{}, false
	}
	return thread, true
}

func (p *DirectMailPublication) unreadMessages(target DirectTarget) ([]directUnreadMessage, error) {
	thread, ok := p.threadForTarget(target)
	if !ok {
		return nil, fmt.Errorf("direct mail publication has no matching stable route %q", DirectThreadKey(target))
	}
	if thread.incomingErr != nil {
		return nil, thread.incomingErr
	}
	return thread.incoming, nil
}

func (p *DirectMailPublication) unreadBoundary(target DirectTarget) (directUnreadCursor, error) {
	thread, ok := p.threadForTarget(target)
	if !ok {
		return directUnreadCursor{}, fmt.Errorf("direct mail publication has no matching stable route %q", DirectThreadKey(target))
	}
	if thread.incomingErr != nil {
		return directUnreadCursor{}, thread.incomingErr
	}
	return directUnreadCursor{receivedAt: thread.latest.receivedAt, ids: cloneDirectUnreadIDs(thread.latest.ids)}, nil
}

func (p *DirectMailPublication) validates(humanAddress string, targets []DirectTarget) error {
	if p == nil {
		return fmt.Errorf("nil direct mail publication")
	}
	if p.humanAddress != strings.TrimSpace(humanAddress) {
		return fmt.Errorf("direct mail publication human address mismatch")
	}
	for _, target := range targets {
		if _, ok := p.threadForTarget(target); !ok {
			return fmt.Errorf("direct mail publication has no matching stable route %q", DirectThreadKey(target))
		}
	}
	return nil
}
