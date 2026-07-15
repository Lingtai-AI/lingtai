package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

type countingProjectMailScanner struct{ scans atomic.Int64 }

func (s *countingProjectMailScanner) Refresh(cache fs.MailCache) fs.MailCache {
	s.scans.Add(1)
	return cache.Refresh()
}

type blockingProjectMailScanner struct {
	started   chan struct{}
	release   chan struct{}
	active    atomic.Int64
	maxActive atomic.Int64
}

func newBlockingProjectMailScanner() *blockingProjectMailScanner {
	return &blockingProjectMailScanner{
		started: make(chan struct{}, 2),
		release: make(chan struct{}, 2),
	}
}

func (s *blockingProjectMailScanner) Refresh(cache fs.MailCache) fs.MailCache {
	active := s.active.Add(1)
	for {
		maximum := s.maxActive.Load()
		if active <= maximum || s.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	s.started <- struct{}{}
	<-s.release
	s.active.Add(-1)
	return cache
}

func waitProjectMailSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

// TestProjectMailStoreOwnsTheOnlyGlobalCache is the structural and scanner
// ownership contract. Activating N cold target-local ThreadStates keeps one root
// store, and coalesced refresh requests perform one mailbox scan.
func TestProjectMailStoreOwnsTheOnlyGlobalCache(t *testing.T) {
	mailCacheType := reflect.TypeOf(fs.MailCache{})
	mailModelType := reflect.TypeOf(MailModel{})
	for i := 0; i < mailModelType.NumField(); i++ {
		field := mailModelType.Field(i)
		if field.Type == mailCacheType {
			t.Fatalf("MailModel still owns project-wide cache field %q; N target models can create N global scan owners", field.Name)
		}
	}

	a := visitTestApp(t)
	scanner := &countingProjectMailScanner{}
	a.mailStore = newProjectMailStoreWithDeps(a.projectDir, a.mail.humanDir, scanner, func(string) {})
	a.installMailModel(a.mail)
	storeID := a.mailStore.id
	for i := 0; i < 8; i++ {
		target := asyncTarget{
			policy:    asyncTargetHomeAgentRail,
			directory: filepath.Join(a.projectDir, "target"),
			pid:       i + 1,
		}
		a.currentThread = newColdThreadState(
			target,
			uint64(i+1),
			a.mailStore.version,
			fs.NewSessionCache(a.mail.humanDir, filepath.Dir(a.projectDir), fs.NoPersist),
		)
		if a.mailStore.id != storeID {
			t.Fatalf("cold ThreadState activation %d replaced project store: got %d want %d", i, a.mailStore.id, storeID)
		}
	}
	first := a.beginProjectMailRefresh(false)
	if first == nil {
		t.Fatal("first root refresh was not scheduled")
	}
	if duplicate := a.beginProjectMailRefresh(false); duplicate != nil {
		t.Fatal("same-store refresh was not coalesced")
	}
	_ = first()
	if got := scanner.scans.Load(); got != 1 {
		t.Fatalf("N target models performed %d mailbox scans, want one root scan", got)
	}
}

func TestProjectMailTargetRevalidationBlocksDirectAndPendingInitialLaunch(t *testing.T) {
	a := visitTestApp(t)
	a.visiting = true
	a.visitTargetPID = 42
	a.installMailModel(a.mail)

	calls := 0
	a.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool {
		calls++
		return false
	})
	if cmd := a.beginProjectMailRefresh(true); cmd != nil {
		t.Fatalf("direct initial launch bypassed target revalidation and returned %T", cmd())
	}
	if calls != 1 {
		t.Fatalf("direct initial launch revalidator calls = %d, want 1", calls)
	}

	calls = 0
	a.mailStore.initialRefreshPending = true
	if cmd := a.mailStore.beginPendingInitialRefresh(a.mail); cmd != nil {
		t.Fatalf("pending initial relaunch bypassed target revalidation and returned %T", cmd())
	}
	if calls != 1 {
		t.Fatalf("pending initial relaunch revalidator calls = %d, want 1", calls)
	}
	if a.mailStore.initialRefreshPending {
		t.Fatal("rejected pending initial remained queued after its launch opportunity")
	}
}

func newNirvanaAsyncContinuationApp(t *testing.T, eventCount int) App {
	t.Helper()
	sourceDir := buildWindowedAgentDir(t, eventCount)
	projectDir := t.TempDir()
	orchDir := filepath.Join(projectDir, "Main")
	if err := os.Rename(sourceDir, orchDir); err != nil {
		t.Fatal(err)
	}
	humanDir := filepath.Join(projectDir, "human")
	a := App{
		projectDir:  projectDir,
		globalDir:   t.TempDir(),
		currentView: appViewMail,
		orchDir:     orchDir,
		orchName:    "Main",
	}
	a.mailStore = newProjectMailStore(projectDir, humanDir)
	a.installMailModel(NewMailModel(humanDir, "human", projectDir, orchDir, "Main", 100, a.globalDir, "en", false, 0))
	return a
}

