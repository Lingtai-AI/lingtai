package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPR5Stage2StaleA1EditorRequestCannotRetargetColdA2(t *testing.T) {
	app, targetA := pr5OrdinarySendApp(t, "agent-a", "Agent A", 4101, 1)
	app.mail.input.SetValue("A1 editor draft")

	a1, cmd := installationDeliverApp(t, app, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("A1 Ctrl+E returned no deferred editor request command")
	}
	produced := runCmd(cmd)
	request, ok := produced.(OpenEditorMsg)
	if !ok {
		t.Fatalf("A1 Ctrl+E produced %T, want OpenEditorMsg", produced)
	}
	staleDone := installationFakeEditorDone(t, a1.mail, "A1 completed editor draft")

	targetB := filepath.Join(a1.projectDir, "agent-b")
	installationWriteAgent(t, targetB, "agent-b", "Agent B", "Agent B")
	pr5BindCoordinatorRailTarget(t, &a1, targetB, "Agent B", 4201, 2)
	pr5BindCoordinatorRailTarget(t, &a1, targetA, "Agent A", 4101, 3)
	a1.mail, _ = a1.mail.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a1.mail.pendingMessage = "fresh A2 pending"
	a1.mail.input.SetValue("fresh A2 input")
	a1.mail.statusFlash = "fresh A2 status"
	beforeStatusExpiry := a1.mail.statusExpiry
	beforeLines := a1.mail.input.LineCount()
	beforeViewport := a1.mail.viewport.Height()
	if a1.mail.showEditorWarn || a1.mail.editorWarnText != "" {
		t.Fatalf("cold A2 started with editor warning state: visible=%v text=%q", a1.mail.showEditorWarn, a1.mail.editorWarnText)
	}

	got, effect := installationDeliverApp(t, a1, request)
	if got.mail.showEditorWarn {
		t.Fatalf("stale A1 editor request opened cold A2 warning with %q", got.mail.editorWarnText)
	}
	if effect != nil {
		t.Fatalf("stale A1 editor request returned effect %T", runCmd(effect))
	}
	if got.mail.editorWarnText != "" {
		t.Fatalf("stale A1 editor request retained warning text %q on cold A2", got.mail.editorWarnText)
	}
	if got.mail.pendingMessage != "fresh A2 pending" || got.mail.input.Value() != "fresh A2 input" ||
		got.mail.statusFlash != "fresh A2 status" || got.mail.statusExpiry != beforeStatusExpiry ||
		got.mail.input.LineCount() != beforeLines || got.mail.viewport.Height() != beforeViewport {
		t.Fatalf(
			"stale A1 editor request changed cold A2 state: pending=%q input=%q status=%q lines=%d viewport=%d",
			got.mail.pendingMessage,
			got.mail.input.Value(),
			got.mail.statusFlash,
			got.mail.input.LineCount(),
			got.mail.viewport.Height(),
		)
	}

	afterDone, effect := installationDeliverApp(t, got, staleDone)
	if effect != nil {
		t.Fatalf("stale A1 editor completion returned refresh/clear effect %T", runCmd(effect))
	}
	if afterDone.mail.showEditorWarn || afterDone.mail.editorWarnText != "" ||
		afterDone.mail.pendingMessage != "fresh A2 pending" || afterDone.mail.input.Value() != "fresh A2 input" ||
		afterDone.mail.statusFlash != "fresh A2 status" || afterDone.mail.statusExpiry != beforeStatusExpiry ||
		afterDone.mail.input.LineCount() != beforeLines || afterDone.mail.viewport.Height() != beforeViewport {
		t.Fatalf(
			"stale A1 editor completion changed cold A2 state: warning=%v warningText=%q pending=%q input=%q status=%q lines=%d viewport=%d",
			afterDone.mail.showEditorWarn,
			afterDone.mail.editorWarnText,
			afterDone.mail.pendingMessage,
			afterDone.mail.input.Value(),
			afterDone.mail.statusFlash,
			afterDone.mail.input.LineCount(),
			afterDone.mail.viewport.Height(),
		)
	}
}

func TestPR5Stage2AcceptedEditorRequestRevalidatesBeforeLaunch(t *testing.T) {
	app, _ := pr5OrdinarySendApp(t, "agent-a", "Agent A", 4101, 1)
	app.mail.input.SetValue("A1 editor draft")

	a1, cmd := installationDeliverApp(t, app, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("A1 Ctrl+E returned no deferred editor request command")
	}
	produced := runCmd(cmd)
	request, ok := produced.(OpenEditorMsg)
	if !ok {
		t.Fatalf("A1 Ctrl+E produced %T, want OpenEditorMsg", produced)
	}
	warned, effect := installationDeliverApp(t, a1, request)
	if effect != nil {
		t.Fatalf("accepted A1 editor request returned unexpected effect %T", runCmd(effect))
	}
	if !warned.mail.showEditorWarn || warned.mail.editorWarnText != "A1 editor draft" {
		t.Fatalf("accepted A1 editor request did not retain its warning: visible=%v text=%q", warned.mail.showEditorWarn, warned.mail.editorWarnText)
	}

	// Model inventory disappearance after receipt while the warning is open. Enter
	// must revalidate the accepted request before even creating a temp file; the
	// returned tea.Cmd is deliberately never executed, so no editor process starts.
	warned.mail.revalidateTarget = func(asyncOwner, asyncTarget) bool { return false }
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	got, launch := installationDeliverApp(t, warned, tea.KeyPressMsg{Code: tea.KeyEnter})
	if launch != nil {
		t.Fatalf("accepted editor request launched after its target disappeared: command=%T", launch)
	}
	if got.mail.showEditorWarn || got.mail.editorWarnText != "" {
		t.Fatalf("rejected editor launch retained warning state: visible=%v text=%q", got.mail.showEditorWarn, got.mail.editorWarnText)
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected editor launch created %d temp files before revalidation", len(entries))
	}
}
