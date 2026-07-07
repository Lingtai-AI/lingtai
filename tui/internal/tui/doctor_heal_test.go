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
	m.healFn = func(p config.UpdatePlan) doctorHealDoneMsg {
		if healCalls != nil {
			*healCalls = append(*healCalls, p)
		}
		return doctorHealDoneMsg{Lines: []doctorLine{{Text: "✓ healed", OK: true}}}
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
	if strings.Contains(ansi.Strip(m.View()), i18n.T("doctor.heal_prompt")) {
		t.Fatalf("healthy result must not render the heal prompt:\n%s", m.View())
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
	m.healFn = func(p config.UpdatePlan) doctorHealDoneMsg {
		calls = append(calls, p)
		return doctorHealDoneMsg{}
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
