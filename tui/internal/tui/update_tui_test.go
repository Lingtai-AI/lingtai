package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/config"
)

func TestUpdateTUIModelUnsupportedSkipsConfirm(t *testing.T) {
	m := NewUpdateTUIModel("/global")
	// Inject an unsupported (unknown/other) install method.
	m.inspectFn = func() config.TUIInstallInfo {
		return config.TUIInstallInfo{Method: config.TUIInstallMethodUnknown}
	}
	updateCalled := false
	m.updateFn = func() config.TUIUpdateResult {
		updateCalled = true
		return config.TUIUpdateResult{Healthy: true}
	}

	if m.state != stateTUIChecking {
		t.Fatalf("new model should start in stateTUIChecking, got %v", m.state)
	}
	// Drive the checking command.
	msg := runCmd(m.Init())
	m, cmd := m.Update(msg)

	if m.state != stateTUIDone {
		t.Fatalf("unsupported install should skip confirm and reach stateTUIDone, got %v", m.state)
	}
	if !m.unsupported {
		t.Fatal("unsupported install should set m.unsupported = true")
	}
	if updateCalled {
		t.Fatal("unsupported install must not run RunManualTUIUpdate")
	}
	if cmd != nil {
		t.Fatal("reaching stateTUIDone without an update should not schedule further work")
	}
}

func TestUpdateTUIModelShowsConfirmThenCancel(t *testing.T) {
	m := NewUpdateTUIModel("/global")
	m.inspectFn = func() config.TUIInstallInfo {
		return config.TUIInstallInfo{Method: config.TUIInstallMethodHomebrew}
	}
	updateCalled := false
	m.updateFn = func() config.TUIUpdateResult {
		updateCalled = true
		return config.TUIUpdateResult{Healthy: true}
	}

	msg := runCmd(m.Init())
	m, _ = m.Update(msg)

	if m.state != stateTUIConfirm {
		t.Fatalf("supported install must enter stateTUIConfirm, got %v", m.state)
	}

	// Move selection to "Cancel" and press enter — this returns to the mail view
	// and must NOT run the update.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.confirmIdx != 1 {
		t.Fatalf("expected confirmIdx=1 (Cancel) after down, got %d", m.confirmIdx)
	}
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if updateCalled {
		t.Fatal("cancel must not run RunManualTUIUpdate")
	}
	// Cancel issues a ViewChangeMsg{View:"mail"}.
	if cmd == nil {
		t.Fatal("cancel should issue a view-change command back to mail")
	}
	if vc, ok := cmd().(ViewChangeMsg); !ok || vc.View != "mail" {
		t.Fatalf("cancel should return to mail view, got %#v", cmd())
	}
}

func TestUpdateTUIModelConfirmRunsUpdate(t *testing.T) {
	m := NewUpdateTUIModel("/global")
	m.inspectFn = func() config.TUIInstallInfo {
		return config.TUIInstallInfo{Method: config.TUIInstallMethodSource}
	}
	updateCalled := false
	m.updateFn = func() config.TUIUpdateResult {
		updateCalled = true
		return config.TUIUpdateResult{
			Healthy: true,
			Lines:   []config.DoctorLine{{Severity: config.DoctorOK, Text: "upgraded"}},
		}
	}

	msg := runCmd(m.Init())
	m, _ = m.Update(msg)
	if m.state != stateTUIConfirm {
		t.Fatalf("expected stateTUIConfirm, got %v", m.state)
	}

	// confirmIdx defaults to 0 ("Update now"); press enter.
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.state != stateTUIUpdating {
		t.Fatalf("confirming should transition to stateTUIUpdating, got %v", m.state)
	}
	if updateCalled {
		t.Fatal("the install must run asynchronously via the returned Cmd, not inline in Update")
	}

	// Run the async updating command; it should call RunManualTUIUpdate and
	// yield the result message that drives the model to stateTUIDone.
	resultMsg := runCmd(cmd)
	m, _ = m.Update(resultMsg)

	if !updateCalled {
		t.Fatal("confirm must run RunManualTUIUpdate")
	}
	if m.state != stateTUIDone {
		t.Fatalf("after update completes the model should reach stateTUIDone, got %v", m.state)
	}
	if m.failed {
		t.Fatal("healthy result must not mark the model as failed")
	}
}

