package tui

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

type agentRailCollapseStableState struct {
	rows                 []agentSelectorRow
	cursor               int
	selectedThreadKey    string
	currentTarget        fs.DirectTarget
	currentDirect        bool
	railScrollOffset     int
	mainViewportOffset   int
	directViewportOffset int
	draft                string
}

type agentRailCollapseGeometryState struct {
	budget              LayoutBudget
	mailWidth           int
	composerWidth       int
	mainViewportWidth   int
	directViewportWidth int
	explicitlyCollapsed bool
	railFocused         bool
	composerFocused     bool
}

type agentRailRawInputModel struct {
	app       App
	key       tea.KeyPressMsg
	sawKey    bool
	appTypeOK bool
}

func (m agentRailRawInputModel) Init() tea.Cmd {
	return nil
}

func (m agentRailRawInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	m.key = key
	m.sawKey = true
	updated, _ := m.app.Update(key)
	m.app, m.appTypeOK = updated.(App)
	return m, tea.Quit
}

func (m agentRailRawInputModel) View() tea.View {
	return tea.NewView("")
}

func agentRailCollapseApply(t *testing.T, app App, msg tea.Msg) (App, tea.Cmd) {
	t.Helper()
	model, cmd := app.Update(msg)
	updated, ok := model.(App)
	if !ok {
		t.Fatalf("App.Update returned %T, want tui.App", model)
	}
	return updated, cmd
}

func agentRailCollapseApplySyntheticSize(t *testing.T, app App) App {
	t.Helper()
	cmd := app.sendSize()
	if cmd == nil {
		t.Fatal("App.sendSize returned no synthetic child resize")
	}
	raw := cmd()
	msg, ok := raw.(childWindowSizeMsg)
	if !ok {
		t.Fatalf("App.sendSize produced %T, want childWindowSizeMsg", raw)
	}
	updated, follow := agentRailCollapseApply(t, app, msg)
	if follow != nil {
		t.Error("synthetic child resize returned an unexpected command")
	}
	return updated
}

func agentRailCollapseStableSnapshot(app App) agentRailCollapseStableState {
	target, direct := app.mail.currentDirectTarget()
	return agentRailCollapseStableState{
		rows:                 append([]agentSelectorRow(nil), app.mail.agentSelector.rows...),
		cursor:               app.mail.agentSelector.cursor,
		selectedThreadKey:    app.mail.agentSelector.selectedThreadKey,
		currentTarget:        target,
		currentDirect:        direct,
		railScrollOffset:     app.mail.agentRail.scrollOffset,
		mainViewportOffset:   app.mail.viewport.YOffset(),
		directViewportOffset: app.mail.directChat.viewport.YOffset(),
		draft:                app.mail.input.Value(),
	}
}

func agentRailCollapseGeometrySnapshot(app App) agentRailCollapseGeometryState {
	return agentRailCollapseGeometryState{
		budget:              app.layoutBudget(),
		mailWidth:           app.mail.width,
		composerWidth:       app.mail.input.width,
		mainViewportWidth:   app.mail.viewport.Width(),
		directViewportWidth: app.mail.directChat.viewport.Width(),
		explicitlyCollapsed: app.mail.agentRail.explicitlyCollapsed,
		railFocused:         app.mail.agentRail.focused,
		composerFocused:     app.mail.input.Focused(),
	}
}

func agentRailCollapseAssertStable(t *testing.T, stage string, want agentRailCollapseStableState, app App) {
	t.Helper()
	if got := agentRailCollapseStableSnapshot(app); !reflect.DeepEqual(got, want) {
		t.Errorf("%s changed canonical conversation/draft/scroll state:\n got: %#v\nwant: %#v", stage, got, want)
	}
	if len(app.mail.agentRail.unreadByThread) != 0 {
		t.Errorf("%s zero-unread fixture acquired badges: %#v", stage, app.mail.agentRail.unreadByThread)
	}
}

func agentRailCollapseAssertGeometryAtWidth(t *testing.T, stage string, app App, terminalWidth, wantRail, wantContent int) {
	t.Helper()
	budget := app.layoutBudget()
	if budget.TerminalWidth != terminalWidth || budget.RailWidth != wantRail || budget.ContentWidth != wantContent {
		t.Errorf("%s at width %d rail/content = %d/%d, want %d/%d in the same update",
			stage, terminalWidth, budget.RailWidth, budget.ContentWidth, wantRail, wantContent)
	}

	viewportWidth := app.mail.viewport.Width()
	viewportName := "Main"
	if _, direct := app.mail.currentDirectTarget(); direct {
		viewportWidth = app.mail.directChat.viewport.Width()
		viewportName = "direct"
	}
	if app.mail.width != wantContent ||
		app.mail.input.width != wantContent ||
		viewportWidth != wantContent {
		t.Errorf("%s child geometry mail/composer/%s-viewport = %d/%d/%d, want %d/%d/%d in the same update",
			stage, viewportName, app.mail.width, app.mail.input.width, viewportWidth,
			wantContent, wantContent, wantContent)
	}
}

func agentRailCollapseAssertGeometry(t *testing.T, stage string, app App, wantRail, wantContent int) {
	t.Helper()
	agentRailCollapseAssertGeometryAtWidth(t, stage, app, 85, wantRail, wantContent)
}

func agentRailCollapseRenderedLine(t *testing.T, app App, childY int) string {
	t.Helper()
	lines := strings.Split(app.View().Content, "\n")
	rootY := app.layoutBudget().TopChromeRows + childY
	if rootY < 0 || rootY >= len(lines) {
		t.Fatalf("rendered child row %d translated to root row %d outside %d lines", childY, rootY, len(lines))
	}
	return lines[rootY]
}

func agentRailCollapseRenderedCells(t *testing.T, app App, childY, start, end int) string {
	t.Helper()
	return ansi.Strip(ansi.Cut(agentRailCollapseRenderedLine(t, app, childY), start, end))
}

