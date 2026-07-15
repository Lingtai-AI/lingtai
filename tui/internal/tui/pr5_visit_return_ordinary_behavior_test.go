package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

func TestPR5Stage5VisitReturnPreservesOrdinaryRailTargetWithoutMainEscalation(t *testing.T) {
	app, targetA := pr5OrdinarySendApp(t, "agent-a", "Agent A", 7101, 7)
	beforeBinding := app.mailStore.binding
	if beforeBinding.target.policy != asyncTargetHomeAgentRail ||
		beforeBinding.target.directory != filepath.Clean(targetA) {
		t.Fatalf("precondition ordinary binding = %#v, want exact Agent A rail target", beforeBinding)
	}
	if len(app.agentRail.rows) == 0 || !app.agentRail.rows[0].originalMain {
		t.Fatalf("precondition Main row missing: %#v", app.agentRail.rows)
	}
	beforeMain := app.agentRail.rows[0].directTarget
	app.mail.pendingMessage = "ordinary A draft"
	app.mail.input.SetValue("ordinary A draft")

	visited, _ := app.enterVisitedAgent(ProjectsAgentSelectedMsg{
		Record: visitRecord(t.TempDir(), "worker", "Worker"),
	})
	if !visited.visiting || visited.visitReturn == nil || visited.suspendedHomeMailStore == nil {
		t.Fatalf(
			"visit did not suspend exact ordinary home state: visiting=%v return=%v store=%v",
			visited.visiting, visited.visitReturn != nil, visited.suspendedHomeMailStore != nil,
		)
	}

	restored, resumeCmd := visited.returnFromVisit()
	if resumeCmd == nil || restored.visiting {
		t.Fatalf("ordinary visit return: cmd=%v visiting=%v, want resume/non-visiting", resumeCmd != nil, restored.visiting)
	}
	binding := restored.mailStore.binding
	if binding.target != beforeBinding.target || binding.target.policy != asyncTargetHomeAgentRail {
		t.Fatalf("ordinary A returned as %#v, want preserved rail target %#v", binding.target, beforeBinding.target)
	}
	if restored.mail.asyncBinding != binding || restored.currentThread.target != binding.target ||
		restored.currentThread.generation != restored.mail.generation {
		t.Fatalf(
			"ordinary visit return coordinates diverged: mail=%#v store=%#v thread=%#v mailGen=%d threadGen=%d",
			restored.mail.asyncBinding, binding, restored.currentThread.target,
			restored.mail.generation, restored.currentThread.generation,
		)
	}
	if restored.mail.orchestrator != targetA || restored.mail.orchAddr != "agent-a" {
		t.Fatalf(
			"ordinary visit return identity = (%q,%q), want (%q,%q)",
			restored.mail.orchestrator, restored.mail.orchAddr, targetA, "agent-a",
		)
	}
	if len(restored.agentRail.rows) == 0 || !restored.agentRail.rows[0].originalMain ||
		restored.agentRail.rows[0].directTarget != beforeMain {
		t.Fatalf(
			"ordinary visit return overwrote synthetic Main: got=%#v want=%#v",
			restored.agentRail.rows, beforeMain,
		)
	}
	if restored.mail.pendingMessage != "ordinary A draft" || restored.mail.input.Value() != "ordinary A draft" {
		t.Fatalf(
			"ordinary visit return lost bound draft: pending=%q input=%q",
			restored.mail.pendingMessage, restored.mail.input.Value(),
		)
	}
}

