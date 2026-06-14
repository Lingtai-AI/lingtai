package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestClearContextAliveWritesClearAndPreservesHistory(t *testing.T) {
	dir := t.TempDir()
	historyPath, contextPath := writeClearTestHistory(t, dir)
	writeJSON(t, filepath.Join(dir, ".agent.json"), map[string]interface{}{"molt_count": 7})

	reviveCalled := false
	completed, err := requestClearContextWithDeps("python", dir, shortClearTestConfig(), clearContextDeps{
		isAlive: func(string, float64) bool { return true },
		revive: func(string, string) error {
			reviveCalled = true
			return nil
		},
		sleep: time.Sleep,
	})
	if err != nil {
		t.Fatalf("requestClearContextWithDeps: %v", err)
	}
	if completed {
		t.Fatal("alive clear should only request clear, not report completion")
	}
	if reviveCalled {
		t.Fatal("alive clear should not revive")
	}
	assertFileContent(t, filepath.Join(dir, ".clear"), "tui\n")
	assertFileContent(t, historyPath, "old chat\n")
	assertFileContent(t, contextPath, "old context\n")
	if _, err := os.Stat(filepath.Join(dir, ".suspend")); !os.IsNotExist(err) {
		t.Fatalf("alive clear wrote .suspend; err=%v", err)
	}
}

func TestRequestClearContextDeadRevivesClearsAndSuspends(t *testing.T) {
	dir := t.TempDir()
	historyPath, contextPath := writeClearTestHistory(t, dir)
	writeJSON(t, filepath.Join(dir, ".agent.json"), map[string]interface{}{"molt_count": 1})

	var alive atomic.Bool
	var revived atomic.Bool
	kernelDone := make(chan error, 1)
	go func() {
		kernelDone <- simulateClearKernel(dir, &alive)
	}()

	completed, err := requestClearContextWithDeps("python", dir, shortClearTestConfig(), clearContextDeps{
		isAlive: func(string, float64) bool { return alive.Load() },
		revive: func(cmd, gotDir string) error {
			if cmd != "python" {
				return fmt.Errorf("lingtaiCmd = %q", cmd)
			}
			if gotDir != dir {
				return fmt.Errorf("dir = %q, want %q", gotDir, dir)
			}
			revived.Store(true)
			alive.Store(true)
			return nil
		},
		sleep: time.Sleep,
	})
	if err != nil {
		t.Fatalf("requestClearContextWithDeps: %v", err)
	}
	if !completed {
		t.Fatal("dead clear should wait for completion")
	}
	if !revived.Load() {
		t.Fatal("dead clear did not revive the agent")
	}
	if err := <-kernelDone; err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(dir, ".suspend"), "")
	assertFileContent(t, historyPath, "old chat\n")
	assertFileContent(t, contextPath, "old context\n")
	if count, ok := readMoltCount(dir); !ok || count != 2 {
		t.Fatalf("molt_count = %d, ok=%v; want 2,true", count, ok)
	}
}

