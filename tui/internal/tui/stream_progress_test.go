package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/streamprogress"
)

// fakeStreamFetcher is the in-memory stand-in for streamprogress.Client: it
// records every requested agent id, returns a configurable snapshot, and can
// be armed to panic so a test proves the render path never touches it.
type fakeStreamFetcher struct {
	calls       int
	agentIDs    []string
	snapshot    streamprogress.Snapshot
	ok          bool
	panicOnCall bool
}

func (f *fakeStreamFetcher) Fetch(agentID string) (streamprogress.Snapshot, bool) {
	if f.panicOnCall {
		panic("stream-progress Fetch invoked on the render path")
	}
	f.calls++
	f.agentIDs = append(f.agentIDs, agentID)
	snap := f.snapshot
	snap.AgentID = agentID
	return snap, f.ok
}

func activeStreamSnapshot(chars int64) streamprogress.Snapshot {
	return streamprogress.Snapshot{
		Schema:        streamprogress.Schema,
		Generation:    1,
		Active:        true,
		StreamedChars: chars,
		UpdatedUnixMS: time.Now().UnixMilli(),
		PID:           1,
	}
}

func charsSuffix(chars string) string {
	return i18n.TF("mail.stream_progress_chars", chars)
}

func delaySuffix(delay float64) string {
	return i18n.TF("mail.stream_progress_delay", delay)
}

// newStreamMail builds a sized Main Mail model with a known orchestrator
// identity and the fake fetcher installed.
func newStreamMail(t *testing.T, fetcher *fakeStreamFetcher, width int) MailModel {
	t.Helper()
	dir := t.TempDir()
	m := NewMailModel(dir, "human", dir, dir, "orch", 20, dir, "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
	if !m.ready {
		t.Fatal("MailModel did not become ready")
	}
	m.orchAgentID = "orch-agent-id"
	m.streamProgressFetcher = fetcher
	return m
}

func refreshState(t *testing.T, m MailModel, state string) MailModel {
	t.Helper()
	m, _ = m.Update(mailRefreshMsg{generation: m.generation, state: state, alive: true})
	return m
}

// pollStream runs one poll-tick schedule + async landing synchronously.
func pollStream(t *testing.T, m MailModel, now time.Time) MailModel {
	t.Helper()
	cmd := m.maybeScheduleStreamProgress(now)
	if cmd == nil {
		return m
	}
	m, _ = m.Update(runCmd(cmd))
	return m
}

func TestStreamProgressSuffixRendersBesideActiveElapsed(t *testing.T) {
	now := time.UnixMilli(1756200000100)
	snapshot := activeStreamSnapshot(4099)
	snapshot.UpdatedUnixMS = now.Add(-100 * time.Millisecond).UnixMilli()
	fetcher := &fakeStreamFetcher{snapshot: snapshot, ok: true}
	m := newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "active")
	m = pollStream(t, m, now)

	footer := emailToLine(m.View())
	if footer == "" {
		t.Fatal("no Email To: footer line")
	}
	label := i18n.T("state.active")
	chars := charsSuffix("4.1k")
	if !strings.Contains(footer, label) || !strings.Contains(footer, chars) {
		t.Fatalf("footer should carry %q and %q; got %q", label, chars, footer)
	}
	// "active Ns · N chars · delay 0.1s": the suffix follows elapsed seconds.
	if strings.Index(footer, label) > strings.Index(footer, chars) {
		t.Fatalf("suffix must render after the state label: %q", footer)
	}
	elapsedThenSuffix := strings.SplitN(footer[strings.Index(footer, label):], chars, 2)[0]
	if !strings.HasSuffix(strings.TrimSpace(elapsedThenSuffix), "s ·") {
		t.Fatalf("expected `active Ns ·` immediately before the suffix; got %q", elapsedThenSuffix)
	}
	want := " · " + charsSuffix("4.1k") + " · " + delaySuffix(0.1)
	if got := m.streamProgressSuffixAt("active", now); got != want {
		t.Fatalf("exact suffix = %q, want %q", got, want)
	}
	if fetcher.agentIDs[0] != "orch-agent-id" {
		t.Fatalf("Main must poll the orchestrator's stable agent_id; got %v", fetcher.agentIDs)
	}
}

