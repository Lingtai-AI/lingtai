package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
)

func appCursorType(app App, text string) App {
	for _, r := range text {
		app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return app
}

func appCursorPlainLines(view tea.View) []string {
	return strings.Split(ansi.Strip(view.Content), "\n")
}

func appCursorRenderedEnd(t *testing.T, view tea.View, needle string) tea.Position {
	t.Helper()
	plainLines := appCursorPlainLines(view)
	for y, line := range plainLines {
		if start := strings.Index(line, needle); start >= 0 {
			return tea.Position{X: lipgloss.Width(line[:start+len(needle)]), Y: y}
		}
	}
	t.Fatalf("final App view has no rendered cursor suffix %q", needle)
	return tea.Position{}
}

func appCursorRequireRenderedEnd(t *testing.T, view tea.View, needle string) tea.Position {
	t.Helper()
	want := appCursorRenderedEnd(t, view, needle)
	if view.Cursor == nil {
		t.Fatal("final App Mail view cursor = nil, want the real focused composer cursor")
	}
	if got := view.Cursor.Position; got != want {
		t.Fatalf("final App Mail view cursor = %#v, want rendered composer cell %#v", got, want)
	}
	return want
}

func appCursorRequireNil(t *testing.T, name string, app App) {
	t.Helper()
	view := app.View()
	if view.Cursor != nil {
		t.Fatalf("%s final App view cursor = %#v, want nil", name, view.Cursor)
	}
}

func TestAppViewMailComposerCursor(t *testing.T) {
	app := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{0}, 80, 24, "").app
	_ = app.mail.input.Focus()

	const text = "composer cursor"
	app = appCursorType(app, text)

	view := app.View()
	composer := "  > " + text
	if got, want := appCursorRequireRenderedEnd(t, view, composer), (tea.Position{X: 19, Y: 21}); got != want {
		t.Fatalf("80-column final App Mail cursor = %#v, want exact root-relative %#v", got, want)
	}
	if repeated := app.View(); repeated.Content != view.Content {
		t.Fatal("publishing the cursor changed final View.Content across read-only renders")
	}
}

func TestAppViewMailComposerCursorRootGeometry(t *testing.T) {
	for _, test := range []struct {
		name   string
		width  int
		height int
		banner string
		want   tea.Position
	}{
		{name: "85-column rail", width: 85, height: 24, want: tea.Position{X: 43, Y: 21}},
		{name: "100-column rail and top chrome", width: 100, height: 25, banner: "ROOT", want: tea.Position{X: 43, Y: 22}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{0}, test.width, test.height, test.banner).app
			app = appCursorType(app, "composer cursor")
			view := app.View()
			if got := appCursorRequireRenderedEnd(t, view, "  > composer cursor"); got != test.want {
				t.Fatalf("final App Mail cursor = %#v, want exact root-relative %#v", got, test.want)
			}

			lines := appCursorPlainLines(view)
			if got := ansi.Cut(lines[test.want.Y], 23, 24); got != "│" {
				t.Fatalf("visible rail divider at root cell 23 = %q, want %q", got, "│")
			}
			if test.banner != "" && (len(lines) == 0 || !strings.Contains(lines[0], test.banner)) {
				t.Fatalf("final View.Content first row does not contain top chrome %q", test.banner)
			}
		})
	}
}

func TestAppViewMailComposerCursorCJKSoftWrap(t *testing.T) {
	app := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{0}, 40, 24, "").app
	const cjk = "你"
	app = appCursorType(app, strings.Repeat(cjk, 20))

	view := app.View()
	if view.Cursor == nil {
		t.Fatal("wrapped CJK final App Mail view cursor = nil")
	}
	lines := appCursorPlainLines(view)
	if view.Cursor.Y < 0 || view.Cursor.Y >= len(lines) {
		t.Fatalf("wrapped CJK cursor Y=%d outside final view with %d rows", view.Cursor.Y, len(lines))
	}
	firstComposerRow := -1
	for y, line := range lines {
		if strings.Contains(line, "  > ") {
			firstComposerRow = y
			break
		}
	}
	if firstComposerRow < 0 {
		t.Fatal("wrapped CJK final view has no first composer row")
	}
	if view.Cursor.Y <= firstComposerRow {
		t.Fatalf("wrapped CJK cursor stayed on first composer row %d: got Y=%d", firstComposerRow, view.Cursor.Y)
	}
	if view.Cursor.X < 2 {
		t.Fatalf("wrapped CJK cursor X=%d, want a cell after the final wide glyph", view.Cursor.X)
	}
	if got := ansi.Cut(lines[view.Cursor.Y], view.Cursor.X-2, view.Cursor.X); got != cjk {
		t.Fatalf("two display cells before wrapped CJK cursor = %q, want %q", got, cjk)
	}
	if repeated := app.View(); repeated.Content != view.Content {
		t.Fatal("wrapped CJK cursor publication changed final View.Content across renders")
	}
}

