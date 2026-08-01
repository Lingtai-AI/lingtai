package fs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const directUnreadDurabilityHelperEnv = "LINGTAI_DIRECT_UNREAD_DURABILITY_HELPER"

// TestMain intercepts durability helper subprocesses before normal test
// enumeration. The helper is intentionally not a Test function, so `go test
// -list` exposes only the parent contract test.
func TestMain(m *testing.M) {
	if os.Getenv(directUnreadDurabilityHelperEnv) == "1" {
		os.Exit(runDirectUnreadDurabilityHelper())
	}
	os.Exit(m.Run())
}

func TestDirectUnreadStaleStoresMergeDistinctThreadAdvances(t *testing.T) {
	project := t.TempDir()
	targetA, targetB, messageA, messageB := directUnreadDurabilityFixture(project)
	targets := []DirectTarget{targetA, targetB}
	_ = mustOpenDirectUnreadStore(t, project, targets, nil)

	storeA := mustOpenDirectUnreadStore(t, project, targets, nil)
	storeB := mustOpenDirectUnreadStore(t, project, targets, nil)
	if err := storeA.MarkSeen(targetA, []MailMessage{messageA}); err != nil {
		t.Fatalf("stale store A MarkSeen: %v", err)
	}
	// A completes before stale B is allowed to mutate. B must refresh the valid
	// durable state while holding the transaction lock rather than overwrite it
	// from the snapshot it opened above.
	if err := storeB.MarkSeen(targetB, []MailMessage{messageB}); err != nil {
		t.Fatalf("stale store B MarkSeen: %v", err)
	}

	assertNoUnexpectedDirectUnreadSiblings(t, project)
	assertDirectUnreadPersistedAdvances(t, project,
		directUnreadAdvance{target: targetA, message: messageA},
		directUnreadAdvance{target: targetB, message: messageB},
	)
}

func TestDirectUnreadSubprocessStaleStoresMergeDistinctThreadAdvances(t *testing.T) {
	project := t.TempDir()
	targetA, targetB, messageA, messageB := directUnreadDurabilityFixture(project)
	_ = mustOpenDirectUnreadStore(t, project, []DirectTarget{targetA, targetB}, nil)

	controls := t.TempDir()
	readyA := filepath.Join(controls, "A.ready")
	readyB := filepath.Join(controls, "B.ready")
	releaseA := filepath.Join(controls, "A.release")
	releaseB := filepath.Join(controls, "B.release")

	childA, err := startDirectUnreadDurabilityHelper(project, "A", readyA, releaseA)
	if err != nil {
		t.Fatalf("start direct-unread helper A: %v", err)
	}
	t.Cleanup(childA.stop)
	childB, err := startDirectUnreadDurabilityHelper(project, "B", readyB, releaseB)
	if err != nil {
		childA.stop()
		t.Fatalf("start direct-unread helper B: %v", err)
	}
	t.Cleanup(childB.stop)

	if err := waitForDirectUnreadBarriers(10*time.Second, readyA, readyB); err != nil {
		childA.stop()
		childB.stop()
		t.Fatalf("both helpers did not open the shared S before release: %v\nA: %s\nB: %s", err, childA.output.String(), childB.output.String())
	}

	if err := os.WriteFile(releaseA, []byte("release A\n"), 0o644); err != nil {
		t.Fatalf("release helper A: %v", err)
	}
	if err := childA.wait(10 * time.Second); err != nil {
		childB.stop()
		t.Fatalf("helper A did not complete its ordered mutation: %v\n%s", err, childA.output.String())
	}
	if err := os.WriteFile(releaseB, []byte("release stale B\n"), 0o644); err != nil {
		t.Fatalf("release helper B: %v", err)
	}
	if err := childB.wait(10 * time.Second); err != nil {
		t.Fatalf("helper B did not complete its ordered mutation: %v\n%s", err, childB.output.String())
	}

	assertNoUnexpectedDirectUnreadSiblings(t, project)
	assertDirectUnreadPersistedAdvances(t, project,
		directUnreadAdvance{target: targetA, message: messageA},
		directUnreadAdvance{target: targetB, message: messageB},
	)
}