func TestStreamProgressFactualCharAndDelayFormatting(t *testing.T) {
	for chars, want := range map[int64]string{-8: "0", 0: "0", 999: "999", 1000: "1.0k", 1049: "1.0k", 1100: "1.1k", 4099: "4.1k"} {
		if got := formatStreamedChars(chars); got != want {
			t.Errorf("formatStreamedChars(%d) = %q, want %q", chars, got, want)
		}
	}
	now := time.UnixMilli(1756200000100)
	for name, tc := range map[string]struct {
		updated int64
		want    float64
	}{
		"one decimal":   {updated: now.Add(-100 * time.Millisecond).UnixMilli(), want: 0.1},
		"future clamps": {updated: now.Add(time.Second).UnixMilli(), want: 0},
		"whole seconds": {updated: now.Add(-1200 * time.Millisecond).UnixMilli(), want: 1.2},
	} {
		if got := streamProgressDelaySeconds(tc.updated, now); got != tc.want {
			t.Errorf("%s: delay = %.3f, want %.3f", name, got, tc.want)
		}
	}
}

func TestStreamProgressHiddenUnlessLifecycleActiveAndSnapshotActive(t *testing.T) {
	// Lifecycle IDLE: no poll is even issued, nothing renders.
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(4000), ok: true}
	m := newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "idle")
	if cmd := m.maybeScheduleStreamProgress(time.Now()); cmd != nil {
		t.Fatal("no fetch may be scheduled while the recipient is not ACTIVE")
	}
	if strings.Contains(ansi.Strip(m.View()), "chars") {
		t.Fatal("idle recipient must not show progress")
	}

	// Lifecycle ACTIVE but the kernel snapshot is inactive: nothing renders.
	inactive := activeStreamSnapshot(4000)
	inactive.Active = false
	fetcher = &fakeStreamFetcher{snapshot: inactive, ok: true}
	m = newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "active")
	m = pollStream(t, m, time.Now())
	if m.streamProgress.ok || strings.Contains(ansi.Strip(m.View()), "chars") {
		t.Fatal("inactive snapshot must not show progress")
	}

	// Endpoint unavailable (fetch not ok): nothing renders, agent keeps going.
	fetcher = &fakeStreamFetcher{ok: false}
	m = newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "active")
	m = pollStream(t, m, time.Now())
	if m.streamProgress.ok || strings.Contains(ansi.Strip(m.View()), "chars") {
		t.Fatal("unavailable endpoint must not show progress")
	}
	if !strings.Contains(emailToLine(m.View()), i18n.T("state.active")) {
		t.Fatal("the ordinary Active badge must survive an unavailable endpoint")
	}
}

func TestStreamProgressClearsImmediatelyWhenRecipientStopsBeingActive(t *testing.T) {
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(8000), ok: true}
	m := newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "active")
	m = pollStream(t, m, time.Now())
	if !strings.Contains(emailToLine(m.View()), charsSuffix("8.0k")) {
		t.Fatal("setup: suffix should be visible")
	}

	// The very next refresh reports IDLE: the suffix is gone on the next frame,
	// before any further poll, and the cached reading is dropped on the next tick.
	m = refreshState(t, m, "idle")
	if strings.Contains(ansi.Strip(m.View()), "chars") {
		t.Fatal("suffix must clear immediately once the recipient is not ACTIVE")
	}
	if cmd := m.maybeScheduleStreamProgress(time.Now()); cmd != nil || m.streamProgress.ok {
		t.Fatal("cached reading must be dropped and no poll issued while inactive")
	}
}

