package tui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestPR5Stage4HiddenOrdinaryCompletionWaitsForMailReentryBeforeUnreadAdvance(t *testing.T) {
	installationTestStart(t)

	app, scanner, _ := installationNewApp(t, 0)
	projectRoot := filepath.Dir(app.projectDir)
	targetADir := filepath.Join(app.projectDir, "agent-a")
	targetBDir := filepath.Join(app.projectDir, "agent-b")
	installationWriteAgent(t, targetADir, "agent-a", "Agent A", "Agent A")
	installationWriteAgent(t, targetBDir, "agent-b", "Agent B", "Agent B")

	acceptedInventory := pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 7001)
	agentB := pr5RailLifecycleSnapshot(app, "agent-b", "Agent B", 7002)
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

	targets, ready := app.agentRail.acceptedDirectTargets(app.mailStore.binding.owner)
	if !ready || len(targets) != 3 || app.railUnreadStore == nil {
		t.Fatalf("accepted direct targets: ready=%v len=%d store=%v, want true/3/live", ready, len(targets), app.railUnreadStore != nil)
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
	steady := installationRefreshResult(t, &app, false)
	app, _ = installationDeliverApp(t, app, steady)
	rootSnapshot := app.mailStore.snapshot
	acceptedMessages := rootSnapshot.cache.Messages
	if got := app.railUnreadStore.UnreadCount(targets[1], acceptedMessages, app.mail.humanAddr); got != 1 {
		t.Fatalf("inactive Agent A unread before activation = %d, want 1", got)
	}
	if got := app.railUnreadStore.UnreadCount(targets[2], acceptedMessages, app.mail.humanAddr); got != 1 {
		t.Fatalf("inactive Agent B unread before activation = %d, want 1", got)
	}

	worker := &pr5RailActivationWorker{}
	app.threadLoads = newThreadLoadCoordinator(worker)
	app.mailStore.pollRate = time.Nanosecond
	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	app.agentRail.cursor = 1
	rowA := app.agentRail.rows[1]
	activated, activationCmd := app.activateOrdinaryRailRow(rowA)
	if activationCmd == nil || !activated.mail.ready || !activated.mail.initialLoading {
		t.Fatalf(
			"ordinary Agent A activation: cmd=%v ready=%v loading=%v, want command/ready/loading",
			activationCmd != nil, activated.mail.ready, activated.mail.initialLoading,
		)
	}

	model, _ := activated.Update(ViewChangeMsg{View: "help"})
	offMail := model.(App)
	if offMail.currentView != appViewHelp || offMail.mailStore.tickRunning ||
		offMail.mailStore.binding.target != rowA.target || offMail.currentThread.target != rowA.target ||
		offMail.mailStore.snapshot != rootSnapshot {
		t.Fatalf(
			"ordinary departure coordinate: view=%v tick=%v storeTarget=%#v threadTarget=%#v sameRoot=%v",
			offMail.currentView, offMail.mailStore.tickRunning, offMail.mailStore.binding.target,
			offMail.currentThread.target, offMail.mailStore.snapshot == rootSnapshot,
		)
	}
	if strings.Contains(ansi.Strip(offMail.View().Content), "later Agent A mail") {
		t.Fatal("Help departure rendered hidden Agent A mail before its direct completion")
	}

	completion, found := pr5FindRailThreadLoadResult(activationCmd)
	if !found || completion.err != nil || completion.sessionCache == nil {
		t.Fatalf("ordinary Agent A cold completion: found=%v cache=%p err=%v, want accepted candidate", found, completion.sessionCache, completion.err)
	}
	if len(worker.requests) != 1 || !reflect.DeepEqual(worker.requests[0].acceptedMessages, acceptedMessages) {
		t.Fatalf("ordinary hidden worker requests=%d accepted=%#v, want one exact detached root snapshot", len(worker.requests), worker.requests)
	}

	hidden, followup := installationDeliverApp(t, offMail, completion)
	if followup != nil {
		t.Fatalf("accepted hidden ordinary completion returned unexpected follow-up %T", runCmd(followup))
	}
	if hidden.currentView != appViewHelp || hidden.mail.initialLoading ||
		hidden.mail.acceptedSnapshot != rootSnapshot || hidden.mail.asyncStoreVersion != rootSnapshot.Version() ||
		hidden.currentThread.acceptedSnapshotVersion != rootSnapshot.Version() {
		t.Fatalf(
			"accepted hidden projection coordinate: view=%v loading=%v sameRoot=%v versions=%d/%d/%d",
			hidden.currentView, hidden.mail.initialLoading, hidden.mail.acceptedSnapshot == rootSnapshot,
			hidden.mail.asyncStoreVersion, hidden.currentThread.acceptedSnapshotVersion, rootSnapshot.Version(),
		)
	}
	visibleBodies := pr5SortedVisibleBodies(hidden.mail.messages)
	if !reflect.DeepEqual(visibleBodies, []string{"historical Agent A mail", "later Agent A mail"}) {
		t.Fatalf("retained hidden Agent A bodies = %v, want exact A-only projection", visibleBodies)
	}
	if !strings.Contains(ansi.Strip(hidden.mail.View()), "later Agent A mail") {
		t.Fatalf("retained hidden Mail viewport omitted its accepted Agent A projection; model bodies=%v", visibleBodies)
	}
	if strings.Contains(ansi.Strip(hidden.View().Content), "later Agent A mail") {
		t.Fatal("accepted hidden Agent A projection escaped into the active Help frame")
	}

	if got := hidden.railUnreadStore.UnreadCount(targets[0], acceptedMessages, hidden.mail.humanAddr); got != 0 {
		t.Fatalf("Main unread after hidden Agent A completion = %d, want 0", got)
	}
	if got := hidden.railUnreadStore.UnreadCount(targets[1], acceptedMessages, hidden.mail.humanAddr); got != 1 {
		t.Fatalf("hidden Agent A unread after accepted completion = %d, want preserved 1", got)
	}
	if got := hidden.railUnreadStore.UnreadCount(targets[2], acceptedMessages, hidden.mail.humanAddr); got != 1 {
		t.Fatalf("inactive Agent B unread after hidden Agent A completion = %d, want 1", got)
	}
	if hidden.agentRail.rows[0].unread != 0 || hidden.agentRail.rows[1].unread != 1 || hidden.agentRail.rows[2].unread != 1 {
		t.Fatalf(
			"cached Main/A/B unread while Help is active = %d/%d/%d, want 0/1/1",
			hidden.agentRail.rows[0].unread, hidden.agentRail.rows[1].unread, hidden.agentRail.rows[2].unread,
		)
	}
	reopenedHidden, err := fs.OpenRailUnreadStore(projectRoot, targets, acceptedMessages, hidden.mail.humanAddr)
	if err != nil {
		t.Fatalf("reopen hidden unread state: %v", err)
	}
	if got := reopenedHidden.UnreadCount(targets[1], acceptedMessages, hidden.mail.humanAddr); got != 1 {
		t.Fatalf("durable Agent A unread before Mail reentry = %d, want preserved 1", got)
	}
	if got := reopenedHidden.UnreadCount(targets[2], acceptedMessages, hidden.mail.humanAddr); got != 1 {
		t.Fatalf("durable Agent B unread before Mail reentry = %d, want 1", got)
	}
	if scanner.scans.Load() != 2 || inventoryScript.calls != 1 || len(worker.requests) != 1 {
		t.Fatalf(
			"hidden completion ownership: mail scans=%d inventory=%d workers=%d, want 2/1/1",
			scanner.scans.Load(), inventoryScript.calls, len(worker.requests),
		)
	}

	model, reentryCmd := hidden.Update(ViewChangeMsg{View: "mail"})
	reentered := model.(App)
	if reentryCmd == nil || reentered.currentView != appViewMail || reentered.mail.initialLoading ||
		reentered.mail.acceptedSnapshot != rootSnapshot || reentered.mail.asyncStoreVersion != rootSnapshot.Version() ||
		reentered.currentThread.acceptedSnapshotVersion != rootSnapshot.Version() {
		t.Fatalf(
			"Mail reentry coordinate: cmd=%v view=%v loading=%v sameRoot=%v versions=%d/%d/%d",
			reentryCmd != nil, reentered.currentView, reentered.mail.initialLoading,
			reentered.mail.acceptedSnapshot == rootSnapshot, reentered.mail.asyncStoreVersion,
			reentered.currentThread.acceptedSnapshotVersion, rootSnapshot.Version(),
		)
	}
	reenteredFrame := ansi.Strip(reentered.View().Content)
	if !strings.Contains(reenteredFrame, "later Agent A mail") || strings.Contains(reenteredFrame, "later Agent B mail") {
		t.Fatalf("Mail reentry frame did not render the exact retained Agent A projection:\n%s", reenteredFrame)
	}
	if got := reentered.railUnreadStore.UnreadCount(targets[0], acceptedMessages, reentered.mail.humanAddr); got != 0 {
		t.Fatalf("Main unread after Agent A Mail reentry = %d, want 0", got)
	}
	if got := reentered.railUnreadStore.UnreadCount(targets[1], acceptedMessages, reentered.mail.humanAddr); got != 0 {
		t.Fatalf("rendered Agent A unread after Mail reentry = %d, want advanced 0", got)
	}
	if got := reentered.railUnreadStore.UnreadCount(targets[2], acceptedMessages, reentered.mail.humanAddr); got != 1 {
		t.Fatalf("inactive Agent B unread after Agent A Mail reentry = %d, want 1", got)
	}
	if reentered.agentRail.rows[0].unread != 0 || reentered.agentRail.rows[1].unread != 0 || reentered.agentRail.rows[2].unread != 1 {
		t.Fatalf(
			"cached Main/A/B unread after Mail reentry = %d/%d/%d, want 0/0/1",
			reentered.agentRail.rows[0].unread, reentered.agentRail.rows[1].unread, reentered.agentRail.rows[2].unread,
		)
	}

	reopened, err := fs.OpenRailUnreadStore(projectRoot, targets, acceptedMessages, reentered.mail.humanAddr)
	if err != nil {
		t.Fatalf("reopen reentry unread state: %v", err)
	}
	if got := reopened.UnreadCount(targets[1], acceptedMessages, reentered.mail.humanAddr); got != 0 {
		t.Fatalf("restart Agent A unread after Mail reentry = %d, want 0", got)
	}
	if got := reopened.UnreadCount(targets[2], acceptedMessages, reentered.mail.humanAddr); got != 1 {
		t.Fatalf("restart Agent B unread after Mail reentry = %d, want 1", got)
	}
	if scanner.scans.Load() != 2 || inventoryScript.calls != 1 || len(worker.requests) != 1 {
		t.Fatalf(
			"Mail reentry ownership before commands run: mail scans=%d inventory=%d workers=%d, want 2/1/1",
			scanner.scans.Load(), inventoryScript.calls, len(worker.requests),
		)
	}
}