func TestDirectUnreadIgnoresFixedTempObstruction(t *testing.T) {
	project := t.TempDir()
	targetA, _, messageA, _ := directUnreadDurabilityFixture(project)
	store := mustOpenDirectUnreadStore(t, project, []DirectTarget{targetA}, nil)
	statePath := directUnreadStatePath(project)
	obstruction := statePath + ".tmp"
	if err := os.Mkdir(obstruction, 0o755); err != nil {
		t.Fatalf("create unrelated fixed-temp obstruction: %v", err)
	}

	if err := store.MarkSeen(targetA, []MailMessage{messageA}); err != nil {
		t.Fatalf("MarkSeen depends on obstructed fixed temp %q: %v", obstruction, err)
	}
	info, err := os.Stat(obstruction)
	if err != nil {
		t.Fatalf("unrelated fixed-temp obstruction disappeared: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("unrelated fixed-temp obstruction changed type: mode=%v", info.Mode())
	}
	assertNoUnexpectedDirectUnreadSiblings(t, project, filepath.Base(obstruction))
	assertDirectUnreadPersistedAdvances(t, project, directUnreadAdvance{target: targetA, message: messageA})
}

func TestDirectUnreadConcurrentSavesUseUniqueTempsAndMerge(t *testing.T) {
	project := t.TempDir()
	targetA, targetB, messageA, messageB := directUnreadDurabilityFixture(project)
	targets := []DirectTarget{targetA, targetB}
	_ = mustOpenDirectUnreadStore(t, project, targets, nil)
	storeA := mustOpenDirectUnreadStore(t, project, targets, nil)
	storeB := mustOpenDirectUnreadStore(t, project, targets, nil)

	start := make(chan struct{})
	errs := make([]error, 2)
	var saves sync.WaitGroup
	saves.Add(2)
	go func() {
		defer saves.Done()
		<-start
		errs[0] = storeA.MarkSeen(targetA, []MailMessage{messageA})
	}()
	go func() {
		defer saves.Done()
		<-start
		errs[1] = storeB.MarkSeen(targetB, []MailMessage{messageB})
	}()
	close(start)
	saves.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent MarkSeen %c failed (shared temp collision): %v", 'A'+rune(i), err)
		}
	}

	assertNoUnexpectedDirectUnreadSiblings(t, project)
	assertDirectUnreadPersistedAdvances(t, project,
		directUnreadAdvance{target: targetA, message: messageA},
		directUnreadAdvance{target: targetB, message: messageB},
	)
}

func directUnreadDurabilityFixture(project string) (DirectTarget, DirectTarget, MailMessage, MailMessage) {
	targetA := directUnreadTarget(project, "agent-a", "durability-agent-a", "project/agent-a")
	targetB := directUnreadTarget(project, "agent-b", "durability-agent-b", "project/agent-b")
	messageA := directUnreadIncoming(targetA, "durability-mail-A", "durability-legacy-A", "2026-07-23T01:00:00.000000001Z")
	messageB := directUnreadIncoming(targetB, "durability-mail-B", "durability-legacy-B", "2026-07-23T01:00:00.000000002Z")
	return targetA, targetB, messageA, messageB
}

type directUnreadAdvance struct {
	target  DirectTarget
	message MailMessage
}

func assertDirectUnreadPersistedAdvances(t *testing.T, project string, advances ...directUnreadAdvance) {
	t.Helper()
	data, err := os.ReadFile(directUnreadStatePath(project))
	if err != nil {
		t.Fatalf("read persisted direct-unread state: %v", err)
	}
	var state directUnreadState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode persisted direct-unread state: %v\n%s", err, data)
	}
	if state.Version != directUnreadStateVersion {
		t.Errorf("persisted direct-unread version = %d, want %d", state.Version, directUnreadStateVersion)
	}
	for _, advance := range advances {
		key := DirectThreadKey(advance.target)
		thread, found := state.Threads[key]
		if !found {
			t.Errorf("persisted direct-unread state is missing thread %q", key)
			continue
		}
		cursor, valid := directUnreadCursorForThread(thread)
		if !valid {
			t.Errorf("persisted direct-unread thread %q has invalid cursor: %#v", key, thread)
			continue
		}
		wantAt, err := time.Parse(time.RFC3339Nano, advance.message.ReceivedAt)
		if err != nil {
			t.Fatalf("test fixture has invalid received_at: %v", err)
		}
		wantID := advance.message.MailboxID
		if strings.TrimSpace(wantID) == "" {
			wantID = advance.message.ID
		}
		if !cursor.receivedAt.Equal(wantAt) || len(cursor.ids) != 1 || cursor.ids[0] != wantID {
			t.Errorf("persisted cursor for %s = (%s, %v), want (%s, [%s])", advance.target.AgentID, cursor.receivedAt.Format(time.RFC3339Nano), cursor.ids, wantAt.Format(time.RFC3339Nano), wantID)
		}
	}
}