func runNirvanaCleanupToCompletedScreen(t *testing.T, a App) App {
	t.Helper()
	a.pauseProjectMail()
	a.currentView = appViewNirvana
	a.nirvana = NewNirvanaModel(a.projectDir)
	a.nirvana.cursor = 0

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a = model.(App)
	for step := 0; step < 3; step++ {
		if cmd == nil {
			t.Fatalf("Nirvana cleanup stopped before the completed screen at step %d", step)
		}
		model, cmd = a.Update(cmd())
		a = model.(App)
		if a.nirvana.done {
			if cmd != nil {
				t.Fatalf("completed Nirvana screen returned unexpected command %T", cmd())
			}
			if _, err := os.Stat(a.projectDir); !os.IsNotExist(err) {
				t.Fatalf("Nirvana cleanup did not remove project before completed screen: %v", err)
			}
			return a
		}
	}
	t.Fatal("Nirvana cleanup did not reach the completed screen")
	return App{}
}

func TestNirvanaInvalidatesMailLocalAsyncContinuations(t *testing.T) {
	resetApp := func(t *testing.T, a App) App {
		t.Helper()
		if err := os.RemoveAll(a.projectDir); err != nil {
			t.Fatal(err)
		}
		a.currentView = appViewNirvana
		a.nirvana = NewNirvanaModel(a.projectDir)
		a.nirvana.done = true
		model, _ := a.Update(NirvanaDoneMsg{})
		return model.(App)
	}

	t.Run("persist", func(t *testing.T) {
		a := newNirvanaAsyncContinuationApp(t, 50)
		initial := detachedAppProjectMailRefresh(&a, true)
		model, postFrame := a.Update(initial)
		a = model.(App)
		if a.mail.sessionCache == nil || !a.mail.sessionCache.Complete() {
			t.Fatal("precondition: initial rebuild did not produce a complete persistable cache")
		}
		persist := runMailPersistCmd(t, postFrame)
		humanDir := a.mail.humanDir
		reset := resetApp(t, a)
		sessionPath := filepath.Join(humanDir, "logs", "session.jsonl")
		if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
			t.Fatalf("precondition: recreated project already has stale session file: %v", err)
		}

		model, cmd := reset.Update(persist)
		if cmd != nil {
			t.Errorf("late pre-Nirvana persist returned command %T", runCmd(cmd))
		}
		if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
			t.Fatalf("late pre-Nirvana persist wrote into recreated project: %v", err)
		}
		got := model.(App)
		if got.mail.sessionCache != reset.mail.sessionCache {
			t.Fatal("late pre-Nirvana persist replaced Mail state")
		}
	})

	t.Run("older page", func(t *testing.T) {
		a := newNirvanaAsyncContinuationApp(t, 250)
		initial := detachedAppProjectMailRefresh(&a, true)
		model, _ := a.Update(initial)
		a = model.(App)
		a.mail, _ = a.mail.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		a.mail.viewport.GotoTop()
		var olderCmd tea.Cmd
		a.mail, olderCmd = a.mail.Update(ctrlU())
		if olderCmd == nil || !a.mail.olderLoadInFlight {
			t.Fatal("precondition: partial history did not launch an older-page continuation")
		}
		older, ok := olderCmd().(mailOlderPageMsg)
		if !ok {
			t.Fatalf("precondition: older-page command returned %T", olderCmd())
		}
		reset := resetApp(t, a)
		beforeCache := reset.mail.sessionCache
		beforeWindow := reset.mail.ingestWindow
		beforeExtra := reset.mail.loadedExtra
		beforeMessages := len(reset.mail.messages)

		model, cmd := reset.Update(older)
		if cmd != nil {
			t.Fatalf("late pre-Nirvana older page scheduled %T", runCmd(cmd))
		}
		got := model.(App)
		if got.mail.sessionCache != beforeCache || got.mail.ingestWindow != beforeWindow || got.mail.loadedExtra != beforeExtra || len(got.mail.messages) != beforeMessages {
			t.Fatalf("late pre-Nirvana older page mutated recreated lifetime: cacheChanged=%v window=%d extra=%d messages=%d", got.mail.sessionCache != beforeCache, got.mail.ingestWindow, got.mail.loadedExtra, len(got.mail.messages))
		}
	})
}

