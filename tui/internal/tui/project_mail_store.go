package tui

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

// ProjectMailSnapshot is an immutable, accepted view of one project's human
// mailbox. A refresh result becomes visible only after its async envelope is
// accepted. The cache remains private so callers cannot turn a snapshot into a
// second refresh owner.
type ProjectMailSnapshot struct {
	version uint64
	cache   fs.MailCache
}

func (s *ProjectMailSnapshot) Version() uint64 {
	if s == nil {
		return 0
	}
	return s.version
}

type projectMailScanner interface {
	Refresh(fs.MailCache) fs.MailCache
}

type filesystemProjectMailScanner struct{}

func (filesystemProjectMailScanner) Refresh(cache fs.MailCache) fs.MailCache {
	return cache.Refresh()
}

type projectMailLocationUpdater func(string)

type projectMailRefreshMsg struct {
	envelope asyncEnvelope
	cache    fs.MailCache
	mail     mailRefreshPayload
}

type projectMailTickMsg struct {
	envelope asyncEnvelope
	at       time.Time
}

type projectMailRefreshRequestMsg struct {
	envelope asyncEnvelope
	initial  bool
}

var projectMailStoreSequence atomic.Uint64

// projectMailScanSingleflight serializes the physical refresh body across store
// identities. A suspended home command cannot be cancelled once Bubble Tea has
// started it, so a newly active visited store waits here instead of scanning in
// parallel. The gate owns no project data, accepted state, or tick lifecycle.
var projectMailScanSingleflight sync.Mutex

// projectMailAsyncState is the atomic/current binding seam used only by delayed
// side effects. Permission still comes from acceptAsync; this holder merely lets
// a command load the current coordinates after App value copies have moved on.
type projectMailAsyncState struct {
	mu      sync.RWMutex
	current asyncCurrent
}

