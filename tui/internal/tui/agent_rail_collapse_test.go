package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

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
