package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/doctorreport"
)

// finishedDoctor returns a sized DoctorModel whose diagnostic has completed and
// produced a draft, so the save affordance is live.
func finishedDoctor(t *testing.T, orchDir, globalDir string) DoctorModel {
	t.Helper()
	m := NewDoctorModel(orchDir, globalDir)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	draft := &doctorreport.Draft{
		GeneratedAt: time.Date(2026, 6, 26, 18, 30, 0, 0, time.UTC),
		AgentName:   filepath.Base(orchDir),
		Lines:       []doctorreport.Line{{Severity: doctorreport.SeverityOK, Text: "done"}},
	}
	m, _ = m.Update(doctorResultMsg{
		Lines: []doctorLine{{Text: "✓ done", OK: true}},
		Draft: draft,
	})
	return m
}

func TestDoctorSaveAffordanceOnlyAfterResultWithDraft(t *testing.T) {
	m := NewDoctorModel(t.TempDir(), t.TempDir())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if strings.Contains(m.View(), "[r] "+"save report") {
		t.Fatalf("loading doctor view must not advertise save:\n%s", m.View())
	}

	m = finishedDoctor(t, t.TempDir(), t.TempDir())
	if !strings.Contains(m.View(), "save report") {
		t.Fatalf("completed doctor view should advertise save:\n%s", m.View())
	}
}

func TestDoctorPrivacyNoticeShownOnlyWithDraft(t *testing.T) {
	const notice = "Review the bundle before sharing"

	// Loading view (no draft yet) must not show the redaction/share reminder.
	m := NewDoctorModel(t.TempDir(), t.TempDir())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if strings.Contains(m.View(), notice) {
		t.Fatalf("loading doctor view must not show the privacy notice:\n%s", m.View())
	}

	// Once a run has completed and produced a draft, the reminder is prominent
	// near the save affordance.
	m = finishedDoctor(t, t.TempDir(), t.TempDir())
	if !strings.Contains(m.View(), notice) {
		t.Fatalf("completed doctor view should show the privacy notice:\n%s", m.View())
	}
}

func TestDoctorExportHintVisibleWhileLoadingAndAfterCompletion(t *testing.T) {
	const exportHint = "Export:"

	// While checks are still running, the export capability must already be
	// advertised so the user knows the [r] save exists before completion.
	m := NewDoctorModel(t.TempDir(), t.TempDir())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	if !m.loading {
		t.Fatal("model should be loading before a result arrives")
	}
	if !strings.Contains(m.View(), exportHint) {
		t.Fatalf("loading doctor view should advertise export:\n%s", m.View())
	}

	// The hint stays visible after the run completes.
	m = finishedDoctor(t, t.TempDir(), t.TempDir())
	if !strings.Contains(m.View(), exportHint) {
		t.Fatalf("completed doctor view should still advertise export:\n%s", m.View())
	}
}

func TestDoctorBareRSavesReportWithoutRerun(t *testing.T) {
	orchDir := filepath.Join(t.TempDir(), "agent-one")
	if err := os.MkdirAll(orchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	globalDir := t.TempDir()
	m := finishedDoctor(t, orchDir, globalDir)

	updated, cmd := m.Update(bareR())
	if cmd == nil {
		t.Fatal("bare r on a completed doctor run should return a save command")
	}
	if !updated.saving {
		t.Fatal("bare r should mark the model saving while the write runs")
	}

	// The save command must NOT re-run the diagnostic — it returns a saved msg.
	msg := cmd()
	saved, ok := msg.(doctorReportSavedMsg)
	if !ok {
		t.Fatalf("save command returned %T, want doctorReportSavedMsg (no rerun)", msg)
	}
	if saved.Err != nil {
		t.Fatalf("save failed: %v", saved.Err)
	}

	// A unique directory under <globalDir>/reports must now hold the bundle.
	if !strings.HasPrefix(saved.Path, filepath.Join(globalDir, "reports")) {
		t.Fatalf("report path %q not under global reports dir", saved.Path)
	}
	for _, name := range []string{"report.md", "metadata.json", "redaction.json"} {
		if _, err := os.Stat(filepath.Join(saved.Path, name)); err != nil {
			t.Fatalf("expected %s in bundle: %v", name, err)
		}
	}
	// No events/log artifact may be written.
	entries, _ := os.ReadDir(saved.Path)
	for _, e := range entries {
		if strings.Contains(e.Name(), "log") || strings.Contains(e.Name(), "events") {
			t.Fatalf("unexpected log artifact %q in bundle", e.Name())
		}
	}

	final, _ := updated.Update(saved)
	if final.savedPath != saved.Path {
		t.Fatalf("savedPath = %q, want %q", final.savedPath, saved.Path)
	}
	if final.canSaveReport() {
		t.Fatal("after a successful save the affordance must retract")
	}
	if !strings.Contains(final.View(), saved.Path) {
		t.Fatalf("view should show saved path:\n%s", final.View())
	}
}

func TestDoctorTwoSavesProduceDistinctDirs(t *testing.T) {
	orchDir := filepath.Join(t.TempDir(), "agent-one")
	if err := os.MkdirAll(orchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	globalDir := t.TempDir()
	draft := doctorreport.Draft{
		GeneratedAt: time.Date(2026, 6, 26, 18, 30, 0, 0, time.UTC),
		AgentName:   "agent-one",
		Lines:       []doctorreport.Line{{Severity: doctorreport.SeverityOK, Text: "done"}},
	}
	// Two saves in the same wall-clock second (same GeneratedAt) must not collide.
	d1, err := createDoctorReportDir(globalDir, orchDir, draft)
	if err != nil {
		t.Fatalf("first dir: %v", err)
	}
	d2, err := createDoctorReportDir(globalDir, orchDir, draft)
	if err != nil {
		t.Fatalf("second dir: %v", err)
	}
	if d1 == d2 {
		t.Fatalf("two report dirs collided: %q", d1)
	}
}

func TestDoctorSaveFailureSurfacedAndRetryable(t *testing.T) {
	m := finishedDoctor(t, t.TempDir(), t.TempDir())

	orig := writeDoctorReport
	t.Cleanup(func() { writeDoctorReport = orig })
	writeDoctorReport = func(dir string, _ doctorreport.Draft) error {
		return errors.New("disk full")
	}

	updated, cmd := m.Update(bareR())
	if cmd == nil {
		t.Fatal("expected a save command")
	}
	updated, _ = updated.Update(cmd().(doctorReportSavedMsg))
	if updated.saveErr == nil {
		t.Fatal("save failure should be recorded on the model")
	}
	if !strings.Contains(updated.View(), "save failed") {
		t.Fatalf("view should surface the failure:\n%s", updated.View())
	}
	// A failed save must remain retryable (affordance still live).
	if !updated.canSaveReport() {
		t.Fatal("after a failed save the user should still be able to retry")
	}
}

func TestDoctorBareRNoopWhileLoading(t *testing.T) {
	m := NewDoctorModel(t.TempDir(), t.TempDir())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	// loading is still true; bare r must be a harmless no-op.
	updated, cmd := m.Update(bareR())
	if cmd != nil {
		t.Fatal("bare r while loading should be a no-op (nil cmd)")
	}
	if updated.saving {
		t.Fatal("bare r while loading must not enter saving state")
	}
}
