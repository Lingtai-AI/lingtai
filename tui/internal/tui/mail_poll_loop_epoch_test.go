package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/anthropics/lingtai-tui/internal/fs"
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
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "", 20, t.TempDir(), "en", 0)
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
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "", 20, t.TempDir(), "en", 0)
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

	var catchup tea.Cmd
	m, catchup = m.Update(periodic())
	if catchup == nil || !m.mailRefreshInFlight || m.mailRefreshInFlightSerial != 3 {
		t.Fatalf("owning periodic completion did not launch the coalesced post-initial catch-up: cmd:%v inFlight:%v owner:%d", catchup != nil, m.mailRefreshInFlight, m.mailRefreshInFlightSerial)
	}
	m, _ = m.Update(mailPollRefreshFromCmd(t, catchup))
	if m.mailRefreshInFlight || m.mailRefreshInFlightSerial != 0 {
		t.Fatalf("post-initial catch-up did not release lane: inFlight:%v owner:%d", m.mailRefreshInFlight, m.mailRefreshInFlightSerial)
	}
}

func TestMailPeriodicFirstInitialSessionStillLaunchesCatchup(t *testing.T) {
	const (
		initialBody  = "present in initial serial 1"
		periodicBody = "present in periodic serial 2"
		lateBody     = "published after periodic serial 2 scan"
	)

	root := t.TempDir()
	humanDir := filepath.Join(root, "human")
	orchDir := filepath.Join(root, "agent")
	writeMailboxProjectionMessage(t, humanDir, "inbox", "20260911T110000-0001", fs.MailMessage{
		From:       "agent",
		To:         []string{"human"},
		Message:    initialBody,
		ReceivedAt: "2026-09-11T11:00:00Z",
	})

	m := NewMailModel(humanDir, "human", root, orchDir, "agent", 200, "", "en", false, 0)
	initialSnapshotReady := make(chan struct{})
	releaseInitial := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(releaseInitial)
		}
	}()
	m.afterInitialMailRefresh = func() {
		close(initialSnapshotReady)
		<-releaseInitial
	}
	initialCmd := m.initialRebuild
	type initialResult struct {
		msg mailRefreshMsg
		ok  bool
	}
	initialResults := make(chan initialResult, 1)
	go func() {
		msg, ok := initialCmd().(mailRefreshMsg)
		initialResults <- initialResult{msg: msg, ok: ok}
	}()

	select {
	case <-initialSnapshotReady:
	case <-time.After(5 * time.Second):
		t.Fatal("initial serial 1 did not reach its post-mailbox-snapshot hold")
	}

	// Advance the mailbox after serial 1's snapshot, then prepare and accept
	// periodic serial 2 while the initial session reconstruction remains held.
	writeMailboxProjectionMessage(t, humanDir, "inbox", "20260911T110001-0002", fs.MailMessage{
		From:       "agent",
		To:         []string{"human"},
		Message:    periodicBody,
		ReceivedAt: "2026-09-11T11:00:01Z",
	})
	var periodicCmd tea.Cmd
	m, periodicCmd = m.issueRefreshRequest()
	periodic := mailPollRefreshFromCmd(t, periodicCmd)
	if periodic.refreshRequestSerial != 2 || !periodic.prepared {
		t.Fatalf("periodic completion = prepared:%v serial:%d, want prepared serial 2", periodic.prepared, periodic.refreshRequestSerial)
	}
	m, _ = m.Update(periodic)
	if m.acceptedRefreshRequestSerial != periodic.refreshRequestSerial || !m.initialLoading {
		t.Fatalf("periodic-first acceptance = watermark:%d loading:%v, want 2/true", m.acceptedRefreshRequestSerial, m.initialLoading)
	}
	acceptedPeriodicPublication := m.directPublication

	// This mail is newer than serial 2's completed scan. Only the immediate
	// post-initial ordinary catch-up can observe it in this deterministic trace.
	writeMailboxProjectionMessage(t, humanDir, "inbox", "20260911T110002-0003", fs.MailMessage{
		From:       "agent",
		To:         []string{"human"},
		Message:    lateBody,
		ReceivedAt: "2026-09-11T11:00:02Z",
	})
	close(releaseInitial)
	released = true

	var initial mailRefreshMsg
	select {
	case result := <-initialResults:
		if !result.ok {
			t.Fatal("initialRebuild did not return mailRefreshMsg")
		}
		initial = result.msg
	case <-time.After(5 * time.Second):
		t.Fatal("initial serial 1 did not complete after release")
	}
	if initial.refreshRequestSerial != 1 || !initial.initial || initial.sessionCache == nil {
		t.Fatalf("initial completion = serial:%d initial:%v session:%v, want serial 1 current initial session", initial.refreshRequestSerial, initial.initial, initial.sessionCache != nil)
	}

	m, catchupCmd := m.Update(initial)
	if m.initialLoading || m.sessionCache != initial.sessionCache {
		t.Fatalf("stale-serial initial session transition = loading:%v installed:%v, want false/true", m.initialLoading, m.sessionCache == initial.sessionCache)
	}
	if m.acceptedRefreshRequestSerial != periodic.refreshRequestSerial {
		t.Fatalf("stale initial advanced accepted watermark to %d, want %d", m.acceptedRefreshRequestSerial, periodic.refreshRequestSerial)
	}
	if m.directPublication != acceptedPeriodicPublication || m.directPublication == initial.directPublication {
		t.Fatal("stale serial-1 direct payload replaced the accepted serial-2 publication")
	}
	cacheCount := func(body string) int {
		count := 0
		for _, msg := range m.cache.Messages {
			if msg.Message == body {
				count++
			}
		}
		return count
	}
	if gotInitial, gotPeriodic, gotLate := cacheCount(initialBody), cacheCount(periodicBody), cacheCount(lateBody); gotInitial != 1 || gotPeriodic != 1 || gotLate != 0 {
		t.Fatalf("cache after stale initial = initial:%d periodic:%d late:%d, want 1/1/0", gotInitial, gotPeriodic, gotLate)
	}

	catchup := mailPollRefreshFromCmd(t, catchupCmd)
	if !catchup.prepared || catchup.refreshRequestSerial <= periodic.refreshRequestSerial {
		t.Fatalf("post-initial catch-up = prepared:%v serial:%d, want newer than periodic %d", catchup.prepared, catchup.refreshRequestSerial, periodic.refreshRequestSerial)
	}
	lateInCatchup := 0
	for _, msg := range catchup.cache.Messages {
		if msg.Message == lateBody {
			lateInCatchup++
		}
	}
	if lateInCatchup != 1 {
		t.Fatalf("newer catch-up contains late mail %d times, want exactly once", lateInCatchup)
	}

	m, _ = m.Update(catchup)
	messageCount := func(body string) int {
		count := 0
		for _, msg := range m.messages {
			if msg.Type == "mail" && msg.Body == body {
				count++
			}
		}
		return count
	}
	if gotInitial, gotPeriodic, gotLate := messageCount(initialBody), messageCount(periodicBody), messageCount(lateBody); gotInitial != 1 || gotPeriodic != 1 || gotLate != 1 {
		t.Fatalf("final projection = initial:%d periodic:%d late:%d, want each exactly once", gotInitial, gotPeriodic, gotLate)
	}
	if m.mailRefreshInFlight || m.mailRefreshInFlightSerial != 0 {
		t.Fatalf("catch-up did not release its lane: inFlight:%v owner:%d", m.mailRefreshInFlight, m.mailRefreshInFlightSerial)
	}
}

func TestNetworkActivityLaneUsesInFlightGateAndTTL(t *testing.T) {
	m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "", 20, t.TempDir(), "en", 0)
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