func assertNoUnexpectedDirectUnreadSiblings(t *testing.T, project string, allowedExtras ...string) {
	t.Helper()
	statePath := directUnreadStatePath(project)
	allowed := map[string]bool{
		filepath.Base(statePath):           true,
		filepath.Base(statePath) + ".lock": true,
	}
	for _, extra := range allowedExtras {
		allowed[extra] = true
	}
	entries, err := os.ReadDir(filepath.Dir(statePath))
	if err != nil {
		t.Fatalf("ReadDir direct-unread state parent: %v", err)
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			t.Errorf("direct-unread transaction leaked unexpected sibling artifact %q", entry.Name())
		}
	}
}

type directUnreadHelperProcess struct {
	cmd      *exec.Cmd
	output   bytes.Buffer
	done     chan error
	finished bool
	waitErr  error
}

func startDirectUnreadDurabilityHelper(project, which, ready, release string) (*directUnreadHelperProcess, error) {
	process := &directUnreadHelperProcess{done: make(chan error, 1)}
	process.cmd = exec.Command(os.Args[0], "-test.run=^$")
	process.cmd.Env = append(os.Environ(),
		directUnreadDurabilityHelperEnv+"=1",
		"LINGTAI_DIRECT_UNREAD_PROJECT="+project,
		"LINGTAI_DIRECT_UNREAD_WHICH="+which,
		"LINGTAI_DIRECT_UNREAD_READY="+ready,
		"LINGTAI_DIRECT_UNREAD_RELEASE="+release,
	)
	process.cmd.Stdout = &process.output
	process.cmd.Stderr = &process.output
	if err := process.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		process.done <- process.cmd.Wait()
	}()
	return process, nil
}

func (process *directUnreadHelperProcess) wait(timeout time.Duration) error {
	if process.finished {
		return process.waitErr
	}
	select {
	case process.waitErr = <-process.done:
		process.finished = true
		return process.waitErr
	case <-time.After(timeout):
		_ = process.cmd.Process.Kill()
		select {
		case process.waitErr = <-process.done:
			process.finished = true
			return fmt.Errorf("helper timed out and was killed: %w", process.waitErr)
		case <-time.After(5 * time.Second):
			return fmt.Errorf("helper timed out and did not terminate after kill")
		}
	}
}

func (process *directUnreadHelperProcess) stop() {
	if process == nil || process.finished {
		return
	}
	_ = process.cmd.Process.Kill()
	select {
	case process.waitErr = <-process.done:
		process.finished = true
	case <-time.After(5 * time.Second):
	}
}

func waitForDirectUnreadBarriers(timeout time.Duration, paths ...string) error {
	deadline := time.Now().Add(timeout)
	for {
		missing := ""
		for _, path := range paths {
			if _, err := os.Stat(path); err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("stat barrier %q: %w", path, err)
				}
				missing = path
				break
			}
		}
		if missing == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %q", missing)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func runDirectUnreadDurabilityHelper() int {
	project := os.Getenv("LINGTAI_DIRECT_UNREAD_PROJECT")
	which := os.Getenv("LINGTAI_DIRECT_UNREAD_WHICH")
	ready := os.Getenv("LINGTAI_DIRECT_UNREAD_READY")
	release := os.Getenv("LINGTAI_DIRECT_UNREAD_RELEASE")
	if project == "" || (which != "A" && which != "B") || ready == "" || release == "" {
		fmt.Fprintf(os.Stderr, "direct-unread helper: invalid controls project=%q which=%q ready=%q release=%q\n", project, which, ready, release)
		return 2
	}

	targetA, targetB, messageA, messageB := directUnreadDurabilityFixture(project)
	store, err := OpenDirectUnreadStore(project, directUnreadHuman, []DirectTarget{targetA, targetB}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "direct-unread helper %s: open shared S: %v\n", which, err)
		return 3
	}
	if err := os.WriteFile(ready, []byte("opened shared S\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "direct-unread helper %s: publish ready barrier: %v\n", which, err)
		return 4
	}
	if err := waitForDirectUnreadBarriers(10*time.Second, release); err != nil {
		fmt.Fprintf(os.Stderr, "direct-unread helper %s: await release: %v\n", which, err)
		return 5
	}

	target, message := targetA, messageA
	if which == "B" {
		target, message = targetB, messageB
	}
	if err := store.MarkSeen(target, []MailMessage{message}); err != nil {
		fmt.Fprintf(os.Stderr, "direct-unread helper %s: MarkSeen after ordered release: %v\n", which, err)
		return 6
	}
	return 0
}