func TestAgentRailCtrlGAccessibleControls(t *testing.T) {
	newFixture := func(t *testing.T) App {
		t.Helper()
		return newVisibleRailV2Fixture(
			t,
			[]string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel"},
			[]int{0, 0, 0, 0, 0, 0, 0, 0},
			85,
			8,
			"ROOT",
		).app
	}
	ctrlG := tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}
	if ctrlG.Code != 'g' || ctrlG.Mod != tea.ModCtrl || ctrlG.Text != "" {
		t.Fatalf("pinned Bubble Tea Ctrl+G representation = %#v, want Code 'g', ModCtrl, and empty Text", ctrlG)
	}
	if got := ctrlG.String(); got != "ctrl+g" {
		t.Fatalf("pinned Bubble Tea Ctrl+G representation string = %q, want %q", got, "ctrl+g")
	}

	app := visibleRailV2Focus(t, newFixture(t))
	app.mail.input.SetValue("Main retained draft")
	app.mail = app.mail.setSelectorCursor(2)
	var activationCmd tea.Cmd
	app, activationCmd = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if activationCmd == nil {
		t.Fatal("canonical direct activation produced no visibility command")
	}
	app.mail.input.SetValue("direct retained draft")
	app.mail = app.mail.
		setSelectorCursor(len(app.mail.agentSelector.rows) - 1).
		keepAgentRailCursorVisible()
	if app.mail.agentRail.scrollOffset == 0 {
		t.Fatal("fixture did not establish a retained rail scroll offset")
	}
	stable := agentRailCollapseStableSnapshot(app)
	if !stable.currentDirect {
		t.Fatal("fixture did not establish a direct conversation")
	}

	var cmd tea.Cmd
	app, cmd = agentRailCollapseApply(t, app, ctrlG)
	if cmd != nil {
		t.Error("Ctrl+G collapse returned an unexpected command")
	}
	if !app.mail.agentRail.explicitlyCollapsed {
		t.Error("Ctrl+G did not collapse the eligible expanded rail")
	}
	agentRailCollapseAssertGeometry(t, "Ctrl+G collapse", app, 0, 85)
	if app.mail.agentRail.focused || !app.mail.input.Focused() {
		t.Errorf("Ctrl+G collapse did not transfer focus to the composer: rail=%v composer=%v",
			app.mail.agentRail.focused, app.mail.input.Focused())
	}
	agentRailCollapseAssertStable(t, "Ctrl+G collapse", stable, app)

	app, cmd = agentRailCollapseApply(t, app, ctrlG)
	if cmd != nil {
		t.Error("Ctrl+G expansion returned an unexpected command")
	}
	if app.mail.agentRail.explicitlyCollapsed {
		t.Error("Ctrl+G did not expand the eligible collapsed rail")
	}
	agentRailCollapseAssertGeometry(t, "Ctrl+G expansion", app, 24, 61)
	if app.mail.agentRail.focused || !app.mail.input.Focused() {
		t.Errorf("Ctrl+G expansion took rail focus: rail=%v composer=%v",
			app.mail.agentRail.focused, app.mail.input.Focused())
	}
	agentRailCollapseAssertStable(t, "Ctrl+G round trip", stable, app)

	for _, state := range []struct {
		name                string
		explicitlyCollapsed bool
		configure           func(*testing.T, App) App
		active              func(App) bool
	}{
		{
			name: "width 84",
			configure: func(t *testing.T, app App) App {
				app, _ = agentRailCollapseApply(t, app, tea.WindowSizeMsg{Width: 84, Height: 8})
				return app
			},
			active: func(app App) bool {
				return app.width == 84 && !app.layoutBudget().RailVisible
			},
		},
		{
			name:                "/agents modal",
			explicitlyCollapsed: true,
			configure: func(_ *testing.T, app App) App {
				app.mail = app.mail.openAgentSelector()
				return app
			},
			active: func(app App) bool { return app.mail.agentSelector.selectorOpen },
		},
	} {
		t.Run("inert in "+state.name, func(t *testing.T) {
			app := newFixture(t)
			if state.explicitlyCollapsed {
				app, _ = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
			}
			app = state.configure(t, app)
			if !state.active(app) {
				t.Fatalf("%s fixture did not establish its ineligible state", state.name)
			}
			stable := agentRailCollapseStableSnapshot(app)
			wantCollapsed := app.mail.agentRail.explicitlyCollapsed

			var cmd tea.Cmd
			app, cmd = agentRailCollapseApply(t, app, ctrlG)
			if cmd != nil {
				t.Errorf("Ctrl+G in %s returned an unexpected command", state.name)
			}
			if app.mail.agentRail.explicitlyCollapsed != wantCollapsed {
				t.Errorf("Ctrl+G in %s changed latent collapsed=%v to %v",
					state.name, wantCollapsed, app.mail.agentRail.explicitlyCollapsed)
			}
			if !state.active(app) {
				t.Errorf("Ctrl+G in %s dismissed its ineligible state", state.name)
			}
			agentRailCollapseAssertStable(t, "ineligible "+state.name+" Ctrl+G", stable, app)
		})
	}
}

func TestAgentRailRawInputAccelerators(t *testing.T) {
	// The F2 case is supplemental parser-to-App coverage that is expected to be
	// GREEN on the base commit. It cannot reproduce macOS interception before
	// bytes reach stdin, so it is not the authentic RED for this change.
	tests := []struct {
		name string
		raw  string
		key  tea.KeyPressMsg
	}{
		{name: "Ctrl+G", raw: "\x07", key: tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}},
		{name: "SS3 F2", raw: "\x1bOQ", key: tea.KeyPressMsg{Code: tea.KeyF2}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newVisibleRailV2Fixture(
				t,
				[]string{"Alpha", "Bravo"},
				[]int{0, 0},
				85,
				14,
				"",
			).app
			app = visibleRailV2Focus(t, app)
			stable := agentRailCollapseStableSnapshot(app)

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			program := tea.NewProgram(
				agentRailRawInputModel{app: app},
				tea.WithContext(ctx),
				tea.WithInput(strings.NewReader(test.raw)),
				tea.WithOutput(io.Discard),
				tea.WithoutRenderer(),
				tea.WithoutSignalHandler(),
				tea.WithEnvironment([]string{"TERM=xterm-256color"}),
			)
			final, err := program.Run()
			if err != nil {
				t.Fatalf("Bubble Tea program returned an error: %v", err)
			}
			got, ok := final.(agentRailRawInputModel)
			if !ok {
				t.Fatalf("Bubble Tea program returned %T, want agentRailRawInputModel", final)
			}
			if !got.sawKey {
				t.Fatal("Bubble Tea program saw no KeyPressMsg")
			}
			if got.key != test.key {
				t.Errorf("raw bytes decoded to %#v, want %#v", got.key, test.key)
			}
			if !got.appTypeOK {
				t.Fatal("App.Update did not return tui.App")
			}
			if !got.app.mail.agentRail.explicitlyCollapsed {
				t.Error("decoded key did not collapse the eligible rail through App.Update")
			}
			agentRailCollapseAssertGeometry(t, "raw input collapse", got.app, 0, 85)
			if got.app.mail.agentRail.focused || !got.app.mail.input.Focused() {
				t.Errorf("raw input collapse did not transfer focus: rail=%v composer=%v",
					got.app.mail.agentRail.focused, got.app.mail.input.Focused())
			}
			agentRailCollapseAssertStable(t, "raw input collapse", stable, got.app)
		})
	}
}

