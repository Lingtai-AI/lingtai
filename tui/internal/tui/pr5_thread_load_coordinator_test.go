package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

type pr5ThreadLoadReply struct {
	sessionCache *fs.SessionCache
	err          error
}

type pr5ThreadLoadFlight struct {
	request threadLoadRequest
	release chan pr5ThreadLoadReply
}

type pr5BlockingThreadLoadWorker struct {
	started chan pr5ThreadLoadFlight
	stop    chan struct{}
}

func newPR5BlockingThreadLoadWorker(t *testing.T) *pr5BlockingThreadLoadWorker {
	t.Helper()
	worker := &pr5BlockingThreadLoadWorker{
		started: make(chan pr5ThreadLoadFlight),
		stop:    make(chan struct{}),
	}
	t.Cleanup(func() { close(worker.stop) })
	return worker
}

func (w *pr5BlockingThreadLoadWorker) Load(request threadLoadRequest) (*fs.SessionCache, error) {
	flight := pr5ThreadLoadFlight{
		request: request,
		release: make(chan pr5ThreadLoadReply, 1),
	}
	select {
	case w.started <- flight:
	case <-w.stop:
		return nil, nil
	}
	select {
	case reply := <-flight.release:
		return reply.sessionCache, reply.err
	case <-w.stop:
		return nil, nil
	}
}

func TestPR5Stage1ThreadLoadCoordinatorCoalescesLatestRerunAndSettlesSerially(t *testing.T) {
	app, _, _ := installationNewApp(t, 0)
	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	worker := newPR5BlockingThreadLoadWorker(t)
	app.threadLoads = newThreadLoadCoordinator(worker)

	targetA := filepath.Join(app.projectDir, "agent-a")
	targetB := filepath.Join(app.projectDir, "agent-b")
	installationWriteAgent(t, targetA, "agent-a", "Agent A", "Agent A")
	installationWriteAgent(t, targetB, "agent-b", "Agent B", "Agent B")

	pr5BindCoordinatorRailTarget(t, &app, targetA, "Agent A", 4101, 1)
	a1Request := pr5CoordinatorRequest(t, app, "A1")
	a1Done := pr5RunThreadLoadCmd(t, app.threadLoads.request(a1Request), "A1")
	a1Flight := pr5AwaitThreadLoadFlight(t, worker, "A1")

	pr5BindCoordinatorRailTarget(t, &app, targetB, "Agent B", 4201, 2)
	b1Request := pr5CoordinatorRequest(t, app, "B1")
	b1Done := pr5RunThreadLoadCmd(t, app.threadLoads.request(b1Request), "B1")
	b1Flight := pr5AwaitThreadLoadFlight(t, worker, "B1")

	// Returning to A creates a fresh cold generation. Both logical A2 requests
	// fold behind the physically running A1 slot, and the second request replaces
	// the first as the single latest rerun.
	pr5BindCoordinatorRailTarget(t, &app, targetA, "Agent A", 4101, 3)
	a2First := pr5CoordinatorRequest(t, app, "A2-first")
	if cmd := app.threadLoads.request(a2First); cmd != nil {
		t.Fatal("first A2 logical request returned physical work; want same-target coalescing behind A1")
	}
	a2Latest := pr5CoordinatorRequest(t, app, "A2-latest")
	if cmd := app.threadLoads.request(a2Latest); cmd != nil {
		t.Fatal("latest A2 logical request returned physical work; want one replaced latest-rerun slot behind A1")
	}
	pr5RequireThreadLoadCounters(t, app.threadLoads.Counters(), ThreadLoadCounters{
		Started:       2,
		Coalesced:     2,
		Completed:     0,
		TrueCancelled: 0,
		StaleDropped:  0,
	})

	currentA2 := app.asyncCurrent()
	beforePublish := app.currentThread.sessionCache
	a1Cache := fs.NewSessionCache(app.mail.humanDir, filepath.Dir(app.projectDir), fs.NoPersist)
	a1Flight.release <- pr5ThreadLoadReply{sessionCache: a1Cache}
	var a2Cmd tea.Cmd
	app, a2Cmd = pr5DeliverThreadLoadResult(t, app, a1Done, "A1")
	if app.currentThread.sessionCache != beforePublish {
		t.Fatal("stale A1 completion published over the current cold A2 state")
	}
	if a2Cmd == nil {
		t.Fatal("stale A1 settlement did not release its target slot and launch the one latest A2 rerun")
	}

	a2Done := pr5RunThreadLoadCmd(t, a2Cmd, "A2-rerun")
	a2Flight := pr5AwaitThreadLoadFlight(t, worker, "A2-rerun")
	if got := a2Flight.request.targetDisplayName; got != "A2-latest" {
		t.Fatalf("A2 rerun payload = %q, want latest logical request %q", got, "A2-latest")
	}

	b1Cache := fs.NewSessionCache(app.mail.humanDir, filepath.Dir(app.projectDir), fs.NoPersist)
	b1Flight.release <- pr5ThreadLoadReply{sessionCache: b1Cache}
	var followup tea.Cmd
	app, followup = pr5DeliverThreadLoadResult(t, app, b1Done, "B1")
	if followup != nil {
		t.Fatal("stale B1 settlement returned an unexpected rerun command")
	}
	if app.currentThread.sessionCache != beforePublish {
		t.Fatal("stale B1 completion published over the current cold A2 state")
	}

	a2Cache := fs.NewSessionCache(app.mail.humanDir, filepath.Dir(app.projectDir), fs.NoPersist)
	a2Flight.release <- pr5ThreadLoadReply{sessionCache: a2Cache}
	app, followup = pr5DeliverThreadLoadResult(t, app, a2Done, "A2")
	if followup != nil {
		t.Fatal("accepted A2 settlement returned an unexpected follow-up command")
	}
	if app.currentThread.target != currentA2.binding.target ||
		app.currentThread.generation != currentA2.binding.generation ||
		app.currentThread.acceptedSnapshotVersion != currentA2.storeVersion ||
		app.currentThread.sessionCache != a2Cache {
		t.Fatalf("published ThreadState = %#v, want current A2 target/generation/version and cache %p", app.currentThread, a2Cache)
	}

	pr5RequireThreadLoadCounters(t, app.threadLoads.Counters(), ThreadLoadCounters{
		Started:       3,
		Coalesced:     2,
		Completed:     3,
		TrueCancelled: 0,
		StaleDropped:  2,
	})
	select {
	case extra := <-worker.started:
		t.Fatalf("unexpected fourth physical worker start for generation %d", extra.request.envelope.generation.thread)
	default:
	}
}