func TestAppViewMailComposerCursorWithActivePalette(t *testing.T) {
	app := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{0}, 80, 28, "").app
	app = appCursorType(app, "/agents")
	if !app.mail.input.IsPaletteActive() || app.mail.palette.LineCount() == 0 {
		t.Fatal("typing /agents did not leave a visible active slash palette")
	}

	view := app.View()
	cursor := appCursorRequireRenderedEnd(t, view, "  > /agents")
	lines := appCursorPlainLines(view)
	paletteLines := app.mail.palette.LineCount()
	if cursor.Y < paletteLines {
		t.Fatalf("composer cursor row %d cannot follow %d palette rows", cursor.Y, paletteLines)
	}
	visiblePalette := strings.Join(lines[cursor.Y-paletteLines:cursor.Y], "\n")
	if strings.TrimSpace(visiblePalette) == "" || !strings.Contains(strings.ToLower(visiblePalette), "agents") {
		t.Fatalf("final rows before focused composer do not contain the active agents palette:\n%s", visiblePalette)
	}
}

func TestAppViewMailComposerCursorWithZeroResultPalette(t *testing.T) {
	app := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{0}, 80, 28, "").app
	const text = "/btw 你"
	app = appCursorType(app, text)
	if !app.mail.input.IsPaletteActive() {
		t.Fatal("typing a valid slash command with an argument did not leave the palette active")
	}
	if got := app.mail.palette.LineCount(); got != 0 {
		t.Fatalf("zero-result slash palette line count = %d, want 0", got)
	}
	if got := app.mail.palette.View(); got != "" {
		t.Fatalf("zero-result slash palette rendered %q, want empty output", got)
	}

	view := app.View()
	appCursorRequireRenderedEnd(t, view, "  > "+text)
}

func TestAppViewMailComposerCursorDirectAndTelemetry(t *testing.T) {
	t.Run("direct target and root chrome", func(t *testing.T) {
		fixture := newDirectPerformanceFixture(t, 50, 100, 24, nil)
		app := directPerformanceActivateA(t, fixture.app, fixture.targetA)
		app.startupBanner = "ROOT"
		app, _ = directPerformanceApply(app, tea.WindowSizeMsg{Width: 100, Height: 25})
		_ = app.mail.input.Focus()
		app = appCursorType(app, "direct cursor")

		view := app.View()
		appCursorRequireRenderedEnd(t, view, "  > direct cursor")
		plain := ansi.Strip(view.Content)
		if !strings.Contains(plain, "ROOT") || !strings.Contains(plain, "Alpha") {
			t.Fatalf("direct final view lacks root chrome or direct target header:\n%s", plain)
		}
		if strings.Contains(plain, "73%") {
			t.Fatal("direct final view unexpectedly retained Main telemetry")
		}
	})

	t.Run("ordinary banner telemetry and root chrome", func(t *testing.T) {
		mail := newReadyMailModelWithTelemetry(t, 100, 24)
		app := App{currentView: appViewMail, mail: mail, startupBanner: "ROOT"}
		app, _ = visibleRailV2Apply(app, tea.WindowSizeMsg{Width: 100, Height: 25})
		app.mail.loadedExtra = 1
		// The only top banner left is the one-time initial-loading line: no
		// history-count state exists to keep a banner visible. Assert it here so
		// this stays the both-banners-plus-telemetry geometry the case tests.
		app.mail.initialLoading = true
		app.mail.lastBannerLines = -1
		app.mail.syncViewportHeight()
		if app.mail.bannerLineCount() != 2 {
			t.Fatalf("ordinary geometry fixture wanted both banners reserved, got %d lines", app.mail.bannerLineCount())
		}
		_ = app.mail.input.Focus()
		app = appCursorType(app, "telemetry cursor")

		view := app.View()
		appCursorRequireRenderedEnd(t, view, "  > telemetry cursor")
		plain := ansi.Strip(view.Content)
		for _, want := range []string{"ROOT", "73%", i18n.T("mail.initial_loading"), i18n.T("mail.collapse")} {
			if !strings.Contains(plain, want) {
				t.Fatalf("ordinary final view lacks %q while projecting cursor:\n%s", want, plain)
			}
		}
	})
}

func TestAppViewMailComposerCursorNilStates(t *testing.T) {
	type nilState struct {
		name      string
		configure func(App) App
	}
	states := []nilState{
		{
			name: "non-Mail",
			configure: func(app App) App {
				app.currentView = appViewHelp
				app.help = NewHelpModel()
				return app
			},
		},
		{
			name: "not ready",
			configure: func(app App) App {
				app.mail.ready = false
				return app
			},
		},
		{
			name: "editor warning",
			configure: func(app App) App {
				app.mail.showEditorWarn = true
				return app
			},
		},
		{
			name: "/agents selector",
			configure: func(app App) App {
				app.mail = app.mail.openAgentSelector()
				return app
			},
		},
		{
			name: "copy mode",
			configure: func(app App) App {
				app.mail.copyMode = true
				return app
			},
		},
		{
			name: "rail focus",
			configure: func(app App) App {
				app.mail.agentRail.focused = true
				return app
			},
		},
		{
			name: "blurred input",
			configure: func(app App) App {
				app.mail.input.Blur()
				return app
			},
		},
		{
			name: "dependency nil cursor",
			configure: func(app App) App {
				app.mail.input.textarea.SetVirtualCursor(true)
				return app
			},
		},
	}

	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			app := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{0}, 85, 24, "").app
			_ = app.mail.input.Focus()
			app = appCursorType(app, "hidden cursor")
			app = state.configure(app)
			appCursorRequireNil(t, state.name, app)
		})
	}
}