func TestNirvanaInvalidatesAsyncBeforeDestructiveCleanup(t *testing.T) {
	t.Run("refresh", func(t *testing.T) {
		a := newNirvanaAsyncContinuationApp(t, 50)
		refresh := detachedAppProjectMailRefresh(&a, true)
		cleaned := runNirvanaCleanupToCompletedScreen(t, a)
		beforeSnapshot := cleaned.mail.acceptedSnapshot
		beforeCache := cleaned.mail.sessionCache

		model, cmd := cleaned.Update(refresh)
		if cmd != nil {
			t.Fatalf("late pre-cleanup refresh returned command %T before final Enter", runCmd(cmd))
		}
		got := model.(App)
		if got.mail.acceptedSnapshot != beforeSnapshot || got.mail.sessionCache != beforeCache {
			t.Fatal("late pre-cleanup refresh published into the destroyed lifetime before final Enter")
		}
		if _, err := os.Stat(cleaned.projectDir); !os.IsNotExist(err) {
			t.Fatalf("late pre-cleanup refresh recreated the deleted project: %v", err)
		}
	})

	t.Run("persist", func(t *testing.T) {
		a := newNirvanaAsyncContinuationApp(t, 50)
		initial := detachedAppProjectMailRefresh(&a, true)
		model, postFrame := a.Update(initial)
		a = model.(App)
		if a.mail.sessionCache == nil || !a.mail.sessionCache.Complete() {
			t.Fatal("precondition: initial rebuild did not produce a complete persistable cache")
		}
		persist := runMailPersistCmd(t, postFrame)
		humanDir := a.mail.humanDir
		cleaned := runNirvanaCleanupToCompletedScreen(t, a)
		sessionPath := filepath.Join(humanDir, "logs", "session.jsonl")

		model, cmd := cleaned.Update(persist)
		if cmd != nil {
			t.Fatalf("late pre-cleanup persist returned command %T before final Enter", runCmd(cmd))
		}
		if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
			t.Fatalf("late pre-cleanup persist recreated deleted session state before final Enter: %v", err)
		}
		got := model.(App)
		if got.mail.sessionCache != cleaned.mail.sessionCache {
			t.Fatal("late pre-cleanup persist replaced Mail state before final Enter")
		}
	})

	t.Run("older page", func(t *testing.T) {
		a := newNirvanaAsyncContinuationApp(t, 250)
		initial := detachedAppProjectMailRefresh(&a, true)
		model, _ := a.Update(initial)
		a = model.(App)
		a.mail, _ = a.mail.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		a.mail.viewport.GotoTop()
		var olderCmd tea.Cmd
		a.mail, olderCmd = a.mail.Update(ctrlU())
		if olderCmd == nil || !a.mail.olderLoadInFlight {
			t.Fatal("precondition: partial history did not launch an older-page continuation")
		}
		older, ok := olderCmd().(mailOlderPageMsg)
		if !ok {
			t.Fatalf("precondition: older-page command returned %T", olderCmd())
		}
		cleaned := runNirvanaCleanupToCompletedScreen(t, a)
		beforeCache := cleaned.mail.sessionCache
		beforeWindow := cleaned.mail.ingestWindow
		beforeExtra := cleaned.mail.loadedExtra
		beforeMessages := len(cleaned.mail.messages)

		model, cmd := cleaned.Update(older)
		if cmd != nil {
			t.Fatalf("late pre-cleanup older page scheduled %T before final Enter", runCmd(cmd))
		}
		got := model.(App)
		if got.mail.sessionCache != beforeCache || got.mail.ingestWindow != beforeWindow || got.mail.loadedExtra != beforeExtra || len(got.mail.messages) != beforeMessages {
			t.Fatalf("late pre-cleanup older page mutated destroyed lifetime before final Enter: cacheChanged=%v window=%d extra=%d messages=%d", got.mail.sessionCache != beforeCache, got.mail.ingestWindow, got.mail.loadedExtra, len(got.mail.messages))
		}
		if _, err := os.Stat(cleaned.projectDir); !os.IsNotExist(err) {
			t.Fatalf("late pre-cleanup older page recreated the deleted project: %v", err)
		}
	})
}

func TestNirvanaCancelPreservesAsyncLifetime(t *testing.T) {
	a := newNirvanaAsyncContinuationApp(t, 50)
	beforeGeneration := a.mail.generation
	beforeBinding := a.mail.asyncBinding
	a.pauseProjectMail()
	a.currentView = appViewNirvana
	a.nirvana = NewNirvanaModel(a.projectDir)

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a = model.(App)
	if cmd == nil {
		t.Fatal("default Nirvana cancel did not return to mail")
	}
	if a.mail.generation != beforeGeneration || a.mail.asyncBinding != beforeBinding {
		t.Fatal("pre-confirmation Nirvana cancel invalidated the Mail lifetime")
	}
	model, _ = a.Update(cmd())
	a = model.(App)
	if a.currentView != appViewMail {
		t.Fatalf("Nirvana cancel returned to view %d, want mail", a.currentView)
	}
	if a.mail.generation == 0 || a.mail.asyncBinding.owner.storeID == 0 {
		t.Fatal("returning from Nirvana cancel left Mail without a live async lifetime")
	}
	if _, err := os.Stat(a.projectDir); err != nil {
		t.Fatalf("Nirvana cancel changed the project filesystem: %v", err)
	}
}

