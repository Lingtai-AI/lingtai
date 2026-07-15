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

func TestPR5Stage4VisibleOrdinaryRootRefreshWaitsForMatchingProjectionBeforeUnreadAdvance(t *testing.T) {
	installationTestStart(t)

	app, scanner, _ := installationNewApp(t, 0)
	projectRoot := filepath.Dir(app.projectDir)
	targetADir := filepath.Join(app.projectDir, "agent-a")
	targetBDir := filepath.Join(app.projectDir, "agent-b")
	installationWriteAgent(t, targetADir, "agent-a", "Agent A", "Agent A")
	installationWriteAgent(t, targetBDir, "agent-b", "Agent B", "Agent B")

	acceptedInventory := pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 6901)
	agentB := pr5RailLifecycleSnapshot(app, "agent-b", "Agent B", 6902)
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

	worker := newPR5BlockingThreadLoadWorker(t)
	app.threadLoads = newThreadLoadCoordinator(worker)
	app.mailStore.pollRate = time.Nanosecond
	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	app.agentRail.cursor = 1
	rowA := app.agentRail.rows[1]
	loading, activationCmd := app.activateOrdinaryRailRow(rowA)
	if activationCmd == nil || !loading.mail.ready || !loading.mail.initialLoading {
		t.Fatalf(
			"A@N activation: cmd=%v ready=%v loading=%v, want command/ready/loading",
			activationCmd != nil, loading.mail.ready, loading.mail.initialLoading,
		)
	}
	activationResults := pr5StartBatchCommands(t, activationCmd, "visible A@N activation")
	a1Flight := pr5AwaitThreadLoadFlight(t, worker, "visible A@N")
	if a1Flight.request.envelope.storeVersion != rootN.Version() ||
		!reflect.DeepEqual(a1Flight.request.acceptedMessages, acceptedN) {
		t.Fatalf(
			"A@N request version=%d accepted=%#v, want exact N version=%d detached messages",
			a1Flight.request.envelope.storeVersion, a1Flight.request.acceptedMessages, rootN.Version(),
		)
	}
	a1Cache, err := (directThreadLoadWorker{}).Load(a1Flight.request)
	if err != nil {
		t.Fatalf("build controlled visible A@N direct result: %v", err)
	}
	a1Flight.release <- pr5ThreadLoadReply{sessionCache: a1Cache}
	a1Result := pr5AwaitThreadLoadResult(t, activationResults, "visible A@N")
	visibleN, followup := installationDeliverApp(t, loading, a1Result)
	if followup != nil {
		t.Fatal("accepted visible A@N settlement returned an unexpected rerun")
	}
	if visibleN.currentView != appViewMail || !visibleN.mail.ready || visibleN.mail.initialLoading ||
		visibleN.mailStore.snapshot != rootN || visibleN.mail.acceptedSnapshot != rootN ||
		visibleN.mail.asyncStoreVersion != rootN.Version() ||
		visibleN.currentThread.acceptedSnapshotVersion != rootN.Version() ||
		visibleN.mailStore.binding.target != rowA.target || visibleN.currentThread.target != rowA.target {
		t.Fatalf(
			"accepted visible A@N coordinate: view=%v ready=%v loading=%v storeN=%v mailN=%v versions=%d/%d/%d storeTarget=%#v threadTarget=%#v",
			visibleN.currentView, visibleN.mail.ready, visibleN.mail.initialLoading,
			visibleN.mailStore.snapshot == rootN, visibleN.mail.acceptedSnapshot == rootN,
			visibleN.mail.asyncStoreVersion, visibleN.currentThread.acceptedSnapshotVersion, rootN.Version(),
			visibleN.mailStore.binding.target, visibleN.currentThread.target,
		)
	}
	if got := pr5SortedVisibleBodies(visibleN.mail.messages); !reflect.DeepEqual(got, []string{
		"historical Agent A mail",
	}) {
		t.Fatalf("accepted visible A@N direct bodies = %v, want exact historical A-only projection", got)
	}
	statePath := fs.RailUnreadStatePath(projectRoot)
	stateAtVisibleN, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read unread state at visible A@N: %v", err)
	}

	laterA := pr5ProjectionMail(
		"later-a", "agent-a", "human", nil,
		"later Agent A mail", "2026-07-15T00:01:00Z",
	)
	laterB := pr5ProjectionMail(
		"later-b", "agent-b", "human", nil,
		"later Agent B mail", "2026-07-15T00:01:00Z",
	)
	scanner.messages = []fs.MailMessage{historyA, historyB, laterA, laterB}
	refreshNPlusOne := installationRefreshResult(t, &visibleN, false)
	staged, refreshCmd := installationDeliverApp(t, visibleN, refreshNPlusOne)
	if refreshCmd == nil {
		t.Fatal("accepted visible ordinary N+1 refresh returned no direct projection rerun")
	}
	refreshResults := pr5StartBatchCommands(t, refreshCmd, "visible A@N+1 refresh rerun")
	a2Flight := pr5AwaitThreadLoadFlight(t, worker, "visible A@N+1 refresh rerun")

	rootNPlusOne := staged.mailStore.snapshot
	if rootNPlusOne == nil {
		t.Fatal("accepted root N+1 coordinate has nil snapshot")
	}
	if rootNPlusOne == rootN || rootNPlusOne.Version() <= rootN.Version() {
		t.Fatalf(
			"accepted root N+1 coordinate: root=%p N=%p versions=%d/%d, want new later root",
			rootNPlusOne, rootN, rootNPlusOne.Version(), rootN.Version(),
		)
	}
	acceptedNPlusOne := rootNPlusOne.cache.Messages
	if a2Flight.request.envelope.storeVersion != rootNPlusOne.Version() ||
		!reflect.DeepEqual(a2Flight.request.acceptedMessages, acceptedNPlusOne) {
		t.Fatalf(
			"blocked A@N+1 request version=%d accepted=%#v, want exact latest root version=%d detached messages",
			a2Flight.request.envelope.storeVersion, a2Flight.request.acceptedMessages, rootNPlusOne.Version(),
		)
	}
	if staged.currentView != appViewMail || !staged.mail.ready || staged.mail.initialLoading ||
		staged.mailStore.binding.target != rowA.target || staged.currentThread.target != rowA.target {
		t.Fatalf(
			"blocked A@N+1 visible shell: view=%v ready=%v loading=%v storeTarget=%#v threadTarget=%#v",
			staged.currentView, staged.mail.ready, staged.mail.initialLoading,
			staged.mailStore.binding.target, staged.currentThread.target,
		)
	}
	stagedBodies := pr5SortedVisibleBodies(staged.mail.messages)
	if scanner.scans.Load() != 2 || inventoryScript.calls != 1 {
		t.Fatalf("visible N→N+1 scans: mail=%d inventory=%d, want exactly 2 root/1 inventory", scanner.scans.Load(), inventoryScript.calls)
	}
	if got := staged.railUnreadStore.UnreadCount(targets[2], acceptedNPlusOne, staged.mail.humanAddr); got != 1 {
		t.Fatalf("inactive Agent B unread while visible A@N+1 is blocked = %d, want independently 1", got)
	}
	if got := staged.railUnreadStore.UnreadCount(targets[1], acceptedNPlusOne, staged.mail.humanAddr); got != 1 {
		t.Fatalf("visible Agent A unread while matching A@N+1 projection is blocked = %d, want preserved 1", got)
	}
	stateWhileBlocked, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read unread state while visible A@N+1 is blocked: %v", err)
	}
	if !bytes.Equal(stateWhileBlocked, stateAtVisibleN) {
		t.Fatal("accepted root N+1 advanced durable Agent A unread before its matching direct projection rendered")
	}
	if staged.agentRail.rows[0].unread != 0 || staged.agentRail.rows[1].unread != 1 || staged.agentRail.rows[2].unread != 1 {
		t.Fatalf(
			"cached Main/A/B unread while A@N+1 is blocked = %d/%d/%d, want 0/1/1 from root N+1",
			staged.agentRail.rows[0].unread, staged.agentRail.rows[1].unread, staged.agentRail.rows[2].unread,
		)
	}
	if staged.mail.acceptedSnapshot != rootN || staged.mail.asyncStoreVersion != rootN.Version() ||
		staged.currentThread.acceptedSnapshotVersion != rootN.Version() ||
		staged.visibleRailUnreadRow(rootNPlusOne) != nil {
		t.Fatalf(
			"staged projection coordinate while root N+1 waits: mailN=%v versions=%d/%d want=%d latestVisible=%v",
			staged.mail.acceptedSnapshot == rootN, staged.mail.asyncStoreVersion,
			staged.currentThread.acceptedSnapshotVersion, rootN.Version(), staged.visibleRailUnreadRow(rootNPlusOne) != nil,
		)
	}
	if !reflect.DeepEqual(stagedBodies, []string{"historical Agent A mail"}) {
		t.Fatalf("visible bodies while A@N+1 is blocked = %v, want preserved rendered A@N projection", stagedBodies)
	}
	pr5RequireThreadLoadCounters(t, staged.threadLoads.Counters(), ThreadLoadCounters{
		Started:       2,
		Coalesced:     0,
		Completed:     1,
		TrueCancelled: 0,
		StaleDropped:  0,
	})

	a2Cache, err := (directThreadLoadWorker{}).Load(a2Flight.request)
	if err != nil {
		t.Fatalf("build controlled visible A@N+1 direct result: %v", err)
	}
	a2Flight.release <- pr5ThreadLoadReply{sessionCache: a2Cache}
	a2Result := pr5AwaitVisibleRefreshThreadLoadResult(t, refreshResults, "visible A@N+1")
	published, further := installationDeliverApp(t, staged, a2Result)
	if further != nil {
		t.Fatal("accepted visible A@N+1 settlement returned an unexpected further rerun")
	}
	if published.mail.initialLoading || published.mail.acceptedSnapshot != rootNPlusOne ||
		published.mail.asyncStoreVersion != rootNPlusOne.Version() ||
		published.currentThread.acceptedSnapshotVersion != rootNPlusOne.Version() {
		t.Fatalf(
			"published A@N+1 coordinate: loading=%v mailRoot=%v versions=%d/%d/%d",
			published.mail.initialLoading, published.mail.acceptedSnapshot == rootNPlusOne,
			published.mail.asyncStoreVersion, published.currentThread.acceptedSnapshotVersion, rootNPlusOne.Version(),
		)
	}
	if got := pr5SortedVisibleBodies(published.mail.messages); !reflect.DeepEqual(got, []string{
		"historical Agent A mail", "later Agent A mail",
	}) {
		t.Fatalf("published visible A@N+1 bodies = %v, want exact latest A-only projection", got)
	}
	if got := published.railUnreadStore.UnreadCount(targets[0], acceptedNPlusOne, published.mail.humanAddr); got != 0 {
		t.Fatalf("Main unread after published A@N+1 = %d, want 0", got)
	}
	if got := published.railUnreadStore.UnreadCount(targets[1], acceptedNPlusOne, published.mail.humanAddr); got != 0 {
		t.Fatalf("Agent A unread after published A@N+1 = %d, want advanced 0", got)
	}
	if got := published.railUnreadStore.UnreadCount(targets[2], acceptedNPlusOne, published.mail.humanAddr); got != 1 {
		t.Fatalf("Agent B unread after published A@N+1 = %d, want independently 1", got)
	}
	if published.agentRail.rows[0].unread != 0 || published.agentRail.rows[1].unread != 0 || published.agentRail.rows[2].unread != 1 {
		t.Fatalf(
			"cached Main/A/B unread after published A@N+1 = %d/%d/%d, want 0/0/1",
			published.agentRail.rows[0].unread, published.agentRail.rows[1].unread, published.agentRail.rows[2].unread,
		)
	}
	pr5RequireThreadLoadCounters(t, published.threadLoads.Counters(), ThreadLoadCounters{
		Started:       2,
		Coalesced:     0,
		Completed:     2,
		TrueCancelled: 0,
		StaleDropped:  0,
	})
	if scanner.scans.Load() != 2 || inventoryScript.calls != 1 {
		t.Fatalf("final visible N→N+1 scans: mail=%d inventory=%d, want exactly 2 root/1 inventory", scanner.scans.Load(), inventoryScript.calls)
	}
	select {
	case extra := <-worker.started:
		t.Fatalf("unexpected third physical ordinary worker at store version %d", extra.request.envelope.storeVersion)
	default:
	}

	reopened, err := fs.OpenRailUnreadStore(projectRoot, targets, acceptedNPlusOne, published.mail.humanAddr)
	if err != nil {
		t.Fatalf("reopen visible ordinary N+1 unread state: %v", err)
	}
	if got := reopened.UnreadCount(targets[0], acceptedNPlusOne, published.mail.humanAddr); got != 0 {
		t.Fatalf("restart Main unread after published A@N+1 = %d, want 0", got)
	}
	if got := reopened.UnreadCount(targets[1], acceptedNPlusOne, published.mail.humanAddr); got != 0 {
		t.Fatalf("restart Agent A unread after published A@N+1 = %d, want 0", got)
	}
	if got := reopened.UnreadCount(targets[2], acceptedNPlusOne, published.mail.humanAddr); got != 1 {
		t.Fatalf("restart Agent B unread after published A@N+1 = %d, want 1", got)
	}
}

func pr5AwaitVisibleRefreshThreadLoadResult(
	t *testing.T,
	results <-chan tea.Msg,
	label string,
) threadLoadResultMsg {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case raw := <-results:
			if msg, ok := raw.(threadLoadResultMsg); ok {
				return msg
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s thread-load result", label)
			return threadLoadResultMsg{}
		}
	}
}