func TestStreamProgressIgnoresStaleGenerationAndMismatchedRecipient(t *testing.T) {
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(4000), ok: true}
	m := newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "active")
	m = pollStream(t, m, time.Now())
	if !m.streamProgress.ok {
		t.Fatal("setup: reading should be cached")
	}

	// Stale generation: dropped entirely (no effect on the cached reading).
	stale := streamProgressMsg{generation: m.generation + 1, recipient: m.visibleStreamRecipient(), snapshot: activeStreamSnapshot(1), ok: true}
	m, cmd := m.Update(stale)
	if cmd != nil || m.streamProgress.snapshot.StreamedChars != 4000 {
		t.Fatal("stale-generation result must be ignored without rescheduling")
	}

	// Same generation and newest serial but a different recipient identity:
	// cleared, never shown.
	other := streamProgressMsg{generation: m.generation, serial: m.streamProgressSerial, recipient: streamRecipient{agentID: "someone-else", directory: m.orchestrator}, snapshot: activeStreamSnapshot(1), ok: true}
	m, _ = m.Update(other)
	if m.streamProgress.ok || strings.Contains(ansi.Strip(m.View()), "chars") {
		t.Fatal("a result for a different recipient must clear the badge")
	}

	// The orchestrator identity changes under a still-cached reading: the
	// render gate hides it at once.
	m = pollStream(t, m, time.Now().Add(time.Minute))
	if !strings.Contains(emailToLine(m.View()), charsSuffix("4.0k")) {
		t.Fatal("setup: suffix should be visible again")
	}
	m.orchAgentID = "replaced-agent-id"
	if strings.Contains(ansi.Strip(m.View()), "chars") {
		t.Fatal("suffix must not render beside a recipient it was not fetched for")
	}
}

func TestStreamProgressMainRequiresOrchestratorIdentity(t *testing.T) {
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(4000), ok: true}
	m := newStreamMail(t, fetcher, 100)
	m.orchAgentID = ""
	m = refreshState(t, m, "active")
	if cmd := m.maybeScheduleStreamProgress(time.Now()); cmd != nil {
		t.Fatal("no agent_id, no probe")
	}
	// A refresh that learns the agent_id from .agent.json enables polling.
	m, _ = m.Update(mailRefreshMsg{generation: m.generation, state: "active", alive: true, orchAgentID: "learned-id"})
	m = pollStream(t, m, time.Now())
	if len(fetcher.agentIDs) != 1 || fetcher.agentIDs[0] != "learned-id" {
		t.Fatalf("poll must use the refreshed agent_id; got %v", fetcher.agentIDs)
	}
}

func TestStreamProgressDirectPageUsesTargetIdentityAndClearsOnReturn(t *testing.T) {
	fx := newDirectOrderingFixture(t)
	m := fx.mail
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(2000), ok: true}
	m.streamProgressFetcher = fetcher
	m.orchAgentID = "main-orch-id"

	// Real refresh discovers agent-a as a direct row; activate it.
	m, cmd := directOrderingIssueRefresh(t, m)
	m, _ = m.Update(directOrderingRunRefresh(t, cmd, "direct discovery"))
	rowIndex := -1
	for i, row := range m.agentSelector.rows {
		if !row.Main && row.Target.AgentID == fx.targetA.AgentID {
			rowIndex = i
		}
	}
	if rowIndex < 0 {
		t.Fatalf("direct row for %s not discovered: %+v", fx.targetA.AgentID, m.agentSelector.rows)
	}
	m, _ = m.activateConversationRow(rowIndex)
	target, ok := m.currentDirectTarget()
	if !ok || target.AgentID != fx.targetA.AgentID {
		t.Fatalf("direct target not current: %+v %v", target, ok)
	}
	key := fs.DirectThreadKey(target)
	if m.directLifecycles == nil {
		m.directLifecycles = map[string]directAgentLifecycle{}
	}
	m.directLifecycles[key] = directAgentLifecycle{state: "active", activeSince: time.Now()}
	m.orchState = "idle" // Main is idle; only the direct target streams

	m = pollStream(t, m, time.Now())
	if len(fetcher.agentIDs) != 1 || fetcher.agentIDs[0] != fx.targetA.AgentID {
		t.Fatalf("direct page must poll the target's stable agent_id, got %v", fetcher.agentIDs)
	}
	want := m.visibleStreamRecipient()
	if want.threadKey != key || want.directory != fx.targetA.Directory {
		t.Fatalf("direct recipient identity = %+v", want)
	}
	if !strings.Contains(emailToLine(m.View()), charsSuffix("2.0k")) {
		t.Fatalf("direct footer should show %q; got %q", charsSuffix("2.0k"), emailToLine(m.View()))
	}

	// Returning to Main changes the visible recipient: the suffix clears at
	// once (Main is idle and the cached reading belongs to agent-a).
	m, _ = m.activateConversationRow(0)
	if _, direct := m.currentDirectTarget(); direct {
		t.Fatal("setup: Main should be current again")
	}
	if strings.Contains(ansi.Strip(m.View()), "chars") {
		t.Fatal("suffix must clear immediately when the visible recipient changes")
	}
	if cmd := m.maybeScheduleStreamProgress(time.Now()); cmd != nil || m.streamProgress.ok {
		t.Fatal("no poll for idle Main, and the direct reading must be dropped")
	}
}

