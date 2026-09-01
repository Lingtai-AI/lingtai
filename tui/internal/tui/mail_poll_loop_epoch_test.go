package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func mailPollRefreshFromCmd(t *testing.T, cmd tea.Cmd) mailRefreshMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("refresh command is nil")
	}
	msg := cmd()
	if refresh, ok := msg.(mailRefreshMsg); ok {
		return refresh
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, nested := range batch {
			if nested == nil {
				continue
			}
			if refresh, ok := nested().(mailRefreshMsg); ok {
				return refresh
			}
		}
	}
	t.Fatalf("refresh command produced %T without mailRefreshMsg", msg)
	return mailRefreshMsg{}
}

func newPollEpochTestApp(t *testing.T) App {
	t.Helper()
	globalDir := t.TempDir()
	app := App{
		currentView: appViewHelp,
		globalDir:   globalDir,
	}
	app.installMailModel(NewMailModel(
		t.TempDir(),
		"human",
		t.TempDir(),
		"",
		"",
		200,
		globalDir,
		"en",
		false,
		0,
	))
	return app
}

func TestMailPollLoopEpochRejectsOldTickAfterSameGenerationReturn(t *testing.T) {
	app := newPollEpochTestApp(t)
	oldTick := tickMsg{
		generation: app.mail.generation,
		pollEpoch:  app.mail.pollEpoch,
	}

	returnedModel, _ := app.Update(MarkdownViewerCloseMsg{})
	returned := returnedModel.(App)
	if returned.mail.generation != oldTick.generation {
		t.Fatalf("same-generation return changed Mail generation from %d to %d", oldTick.generation, returned.mail.generation)
	}
	if returned.mail.pollEpoch == oldTick.pollEpoch {
		t.Fatalf("same-generation return kept stale poll epoch %d", oldTick.pollEpoch)
	}
	serialAfterReturn := returned.mail.refreshRequestSerial

	updatedModel, cmd := returned.Update(oldTick)
	updated := updatedModel.(App)
	if updated.mail.refreshRequestSerial != serialAfterReturn {
		t.Fatalf(
			"old tick advanced refresh serial after same-generation return: got %d, want %d",
			updated.mail.refreshRequestSerial,
			serialAfterReturn,
		)
	}
	if cmd != nil {
		t.Fatal("old tick returned a refresh/rearm command after same-generation return")
	}

	// The return command has not been executed in this white-box test. Model its
	// prepared completion before checking the next live tick's epoch behavior.
	returned.mail.mailRefreshInFlight = false
	liveEpoch := returned.mail.pollEpoch
	updatedModel, cmd = returned.Update(tickMsg{
		generation: returned.mail.generation,
		pollEpoch:  liveEpoch,
	})
	updated = updatedModel.(App)
	if updated.mail.refreshRequestSerial != serialAfterReturn+1 {
		t.Fatalf(
			"live tick advanced refresh serial by %d, want exactly 1",
			updated.mail.refreshRequestSerial-serialAfterReturn,
		)
	}
	if cmd == nil {
		t.Fatal("live tick did not return its refresh/rearm command")
	}
	if updated.mail.pollEpoch != liveEpoch {
		t.Fatalf("live tick changed poll epoch from %d to %d", liveEpoch, updated.mail.pollEpoch)
	}
}

func TestMailRefreshSingleFlightCoalescesOverlappingRequests(t *testing.T) {
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "", 20, t.TempDir(), "en", false, 0)
	m.generation = 7

	m, first := m.issueRefreshRequest()
	if first == nil || !m.mailRefreshInFlight {
		t.Fatal("first refresh request did not start the single-flight lane")
	}
	serial := m.refreshRequestSerial
	m, overlapping := m.issueRefreshRequest()
	if overlapping != nil {
		t.Fatal("overlapping refresh request launched a second filesystem command")
	}
	if m.refreshRequestSerial != serial {
		t.Fatalf("coalesced request advanced serial from %d to %d", serial, m.refreshRequestSerial)
	}
	if !m.mailRefreshPending {
		t.Fatal("coalesced request was not retained as one pending demand")
	}

	completed := first()
	var next tea.Cmd
	m, next = m.Update(completed)
	if next == nil || !m.mailRefreshInFlight || m.mailRefreshInFlightSerial != serial+1 || m.mailRefreshPending {
		t.Fatalf("owner completion did not launch exactly one follow-up: cmd:%v inFlight:%v owner:%d pending:%v", next != nil, m.mailRefreshInFlight, m.mailRefreshInFlightSerial, m.mailRefreshPending)
	}
	m, _ = m.Update(mailPollRefreshFromCmd(t, next))
	if m.mailRefreshInFlight || m.mailRefreshInFlightSerial != 0 {
		t.Fatalf("follow-up completion did not release lane: inFlight:%v owner:%d", m.mailRefreshInFlight, m.mailRefreshInFlightSerial)
	}
}