func TestNirvanaCleanupStartIsSingleUseBeforeDestructiveCommand(t *testing.T) {
	a := newNirvanaAsyncContinuationApp(t, 50)
	a.pauseProjectMail()
	a.currentView = appViewNirvana
	a.nirvana = NewNirvanaModel(a.projectDir)
	a.nirvana.cursor = 0 // Confirm.

	model, startCmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a = model.(App)
	if startCmd == nil {
		t.Fatal("confirmed Nirvana did not emit cleanup-start command")
	}
	startMsg := startCmd()
	if _, ok := startMsg.(nirvanaCleanStartMsg); !ok {
		t.Fatalf("confirmed Nirvana emitted %T, want nirvanaCleanStartMsg", startMsg)
	}
	beforeGeneration := a.mail.generation

	model, cleanupCmd := a.Update(startMsg)
	started := model.(App)
	if cleanupCmd == nil {
		t.Fatal("first cleanup-start did not return destructive cleanup command")
	}
	if _, err := os.Stat(started.projectDir); err != nil {
		t.Fatalf("destructive cleanup ran before the root invalidation boundary returned: %v", err)
	}
	if started.mailStore.id != 0 || started.mailStore.active || started.suspendedHomeMailStore != nil || started.visiting || started.visitReturn != nil {
		t.Fatalf("cleanup-start retained project Mail ownership: store=%+v suspended=%v visiting=%v return=%v", started.mailStore, started.suspendedHomeMailStore != nil, started.visiting, started.visitReturn != nil)
	}
	if started.mail.generation == beforeGeneration || started.mail.asyncBinding.owner.storeID != 0 {
		t.Fatalf("cleanup-start did not invalidate Mail-local async identity: generation=%d binding=%+v", started.mail.generation, started.mail.asyncBinding)
	}

	model, duplicateCmd := started.Update(startMsg)
	duplicate := model.(App)
	if duplicateCmd != nil {
		t.Fatal("duplicate cleanup-start returned a second destructive cleanup command")
	}
	if duplicate.mail.generation != started.mail.generation || duplicate.mail.asyncBinding != started.mail.asyncBinding {
		t.Fatal("duplicate cleanup-start mutated the already-retired Mail lifetime")
	}
}

func TestNirvanaDoneRequiresCompletedActiveScreen(t *testing.T) {
	a := newNirvanaAsyncContinuationApp(t, 50)
	beforeView := a.currentView
	beforeGeneration := a.mail.generation
	beforeBinding := a.mail.asyncBinding
	beforeStore := a.mailStore

	model, cmd := a.Update(NirvanaDoneMsg{})
	got := model.(App)
	if cmd != nil {
		t.Fatal("premature NirvanaDoneMsg scheduled project recreation")
	}
	if got.currentView != beforeView {
		t.Fatalf("premature NirvanaDoneMsg changed view to %d, want %d", got.currentView, beforeView)
	}
	if got.mail.generation != beforeGeneration || got.mail.asyncBinding != beforeBinding ||
		got.mailStore.id != beforeStore.id || got.mailStore.active != beforeStore.active ||
		got.mailStore.activation != beforeStore.activation || got.mailStore.tickChain != beforeStore.tickChain ||
		got.mailStore.binding != beforeStore.binding {
		t.Fatal("premature NirvanaDoneMsg invalidated the live project Mail lifetime")
	}
	if _, err := os.Stat(got.projectDir); err != nil {
		t.Fatalf("premature NirvanaDoneMsg changed the project filesystem: %v", err)
	}
}

// TestShortMailOtherMailCycleRejectsLateTick exercises a cycle shorter than
// the polling interval. The old chain has not fired yet when mail resumes, so
// its eventual tick must be rejected and must not re-arm itself.
func TestShortMailOtherMailCycleRejectsLateTick(t *testing.T) {
	a := visitTestApp(t)
	_ = a.mailStore.resumeTick()
	old := projectMailTickMsg{envelope: captureAsync(asyncRefreshTick, a.asyncCurrent()), at: time.Now()}

	model, _ := a.switchToView("help")
	a = model.(App)
	model, _ = a.switchToView("mail")
	a = model.(App)

	_, cmd := a.Update(old)
	if cmd != nil {
		t.Fatalf("late tick from pre-pause chain re-armed after mail→other→mail; command produced %T", runCmd(cmd))
	}
}

