package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// ThreadState is the one active, cold thread coordinate owned by App in PR5.
// It references the currently installed target-local session projection; it does
// not own a project store, mailbox cache, scanner, tick, or inactive-state map.
// PR7 owns any future bounded retention of inactive warm states.
type ThreadState struct {
	target                  asyncTarget
	generation              uint64
	acceptedSnapshotVersion uint64
	sessionCache            *fs.SessionCache
}

func newColdThreadState(target asyncTarget, generation, acceptedSnapshotVersion uint64, sessionCache *fs.SessionCache) ThreadState {
	return ThreadState{
		target:                  target,
		generation:              generation,
		acceptedSnapshotVersion: acceptedSnapshotVersion,
		sessionCache:            sessionCache,
	}
}

// threadLoadRequest carries detached accepted mailbox data and only the local
// coordinates needed by the future direct cold loader. It deliberately does not
// retain ProjectMailSnapshot, MailCache, MailModel, or another global owner.
type threadLoadRequest struct {
	envelope          asyncEnvelope
	humanDir          string
	humanAddress      string
	targetAddress     string
	targetDisplayName string
	acceptedMessages  []fs.MailMessage
	eventWindow       int
	inquiryWindow     int
}

// threadLoadResultMsg is the exact-envelope completion carrier for one physical
// direct load. App settles its physical token before the shared publication gate.
type threadLoadResultMsg struct {
	envelope     asyncEnvelope
	sessionCache *fs.SessionCache
	err          error
}

// threadLoadWorker is the internal physical-work seam used by deterministic
// tests and the production filesystem implementation. It is never exported.
type threadLoadWorker interface {
	Load(threadLoadRequest) (*fs.SessionCache, error)
}

// directThreadLoadWorker is the fieldless production implementation. It owns no
// mailbox, snapshot, scanner, tick, or retained ThreadState; every input arrives
// detached on the request and every output is one target-local NoPersist cache.
type directThreadLoadWorker struct{}

func (directThreadLoadWorker) Load(request threadLoadRequest) (*fs.SessionCache, error) {
	projectPath := filepath.Dir(filepath.Dir(request.humanDir))
	session := fs.NewSessionCache(request.humanDir, projectPath, fs.NoPersist)
	session.RebuildDirectThreadWindowedInMemory(
		request.acceptedMessages,
		request.humanAddress,
		request.targetAddress,
		request.envelope.target.directory,
		request.targetDisplayName,
		request.eventWindow,
		request.inquiryWindow,
	)
	return session, nil
}

// ThreadLoadCounters classifies physical and logical cold-load work without
// calling completed filesystem work cancellation. TrueCancelled remains zero in
// this slice because no cancellation reaches the filesystem loops.
type ThreadLoadCounters struct {
	Started       uint64
	Coalesced     uint64
	Completed     uint64
	TrueCancelled uint64
	StaleDropped  uint64
}

type threadLoadTargetKey struct {
	owner  asyncOwner
	target asyncTarget
}

// ThreadLoadCoordinator owns only physical in-flight tokens and at most one
// latest logical rerun per stable owner+target identity. It does not retain an
// inactive ThreadState or any project/mailbox owner.
type ThreadLoadCoordinator struct {
	worker      threadLoadWorker
	counters    ThreadLoadCounters
	inFlight    map[threadLoadTargetKey]asyncEnvelope
	latestRerun map[threadLoadTargetKey]threadLoadRequest
}

func newThreadLoadCoordinator(worker threadLoadWorker) ThreadLoadCoordinator {
	return ThreadLoadCoordinator{
		worker:      worker,
		inFlight:    make(map[threadLoadTargetKey]asyncEnvelope),
		latestRerun: make(map[threadLoadTargetKey]threadLoadRequest),
	}
}

func (c *ThreadLoadCoordinator) request(request threadLoadRequest) tea.Cmd {
	// Re-capture the completion envelope from the request's exact coordinates so
	// the shared protocol, rather than a manually assembled result, owns presence
	// bits and kind identity.
	current := asyncCurrent{
		binding: asyncBinding{
			owner:      request.envelope.owner,
			target:     request.envelope.target,
			generation: request.envelope.generation.thread,
		},
		storeVersion: request.envelope.storeVersion,
	}
	envelope := captureAsync(asyncColdThreadLoad, current)
	request.envelope = envelope
	request.acceptedMessages = append([]fs.MailMessage(nil), request.acceptedMessages...)
	key := threadLoadKey(envelope)
	c.ensureBookkeeping()
	if _, exists := c.inFlight[key]; exists {
		c.latestRerun[key] = request
		c.counters.Coalesced++
		return nil
	}
	c.inFlight[key] = envelope
	c.counters.Started++
	worker := c.worker
	return func() tea.Msg {
		if worker == nil {
			return threadLoadResultMsg{envelope: envelope}
		}
		sessionCache, err := worker.Load(request)
		return threadLoadResultMsg{envelope: envelope, sessionCache: sessionCache, err: err}
	}
}

// settle releases only the exact physical token, records its return, and may
// launch one still-current latest rerun. It constructs a candidate ThreadState
// but never installs it; App's single shared acceptAsync guard owns publication.
func (c *ThreadLoadCoordinator) settle(current asyncCurrent, msg threadLoadResultMsg) (*ThreadState, tea.Cmd, bool) {
	key := threadLoadKey(msg.envelope)
	inFlight, exists := c.inFlight[key]
	if !exists || inFlight != msg.envelope {
		return nil, nil, false
	}
	delete(c.inFlight, key)
	c.counters.Completed++

	var state *ThreadState
	if msg.err == nil && msg.sessionCache != nil {
		candidate := newColdThreadState(
			msg.envelope.target,
			msg.envelope.generation.thread,
			msg.envelope.storeVersion,
			msg.sessionCache,
		)
		state = &candidate
	}

	var rerun tea.Cmd
	if request, pending := c.latestRerun[key]; pending {
		delete(c.latestRerun, key)
		if acceptAsync(current, request.envelope) {
			rerun = c.request(request)
		}
	}
	return state, rerun, true
}

func (c *ThreadLoadCoordinator) recordStaleDrop() {
	c.counters.StaleDropped++
}

func (c *ThreadLoadCoordinator) ensureBookkeeping() {
	if c.inFlight == nil {
		c.inFlight = make(map[threadLoadTargetKey]asyncEnvelope)
	}
	if c.latestRerun == nil {
		c.latestRerun = make(map[threadLoadTargetKey]threadLoadRequest)
	}
}

func threadLoadKey(envelope asyncEnvelope) threadLoadTargetKey {
	return threadLoadTargetKey{owner: envelope.owner, target: envelope.target}
}

func (c ThreadLoadCoordinator) Counters() ThreadLoadCounters {
	return c.counters
}