func TestPR5Stage5VisitReturnStartsOrdinaryRefreshAndKeepsTickBeforeInventoryResult(t *testing.T) {
	app, targetA := pr5OrdinarySendApp(t, "agent-a", "Agent A", 7101, 7)
	binding := app.mailStore.binding
	lifecycle := app.ensureAgentRailInventoryLifecycle()
	_, requestSequence := lifecycle.schedule()
	record := inventory.Record{
		PID:                     binding.target.pid,
		Project:                 filepath.Dir(binding.owner.projectID),
		AgentDir:                targetA,
		Address:                 "agent-a",
		AgentName:               "Agent A",
		Nickname:                "Agent A",
		ManifestAddressVerified: true,
		Role:                    inventory.RoleAgent,
	}
	if !lifecycle.acceptLatest(requestSequence, binding.owner, []inventory.Record{record}, nil) {
		t.Fatal("precondition did not accept exact ordinary home inventory")
	}
	if !lifecycle.revalidateTarget(binding.owner, binding.target) {
		t.Fatal("precondition exact ordinary target is not authorized by accepted inventory")
	}
	// The shared send fixture uses an explicit always-true revalidator. Release
	// that unrelated test bypass so visit install/return binds the sole accepted
	// inventory lifecycle exactly as production does.
	app.mailStore.setAsyncTargetRevalidator(nil)
	if app.mailStore.revalidateTargetExplicit {
		t.Fatal("precondition ordinary store retained an explicit test revalidator")
	}
	app.setAgentRailInventoryScanner(func(inventory.Options) (inventory.Snapshot, error) {
		return inventory.Snapshot{}, errors.New("delayed home inventory")
	})

	visited, _ := app.enterVisitedAgent(ProjectsAgentSelectedMsg{
		Record: visitRecord(t.TempDir(), "worker", "Worker"),
	})
	if visited.suspendedHomeMailStore == nil {
		t.Fatal("visit did not retain the exact ordinary home store")
	}
	visited.suspendedHomeMailStore.pollRate = time.Nanosecond

	restored, resumeCmd := visited.returnFromVisit()
	if resumeCmd == nil {
		t.Fatal("ordinary visit return produced no resume batch")
	}
	if !restored.mailStore.refreshInFlight || !restored.mailStore.refreshInitial {
		t.Fatalf(
			"ordinary visit return did not start its initial refresh: inFlight=%v initial=%v",
			restored.mailStore.refreshInFlight, restored.mailStore.refreshInitial,
		)
	}

	var (
		refreshCount int
		tick         projectMailTickMsg
		haveTick     bool
		inventoryErr error
	)
	for _, msg := range installationRunBatch(t, resumeCmd) {
		switch msg := msg.(type) {
		case projectMailRefreshMsg:
			refreshCount++
		case projectMailTickMsg:
			tick = msg
			haveTick = true
		case agentRailInventoryResultMsg:
			inventoryErr = msg.err
		}
	}
	if refreshCount != 1 {
		t.Fatalf("ordinary visit return refresh messages=%d, want one initial refresh", refreshCount)
	}
	if !haveTick {
		t.Fatal("ordinary visit return produced no refresh tick")
	}
	if inventoryErr == nil || inventoryErr.Error() != "delayed home inventory" {
		t.Fatalf("inventory result precondition error=%v, want delayed home inventory", inventoryErr)
	}
	_, tickCmd := installationDeliverApp(t, restored, tick)
	if tickCmd == nil {
		t.Fatal("ordinary refresh tick stopped before the delayed inventory result could authorize a new owner")
	}
}

func TestPR5Stage5RenderedSyntheticMainUsesLocalizedLabel(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("zh"); err != nil {
		t.Fatal(err)
	}
	app, _, _ := installationNewApp(t, 0)
	app.mail.orchName = "Coordinator"
	if len(app.agentRail.rows) == 0 || !app.agentRail.rows[0].originalMain {
		t.Fatalf("precondition synthetic Main missing: %#v", app.agentRail.rows)
	}
	app.agentRail.installMain(app.mail.orchDisplayName(), app.agentRail.rows[0].directTarget)

	rendered := app.agentRail.View(24, 6, app.mail.orchDisplayName())
	if !strings.Contains(rendered, i18n.T("rail.main")) {
		t.Fatalf("rendered synthetic Main=%q, want localized label %q", rendered, i18n.T("rail.main"))
	}
	if strings.Contains(rendered, "Coordinator") {
		t.Fatalf("rendered synthetic Main leaked orchestrator display name: %q", rendered)
	}
}