func TestStreamProgressNarrowTerminalOmitsSuffixWithoutWrapping(t *testing.T) {
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(4000), ok: true}
	m := newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "active")
	m = pollStream(t, m, time.Now())
	if !strings.Contains(emailToLine(m.View()), charsSuffix("4.0k")) {
		t.Fatal("setup: wide terminal shows the suffix")
	}

	m, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 24})
	view := ansi.Strip(m.View())
	footers := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Email To:") {
			footers++
			if strings.Contains(line, "chars") {
				t.Fatalf("narrow footer must omit the suffix: %q", line)
			}
			if !strings.Contains(line, i18n.T("state.active")) {
				t.Fatalf("narrow footer must keep the Active badge: %q", line)
			}
		}
	}
	if footers != 1 {
		t.Fatalf("expected exactly one Email To: line, got %d", footers)
	}
	if strings.Contains(view, "chars") {
		t.Fatal("suffix must not wrap onto another line")
	}
}

func TestStreamProgressViewPerformsNoIO(t *testing.T) {
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(4000), ok: true}
	m := newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "active")
	m = pollStream(t, m, time.Now())
	if fetcher.calls != 1 {
		t.Fatalf("exactly one fetch expected from the poll path, got %d", fetcher.calls)
	}

	// Arm the fetcher: any Fetch from here on panics. Rendering, layout, and
	// the suffix helper must all read the cached reading only.
	fetcher.panicOnCall = true
	for i := 0; i < 3; i++ {
		if !strings.Contains(emailToLine(m.View()), charsSuffix("4.0k")) {
			t.Fatal("cached reading should keep rendering")
		}
		_ = m.streamProgressSuffix("active")
		m.lastInputLines = -1
		_ = m.syncViewportHeight()
	}
	// The poll tick returns a command but never executes it inline.
	fetcher.panicOnCall = false
	m.streamProgressInFlight = false
	before := fetcher.calls
	m, _ = m.Update(tickMsg{generation: m.generation, pollEpoch: m.pollEpoch})
	if fetcher.calls != before {
		t.Fatal("Update must only schedule the fetch, never run it inline")
	}
	if !m.streamProgressInFlight {
		t.Fatal("tick should have scheduled one stream-progress fetch for the ACTIVE recipient")
	}
}

