package tui

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestPR5Stage4VisibleMainRefreshAdvancesOnlyMainNotCursorOrAggregateRows(t *testing.T) {
	installationTestStart(t)

	app, scanner, _ := installationNewApp(t, 0)
	projectRoot := filepath.Dir(app.projectDir)
	targetADir := filepath.Join(app.projectDir, "agent-a")
	installationWriteAgent(t, targetADir, "agent-a", "Agent A", "Agent A")

	acceptedInventory := pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 6501)
	inventoryScript := &pr5RailInventoryScanScript{steps: []pr5RailInventoryScanStep{{
		snapshot: acceptedInventory,
	}}}
	setter, ok := any(&app).(pr5AgentRailInventoryScannerSetter)
	if !ok {
		t.Fatal("App has no root-owned agent-rail inventory lifecycle boundary")
	}
	setter.setAgentRailInventoryScanner(inventoryScript.Scan)
	inventoryResult := pr5RunTrailingRailInventoryScan(t, app.Init(), inventoryScript)
	app, _ = installationDeliverApp(t, app, inventoryResult)
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent A"})

	historyMain := pr5ProjectionMail(
		"historical-main", app.mail.orchAddr, "human", nil,
		"historical Main mail", "2026-07-15T00:00:00Z",
	)
	historyA := pr5ProjectionMail(
		"historical-a", "agent-a", "human", nil,
		"historical Agent A mail", "2026-07-15T00:00:00Z",
	)
	scanner.messages = []fs.MailMessage{historyMain, historyA}
	app, _ = installationAcceptInitial(t, app)
	app, _ = app.updateMailChildWindowSize(app.layoutBudget().ChildWindowSize())

	if app.currentView != appViewMail || !app.mail.ready || app.mail.initialLoading {
		t.Fatalf(
			"initial visible Main readiness: view=%v ready=%v loading=%v, want visible ready Mail",
			app.currentView, app.mail.ready, app.mail.initialLoading,
		)
	}
	if app.mailStore.binding.target.policy != asyncTargetHomeMain ||
		app.currentThread.target.policy != asyncTargetHomeMain ||
		app.mail.asyncBinding.target.policy != asyncTargetHomeMain {
		t.Fatalf(
			"initial active binding policies: store=%v thread=%v mail=%v, want Main",
			app.mailStore.binding.target.policy,
			app.currentThread.target.policy,
			app.mail.asyncBinding.target.policy,
		)
	}

	targets, ready := app.agentRail.acceptedDirectTargets(app.mailStore.binding.owner)
	if !ready || len(targets) != 2 {
		t.Fatalf("accepted Main+A unread targets: ready=%v len=%d, want true/2", ready, len(targets))
	}
	if app.railUnreadStore == nil || app.mailStore.snapshot == nil {
		t.Fatalf("initial unread baseline: store=%v snapshot=%v, want both live", app.railUnreadStore != nil, app.mailStore.snapshot != nil)
	}
	if got := app.railUnreadStore.UnreadCount(targets[0], app.mailStore.snapshot.cache.Messages, app.mail.humanAddr); got != 0 {
		t.Fatalf("historical Main unread after baseline = %d, want 0", got)
	}
	if got := app.railUnreadStore.UnreadCount(targets[1], app.mailStore.snapshot.cache.Messages, app.mail.humanAddr); got != 0 {
		t.Fatalf("historical Agent A unread after baseline = %d, want 0", got)
	}

	// Cursor movement is selection only. Keep aggregate Main as the active async
	// binding while Agent A is selected, so advancement cannot follow the cursor.
	if !app.agentRail.moveCursor(1) || app.agentRail.cursor != 1 {
		t.Fatalf("move cursor to Agent A: cursor=%d, want 1", app.agentRail.cursor)
	}
	if app.mailStore.binding.target.policy != asyncTargetHomeMain || app.currentThread.target.policy != asyncTargetHomeMain {
		t.Fatalf(
			"cursor movement changed active target: store=%v thread=%v, want Main",
			app.mailStore.binding.target.policy, app.currentThread.target.policy,
		)
	}

	laterMain := pr5ProjectionMail(
		"later-main", app.mail.orchAddr, "human", nil,
		"later Main mail", "2026-07-15T00:01:00Z",
	)
	laterA := pr5ProjectionMail(
		"later-a", "agent-a", "human", nil,
		"later Agent A mail", "2026-07-15T00:01:00Z",
	)
	acceptedLater := []fs.MailMessage{historyMain, historyA, laterMain, laterA}
	scanner.messages = acceptedLater
	beforeMailScans := scanner.scans.Load()
	beforeInventoryScans := inventoryScript.calls
	steady := installationRefreshResult(t, &app, false)
	app, _ = installationDeliverApp(t, app, steady)

	if got := scanner.scans.Load(); got != beforeMailScans+1 {
		t.Fatalf("continuous-open accepted refresh scans = %d, want %d", got, beforeMailScans+1)
	}
	if got := inventoryScript.calls; got != beforeInventoryScans {
		t.Fatalf("continuous-open accepted refresh inventory scans = %d, want unchanged %d", got, beforeInventoryScans)
	}
	if app.currentView != appViewMail || !app.mail.ready || app.mail.initialLoading ||
		app.mailStore.binding.target.policy != asyncTargetHomeMain ||
		app.currentThread.target.policy != asyncTargetHomeMain ||
		app.agentRail.cursor != 1 {
		t.Fatalf(
			"accepted visible Main coordinate: view=%v ready=%v loading=%v store=%v thread=%v cursor=%d",
			app.currentView, app.mail.ready, app.mail.initialLoading,
			app.mailStore.binding.target.policy, app.currentThread.target.policy, app.agentRail.cursor,
		)
	}
	if app.mail.acceptedSnapshot != app.mailStore.snapshot ||
		app.currentThread.acceptedSnapshotVersion != app.mailStore.snapshot.Version() ||
		app.currentThread.generation != app.mail.generation {
		t.Fatalf(
			"accepted Main projection coordinate: sameSnapshot=%v threadVersion=%d storeVersion=%d threadGeneration=%d mailGeneration=%d",
			app.mail.acceptedSnapshot == app.mailStore.snapshot,
			app.currentThread.acceptedSnapshotVersion,
			app.mailStore.snapshot.Version(),
			app.currentThread.generation,
			app.mail.generation,
		)
	}
	wantVisibleBodies := []string{
		"historical Agent A mail",
		"historical Main mail",
		"later Agent A mail",
		"later Main mail",
	}
	if got := pr5SortedVisibleBodies(app.mail.messages); !reflect.DeepEqual(got, wantVisibleBodies) {
		t.Fatalf("accepted visible Main messages = %v, want exact aggregate projection %v", got, wantVisibleBodies)
	}

	acceptedMessages := app.mailStore.snapshot.cache.Messages
	if got := app.railUnreadStore.UnreadCount(targets[0], acceptedMessages, app.mail.humanAddr); got != 0 {
		t.Fatalf("visible active Main unread after accepted refresh = %d, want advanced to 0", got)
	}
	if got := app.railUnreadStore.UnreadCount(targets[1], acceptedMessages, app.mail.humanAddr); got != 1 {
		t.Fatalf("cursor-selected aggregate-visible Agent A unread = %d, want inactive 1", got)
	}
	if app.agentRail.rows[0].unread != 0 || app.agentRail.rows[1].unread != 1 {
		t.Fatalf("cached Main/Agent A unread = %d/%d, want 0/1", app.agentRail.rows[0].unread, app.agentRail.rows[1].unread)
	}

	railView := ansi.Strip(app.agentRail.View(24, 8, app.mail.orchDisplayName()))
	pr5RequireNoRenderedUnreadCount(t, pr5RailRenderedLine(t, railView, app.mail.orchDisplayName()), 1)
	pr5RequireRenderedUnreadCount(t, pr5RailRenderedLine(t, railView, "Agent A"), 1)

	reopened, err := fs.OpenRailUnreadStore(projectRoot, targets, acceptedMessages, app.mail.humanAddr)
	if err != nil {
		t.Fatalf("reopen advanced visible Main unread state: %v", err)
	}
	if got := reopened.UnreadCount(targets[0], acceptedMessages, app.mail.humanAddr); got != 0 {
		t.Fatalf("restart-visible Main unread after accepted visible refresh = %d, want 0", got)
	}
	if got := reopened.UnreadCount(targets[1], acceptedMessages, app.mail.humanAddr); got != 1 {
		t.Fatalf("restart-visible inactive Agent A unread = %d, want 1", got)
	}
}
