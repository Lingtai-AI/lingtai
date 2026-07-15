package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

type pr5DirectThreadTestWorker struct {
	calls    int
	received []fs.MailMessage
}

func (w *pr5DirectThreadTestWorker) Load(request threadLoadRequest) (*fs.SessionCache, error) {
	w.calls++
	w.received = append([]fs.MailMessage(nil), request.acceptedMessages...)
	return (directThreadLoadWorker{}).Load(request)
}

func TestPR5Stage1ColdLoaderUsesAcceptedSliceWithoutGlobalRescan(t *testing.T) {
	messages := []fs.MailMessage{
		{ID: "to-a", From: "human", To: "agent-a", Message: "mail-to-a", ReceivedAt: "2026-07-14T01:00:00Z", Delivered: true},
		{ID: "to-b", From: "human", To: "agent-b", Message: "mail-to-b", ReceivedAt: "2026-07-14T01:00:01Z", Delivered: true},
		{ID: "cc-a", From: "human", To: "agent-b", CC: []string{"agent-a"}, Message: "mail-cc-only-a", ReceivedAt: "2026-07-14T01:00:02Z", Delivered: true},
	}
	store, scanner, snapshot, humanDir, targetDir := pr5AcceptedRootSnapshot(t, messages)
	accepted := append([]fs.MailMessage(nil), snapshot.cache.Messages...)
	request := pr5ColdThreadRequest(t, store, snapshot, accepted, humanDir, targetDir, 2, 2)

	beforeID := store.id
	beforeVersion := store.version
	beforeSnapshot := store.snapshot
	beforeTick := store.tickChain
	beforeScans := scanner.scans.Load()

	worker := &pr5DirectThreadTestWorker{}
	coordinator := newThreadLoadCoordinator(worker)
	cmd := coordinator.request(request)
	if cmd == nil {
		t.Fatal("cold loader returned nil command")
	}
	request.acceptedMessages[0].Message = "mutated-after-schedule"
	raw := cmd()
	msg, ok := raw.(threadLoadResultMsg)
	if !ok {
		t.Fatalf("cold loader completion type = %T, want threadLoadResultMsg", raw)
	}

	if worker.calls != 1 {
		t.Fatalf("physical worker calls = %d, want 1", worker.calls)
	}
	if len(worker.received) != len(messages) || worker.received[0].Message != "mail-to-a" {
		t.Fatalf("worker received mutable/non-snapshot mail = %#v, want original accepted slice", worker.received)
	}
	if msg.err != nil {
		t.Fatalf("cold loader err = %v", msg.err)
	}
	if msg.sessionCache == nil {
		t.Fatal("cold loader returned nil SessionCache")
	}
	if msg.envelope != request.envelope {
		t.Fatalf("cold loader completion envelope = %#v, want exact request envelope %#v", msg.envelope, request.envelope)
	}
	if got := scanner.scans.Load(); got != beforeScans {
		t.Fatalf("cold loader root scans = %d after accepted snapshot, want unchanged %d", got, beforeScans)
	}
	if store.id != beforeID || store.version != beforeVersion || store.snapshot != beforeSnapshot || store.tickChain != beforeTick {
		t.Fatalf(
			"cold loader mutated root store: id %d/%d version %d/%d snapshot %p/%p tick %d/%d",
			store.id, beforeID, store.version, beforeVersion, store.snapshot, beforeSnapshot, store.tickChain, beforeTick,
		)
	}
}