// TestRepeatedMarkdownCloseDoesNotRestartMailTick verifies that idempotent
// close/return delivery cannot create an additional polling chain.
func TestRepeatedMarkdownCloseDoesNotRestartMailTick(t *testing.T) {
	a := visitTestApp(t)
	_ = a.mailStore.resumeTick()
	a.pauseProjectMail()
	a.currentView = appViewHelp
	a.help = NewHelpModel()

	model, _ := a.Update(MarkdownViewerCloseMsg{})
	a = model.(App)
	chain := a.mailStore.tickChain
	model, _ = a.Update(MarkdownViewerCloseMsg{})
	a = model.(App)
	if a.mailStore.tickChain != chain {
		t.Fatalf("repeated markdown close created another chain: got %d want %d", a.mailStore.tickChain, chain)
	}
}

// TestVisitStoreIdentityRejectsWrongProjectRefresh proves project identity is
// independent of MailModel's target generation.
func TestVisitStoreIdentityRejectsWrongProjectRefresh(t *testing.T) {
	a := visitTestApp(t)
	homeRefresh := detachedAppProjectMailRefresh(&a, false)
	visited, _ := a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(t.TempDir(), "worker", "Worker")})
	visitedVersion := visited.mailStore.version

	model, cmd := visited.Update(homeRefresh)
	if cmd != nil {
		t.Fatalf("wrong-project refresh returned a command: %T", runCmd(cmd))
	}
	got := model.(App)
	if got.mailStore.version != visitedVersion || got.mail.acceptedSnapshot != nil {
		t.Fatal("visited project accepted a refresh from the suspended home store")
	}
}

// TestHomeVisitedBackHasOneActiveStore checks the explicit visit lifecycle.
func TestHomeVisitedBackHasOneActiveStore(t *testing.T) {
	a := visitTestApp(t)
	homeRefresh := detachedAppProjectMailRefresh(&a, false)
	homeSnapshot, accepted, _ := acceptStoreRefreshForTest(&a.mailStore, homeRefresh)
	if !accepted {
		t.Fatal("failed to seed the accepted home snapshot")
	}
	a.mail.acceptedSnapshot = homeSnapshot
	homeID := a.mailStore.id
	visited, _ := a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(t.TempDir(), "worker", "Worker")})
	if visited.mailStore.id == homeID || !visited.mailStore.active {
		t.Fatal("visited project did not become the sole active store")
	}
	if visited.suspendedHomeMailStore == nil || visited.suspendedHomeMailStore.active || visited.suspendedHomeMailStore.tickRunning {
		t.Fatal("home store was not suspended with its tick stopped")
	}
	visitedID := visited.mailStore.id
	restored, _ := visited.returnFromVisit()
	if restored.mailStore.id != homeID || !restored.mailStore.active {
		t.Fatal("back did not reactivate the original home store")
	}
	if restored.mailStore.id == visitedID || restored.suspendedHomeMailStore != nil || restored.visiting {
		t.Fatal("back retained visited ownership or suspended-home state")
	}
	if !restored.mail.initialLoading || restored.mail.acceptedSnapshot != nil {
		t.Fatal("home state was published before its fresh accepted restore refresh")
	}
}

// TestVisitReturnToProjectsKeepsHomeMailPaused preserves the ordinary
// non-Mail lifecycle: restoring project ownership must not start mailbox work
// until the user actually opens Mail again.
func TestVisitReturnToProjectsKeepsHomeMailPaused(t *testing.T) {
	a := visitTestApp(t)
	a.currentView = appViewProjects
	a.mailStore.pauseTick()

	visited, _ := a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(t.TempDir(), "worker", "Worker")})
	restored, cmd := visited.returnFromVisit()
	if restored.currentView != appViewProjects {
		t.Fatalf("return view = %v, want Projects", restored.currentView)
	}
	if cmd != nil {
		t.Fatal("return to Projects started mailbox work before Mail became current")
	}
	if !restored.mailStore.active || restored.mailStore.tickRunning || restored.mailStore.refreshInFlight {
		t.Fatal("restored home store did not remain active-but-paused in Projects")
	}
}

// TestVisitReturnToProjectsThenMailRequiresInitialRefresh closes the publication
// barrier: when the restored Projects view later opens Mail, the suspended home
// snapshot must still be withheld until an authoritative initial rebuild lands.
func TestVisitReturnToProjectsThenMailRequiresInitialRefresh(t *testing.T) {
	a := visitTestApp(t)
	a.currentView = appViewProjects
	a.mailStore.pauseTick()

	visited, _ := a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(t.TempDir(), "worker", "Worker")})
	restored, _ := visited.returnFromVisit()
	if !restored.mail.initialLoading || restored.mail.acceptedSnapshot != nil {
		t.Fatal("precondition: Projects return did not retain the restore publication barrier")
	}

	model, cmd := restored.switchToView("mail")
	got := model.(App)
	refresh, ok := findProjectMailRefresh(cmd)
	if !ok {
		t.Fatal("opening Mail after Projects return did not schedule a project refresh")
	}
	if !refresh.mail.initial || refresh.mail.sessionCache == nil {
		t.Fatal("opening Mail after Projects return used a steady refresh instead of the required authoritative initial rebuild")
	}
	if !got.mailStore.tickRunning {
		t.Fatal("opening Mail after Projects return did not resume the sole store tick")
	}
}

