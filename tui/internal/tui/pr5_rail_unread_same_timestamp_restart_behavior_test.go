package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestPR5Stage4RestartedUnreadBoundaryCountsAndPublishesLateSameTimestampID(t *testing.T) {
	installationTestStart(t)

	app, scanner, _ := installationNewApp(t, 0)
	projectRoot := filepath.Dir(app.projectDir)
	targetADir := filepath.Join(app.projectDir, "agent-a")
	targetBDir := filepath.Join(app.projectDir, "agent-b")
	installationWriteAgent(t, targetADir, "agent-a", "Agent A", "Agent A")
	installationWriteAgent(t, targetBDir, "agent-b", "Agent B", "Agent B")

	acceptedInventory := pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 7201)
	agentB := pr5RailLifecycleSnapshot(app, "agent-b", "Agent B", 7202)
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

	const sameTimestamp = "2026-07-15T00:00:00.123456789Z"
	historyA := pr5ProjectionMail(
		"same-a-0", "agent-a", "human", nil,
		"same-timestamp Agent A baseline", sameTimestamp,
	)
	historyB := pr5ProjectionMail(
		"same-b-0", "agent-b", "human", nil,
		"same-timestamp Agent B baseline", sameTimestamp,
	)
	scanner.messages = []fs.MailMessage{historyA, historyB}
	app, _ = installationAcceptInitial(t, app)

	targets, ready := app.agentRail.acceptedDirectTargets(app.mailStore.binding.owner)
	if !ready || len(targets) != 3 || app.railUnreadStore == nil {
		t.Fatalf("same-timestamp baseline targets: ready=%v count=%d unreadStore=%v, want true/3/live",
			ready, len(targets), app.railUnreadStore != nil)
	}
	pr5RequireSameTimestampUnread(t, app, targets, append([]fs.MailMessage(nil), app.mailStore.snapshot.cache.Messages...), 0, 0)

	lateA1 := pr5ProjectionMail(
		"same-a-1", "agent-a", "human", nil,
		"first late same-timestamp Agent A mail", sameTimestamp,
	)
	lateB1 := pr5ProjectionMail(
		"same-b-1", "agent-b", "human", nil,
		"first late same-timestamp Agent B mail", sameTimestamp,
	)
	scanner.messages = []fs.MailMessage{historyA, historyB, lateA1, lateB1}
	firstLateRefresh := installationRefreshResult(t, &app, false)
	app, _ = installationDeliverApp(t, app, firstLateRefresh)
	firstLateSnapshot := append([]fs.MailMessage(nil), app.mailStore.snapshot.cache.Messages...)
	pr5RequireSameTimestampUnread(t, app, targets, firstLateSnapshot, 1, 1)

	app.threadLoads = newThreadLoadCoordinator(&pr5RailActivationWorker{})
	app.mailStore.pollRate = time.Nanosecond
	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	app.agentRail.cursor = 1
	rowA := app.agentRail.rows[1]
	loadingA, activationCmd := app.activateOrdinaryRailRow(rowA)
	if activationCmd == nil || !loadingA.mail.initialLoading || loadingA.mailStore.binding.target != rowA.target {
		t.Fatalf("same-timestamp A activation: cmd=%v loading=%v target=%#v want=%#v",
			activationCmd != nil, loadingA.mail.initialLoading, loadingA.mailStore.binding.target, rowA.target)
	}
	firstAResult, ok := pr5FindRailThreadLoadResult(activationCmd)
	if !ok {
		t.Fatal("same-timestamp A activation returned no direct completion")
	}
	app, followup := installationDeliverApp(t, loadingA, firstAResult)
	if followup != nil || app.mail.initialLoading || app.currentThread.sessionCache != firstAResult.sessionCache {
		t.Fatalf("first same-timestamp A completion: followup=%v loading=%v installed=%v",
			followup != nil, app.mail.initialLoading, app.currentThread.sessionCache == firstAResult.sessionCache)
	}
	if bodies := strings.Join(pr5SortedVisibleBodies(app.mail.messages), "\n"); !strings.Contains(bodies, "first late same-timestamp Agent A mail") ||
		strings.Contains(bodies, "first late same-timestamp Agent B mail") {
		t.Fatalf("first same-timestamp projection bodies are not exact A-only content:\n%s", bodies)
	}
	pr5RequireSameTimestampUnread(t, app, targets, firstLateSnapshot, 0, 1)

	firstSeenBytes, err := os.ReadFile(fs.RailUnreadStatePath(projectRoot))
	if err != nil {
		t.Fatalf("read first same-timestamp seen boundary: %v", err)
	}
	restarted, err := fs.OpenRailUnreadStore(projectRoot, targets, firstLateSnapshot, app.mail.humanAddr)
	if err != nil {
		t.Fatalf("restart same-timestamp unread store: %v", err)
	}
	app.railUnreadStore = restarted
	pr5RequireSameTimestampUnread(t, app, targets, firstLateSnapshot, 0, 1)

	rootBeforeSecondLateID := app.mailStore.snapshot
	versionBeforeSecondLateID := app.mailStore.version
	lateA2 := pr5ProjectionMail(
		"same-a-2", "agent-a", "human", nil,
		"second late same-timestamp Agent A mail after restart", sameTimestamp,
	)
	lateB2 := pr5ProjectionMail(
		"same-b-2", "agent-b", "human", nil,
		"second late same-timestamp Agent B mail after restart", sameTimestamp,
	)
	worker := newPR5BlockingThreadLoadWorker(t)
	app.threadLoads = newThreadLoadCoordinator(worker)
	scanner.messages = []fs.MailMessage{historyA, historyB, lateA1, lateB1, lateA2, lateB2}
	secondLateRefresh := installationRefreshResult(t, &app, false)
	staged, stagedCmd := installationDeliverApp(t, app, secondLateRefresh)
	if stagedCmd == nil || staged.mail.initialLoading || staged.mail.acceptedSnapshot != rootBeforeSecondLateID ||
		staged.mail.asyncStoreVersion != versionBeforeSecondLateID ||
		staged.currentThread.acceptedSnapshotVersion != versionBeforeSecondLateID ||
		staged.mailStore.snapshot == rootBeforeSecondLateID || staged.mailStore.version <= versionBeforeSecondLateID ||
		staged.visibleRailUnreadRow(staged.mailStore.snapshot) != nil {
		t.Fatalf("second same-timestamp root acceptance violated staged A@N while exact A@N+1 waits: cmd=%v loading=%v oldSnapshot=%v versions=%d/%d old=%d store=%d storeAdvanced=%v latestVisible=%v",
			stagedCmd != nil, staged.mail.initialLoading, staged.mail.acceptedSnapshot == rootBeforeSecondLateID,
			staged.mail.asyncStoreVersion, staged.currentThread.acceptedSnapshotVersion, versionBeforeSecondLateID,
			staged.mailStore.version, staged.mailStore.snapshot != rootBeforeSecondLateID,
			staged.visibleRailUnreadRow(staged.mailStore.snapshot) != nil)
	}
	if bodies := strings.Join(pr5SortedVisibleBodies(staged.mail.messages), "\n"); !strings.Contains(bodies, "first late same-timestamp Agent A mail") ||
		strings.Contains(bodies, "second late same-timestamp Agent A mail after restart") {
		t.Fatalf("staged same-timestamp projection did not preserve rendered A@N while A@N+1 waits:\n%s", bodies)
	}
	secondLateSnapshot := append([]fs.MailMessage(nil), staged.mailStore.snapshot.cache.Messages...)
	pr5RequireSameTimestampUnread(t, staged, targets, secondLateSnapshot, 1, 2)
	if durableDuring, err := os.ReadFile(fs.RailUnreadStatePath(projectRoot)); err != nil {
		t.Fatalf("read same-timestamp boundary while exact A projection is pending: %v", err)
	} else if !bytes.Equal(durableDuring, firstSeenBytes) {
		t.Fatal("accepted same-timestamp root snapshot advanced durable A boundary before exact direct publication")
	}

	stagedResults := pr5StartBatchCommands(t, stagedCmd, "second same-timestamp A projection")
	flight := pr5AwaitThreadLoadFlight(t, worker, "second same-timestamp A")
	if flight.request.envelope.target != rowA.target ||
		flight.request.envelope.generation.thread != staged.mailStore.binding.generation ||
		flight.request.envelope.storeVersion != staged.mailStore.version ||
		len(flight.request.acceptedMessages) != len(secondLateSnapshot) {
		t.Fatalf("second same-timestamp A flight coordinates: envelope=%#v messages=%d, want target A generation=%d version=%d messages=%d",
			flight.request.envelope, len(flight.request.acceptedMessages),
			staged.mailStore.binding.generation, staged.mailStore.version, len(secondLateSnapshot))
	}
	secondACache, err := (directThreadLoadWorker{}).Load(flight.request)
	if err != nil {
		t.Fatalf("build second same-timestamp A completion: %v", err)
	}
	flight.release <- pr5ThreadLoadReply{sessionCache: secondACache}
	secondAResult := pr5AwaitVisibleRefreshThreadLoadResult(t, stagedResults, "second same-timestamp A completion")
	published, followup := installationDeliverApp(t, staged, secondAResult)
	if followup != nil || published.mail.initialLoading || published.currentThread.sessionCache != secondACache ||
		published.mail.sessionCache != secondACache {
		t.Fatalf("second same-timestamp A publication: followup=%v loading=%v threadCache=%v mailCache=%v",
			followup != nil, published.mail.initialLoading,
			published.currentThread.sessionCache == secondACache, published.mail.sessionCache == secondACache)
	}
	if bodies := strings.Join(pr5SortedVisibleBodies(published.mail.messages), "\n"); !strings.Contains(bodies, "second late same-timestamp Agent A mail after restart") ||
		strings.Contains(bodies, "second late same-timestamp Agent B mail after restart") {
		t.Fatalf("second same-timestamp projection bodies are not exact A-only content:\n%s", bodies)
	}
	pr5RequireSameTimestampUnread(t, published, targets, secondLateSnapshot, 0, 2)
	pr5RequireThreadLoadCounters(t, published.threadLoads.Counters(), ThreadLoadCounters{
		Started:       1,
		Coalesced:     0,
		Completed:     1,
		TrueCancelled: 0,
		StaleDropped:  0,
	})

	finalRestart, err := fs.OpenRailUnreadStore(projectRoot, targets, secondLateSnapshot, published.mail.humanAddr)
	if err != nil {
		t.Fatalf("final restart same-timestamp unread store: %v", err)
	}
	for i, want := range []int{0, 0, 2} {
		if unread := finalRestart.UnreadCount(targets[i], secondLateSnapshot, published.mail.humanAddr); unread != want {
			t.Fatalf("final restarted Main/A/B unread[%d] = %d, want %d", i, unread, want)
		}
	}
	if got := scanner.scans.Load(); got != 3 {
		t.Fatalf("same-timestamp mail scans = %d, want exactly 3", got)
	}
	if inventoryScript.calls != 1 {
		t.Fatalf("same-timestamp inventory scans = %d, want exactly 1", inventoryScript.calls)
	}
	select {
	case extra := <-worker.started:
		t.Fatalf("same-timestamp flow started an extra physical worker for target=%#v", extra.request.envelope.target)
	default:
	}
}

func pr5RequireSameTimestampUnread(
	t *testing.T,
	app App,
	targets []fs.DirectTarget,
	acceptedMessages []fs.MailMessage,
	wantA int,
	wantB int,
) {
	t.Helper()

	if app.railUnreadStore == nil || len(app.agentRail.rows) != 3 || len(targets) != 3 {
		t.Fatalf("same-timestamp unread state is not installed: store=%v rows=%d targets=%d",
			app.railUnreadStore != nil, len(app.agentRail.rows), len(targets))
	}
	for i, want := range []int{0, wantA, wantB} {
		if unread := app.railUnreadStore.UnreadCount(targets[i], acceptedMessages, app.mail.humanAddr); unread != want {
			t.Fatalf("live Main/A/B unread[%d] = %d, want %d", i, unread, want)
		}
		if app.agentRail.rows[i].unread != want {
			t.Fatalf("cached Main/A/B unread[%d] = %d, want %d", i, app.agentRail.rows[i].unread, want)
		}
	}
}
