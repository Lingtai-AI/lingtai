package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

type pr5UnreadCompletionGuardFixture struct {
	app               App
	scanner           *installationScriptedScanner
	inventoryScript   *pr5RailInventoryScanScript
	projectRoot       string
	humanAddress      string
	targets           []fs.DirectTarget
	acceptedMessages  []fs.MailMessage
	rowA              railRow
	rowB              railRow
	homeUnread        *fs.RailUnreadStore
	durableBefore     []byte
	worker            *pr5BlockingThreadLoadWorker
	activationResults <-chan tea.Msg
	flight            pr5ThreadLoadFlight
}

func TestPR5Stage4RejectedOrdinaryCompletionsCannotPublishOrAdvanceUnread(t *testing.T) {
	scenarios := []struct {
		name          string
		wantFollowup  bool
		wantStarted   uint64
		wantCoalesced uint64
		wantMailScans int64
	}{
		{name: "inventory_revalidation_rejected", wantStarted: 1, wantMailScans: 2},
		{name: "same_target_new_generation", wantFollowup: true, wantStarted: 2, wantCoalesced: 1, wantMailScans: 2},
		{name: "different_target", wantStarted: 2, wantMailScans: 2},
		{name: "accepted_store_version_advanced", wantFollowup: true, wantStarted: 2, wantCoalesced: 1, wantMailScans: 3},
		{name: "entered_project_visit", wantStarted: 1, wantMailScans: 2},
		{name: "owner_suspended", wantStarted: 1, wantMailScans: 2},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := pr5NewUnreadCompletionGuardFixture(t)
			guarded := fixture.app

			switch scenario.name {
			case "inventory_revalidation_rejected":
				var calls int
				guarded.setAsyncTargetRevalidator(func(gotOwner asyncOwner, gotTarget asyncTarget) bool {
					calls++
					if gotOwner != fixture.flight.request.envelope.owner || gotTarget != fixture.rowA.target {
						t.Errorf("rejected completion revalidated owner=%#v target=%#v, want original A owner=%#v target=%#v",
							gotOwner, gotTarget, fixture.flight.request.envelope.owner, fixture.rowA.target)
					}
					return false
				})
				t.Cleanup(func() {
					if calls != 1 {
						t.Errorf("rejected completion inventory revalidations = %d, want exactly one publication gate", calls)
					}
				})

			case "same_target_new_generation":
				var cmd tea.Cmd
				guarded, cmd = guarded.activateOrdinaryRailRow(fixture.rowA)
				if cmd == nil || guarded.mailStore.binding.target != fixture.rowA.target ||
					guarded.mailStore.binding.generation == fixture.flight.request.envelope.generation.thread {
					t.Fatalf("same-target reactivation did not install a fresh A generation: cmd=%v binding=%#v oldGeneration=%d",
						cmd != nil, guarded.mailStore.binding, fixture.flight.request.envelope.generation.thread)
				}

			case "different_target":
				var cmd tea.Cmd
				guarded, cmd = guarded.activateOrdinaryRailRow(fixture.rowB)
				if cmd == nil || guarded.mailStore.binding.target != fixture.rowB.target ||
					guarded.currentThread.target != fixture.rowB.target {
					t.Fatalf("different-target activation did not install B loading state: cmd=%v store=%#v thread=%#v",
						cmd != nil, guarded.mailStore.binding.target, guarded.currentThread.target)
				}

			case "accepted_store_version_advanced":
				oldVersion := guarded.mailStore.version
				refresh := installationRefreshResult(t, &guarded, false)
				var cmd tea.Cmd
				guarded, cmd = installationDeliverApp(t, guarded, refresh)
				if guarded.mailStore.version <= oldVersion || guarded.mail.asyncStoreVersion != oldVersion ||
					guarded.currentThread.acceptedSnapshotVersion != oldVersion || !guarded.mail.initialLoading {
					t.Fatalf("accepted root advance did not stage loading A on its published version: versions=%d/%d/%d old=%d loading=%v",
						guarded.mailStore.version, guarded.mail.asyncStoreVersion,
						guarded.currentThread.acceptedSnapshotVersion, oldVersion, guarded.mail.initialLoading)
				}
				// The accepted refresh may return only root-owned location work. Do not run
				// it here: this contract controls the already-running A completion exactly.
				_ = cmd

			case "entered_project_visit":
				visitedProject := filepath.Join(filepath.Dir(filepath.Dir(guarded.projectDir)), "visited-project")
				var cmd tea.Cmd
				guarded, cmd = guarded.enterVisitedAgent(ProjectsAgentSelectedMsg{
					Record: visitRecord(visitedProject, "visited-agent", "Visited Agent"),
				})
				if cmd == nil || !guarded.visiting || guarded.mailStore.binding.target.policy != asyncTargetProjectVisit ||
					guarded.suspendedHomeMailStore == nil {
					t.Fatalf("production visit transition did not suspend home and install visited Mail: cmd=%v visiting=%v policy=%v suspendedHome=%v",
						cmd != nil, guarded.visiting, guarded.mailStore.binding.target.policy,
						guarded.suspendedHomeMailStore != nil)
				}

			case "owner_suspended":
				oldActivation := guarded.mailStore.binding.owner.activation
				guarded.mailStore.suspend()
				if guarded.mailStore.active || guarded.mailStore.binding.owner.activation == oldActivation {
					t.Fatalf("production store suspension did not retire the active owner: active=%v activation=%d/%d",
						guarded.mailStore.active, guarded.mailStore.binding.owner.activation, oldActivation)
				}
			}

			beforeThread := guarded.currentThread
			beforeMailCache := guarded.mail.sessionCache
			beforeMailSnapshot := guarded.mail.acceptedSnapshot
			beforeMailVersion := guarded.mail.asyncStoreVersion
			beforeLoading := guarded.mail.initialLoading
			beforeBodies := pr5SortedVisibleBodies(guarded.mail.messages)

			completionCache, err := (directThreadLoadWorker{}).Load(fixture.flight.request)
			if err != nil {
				t.Fatalf("build controlled original A completion: %v", err)
			}
			if completionCache == beforeThread.sessionCache || completionCache == beforeMailCache {
				t.Fatal("controlled rejected completion reused an installed cache and cannot prove non-publication")
			}
			fixture.flight.release <- pr5ThreadLoadReply{sessionCache: completionCache}
			completion := pr5AwaitThreadLoadResult(t, fixture.activationResults, "original A completion")
			if completion.sessionCache != completionCache || completion.err != nil {
				t.Fatalf("controlled original A completion cache=%p/%p err=%v", completion.sessionCache, completionCache, completion.err)
			}

			settled, followup := installationDeliverApp(t, guarded, completion)
			if (followup != nil) != scenario.wantFollowup {
				t.Fatalf("rejected completion follow-up present=%v, want %v", followup != nil, scenario.wantFollowup)
			}
			if settled.currentThread != beforeThread || settled.mail.sessionCache != beforeMailCache ||
				settled.mail.acceptedSnapshot != beforeMailSnapshot || settled.mail.asyncStoreVersion != beforeMailVersion ||
				settled.mail.initialLoading != beforeLoading {
				t.Fatalf("rejected completion published direct state: threadChanged=%v mailCacheChanged=%v snapshotChanged=%v version=%d/%d loading=%v/%v",
					settled.currentThread != beforeThread, settled.mail.sessionCache != beforeMailCache,
					settled.mail.acceptedSnapshot != beforeMailSnapshot, settled.mail.asyncStoreVersion,
					beforeMailVersion, settled.mail.initialLoading, beforeLoading)
			}
			if got := pr5SortedVisibleBodies(settled.mail.messages); !reflect.DeepEqual(got, beforeBodies) {
				t.Fatalf("rejected completion changed visible direct bodies: got=%v want=%v", got, beforeBodies)
			}
			mailFrame := settled.mail.View()
			if strings.Contains(mailFrame, "historical Agent A mail") || strings.Contains(mailFrame, "later Agent A mail") {
				t.Fatalf("rejected original A completion escaped into retained/current Mail frame:\n%s", mailFrame)
			}

			pr5RequireRejectedOrdinaryUnreadPreserved(t, fixture, settled)
			pr5RequireThreadLoadCounters(t, settled.threadLoads.Counters(), ThreadLoadCounters{
				Started:       scenario.wantStarted,
				Coalesced:     scenario.wantCoalesced,
				Completed:     1,
				TrueCancelled: 0,
				StaleDropped:  1,
			})
			if got := fixture.scanner.scans.Load(); got != scenario.wantMailScans {
				t.Fatalf("mail scans after rejected completion = %d, want %d", got, scenario.wantMailScans)
			}
			if fixture.inventoryScript.calls != 1 {
				t.Fatalf("inventory scans after rejected completion = %d, want exactly 1", fixture.inventoryScript.calls)
			}
			select {
			case extra := <-fixture.worker.started:
				t.Fatalf("rejected completion unexpectedly started another physical worker for target=%#v generation=%d version=%d",
					extra.request.envelope.target, extra.request.envelope.generation.thread, extra.request.envelope.storeVersion)
			default:
			}
		})
	}
}