func TestRequestClearContextDeadIgnoresStaleClearCompletionDuringRevive(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, ".agent.json"), map[string]interface{}{"molt_count": 1})
	if err := os.WriteFile(filepath.Join(dir, ".clear"), []byte("old-tui-clear\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var alive atomic.Bool
	kernelDone := make(chan error, 1)
	go func() {
		kernelDone <- simulateDelayedTuiClearKernel(dir, &alive, 3, 50*time.Millisecond)
	}()

	completed, err := requestClearContextWithDeps("python", dir, shortClearTestConfig(), clearContextDeps{
		isAlive: func(string, float64) bool { return alive.Load() },
		revive: func(string, string) error {
			// Simulate the revived kernel consuming a stale previous TUI clear
			// before this request writes its own .clear signal.
			_ = os.Remove(filepath.Join(dir, ".clear"))
			writeJSON(t, filepath.Join(dir, ".agent.json"), map[string]interface{}{"molt_count": 2})
			if err := appendClearTestEvent(dir, "{\"type\":\"psyche_molt\",\"source\":\"tui\"}\n"); err != nil {
				return err
			}
			alive.Store(true)
			return nil
		},
		sleep: time.Sleep,
	})
	if err != nil {
		t.Fatalf("requestClearContextWithDeps: %v", err)
	}
	if !completed {
		t.Fatal("dead clear should wait for the new clear completion")
	}
	if err := <-kernelDone; err != nil {
		t.Fatal(err)
	}
	if count, ok := readMoltCount(dir); !ok || count != 3 {
		t.Fatalf("molt_count = %d, ok=%v; want 3,true", count, ok)
	}
}

func TestWaitForClearCompletionAcceptsTUIEventFallback(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	eventsPath := filepath.Join(logDir, "events.jsonl")
	if err := os.WriteFile(eventsPath, []byte("{\"type\":\"psyche_molt\",\"source\":\"tui\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	offset := eventLogOffset(dir)

	go func() {
		time.Sleep(5 * time.Millisecond)
		f, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		_, _ = f.WriteString("{\"type\":\"clear_received\",\"source\":\"tui\"}\n")
	}()

	err := waitForClearCompletion(dir, 1, true, offset, shortClearTestConfig(), clearContextDeps{sleep: time.Sleep})
	if err != nil {
		t.Fatalf("waitForClearCompletion: %v", err)
	}
}

func shortClearTestConfig() clearWaitConfig {
	return clearWaitConfig{
		aliveTimeout:      500 * time.Millisecond,
		completionTimeout: 500 * time.Millisecond,
		suspendTimeout:    500 * time.Millisecond,
		pollInterval:      time.Millisecond,
	}
}

func writeClearTestHistory(t *testing.T, dir string) (historyPath, contextPath string) {
	t.Helper()
	historyPath = filepath.Join(dir, "history", "chat_history.jsonl")
	contextPath = filepath.Join(dir, "system", "context.md")
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(contextPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, []byte("old chat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contextPath, []byte("old context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return historyPath, contextPath
}

func simulateClearKernel(dir string, alive *atomic.Bool) error {
	clearPath := filepath.Join(dir, ".clear")
	deadline := time.Now().Add(time.Second)
	for {
		data, err := os.ReadFile(clearPath)
		if err == nil {
			if string(data) != "tui\n" {
				return fmt.Errorf(".clear = %q, want %q", string(data), "tui\n")
			}
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for .clear")
		}
		time.Sleep(time.Millisecond)
	}

	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), []byte("{\"molt_count\":2}\n"), 0o644); err != nil {
		return err
	}

	suspendPath := filepath.Join(dir, ".suspend")
	deadline = time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(suspendPath); err == nil {
			alive.Store(false)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for .suspend")
		}
		time.Sleep(time.Millisecond)
	}
}

func simulateDelayedTuiClearKernel(dir string, alive *atomic.Bool, moltCount int, delay time.Duration) error {
	clearPath := filepath.Join(dir, ".clear")
	suspendPath := filepath.Join(dir, ".suspend")
	deadline := time.Now().Add(time.Second)
	for {
		data, err := os.ReadFile(clearPath)
		if err == nil && string(data) == "tui\n" {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for new .clear")
		}
		time.Sleep(time.Millisecond)
	}

	delayDeadline := time.Now().Add(delay)
	for time.Now().Before(delayDeadline) {
		if _, err := os.Stat(suspendPath); err == nil {
			return fmt.Errorf("agent was suspended before new clear completed")
		}
		time.Sleep(time.Millisecond)
	}

	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), []byte(fmt.Sprintf("{\"molt_count\":%d}\n", moltCount)), 0o644); err != nil {
		return err
	}

	deadline = time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(suspendPath); err == nil {
			alive.Store(false)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for .suspend")
		}
		time.Sleep(time.Millisecond)
	}
}

func appendClearTestEvent(dir, line string) error {
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(logDir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}
