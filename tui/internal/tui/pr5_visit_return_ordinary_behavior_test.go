package tui

import (
	"path/filepath"
	"testing"
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
