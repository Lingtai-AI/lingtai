package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/doctorreport"
)

// healNeedingPlan is a plan with an out-of-date kernel, the simplest fixable
// finding: NeedsHeal()==true, TUIHealable()==false.
func healNeedingPlan() config.UpdatePlan {
	return config.UpdatePlan{
		Kernel: config.KernelStatus{Installed: "0.9.6", Latest: "0.9.7", NeedsUpdate: true},
	}
}

// healDoctor returns a sized DoctorModel whose diagnose/heal seams are stubbed:
// diagnoseFn returns resultLines + plan, healFn records its calls.
func healDoctor(t *testing.T, plan config.UpdatePlan, healCalls *[]config.UpdatePlan) DoctorModel {
	t.Helper()
	m := NewDoctorModel("/tmp/orch", "/tmp/global")
	m.diagnoseFn = func() doctorResultMsg {
		return doctorResultMsg{
			Lines: []doctorLine{{Text: "✓ diagnosed", OK: true}},
			Draft: &doctorreport.Draft{
				GeneratedAt: time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC),
				AgentName:   "orch",
				Lines:       []doctorreport.Line{{Severity: doctorreport.SeverityOK, Text: "✓ diagnosed"}},
			},
			HealPlan: plan,
		}
	}
	m.healFn = func(p config.UpdatePlan) tea.Cmd {
		return func() tea.Msg {
			if healCalls != nil {
				*healCalls = append(*healCalls, p)
			}
			return doctorHealStepMsg{Lines: []doctorLine{{Text: "✓ healed", OK: true}}}
		}
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return m
}

func TestDoctorHealPromptShownWhenNeedsHeal(t *testing.T) {
	var calls []config.UpdatePlan
	m := healDoctor(t, healNeedingPlan(), &calls)
	m, _ = m.Update(runCmd(m.Init()).(doctorResultMsg))

	if m.state != doctorStateHealPrompt {
		t.Fatalf("heal-needing plan must enter doctorStateHealPrompt, got %v", m.state)
	}
	if len(calls) != 0 {
		t.Fatal("entering the prompt must not run the heal")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, i18n.T("doctor.heal_prompt")) {
		t.Fatalf("prompt view should contain the heal prompt line:\n%s", view)
	}
	if !strings.Contains(view, i18n.T("doctor.heal_actions_header")) {
		t.Fatalf("prompt view should render the actions header:\n%s", view)
	}
	kernelAction := i18n.TF("doctor.heal_action_kernel", "0.9.6", "0.9.7")
	if !strings.Contains(view, kernelAction) {
		t.Fatalf("prompt view should enumerate the kernel action %q:\n%s", kernelAction, view)
	}
	if !strings.Contains(view, i18n.T("doctor.heal_action_bootstrap")) {
		t.Fatalf("prompt view should always enumerate the bootstrap action:\n%s", view)
	}
	// TUIHealable()==false: the TUI action must not appear.
	if tuiAction := strings.TrimSpace(i18n.TF("doctor.heal_action_tui", "")); strings.Contains(view, tuiAction) {
		t.Fatalf("non-healable TUI must not appear in the action list (%q):\n%s", tuiAction, view)
	}
	if !strings.Contains(view, i18n.T("doctor.heal_apply")) || !strings.Contains(view, i18n.T("doctor.heal_cancel")) {
		t.Fatalf("prompt view should render the Apply/Cancel selector:\n%s", view)
	}
}

func TestDoctorNoPromptWhenHealthy(t *testing.T) {
	var calls []config.UpdatePlan
	m := healDoctor(t, config.UpdatePlan{}, &calls)
	m, _ = m.Update(runCmd(m.Init()).(doctorResultMsg))

	if m.state != doctorStateReport {
		t.Fatalf("NeedsHeal()==false must stay in doctorStateReport, got %v", m.state)
	}
	view := ansi.Strip(m.View())
	if strings.Contains(view, i18n.T("doctor.heal_prompt")) {
		t.Fatalf("healthy result must not render the heal prompt:\n%s", view)
	}
	if !strings.Contains(view, i18n.T("doctor.healthy_ok")) {
		t.Fatalf("healthy result should render the explicit healthy summary line:\n%s", view)
	}
	if len(calls) != 0 {
		t.Fatal("healthy result must never run the heal")
	}
}

