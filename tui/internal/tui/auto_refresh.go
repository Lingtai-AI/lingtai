package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Auto-refresh: a single, app-level 1s tick retained for views that may opt in.
// Kanban is deliberately manual-only and never returns a command here.
//
// Design notes:
//   - One ticker lives on the App, not per-view. The App routes the tick to
//     whichever view is current. Views never start their own auto tick (mail
//     is the historical exception and keeps its own loop).
//   - Kanban refreshes only on entry, selected-agent change, or Ctrl+R.

// autoRefreshInterval is the cadence for the app-level auto-refresh tick. It
// matches the mail view's poll rate (mail.go: pollRate = 1s) so all reloadable
// views feel equally live.
const autoRefreshInterval = 1 * time.Second

// autoRefreshTickMsg is delivered every autoRefreshInterval while auto refresh
// is enabled. It is distinct from mail's tickMsg so the two loops never alias.
type autoRefreshTickMsg time.Time

// autoRefreshTick schedules the next auto-refresh tick.
func autoRefreshTick() tea.Cmd {
	return tea.Every(autoRefreshInterval, func(t time.Time) tea.Msg { return autoRefreshTickMsg(t) })
}

func autoRefreshCtrlRMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
}

// autoRefreshActiveView currently has no opted-in root view. In particular,
// /kanban must not acquire a filesystem command from the one-second tick.
func (a App) autoRefreshActiveView() (App, tea.Cmd) {
	return a, nil
}