func TestAgentRailF2CollapseStateMachine(t *testing.T) {
	for _, test := range []struct {
		name   string
		direct bool
	}{
		{name: "Main", direct: false},
		{name: "direct", direct: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newVisibleRailV2Fixture(
				t,
				[]string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel"},
				[]int{0, 0, 0, 0, 0, 0, 0, 0},
				85,
				8,
				"",
			)
			app := fixture.app
			app.mail.input.SetValue("Main retained draft")
			if test.direct {
				app = visibleRailV2Focus(t, app)
				index := visibleRailV2RowIndex(t, app.mail, fixture.targets[2].AgentID)
				app.mail = app.mail.setSelectorCursor(index)
				var activationCmd tea.Cmd
				app, activationCmd = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
				if activationCmd == nil {
					t.Fatal("canonical direct activation produced no visibility command")
				}
				app.mail.input.SetValue("direct collapse draft")
			} else {
				app.mail.input.SetValue("Main collapse draft")
				app = visibleRailV2Focus(t, app)
			}
			app.mail = app.mail.
				setSelectorCursor(len(app.mail.agentSelector.rows) - 1).
				keepAgentRailCursorVisible()
			if app.mail.agentRail.scrollOffset == 0 {
				t.Fatal("fixture did not establish a nonzero retained rail scroll offset")
			}

			agentRailCollapseAssertGeometry(t, "expanded baseline", app, 24, 61)
			stable := agentRailCollapseStableSnapshot(app)

			var cmd tea.Cmd
			app, cmd = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
			if cmd != nil {
				t.Error("F2 collapse returned an unexpected command")
			}
			agentRailCollapseAssertGeometry(t, "F2 collapse", app, 0, 85)
			if app.mail.agentRail.focused || !app.mail.input.Focused() {
				t.Errorf("F2 collapse did not blur the focused rail and focus the composer in the same update: rail=%v composer=%v",
					app.mail.agentRail.focused, app.mail.input.Focused())
			}
			agentRailCollapseAssertStable(t, "F2 collapse", stable, app)
			if app.layoutBudget().RailVisible {
				return
			}

			app, cmd = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyTab})
			if cmd != nil {
				t.Error("Tab with the rail hidden returned an unexpected command")
			}
			if app.mail.agentRail.focused || !app.mail.input.Focused() {
				t.Errorf("Tab entered a hidden rail: rail=%v composer=%v",
					app.mail.agentRail.focused, app.mail.input.Focused())
			}
			agentRailCollapseAssertGeometry(t, "hidden-rail Tab", app, 0, 85)
			agentRailCollapseAssertStable(t, "hidden-rail Tab", stable, app)

			app, cmd = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
			if cmd != nil {
				t.Error("F2 expansion returned an unexpected command")
			}
			agentRailCollapseAssertGeometry(t, "F2 expansion", app, 24, 61)
			if app.mail.agentRail.focused || !app.mail.input.Focused() {
				t.Errorf("F2 expansion changed focus instead of leaving the composer focused: rail=%v composer=%v",
					app.mail.agentRail.focused, app.mail.input.Focused())
			}
			agentRailCollapseAssertStable(t, "F2 expansion", stable, app)

			if test.direct {
				app, cmd = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
				if cmd != nil {
					t.Error("second F2 collapse returned an unexpected command")
				}
				agentRailCollapseAssertGeometry(t, "direct-to-Main collapse", app, 0, 85)
				mainIndex := visibleRailV2RowIndex(t, app.mail, "")
				app.mail, cmd = app.mail.activateConversationRow(mainIndex)
				if cmd != nil {
					t.Error("canonical Main activation returned an unexpected command")
				}
				agentRailCollapseAssertGeometry(t, "Main restored after direct collapse", app, 0, 85)
				if got := app.mail.input.Value(); got != "Main retained draft" {
					t.Errorf("Main activation restored draft %q, want %q", got, "Main retained draft")
				}
				if app.mail.agentRail.focused {
					t.Error("Main activation after direct collapse restored stale rail focus")
				}
			}
		})
	}
}

