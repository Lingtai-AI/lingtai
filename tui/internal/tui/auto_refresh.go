package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Auto-refresh: a single, app-level 1s tick that periodically asks the active
// view to reload its on-disk data. This generalizes the mail view's existing
// poll loop (mail.go: tickEvery/tickMsg) to the other reloadable views so the
// human sees fresh network/agent state without pressing Ctrl+R.
//
// Design notes:
//   - One ticker lives on the App, not per-view. The App routes the tick to
//     whichever view is current. Views never start their own auto tick (mail
//     is the historical exception and keeps its own loop).
//   - Each participating view reuses the same reload path as Ctrl+R. Views opt
//     out for an individual tick when reloading would interrupt the user
//     (picker open, drill-in/detail/editor open, or in-flight doctor run).
//   - Ctrl+R remains the manual fallback and is unchanged; the tick simply
//     calls the same reload path on a timer.

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

// autoRefreshActiveView reloads the kanban/props view and the knowledge/library
// markdown viewers. Other Ctrl+R-reloadable screens (/mailbox, /system, /daemons,
// editors) are intentionally excluded from the 1s auto-refresh loop because
// reloading them resets or disrupts keyboard selection/scroll state. They keep
// Ctrl+R as the explicit manual refresh path.
//
// The knowledge and library views use a change-aware reload (mtime check) so
// the catalog only rebuilds when the underlying directory actually changed —
// avoiding unnecessary scroll/selection resets on every tick.
func (a App) autoRefreshActiveView() (App, tea.Cmd) {
	switch a.currentView {
	case appViewProps:
		// Keep the dashboard live, but do not interrupt the agent picker. The
		// Ctrl+D detail layer is also live: refresh its derived breakdowns in
		// place before scheduling the same outer dashboard reload Ctrl+R uses.
		if a.props.pickerOpen {
			return a, nil
		}
		if a.props.detailOpen {
			a.props.loadDetail()
			a.props.syncViewportContent()
		}
		return a, a.props.AutoReloadCmd()
	case appViewCodex:
		// /knowledge: reload only when the knowledge/ directory changed on disk.
		var cmd tea.Cmd
		a.codex, cmd = a.codex.reloadIfChanged()
		return a, cmd
	case appViewLibrary:
		// /skills: reload only when the skills directories changed on disk.
		var cmd tea.Cmd
		a.library, cmd = a.library.reloadIfChanged()
		return a, cmd
	}
	return a, nil
}
