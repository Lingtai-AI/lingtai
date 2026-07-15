package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestPR5Stage2ColdDirectProjectionInstallsOnlyCurrentA1B1A2Thread(t *testing.T) {
	app, scanner, _ := installationNewApp(t, 0)
	app.mail.insightsEnabled = true
	targetA := filepath.Join(app.projectDir, "agent-a")
	targetB := filepath.Join(app.projectDir, "agent-b")
	installationWriteAgent(t, targetA, "agent-a", "Agent A", "Agent A")
	installationWriteAgent(t, targetB, "agent-b", "Agent B", "Agent B")
	installationWriteEvents(t, targetA, 1, "event-a")
	installationWriteEvents(t, targetB, 1, "event-b")

	scanner.messages = []fs.MailMessage{
		pr5ProjectionMail("a-in", "human", "agent-a", nil, "mail-a-in", "2026-07-14T01:00:00Z"),
		pr5ProjectionMail("a-out", "agent-a", "human", nil, "mail-a-out", "2026-07-14T01:00:01Z"),
		pr5ProjectionMail("b-in", "human", "agent-b", nil, "mail-b-in", "2026-07-14T01:00:02Z"),
		pr5ProjectionMail("b-out", "agent-b", "human", nil, "mail-b-out", "2026-07-14T01:00:03Z"),
		// This is direct mail for B because B is the primary recipient. A is only
		// copied, so the row must not leak into either A1 or cold A2.
		pr5ProjectionMail("b-cc-a", "human", "agent-b", []string{"agent-a"}, "mail-b-cc-a", "2026-07-14T01:00:04Z"),
	}

	initial := installationRefreshResult(t, &app, true)
	app, _ = installationDeliverApp(t, app, initial)
	rootSnapshot := app.mail.acceptedSnapshot
	if rootSnapshot == nil || app.mailStore.snapshot != rootSnapshot {
		t.Fatalf("initial Main snapshot = %p store=%p, want one accepted root snapshot", rootSnapshot, app.mailStore.snapshot)
	}
	if got := pr5SortedVisibleBodies(app.mail.messages); !reflect.DeepEqual(got, []string{
		"mail-a-in", "mail-a-out", "mail-b-cc-a", "mail-b-in", "mail-b-out",
	}) {
		t.Fatalf("initial Main aggregate bodies = %v, want all five accepted mailbox rows", got)
	}
	if got := scanner.scans.Load(); got != 1 {
		t.Fatalf("initial root scans = %d, want 1", got)
	}
	if !app.mail.insightsEnabled {
		t.Fatal("initial Main fixture lost its enabled automatic-insight policy")
	}

	sentinelPath := pr5WriteMainInsightSentinel(t, app.projectDir)
	beforeStoreID := app.mailStore.id
	beforeStoreVersion := app.mailStore.version
	beforeStoreSnapshot := app.mailStore.snapshot
	beforeTick := app.mailStore.tickChain
	beforeScans := scanner.scans.Load()
	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	app.threadLoads = newThreadLoadCoordinator(directThreadLoadWorker{})

	app = pr5ProjectColdDirectTarget(t, app, rootSnapshot, targetA, "Agent A", 4101, 1, "A1")
	a1Cache := app.currentThread.sessionCache
	pr5RequireColdDirectProjection(t, app, rootSnapshot, targetA, 1, []string{
		"event-a-000", "mail-a-in", "mail-a-out",
	}, []string{"mail-a-in", "mail-a-out"})

	app = pr5ProjectColdDirectTarget(t, app, rootSnapshot, targetB, "Agent B", 4201, 2, "B1")
	pr5RequireColdDirectProjection(t, app, rootSnapshot, targetB, 2, []string{
		"event-b-000", "mail-b-cc-a", "mail-b-in", "mail-b-out",
	}, []string{"mail-b-cc-a", "mail-b-in", "mail-b-out"})

	app = pr5ProjectColdDirectTarget(t, app, rootSnapshot, targetA, "Agent A", 4101, 3, "A2")
	if app.currentThread.sessionCache == a1Cache {
		t.Fatal("cold A2 reused A1 SessionCache; want one fresh active ThreadState with no PR5 warm retention")
	}
	pr5RequireColdDirectProjection(t, app, rootSnapshot, targetA, 3, []string{
		"event-a-000", "mail-a-in", "mail-a-out",
	}, []string{"mail-a-in", "mail-a-out"})

	if app.mailStore.id != beforeStoreID || app.mailStore.version != beforeStoreVersion ||
		app.mailStore.snapshot != beforeStoreSnapshot || app.mailStore.tickChain != beforeTick {
		t.Fatalf(
			"cold direct projection mutated root store: id %d/%d version %d/%d snapshot %p/%p tick %d/%d",
			app.mailStore.id, beforeStoreID,
			app.mailStore.version, beforeStoreVersion,
			app.mailStore.snapshot, beforeStoreSnapshot,
			app.mailStore.tickChain, beforeTick,
		)
	}
	if got := scanner.scans.Load(); got != beforeScans {
		t.Fatalf("cold A1/B1/A2 root scans = %d, want unchanged %d", got, beforeScans)
	}
	if len(app.threadLoads.inFlight) != 0 || len(app.threadLoads.latestRerun) != 0 {
		t.Fatalf("coordinator retained inactive state: inFlight=%d latestRerun=%d", len(app.threadLoads.inFlight), len(app.threadLoads.latestRerun))
	}
	pr5RequireThreadLoadCounters(t, app.threadLoads.Counters(), ThreadLoadCounters{
		Started: 3, Completed: 3,
	})
	if app.mail.insightsEnabled || app.mail.insightPending {
		t.Fatalf("ordinary A2 inherited Main insight policy: enabled=%v pending=%v", app.mail.insightsEnabled, app.mail.insightPending)
	}
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Fatalf("ordinary projection removed Main insight sentinel: %v", err)
	}
}