func TestAgentRailF2LatentPreferenceAndEligibility(t *testing.T) {
	newFixture := func(t *testing.T) visibleRailV2Fixture {
		t.Helper()
		return newVisibleRailV2Fixture(
			t,
			[]string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel"},
			[]int{0, 0, 0, 0, 0, 0, 0, 0},
			85,
			16,
			"",
		)
	}

	t.Run("84 is inert and widening restores the latent preference", func(t *testing.T) {
		fixture := newFixture(t)
		if fixture.app.mail.agentRail.explicitlyCollapsed {
			t.Fatal("fresh process state started explicitly collapsed, want expanded")
		}
		agentRailCollapseAssertGeometry(t, "fresh expanded preference", fixture.app, 24, 61)

		for _, explicitlyCollapsed := range []bool{false, true} {
			name := "expanded"
			if explicitlyCollapsed {
				name = "collapsed"
			}
			t.Run(name, func(t *testing.T) {
				raw := newFixture(t).app
				synthetic := newFixture(t).app
				if explicitlyCollapsed {
					var rawCmd, syntheticCmd tea.Cmd
					raw, rawCmd = agentRailCollapseApply(t, raw, tea.KeyPressMsg{Code: tea.KeyF2})
					synthetic, syntheticCmd = agentRailCollapseApply(t, synthetic, tea.KeyPressMsg{Code: tea.KeyF2})
					if rawCmd != nil || syntheticCmd != nil {
						t.Errorf("eligible F2 setup returned commands: raw=%v synthetic=%v",
							rawCmd != nil, syntheticCmd != nil)
					}
				}
				if raw.mail.agentRail.explicitlyCollapsed != explicitlyCollapsed ||
					synthetic.mail.agentRail.explicitlyCollapsed != explicitlyCollapsed {
					t.Fatalf("eligible F2 did not establish latent collapsed=%v", explicitlyCollapsed)
				}

				raw, _ = agentRailCollapseApply(t, raw, tea.WindowSizeMsg{Width: 84, Height: 16})
				synthetic.width = 84
				synthetic.height = 16
				synthetic = agentRailCollapseApplySyntheticSize(t, synthetic)
				agentRailCollapseAssertGeometryAtWidth(t, "raw narrow resize", raw, 84, 0, 84)
				agentRailCollapseAssertGeometryAtWidth(t, "synthetic narrow resize", synthetic, 84, 0, 84)
				if got, want := agentRailCollapseGeometrySnapshot(synthetic), agentRailCollapseGeometrySnapshot(raw); !reflect.DeepEqual(got, want) {
					t.Errorf("raw and synthetic narrow resize differ:\n synthetic: %#v\n       raw: %#v", got, want)
				}

				rawStable := agentRailCollapseStableSnapshot(raw)
				syntheticStable := agentRailCollapseStableSnapshot(synthetic)
				var rawF2Cmd, syntheticF2Cmd tea.Cmd
				raw, rawF2Cmd = agentRailCollapseApply(t, raw, tea.KeyPressMsg{Code: tea.KeyF2})
				synthetic, syntheticF2Cmd = agentRailCollapseApply(t, synthetic, tea.KeyPressMsg{Code: tea.KeyF2})
				if rawF2Cmd != nil || syntheticF2Cmd != nil {
					t.Errorf("F2 at width 84 returned commands: raw=%v synthetic=%v",
						rawF2Cmd != nil, syntheticF2Cmd != nil)
				}
				if raw.mail.agentRail.explicitlyCollapsed != explicitlyCollapsed ||
					synthetic.mail.agentRail.explicitlyCollapsed != explicitlyCollapsed {
					t.Errorf("F2 at width 84 changed latent preference: raw=%v synthetic=%v want=%v",
						raw.mail.agentRail.explicitlyCollapsed,
						synthetic.mail.agentRail.explicitlyCollapsed,
						explicitlyCollapsed)
				}
				agentRailCollapseAssertGeometryAtWidth(t, "raw narrow F2", raw, 84, 0, 84)
				agentRailCollapseAssertGeometryAtWidth(t, "synthetic narrow F2", synthetic, 84, 0, 84)
				agentRailCollapseAssertStable(t, "raw narrow F2", rawStable, raw)
				agentRailCollapseAssertStable(t, "synthetic narrow F2", syntheticStable, synthetic)

				raw, _ = agentRailCollapseApply(t, raw, tea.WindowSizeMsg{Width: 85, Height: 16})
				synthetic.width = 85
				synthetic.height = 16
				synthetic = agentRailCollapseApplySyntheticSize(t, synthetic)
				wantRail, wantContent := 24, 61
				if explicitlyCollapsed {
					wantRail, wantContent = 0, 85
				}
				agentRailCollapseAssertGeometry(t, "raw widening", raw, wantRail, wantContent)
				agentRailCollapseAssertGeometry(t, "synthetic widening", synthetic, wantRail, wantContent)
				if got, want := agentRailCollapseGeometrySnapshot(synthetic), agentRailCollapseGeometrySnapshot(raw); !reflect.DeepEqual(got, want) {
					t.Errorf("raw and synthetic widening differ:\n synthetic: %#v\n       raw: %#v", got, want)
				}
			})
		}
	})

	t.Run("leaving and returning preserves preference but never rail focus", func(t *testing.T) {
		for _, explicitlyCollapsed := range []bool{false, true} {
			name := "expanded"
			if explicitlyCollapsed {
				name = "collapsed"
			}
			t.Run(name, func(t *testing.T) {
				app := newFixture(t).app
				if explicitlyCollapsed {
					app, _ = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
				} else {
					app = visibleRailV2Focus(t, app)
				}

				app, _ = agentRailCollapseApply(t, app, ViewChangeMsg{View: "help"})
				if app.currentView != appViewHelp {
					t.Fatalf("leaving Mail reached view %v, want Help", app.currentView)
				}
				if app.mail.agentRail.explicitlyCollapsed != explicitlyCollapsed {
					t.Errorf("leaving Mail changed latent collapsed=%v to %v",
						explicitlyCollapsed, app.mail.agentRail.explicitlyCollapsed)
				}
				if app.mail.agentRail.focused || !app.mail.input.Focused() {
					t.Errorf("leaving Mail retained rail focus: rail=%v composer=%v",
						app.mail.agentRail.focused, app.mail.input.Focused())
				}

				app, _ = agentRailCollapseApply(t, app, ViewChangeMsg{View: "mail"})
				app = agentRailCollapseApplySyntheticSize(t, app)
				if app.currentView != appViewMail {
					t.Fatalf("return reached view %v, want Mail", app.currentView)
				}
				if app.mail.agentRail.explicitlyCollapsed != explicitlyCollapsed ||
					app.mail.agentRail.focused || !app.mail.input.Focused() {
					t.Errorf("return restored wrong preference/focus: collapsed=%v rail=%v composer=%v",
						app.mail.agentRail.explicitlyCollapsed,
						app.mail.agentRail.focused,
						app.mail.input.Focused())
				}
				wantRail, wantContent := 24, 61
				if explicitlyCollapsed {
					wantRail, wantContent = 0, 85
				}
				agentRailCollapseAssertGeometry(t, "return to Mail", app, wantRail, wantContent)
			})
		}
	})

	t.Run("model replacement carries only preference and restart defaults expanded", func(t *testing.T) {
		fixture := newFixture(t)
		app, _ := agentRailCollapseApply(t, fixture.app, tea.KeyPressMsg{Code: tea.KeyF2})
		app.mail.agentSelector.cursor = len(app.mail.agentSelector.rows) - 1
		app.mail.agentRail.scrollOffset = 4

		replacement := NewMailModel(
			fixture.humanDir,
			visibleRailV2Human,
			fixture.lingtai,
			"",
			"Main",
			200,
			"",
			"en",
			false,
			0,
		)
		replacement.agentRail.focused = true
		replacement.input.Blur()
		app.installMailModel(replacement)
		if !app.mail.agentRail.explicitlyCollapsed {
			t.Error("Mail-model replacement lost the explicit collapsed preference")
		}
		if app.mail.agentRail.focused || !app.mail.input.Focused() {
			t.Errorf("Mail-model replacement retained stale rail focus: rail=%v composer=%v",
				app.mail.agentRail.focused, app.mail.input.Focused())
		}
		if len(app.mail.agentSelector.rows) != 0 ||
			app.mail.agentSelector.cursor != 0 ||
			app.mail.agentRail.scrollOffset != 0 {
			t.Errorf("Mail-model replacement carried stale selector/scroll state: rows=%d cursor=%d scroll=%d",
				len(app.mail.agentSelector.rows),
				app.mail.agentSelector.cursor,
				app.mail.agentRail.scrollOffset)
		}
		app = agentRailCollapseApplySyntheticSize(t, app)
		agentRailCollapseAssertGeometry(t, "replacement with retained preference", app, 0, 85)

		restarted := App{
			currentView: appViewMail,
			projectDir:  fixture.lingtai,
			mail: NewMailModel(
				fixture.humanDir,
				visibleRailV2Human,
				fixture.lingtai,
				"",
				"Main",
				200,
				"",
				"en",
				false,
				0,
			),
		}
		restarted, _ = agentRailCollapseApply(t, restarted, tea.WindowSizeMsg{Width: 85, Height: 16})
		if restarted.mail.agentRail.explicitlyCollapsed {
			t.Error("fresh App reconstructed a persisted collapsed preference")
		}
		agentRailCollapseAssertGeometry(t, "fresh process restart", restarted, 24, 61)
	})

	t.Run("F2 is inert outside eligible unobscured Mail", func(t *testing.T) {
		type ineligibleState struct {
			name      string
			configure func(App) App
			active    func(App) bool
		}
		states := []ineligibleState{
			{
				name: "non-Mail",
				configure: func(app App) App {
					app.currentView = appViewHelp
					app.help = NewHelpModel()
					return app
				},
				active: func(app App) bool { return app.currentView == appViewHelp },
			},
			{
				name: "copy mode",
				configure: func(app App) App {
					app.mail.copyMode = true
					return app
				},
				active: func(app App) bool { return app.mail.copyMode },
			},
			{
				name: "/agents",
				configure: func(app App) App {
					app.mail = app.mail.openAgentSelector()
					return app
				},
				active: func(app App) bool { return app.mail.agentSelector.selectorOpen },
			},
			{
				name: "editor warning",
				configure: func(app App) App {
					app.mail.showEditorWarn = true
					return app
				},
				active: func(app App) bool { return app.mail.showEditorWarn },
			},
			{
				name: "command palette",
				configure: func(app App) App {
					app.mail.input.SetValue("/agents")
					app.mail.palette.SetFilter("agents")
					return app
				},
				active: func(app App) bool { return app.mail.input.IsPaletteActive() },
			},
		}

		for _, state := range states {
			for _, explicitlyCollapsed := range []bool{false, true} {
				name := state.name + "/expanded"
				if explicitlyCollapsed {
					name = state.name + "/collapsed"
				}
				t.Run(name, func(t *testing.T) {
					app := newFixture(t).app
					if explicitlyCollapsed {
						app, _ = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
					}
					app = state.configure(app)
					if !state.active(app) {
						t.Fatalf("%s fixture did not establish its obstruction", state.name)
					}
					stable := agentRailCollapseStableSnapshot(app)
					railFocused := app.mail.agentRail.focused
					composerFocused := app.mail.input.Focused()
					statusFlash := app.mail.statusFlash
					statusExpiry := app.mail.statusExpiry

					var cmd tea.Cmd
					app, cmd = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
					if cmd != nil {
						t.Errorf("F2 in %s returned an unexpected command", state.name)
					}
					if app.mail.agentRail.explicitlyCollapsed != explicitlyCollapsed {
						t.Errorf("F2 in %s changed latent collapsed=%v to %v",
							state.name, explicitlyCollapsed, app.mail.agentRail.explicitlyCollapsed)
					}
					if !state.active(app) {
						t.Errorf("F2 in %s dismissed or displaced the owning surface", state.name)
					}
					if app.mail.statusFlash != statusFlash || !app.mail.statusExpiry.Equal(statusExpiry) {
						t.Errorf("F2 in %s changed status state: flash %q->%q expiry %v->%v",
							state.name,
							statusFlash,
							app.mail.statusFlash,
							statusExpiry,
							app.mail.statusExpiry)
					}
					if app.mail.agentRail.focused != railFocused ||
						app.mail.input.Focused() != composerFocused {
						t.Errorf("F2 in %s changed focus: rail %v->%v composer %v->%v",
							state.name,
							railFocused,
							app.mail.agentRail.focused,
							composerFocused,
							app.mail.input.Focused())
					}
					agentRailCollapseAssertStable(t, "ineligible "+state.name+" F2", stable, app)
				})
			}
		}
	})
}

