//go:build !windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPurgeMainFailsLoudOnScanError(t *testing.T) {
	if os.Getenv("LINGTAI_TEST_PURGE_SCAN_FAIL") == "1" {
		os.Args = []string{"lingtai-tui", "purge"}
		purgeMain()
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestPurgeMainFailsLoudOnScanError$")
	cmd.Env = append(os.Environ(),
		"LINGTAI_TEST_PURGE_SCAN_FAIL=1",
		"PATH="+t.TempDir(), // ps is unreachable, so the scan command fails
	)
	cmd.Stdin = strings.NewReader("n\n") // never reached; keeps a regression from hanging on the kill prompt
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected nonzero exit when ps is unavailable, got err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if code := exitErr.ExitCode(); code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "error running ps") {
		t.Fatalf("stderr = %q, want it to report the ps failure", stderr.String())
	}
	if strings.Contains(stdout.String(), "No lingtai processes found") {
		t.Fatalf("stdout = %q, scan failure must not masquerade as an empty process list", stdout.String())
	}
}

func TestPurgeUnixProcessesReportsObservedOutcomes(t *testing.T) {
	origSignalProcess := unixSignalProcess
	defer func() {
		unixSignalProcess = origSignalProcess
	}()

	alive := map[int]bool{
		101: true,
		202: true,
		303: true,
	}
	unixSignalProcess = func(pid int, sig syscall.Signal) error {
		switch sig {
		case syscall.SIGTERM:
			if pid == 101 {
				alive[pid] = false
			}
			return nil
		case syscall.SIGKILL:
			if pid == 303 {
				return errors.New("permission denied")
			}
			alive[pid] = false
			return nil
		case syscall.Signal(0):
			if alive[pid] {
				return nil
			}
			return syscall.ESRCH
		default:
			return nil
		}
	}

	result := purgeUnixProcesses([]purgeProc{
		{pid: 101},
		{pid: 202},
		{pid: 303},
	}, 0)

	if result.purged != 2 || result.failed != 1 {
		t.Fatalf("purge result = %+v, want purged=2 failed=1", result)
	}
	if waitForUnixProcessExit(303, 0, time.Millisecond) {
		t.Fatalf("waitForUnixProcessExit reported a still-signalable process as exited")
	}
}

func TestPurgeUnixProcessesDoesNotCountUnobservableProcessAsPurged(t *testing.T) {
	origSignalProcess := unixSignalProcess
	defer func() {
		unixSignalProcess = origSignalProcess
	}()

	// 404 exists but belongs to another user: every signal, including the
	// signal-0 probe, fails with EPERM. 505 accepts TERM and KILL, but its probe
	// keeps failing with EPERM, so the post-KILL wait never observes it gone.
	killed := map[int]bool{}
	unixSignalProcess = func(pid int, sig syscall.Signal) error {
		if pid == 404 {
			return syscall.EPERM
		}
		switch sig {
		case syscall.SIGKILL:
			killed[pid] = true
			return nil
		case syscall.Signal(0):
			return syscall.EPERM
		default:
			return nil
		}
	}

	result := purgeUnixProcesses([]purgeProc{
		{pid: 404},
		{pid: 505},
	}, 0)

	if result.purged != 0 || result.failed != 2 {
		t.Fatalf("purge result = %+v, want purged=0 failed=2", result)
	}
	if !killed[505] {
		t.Fatalf("pid 505 was not sent SIGKILL; the post-KILL wait was not exercised")
	}
}

func TestUnixProcessGoneRequiresProofOfAbsence(t *testing.T) {
	origSignalProcess := unixSignalProcess
	defer func() {
		unixSignalProcess = origSignalProcess
	}()

	for _, tc := range []struct {
		name     string
		probeErr error
		gone     bool
	}{
		{name: "signalable", probeErr: nil, gone: false},
		{name: "permission denied", probeErr: syscall.EPERM, gone: false},
		{name: "unexpected error", probeErr: syscall.EIO, gone: false},
		{name: "no such process", probeErr: syscall.ESRCH, gone: true},
		{name: "wrapped no such process", probeErr: fmt.Errorf("probe: %w", syscall.ESRCH), gone: true},
		{name: "os process done", probeErr: os.ErrProcessDone, gone: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unixSignalProcess = func(pid int, sig syscall.Signal) error {
				if sig != syscall.Signal(0) {
					t.Fatalf("unexpected signal %v sent to pid %d", sig, pid)
				}
				return tc.probeErr
			}
			if got := unixProcessGone(4242); got != tc.gone {
				t.Fatalf("unixProcessGone = %v, want %v", got, tc.gone)
			}
			if got := waitForUnixProcessExit(4242, 0, time.Millisecond); got != tc.gone {
				t.Fatalf("waitForUnixProcessExit = %v, want %v", got, tc.gone)
			}
		})
	}
}

func TestUnixProcessGoneRecognizesReapedChild(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	pid := cmd.Process.Pid
	if unixProcessGone(pid) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("unixProcessGone(%d) = true for a running child", pid)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait() // reap, so the PID no longer exists

	// The real os.Process.Signal reports ESRCH as os.ErrProcessDone.
	if !unixProcessGone(pid) {
		t.Fatalf("unixProcessGone(%d) = false for a reaped child", pid)
	}
}
