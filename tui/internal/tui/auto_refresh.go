package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Auto-refresh: a single, app-level 1s tick that periodically asks the active
// non-mail view to reload its own on-disk data. Project mailbox refresh is NOT
// part of this loop: ProjectMailStore is the sole mailbox cache/scan/tick owner.
// Do not add mail refresh routing here or a second mailbox polling chain will
// exist beside project_mail_store.go.
//
// Design notes:
//   - One view-refresh ticker lives on the App, not per-view. It is independent
//     from the root-owned ProjectMailStore ticker and must never scan mail.
//   - Each participating view reuses the same reload path as Ctrl+R. Views opt
//     out for an individual tick when reloading would interrupt the user
//     (picker open, drill-in/detail/editor open, or in-flight doctor run).
//   - Ctrl+R remains the manual fallback and is unchanged; the tick simply
//     calls the same reload path on a timer.

// autoRefreshInterval is the cadence for the app-level auto-refresh tick. It
// matches the ProjectMailStore poll rate so all reloadable
// views feel equally live.
const autoRefreshInterval = 1 * time.Second

// autoRefreshTickMsg is delivered every autoRefreshInterval while auto refresh
// is enabled. It is distinct from projectMailTickMsg so the loops never alias.
type autoRefreshTickMsg time.Time

// autoRefreshTick schedules the next auto-refresh tick.
func autoRefreshTick() tea.Cmd {
	return tea.Every(autoRefreshInterval, func(t time.Time) tea.Msg { return autoRefreshTickMsg(t) })
}

func autoRefreshCtrlRMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
}

// autoRefreshActiveView reloads only the kanban/props view. Other Ctrl+R-
// reloadable screens (/mailbox, /system, /daemons, markdown viewers, pickers,
// and editors) are intentionally excluded from the 1s auto-refresh loop because
// reloading them resets or disrupts keyboard selection/scroll state. They keep
// Ctrl+R as the explicit manual refresh path.
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
	}
	return a, nil
}
