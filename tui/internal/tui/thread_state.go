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
// direct load. The surface is inert until the typed physical-loader RED exists.
type threadLoadResultMsg struct {
	envelope     asyncEnvelope
	sessionCache *fs.SessionCache
	err          error
}

// threadLoadWorker is the internal physical-work seam used by deterministic
// tests and the eventual filesystem implementation. It is never exported.
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

// ThreadLoadCoordinator is the App-owned resource-accounting surface for the
// forthcoming behavioral cold loader. This compiling seam intentionally does
// not call the worker, coalesce, publish, or increment counters; those behaviors
// follow their own typed assertion REDs.
type ThreadLoadCoordinator struct {
	worker   threadLoadWorker
	counters ThreadLoadCounters
}

func newThreadLoadCoordinator(worker threadLoadWorker) ThreadLoadCoordinator {
	return ThreadLoadCoordinator{worker: worker}
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
	worker := c.worker
	return func() tea.Msg {
		if worker == nil {
			return threadLoadResultMsg{envelope: envelope}
		}
		sessionCache, err := worker.Load(request)
		return threadLoadResultMsg{envelope: envelope, sessionCache: sessionCache, err: err}
	}
}

func (c *ThreadLoadCoordinator) settle(asyncCurrent, threadLoadResultMsg) (*ThreadState, tea.Cmd, bool) {
	// Intentionally non-publishing until the physical-loader and coordinator
	// behavior tests exist. In particular, counters remain honest zeros.
	return nil, nil, false
}

func (c ThreadLoadCoordinator) Counters() ThreadLoadCounters {
	return c.counters
}
