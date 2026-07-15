package tui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestPR5Stage4HiddenRailKeepsOrdinaryChatVisibleForUnreadAdvance(t *testing.T) {
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
	wideModel, _ := app.Update(tea.WindowSizeMsg{Width: 84, Height: 24})
	app = wideModel.(App)
	wideBudget := app.layoutBudget()
	if !wideBudget.RailVisible || wideBudget.RailWidth != 24 || wideBudget.ContentWidth != 60 {
		t.Fatalf("wide activation budget = %+v, want visible 24-column rail plus 60-column chat", wideBudget)
	}

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
		t.Fatalf("inactive Agent A unread before hidden-rail activation = %d, want 1", got)
	}
	if got := app.railUnreadStore.UnreadCount(targets[2], acceptedMessages, app.mail.humanAddr); got != 1 {
		t.Fatalf("inactive Agent B unread before hidden-rail activation = %d, want 1", got)
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

	hiddenModel, _ := activated.Update(tea.WindowSizeMsg{Width: 83, Height: 24})
	hidden := hiddenModel.(App)
	hiddenBudget := hidden.layoutBudget()
	if hiddenBudget.RailVisible || hiddenBudget.RailWidth != 0 || hiddenBudget.ContentX != 0 ||
		hiddenBudget.ContentWidth != 83 || hiddenBudget.ChildWindowSize().Width != 83 {
		t.Fatalf("hidden-rail chat budget = %+v child=%+v, want zero rail and full-width 83-column chat", hiddenBudget, hiddenBudget.ChildWindowSize())
	}
	if got, want := ansi.Strip(hidden.View().Content), ansi.Strip(hidden.mail.View()); got != want {
		t.Fatalf("hidden-rail loading frame must remain the full Mail chat:\n--- root ---\n%s\n--- mail ---\n%s", got, want)
	}
	if hidden.currentView != appViewMail || hidden.mailStore.binding.target != rowA.target ||
		hidden.currentThread.target != rowA.target || hidden.mailStore.snapshot != rootSnapshot {
		t.Fatalf(
			"hidden-rail loading coordinate: view=%v storeTarget=%#v threadTarget=%#v sameRoot=%v",
			hidden.currentView, hidden.mailStore.binding.target, hidden.currentThread.target,
			hidden.mailStore.snapshot == rootSnapshot,
		)
	}

	completion, found := pr5FindRailThreadLoadResult(activationCmd)
	if !found || completion.err != nil || completion.sessionCache == nil {
		t.Fatalf("ordinary Agent A cold completion: found=%v cache=%p err=%v, want accepted candidate", found, completion.sessionCache, completion.err)
	}
	if len(worker.requests) != 1 || !reflect.DeepEqual(worker.requests[0].acceptedMessages, acceptedMessages) {
		t.Fatalf("ordinary hidden-rail worker requests=%d accepted=%#v, want one exact detached root snapshot", len(worker.requests), worker.requests)
	}
	published, followup := installationDeliverApp(t, hidden, completion)
	if followup != nil {
		t.Fatalf("accepted hidden-rail ordinary completion returned unexpected follow-up %T", runCmd(followup))
	}

	publishedBudget := published.layoutBudget()
	if publishedBudget.RailVisible || publishedBudget.RailWidth != 0 || publishedBudget.ContentWidth != 83 ||
		published.mail.initialLoading || published.mail.acceptedSnapshot != rootSnapshot ||
		published.mail.asyncStoreVersion != rootSnapshot.Version() ||
		published.currentThread.acceptedSnapshotVersion != rootSnapshot.Version() {
		t.Fatalf(
			"published hidden-rail coordinate: budget=%+v loading=%v sameRoot=%v versions=%d/%d/%d",
			publishedBudget, published.mail.initialLoading, published.mail.acceptedSnapshot == rootSnapshot,
			published.mail.asyncStoreVersion, published.currentThread.acceptedSnapshotVersion, rootSnapshot.Version(),
		)
	}
	visibleBodies := pr5SortedVisibleBodies(published.mail.messages)
	if !reflect.DeepEqual(visibleBodies, []string{"historical Agent A mail", "later Agent A mail"}) {
		t.Fatalf("published hidden-rail Agent A bodies = %v, want exact A-only projection", visibleBodies)
	}
	if got := published.railUnreadStore.UnreadCount(targets[0], acceptedMessages, published.mail.humanAddr); got != 0 {
		t.Fatalf("Main unread after hidden-rail Agent A publication = %d, want 0", got)
	}
	if got := published.railUnreadStore.UnreadCount(targets[1], acceptedMessages, published.mail.humanAddr); got != 0 {
		t.Fatalf("visible Agent A unread with rail hidden = %d, want advanced 0", got)
	}
	if got := published.railUnreadStore.UnreadCount(targets[2], acceptedMessages, published.mail.humanAddr); got != 1 {
		t.Fatalf("inactive Agent B unread after hidden-rail Agent A publication = %d, want 1", got)
	}
	if published.agentRail.rows[0].unread != 0 || published.agentRail.rows[1].unread != 0 || published.agentRail.rows[2].unread != 1 {
		t.Fatalf(
			"cached Main/A/B unread with rail hidden = %d/%d/%d, want 0/0/1",
			published.agentRail.rows[0].unread, published.agentRail.rows[1].unread, published.agentRail.rows[2].unread,
		)
	}
	rootView := ansi.Strip(published.View().Content)
	mailView := ansi.Strip(published.mail.View())
	if rootView != mailView {
		t.Fatal("hidden rail did not render the Mail child unchanged at full chat width")
	}
	if !strings.Contains(rootView, "later Agent A mail") {
		t.Fatalf(
			"accepted hidden-rail projection advanced Agent A unread but did not render its accepted chat bodies; model bodies=%v",
			visibleBodies,
		)
	}

	reopened, err := fs.OpenRailUnreadStore(projectRoot, targets, acceptedMessages, published.mail.humanAddr)
	if err != nil {
		t.Fatalf("reopen hidden-rail unread state: %v", err)
	}
	if got := reopened.UnreadCount(targets[1], acceptedMessages, published.mail.humanAddr); got != 0 {
		t.Fatalf("restart Agent A unread after hidden-rail publication = %d, want 0", got)
	}
	if got := reopened.UnreadCount(targets[2], acceptedMessages, published.mail.humanAddr); got != 1 {
		t.Fatalf("restart Agent B unread after hidden-rail publication = %d, want 1", got)
	}
}