func TestUpdateTUIModelEscReturnsToMail(t *testing.T) {
	m := NewUpdateTUIModel("/global")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should issue a view-change command")
	}
	if vc, ok := cmd().(ViewChangeMsg); !ok || vc.View != "mail" {
		t.Fatalf("esc should return to mail view, got %#v", cmd())
	}
}

func TestUpdateTUIModelRetryOnFailure(t *testing.T) {
	m := NewUpdateTUIModel("/global")
	m.inspectFn = func() config.TUIInstallInfo {
		return config.TUIInstallInfo{Method: config.TUIInstallMethodHomebrew}
	}
	calls := 0
	m.updateFn = func() config.TUIUpdateResult {
		calls++
		// First attempt fails; the retry succeeds.
		if calls == 1 {
			return config.TUIUpdateResult{
				Healthy: false,
				Err:     fmt.Errorf("boom"),
				Lines:   []config.DoctorLine{{Severity: config.DoctorFail, Text: "boom"}},
			}
		}
		return config.TUIUpdateResult{Healthy: true}
	}

	// check -> confirm
	m, _ = m.Update(runCmd(m.Init()))
	// confirm -> stateTUIUpdating (returns the async update cmd)
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// run update -> stateTUIDone (failed)
	m, _ = m.Update(runCmd(cmd))

	if !m.failed {
		t.Fatal("first attempt should mark the model failed")
	}
	if calls != 1 {
		t.Fatalf("expected 1 update call, got %d", calls)
	}

	// Press 'r' to retry — clears prior result and returns to the confirm prompt.
	m, _ = m.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})
	if m.state != stateTUIConfirm {
		t.Fatalf("retry should return to stateTUIConfirm, got %v", m.state)
	}
	if m.failed {
		t.Fatal("retry should clear the failed flag")
	}
	if len(m.resultLines) != 0 {
		t.Fatal("retry should clear prior result lines")
	}

	// Confirm again -> stateTUIUpdating -> stateTUIDone (success).
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = m.Update(runCmd(cmd))

	if m.state != stateTUIDone {
		t.Fatalf("after retry the model should reach stateTUIDone, got %v", m.state)
	}
	if m.failed {
		t.Fatal("retry should succeed and leave the model healthy")
	}
	if calls != 2 {
		t.Fatalf("expected 2 update calls after retry, got %d", calls)
	}
}

func TestUpdateTUIModelUnsupportedViewDoesNotShowSuccess(t *testing.T) {
	m := NewUpdateTUIModel("/global")
	m.inspectFn = func() config.TUIInstallInfo {
		return config.TUIInstallInfo{Method: config.TUIInstallMethodUnknown}
	}
	m.updateFn = func() config.TUIUpdateResult {
		t.Fatal("unsupported install must not run update")
		return config.TUIUpdateResult{}
	}
	m.width = 80
	m.height = 24

	// Drive to stateTUIDone via unsupported path.
	msg := runCmd(m.Init())
	m, _ = m.Update(msg)

	if !m.unsupported {
		t.Fatal("expected m.unsupported = true")
	}

	output := m.View()

	// The view must NOT claim the TUI was updated successfully.
	if strings.Contains(output, "updated successfully") {
		t.Fatalf("unsupported path View() must not show success message, got:\n%s", output)
	}

	// The view SHOULD contain an unsupported / cannot self-update message.
	if !strings.Contains(output, "Cannot self-update") {
		t.Fatalf("unsupported path View() should explain the install method is unsupported, got:\n%s", output)
	}
}
