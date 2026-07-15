package tui

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestPR5Stage5KeyboardAndMouseReturnToAggregateMainThroughSharedRailActivation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		activate func(*testing.T, App) (App, tea.Cmd)
	}{
		{
			name: "keyboard",
			activate: func(t *testing.T, app App) (App, tea.Cmd) {
				t.Helper()
				app = pr5UpdateRailFocusApp(t, app, tea.KeyPressMsg{Code: tea.KeyUp})
				if app.agentRail.cursor != 0 {
					t.Fatalf("Main keyboard selection cursor=%d, want 0", app.agentRail.cursor)
				}
				return installationDeliverApp(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
			},
		},
		{
			name: "mouse",
			activate: func(t *testing.T, app App) (App, tea.Cmd) {
				t.Helper()
				return installationDeliverApp(t, app, pr5RailMouseClick(t, app.layoutBudget(), pr5RailFirstRowLocalY))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, scanner, _ := installationNewApp(t, 0)
			targetDir := filepath.Join(app.projectDir, "agent-a")
			installationWriteAgent(t, targetDir, "agent-a", "Agent A", "Agent A")
			installationWriteEvents(t, targetDir, 1, "event-a")

			mainMail := pr5ProjectionMail(
				"main-in", app.mail.orchAddr, "human", nil,
				"Main aggregate mail", "2026-07-15T01:00:00Z",
			)
			agentMail := pr5ProjectionMail(
				"a-in", "agent-a", "human", nil,
				"Agent A aggregate mail", "2026-07-15T01:01:00Z",
			)
			scanner.messages = []fs.MailMessage{mainMail, agentMail}

			inventoryScript := &pr5RailInventoryScanScript{steps: []pr5RailInventoryScanStep{{
				snapshot: pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 7501),
			}}}
			app.setAgentRailInventoryScanner(inventoryScript.Scan)
			inventoryResult := pr5RunTrailingRailInventoryScan(t, app.Init(), inventoryScript)
			app, _ = installationDeliverApp(t, app, inventoryResult)
			app, _ = installationAcceptInitial(t, app)
			app = pr5UpdateRailFocusApp(t, app, tea.WindowSizeMsg{Width: 84, Height: 24})
			app = pr5UpdateRailFocusApp(t, app, tea.KeyPressMsg{Code: tea.KeyTab})
			app = pr5UpdateRailFocusApp(t, app, tea.KeyPressMsg{Code: tea.KeyDown})

			if app.agentRail.cursor != 1 || len(app.agentRail.rows) != 2 ||
				!app.agentRail.rows[0].originalMain || app.agentRail.rows[1].originalMain {
				t.Fatalf("ordinary fixture rail cursor=%d rows=%#v, want selected Agent A after Main", app.agentRail.cursor, app.agentRail.rows)
			}
			rootSnapshot := app.mailStore.snapshot
			if rootSnapshot == nil {
				t.Fatal("ordinary fixture has no accepted root snapshot")
			}
			worker := &pr5RailActivationWorker{}
			app.threadLoads = newThreadLoadCoordinator(worker)
			app.mailStore.pollRate = time.Nanosecond

			ordinary, ordinaryCmd := installationDeliverApp(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
			completion, ok := pr5FindRailThreadLoadResult(ordinaryCmd)
			if !ok || completion.err != nil || completion.sessionCache == nil {
				t.Fatalf("ordinary setup completion=(ok=%v cache=%p err=%v), want one successful cold direct result", ok, completion.sessionCache, completion.err)
			}
			ordinary, followup := installationDeliverApp(t, ordinary, completion)
			if followup != nil {
				t.Fatalf("ordinary setup publication returned follow-up %T, want nil", runCmd(followup))
			}
			if ordinary.mailStore.binding.target.policy != asyncTargetHomeAgentRail ||
				ordinary.currentThread.target.policy != asyncTargetHomeAgentRail ||
				ordinary.mail.asyncBinding.target.policy != asyncTargetHomeAgentRail {
				t.Fatalf("ordinary setup policies store/thread/mail=%v/%v/%v, want Agent A",
					ordinary.mailStore.binding.target.policy, ordinary.currentThread.target.policy, ordinary.mail.asyncBinding.target.policy)
			}
			if got := pr5SortedVisibleBodies(ordinary.mail.messages); !reflect.DeepEqual(got, []string{"Agent A aggregate mail"}) {
				t.Fatalf("ordinary direct projection bodies=%v, want only Agent A direct mail at the default verbosity", got)
			}
			ordinaryCache := ordinary.mail.sessionCache
			ordinary.mail.input.SetValue("ordinary draft must not cross into Main")

			beforeStoreID := ordinary.mailStore.id
			beforeStoreVersion := ordinary.mailStore.version
			beforeStoreCache := ordinary.mailStore.cache
			beforeTickChain := ordinary.mailStore.tickChain
			beforeScans := scanner.scans.Load()
			beforeInventoryScans := inventoryScript.calls
			beforeCounters := ordinary.threadLoads.Counters()
			beforeRows := append([]railRow(nil), ordinary.agentRail.rows...)
			beforeGeneration := ordinary.mailGeneration

			main, mainCmd := tc.activate(t, ordinary)
			if main.agentRail.cursor != 0 || main.mailFocus != mailFocusRail || main.mail.input.Focused() {
				t.Fatalf("Main activation selection/focus cursor=%d focus=%v input=%v, want selected rail-only Main",
					main.agentRail.cursor, main.mailFocus, main.mail.input.Focused())
			}
			if main.mailStore.binding.target.policy != asyncTargetHomeMain ||
				main.currentThread.target.policy != asyncTargetHomeMain ||
				main.mail.asyncBinding.target.policy != asyncTargetHomeMain {
				t.Fatalf("Main activation policies store/thread/mail=%v/%v/%v, want aggregate Main",
					main.mailStore.binding.target.policy, main.currentThread.target.policy, main.mail.asyncBinding.target.policy)
			}
			if main.mailGeneration <= beforeGeneration || main.mailStore.binding.generation != main.mailGeneration ||
				main.currentThread.generation != main.mailGeneration || main.mail.generation != main.mailGeneration {
				t.Fatalf("Main activation generations store/thread/mail/app=%d/%d/%d/%d, want one fresh generation > %d",
					main.mailStore.binding.generation, main.currentThread.generation, main.mail.generation, main.mailGeneration, beforeGeneration)
			}
			if main.mailStore.id != beforeStoreID || main.mailStore.version != beforeStoreVersion ||
				main.mailStore.snapshot != rootSnapshot || !reflect.DeepEqual(main.mailStore.cache, beforeStoreCache) {
				t.Fatalf("Main activation replaced/mutated root owner: id=%d/%d version=%d/%d snapshot=%p/%p",
					main.mailStore.id, beforeStoreID, main.mailStore.version, beforeStoreVersion,
					main.mailStore.snapshot, rootSnapshot)
			}
			if main.mail.acceptedSnapshot != rootSnapshot || main.currentThread.acceptedSnapshotVersion != rootSnapshot.Version() ||
				main.mail.asyncStoreVersion != rootSnapshot.Version() || main.currentThread.sessionCache != main.mail.sessionCache ||
				main.mail.sessionCache == nil || main.mail.sessionCache == ordinaryCache {
				t.Fatalf("Main projection ownership snapshot=%p/%p version=%d/%d mailVersion=%d threadCache=%p mailCache=%p ordinaryCache=%p",
					main.mail.acceptedSnapshot, rootSnapshot, main.currentThread.acceptedSnapshotVersion, rootSnapshot.Version(),
					main.mail.asyncStoreVersion, main.currentThread.sessionCache, main.mail.sessionCache, ordinaryCache)
			}
			if main.mail.orchestrator != main.orchDir || main.mail.orchAddr != main.agentRail.rows[0].directTarget.Address ||
				main.mail.input.Value() != "" {
				t.Fatalf("Main presentation target/draft orchestrator=%q/%q address=%q/%q draft=%q",
					main.mail.orchestrator, main.orchDir, main.mail.orchAddr, main.agentRail.rows[0].directTarget.Address, main.mail.input.Value())
			}
			wantBodies := []string{"Agent A aggregate mail", "Main aggregate mail"}
			if got := pr5SortedVisibleBodies(main.mail.messages); !reflect.DeepEqual(got, wantBodies) {
				t.Fatalf("returned Main bodies=%v, want exact aggregate projection %v", got, wantBodies)
			}
			if scanner.scans.Load() != beforeScans || inventoryScript.calls != beforeInventoryScans ||
				main.threadLoads.Counters() != beforeCounters || len(worker.requests) != 1 {
				t.Fatalf("Main return started work: mailbox=%d/%d inventory=%d/%d counters=%#v/%#v workerRequests=%d",
					scanner.scans.Load(), beforeScans, inventoryScript.calls, beforeInventoryScans,
					main.threadLoads.Counters(), beforeCounters, len(worker.requests))
			}
			if !reflect.DeepEqual(main.agentRail.rows, beforeRows) || main.mailStore.tickChain == beforeTickChain || !main.mailStore.tickRunning {
				t.Fatalf("Main return rows/tick rowsEqual=%v tick=(%d,%v) before=%d, want unchanged rows and one live rotated chain",
					reflect.DeepEqual(main.agentRail.rows, beforeRows), main.mailStore.tickChain, main.mailStore.tickRunning, beforeTickChain)
			}
			if mainCmd == nil {
				t.Fatal("Main return command = nil; want the sole root store's resumed tick command")
			}
		})
	}
}