func (s *projectMailAsyncState) load() asyncCurrent {
	if s == nil {
		return asyncCurrent{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *projectMailAsyncState) store(current asyncCurrent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.current = current
	s.mu.Unlock()
}

// ProjectMailStore is the root-owned project-lifetime mailbox owner. It owns
// exactly one MailCache, one accepted-snapshot sequence, one refresh pipeline,
// and one invalidatable polling chain. Bubble Tea serializes its mutations on
// App.Update; background commands receive detached values and can publish only
// after the root's shared async-envelope acceptance.
type ProjectMailStore struct {
	id                       uint64
	projectID                string
	projectDir               string
	humanDir                 string
	cache                    fs.MailCache
	snapshot                 *ProjectMailSnapshot
	version                  uint64
	activation               uint64
	tickChain                uint64
	active                   bool
	tickRunning              bool
	refreshInFlight          bool
	refreshInitial           bool
	refreshInFlightEnvelope  asyncEnvelope
	initialRefreshPending    bool
	pollRate                 time.Duration
	scanner                  projectMailScanner
	updateLocation           projectMailLocationUpdater
	binding                  asyncBinding
	revalidateTarget         func(asyncOwner, asyncTarget) bool
	revalidateTargetExplicit bool
	asyncState               *projectMailAsyncState
	locationSourceVersion    uint64
}

func canonicalProjectMailIdentity(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return filepath.Clean(projectDir)
	}
	return filepath.Clean(abs)
}

func newProjectMailStore(projectDir, humanDir string) ProjectMailStore {
	return newProjectMailStoreWithDeps(projectDir, humanDir, filesystemProjectMailScanner{}, fs.UpdateHumanLocation)
}

func newProjectMailStoreWithDeps(projectDir, humanDir string, scanner projectMailScanner, updateLocation projectMailLocationUpdater) ProjectMailStore {
	if scanner == nil {
		scanner = filesystemProjectMailScanner{}
	}
	if updateLocation == nil {
		updateLocation = func(string) {}
	}
	store := ProjectMailStore{
		id:                    projectMailStoreSequence.Add(1),
		projectID:             canonicalProjectMailIdentity(projectDir),
		projectDir:            projectDir,
		humanDir:              humanDir,
		cache:                 fs.NewMailCache(humanDir),
		activation:            1,
		active:                true,
		pollRate:              time.Second,
		scanner:               scanner,
		updateLocation:        updateLocation,
		revalidateTarget:      rejectUnboundAsyncTarget,
		asyncState:            &projectMailAsyncState{},
		locationSourceVersion: 0,
	}
	store.syncAsyncState()
	return store
}

// rejectUnboundAsyncTarget keeps a ProjectMailStore fail-closed until App binds
// its one owner-aware lifecycle revalidator. Home ordinary targets are checked
// against the accepted root inventory; project visits retain their explicit
// bridge below. No store-level fallback may launch a second inventory scan.
func rejectUnboundAsyncTarget(asyncOwner, asyncTarget) bool {
	return false
}

// revalidateProjectVisitTarget is the PR2 visit-policy bridge. It deliberately
// retains global inventory.Record.Enterable semantics. Owner: App visit
// coordinator. Reason: preserve cross-project visit behavior independently of
// ordinary home-rail admission. Expiry: PR7.
func revalidateProjectVisitTarget(owner asyncOwner, target asyncTarget) bool {
	if target.policy != asyncTargetProjectVisit || !validAsyncOwner(owner) || !validAsyncTarget(owner, target) {
		return false
	}
	snapshot, err := inventory.Scan(inventory.Options{FilterDir: filepath.Dir(owner.projectID)})
	if err != nil {
		return false
	}
	for _, record := range snapshot.Records {
		if inventory.NormalizePath(record.AgentDir) != target.directory {
			continue
		}
		return record.Enterable && record.PID == target.pid && record.ManifestAddressVerified &&
			canonicalProjectMailIdentity(filepath.Join(record.Project, ".lingtai")) == owner.projectID &&
			fs.AddressFingerprint(record.Address) == target.addressFingerprint
	}
	return false
}

// ordinaryRailRecordEligible is the ordinary home-Agent admission contract. It
// is intentionally separate from global project-visit Enterable semantics.
func ordinaryRailRecordEligible(owner asyncOwner, target asyncTarget, record inventory.Record) bool {
	if target.policy != asyncTargetHomeAgentRail || !validAsyncOwner(owner) || !validAsyncTarget(owner, target) {
		return false
	}
	return inventory.NormalizePath(record.AgentDir) == target.directory &&
		canonicalProjectMailIdentity(filepath.Join(record.Project, ".lingtai")) == owner.projectID &&
		!record.Phantom && record.ReadError == "" && record.ManifestAddressVerified &&
		!record.IsHuman && !record.IsOrchestrator && record.Role == inventory.RoleAgent &&
		record.PID == target.pid &&
		fs.AddressFingerprint(record.Address) == target.addressFingerprint
}

func (s *ProjectMailStore) bindAsyncTargetRevalidator(revalidate func(asyncOwner, asyncTarget) bool) {
	if s == nil || s.revalidateTargetExplicit {
		return
	}
	if revalidate == nil {
		revalidate = rejectUnboundAsyncTarget
	}
	s.revalidateTarget = revalidate
	s.syncAsyncState()
}

func (s *ProjectMailStore) setAsyncTargetRevalidator(revalidate func(asyncOwner, asyncTarget) bool) {
	if s == nil {
		return
	}
	s.revalidateTargetExplicit = revalidate != nil
	if revalidate == nil {
		revalidate = rejectUnboundAsyncTarget
	}
	s.revalidateTarget = revalidate
	s.syncAsyncState()
}

func (s *ProjectMailStore) bindMailModel(mail *MailModel, policy asyncTargetPolicy, pid int) {
	if s == nil || mail == nil || s.id == 0 {
		return
	}
	binding := asyncBinding{
		owner: asyncOwner{
			projectID:  s.projectID,
			storeID:    s.id,
			activation: s.activation,
		},
		target: asyncTarget{
			directory:          inventory.NormalizePath(mail.orchestrator),
			addressFingerprint: fs.AddressFingerprint(mail.orchAddr),
			policy:             policy,
			pid:                pid,
		},
		generation: mail.generation,
	}
	s.binding = binding
	s.locationSourceVersion = s.version
	mail.asyncBinding = binding
	mail.asyncStoreVersion = s.version
	mail.asyncTickEpoch = s.tickChain
	mail.revalidateTarget = s.revalidateTarget
	if mail.pulseEpoch == 0 {
		mail.pulseEpoch = 1
	}
	s.syncAsyncState()
}

func (s ProjectMailStore) asyncCurrent() asyncCurrent {
	return asyncCurrent{
		binding:          s.binding,
		storeVersion:     s.version,
		tickEpoch:        s.tickChain,
		revalidateTarget: s.revalidateTarget,
	}
}

func (s *ProjectMailStore) syncAsyncState() {
	if s == nil || s.asyncState == nil {
		return
	}
	current := s.asyncCurrent()
	current.storeVersion = s.locationSourceVersion
	s.asyncState.store(current)
}

func (s ProjectMailStore) matches(projectDir, humanDir string) bool {
	return s.id != 0 &&
		s.projectID == canonicalProjectMailIdentity(projectDir) &&
		filepath.Clean(s.humanDir) == filepath.Clean(humanDir)
}

func (s *ProjectMailStore) suspend() {
	if s == nil || s.id == 0 {
		return
	}
	s.pauseTick()
	s.active = false
	s.activation++
	s.binding.owner.activation = s.activation
	s.refreshInFlight = false
	s.refreshInitial = false
	s.refreshInFlightEnvelope = asyncEnvelope{}
	s.initialRefreshPending = false
	s.syncAsyncState()
}

func (s *ProjectMailStore) activate() {
	if s == nil || s.id == 0 {
		return
	}
	s.active = true
	s.activation++
	s.binding.owner.activation = s.activation
	s.refreshInFlight = false
	s.refreshInitial = false
	s.refreshInFlightEnvelope = asyncEnvelope{}
	s.initialRefreshPending = false
	s.tickRunning = false
	s.locationSourceVersion = s.version
	s.syncAsyncState()
}

// pauseTick invalidates the outstanding chain even if its tea.Every command has
// already fired. A late message therefore cannot pass shared acceptance and re-arm.
func (s *ProjectMailStore) pauseTick() {
	if s == nil || s.id == 0 {
		return
	}
	s.tickChain++
	s.tickRunning = false
	s.syncAsyncState()
}

// resumeTick creates at most one chain for the current activation.
func (s *ProjectMailStore) resumeTick() tea.Cmd {
	if s == nil || s.id == 0 || !s.active || s.tickRunning {
		return nil
	}
	s.tickChain++
	s.tickRunning = true
	s.syncAsyncState()
	return projectMailTickEvery(s.pollRate, s.asyncCurrent())
}

func projectMailTickEvery(d time.Duration, current asyncCurrent) tea.Cmd {
	envelope := captureAsync(asyncRefreshTick, current)
	return tea.Every(d, func(t time.Time) tea.Msg {
		return projectMailTickMsg{envelope: envelope, at: t}
	})
}

func (s ProjectMailStore) nextTick() tea.Cmd {
	if !s.active || !s.tickRunning {
		return nil
	}
	return projectMailTickEvery(s.pollRate, s.asyncCurrent())
}

func refreshAsyncKind(initial bool) asyncKind {
	if initial {
		return asyncInitialRebuild
	}
	return asyncSteadyRefresh
}

// beginRefresh coalesces every project-mail refresh path onto the one active
// store pipeline. The command works on detached cache/session values; only
// acceptAsync in App.Update can authorize publication.
func (s *ProjectMailStore) beginRefresh(mail MailModel, initial bool) tea.Cmd {
	if s == nil || s.id == 0 || !s.active {
		return nil
	}
	if s.refreshInFlight {
		// A steady scan may be reused only as a cache warm-up. An initial scan
		// satisfies only the MailModel generation that launched it; a replacement
		// generation still needs its own authoritative session rebuild.
		if initial && (!s.refreshInitial || s.refreshInFlightEnvelope.generation.thread != mail.generation) {
			s.initialRefreshPending = true
		}
		return nil
	}
	current := s.asyncCurrent()
	current.storeVersion = s.version
	envelope := captureAsync(refreshAsyncKind(initial), current)
	s.refreshInFlight = true
	s.refreshInitial = initial
	s.refreshInFlightEnvelope = envelope
	cache := s.cache
	scanner := s.scanner
	return func() tea.Msg {
		// Bubble Tea commands are not cancellable after launch. Serialize the
		// complete physical refresh/rebuild body so a newly active visited store
		// waits for a suspended home command instead of scanning concurrently.
		projectMailScanSingleflight.Lock()
		defer projectMailScanSingleflight.Unlock()
		if initial && mail.beforeRebuild != nil {
			mail.beforeRebuild()
		}
		refreshed := scanner.Refresh(cache)
		refresh := mail.collectRefreshState()
		refresh.initial = initial
		if initial {
			refresh.sessionCache = mail.rebuildSession(refreshed)
		}
		return projectMailRefreshMsg{
			envelope: envelope,
			cache:    refreshed,
			mail:     refresh,
		}
	}
}

// settleRefreshWork performs non-publishing execution bookkeeping. Only the
// exact captured physical work token may release the slot; it never installs a
// cache, snapshot, model field, or location update.
func (s *ProjectMailStore) settleRefreshWork(envelope asyncEnvelope) bool {
	if s == nil || !s.refreshInFlight || envelope != s.refreshInFlightEnvelope {
		return false
	}
	s.refreshInFlight = false
	s.refreshInitial = false
	s.refreshInFlightEnvelope = asyncEnvelope{}
	return true
}

// installRefresh publishes a result only after App.Update has accepted its
// envelope and settled its exact physical work token.
func (s *ProjectMailStore) installRefresh(msg projectMailRefreshMsg) *ProjectMailSnapshot {
	if s == nil {
		return nil
	}
	s.cache = msg.cache
	s.version++
	s.snapshot = &ProjectMailSnapshot{version: s.version, cache: msg.cache}
	// A delayed location command reuses the accepted result's source coordinate.
	// A newer accepted refresh replaces this coordinate and rejects the old command.
	s.locationSourceVersion = msg.envelope.storeVersion
	s.syncAsyncState()
	return s.snapshot
}

// beginPendingInitialRefresh starts the authoritative rebuild deferred behind
// older work. A completed-but-rejected exact token may release the slot, but the
// queued current initial must still pass fresh shared acceptance before launch;
// it never inherits permission from the old result.
func (s *ProjectMailStore) beginPendingInitialRefresh(mail MailModel) tea.Cmd {
	if s == nil || !s.active || !s.initialRefreshPending || s.refreshInFlight {
		return nil
	}
	s.initialRefreshPending = false
	current := s.asyncCurrent()
	current.storeVersion = s.version
	envelope := captureAsync(asyncInitialRebuild, current)
	if !acceptAsync(current, envelope) {
		return nil
	}
	return s.beginRefresh(mail, true)
}

func (s ProjectMailStore) locationUpdateCmd(envelope asyncEnvelope) tea.Cmd {
	if s.id == 0 || !s.active || s.updateLocation == nil || s.asyncState == nil {
		return nil
	}
	humanDir := s.humanDir
	update := s.updateLocation
	state := s.asyncState
	return func() tea.Msg {
		current := state.load()
		if !acceptAsync(current, envelope) {
			return nil
		}
		update(humanDir)
		return nil
	}
}
