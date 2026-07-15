package tui

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

func TestPR5Stage4OrdinaryActivationSizesReplacementMailBeforeColdProjection(t *testing.T) {
	installationTestStart(t)

	app, scanner, _ := installationNewApp(t, 0)
	targetDir := filepath.Join(app.projectDir, "agent-a")
	installationWriteAgent(t, targetDir, "agent-a", "Agent A", "Agent A")
	scanner.messages = []fs.MailMessage{
		pr5ProjectionMail("a-in", "agent-a", "human", nil, "mail-a-in", "2026-07-15T01:00:00Z"),
	}
	app, _ = installationAcceptInitial(t, app)

	owner := app.asyncCurrent().binding.owner
	app.agentRail.installInventory(owner, inventory.Snapshot{
		FilterDir: filepath.Dir(app.projectDir),
		Records: []inventory.Record{{
			PID:                     6601,
			Agent:                   "agent-a",
			Project:                 filepath.Dir(app.projectDir),
			AgentDir:                targetDir,
			Address:                 "agent-a",
			AgentName:               "Agent A",
			Nickname:                "Agent A",
			ManifestAddressVerified: true,
			Role:                    inventory.RoleAgent,
		}},
	})
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent A"})

	app = pr5UpdateRailFocusApp(t, app, tea.WindowSizeMsg{Width: 84, Height: 24})
	if !app.mail.ready {
		t.Fatal("sized Main fixture is not render-ready")
	}
	childSize := app.layoutBudget().ChildWindowSize()
	row := app.agentRail.rows[1]
	if row.originalMain || row.target.policy != asyncTargetHomeAgentRail {
		t.Fatalf("ordinary activation row = %#v, want home Agent A", row)
	}

	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	app.threadLoads = newThreadLoadCoordinator(&pr5RailActivationWorker{})
	beforeSnapshot := app.mailStore.snapshot
	beforeVersion := app.mailStore.version
	beforeScans := scanner.scans.Load()

	activated, cmd := app.activateOrdinaryRailRow(row)
	if cmd == nil {
		t.Fatal("ordinary activation returned nil cold-load command")
	}
	if activated.mailStore.snapshot != beforeSnapshot || activated.mailStore.version != beforeVersion || scanner.scans.Load() != beforeScans {
		t.Fatalf(
			"ordinary activation changed root accepted state before cold completion: snapshotChanged=%v version=%d/%d scans=%d/%d",
			activated.mailStore.snapshot != beforeSnapshot,
			activated.mailStore.version, beforeVersion,
			scanner.scans.Load(), beforeScans,
		)
	}
	if !activated.mail.initialLoading {
		t.Fatal("ordinary replacement Mail stopped loading before its cold projection was accepted")
	}
	if !activated.mail.ready || activated.mail.width != childSize.Width || activated.mail.height != childSize.Height {
		t.Fatalf(
			"ordinary replacement Mail ready=%v size=%dx%d, want render-ready exact child size %dx%d before cold completion",
			activated.mail.ready, activated.mail.width, activated.mail.height,
			childSize.Width, childSize.Height,
		)
	}
	if activated.currentView != appViewMail || activated.mailStore.binding.target != row.target ||
		activated.currentThread.target != row.target || activated.currentThread.generation != activated.mail.generation {
		t.Fatalf(
			"ordinary visible loading coordinate: view=%v store=%#v thread=%#v threadGeneration=%d mailGeneration=%d",
			activated.currentView, activated.mailStore.binding.target, activated.currentThread.target,
			activated.currentThread.generation, activated.mail.generation,
		)
	}
}
