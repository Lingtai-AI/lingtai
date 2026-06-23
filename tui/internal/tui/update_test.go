package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/config"
)

func TestUpdateModelUpToDateGoesStraightToDone(t *testing.T) {
	m := NewUpdateModel("/orch", "/global")
	// Inject a healthy, up-to-date inspection: no update needed.
	m.inspectFn = func() config.KernelStatus {
		return config.KernelStatus{Installed: "0.9.7", Latest: "0.9.7", NeedsUpdate: false}
	}
	updateCalled := false
	m.updateFn = func(force bool) config.DoctorReport {
		updateCalled = true
		return config.DoctorReport{Healthy: true}
	}

	if m.state != stateChecking {
		t.Fatalf("new model should start in stateChecking, got %v", m.state)
	}
	// Drive the checking command.
	msg := runCmd(m.Init())
	m, cmd := m.Update(msg)

	if m.state != stateDone {
		t.Fatalf("up-to-date kernel should skip confirm and reach stateDone, got %v", m.state)
	}
	if updateCalled {
		t.Fatal("up-to-date kernel must not run RunKernelUpdate")
	}
	if cmd != nil {
		t.Fatal("reaching stateDone without an update should not schedule further work")
	}
}

func TestUpdateModelEditableSkipsConfirm(t *testing.T) {
	m := NewUpdateModel("/orch", "/global")
	m.inspectFn = func() config.KernelStatus {
		return config.KernelStatus{Installed: "0.9.6", Editable: true, NeedsUpdate: false}
	}
	updateCalled := false
	m.updateFn = func(force bool) config.DoctorReport {
		updateCalled = true
		return config.DoctorReport{Healthy: true}
	}

	msg := runCmd(m.Init())
	m, _ = m.Update(msg)

	if m.state != stateDone {
		t.Fatalf("editable dev install should skip confirm and reach stateDone, got %v", m.state)
	}
	if updateCalled {
		t.Fatal("editable dev install must not run RunKernelUpdate")
	}
}

func TestUpdateModelOutOfDateShowsConfirmThenCancel(t *testing.T) {
	m := NewUpdateModel("/orch", "/global")
	m.inspectFn = func() config.KernelStatus {
		return config.KernelStatus{Installed: "0.9.6", Latest: "0.9.7", NeedsUpdate: true}
	}
	updateCalled := false
	m.updateFn = func(force bool) config.DoctorReport {
		updateCalled = true
		return config.DoctorReport{Healthy: true}
	}

	msg := runCmd(m.Init())
	m, _ = m.Update(msg)

	if m.state != stateConfirm {
		t.Fatalf("out-of-date kernel must enter stateConfirm, got %v", m.state)
	}

	// Move selection to "Cancel" and press enter — this returns to mail view
	// and must NOT run the update.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.confirmIdx != 1 {
		t.Fatalf("expected confirmIdx=1 (Cancel) after down, got %d", m.confirmIdx)
	}
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if updateCalled {
		t.Fatal("cancel must not run RunKernelUpdate")
	}
	// Cancel issues a ViewChangeMsg{View:"mail"}.
	if cmd == nil {
		t.Fatal("cancel should issue a view-change command back to mail")
	}
	if vc, ok := cmd().(ViewChangeMsg); !ok || vc.View != "mail" {
		t.Fatalf("cancel should return to mail view, got %#v", cmd())
	}
}

func TestUpdateModelOutOfDateConfirmRunsUpdate(t *testing.T) {
	m := NewUpdateModel("/orch", "/global")
	m.inspectFn = func() config.KernelStatus {
		return config.KernelStatus{Installed: "0.9.6", Latest: "0.9.7", NeedsUpdate: true}
	}
	var forced bool
	updateCalled := false
	m.updateFn = func(force bool) config.DoctorReport {
		updateCalled = true
		forced = force
		return config.DoctorReport{
			Healthy: true,
			Lines:   []config.DoctorLine{{Severity: config.DoctorOK, Text: "upgraded"}},
		}
	}

	msg := runCmd(m.Init())
	m, _ = m.Update(msg)
	if m.state != stateConfirm {
		t.Fatalf("expected stateConfirm, got %v", m.state)
	}

	// confirmIdx defaults to 0 ("Update now"); press enter.
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.state != stateUpdating {
		t.Fatalf("confirming should transition to stateUpdating, got %v", m.state)
	}
	if updateCalled {
		t.Fatal("the install must run asynchronously via the returned Cmd, not inline in Update")
	}

	// Run the async updating command; it should call RunKernelUpdate and yield
	// the result message that drives the model to stateDone.
	resultMsg := runCmd(cmd)
	m, _ = m.Update(resultMsg)

	if !updateCalled {
		t.Fatal("confirm must run RunKernelUpdate")
	}
	if !forced {
		t.Fatal("RunKernelUpdate must be called with force=true")
	}
	if m.state != stateDone {
		t.Fatalf("after update completes the model should reach stateDone, got %v", m.state)
	}
}

func TestUpdateModelEscReturnsToMail(t *testing.T) {
	m := NewUpdateModel("/orch", "/global")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should issue a view-change command")
	}
	if vc, ok := cmd().(ViewChangeMsg); !ok || vc.View != "mail" {
		t.Fatalf("esc should return to mail view, got %#v", cmd())
	}
}