func TestAgentRailCollapseMouseControlGeometry(t *testing.T) {
	newFixture := func(t *testing.T) App {
		t.Helper()
		fixture := newVisibleRailV2Fixture(
			t,
			[]string{"Alpha", "Bravo"},
			[]int{0, 0},
			85,
			14,
			"ROOT",
		)
		if got := fixture.app.layoutBudget().TopChromeRows; got != 1 {
			t.Fatalf("fixture TopChromeRows = %d, want 1", got)
		}
		if len(fixture.app.mail.agentRail.unreadByThread) != 0 {
			t.Fatalf("fixture has unread badges: %#v", fixture.app.mail.agentRail.unreadByThread)
		}
		return fixture.app
	}

	t.Run("expanded rendered cells collapse through one root chrome translation", func(t *testing.T) {
		for x := 20; x <= 22; x++ {
			t.Run(string(rune('a'+x-20)), func(t *testing.T) {
				app := visibleRailV2Focus(t, newFixture(t))
				budget := app.layoutBudget()
				if got := agentRailCollapseRenderedCells(t, app, 0, 20, 23); got != "[<]" {
					t.Errorf("expanded rail cells 20..22 render %q, want %q", got, "[<]")
				}

				stable := agentRailCollapseStableSnapshot(app)
				var cmd tea.Cmd
				app, cmd = agentRailCollapseApply(t, app, tea.MouseClickMsg(tea.Mouse{
					X: x, Y: budget.TopChromeRows, Button: tea.MouseLeft,
				}))
				if cmd != nil {
					t.Error("expanded collapse control scheduled unexpected work")
				}
				if !app.mail.agentRail.explicitlyCollapsed {
					t.Errorf("left click on expanded control cell x=%d did not collapse the rail", x)
				}
				agentRailCollapseAssertGeometry(t, "expanded control click", app, 0, 85)
				if app.mail.agentRail.focused || !app.mail.input.Focused() {
					t.Errorf("mouse collapse did not transfer focused rail to composer: rail=%v composer=%v",
						app.mail.agentRail.focused, app.mail.input.Focused())
				}
				agentRailCollapseAssertStable(t, "expanded control click", stable, app)
			})
		}

		app := newFixture(t)
		budget := app.layoutBudget()
		app, _ = agentRailCollapseApply(t, app, tea.MouseClickMsg(tea.Mouse{
			X: 20, Y: budget.TopChromeRows - 1, Button: tea.MouseLeft,
		}))
		if app.mail.agentRail.explicitlyCollapsed {
			t.Error("control X on root chrome row collapsed the rail before child-Y translation")
		}
	})

	t.Run("collapsed rendered cells expand without taking focus", func(t *testing.T) {
		for x := 0; x <= 2; x++ {
			t.Run(string(rune('a'+x)), func(t *testing.T) {
				app := newFixture(t)
				app, _ = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
				budget := app.layoutBudget()
				if got := agentRailCollapseRenderedCells(t, app, 0, 0, 3); got != "[>]" {
					t.Errorf("collapsed Mail header cells 0..2 render %q, want %q", got, "[>]")
				}

				stable := agentRailCollapseStableSnapshot(app)
				app, cmd := agentRailCollapseApply(t, app, tea.MouseClickMsg(tea.Mouse{
					X: x, Y: budget.TopChromeRows, Button: tea.MouseLeft,
				}))
				if cmd != nil {
					t.Error("collapsed expand control scheduled unexpected work")
				}
				if app.mail.agentRail.explicitlyCollapsed {
					t.Errorf("left click on collapsed control cell x=%d did not expand the rail", x)
				}
				agentRailCollapseAssertGeometry(t, "collapsed control click", app, 24, 61)
				if app.mail.agentRail.focused || !app.mail.input.Focused() {
					t.Errorf("mouse expansion changed focus: rail=%v composer=%v",
						app.mail.agentRail.focused, app.mail.input.Focused())
				}
				agentRailCollapseAssertStable(t, "collapsed control click", stable, app)
			})
		}
	})

	t.Run("both controls work above the width threshold without forcing focus", func(t *testing.T) {
		fixture := newVisibleRailV2Fixture(
			t,
			[]string{"Alpha", "Bravo"},
			[]int{0, 0},
			120,
			14,
			"ROOT",
		)
		app := fixture.app
		budget := app.layoutBudget()
		stable := agentRailCollapseStableSnapshot(app)
		if got := agentRailCollapseRenderedCells(t, app, 0, 20, 23); got != "[<]" {
			t.Fatalf("width-120 expanded control renders %q, want %q", got, "[<]")
		}

		var cmd tea.Cmd
		app, cmd = agentRailCollapseApply(t, app, tea.MouseClickMsg(tea.Mouse{
			X: 22, Y: budget.TopChromeRows, Button: tea.MouseLeft,
		}))
		if cmd != nil {
			t.Error("width-120 collapse control scheduled unexpected work")
		}
		if !app.mail.agentRail.explicitlyCollapsed {
			t.Error("width-120 expanded edge click did not collapse the rail")
		}
		agentRailCollapseAssertGeometryAtWidth(t, "width-120 collapse", app, 120, 0, 120)
		if app.mail.agentRail.focused || !app.mail.input.Focused() {
			t.Errorf("unfocused width-120 collapse changed focus: rail=%v composer=%v",
				app.mail.agentRail.focused, app.mail.input.Focused())
		}
		if got := agentRailCollapseRenderedCells(t, app, 0, 0, 3); got != "[>]" {
			t.Fatalf("width-120 collapsed control renders %q, want %q", got, "[>]")
		}

		budget = app.layoutBudget()
		app, cmd = agentRailCollapseApply(t, app, tea.MouseClickMsg(tea.Mouse{
			X: 2, Y: budget.TopChromeRows, Button: tea.MouseLeft,
		}))
		if cmd != nil {
			t.Error("width-120 expand control scheduled unexpected work")
		}
		if app.mail.agentRail.explicitlyCollapsed {
			t.Error("width-120 collapsed edge click did not expand the rail")
		}
		agentRailCollapseAssertGeometryAtWidth(t, "width-120 expansion", app, 120, 24, 96)
		if app.mail.agentRail.focused || !app.mail.input.Focused() {
			t.Errorf("width-120 expansion changed focus: rail=%v composer=%v",
				app.mail.agentRail.focused, app.mail.input.Focused())
		}
		agentRailCollapseAssertStable(t, "width-120 control round trip", stable, app)
	})

	t.Run("collapsed loading frame has no invisible target", func(t *testing.T) {
		app := newFixture(t)
		app, _ = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
		app.mail.ready = false
		budget := app.layoutBudget()
		if got := agentRailCollapseRenderedCells(t, app, 0, 0, 3); got == "[>]" {
			t.Fatalf("loading frame rendered collapsed control %q", got)
		}
		stable := agentRailCollapseStableSnapshot(app)
		app, cmd := agentRailCollapseApply(t, app, tea.MouseClickMsg(tea.Mouse{
			X: 0, Y: budget.TopChromeRows, Button: tea.MouseLeft,
		}))
		if cmd != nil {
			t.Error("loading-frame click scheduled unexpected work")
		}
		if !app.mail.agentRail.explicitlyCollapsed {
			t.Error("loading frame exposed an invisible collapsed expand target")
		}
		agentRailCollapseAssertStable(t, "loading-frame click", stable, app)
	})

	t.Run("expanded adjacent buttons bounds and non-control rows do not toggle", func(t *testing.T) {
		clicks := []struct {
			name  string
			mouse func(LayoutBudget) tea.Mouse
		}{
			{
				name: "adjacent title cell",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 19, Y: b.TopChromeRows, Button: tea.MouseLeft}
				},
			},
			{
				name: "divider cell",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 23, Y: b.TopChromeRows, Button: tea.MouseLeft}
				},
			},
			{
				name: "right button",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 20, Y: b.TopChromeRows, Button: tea.MouseRight}
				},
			},
			{
				name: "middle button",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 20, Y: b.TopChromeRows, Button: tea.MouseMiddle}
				},
			},
			{
				name: "separator row",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 20, Y: b.TopChromeRows + 1, Button: tea.MouseLeft}
				},
			},
			{
				name: "canonical Main row",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 20, Y: b.TopChromeRows + 2, Button: tea.MouseLeft}
				},
			},
			{
				name: "root chrome row",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 20, Y: b.TopChromeRows - 1, Button: tea.MouseLeft}
				},
			},
			{
				name: "negative x",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: -1, Y: b.TopChromeRows, Button: tea.MouseLeft}
				},
			},
			{
				name: "negative y",
				mouse: func(LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 20, Y: -1, Button: tea.MouseLeft}
				},
			},
			{
				name: "terminal right edge",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: b.TerminalWidth, Y: b.TopChromeRows, Button: tea.MouseLeft}
				},
			},
			{
				name: "child bottom edge",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 20, Y: b.TopChromeRows + b.ChildHeight, Button: tea.MouseLeft}
				},
			},
		}

		for _, click := range clicks {
			t.Run(click.name, func(t *testing.T) {
				app := newFixture(t)
				budget := app.layoutBudget()
				stable := agentRailCollapseStableSnapshot(app)
				var cmd tea.Cmd
				app, cmd = agentRailCollapseApply(t, app, tea.MouseClickMsg(click.mouse(budget)))
				if cmd != nil {
					t.Errorf("%s scheduled unexpected work", click.name)
				}
				if app.mail.agentRail.explicitlyCollapsed {
					t.Errorf("%s toggled the expanded rail", click.name)
				}
				if app.mail.agentRail.focused || !app.mail.input.Focused() {
					t.Errorf("%s changed focus: rail=%v composer=%v",
						click.name, app.mail.agentRail.focused, app.mail.input.Focused())
				}
				agentRailCollapseAssertGeometry(t, click.name, app, 24, 61)
				agentRailCollapseAssertStable(t, click.name, stable, app)
			})
		}
	})

	t.Run("collapsed adjacent buttons bounds and non-header rows do not toggle", func(t *testing.T) {
		clicks := []struct {
			name  string
			mouse func(LayoutBudget) tea.Mouse
		}{
			{
				name: "adjacent header cell",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 3, Y: b.TopChromeRows, Button: tea.MouseLeft}
				},
			},
			{
				name: "right button",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 0, Y: b.TopChromeRows, Button: tea.MouseRight}
				},
			},
			{
				name: "middle button",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 0, Y: b.TopChromeRows, Button: tea.MouseMiddle}
				},
			},
			{
				name: "non-header row",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 0, Y: b.TopChromeRows + 1, Button: tea.MouseLeft}
				},
			},
			{
				name: "root chrome row",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 0, Y: b.TopChromeRows - 1, Button: tea.MouseLeft}
				},
			},
			{
				name: "negative x",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: -1, Y: b.TopChromeRows, Button: tea.MouseLeft}
				},
			},
			{
				name: "negative y",
				mouse: func(LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 0, Y: -1, Button: tea.MouseLeft}
				},
			},
			{
				name: "terminal right edge",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: b.TerminalWidth, Y: b.TopChromeRows, Button: tea.MouseLeft}
				},
			},
			{
				name: "child bottom edge",
				mouse: func(b LayoutBudget) tea.Mouse {
					return tea.Mouse{X: 0, Y: b.TopChromeRows + b.ChildHeight, Button: tea.MouseLeft}
				},
			},
		}

		for _, click := range clicks {
			t.Run(click.name, func(t *testing.T) {
				app := newFixture(t)
				app, _ = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
				budget := app.layoutBudget()
				stable := agentRailCollapseStableSnapshot(app)
				var cmd tea.Cmd
				app, cmd = agentRailCollapseApply(t, app, tea.MouseClickMsg(click.mouse(budget)))
				if cmd != nil {
					t.Errorf("%s scheduled unexpected work", click.name)
				}
				if !app.mail.agentRail.explicitlyCollapsed {
					t.Errorf("%s toggled the collapsed rail", click.name)
				}
				if app.mail.agentRail.focused || !app.mail.input.Focused() {
					t.Errorf("%s changed focus: rail=%v composer=%v",
						click.name, app.mail.agentRail.focused, app.mail.input.Focused())
				}
				agentRailCollapseAssertGeometry(t, click.name, app, 0, 85)
				agentRailCollapseAssertStable(t, click.name, stable, app)
			})
		}
	})

	t.Run("expanded controls remain visible but inert behind owning surfaces", func(t *testing.T) {
		states := []struct {
			name      string
			configure func(App) App
			active    func(App) bool
		}{
			{
				name: "copy mode",
				configure: func(app App) App {
					app.mail.copyMode = true
					return app
				},
				active: func(app App) bool { return app.mail.copyMode },
			},
			{
				name: "command palette",
				configure: func(app App) App {
					app.mail.input.SetValue("/")
					return app
				},
				active: func(app App) bool { return app.mail.input.IsPaletteActive() },
			},
			{
				name: "editor warning",
				configure: func(app App) App {
					app.mail.showEditorWarn = true
					return app
				},
				active: func(app App) bool { return app.mail.showEditorWarn },
			},
			{
				name:      "/agents",
				configure: func(app App) App { app.mail = app.mail.openAgentSelector(); return app },
				active:    func(app App) bool { return app.mail.agentSelector.selectorOpen },
			},
		}

		for _, state := range states {
			t.Run(state.name, func(t *testing.T) {
				app := state.configure(newFixture(t))
				if got := agentRailCollapseRenderedCells(t, app, 0, 20, 23); got != "[<]" {
					t.Fatalf("%s obscured expanded control renders %q, want %q", state.name, got, "[<]")
				}
				budget := app.layoutBudget()
				stable := agentRailCollapseStableSnapshot(app)
				app, cmd := agentRailCollapseApply(t, app, tea.MouseClickMsg(tea.Mouse{
					X: 20, Y: budget.TopChromeRows, Button: tea.MouseLeft,
				}))
				if cmd != nil {
					t.Errorf("%s control click scheduled unexpected work", state.name)
				}
				if app.mail.agentRail.explicitlyCollapsed {
					t.Errorf("%s allowed a click behind the owning surface to collapse the rail", state.name)
				}
				if !state.active(app) {
					t.Errorf("%s control click displaced the owning surface", state.name)
				}
				agentRailCollapseAssertStable(t, state.name+" expanded control", stable, app)
			})
		}
	})

	t.Run("collapsed control remains visible but inert behind copy and palette", func(t *testing.T) {
		states := []struct {
			name      string
			configure func(App) App
			active    func(App) bool
		}{
			{
				name: "copy mode",
				configure: func(app App) App {
					app.mail.copyMode = true
					return app
				},
				active: func(app App) bool { return app.mail.copyMode },
			},
			{
				name: "command palette",
				configure: func(app App) App {
					app.mail.input.SetValue("/")
					return app
				},
				active: func(app App) bool { return app.mail.input.IsPaletteActive() },
			},
		}

		for _, state := range states {
			t.Run(state.name, func(t *testing.T) {
				app := newFixture(t)
				app, _ = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
				app = state.configure(app)
				if got := agentRailCollapseRenderedCells(t, app, 0, 0, 3); got != "[>]" {
					t.Fatalf("%s obscured collapsed control renders %q, want %q", state.name, got, "[>]")
				}
				budget := app.layoutBudget()
				stable := agentRailCollapseStableSnapshot(app)
				app, cmd := agentRailCollapseApply(t, app, tea.MouseClickMsg(tea.Mouse{
					X: 0, Y: budget.TopChromeRows, Button: tea.MouseLeft,
				}))
				if cmd != nil {
					t.Errorf("%s control click scheduled unexpected work", state.name)
				}
				if !app.mail.agentRail.explicitlyCollapsed {
					t.Errorf("%s allowed a click behind the owning surface to expand the rail", state.name)
				}
				if !state.active(app) {
					t.Errorf("%s control click displaced the owning surface", state.name)
				}
				agentRailCollapseAssertStable(t, state.name+" collapsed control", stable, app)
			})
		}
	})

	t.Run("absent ordinary header never advertises or receives a collapsed target", func(t *testing.T) {
		states := []struct {
			name               string
			explicitlyCollapse bool
			configure          func(*testing.T, App) App
			active             func(App) bool
			wantCollapsed      bool
			wantTerminalWidth  int
			wantContentWidth   int
		}{
			{
				name:               "loading",
				explicitlyCollapse: true,
				configure: func(_ *testing.T, app App) App {
					app.mail.ready = false
					return app
				},
				active:            func(app App) bool { return !app.mail.ready },
				wantCollapsed:     true,
				wantTerminalWidth: 85,
				wantContentWidth:  85,
			},
			{
				name:               "editor warning",
				explicitlyCollapse: true,
				configure: func(_ *testing.T, app App) App {
					app.mail.showEditorWarn = true
					return app
				},
				active:            func(app App) bool { return app.mail.showEditorWarn },
				wantCollapsed:     true,
				wantTerminalWidth: 85,
				wantContentWidth:  85,
			},
			{
				name:               "/agents replacement",
				explicitlyCollapse: true,
				configure:          func(_ *testing.T, app App) App { app.mail = app.mail.openAgentSelector(); return app },
				active:             func(app App) bool { return app.mail.agentSelector.selectorOpen },
				wantCollapsed:      true,
				wantTerminalWidth:  85,
				wantContentWidth:   85,
			},
			{
				name:               "automatic narrow hiding",
				explicitlyCollapse: false,
				configure: func(t *testing.T, app App) App {
					app, _ = agentRailCollapseApply(t, app, tea.WindowSizeMsg{Width: 84, Height: 14})
					return app
				},
				active:            func(app App) bool { return app.width == 84 && !app.layoutBudget().RailVisible },
				wantCollapsed:     false,
				wantTerminalWidth: 84,
				wantContentWidth:  84,
			},
			{
				name:               "explicit narrow hiding",
				explicitlyCollapse: true,
				configure: func(t *testing.T, app App) App {
					app, _ = agentRailCollapseApply(t, app, tea.WindowSizeMsg{Width: 84, Height: 14})
					return app
				},
				active:            func(app App) bool { return app.width == 84 && !app.layoutBudget().RailVisible },
				wantCollapsed:     true,
				wantTerminalWidth: 84,
				wantContentWidth:  84,
			},
			{
				name:               "non-Mail frame",
				explicitlyCollapse: true,
				configure: func(_ *testing.T, app App) App {
					app.currentView = appViewHelp
					app.help = NewHelpModel()
					return app
				},
				active:            func(app App) bool { return app.currentView == appViewHelp },
				wantCollapsed:     true,
				wantTerminalWidth: 85,
				wantContentWidth:  85,
			},
		}

		for _, state := range states {
			t.Run(state.name, func(t *testing.T) {
				app := newFixture(t)
				if state.explicitlyCollapse {
					app, _ = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
				}
				app = state.configure(t, app)
				budget := app.layoutBudget()
				if got := agentRailCollapseRenderedCells(t, app, 0, 0, 3); got == "[>]" {
					t.Fatalf("%s rendered a collapsed control without the ordinary Mail header", state.name)
				}
				stable := agentRailCollapseStableSnapshot(app)
				var cmd tea.Cmd
				app, cmd = agentRailCollapseApply(t, app, tea.MouseClickMsg(tea.Mouse{
					X: 0, Y: budget.TopChromeRows, Button: tea.MouseLeft,
				}))
				if cmd != nil {
					t.Errorf("%s hidden-target click scheduled unexpected work", state.name)
				}
				if app.mail.agentRail.explicitlyCollapsed != state.wantCollapsed {
					t.Errorf("%s exposed a hidden target: collapsed=%v want=%v",
						state.name, app.mail.agentRail.explicitlyCollapsed, state.wantCollapsed)
				}
				if !state.active(app) {
					t.Errorf("%s hidden-target click displaced the owning frame", state.name)
				}
				if got := agentRailCollapseRenderedCells(t, app, 0, 0, 3); got == "[>]" {
					t.Errorf("%s advertised a collapsed control after the hidden-target click", state.name)
				}
				if got := app.layoutBudget(); got.TerminalWidth != state.wantTerminalWidth ||
					got.ContentWidth != state.wantContentWidth || got.RailVisible {
					t.Errorf("%s geometry after click = terminal/content/rail-visible %d/%d/%v, want %d/%d/false",
						state.name,
						got.TerminalWidth,
						got.ContentWidth,
						got.RailVisible,
						state.wantTerminalWidth,
						state.wantContentWidth)
				}
				agentRailCollapseAssertStable(t, state.name+" hidden target", stable, app)
			})
		}
	})
}

