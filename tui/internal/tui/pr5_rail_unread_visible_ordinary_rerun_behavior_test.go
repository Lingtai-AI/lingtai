package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestPR5Stage4VisibleOrdinaryRerunsLatestAcceptedSnapshotBeforeUnreadAdvance(t *testing.T) {
	installationTestStart(t)

	app, scanner, _ := installationNewApp(t, 0)
	projectRoot := filepath.Dir(app.projectDir)
	targetADir := filepath.Join(app.projectDir, "agent-a")
	targetBDir := filepath.Join(app.projectDir, "agent-b")
	installationWriteAgent(t, targetADir, "agent-a", "Agent A", "Agent A")
	installationWriteAgent(t, targetBDir, "agent-b", "Agent B", "Agent B")
	// Keep target history present while the first ordinary cold worker is blocked.
	// An accepted root refresh may stage a newer mailbox snapshot, but the UI
	// event loop must not synchronously ingest this file outside that worker.
	installationWriteEvents(t, targetADir, 150, "event-a")

	acceptedInventory := pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 6801)
	agentB := pr5RailLifecycleSnapshot(app, "agent-b", "Agent B", 6802)
	acceptedInventory.Records = append(acceptedInventory.Records, agentB.Records...)
	inventoryScript := &pr5RailInventoryScanScript{steps: []pr5RailInventoryScanStep{{snapshot: acceptedInventory}}}
	setter, ok := any(&app).(pr5AgentRailInventoryScannerSetter)
	if !ok {
		t.Fatal("App has no root-owned agent-rail inventory lifecycle boundary")
	}
	setter.setAgentRailInventoryScanner(inventoryScript.Scan)
	inventoryResult := pr5RunTrailingRailInventoryScan(t, app.Init(), inventoryScript)
	app, _ = installationDeliverApp(t, app, inventoryResult)
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent A", "Agent B"})

	historyA := pr5ProjectionMail(
		"historical-a", "agent-a", "human", nil,
		"historical Agent A mail", "2026-07-15T00:00:00Z",
	)
	historyB := pr5ProjectionMail(
		"historical-b", "agent-b", "human", nil,
		"historical Agent B mail", "2026-07-15T00:00:00Z",
	)
	scanner.messages = []fs.MailMessage{historyA, historyB}
	app, _ = installationAcceptInitial(t, app)
	app, _ = app.updateMailChildWindowSize(app.layoutBudget().ChildWindowSize())

	targets, ready := app.agentRail.acceptedDirectTargets(app.mailStore.binding.owner)
	if !ready || len(targets) != 3 || app.railUnreadStore == nil || app.mailStore.snapshot == nil {
		t.Fatalf(
			"N baseline: ready=%v targets=%d store=%v snapshot=%v, want true/3/live/live",
			ready, len(targets), app.railUnreadStore != nil, app.mailStore.snapshot != nil,
		)
	}
	rootN := app.mailStore.snapshot
	acceptedN := append([]fs.MailMessage(nil), rootN.cache.Messages...)
	for i, label := range []string{"Main", "Agent A", "Agent B"} {
		if got := app.railUnreadStore.UnreadCount(targets[i], acceptedN, app.mail.humanAddr); got != 0 {
			t.Fatalf("N historical %s unread = %d, want baseline 0", label, got)
		}
	}
	statePath := fs.RailUnreadStatePath(projectRoot)
	stateAtN, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read N unread state: %v", err)
	}

	worker := newPR5BlockingThreadLoadWorker(t)
	app.threadLoads = newThreadLoadCoordinator(worker)
	app.mailStore.pollRate = time.Nanosecond
	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	app.agentRail.cursor = 1
	rowA := app.agentRail.rows[1]
	loading, activationCmd := app.activateOrdinaryRailRow(rowA)
	if activationCmd == nil || !loading.mail.ready || !loading.mail.initialLoading ||
		loading.mailStore.snapshot != rootN || loading.mail.acceptedSnapshot != rootN ||
		loading.currentThread.acceptedSnapshotVersion != rootN.Version() {
		t.Fatalf(
			"A@N activation: cmd=%v ready=%v loading=%v storeN=%v mailN=%v threadVersion=%d want=%d",
			activationCmd != nil, loading.mail.ready, loading.mail.initialLoading,
			loading.mailStore.snapshot == rootN, loading.mail.acceptedSnapshot == rootN,
			loading.currentThread.acceptedSnapshotVersion, rootN.Version(),
		)
	}
	activationResults := pr5StartBatchCommands(t, activationCmd, "A@N activation")
	a1Flight := pr5AwaitThreadLoadFlight(t, worker, "A@N")
	if a1Flight.request.envelope.storeVersion != rootN.Version() ||
		!reflect.DeepEqual(a1Flight.request.acceptedMessages, acceptedN) {
		t.Fatalf(
			"A@N request version=%d accepted=%#v, want exact N version=%d detached messages",
			a1Flight.request.envelope.storeVersion, a1Flight.request.acceptedMessages, rootN.Version(),
		)
	}
	beforeColdMailCache := loading.mail.sessionCache
	beforeColdThreadCache := loading.currentThread.sessionCache
	beforeColdEntries := append([]fs.SessionEntry{}, loading.mail.sessionCache.Entries()...)
	beforeColdMessages := append([]ChatMessage{}, loading.mail.messages...)
	beforeColdMailIngest := loading.mail.ingestWindow
	beforeColdThreadIngest := loading.currentThread.ingestWindow

	laterA := pr5ProjectionMail(
		"later-a", "agent-a", "human", nil,
		"later Agent A mail", "2026-07-15T00:01:00Z",
	)
	laterB := pr5ProjectionMail(
		"later-b", "agent-b", "human", nil,
		"later Agent B mail", "2026-07-15T00:01:00Z",
	)
	scanner.messages = []fs.MailMessage{historyA, historyB, laterA, laterB}
	refreshNPlusOne := installationRefreshResult(t, &loading, false)
	advanced, _ := installationDeliverApp(t, loading, refreshNPlusOne)
	rootNPlusOne := advanced.mailStore.snapshot
	acceptedNPlusOne := rootNPlusOne.cache.Messages
	if rootNPlusOne == nil || rootNPlusOne == rootN || rootNPlusOne.Version() <= rootN.Version() ||
		advanced.mail.acceptedSnapshot != rootN || advanced.mail.asyncStoreVersion != rootN.Version() ||
		advanced.currentThread.acceptedSnapshotVersion != rootN.Version() || !advanced.mail.initialLoading {
		t.Fatalf(
			"staged N+1 while A@N loads: root=%p N=%p versions=%d/%d mailRoot=%v mailVersion=%d threadVersion=%d loading=%v",
			rootNPlusOne, rootN, rootNPlusOne.Version(), rootN.Version(),
			advanced.mail.acceptedSnapshot == rootN, advanced.mail.asyncStoreVersion,
			advanced.currentThread.acceptedSnapshotVersion, advanced.mail.initialLoading,
		)
	}
	if advanced.mail.sessionCache != beforeColdMailCache ||
		advanced.currentThread.sessionCache != beforeColdThreadCache ||
		!reflect.DeepEqual(append([]fs.SessionEntry{}, advanced.mail.sessionCache.Entries()...), beforeColdEntries) ||
		!reflect.DeepEqual(append([]ChatMessage{}, advanced.mail.messages...), beforeColdMessages) ||
		!reflect.DeepEqual(advanced.mail.ingestWindow, beforeColdMailIngest) ||
		!reflect.DeepEqual(advanced.currentThread.ingestWindow, beforeColdThreadIngest) {
		t.Fatalf(
			"accepted root refresh bypassed blocked cold worker: mailCacheChanged=%v threadCacheChanged=%v entries=%d/%d messages=%d/%d mailWindowChanged=%v threadWindowChanged=%v",
			advanced.mail.sessionCache != beforeColdMailCache,
			advanced.currentThread.sessionCache != beforeColdThreadCache,
			len(advanced.mail.sessionCache.Entries()), len(beforeColdEntries),
			len(advanced.mail.messages), len(beforeColdMessages),
			!reflect.DeepEqual(advanced.mail.ingestWindow, beforeColdMailIngest),
			!reflect.DeepEqual(advanced.currentThread.ingestWindow, beforeColdThreadIngest),
		)
	}
	if got := advanced.railUnreadStore.UnreadCount(targets[1], acceptedNPlusOne, advanced.mail.humanAddr); got != 1 {
		t.Fatalf("Agent A unread at accepted N+1 while A@N loads = %d, want preserved 1", got)
	}
	if got := advanced.railUnreadStore.UnreadCount(targets[2], acceptedNPlusOne, advanced.mail.humanAddr); got != 1 {
		t.Fatalf("Agent B unread at accepted N+1 while A@N loads = %d, want independent 1", got)
	}
	stateWhileLoading, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read unread state while A@N loads after N+1: %v", err)
	}
	if !bytes.Equal(stateWhileLoading, stateAtN) {
		t.Fatal("accepted N+1 advanced durable unread before a matching visible cold projection")
	}
	if scanner.scans.Load() != 2 || inventoryScript.calls != 1 {
		t.Fatalf("N→N+1 scans: mail=%d inventory=%d, want exactly 2 root/1 inventory", scanner.scans.Load(), inventoryScript.calls)
	}

	a1Cache, err := (directThreadLoadWorker{}).Load(a1Flight.request)
	if err != nil {
		t.Fatalf("build controlled A@N direct result: %v", err)
	}
	a1Flight.release <- pr5ThreadLoadReply{sessionCache: a1Cache}
	a1Result := pr5AwaitThreadLoadResult(t, activationResults, "A@N")
	beforeRejectedCache := advanced.currentThread.sessionCache
	settled, rerunCmd := installationDeliverApp(t, advanced, a1Result)
	if settled.currentThread.sessionCache != beforeRejectedCache || !settled.mail.initialLoading ||
		settled.mail.acceptedSnapshot != rootN || settled.mail.asyncStoreVersion != rootN.Version() ||
		settled.currentThread.acceptedSnapshotVersion != rootN.Version() {
		t.Fatalf(
			"stale A@N settlement published: cacheChanged=%v loading=%v sameNPlusOne=%v mailVersion=%d threadVersion=%d want-staged=%d",
			settled.currentThread.sessionCache != beforeRejectedCache, settled.mail.initialLoading,
			settled.mail.acceptedSnapshot == rootN, settled.mail.asyncStoreVersion,
			settled.currentThread.acceptedSnapshotVersion, rootN.Version(),
		)
	}
	if rerunCmd == nil {
		t.Fatal("stale A@N settlement did not launch the one latest accepted N+1 rerun")
	}
	pr5RequireThreadLoadCounters(t, settled.threadLoads.Counters(), ThreadLoadCounters{
		Started:       2,
		Coalesced:     1,
		Completed:     1,
		TrueCancelled: 0,
		StaleDropped:  1,
	})

	a2Done := pr5RunThreadLoadCmd(t, rerunCmd, "A@N+1 rerun")
	a2Flight := pr5AwaitThreadLoadFlight(t, worker, "A@N+1 rerun")
	if a2Flight.request.envelope.storeVersion != rootNPlusOne.Version() ||
		!reflect.DeepEqual(a2Flight.request.acceptedMessages, acceptedNPlusOne) {
		t.Fatalf(
			"A@N+1 rerun version=%d accepted=%#v, want exact latest version=%d detached messages",
			a2Flight.request.envelope.storeVersion, a2Flight.request.acceptedMessages, rootNPlusOne.Version(),
		)
	}
	a2Cache, err := (directThreadLoadWorker{}).Load(a2Flight.request)
	if err != nil {
		t.Fatalf("build controlled A@N+1 direct result: %v", err)
	}
	a2Flight.release <- pr5ThreadLoadReply{sessionCache: a2Cache}
	published, followup := pr5DeliverThreadLoadResult(t, settled, a2Done, "A@N+1")
	if followup != nil {
		t.Fatal("accepted A@N+1 settlement returned an unexpected further rerun")
	}

	if published.currentView != appViewMail || !published.mail.ready || published.mail.initialLoading ||
		published.mailStore.snapshot != rootNPlusOne || published.mail.acceptedSnapshot != rootNPlusOne ||
		published.mail.asyncStoreVersion != rootNPlusOne.Version() ||
		published.currentThread.acceptedSnapshotVersion != rootNPlusOne.Version() ||
		published.mailStore.binding.target != rowA.target || published.currentThread.target != rowA.target {
		t.Fatalf(
			"accepted visible A@N+1 coordinate: view=%v ready=%v loading=%v storeRoot=%v mailRoot=%v versions=%d/%d/%d storeTarget=%#v threadTarget=%#v",
			published.currentView, published.mail.ready, published.mail.initialLoading,
			published.mailStore.snapshot == rootNPlusOne, published.mail.acceptedSnapshot == rootNPlusOne,
			published.mail.asyncStoreVersion, published.currentThread.acceptedSnapshotVersion, rootNPlusOne.Version(),
			published.mailStore.binding.target, published.currentThread.target,
		)
	}
	if got := pr5SortedVisibleBodies(published.mail.messages); !reflect.DeepEqual(got, []string{
		"historical Agent A mail", "later Agent A mail",
	}) {
		t.Fatalf("accepted visible A@N+1 direct bodies = %v, want exact latest A-only projection", got)
	}
	if got := published.railUnreadStore.UnreadCount(targets[0], acceptedNPlusOne, published.mail.humanAddr); got != 0 {
		t.Fatalf("Main unread after A@N+1 = %d, want independently 0", got)
	}
	if got := published.railUnreadStore.UnreadCount(targets[1], acceptedNPlusOne, published.mail.humanAddr); got != 0 {
		t.Fatalf("Agent A unread after exact visible A@N+1 = %d, want advanced 0", got)
	}
	if got := published.railUnreadStore.UnreadCount(targets[2], acceptedNPlusOne, published.mail.humanAddr); got != 1 {
		t.Fatalf("Agent B unread after A@N+1 = %d, want independently 1", got)
	}
	if published.agentRail.rows[0].unread != 0 || published.agentRail.rows[1].unread != 0 || published.agentRail.rows[2].unread != 1 {
		t.Fatalf(
			"cached Main/A/B unread after A@N+1 = %d/%d/%d, want 0/0/1",
			published.agentRail.rows[0].unread, published.agentRail.rows[1].unread, published.agentRail.rows[2].unread,
		)
	}
	pr5RequireThreadLoadCounters(t, published.threadLoads.Counters(), ThreadLoadCounters{
		Started:       2,
		Coalesced:     1,
		Completed:     2,
		TrueCancelled: 0,
		StaleDropped:  1,
	})
	if scanner.scans.Load() != 2 || inventoryScript.calls != 1 {
		t.Fatalf("final N→N+1 scans: mail=%d inventory=%d, want exactly 2 root/1 inventory", scanner.scans.Load(), inventoryScript.calls)
	}
	select {
	case extra := <-worker.started:
		t.Fatalf("unexpected third physical cold worker start at store version %d", extra.request.envelope.storeVersion)
	default:
	}

	reopened, err := fs.OpenRailUnreadStore(projectRoot, targets, acceptedNPlusOne, published.mail.humanAddr)
	if err != nil {
		t.Fatalf("reopen N+1 ordinary unread state: %v", err)
	}
	if got := reopened.UnreadCount(targets[0], acceptedNPlusOne, published.mail.humanAddr); got != 0 {
		t.Fatalf("restart Main unread after A@N+1 = %d, want 0", got)
	}
	if got := reopened.UnreadCount(targets[1], acceptedNPlusOne, published.mail.humanAddr); got != 0 {
		t.Fatalf("restart Agent A unread after A@N+1 = %d, want 0", got)
	}
	if got := reopened.UnreadCount(targets[2], acceptedNPlusOne, published.mail.humanAddr); got != 1 {
		t.Fatalf("restart Agent B unread after A@N+1 = %d, want 1", got)
	}
}

func pr5StartBatchCommands(t *testing.T, cmd tea.Cmd, label string) <-chan tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatalf("%s command is nil", label)
	}
	raw := cmd()
	batch, ok := raw.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("%s command returned %T, want non-empty tea.BatchMsg", label, raw)
	}
	results := make(chan tea.Msg, len(batch))
	for _, child := range batch {
		if child == nil {
			continue
		}
		go func(child tea.Cmd) { results <- child() }(child)
	}
	return results
}

func pr5AwaitThreadLoadResult(t *testing.T, results <-chan tea.Msg, label string) threadLoadResultMsg {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case raw := <-results:
			switch msg := raw.(type) {
			case threadLoadResultMsg:
				return msg
			case projectMailTickMsg:
				// The activation batch also owns the one root tick. It is deliberately
				// not delivered here; this test controls the exact N+1 root refresh.
			default:
				t.Fatalf("%s activation child completion type = %T, want thread load or root tick", label, raw)
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s thread-load result", label)
		}
	}
}