func TestStreamProgressTickSchedulesOnlyWhenActiveAndOneInFlight(t *testing.T) {
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(4000), ok: true}
	m := newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "idle")
	m, _ = m.Update(tickMsg{generation: m.generation, pollEpoch: m.pollEpoch})
	if m.streamProgressInFlight {
		t.Fatal("idle recipient must not schedule a fetch")
	}

	m = refreshState(t, m, "active")
	now := time.Now()
	if cmd := m.maybeScheduleStreamProgress(now); cmd == nil {
		t.Fatal("active recipient should schedule a fetch")
	}
	if cmd := m.maybeScheduleStreamProgress(now.Add(time.Second)); cmd != nil {
		t.Fatal("a second fetch must not be issued while one is in flight")
	}
	// A fetch whose result never came back (dropped by a view switch) expires.
	if cmd := m.maybeScheduleStreamProgress(now.Add(streamProgressInFlightTTL + time.Second)); cmd == nil {
		t.Fatal("in-flight gate must expire so polling resumes")
	}
	// A new activation generation is never blocked by the old flag.
	m.generation++
	cmd := m.maybeScheduleStreamProgress(now.Add(2 * time.Second))
	if cmd == nil {
		t.Fatal("a new generation must be able to poll immediately")
	}
	// Landing that fetch clears the gate; the next tick may poll again.
	m, _ = m.Update(runCmd(cmd))
	if m.streamProgressInFlight {
		t.Fatal("landing must clear the in-flight gate")
	}
	if cmd := m.maybeScheduleStreamProgress(now.Add(3 * time.Second)); cmd == nil {
		t.Fatal("polling must resume after a landed fetch")
	}
}

// (a) A TTL-expired request that lands after its replacement is a no-op: it
// neither clears the replacement's in-flight gate nor installs its reading —
// including when the replacement's result has already been installed.
func TestStreamProgressExpiredRequestLandingAfterReplacementIsNoOp(t *testing.T) {
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(4000), ok: true}
	m := newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "active")
	now := time.Now()

	old := m.maybeScheduleStreamProgress(now)
	if old == nil {
		t.Fatal("setup: first fetch should be scheduled")
	}
	oldSerial := m.streamProgressSerial
	replacement := m.maybeScheduleStreamProgress(now.Add(streamProgressInFlightTTL + time.Second))
	if replacement == nil {
		t.Fatal("setup: TTL expiry must schedule a replacement")
	}
	if m.streamProgressSerial == 0 || m.streamProgressSerial <= oldSerial {
		t.Fatalf("serials must be nonzero and monotonic: old=%d new=%d", oldSerial, m.streamProgressSerial)
	}

	// The old request reports first, after the replacement was issued.
	oldMsg := runCmd(old).(streamProgressMsg)
	if oldMsg.serial != oldSerial {
		t.Fatalf("old result must carry its own serial %d, got %d", oldSerial, oldMsg.serial)
	}
	m, cmd := m.Update(oldMsg)
	if cmd != nil {
		t.Fatal("stale result must not reschedule")
	}
	if !m.streamProgressInFlight {
		t.Fatal("old result must not clear the replacement's in-flight gate")
	}
	if m.streamProgress.ok {
		t.Fatal("old result must not install a reading")
	}
	if c := m.maybeScheduleStreamProgress(now.Add(streamProgressInFlightTTL + 2*time.Second)); c != nil {
		t.Fatal("replacement must still gate the next tick after the old result landed")
	}

	// The replacement lands normally.
	fetcher.snapshot = activeStreamSnapshot(8000)
	m, _ = m.Update(runCmd(replacement))
	if m.streamProgressInFlight || !m.streamProgress.ok || m.streamProgress.snapshot.StreamedChars != 8000 {
		t.Fatalf("replacement must clear the gate and install its reading; inFlight=%v state=%+v", m.streamProgressInFlight, m.streamProgress)
	}

	// Already installed: a second copy of the old result — even an "unavailable"
	// one — cannot overwrite or clear the newer reading.
	unavailable := oldMsg
	unavailable.ok = false
	m, _ = m.Update(unavailable)
	m, _ = m.Update(oldMsg)
	if !m.streamProgress.ok || m.streamProgress.snapshot.StreamedChars != 8000 {
		t.Fatalf("installed newer reading must survive stale results; got %+v", m.streamProgress)
	}
	if m.streamProgressInFlight {
		t.Fatal("stale results must not re-arm the in-flight gate")
	}
	if !strings.Contains(emailToLine(m.View()), charsSuffix("8.0k")) {
		t.Fatalf("footer should keep the newer reading; got %q", emailToLine(m.View()))
	}
}

