package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestPR5Stage4VisibleOrdinaryAcceptedColdProjectionAdvancesOnlyActiveTarget(t *testing.T) {
	installationTestStart(t)

	app, scanner, _ := installationNewApp(t, 0)
	projectRoot := filepath.Dir(app.projectDir)
	targetADir := filepath.Join(app.projectDir, "agent-a")
	targetBDir := filepath.Join(app.projectDir, "agent-b")
	installationWriteAgent(t, targetADir, "agent-a", "Agent A", "Agent A")
	installationWriteAgent(t, targetBDir, "agent-b", "Agent B", "Agent B")

	acceptedInventory := pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 6701)
	agentB := pr5RailLifecycleSnapshot(app, "agent-b", "Agent B", 6702)
	acceptedInventory.Records = append(acceptedInventory.Records, agentB.Records...)
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
			"ordinary unread baseline: ready=%v targets=%d store=%v snapshot=%v, want true/3/live/live",
			ready, len(targets), app.railUnreadStore != nil, app.mailStore.snapshot != nil,
		)
	}
	for i, label := range []string{"Main", "Agent A", "Agent B"} {
		if got := app.railUnreadStore.UnreadCount(targets[i], app.mailStore.snapshot.cache.Messages, app.mail.humanAddr); got != 0 {
			t.Fatalf("historical %s unread after baseline = %d, want 0", label, got)
		}
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
	if got := app.railUnreadStore.UnreadCount(targets[0], acceptedMessages, app.mail.humanAddr); got != 0 {
		t.Fatalf("visible Main unread after accepted refresh = %d, want 0", got)
	}
	if got := app.railUnreadStore.UnreadCount(targets[1], acceptedMessages, app.mail.humanAddr); got != 1 {
		t.Fatalf("inactive Agent A unread before activation = %d, want 1", got)
	}
	if got := app.railUnreadStore.UnreadCount(targets[2], acceptedMessages, app.mail.humanAddr); got != 1 {
		t.Fatalf("inactive Agent B unread before activation = %d, want 1", got)
	}
	if app.agentRail.rows[0].unread != 0 || app.agentRail.rows[1].unread != 1 || app.agentRail.rows[2].unread != 1 {
		t.Fatalf(
			"cached Main/A/B unread before activation = %d/%d/%d, want 0/1/1",
			app.agentRail.rows[0].unread, app.agentRail.rows[1].unread, app.agentRail.rows[2].unread,
		)
	}

	statePath := fs.RailUnreadStatePath(projectRoot)
	beforeActivationState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read unread state before ordinary activation: %v", err)
	}
	beforeMailScans := scanner.scans.Load()
	beforeInventoryScans := inventoryScript.calls
	beforeVersion := app.mailStore.version
	worker := &pr5RailActivationWorker{}
	app.threadLoads = newThreadLoadCoordinator(worker)
	app.mailStore.pollRate = time.Nanosecond
	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	app.agentRail.cursor = 1
	rowA := app.agentRail.rows[1]

	activated, activationCmd := app.activateOrdinaryRailRow(rowA)
	if activationCmd == nil {
		t.Fatal("ordinary Agent A activation returned nil cold-load command")
	}
	if !activated.mail.ready || !activated.mail.initialLoading || activated.currentView != appViewMail ||
		activated.mailStore.binding.target != rowA.target || activated.mail.asyncBinding != activated.mailStore.binding ||
		activated.currentThread.target != rowA.target || activated.currentThread.generation != activated.mail.generation {
		t.Fatalf(
			"ordinary activation loading coordinate: ready=%v loading=%v view=%v store=%#v mail=%#v thread=%#v threadGeneration=%d mailGeneration=%d",
			activated.mail.ready, activated.mail.initialLoading, activated.currentView,
			activated.mailStore.binding.target, activated.mail.asyncBinding.target, activated.currentThread.target,
			activated.currentThread.generation, activated.mail.generation,
		)
	}
	if activated.mailStore.snapshot != rootSnapshot || activated.mailStore.version != beforeVersion ||
		scanner.scans.Load() != beforeMailScans || inventoryScript.calls != beforeInventoryScans {
		t.Fatalf(
			"ordinary activation changed accepted owner before completion: snapshotChanged=%v version=%d/%d mailScans=%d/%d inventoryScans=%d/%d",
			activated.mailStore.snapshot != rootSnapshot, activated.mailStore.version, beforeVersion,
			scanner.scans.Load(), beforeMailScans, inventoryScript.calls, beforeInventoryScans,
		)
	}
	if got := activated.railUnreadStore.UnreadCount(targets[1], acceptedMessages, activated.mail.humanAddr); got != 1 {
		t.Fatalf("Agent A unread at activation time = %d, want preserved 1 until accepted cold projection", got)
	}
	afterActivationState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read unread state after ordinary activation: %v", err)
	}
	if !bytes.Equal(afterActivationState, beforeActivationState) {
		t.Fatal("ordinary activation advanced durable unread before accepted cold projection")
	}
	if !activated.agentRail.moveCursor(1) || activated.agentRail.cursor != 2 {
		t.Fatalf("move rail cursor away from active Agent A: cursor=%d, want Agent B index 2", activated.agentRail.cursor)
	}

	completion, ok := pr5FindRailThreadLoadResult(activationCmd)
	if !ok || completion.err != nil || completion.sessionCache == nil {
		t.Fatalf("ordinary Agent A cold completion: found=%v cache=%p err=%v, want accepted candidate", ok, completion.sessionCache, completion.err)
	}
	if len(worker.requests) != 1 || !reflect.DeepEqual(worker.requests[0].acceptedMessages, acceptedMessages) {
		t.Fatalf("ordinary cold worker requests=%d accepted=%#v, want one exact detached root snapshot", len(worker.requests), worker.requests)
	}
	published, followup := installationDeliverApp(t, activated, completion)
	if followup != nil {
		t.Fatalf("accepted ordinary cold completion returned unexpected follow-up %T", runCmd(followup))
	}

	if published.currentView != appViewMail || !published.mail.ready || published.mail.initialLoading ||
		published.mailStore.binding.target != rowA.target || published.mail.asyncBinding != published.mailStore.binding ||
		published.currentThread.target != rowA.target || published.currentThread.generation != published.mail.generation ||
		published.mail.acceptedSnapshot != rootSnapshot || published.mail.asyncStoreVersion != rootSnapshot.Version() ||
		published.currentThread.acceptedSnapshotVersion != rootSnapshot.Version() || published.agentRail.cursor != 2 {
		t.Fatalf(
			"accepted visible Agent A coordinate: view=%v ready=%v loading=%v store=%#v mail=%#v thread=%#v generations=%d/%d sameSnapshot=%v versions=%d/%d/%d cursor=%d",
			published.currentView, published.mail.ready, published.mail.initialLoading,
			published.mailStore.binding.target, published.mail.asyncBinding.target, published.currentThread.target,
			published.currentThread.generation, published.mail.generation,
			published.mail.acceptedSnapshot == rootSnapshot,
			published.mail.asyncStoreVersion, published.currentThread.acceptedSnapshotVersion, rootSnapshot.Version(),
			published.agentRail.cursor,
		)
	}
	if got := pr5SortedVisibleBodies(published.mail.messages); !reflect.DeepEqual(got, []string{
		"historical Agent A mail", "later Agent A mail",
	}) {
		t.Fatalf("accepted visible Agent A direct bodies = %v, want exact A-only projection", got)
	}
	if published.mailStore.snapshot != rootSnapshot || published.mailStore.version != beforeVersion ||
		scanner.scans.Load() != beforeMailScans || inventoryScript.calls != beforeInventoryScans {
		t.Fatalf(
			"accepted ordinary projection changed root owner: snapshotChanged=%v version=%d/%d mailScans=%d/%d inventoryScans=%d/%d",
			published.mailStore.snapshot != rootSnapshot, published.mailStore.version, beforeVersion,
			scanner.scans.Load(), beforeMailScans, inventoryScript.calls, beforeInventoryScans,
		)
	}
	if got := published.railUnreadStore.UnreadCount(targets[0], acceptedMessages, published.mail.humanAddr); got != 0 {
		t.Fatalf("Main unread after visible Agent A projection = %d, want independently 0", got)
	}
	if got := published.railUnreadStore.UnreadCount(targets[2], acceptedMessages, published.mail.humanAddr); got != 1 {
		t.Fatalf("cursor-selected inactive Agent B unread after Agent A projection = %d, want independently 1", got)
	}
	if got := published.railUnreadStore.UnreadCount(targets[1], acceptedMessages, published.mail.humanAddr); got != 0 {
		t.Fatalf("visible active Agent A unread after exact accepted cold projection = %d, want advanced to 0", got)
	}

	if published.agentRail.rows[0].unread != 0 || published.agentRail.rows[1].unread != 0 || published.agentRail.rows[2].unread != 1 {
		t.Fatalf(
			"cached Main/A/B unread after visible Agent A projection = %d/%d/%d, want 0/0/1",
			published.agentRail.rows[0].unread, published.agentRail.rows[1].unread, published.agentRail.rows[2].unread,
		)
	}
	railView := ansi.Strip(published.agentRail.View(24, 10, published.mail.orchDisplayName()))
	pr5RequireNoRenderedUnreadCount(t, pr5RailRenderedLine(t, railView, "Agent A"), 1)
	pr5RequireRenderedUnreadCount(t, pr5RailRenderedLine(t, railView, "Agent B"), 1)

	reopened, err := fs.OpenRailUnreadStore(projectRoot, targets, acceptedMessages, published.mail.humanAddr)
	if err != nil {
		t.Fatalf("reopen ordinary visible unread state: %v", err)
	}
	if got := reopened.UnreadCount(targets[0], acceptedMessages, published.mail.humanAddr); got != 0 {
		t.Fatalf("restart-visible Main unread after Agent A projection = %d, want 0", got)
	}
	if got := reopened.UnreadCount(targets[1], acceptedMessages, published.mail.humanAddr); got != 0 {
		t.Fatalf("restart-visible Agent A unread after accepted projection = %d, want 0", got)
	}
	if got := reopened.UnreadCount(targets[2], acceptedMessages, published.mail.humanAddr); got != 1 {
		t.Fatalf("restart-visible Agent B unread after Agent A projection = %d, want 1", got)
	}
}