func TestPR5Stage1ColdLoaderBuildsDirectNoPersistBoundedSession(t *testing.T) {
	messages := []fs.MailMessage{
		{ID: "to-a", From: "human", To: "agent-a", Message: "mail-to-a", ReceivedAt: "2026-07-14T01:00:00Z", Delivered: true},
		{ID: "to-b", From: "human", To: "agent-b", Message: "mail-to-b", ReceivedAt: "2026-07-14T01:00:01Z", Delivered: true},
	}
	store, _, snapshot, humanDir, targetDir := pr5AcceptedRootSnapshot(t, messages)
	installationWriteEvents(t, targetDir, 1, "event-a")
	if err := os.WriteFile(
		filepath.Join(targetDir, "logs", "soul_inquiry.jsonl"),
		[]byte("{\"ts\":\"2026-07-14T00:00:01Z\",\"source\":\"human\",\"prompt\":\"question-a\",\"voice\":\"inquiry-a\"}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	sentinelPath := filepath.Join(humanDir, "logs", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(sentinelPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const sentinel = "main-aggregate-sentinel\n"
	if err := os.WriteFile(sentinelPath, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	accepted := append([]fs.MailMessage(nil), snapshot.cache.Messages...)
	request := pr5ColdThreadRequest(t, store, snapshot, accepted, humanDir, targetDir, 4, 4)
	worker := &pr5DirectThreadTestWorker{}
	coordinator := newThreadLoadCoordinator(worker)
	cmd := coordinator.request(request)
	if cmd == nil {
		t.Fatal("cold loader returned nil command")
	}
	raw := cmd()
	msg, ok := raw.(threadLoadResultMsg)
	if !ok {
		t.Fatalf("cold loader completion type = %T, want threadLoadResultMsg", raw)
	}
	if worker.calls != 1 {
		t.Fatalf("physical worker calls = %d, want 1", worker.calls)
	}
	if msg.err != nil {
		t.Fatalf("cold loader err = %v", msg.err)
	}
	if msg.sessionCache == nil {
		t.Fatal("cold loader returned nil SessionCache")
	}

	wantBodies := []string{"event-a-000", "inquiry-a", "mail-to-a"}
	if got := pr5SortedSessionBodies(msg.sessionCache.Entries()); !reflect.DeepEqual(got, wantBodies) {
		t.Fatalf("direct cold session bodies = %v, want %v", got, wantBodies)
	}
	if !msg.sessionCache.Complete() {
		t.Fatal("direct cold session within both windows reported incomplete")
	}
	msg.sessionCache.Persist()
	gotSentinel, err := os.ReadFile(sentinelPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotSentinel) != sentinel {
		t.Fatalf("NoPersist cold session changed Main sentinel: got %q want %q", gotSentinel, sentinel)
	}
}

func pr5AcceptedRootSnapshot(
	t *testing.T,
	messages []fs.MailMessage,
) (*ProjectMailStore, *installationScriptedScanner, *ProjectMailSnapshot, string, string) {
	t.Helper()
	projectRoot := filepath.Join(t.TempDir(), "project")
	projectDir := filepath.Join(projectRoot, ".lingtai")
	humanDir := filepath.Join(projectDir, "human")
	targetDir := filepath.Join(projectDir, "agent-a")
	for _, dir := range []string{humanDir, targetDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	scanner := &installationScriptedScanner{messages: append([]fs.MailMessage(nil), messages...)}
	store := newProjectMailStoreWithDeps(projectDir, humanDir, scanner, func(string) {})
	mail := NewMailModel(humanDir, "human", projectRoot, targetDir, "Agent A", 10, "", "en", false, 0)
	mail.generation = 1
	store.bindMailModel(&mail, asyncTargetHomeMain, 0)
	cmd := store.beginRefresh(mail, true)
	if cmd == nil {
		t.Fatal("root store returned nil refresh command")
	}
	raw := cmd()
	msg, ok := raw.(projectMailRefreshMsg)
	if !ok {
		t.Fatalf("root refresh completion type = %T, want projectMailRefreshMsg", raw)
	}
	current := store.asyncCurrent()
	if !acceptAsync(current, msg.envelope) {
		t.Fatalf("root refresh completion failed exact acceptance: current=%#v envelope=%#v validOwner=%v validTarget=%v", current, msg.envelope, validAsyncOwner(current.binding.owner), validAsyncTarget(current.binding.owner, current.binding.target))
	}
	if !store.settleRefreshWork(msg.envelope) {
		t.Fatal("root refresh completion failed exact settlement")
	}
	snapshot := store.installRefresh(msg)
	if snapshot == nil || scanner.scans.Load() != 1 {
		t.Fatalf("accepted root snapshot = %v scans = %d, want nonnil/1", snapshot, scanner.scans.Load())
	}
	return &store, scanner, snapshot, humanDir, targetDir
}

func pr5ColdThreadRequest(
	t *testing.T,
	store *ProjectMailStore,
	snapshot *ProjectMailSnapshot,
	accepted []fs.MailMessage,
	humanDir string,
	targetDir string,
	eventWindow int,
	inquiryWindow int,
) threadLoadRequest {
	t.Helper()
	current := store.asyncCurrent()
	current.binding.target.policy = asyncTargetHomeAgentRail
	current.binding.target.pid = 4242
	current.revalidateTarget = func(asyncOwner, asyncTarget) bool { return true }
	if current.binding.target.directory != targetDir || current.storeVersion != snapshot.Version() {
		t.Fatalf("cold thread source coordinates drifted: target=%q/%q version=%d/%d", current.binding.target.directory, targetDir, current.storeVersion, snapshot.Version())
	}
	envelope := captureAsync(asyncColdThreadLoad, current)
	if !acceptAsync(current, envelope) {
		t.Fatalf("cold thread envelope acceptance failed: envelope=%#v", envelope)
	}
	return threadLoadRequest{
		envelope:          envelope,
		humanDir:          humanDir,
		humanAddress:      "human",
		targetAddress:     "agent-a",
		targetDisplayName: "Agent A",
		acceptedMessages:  append([]fs.MailMessage(nil), accepted...),
		eventWindow:       eventWindow,
		inquiryWindow:     inquiryWindow,
	}
}

func pr5SortedSessionBodies(entries []fs.SessionEntry) []string {
	bodies := make([]string, 0, len(entries))
	for _, entry := range entries {
		bodies = append(bodies, entry.Body)
	}
	sort.Strings(bodies)
	return bodies
}
