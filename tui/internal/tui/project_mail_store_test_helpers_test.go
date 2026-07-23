package tui

import tea "charm.land/bubbletea/v2"

// acceptedInitialMailRefresh runs the root store pipeline synchronously for
// MailModel-focused tests, accepts its snapshot, and returns the target payload
// that production App.Update would route to MailModel.Update.
func acceptedInitialMailRefresh(m MailModel) tea.Msg {
	store := newProjectMailStore(m.baseDir, m.humanDir)
	cmd := store.beginRefresh(m, true)
	msg := cmd().(projectMailRefreshMsg)
	snapshot, _, _ := store.acceptRefresh(msg, m.generation)
	msg.mail.snapshot = snapshot
	return msg.mail
}

func acceptedSteadyMailRefresh(m MailModel) tea.Msg {
	store := newProjectMailStore(m.baseDir, m.humanDir)
	cmd := store.beginRefresh(m, false)
	msg := cmd().(projectMailRefreshMsg)
	snapshot, _, _ := store.acceptRefresh(msg, m.generation)
	msg.mail.snapshot = snapshot
	return msg.mail
}

func detachedAppProjectMailRefresh(a *App, initial bool) projectMailRefreshMsg {
	return a.beginProjectMailRefresh(initial)().(projectMailRefreshMsg)
}

func findProjectMailRefresh(cmd tea.Cmd) (projectMailRefreshMsg, bool) {
	if cmd == nil {
		return projectMailRefreshMsg{}, false
	}
	msg := cmd()
	if refresh, ok := msg.(projectMailRefreshMsg); ok {
		return refresh, true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			if refresh, ok := findProjectMailRefresh(child); ok {
				return refresh, true
			}
		}
	}
	return projectMailRefreshMsg{}, false
}