func TestDoctorHealCancelReturnsToReport(t *testing.T) {
	var calls []config.UpdatePlan
	m := healDoctor(t, healNeedingPlan(), &calls)
	m, _ = m.Update(runCmd(m.Init()).(doctorResultMsg))

	// Move selection to Cancel and confirm.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.healConfirmIdx != 1 {
		t.Fatalf("right should select Cancel (idx 1), got %d", m.healConfirmIdx)
	}
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("cancel must not schedule any work")
	}
	if m.state != doctorStateReport {
		t.Fatalf("cancel should return to doctorStateReport, got %v", m.state)
	}
	if len(calls) != 0 {
		t.Fatal("cancel must not run the heal")
	}
	// Report lines stay visible after cancel.
	if !strings.Contains(ansi.Strip(m.View()), "✓ diagnosed") {
		t.Fatalf("report lines should remain visible after cancel:\n%s", m.View())
	}
}

func TestDoctorHealApplyRunsHealOnceThenRerunsDiagnostics(t *testing.T) {
	var calls []config.UpdatePlan
	plan := healNeedingPlan()
	m := healDoctor(t, plan, &calls)
	m, _ = m.Update(runCmd(m.Init()).(doctorResultMsg))

	// confirmIdx defaults to 0 (Apply); press enter.
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.state != doctorStateHealing {
		t.Fatalf("apply should enter doctorStateHealing, got %v", m.state)
	}
	if len(calls) != 0 {
		t.Fatal("the heal must run asynchronously via the returned Cmd, not inline in Update")
	}
	if !strings.Contains(ansi.Strip(m.View()), i18n.T("doctor.healing")) {
		t.Fatalf("healing state should render the healing line:\n%s", m.View())
	}

	healMsg := runCmd(cmd)
	if len(calls) != 1 {
		t.Fatalf("apply must run the heal exactly once, got %d", len(calls))
	}
	if calls[0].Kernel.Installed != plan.Kernel.Installed ||
		calls[0].Kernel.Latest != plan.Kernel.Latest ||
		calls[0].Kernel.NeedsUpdate != plan.Kernel.NeedsUpdate {
		t.Fatalf("healFn must receive the same plan, got %+v", calls[0])
	}

	// Heal done: the model stashes the heal lines and re-runs diagnostics.
	m, cmd = m.Update(healMsg)
	if !m.loading {
		t.Fatal("after the heal completes the model should re-run diagnostics (loading)")
	}
	if cmd == nil {
		t.Fatal("doctorHealDoneMsg should schedule a fresh diagnostic")
	}
	resultMsg := runCmd(cmd).(doctorResultMsg)
	m, _ = m.Update(resultMsg)

	// The merged report starts with the heal section, then the heal lines,
	// then the fresh diagnostic lines.
	if len(m.lines) < 3 {
		t.Fatalf("merged report too short: %#v", m.lines)
	}
	if !m.lines[0].Section || m.lines[0].Text != i18n.T("doctor.heal_section") {
		t.Fatalf("merged report must start with the heal section header, got %+v", m.lines[0])
	}
	if m.lines[1].Text != "✓ healed" {
		t.Fatalf("heal lines should follow the section header, got %+v", m.lines[1])
	}
	if m.lines[2].Text != i18n.T("doctor.heal_done") || !m.lines[2].OK {
		t.Fatalf("the heal completion line should follow the heal output, got %+v", m.lines[2])
	}
	if m.lines[len(m.lines)-1].Text != "✓ diagnosed" {
		t.Fatalf("fresh diagnostic lines should follow the heal lines, got %+v", m.lines)
	}
	// The saved draft mirrors the screen, heal section included.
	if m.draft == nil || len(m.draft.Lines) != len(m.lines) {
		t.Fatalf("draft lines must mirror the merged screen lines: draft=%+v", m.draft)
	}
	if m.draft.Lines[0].Text != i18n.T("doctor.heal_section") {
		t.Fatalf("draft should start with the heal section header, got %+v", m.draft.Lines[0])
	}
	if len(m.pendingHealLines) != 0 {
		t.Fatal("pendingHealLines must be cleared after the merge")
	}
}

