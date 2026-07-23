package tui

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

// bindMailModelForAsyncTest gives MailModel-focused fixtures a complete current
// binding without weakening production acceptance. Tests whose historical base
// and target directories were independent use the target's parent as the
// synthetic canonical project owner.
func bindMailModelForAsyncTest(t *testing.T, m *MailModel, generation uint64) ProjectMailStore {
	t.Helper()
	if generation == 0 {
		generation = 1
	}
	m.generation = generation
	if m.orchestrator == "" {
		m.orchestrator = filepath.Join(t.TempDir(), "Main")
	}
	if m.orchAddr == "" {
		m.orchAddr = "mail-test-address"
	}
	projectDir := m.baseDir
	probe := asyncOwner{projectID: canonicalProjectMailIdentity(projectDir), storeID: 1, activation: 1}
	target := asyncTarget{directory: inventory.NormalizePath(m.orchestrator), addressFingerprint: "invalid"}
	if projectDir == "" || !validAsyncTarget(probe, asyncTarget{
		directory:          target.directory,
		addressFingerprint: fs.AddressFingerprint(m.orchAddr),
	}) {
		projectDir = filepath.Dir(m.orchestrator)
	}
	store := newProjectMailStore(projectDir, m.humanDir)
	store.bindMailModel(m, false)
	return store
}

func bindExistingStoreMailForAsyncTest(t *testing.T, store *ProjectMailStore, m *MailModel, generation uint64) {
	t.Helper()
	if generation == 0 {
		generation = 1
	}
	m.generation = generation
	if m.orchestrator == "" || inventory.NormalizePath(m.orchestrator) == store.projectID {
		m.orchestrator = filepath.Join(store.projectID, "Main")
	}
	if m.orchAddr == "" {
		m.orchAddr = "mail-test-address"
	}
	store.bindMailModel(m, false)
}

// acceptedInitialMailRefresh drives the real outer App.Update acceptance and
// then returns the already-accepted payload for MailModel-focused tests. The
// helper restores the pre-payload model with its accepted store version so the
// test can exercise the payload transition itself exactly once.
func acceptedInitialMailRefresh(t *testing.T, m *MailModel) tea.Msg {
	return acceptedMailRefresh(t, m, true)
}

func acceptedSteadyMailRefresh(t *testing.T, m *MailModel) tea.Msg {
	return acceptedMailRefresh(t, m, false)
}

func acceptedMailRefresh(t *testing.T, m *MailModel, initial bool) tea.Msg {
	t.Helper()
	generation := m.generation
	store := bindMailModelForAsyncTest(t, m, generation)
	bound := *m
	cmd := store.beginRefresh(bound, initial)
	if cmd == nil {
		t.Fatal("test fixture could not launch project-mail refresh")
	}
	msg := cmd().(projectMailRefreshMsg)
	app := App{currentView: appViewMail, mail: bound, mailStore: store}
	model, _ := app.Update(msg)
	updated := model.(App)
	if updated.mail.acceptedSnapshot == nil {
		t.Fatal("test fixture refresh did not pass real App.Update acceptance")
	}
	payload := msg.mail
	payload.snapshot = updated.mail.acceptedSnapshot
	bound.asyncStoreVersion = updated.mailStore.version
	*m = bound
	return payload
}

func acceptStoreRefreshForTest(store *ProjectMailStore, msg projectMailRefreshMsg) (*ProjectMailSnapshot, bool, bool) {
	settled := store.settleRefreshWork(msg.envelope)
	if !acceptAsync(store.asyncCurrent(), msg.envelope) {
		return nil, false, settled
	}
	if !settled {
		return nil, false, false
	}
	return store.installRefresh(msg), true, true
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