func pr5BindCoordinatorRailTarget(
	t *testing.T,
	app *App,
	targetDir string,
	targetName string,
	pid int,
	generation uint64,
) {
	t.Helper()
	mail := NewMailModel(
		app.mail.humanDir,
		app.mail.humanAddr,
		app.projectDir,
		targetDir,
		targetName,
		app.mail.pageSize,
		app.globalDir,
		"en",
		false,
		0,
	)
	mail.generation = generation
	app.mailStore.bindMailModel(&mail, asyncTargetHomeAgentRail, pid)
	app.mail = mail
	app.currentThread = newColdThreadState(
		app.mailStore.binding.target,
		mail.generation,
		app.mailStore.version,
		mail.sessionCache,
	)
	current := app.asyncCurrent()
	if !validAsyncOwner(current.binding.owner) || !validAsyncTarget(current.binding.owner, current.binding.target) ||
		current.binding.generation != generation || !current.revalidateTarget(current.binding.owner, current.binding.target) {
		t.Fatalf("invalid coordinator fixture binding: %#v", current)
	}
}

func pr5CoordinatorRequest(t *testing.T, app App, label string) threadLoadRequest {
	t.Helper()
	current := app.asyncCurrent()
	envelope := captureAsync(asyncColdThreadLoad, current)
	if !acceptAsync(current, envelope) {
		t.Fatalf("%s request did not capture an acceptable cold-load envelope: %#v", label, envelope)
	}
	return threadLoadRequest{
		envelope:          envelope,
		humanDir:          app.mail.humanDir,
		humanAddress:      app.mail.humanAddr,
		targetAddress:     app.mail.orchAddr,
		targetDisplayName: label,
		eventWindow:       1,
		inquiryWindow:     1,
	}
}

func pr5RunThreadLoadCmd(t *testing.T, cmd tea.Cmd, label string) <-chan tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatalf("%s physical command is nil", label)
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	return done
}

func pr5AwaitThreadLoadFlight(t *testing.T, worker *pr5BlockingThreadLoadWorker, label string) pr5ThreadLoadFlight {
	t.Helper()
	select {
	case flight := <-worker.started:
		return flight
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s physical worker start", label)
		return pr5ThreadLoadFlight{}
	}
}

func pr5DeliverThreadLoadResult(
	t *testing.T,
	app App,
	done <-chan tea.Msg,
	label string,
) (App, tea.Cmd) {
	t.Helper()
	select {
	case raw := <-done:
		msg, ok := raw.(threadLoadResultMsg)
		if !ok {
			t.Fatalf("%s completion type = %T, want threadLoadResultMsg", label, raw)
		}
		return installationDeliverApp(t, app, msg)
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s physical completion", label)
		return app, nil
	}
}

func pr5RequireThreadLoadCounters(t *testing.T, got, want ThreadLoadCounters) {
	t.Helper()
	if got != want {
		t.Fatalf("thread-load counters = %#v, want %#v", got, want)
	}
}
