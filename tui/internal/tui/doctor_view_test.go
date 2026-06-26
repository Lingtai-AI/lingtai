package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// makeManyLines builds a DoctorModel whose diagnostic output is taller than the
// viewport so the scroll path is exercised.
func doctorWithLines(n int) DoctorModel {
	m := NewDoctorModel("/tmp/orch", "/tmp/global")
	m.loading = false
	lines := make([]doctorLine, 0, n)
	for i := 0; i < n; i++ {
		lines = append(lines, doctorLine{Text: "line", OK: true})
	}
	m.lines = lines
	return m
}

// sizeDoctor applies a WindowSizeMsg and the finished diagnostic so the
// viewport is initialized and populated.
func sizeDoctor(m DoctorModel, w, h int) DoctorModel {
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	// Re-publish the lines as a finished result so the viewport content syncs
	// through the same path the real diagnostic uses.
	m, _ = m.Update(doctorResultMsg{Lines: m.lines})
	return m
}

func TestDoctorViewInitializesViewport(t *testing.T) {
	m := doctorWithLines(100)
	m = sizeDoctor(m, 80, 24)
	if !m.ready {
		t.Fatal("DoctorModel should mark its viewport ready after a WindowSizeMsg")
	}
}

func TestDoctorMouseWheelScrolls(t *testing.T) {
	m := doctorWithLines(200)
	m = sizeDoctor(m, 80, 24)
	if m.viewport.YOffset() != 0 {
		t.Fatalf("expected viewport to start at top, YOffset=%d", m.viewport.YOffset())
	}
	for i := 0; i < 5; i++ {
		m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	if m.viewport.YOffset() == 0 {
		t.Fatal("mouse wheel down should scroll the doctor viewport off the top")
	}
}

func TestDoctorArrowKeysScroll(t *testing.T) {
	m := doctorWithLines(200)
	m = sizeDoctor(m, 80, 24)
	for i := 0; i < 5; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.viewport.YOffset() == 0 {
		t.Fatal("down arrow should scroll the doctor viewport off the top")
	}
}

func TestDoctorEscStillExits(t *testing.T) {
	m := doctorWithLines(10)
	m = sizeDoctor(m, 80, 24)
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc should still return a ViewChangeMsg command")
	}
}

func TestDoctorViewRendersSectionHeaders(t *testing.T) {
	// A section line should render its label without the indent that normal
	// status lines get, so the hierarchy is visible.
	m := NewDoctorModel("/tmp/orch", "/tmp/global")
	m.loading = false
	m.lines = []doctorLine{
		{Text: "RUNTIME", Section: true},
		{Text: "✓ something", OK: true},
	}
	m = sizeDoctor(m, 80, 24)
	out := m.View()
	if !strings.Contains(out, "RUNTIME") {
		t.Fatal("section header text should appear in the rendered view")
	}
}

func TestRenderSectionHeaderAnchorsLabel(t *testing.T) {
	// Section headers carry the accent marker so group boundaries are scannable;
	// the label itself must survive verbatim. Strip ANSI so the assertion holds
	// regardless of the active theme's color codes.
	out := ansi.Strip(renderSectionHeader("LLM connectivity"))
	if !strings.HasPrefix(out, doctorSectionMarker) {
		t.Fatalf("section header should begin with the marker %q, got %q", doctorSectionMarker, out)
	}
	if !strings.Contains(out, "LLM connectivity") {
		t.Fatalf("section header should preserve its label, got %q", out)
	}
}

func TestRenderBodyMarksOnlySectionLines(t *testing.T) {
	// Only Section lines get the marker; status/hint lines stay indented and
	// unmarked so the marker reliably signals a group boundary.
	m := NewDoctorModel("/tmp/orch", "/tmp/global")
	m.lines = []doctorLine{
		{Text: "Runtime & assets", Section: true},
		{Text: "✓ ready", OK: true},
		{Text: "→ do the thing", Hint: true},
	}
	lines := strings.Split(ansi.Strip(m.renderBody()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 rendered lines, got %d: %#v", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], doctorSectionMarker) {
		t.Fatalf("section line should be marked, got %q", lines[0])
	}
	if strings.Contains(lines[1], doctorSectionMarker) || strings.Contains(lines[2], doctorSectionMarker) {
		t.Fatalf("non-section lines must not carry the section marker: %#v", lines[1:])
	}
	if !strings.HasPrefix(lines[1], "  ") || !strings.HasPrefix(lines[2], "  ") {
		t.Fatalf("status/hint lines should stay indented: %#v", lines[1:])
	}
}