// TestUnknownViewDoesNotPauseProjectMailStore preserves no-op navigation. A
// removed, stale, or otherwise unknown view request must not invalidate the
// current Mail polling chain when the App remains in Mail.
func TestUnknownViewDoesNotPauseProjectMailStore(t *testing.T) {
	a := visitTestApp(t)
	_ = a.mailStore.resumeTick()
	chain := a.mailStore.tickChain

	model, cmd := a.switchToView("agora")
	got := model.(App)
	if cmd != nil {
		t.Fatal("unknown view returned a command")
	}
	if got.currentView != appViewMail {
		t.Fatalf("unknown view changed current view to %v", got.currentView)
	}
	if !got.mailStore.tickRunning || got.mailStore.tickChain != chain {
		t.Fatalf("unknown no-op navigation invalidated Mail tick: running=%v chain=%d want=%d", got.mailStore.tickRunning, got.mailStore.tickChain, chain)
	}
}

func TestProjectMailStoreRejectsStaleRefreshAndUpdatesLocationOnce(t *testing.T) {
	mail := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "agent", 20, "", "en", false, 0)
	var locations atomic.Int64
	store := newProjectMailStoreWithDeps(mail.baseDir, mail.humanDir, filesystemProjectMailScanner{}, func(string) {
		locations.Add(1)
	})
	bindExistingStoreMailForAsyncTest(t, &store, &mail, 1)
	old := store.beginRefresh(mail, false)().(projectMailRefreshMsg)
	store.suspend()
	store.activate()
	bindExistingStoreMailForAsyncTest(t, &store, &mail, mail.generation)
	current := store.beginRefresh(mail, false)().(projectMailRefreshMsg)

	if _, ok, _ := acceptStoreRefreshForTest(&store, old); ok {
		t.Fatal("stale pre-suspend refresh was accepted")
	}
	if _, ok, _ := acceptStoreRefreshForTest(&store, current); !ok {
		t.Fatal("current refresh was rejected")
	}
	if cmd := store.locationUpdateCmd(current.envelope); cmd == nil {
		t.Fatal("accepted active store did not own location update")
	} else {
		_ = cmd()
	}
	if got := locations.Load(); got != 1 {
		t.Fatalf("accepted refresh produced %d location updates, want one", got)
	}
}

// TestDelayedRefreshRequestOutsideMailCannotRestartStore covers a command that
// was emitted while Mail was current but delivered after the user left Mail.
// The request must not scan or restart a polling chain in an irrelevant view.
func TestDelayedRefreshRequestOutsideMailCannotRestartStore(t *testing.T) {
	a := visitTestApp(t)
	delayed := a.mail.requestMailRefresh(false)().(projectMailRefreshRequestMsg)

	model, _ := a.switchToView("help")
	a = model.(App)
	if a.mailStore.tickRunning {
		t.Fatal("leaving Mail did not pause the project-mail tick")
	}

	model, cmd := a.Update(delayed)
	got := model.(App)
	if cmd != nil {
		t.Fatal("delayed Mail refresh request returned work in Help")
	}
	if got.mailStore.tickRunning || got.mailStore.refreshInFlight {
		t.Fatal("delayed Mail refresh request reactivated tick or refresh ownership outside Mail")
	}
}

// TestInitialRefreshQueuesBehindSteadyRefresh proves coalescing cannot discard
// a required authoritative rebuild. Once the older steady scan completes, the
// store must schedule the pending initial refresh rather than leave Mail in its
// loading state indefinitely.
func TestInitialRefreshQueuesBehindSteadyRefresh(t *testing.T) {
	a := visitTestApp(t)
	steadyCmd := a.beginProjectMailRefresh(false)
	if steadyCmd == nil {
		t.Fatal("steady refresh was not scheduled")
	}
	if cmd := a.beginProjectMailRefresh(true); cmd != nil {
		t.Fatal("required initial refresh bypassed the one in-flight pipeline")
	}

	steady := steadyCmd().(projectMailRefreshMsg)
	model, cmd := a.Update(steady)
	got := model.(App)
	initial, ok := findProjectMailRefresh(cmd)
	if !ok {
		t.Fatal("required initial refresh was lost behind the older steady refresh")
	}
	if !initial.mail.initial || initial.mail.sessionCache == nil {
		t.Fatal("queued refresh did not perform the authoritative initial session rebuild")
	}
	if !got.mailStore.refreshInFlight {
		t.Fatal("store did not retain ownership of the queued initial refresh")
	}
}

