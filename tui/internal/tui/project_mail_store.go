package tui

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// ProjectMailSnapshot is an immutable, accepted view of one project's human
// mailbox. A refresh result becomes visible only after ProjectMailStore accepts
// its store identity, activation, and source version. The cache remains private
// so callers cannot turn a snapshot into a second refresh owner.
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
	storeID       uint64
	projectID     string
	activation    uint64
	sourceVersion uint64
	cache         fs.MailCache
	mail          mailRefreshMsg
}

type projectMailTickMsg struct {
	storeID    uint64
	activation uint64
	chain      uint64
	at         time.Time
}

type projectMailRefreshRequestMsg struct {
	generation uint64
	initial    bool
}

var projectMailStoreSequence atomic.Uint64

// projectMailScanSingleflight serializes the physical refresh body across store
// identities. A suspended home command cannot be cancelled once Bubble Tea has
// started it, so a newly active visited store waits here instead of scanning in
// parallel. The gate owns no project data, accepted state, or tick lifecycle.
var projectMailScanSingleflight sync.Mutex

// projectMailRuntimeGate is shared by value copies of one store. It lets a
// delayed side-effect command re-check the live activation/version at execution
// time even though Bubble Tea returns App values by copy.
type projectMailRuntimeGate struct {
	active     atomic.Bool
	activation atomic.Uint64
	version    atomic.Uint64
}

// ProjectMailStore is the root-owned project-lifetime mailbox owner. It owns
// exactly one MailCache, one accepted-snapshot sequence, one refresh pipeline,
// and one invalidatable polling chain. Bubble Tea serializes its mutations on
// App.Update; background commands receive detached cache values and can only
// publish through the identity/version acceptance gate below.
type ProjectMailStore struct {
	id                    uint64
	projectID             string
	projectDir            string
	humanDir              string
	cache                 fs.MailCache
	snapshot              *ProjectMailSnapshot
	version               uint64
	activation            uint64
	tickChain             uint64
	active                bool
	tickRunning           bool
	refreshInFlight       bool
	refreshInitial        bool
	refreshGeneration     uint64
	initialRefreshPending bool
	pollRate              time.Duration
	scanner               projectMailScanner
	updateLocation        projectMailLocationUpdater
	runtime               *projectMailRuntimeGate
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
		id:             projectMailStoreSequence.Add(1),
		projectID:      canonicalProjectMailIdentity(projectDir),
		projectDir:     projectDir,
		humanDir:       humanDir,
		cache:          fs.NewMailCache(humanDir),
		activation:     1,
		active:         true,
		pollRate:       time.Second,
		scanner:        scanner,
		updateLocation: updateLocation,
		runtime:        &projectMailRuntimeGate{},
	}
	store.syncRuntime()
	return store
}

func (s *ProjectMailStore) syncRuntime() {
	if s == nil || s.id == 0 {
		return
	}
	if s.runtime == nil {
		s.runtime = &projectMailRuntimeGate{}
	}
	s.runtime.active.Store(s.active)
	s.runtime.activation.Store(s.activation)
	s.runtime.version.Store(s.version)
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
	s.refreshInFlight = false
	s.refreshInitial = false
	s.refreshGeneration = 0
	s.initialRefreshPending = false
	s.syncRuntime()
}

func (s *ProjectMailStore) activate() {
	if s == nil || s.id == 0 {
		return
	}
	s.active = true
	s.activation++
	s.refreshInFlight = false
	s.refreshInitial = false
	s.refreshGeneration = 0
	s.initialRefreshPending = false
	s.tickRunning = false
	s.syncRuntime()
}

// pauseTick invalidates the outstanding chain even if its tea.Every command
// has already fired. A late message therefore cannot pass acceptTick and re-arm.
func (s *ProjectMailStore) pauseTick() {
	if s == nil || s.id == 0 {
		return
	}
	s.tickChain++
	s.tickRunning = false
}

// resumeTick creates at most one chain for the current activation.
func (s *ProjectMailStore) resumeTick() tea.Cmd {
	if s == nil || s.id == 0 || !s.active || s.tickRunning {
		return nil
	}
	s.tickChain++
	s.tickRunning = true
	return projectMailTickEvery(s.pollRate, s.id, s.activation, s.tickChain)
}