func TestMailInitialCompletionCannotReleaseNewerPeriodicLane(t *testing.T) {
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "", 20, t.TempDir(), "en", false, 0)
	m.generation = 8

	// Capture the one-shot initial command at serial 1, then let the first tick
	// launch periodic serial 2 while that startup work is still running.
	initial := m.initialRebuild
	m, periodic := m.issueRefreshRequest()
	if periodic == nil || m.mailRefreshInFlightSerial != 2 {
		t.Fatalf("periodic lane = cmd:%v owner:%d, want command owned by serial 2", periodic != nil, m.mailRefreshInFlightSerial)
	}

	m, _ = m.Update(initial())
	if !m.mailRefreshInFlight || m.mailRefreshInFlightSerial != 2 {
		t.Fatalf("older initial completion released newer lane: inFlight:%v owner:%d", m.mailRefreshInFlight, m.mailRefreshInFlightSerial)
	}

	m, _ = m.Update(periodic())
	if m.mailRefreshInFlight || m.mailRefreshInFlightSerial != 0 {
		t.Fatalf("owning periodic completion did not release lane: inFlight:%v owner:%d", m.mailRefreshInFlight, m.mailRefreshInFlightSerial)
	}
}

func TestNetworkActivityLaneUsesInFlightGateAndTTL(t *testing.T) {
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "", 20, t.TempDir(), "en", false, 0)
	m.generation = 9
	now := time.Now()
	first := m.maybeScheduleNetworkActivity(now)
	if first == nil || !m.networkActivityInFlight {
		t.Fatal("first network-activity request did not start")
	}
	if overlapping := m.maybeScheduleNetworkActivity(now); overlapping != nil {
		t.Fatal("network-activity lane launched overlapping filesystem work")
	}

	m, _ = m.Update(first())
	if m.networkActivityInFlight || m.networkActivityLastFetch.IsZero() {
		t.Fatalf("completion state = inFlight:%v lastFetch:%v", m.networkActivityInFlight, m.networkActivityLastFetch)
	}
	if withinTTL := m.maybeScheduleNetworkActivity(m.networkActivityLastFetch.Add(networkActivityTTL - time.Nanosecond)); withinTTL != nil {
		t.Fatal("network-activity lane ignored its TTL floor")
	}
	if afterTTL := m.maybeScheduleNetworkActivity(m.networkActivityLastFetch.Add(networkActivityTTL)); afterTTL == nil {
		t.Fatal("network-activity lane did not refresh at the TTL boundary")
	}
}

func TestMailPollLoopEpochRejectsOldPulseAfterSameGenerationReturn(t *testing.T) {
	app := newPollEpochTestApp(t)
	app.mail.orchState = "ACTIVE"
	oldPulse := pulseTickMsg{
		generation: app.mail.generation,
		pollEpoch:  app.mail.pollEpoch,
	}

	returnedModel, _ := app.Update(MarkdownViewerCloseMsg{})
	returned := returnedModel.(App)
	pulseBefore := returned.mail.pulseTick

	updatedModel, cmd := returned.Update(oldPulse)
	updated := updatedModel.(App)
	if updated.mail.pulseTick != pulseBefore {
		t.Fatalf("old pulse advanced animation from %d to %d", pulseBefore, updated.mail.pulseTick)
	}
	if cmd != nil {
		t.Fatal("old pulse returned a rearm command after same-generation return")
	}

	liveEpoch := returned.mail.pollEpoch
	updatedModel, cmd = returned.Update(pulseTickMsg{
		generation: returned.mail.generation,
		pollEpoch:  liveEpoch,
	})
	updated = updatedModel.(App)
	if updated.mail.pulseTick != pulseBefore+1 {
		t.Fatalf("live pulse advanced animation by %d, want exactly 1", updated.mail.pulseTick-pulseBefore)
	}
	if cmd == nil {
		t.Fatal("live pulse did not return its rearm command")
	}
	if updated.mail.pollEpoch != liveEpoch {
		t.Fatalf("live pulse changed poll epoch from %d to %d", liveEpoch, updated.mail.pollEpoch)
	}
}

func TestMailPollLoopEpochAdvancesAtEveryLoopStart(t *testing.T) {
	app := newPollEpochTestApp(t)
	if app.mail.pollEpoch == 0 {
		t.Fatal("initial Mail installation did not establish a poll epoch")
	}

	beforeSwitch := app.mail.pollEpoch
	switchedModel, _ := app.switchToView("mail")
	switched := switchedModel.(App)
	if switched.mail.generation != app.mail.generation {
		t.Fatalf("ordinary Mail return changed generation from %d to %d", app.mail.generation, switched.mail.generation)
	}
	if switched.mail.pollEpoch == beforeSwitch {
		t.Fatalf("ordinary Mail return kept poll epoch %d", beforeSwitch)
	}

	beforeResumeGeneration := switched.mail.generation
	beforeResumeEpoch := switched.mail.pollEpoch
	resumeCmd := switched.resumeMailModel(switched.mail)
	if resumeCmd == nil {
		t.Fatal("restored Mail model did not schedule refresh and poll commands")
	}
	if switched.mail.generation == beforeResumeGeneration {
		t.Fatalf("restored Mail model kept generation %d", beforeResumeGeneration)
	}
	if switched.mail.pollEpoch == beforeResumeEpoch {
		t.Fatalf("restored Mail model kept poll epoch %d", beforeResumeEpoch)
	}
}