// (b) Switching the active visible recipient (Main -> direct target) schedules
// at once instead of waiting for Main's in-flight request to expire, and Main's
// late result can neither clear the direct request's gate nor be applied.
func TestStreamProgressRecipientSwitchSchedulesImmediatelyAndIgnoresOldResult(t *testing.T) {
	fx := newDirectOrderingFixture(t)
	m := fx.mail
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(4000), ok: true}
	m.streamProgressFetcher = fetcher
	m.orchAgentID = "main-orch-id"
	m.orchState = "active"
	m.activeSince = time.Now()
	now := time.Now()

	// Main is ACTIVE and has a request in flight.
	mainCmd := m.maybeScheduleStreamProgress(now)
	if mainCmd == nil {
		t.Fatal("setup: Main should schedule a fetch")
	}
	mainSerial := m.streamProgressSerial
	if m.streamProgressInFlightFor != m.visibleStreamRecipient() {
		t.Fatalf("in-flight recipient must be Main; got %+v", m.streamProgressInFlightFor)
	}

	// Switch to the direct target well before Main's request expires.
	m, cmd := directOrderingIssueRefresh(t, m)
	m, _ = m.Update(directOrderingRunRefresh(t, cmd, "direct discovery"))
	rowIndex := -1
	for i, row := range m.agentSelector.rows {
		if !row.Main && row.Target.AgentID == fx.targetA.AgentID {
			rowIndex = i
		}
	}
	if rowIndex < 0 {
		t.Fatalf("direct row for %s not discovered: %+v", fx.targetA.AgentID, m.agentSelector.rows)
	}
	m, _ = m.activateConversationRow(rowIndex)
	target, ok := m.currentDirectTarget()
	if !ok || target.AgentID != fx.targetA.AgentID {
		t.Fatalf("direct target not current: %+v %v", target, ok)
	}
	if m.directLifecycles == nil {
		m.directLifecycles = map[string]directAgentLifecycle{}
	}
	m.directLifecycles[fs.DirectThreadKey(target)] = directAgentLifecycle{state: "active", activeSince: time.Now()}

	directCmd := m.maybeScheduleStreamProgress(now.Add(time.Second))
	if directCmd == nil {
		t.Fatal("recipient switch must schedule immediately, not wait for the old in-flight TTL")
	}
	directRecipient := m.visibleStreamRecipient()
	if m.streamProgressSerial <= mainSerial || m.streamProgressInFlightFor != directRecipient {
		t.Fatalf("direct request must carry a newer serial for the direct recipient; serial=%d for=%+v", m.streamProgressSerial, m.streamProgressInFlightFor)
	}
	// The same recipient is still gated while its own request is unexpired.
	if c := m.maybeScheduleStreamProgress(now.Add(2 * time.Second)); c != nil {
		t.Fatal("direct recipient must be gated by its own in-flight request")
	}

	// Main's old result arrives late: no-op.
	mainMsg := runCmd(mainCmd).(streamProgressMsg)
	if mainMsg.recipient.agentID != "main-orch-id" || mainMsg.serial != mainSerial {
		t.Fatalf("unexpected old result %+v", mainMsg)
	}
	m, _ = m.Update(mainMsg)
	if !m.streamProgressInFlight || m.streamProgressInFlightFor != directRecipient {
		t.Fatal("old recipient's result must not clear the direct request's in-flight gate")
	}
	if m.streamProgress.ok || strings.Contains(ansi.Strip(m.View()), "chars") {
		t.Fatal("old recipient's result must not be applied or rendered")
	}

	// The direct result lands normally for the direct recipient.
	fetcher.snapshot = activeStreamSnapshot(2000)
	m, _ = m.Update(runCmd(directCmd))
	if m.streamProgressInFlight || !m.streamProgress.ok || m.streamProgress.recipient != directRecipient {
		t.Fatalf("direct result must clear the gate and install for the direct recipient; inFlight=%v state=%+v", m.streamProgressInFlight, m.streamProgress)
	}
	if !strings.Contains(emailToLine(m.View()), charsSuffix("2.0k")) {
		t.Fatalf("direct footer should show %q; got %q", charsSuffix("2.0k"), emailToLine(m.View()))
	}
	if len(fetcher.agentIDs) != 2 || fetcher.agentIDs[0] != "main-orch-id" || fetcher.agentIDs[1] != fx.targetA.AgentID {
		t.Fatalf("fetch order should be Main then direct target; got %v", fetcher.agentIDs)
	}
}

