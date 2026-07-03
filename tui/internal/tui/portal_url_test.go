package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestHelperFakePortal is not a real test — it is the body of the fake
// lingtai-portal executable. When invoked with GO_WANT_FAKE_PORTAL=1 it writes
// its own pid to GO_FAKE_PORTAL_PID_FILE (so the test can reap-check it), never
// writes .portal/port, and blocks — simulating a portal that hangs during
// startup so portalURL()'s readiness poll times out.
func TestHelperFakePortal(t *testing.T) {
	if os.Getenv("GO_WANT_FAKE_PORTAL") != "1" {
		return
	}
	if pidFile := os.Getenv("GO_FAKE_PORTAL_PID_FILE"); pidFile != "" {
		os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
	}
	// Block "forever" — the parent must kill us. Cap the sleep so a leaked
	// process still exits on its own and never wedges CI.
	time.Sleep(60 * time.Second)
	os.Exit(0)
}

// writeFakePortal drops a lingtai-portal wrapper into dir that re-executes the
// current test binary in TestHelperFakePortal mode. The wrapper exec's the test
// binary, so the running process keeps the wrapper's pid — the same pid
// portalURL() sees via cmd.Process.Pid and the helper records to pidFile.
func writeFakePortal(t *testing.T, dir, pidFile string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	name := "lingtai-portal"
	var script string
	if runtime.GOOS == "windows" {
		name += ".bat"
		script = "@echo off\r\n" +
			"set GO_WANT_FAKE_PORTAL=1\r\n" +
			"set GO_FAKE_PORTAL_PID_FILE=" + pidFile + "\r\n" +
			`"` + self + `" -test.run=TestHelperFakePortal` + "\r\n"
	} else {
		script = "#!/bin/sh\n" +
			"exec env GO_WANT_FAKE_PORTAL=1 " +
			"GO_FAKE_PORTAL_PID_FILE=" + shellQuote(pidFile) + " " +
			shellQuote(self) + " -test.run=TestHelperFakePortal\n"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake portal: %v", err)
	}
}

func shellQuote(s string) string {
	return "'" + s + "'"
}

// processAlive reports whether pid names a live process. On Unix, signal 0
// probes without delivering a signal.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, Signal(0) returns nil for a live process and an error once
	// reaped. On Windows FindProcess already fails for dead pids.
	if runtime.GOOS == "windows" {
		return true
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// TestPortalURLTimeoutKillsChild proves the #489 regression fix: when the
// portal never writes .portal/port, portalURL() must return an error AND must
// not leave the just-started portal process alive.
func TestPortalURLTimeoutKillsChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-alive probe via signal 0 is Unix-only")
	}

	// Fake portal on PATH; it records its pid to pidFile before blocking.
	binDir := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "portal.pid")
	writeFakePortal(t, binDir, pidFile)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Shrink the timeout so the test is fast.
	oldTimeout, oldPoll := portalReadyTimeout, portalReadyPoll
	portalReadyTimeout = 400 * time.Millisecond
	portalReadyPoll = 50 * time.Millisecond
	t.Cleanup(func() {
		portalReadyTimeout, portalReadyPoll = oldTimeout, oldPoll
	})

	projectDir := filepath.Join(t.TempDir(), "proj", ".lingtai")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	a := &App{projectDir: projectDir}

	url, err := a.portalURL()
	if err == nil {
		t.Fatalf("expected timeout error, got url=%q", url)
	}
	if url != "" {
		t.Fatalf("expected empty url on timeout, got %q", url)
	}

	// Read the pid the fake portal recorded for itself.
	pid := readPidFile(t, pidFile)
	if pid <= 0 {
		t.Fatal("fake portal never wrote its pid; cannot verify it was reaped")
	}

	// It must have been killed/reaped by portalURL's timeout path. Give the
	// kill+wait a brief moment to complete reaping.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && processAlive(pid) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("portal pid %d still alive after timeout; it should have been killed", pid)
	}
}

// readPidFile reads the pid the fake portal wrote. The child writes the file
// asynchronously after exec, so poll briefly for it to appear.
func readPidFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if data, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return pid
			}
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(20 * time.Millisecond)
	}
}