// TestSupersededMailGenerationCannotInstallRootRefresh covers a same-store
// replacement such as a mail-page-size change. The physical steady refresh may
// finish, but its old MailModel generation must release the pipeline without
// mutating the root cache/version/snapshot or running a location update. A
// queued authoritative initial for the new generation must then proceed.
func TestSupersededMailGenerationCannotInstallRootRefresh(t *testing.T) {
	a := visitTestApp(t)
	var locations atomic.Int64
	a.mailStore = newProjectMailStoreWithDeps(a.projectDir, a.mail.humanDir, filesystemProjectMailScanner{}, func(string) {
		locations.Add(1)
	})
	policy, pid := a.currentMailTargetPolicy(a.mail)
	a.mailStore.bindMailModel(&a.mail, policy, pid)
	a.mail.acceptedSnapshot = nil

	steadyCmd := a.beginProjectMailRefresh(false)
	if steadyCmd == nil {
		t.Fatal("pre-replacement steady refresh was not scheduled")
	}
	steady := steadyCmd().(projectMailRefreshMsg)
	oldGeneration := steady.envelope.generation.thread
	beforeVersion := a.mailStore.version

	a.installMailModel(NewMailModel(a.mail.humanDir, "human", a.projectDir, a.orchDir, a.orchName, 500, a.globalDir, "en", false, 0))
	if a.mail.generation == oldGeneration {
		t.Fatal("precondition: replacement did not create a new MailModel generation")
	}
	if cmd := a.beginProjectMailRefresh(true); cmd != nil {
		t.Fatal("new-generation initial bypassed the in-flight steady refresh")
	}

	model, followup := a.Update(steady)
	got := model.(App)
	if got.mailStore.version != beforeVersion || got.mailStore.snapshot != nil {
		t.Fatalf("old generation installed root state before MailModel rejection: version=%d want=%d snapshot=%v", got.mailStore.version, beforeVersion, got.mailStore.snapshot != nil)
	}
	initial, ok := findProjectMailRefresh(followup)
	if !ok {
		t.Fatal("old generation did not release the pipeline for the queued authoritative initial")
	}
	if !initial.mail.initial || initial.envelope.generation.thread != got.mail.generation || initial.mail.sessionCache == nil {
		t.Fatalf("follow-up refresh = initial %v generation %d sessionCache %v; want authoritative generation %d", initial.mail.initial, initial.envelope.generation.thread, initial.mail.sessionCache != nil, got.mail.generation)
	}
	if locations.Load() != 0 {
		t.Fatal("rejected old-generation refresh ran a human-location update")
	}
}

// TestSupersededInitialRefreshQueuesReplacementInitial covers the same-store
// replacement ordering where the old generation already owns an authoritative
// initial rebuild. The replacement generation still needs its own initial
// rebuild; otherwise only steady polling remains and Mail stays loading forever.
func TestSupersededInitialRefreshQueuesReplacementInitial(t *testing.T) {
	a := visitTestApp(t)
	a.mail.acceptedSnapshot = nil

	oldInitialCmd := a.beginProjectMailRefresh(true)
	if oldInitialCmd == nil {
		t.Fatal("pre-replacement initial refresh was not scheduled")
	}
	oldInitial := oldInitialCmd().(projectMailRefreshMsg)
	oldGeneration := oldInitial.envelope.generation.thread

	a.installMailModel(NewMailModel(a.mail.humanDir, "human", a.projectDir, a.orchDir, a.orchName, 500, a.globalDir, "en", false, 0))
	if a.mail.generation == oldGeneration {
		t.Fatal("precondition: replacement did not create a new MailModel generation")
	}
	if !a.mail.initialLoading {
		t.Fatal("precondition: replacement MailModel did not start in loading state")
	}
	if cmd := a.beginProjectMailRefresh(true); cmd != nil {
		t.Fatal("new-generation initial bypassed the in-flight old-generation initial")
	}

	model, followup := a.Update(oldInitial)
	got := model.(App)
	initial, ok := findProjectMailRefresh(followup)
	if !ok {
		t.Fatal("replacement generation lost its authoritative initial behind the old-generation initial")
	}
	if !initial.mail.initial || initial.envelope.generation.thread != got.mail.generation || initial.mail.sessionCache == nil {
		t.Fatalf("follow-up refresh = initial %v generation %d sessionCache %v; want authoritative generation %d", initial.mail.initial, initial.envelope.generation.thread, initial.mail.sessionCache != nil, got.mail.generation)
	}

	model, _ = got.Update(initial)
	got = model.(App)
	if got.mail.initialLoading {
		t.Fatal("replacement generation remained loading after its authoritative initial refresh")
	}
}