// (c) The newest request's result still clears the gate and applies normally,
// and the next tick polls again — the ordinary one-second cadence.
func TestStreamProgressNewestResultClearsGateAndApplies(t *testing.T) {
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(4000), ok: true}
	m := newStreamMail(t, fetcher, 100)
	m = refreshState(t, m, "active")
	now := time.Now()

	first := m.maybeScheduleStreamProgress(now)
	if first == nil {
		t.Fatal("setup: fetch should be scheduled")
	}
	msg := runCmd(first).(streamProgressMsg)
	if msg.serial == 0 || msg.serial != m.streamProgressSerial || msg.generation != m.generation {
		t.Fatalf("result must carry the newest nonzero serial for the current generation: %+v", msg)
	}
	m, _ = m.Update(msg)
	if m.streamProgressInFlight {
		t.Fatal("newest result must clear the in-flight gate")
	}
	if !m.streamProgress.ok || m.streamProgress.snapshot.StreamedChars != 4000 || m.streamProgress.recipient != m.visibleStreamRecipient() {
		t.Fatalf("newest result must install its reading; got %+v", m.streamProgress)
	}
	if !strings.Contains(emailToLine(m.View()), charsSuffix("4.0k")) {
		t.Fatalf("footer should show %q; got %q", charsSuffix("4.0k"), emailToLine(m.View()))
	}

	// Next tick polls again with a fresh, strictly newer serial, and its
	// result replaces the reading.
	second := m.maybeScheduleStreamProgress(now.Add(time.Second))
	if second == nil {
		t.Fatal("polling must resume after a landed fetch")
	}
	if m.streamProgressSerial != msg.serial+1 {
		t.Fatalf("serial must advance by one per schedule: %d -> %d", msg.serial, m.streamProgressSerial)
	}
	fetcher.snapshot = activeStreamSnapshot(8000)
	m, _ = m.Update(runCmd(second))
	if m.streamProgressInFlight || m.streamProgress.snapshot.StreamedChars != 8000 {
		t.Fatalf("second result must clear the gate and replace the reading; inFlight=%v state=%+v", m.streamProgressInFlight, m.streamProgress)
	}
	if fetcher.calls != 2 {
		t.Fatalf("exactly two fetches expected, got %d", fetcher.calls)
	}
}

func TestStreamProgressMsgIsRootRoutedToMailFromOtherViews(t *testing.T) {
	fetcher := &fakeStreamFetcher{snapshot: activeStreamSnapshot(4000), ok: true}
	mail := newStreamMail(t, fetcher, 100)
	mail = refreshState(t, mail, "active")
	cmd := mail.maybeScheduleStreamProgress(time.Now())
	if cmd == nil {
		t.Fatal("setup: fetch should be scheduled")
	}
	a := App{currentView: appViewHelp, help: NewHelpModel(), mail: mail}
	model, _ := a.Update(runCmd(cmd))
	got := model.(App).mail
	if !got.streamProgress.ok || got.streamProgress.snapshot.StreamedChars != 4000 {
		t.Fatal("streamProgressMsg must reach the preserved Mail model while another view is active")
	}
	if got.streamProgressInFlight {
		t.Fatal("root-routed landing must clear Mail's in-flight gate")
	}
}
