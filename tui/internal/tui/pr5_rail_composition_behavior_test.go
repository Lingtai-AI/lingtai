package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPR5Stage3RootComposesVisibleRailAndChatFromOneBudget(t *testing.T) {
	a := mailLayoutApp(t)
	model, _ := a.Update(tea.WindowSizeMsg{Width: 84, Height: 24})
	a = model.(App)

	budget := a.layoutBudget()
	if !budget.RailVisible || budget.RailWidth != 24 || budget.ContentWidth != 60 {
		t.Fatalf("precondition budget = %+v, want visible 24-column rail plus 60-column chat", budget)
	}

	root := ansi.Strip(a.View().Content)
	chat := ansi.Strip(lipgloss.NewStyle().
		Width(budget.ContentWidth).
		Height(budget.ChildHeight).
		Render(a.mail.View()))
	rootLines := strings.Split(root, "\n")
	chatLines := strings.Split(chat, "\n")
	if len(rootLines) != len(chatLines) {
		t.Fatalf("root lines = %d, chat lines = %d; horizontal composition must preserve child height", len(rootLines), len(chatLines))
	}

	railLines := make([]string, 0, len(rootLines))
	for i := range chatLines {
		if got := lipgloss.Width(rootLines[i]); got != budget.TerminalWidth {
			t.Fatalf("root line %d width = %d, want terminal width %d", i, got, budget.TerminalWidth)
		}
		if !strings.HasSuffix(rootLines[i], chatLines[i]) {
			t.Fatalf("line %d does not retain the Mail child at ContentX=%d:\nroot=%q\nchat=%q", i, budget.ContentX, rootLines[i], chatLines[i])
		}
		if got := lipgloss.Width(rootLines[i]) - lipgloss.Width(chatLines[i]); got != budget.RailWidth {
			t.Fatalf("line %d rail prefix width = %d, want budget RailWidth %d", i, got, budget.RailWidth)
		}
		railLines = append(railLines, ansi.Truncate(rootLines[i], budget.RailWidth, ""))
	}

	rail := strings.Join(railLines, "\n")
	if !strings.Contains(rail, "Agents") {
		t.Fatalf("visible rail does not render the existing localized Agents heading:\n%s", rail)
	}
	if !strings.Contains(rail, "main") {
		t.Fatalf("visible rail does not include the original Main target:\n%s", rail)
	}
}

func TestPR5Stage3HiddenAndVisitedFramesRenderOnlyFullWidthChat(t *testing.T) {
	for _, tc := range []struct {
		name     string
		width    int
		visiting bool
	}{
		{name: "below minimum boundary", width: 83},
		{name: "cross-project visit", width: 84, visiting: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := mailLayoutApp(t)
			a.visiting = tc.visiting
			model, _ := a.Update(tea.WindowSizeMsg{Width: tc.width, Height: 24})
			a = model.(App)

			budget := a.layoutBudget()
			if budget.RailVisible || budget.RailWidth != 0 || budget.ContentWidth != tc.width {
				t.Fatalf("hidden budget = %+v, want zero rail and full-width chat %d", budget, tc.width)
			}

			got := ansi.Strip(a.View().Content)
			want := ansi.Strip(a.mail.View())
			if got != want {
				t.Fatalf("hidden/visited root must render the Mail child unchanged:\n--- root ---\n%s\n--- chat ---\n%s", got, want)
			}
		})
	}
}