func TestDoctorHealPromptArrowsStillScroll(t *testing.T) {
	// The prompt must not steal the review keys: up/down keep scrolling the
	// viewport so the user can read the full report before deciding.
	var calls []config.UpdatePlan
	m := NewDoctorModel("/tmp/orch", "/tmp/global")
	lines := make([]doctorLine, 0, 200)
	for i := 0; i < 200; i++ {
		lines = append(lines, doctorLine{Text: "line", OK: true})
	}
	m.diagnoseFn = func() doctorResultMsg {
		return doctorResultMsg{Lines: lines, HealPlan: healNeedingPlan()}
	}
	m.healFn = func(p config.UpdatePlan) tea.Cmd {
		return func() tea.Msg {
			calls = append(calls, p)
			return doctorHealStepMsg{}
		}
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(runCmd(m.Init()).(doctorResultMsg))
	if m.state != doctorStateHealPrompt {
		t.Fatalf("expected prompt state, got %v", m.state)
	}
	for i := 0; i < 5; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.viewport.YOffset() == 0 {
		t.Fatal("down arrow in the prompt state should still scroll the viewport")
	}
	if m.healConfirmIdx != 0 {
		t.Fatalf("scrolling must not move the Apply/Cancel selection, got %d", m.healConfirmIdx)
	}
	if len(calls) != 0 {
		t.Fatal("scrolling must not run the heal")
	}
}

func TestDoctorHealingBlocksRefresh(t *testing.T) {
	// ctrl+r while a heal is in flight must be rejected: a concurrent
	// diagnostic would still see the stale pre-heal state, re-open the prompt
	// over the running cascade, and allow a second overlapping heal.
	var calls []config.UpdatePlan
	m := healDoctor(t, healNeedingPlan(), &calls)
	m, _ = m.Update(runCmd(m.Init()).(doctorResultMsg))
	m, healCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.state != doctorStateHealing {
		t.Fatalf("apply should enter doctorStateHealing, got %v", m.state)
	}

	m, cmd := m.Update(ctrlR())
	if cmd != nil {
		t.Fatal("ctrl+r during a heal must not start a diagnostic")
	}
	if m.state != doctorStateHealing {
		t.Fatalf("ctrl+r during a heal must not leave doctorStateHealing, got %v", m.state)
	}
	if m.loading {
		t.Fatal("ctrl+r during a heal must not flip the model into loading")
	}
	if view := ansi.Strip(m.View()); strings.Contains(view, "[ctrl+r]") {
		t.Fatalf("healing footer must not advertise ctrl+r:\n%s", view)
	}

	// The in-flight heal still completes normally afterwards.
	m, _ = m.Update(runCmd(healCmd))
	if !m.loading {
		t.Fatal("heal completion should still re-run diagnostics")
	}
	if len(calls) != 1 {
		t.Fatalf("exactly one heal must have run, got %d", len(calls))
	}
}

func TestDoctorHealStreamsStageLines(t *testing.T) {
	// The cascade delivers one msg per stage; each stage's output must appear
	// on screen while the next stage is still running.
	m := healDoctor(t, healNeedingPlan(), nil)
	m.healFn = func(p config.UpdatePlan) tea.Cmd {
		second := func() tea.Msg {
			return doctorHealStepMsg{Lines: []doctorLine{{Text: "✓ stage2", OK: true}}}
		}
		return func() tea.Msg {
			return doctorHealStepMsg{Lines: []doctorLine{{Text: "✓ stage1", OK: true}}, Next: second}
		}
	}
	m, _ = m.Update(runCmd(m.Init()).(doctorResultMsg))
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	m, cmd = m.Update(runCmd(cmd))
	if m.state != doctorStateHealing {
		t.Fatalf("mid-cascade the model must stay in doctorStateHealing, got %v", m.state)
	}
	if cmd == nil {
		t.Fatal("a stage msg with Next set must schedule the next stage")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "✓ stage1") {
		t.Fatalf("the first stage's output should stream into the view before the cascade finishes:\n%s", view)
	}
	if !strings.Contains(view, i18n.T("doctor.heal_section")) {
		t.Fatalf("streamed output should sit under the heal section header:\n%s", view)
	}

	m, cmd = m.Update(runCmd(cmd))
	if !m.loading || cmd == nil {
		t.Fatal("the final stage should re-run diagnostics")
	}
	m, _ = m.Update(runCmd(cmd).(doctorResultMsg))
	joined := ""
	for _, line := range m.lines {
		joined += line.Text + "\n"
	}
	if !strings.Contains(joined, "✓ stage1") || !strings.Contains(joined, "✓ stage2") {
		t.Fatalf("the merged report should carry every stage's output:\n%s", joined)
	}
}

// tuiHealablePlan is a plan whose only fixable finding is a Homebrew TUI
// update — the surface the running process can never observe as fixed,
// because its baked-in version only changes on restart.
func tuiHealablePlan() config.UpdatePlan {
	return config.UpdatePlan{
		TUI: config.TUIUpdateCheck{
			UpdateAvailable: true,
			Install:         config.TUIInstallInfo{Method: config.TUIInstallMethodHomebrew},
			Latest:          "v0.9.4",
		},
	}
}

func TestDoctorTUIHealSuppressesRepromptUntilRestart(t *testing.T) {
	// diagnoseFn always reports the same stale TUI update, mirroring the real
	// world: tuiVersion is baked at startup, so the post-heal diagnostic
	// re-classifies the just-installed update as available. The model must
	// suppress the re-prompt instead of offering the same brew upgrade forever.
	var calls []config.UpdatePlan
	m := healDoctor(t, tuiHealablePlan(), &calls)
	m.healFn = func(p config.UpdatePlan) tea.Cmd {
		return func() tea.Msg {
			calls = append(calls, p)
			return doctorHealStepMsg{
				Lines:     []doctorLine{{Text: "✓ brewed", OK: true}},
				HealedTUI: "v0.9.4",
			}
		}
	}
	m, _ = m.Update(runCmd(m.Init()).(doctorResultMsg))
	if m.state != doctorStateHealPrompt {
		t.Fatalf("healable TUI plan must prompt, got %v", m.state)
	}
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, cmd = m.Update(runCmd(cmd)) // heal step (records call, reports HealedTUI)
	m, _ = m.Update(runCmd(cmd).(doctorResultMsg))

	if m.state != doctorStateReport {
		t.Fatalf("post-heal diagnostic must not re-prompt for the just-healed TUI update, got state %v", m.state)
	}
	if m.healPlan.TUIHealable() {
		t.Fatal("the stored plan must not keep the healed TUI update healable")
	}
	restartHint := i18n.TF("doctor.heal_tui_restart_pending", "v0.9.4")
	joined := ""
	for _, line := range m.lines {
		joined += line.Text + "\n"
	}
	if !strings.Contains(joined, restartHint) {
		t.Fatalf("the report should explain the restart-pending suppression:\n%s", joined)
	}
	if len(calls) != 1 {
		t.Fatalf("the brew heal must have run exactly once, got %d", len(calls))
	}
}

func TestDoctorHealPromptViewportFitsTerminal(t *testing.T) {
	// The prompt footer is taller than the report footer; the viewport must
	// shrink so the whole view still fits the terminal height.
	m := healDoctor(t, healNeedingPlan(), nil)
	m, _ = m.Update(runCmd(m.Init()).(doctorResultMsg))
	view := m.View()
	if got := strings.Count(view, "\n") + 1; got > 40 {
		t.Fatalf("prompt view is %d rows tall for a 40-row terminal:\n%s", got, view)
	}
}