func TestAgentRailVisibleControlRendering(t *testing.T) {
	fixture := newVisibleRailV2Fixture(
		t,
		[]string{"Alpha", "Bravo"},
		[]int{0, 0},
		85,
		14,
		"ROOT",
	)
	app := fixture.app
	if len(app.mail.agentRail.unreadByThread) != 0 {
		t.Fatalf("zero-unread fixture has badges: %#v", app.mail.agentRail.unreadByThread)
	}

	expandedLine := agentRailCollapseRenderedLine(t, app, 0)
	if got := ansi.Strip(ansi.Cut(expandedLine, 20, 23)); got != "[<]" {
		t.Errorf("expanded control cells 20..22 = %q, want %q", got, "[<]")
	}
	if !strings.Contains(expandedLine, StyleSubtle.Render("[<]")) {
		t.Errorf("ordinary expanded control does not use the exact subtle span: %q", expandedLine)
	}
	if got := ansi.Strip(ansi.Cut(expandedLine, 23, 24)); got != "│" {
		t.Errorf("expanded divider cell 23 = %q, want %q", got, "│")
	}

	app = visibleRailV2Focus(t, app)
	focusedLine := agentRailCollapseRenderedLine(t, app, 0)
	if !strings.Contains(focusedLine, StyleAccent.Render("[<]")) {
		t.Errorf("focused expanded control does not use the exact accent span: %q", focusedLine)
	}
	if got := ansi.Strip(ansi.Cut(focusedLine, 20, 23)); got != "[<]" {
		t.Errorf("focused expanded control moved from cells 20..22: %q", got)
	}

	app, _ = agentRailCollapseApply(t, app, tea.KeyPressMsg{Code: tea.KeyF2})
	collapsedLine := agentRailCollapseRenderedLine(t, app, 0)
	if got := ansi.Strip(ansi.Cut(collapsedLine, 0, 3)); got != "[>]" {
		t.Errorf("collapsed control cells 0..2 = %q, want %q", got, "[>]")
	}
	if !strings.Contains(collapsedLine, StyleSubtle.Render("[>]")) {
		t.Errorf("collapsed ordinary-header control does not use the exact subtle span: %q", collapsedLine)
	}
	if got := ansi.Strip(ansi.Cut(app.mail.View(), 0, 3)); got == "[>]" {
		t.Errorf("public MailModel.View compatibility wrapper unexpectedly advertised %q", got)
	}
	if app.mail.agentRail.focused || !app.mail.input.Focused() {
		t.Errorf("rendering fixture ended with wrong collapse focus: rail=%v composer=%v",
			app.mail.agentRail.focused, app.mail.input.Focused())
	}
	if len(app.mail.agentRail.unreadByThread) != 0 {
		t.Errorf("visible-control rendering introduced unread badges: %#v", app.mail.agentRail.unreadByThread)
	}
}
