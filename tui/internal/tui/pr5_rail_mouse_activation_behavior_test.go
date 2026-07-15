package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/inventory"
)

const pr5RailFirstRowLocalY = 2 // title, then one blank line

func pr5RailMouseClick(t *testing.T, budget LayoutBudget, localY int) tea.MouseClickMsg {
	t.Helper()
	terminalX := budget.RailX
	localX, ok := budget.RailLocalX(terminalX)
	if !ok || localX != 0 {
		t.Fatalf("RailLocalX(%d) = (%d, %v), want the visible rail origin", terminalX, localX, ok)
	}
	terminalY := budget.TopChromeRows + localY
	if terminalY < budget.TopChromeRows || terminalY >= budget.TopChromeRows+budget.ChildHeight {
		t.Fatalf("rail local y=%d translated to terminal y=%d outside child [%d,%d)",
			localY, terminalY, budget.TopChromeRows, budget.TopChromeRows+budget.ChildHeight)
	}
	return tea.MouseClickMsg(tea.Mouse{X: terminalX, Y: terminalY, Button: tea.MouseLeft})
}

func TestPR5Stage3RailMouseHitTestsRowsAndSharesOrdinaryActivation(t *testing.T) {
	app, scanner, _ := installationNewApp(t, 0)
	targetDir := filepath.Join(app.projectDir, "agent-a")
	installationWriteAgent(t, targetDir, "agent-a", "Agent A", "Agent A")

	app, _ = installationAcceptInitial(t, app)
	owner := app.asyncCurrent().binding.owner
	app.agentRail.installInventory(owner, inventory.Snapshot{
		FilterDir: filepath.Dir(app.projectDir),
		Records: []inventory.Record{{
			PID:                     4101,
			Agent:                   "agent-a",
			Project:                 filepath.Dir(app.projectDir),
			AgentDir:                targetDir,
			Address:                 "agent-a",
			AgentName:               "Agent A",
			Nickname:                "Agent A",
			ManifestAddressVerified: true,
			Role:                    inventory.RoleAgent,
			Enterable:               false,
		}},
	})
	app = pr5UpdateRailFocusApp(t, app, tea.WindowSizeMsg{Width: 84, Height: 24})
	budget := app.layoutBudget()
	if !budget.RailVisible || budget.TopChromeRows != app.topChromeRows() {
		t.Fatalf("mouse fixture budget=%#v, want one visible rail and authoritative top chrome", budget)
	}
	if len(app.agentRail.rows) != 2 || !app.agentRail.rows[0].originalMain || app.agentRail.rows[1].originalMain {
		t.Fatalf("rail rows=%#v, want Main followed by one ordinary row", app.agentRail.rows)
	}
	ordinary := app.agentRail.rows[1]

	// Put the cursor on the ordinary row through the real focus/key route, then
	// return focus to chat. A click on rail chrome must focus only and preserve
	// that cursor and every active-thread coordinate.
	app = pr5UpdateRailFocusApp(t, app, tea.KeyPressMsg{Code: tea.KeyTab})
	app = pr5UpdateRailFocusApp(t, app, tea.KeyPressMsg{Code: tea.KeyDown})
	app = pr5UpdateRailFocusApp(t, app, tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.agentRail.cursor != 1 || !app.mail.input.Focused() {
		t.Fatalf("precondition cursor=%d chatFocused=%v, want ordinary cursor with chat focus", app.agentRail.cursor, app.mail.input.Focused())
	}

	worker := &pr5RailActivationWorker{}
	app.threadLoads = newThreadLoadCoordinator(worker)
	app.mailStore.pollRate = time.Nanosecond
	var revalidations int
	app.setAsyncTargetRevalidator(func(gotOwner asyncOwner, gotTarget asyncTarget) bool {
		revalidations++
		if gotOwner != owner || gotTarget != ordinary.target {
			t.Errorf("mouse activation revalidation owner=%#v target=%#v, want owner=%#v ordinary=%#v", gotOwner, gotTarget, owner, ordinary.target)
		}
		return true
	})

	beforeBinding := app.mailStore.binding
	beforeThread := app.currentThread
	beforeGeneration := app.mailGeneration
	beforeScans := scanner.scans.Load()

	focused, focusCmd := installationDeliverApp(t, app, pr5RailMouseClick(t, budget, 1))
	if focusCmd != nil {
		t.Fatalf("click on rail blank line returned command %T, want focus only", focusCmd)
	}
	if focused.mailFocus != mailFocusRail || focused.mail.input.Focused() || focused.agentRail.cursor != 1 {
		t.Fatalf("blank-line click focus=%v chatFocused=%v cursor=%d, want rail focus with unchanged ordinary cursor",
			focused.mailFocus, focused.mail.input.Focused(), focused.agentRail.cursor)
	}
	if revalidations != 0 || focused.mailStore.binding != beforeBinding || focused.currentThread != beforeThread ||
		focused.mailGeneration != beforeGeneration || scanner.scans.Load() != beforeScans {
		t.Fatalf("blank-line click activated or mutated thread state: revalidations=%d binding=%#v generation=%d scans=%d",
			revalidations, focused.mailStore.binding, focused.mailGeneration, scanner.scans.Load())
	}

	// Main participates in the same row hit-test and selection, but current Enter
	// semantics do not cold-load Main. Returning from an ordinary thread is a
	// separate behavior boundary; this seam only selects Main without inventing it.
	mainSelected, mainCmd := installationDeliverApp(t, focused, pr5RailMouseClick(t, budget, pr5RailFirstRowLocalY))
	if mainCmd != nil {
		t.Fatalf("Main-row click returned command %T, want selection without an ordinary cold load", mainCmd)
	}
	if mainSelected.agentRail.cursor != 0 {
		t.Fatalf("Main-row click cursor=%d, want 0", mainSelected.agentRail.cursor)
	}
	if revalidations != 0 || mainSelected.mailStore.binding != beforeBinding || mainSelected.currentThread != beforeThread ||
		mainSelected.mailGeneration != beforeGeneration || scanner.scans.Load() != beforeScans {
		t.Fatalf("Main-row click started an ordinary activation: revalidations=%d binding=%#v generation=%d scans=%d",
			revalidations, mainSelected.mailStore.binding, mainSelected.mailGeneration, scanner.scans.Load())
	}

	clicked, activationCmd := installationDeliverApp(t, mainSelected, pr5RailMouseClick(t, budget, pr5RailFirstRowLocalY+1))
	if clicked.agentRail.cursor != 1 {
		t.Fatalf("ordinary-row click cursor=%d, want 1", clicked.agentRail.cursor)
	}
	if revalidations != 1 {
		t.Fatalf("ordinary-row click revalidations=%d, want the one prospective gate shared with Enter", revalidations)
	}
	if clicked.mailStore.binding.target != ordinary.target || clicked.mailGeneration <= beforeGeneration ||
		clicked.mailStore.binding.generation != clicked.mailGeneration {
		t.Fatalf("ordinary-row click binding=%#v generation=%d, want selected target and one fresh generation > %d",
			clicked.mailStore.binding, clicked.mailGeneration, beforeGeneration)
	}
	if activationCmd == nil {
		t.Fatal("ordinary-row click returned nil, want the cold direct-load command shared with Enter")
	}
	completion, ok := pr5FindRailThreadLoadResult(activationCmd)
	if !ok || completion.err != nil || completion.sessionCache == nil {
		t.Fatalf("ordinary-row click completion=(ok=%v cache=%p err=%v), want one successful cold direct result", ok, completion.sessionCache, completion.err)
	}
	if len(worker.requests) != 1 || worker.requests[0].envelope.target != ordinary.target {
		t.Fatalf("ordinary-row click physical requests=%#v, want exactly one request for the selected target", worker.requests)
	}
	if scanner.scans.Load() != beforeScans {
		t.Fatalf("ordinary-row click root scans=%d, want unchanged %d", scanner.scans.Load(), beforeScans)
	}
}