func projectMailTickEvery(d time.Duration, storeID, activation, chain uint64) tea.Cmd {
	return tea.Every(d, func(t time.Time) tea.Msg {
		return projectMailTickMsg{storeID: storeID, activation: activation, chain: chain, at: t}
	})
}

func (s ProjectMailStore) acceptsTick(msg projectMailTickMsg) bool {
	return s.id != 0 && s.active && s.tickRunning &&
		msg.storeID == s.id && msg.activation == s.activation && msg.chain == s.tickChain
}

func (s ProjectMailStore) nextTick() tea.Cmd {
	if !s.active || !s.tickRunning {
		return nil
	}
	return projectMailTickEvery(s.pollRate, s.id, s.activation, s.tickChain)
}

// beginRefresh coalesces every project-mail refresh path onto the one active
// store pipeline. The command works on detached cache/session values; only
// acceptRefresh can install its result.
func (s *ProjectMailStore) beginRefresh(mail MailModel, initial bool) tea.Cmd {
	if s == nil || s.id == 0 || !s.active {
		return nil
	}
	if s.refreshInFlight {
		// A steady scan may be reused only as a cache warm-up. An initial scan
		// satisfies only the MailModel generation that launched it; a replacement
		// generation still needs its own authoritative session rebuild.
		if initial && (!s.refreshInitial || s.refreshGeneration != mail.generation) {
			s.initialRefreshPending = true
		}
		return nil
	}
	s.refreshInFlight = true
	s.refreshInitial = initial
	s.refreshGeneration = mail.generation
	storeID := s.id
	projectID := s.projectID
	activation := s.activation
	sourceVersion := s.version
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
		refresh.generation = mail.generation
		refresh.initial = initial
		if initial {
			refresh.sessionCache = mail.rebuildSession(refreshed)
		}
		return projectMailRefreshMsg{
			storeID:       storeID,
			projectID:     projectID,
			activation:    activation,
			sourceVersion: sourceVersion,
			cache:         refreshed,
			mail:          refresh,
		}
	}
}

// acceptRefresh distinguishes physical completion from publication. A result
// for the current store/source releases the one in-flight slot even when its
// MailModel generation was superseded, but only the current generation may
// replace root cache/version/snapshot state.
func (s *ProjectMailStore) acceptRefresh(msg projectMailRefreshMsg, generation uint64) (*ProjectMailSnapshot, bool, bool) {
	if s == nil || s.id == 0 || !s.active ||
		msg.storeID != s.id || msg.projectID != s.projectID ||
		msg.activation != s.activation || msg.sourceVersion != s.version {
		return nil, false, false
	}
	s.refreshInFlight = false
	s.refreshInitial = false
	s.refreshGeneration = 0
	if msg.mail.generation != generation {
		return nil, false, true
	}
	s.cache = msg.cache
	s.version++
	s.snapshot = &ProjectMailSnapshot{version: s.version, cache: msg.cache}
	s.syncRuntime()
	return s.snapshot, true, true
}

// beginPendingInitialRefresh starts the authoritative rebuild deferred behind
// an older steady scan. The completed steady result may itself be rejected for
// a superseded MailModel generation; the pending current initial still starts
// after that exact physical slot is released.
func (s *ProjectMailStore) beginPendingInitialRefresh(mail MailModel) tea.Cmd {
	if s == nil || !s.active || !s.initialRefreshPending || s.refreshInFlight {
		return nil
	}
	s.initialRefreshPending = false
	return s.beginRefresh(mail, true)
}

func (s ProjectMailStore) locationUpdateCmd() tea.Cmd {
	if s.id == 0 || !s.active || s.updateLocation == nil || s.runtime == nil {
		return nil
	}
	humanDir := s.humanDir
	update := s.updateLocation
	runtime := s.runtime
	activation := s.activation
	version := s.version
	return func() tea.Msg {
		// The command may execute after a visit switch, store suspension, or a
		// newer accepted snapshot. Re-check the shared live token immediately
		// before the side effect so only the current accepted owner may update.
		if !runtime.active.Load() ||
			runtime.activation.Load() != activation ||
			runtime.version.Load() != version {
			return nil
		}
		update(humanDir)
		return nil
	}
}