// TestProjectMailStoresNeverScanConcurrently uses two active store instances to
// model the physical handoff between suspended home and visited ownership. A
// second refresh command may wait, but its scanner must not enter until the
// first scanner has returned.
func TestProjectMailStoresNeverScanConcurrently(t *testing.T) {
	scanner := newBlockingProjectMailScanner()
	homeProject := t.TempDir()
	visitedProject := t.TempDir()
	homeMail := NewMailModel(filepath.Join(homeProject, "human"), "human", homeProject, "", "home", 20, "", "en", false, 0)
	visitedMail := NewMailModel(filepath.Join(visitedProject, "human"), "human", visitedProject, "", "visited", 20, "", "en", false, 0)
	home := newProjectMailStoreWithDeps(homeProject, homeMail.humanDir, scanner, func(string) {})
	visited := newProjectMailStoreWithDeps(visitedProject, visitedMail.humanDir, scanner, func(string) {})
	homeCmd := home.beginRefresh(homeMail, false)
	visitedCmd := visited.beginRefresh(visitedMail, true)
	if homeCmd == nil || visitedCmd == nil {
		t.Fatal("precondition: both store refresh commands must be scheduled")
	}

	homeDone := make(chan struct{})
	visitedDone := make(chan struct{})
	go func() {
		_ = homeCmd()
		close(homeDone)
	}()
	waitProjectMailSignal(t, scanner.started, "home scanner start")
	go func() {
		_ = visitedCmd()
		close(visitedDone)
	}()

	overlapped := false
	select {
	case <-scanner.started:
		overlapped = true
		scanner.release <- struct{}{}
		scanner.release <- struct{}{}
	case <-time.After(250 * time.Millisecond):
		scanner.release <- struct{}{}
		waitProjectMailSignal(t, homeDone, "home scanner completion")
		waitProjectMailSignal(t, scanner.started, "visited scanner start after home")
		scanner.release <- struct{}{}
	}
	waitProjectMailSignal(t, homeDone, "home refresh completion")
	waitProjectMailSignal(t, visitedDone, "visited refresh completion")
	if overlapped || scanner.maxActive.Load() != 1 {
		t.Fatalf("home and visited scanners overlapped: max active=%d", scanner.maxActive.Load())
	}
}

// TestHumanLocationUpdateRevalidatesAtExecution proves a command accepted for
// one activation/version cannot update after that store is suspended or after a
// newer accepted snapshot supersedes it.
func TestHumanLocationUpdateRevalidatesAtExecution(t *testing.T) {
	newStore := func(t *testing.T) (*ProjectMailStore, MailModel, *atomic.Int64) {
		t.Helper()
		projectDir := t.TempDir()
		mail := NewMailModel(filepath.Join(projectDir, "human"), "human", projectDir, "", "agent", 20, "", "en", false, 0)
		updates := &atomic.Int64{}
		store := newProjectMailStoreWithDeps(projectDir, mail.humanDir, filesystemProjectMailScanner{}, func(string) {
			updates.Add(1)
		})
		bindExistingStoreMailForAsyncTest(t, &store, &mail, 1)
		return &store, mail, updates
	}

	t.Run("suspended activation", func(t *testing.T) {
		store, mail, updates := newStore(t)
		msg := store.beginRefresh(mail, false)().(projectMailRefreshMsg)
		if _, accepted, _ := acceptStoreRefreshForTest(store, msg); !accepted {
			t.Fatal("precondition: current refresh was rejected")
		}
		cmd := store.locationUpdateCmd(msg.envelope)
		store.suspend()
		_ = cmd()
		if updates.Load() != 0 {
			t.Fatal("suspended store executed a stale human-location update")
		}
	})

	t.Run("superseded version", func(t *testing.T) {
		store, mail, updates := newStore(t)
		first := store.beginRefresh(mail, false)().(projectMailRefreshMsg)
		if _, accepted, _ := acceptStoreRefreshForTest(store, first); !accepted {
			t.Fatal("precondition: first refresh was rejected")
		}
		staleCmd := store.locationUpdateCmd(first.envelope)
		second := store.beginRefresh(mail, false)().(projectMailRefreshMsg)
		if _, accepted, _ := acceptStoreRefreshForTest(store, second); !accepted {
			t.Fatal("precondition: second refresh was rejected")
		}
		currentCmd := store.locationUpdateCmd(second.envelope)
		_ = staleCmd()
		if updates.Load() != 0 {
			t.Fatal("superseded store version executed a stale human-location update")
		}
		_ = currentCmd()
		if updates.Load() != 1 {
			t.Fatalf("current accepted store version produced %d updates, want one", updates.Load())
		}
	})
}

// TestMainDoesNotOwnHumanLocationUpdate protects the one-updater boundary: TUI
// startup may construct the App, but only an accepted ProjectMailStore refresh
// may call fs.UpdateHumanLocation.
func TestMainDoesNotOwnHumanLocationUpdate(t *testing.T) {
	mainSource, err := os.ReadFile(filepath.Join("..", "..", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(mainSource, []byte("fs.UpdateHumanLocation(")) {
		t.Fatal("tui/main.go retains a second human-location updater outside ProjectMailStore acceptance")
	}
}

var _ tea.Msg = projectMailTickMsg{}
