package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/doctorreport"
)

func TestDoctorModelShowsSaveShortcutOnlyAfterResultWithDraft(t *testing.T) {
	m := NewDoctorModel(t.TempDir(), t.TempDir())
	m.width = 100

	if strings.Contains(m.View(), "[r] save report") {
		t.Fatalf("loading doctor view should not show save shortcut:\n%s", m.View())
	}

	draft := &doctorreport.Draft{
		GeneratedAt: time.Date(2026, 6, 12, 18, 30, 0, 0, time.UTC),
		Lines:       []doctorreport.Line{{Severity: doctorreport.SeverityOK, Text: "done"}},
	}
	m, _ = m.Update(doctorResultMsg{
		Lines: []doctorLine{{Text: "done", OK: true}},
		Draft: draft,
	})

	if !strings.Contains(m.View(), "[r] save report") {
		t.Fatalf("completed doctor view should show save shortcut:\n%s", m.View())
	}
}

func TestDoctorModelSaveShortcutWritesStoredDraftOnce(t *testing.T) {
	orchDir := filepath.Join(t.TempDir(), "agent-one")
	globalDir := t.TempDir()
	m := NewDoctorModel(orchDir, globalDir)

	oldWriter := writeDoctorReport
	oldNow := doctorReportNow
	t.Cleanup(func() {
		writeDoctorReport = oldWriter
		doctorReportNow = oldNow
	})

	doctorReportNow = func() time.Time {
		return time.Date(2026, 6, 12, 18, 30, 0, 0, time.UTC)
	}
	var calls int
	var gotDir string
	var gotDraft doctorreport.Draft
	writeDoctorReport = func(dir string, draft doctorreport.Draft) error {
		calls++
		gotDir = dir
		gotDraft = draft
		return nil
	}

	draft := &doctorreport.Draft{
		GeneratedAt: time.Date(2026, 6, 12, 18, 30, 0, 0, time.UTC),
		AgentName:   "agent-one",
		Lines: []doctorreport.Line{
			{Severity: doctorreport.SeverityFail, Text: "stored draft finding"},
		},
	}
	m, _ = m.Update(doctorResultMsg{Lines: []doctorLine{{Text: "rendered line"}}, Draft: draft})

	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("pressing r with a completed draft should return a save command")
	}
	if !m.saving {
		t.Fatal("doctor model should enter saving state while save command runs")
	}

	msg := runDoctorCmd(t, cmd)
	m, _ = m.Update(msg)
	if calls != 1 {
		t.Fatalf("writeDoctorReport calls = %d, want 1", calls)
	}
	if gotDraft.Lines[0].Text != "stored draft finding" {
		t.Fatalf("save did not use stored draft: %#v", gotDraft)
	}
	if !strings.HasPrefix(gotDir, filepath.Join(globalDir, "reports")) {
		t.Fatalf("report dir %q is not under global reports dir %q", gotDir, globalDir)
	}
	if !filepath.IsAbs(m.savedPath) || m.savedPath != gotDir {
		t.Fatalf("savedPath = %q, want absolute writer dir %q", m.savedPath, gotDir)
	}
	if m.saveErr != nil {
		t.Fatalf("saveErr = %v, want nil", m.saveErr)
	}

	m, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd != nil {
		t.Fatal("pressing r after successful save should not return another save command")
	}
	if calls != 1 {
		t.Fatalf("writeDoctorReport calls after repeated r = %d, want 1", calls)
	}
}

func TestDoctorModelSaveShortcutRecordsError(t *testing.T) {
	m := NewDoctorModel(filepath.Join(t.TempDir(), "agent-one"), t.TempDir())

	oldWriter := writeDoctorReport
	t.Cleanup(func() { writeDoctorReport = oldWriter })

	wantErr := errors.New("disk full")
	writeDoctorReport = func(string, doctorreport.Draft) error {
		return wantErr
	}
	draft := &doctorreport.Draft{
		GeneratedAt: time.Date(2026, 6, 12, 18, 30, 0, 0, time.UTC),
		Lines:       []doctorreport.Line{{Severity: doctorreport.SeverityFail, Text: "failed"}},
	}
	m, _ = m.Update(doctorResultMsg{Lines: []doctorLine{{Text: "failed"}}, Draft: draft})

	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("pressing r should return save command")
	}
	m, _ = m.Update(runDoctorCmd(t, cmd))

	if m.saveErr == nil || !strings.Contains(m.saveErr.Error(), "disk full") {
		t.Fatalf("saveErr = %v, want disk full", m.saveErr)
	}
	if m.savedPath != "" {
		t.Fatalf("savedPath = %q after failed save, want empty", m.savedPath)
	}
}

func TestDoctorModelEscStillReturnsToMail(t *testing.T) {
	m := NewDoctorModel(t.TempDir(), t.TempDir())
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should return a command")
	}
	msg := runDoctorCmd(t, cmd)
	viewMsg, ok := msg.(ViewChangeMsg)
	if !ok {
		t.Fatalf("esc command msg = %T, want ViewChangeMsg", msg)
	}
	if viewMsg.View != "mail" {
		t.Fatalf("esc ViewChangeMsg.View = %q, want mail", viewMsg.View)
	}
}

func TestAppForwardsDoctorSaveCompletion(t *testing.T) {
	a := App{
		currentView: appViewDoctor,
		doctor: DoctorModel{
			saving: true,
		},
	}

	updated, _ := a.Update(doctorReportSavedMsg{path: "/tmp/report", err: nil})
	got := updated.(App)
	if got.doctor.savedPath != "/tmp/report" {
		t.Fatalf("doctor.savedPath = %q, want forwarded save path", got.doctor.savedPath)
	}
	if got.doctor.saving {
		t.Fatal("doctor saving state should be cleared after forwarded save completion")
	}
}

func runDoctorCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	msg := cmd()
	if msg == nil {
		t.Fatal("command returned nil message")
	}
	return msg
}