func pr5ProjectionMail(id, from, to string, cc []string, body, receivedAt string) fs.MailMessage {
	return fs.MailMessage{
		ID: id, MailboxID: id, From: from, To: to, CC: cc,
		Message: body, Type: "normal", ReceivedAt: receivedAt, Delivered: true,
	}
}

func pr5ProjectColdDirectTarget(
	t *testing.T,
	app App,
	rootSnapshot *ProjectMailSnapshot,
	targetDir string,
	targetName string,
	pid int,
	generation uint64,
	label string,
) App {
	t.Helper()
	pr5BindCoordinatorRailTarget(t, &app, targetDir, targetName, pid, generation)
	request := pr5CoordinatorRequest(t, app, label)
	request.acceptedMessages = append([]fs.MailMessage(nil), rootSnapshot.cache.Messages...)
	cmd := app.threadLoads.request(request)
	if cmd == nil {
		t.Fatalf("%s cold direct request returned nil physical command", label)
	}
	raw := cmd()
	msg, ok := raw.(threadLoadResultMsg)
	if !ok {
		t.Fatalf("%s cold direct completion type = %T, want threadLoadResultMsg", label, raw)
	}
	if msg.err != nil || msg.sessionCache == nil {
		t.Fatalf("%s cold direct completion cache=%p err=%v, want nonnil/no error", label, msg.sessionCache, msg.err)
	}
	updated, followup := installationDeliverApp(t, app, msg)
	if followup != nil {
		t.Fatalf("%s accepted cold direct completion returned unexpected follow-up", label)
	}
	return updated
}

func pr5RequireColdDirectProjection(
	t *testing.T,
	app App,
	rootSnapshot *ProjectMailSnapshot,
	targetDir string,
	generation uint64,
	wantSessionBodies []string,
	wantVisibleBodies []string,
) {
	t.Helper()
	if app.currentThread.target.directory != targetDir ||
		app.currentThread.target.policy != asyncTargetHomeAgentRail ||
		app.currentThread.generation != generation ||
		app.currentThread.acceptedSnapshotVersion != rootSnapshot.Version() {
		t.Fatalf(
			"current direct ThreadState target=%#v generation=%d snapshot=%d, want dir=%q policy=%d generation=%d snapshot=%d",
			app.currentThread.target, app.currentThread.generation, app.currentThread.acceptedSnapshotVersion,
			targetDir, asyncTargetHomeAgentRail, generation, rootSnapshot.Version(),
		)
	}
	if app.currentThread.sessionCache == nil || app.mail.sessionCache != app.currentThread.sessionCache {
		t.Fatalf("Mail projection cache=%p current ThreadState cache=%p, want one exact installed NoPersist cache", app.mail.sessionCache, app.currentThread.sessionCache)
	}
	if app.mail.acceptedSnapshot != rootSnapshot {
		t.Fatalf("Mail accepted snapshot=%p, want shared root snapshot %p", app.mail.acceptedSnapshot, rootSnapshot)
	}
	if got := pr5SortedSessionBodies(app.currentThread.sessionCache.Entries()); !reflect.DeepEqual(got, wantSessionBodies) {
		t.Fatalf("direct session bodies = %v, want %v", got, wantSessionBodies)
	}
	if got := pr5SortedVisibleBodies(app.mail.messages); !reflect.DeepEqual(got, wantVisibleBodies) {
		t.Fatalf("direct visible bodies = %v, want %v", got, wantVisibleBodies)
	}
}

func pr5SortedVisibleBodies(messages []ChatMessage) []string {
	bodies := make([]string, 0, len(messages))
	for _, message := range messages {
		bodies = append(bodies, message.Body)
	}
	sort.Strings(bodies)
	return bodies
}