func pr5NewUnreadCompletionGuardFixture(t *testing.T) pr5UnreadCompletionGuardFixture {
	t.Helper()
	installationTestStart(t)

	app, scanner, _ := installationNewApp(t, 0)
	projectRoot := filepath.Dir(app.projectDir)
	targetADir := filepath.Join(app.projectDir, "agent-a")
	targetBDir := filepath.Join(app.projectDir, "agent-b")
	installationWriteAgent(t, targetADir, "agent-a", "Agent A", "Agent A")
	installationWriteAgent(t, targetBDir, "agent-b", "Agent B", "Agent B")

	acceptedInventory := pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 7101)
	agentB := pr5RailLifecycleSnapshot(app, "agent-b", "Agent B", 7102)
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
		t.Fatalf("guard baseline targets: ready=%v count=%d unreadStore=%v, want true/3/live",
			ready, len(targets), app.railUnreadStore != nil)
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
	acceptedMessages := append([]fs.MailMessage(nil), app.mailStore.snapshot.cache.Messages...)
	if got := app.railUnreadStore.UnreadCount(targets[1], acceptedMessages, app.mail.humanAddr); got != 1 {
		t.Fatalf("guard baseline Agent A unread = %d, want 1", got)
	}
	if got := app.railUnreadStore.UnreadCount(targets[2], acceptedMessages, app.mail.humanAddr); got != 1 {
		t.Fatalf("guard baseline Agent B unread = %d, want 1", got)
	}
	if app.agentRail.rows[0].unread != 0 || app.agentRail.rows[1].unread != 1 || app.agentRail.rows[2].unread != 1 {
		t.Fatalf("guard cached baseline Main/A/B unread = %d/%d/%d, want 0/1/1",
			app.agentRail.rows[0].unread, app.agentRail.rows[1].unread, app.agentRail.rows[2].unread)
	}

	durableBefore, err := os.ReadFile(fs.RailUnreadStatePath(projectRoot))
	if err != nil {
		t.Fatalf("read guard baseline durable unread state: %v", err)
	}
	worker := newPR5BlockingThreadLoadWorker(t)
	app.threadLoads = newThreadLoadCoordinator(worker)
	app.mailStore.pollRate = time.Nanosecond
	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	app.agentRail.cursor = 1
	rowA := app.agentRail.rows[1]
	rowB := app.agentRail.rows[2]
	loading, activationCmd := app.activateOrdinaryRailRow(rowA)
	if activationCmd == nil || !loading.mail.ready || !loading.mail.initialLoading ||
		loading.mailStore.binding.target != rowA.target || loading.currentThread.target != rowA.target {
		t.Fatalf("guard original A activation: cmd=%v ready=%v loading=%v store=%#v thread=%#v",
			activationCmd != nil, loading.mail.ready, loading.mail.initialLoading,
			loading.mailStore.binding.target, loading.currentThread.target)
	}
	activationResults := pr5StartBatchCommands(t, activationCmd, "guard original A activation")
	flight := pr5AwaitThreadLoadFlight(t, worker, "guard original A")
	if flight.request.envelope.target != rowA.target ||
		flight.request.envelope.generation.thread != loading.mailStore.binding.generation ||
		flight.request.envelope.storeVersion != loading.mailStore.version ||
		!reflect.DeepEqual(flight.request.acceptedMessages, acceptedMessages) {
		t.Fatalf("guard original A flight coordinates: envelope=%#v accepted=%#v, want row A generation=%d version=%d exact root messages",
			flight.request.envelope, flight.request.acceptedMessages,
			loading.mailStore.binding.generation, loading.mailStore.version)
	}

	return pr5UnreadCompletionGuardFixture{
		app:               loading,
		scanner:           scanner,
		inventoryScript:   inventoryScript,
		projectRoot:       projectRoot,
		humanAddress:      loading.mail.humanAddr,
		targets:           targets,
		acceptedMessages:  acceptedMessages,
		rowA:              rowA,
		rowB:              rowB,
		homeUnread:        loading.railUnreadStore,
		durableBefore:     durableBefore,
		worker:            worker,
		activationResults: activationResults,
		flight:            flight,
	}
}

