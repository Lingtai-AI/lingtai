package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeHeartbeat(t *testing.T, dir string, ts time.Time) {
	t.Helper()
	content := fmt.Sprintf("%.6f", float64(ts.UnixNano())/1e9)
	if err := os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAliveDirs_FreshAndStaleHeartbeats(t *testing.T) {
	fresh := t.TempDir()
	stale := t.TempDir()
	missing := t.TempDir()
	garbage := t.TempDir()

	writeHeartbeat(t, fresh, time.Now())
	writeHeartbeat(t, stale, time.Now().Add(-60*time.Second))
	if err := os.WriteFile(filepath.Join(garbage, ".agent.heartbeat"), []byte("not-a-number"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := aliveDirs([]string{fresh, stale, missing, garbage}, 3.0)
	if len(got) != 1 || got[0] != fresh {
		t.Fatalf("aliveDirs = %v, want only %q", got, fresh)
	}
}

func TestWaitForSuspend_ReturnsEmptyWhenAgentsDie(t *testing.T) {
	dir := t.TempDir()
	writeHeartbeat(t, dir, time.Now())

	// With a 0.5s threshold and no refresher, the heartbeat goes stale
	// mid-wait; waitForSuspend should return before the full 5s timeout.
	start := time.Now()
	survivors := waitForSuspend([]string{dir}, 5*time.Second, 0.5)
	elapsed := time.Since(start)

	if len(survivors) != 0 {
		t.Fatalf("survivors = %v, want none", survivors)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("waitForSuspend took %v, expected early return before timeout", elapsed)
	}
}

func TestWaitForSuspend_ReturnsSurvivorsOnTimeout(t *testing.T) {
	dir := t.TempDir()
	writeHeartbeat(t, dir, time.Now())

	// Keep the heartbeat fresh for the duration of the wait, simulating an
	// agent that never suspends. Under the old code this state fell through
	// to os.RemoveAll and created a phantom.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// Write via rename so a concurrent poll never reads a
				// truncated file. (Not writeHeartbeat: t.Fatal must not
				// run off the test goroutine.)
				content := fmt.Sprintf("%.6f", float64(time.Now().UnixNano())/1e9)
				tmp := filepath.Join(dir, ".agent.heartbeat.tmp")
				os.WriteFile(tmp, []byte(content), 0o644)
				os.Rename(tmp, filepath.Join(dir, ".agent.heartbeat"))
			}
		}
	}()

	survivors := waitForSuspend([]string{dir}, 1*time.Second, 3.0)
	close(stop)
	<-done

	if len(survivors) != 1 || survivors[0] != dir {
		t.Fatalf("survivors = %v, want [%q]", survivors, dir)
	}
}

func TestWaitForSuspend_NoDirs(t *testing.T) {
	start := time.Now()
	survivors := waitForSuspend(nil, 5*time.Second, 3.0)
	if len(survivors) != 0 {
		t.Fatalf("survivors = %v, want none", survivors)
	}
	if time.Since(start) >= 1*time.Second {
		t.Fatalf("waitForSuspend with no dirs should return immediately")
	}
}