func pr5RequireRejectedOrdinaryUnreadPreserved(t *testing.T, fixture pr5UnreadCompletionGuardFixture, got App) {
	t.Helper()

	if got.railUnreadStore != fixture.homeUnread {
		t.Fatalf("rejected completion replaced the root-owned unread store: got=%p want=%p", got.railUnreadStore, fixture.homeUnread)
	}
	for i, want := range []int{0, 1, 1} {
		if unread := fixture.homeUnread.UnreadCount(fixture.targets[i], fixture.acceptedMessages, fixture.humanAddress); unread != want {
			t.Fatalf("live Main/A/B unread[%d] after rejected completion = %d, want %d", i, unread, want)
		}
		if got.agentRail.rows[i].unread != want {
			t.Fatalf("cached Main/A/B unread[%d] after rejected completion = %d, want %d", i, got.agentRail.rows[i].unread, want)
		}
	}

	durableAfter, err := os.ReadFile(fs.RailUnreadStatePath(fixture.projectRoot))
	if err != nil {
		t.Fatalf("read durable unread state after rejected completion: %v", err)
	}
	if !bytes.Equal(durableAfter, fixture.durableBefore) {
		t.Fatal("rejected completion changed durable unread state bytes")
	}
	reopened, err := fs.OpenRailUnreadStore(fixture.projectRoot, fixture.targets, fixture.acceptedMessages, fixture.humanAddress)
	if err != nil {
		t.Fatalf("reopen durable unread state after rejected completion: %v", err)
	}
	for i, want := range []int{0, 1, 1} {
		if unread := reopened.UnreadCount(fixture.targets[i], fixture.acceptedMessages, fixture.humanAddress); unread != want {
			t.Fatalf("restart Main/A/B unread[%d] after rejected completion = %d, want %d", i, unread, want)
		}
	}
}
