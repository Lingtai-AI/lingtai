package process

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInitProject(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")

	if err := InitProject(lingtaiDir); err != nil {
		t.Fatalf("init: %v", err)
	}

	humanManifest := filepath.Join(lingtaiDir, "human", ".agent.json")
	if _, err := os.Stat(humanManifest); os.IsNotExist(err) {
		t.Error("human/.agent.json not created")
	}

	for _, sub := range []string{"mailbox/inbox", "mailbox/sent", "mailbox/archive"} {
		path := filepath.Join(lingtaiDir, "human", sub)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("%s not created", sub)
		}
	}

	contactsPath := filepath.Join(lingtaiDir, "human", "mailbox", "contacts.json")
	data, err := os.ReadFile(contactsPath)
	if err != nil {
		t.Fatalf("read contacts: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("contacts = %q, want %q", string(data), "[]")
	}

	if _, err := os.Stat(filepath.Join(lingtaiDir, "meta.json")); !os.IsNotExist(err) {
		t.Errorf("InitProject wrote retired migration progress meta.json (err=%v)", err)
	}
}

// writeStubPython writes a POSIX shell stub that records every invocation
// (one argument per line) to logPath and then exits with the given status.
// It lets tests prove whether an interpreter process is (or is not) started
// during agent launch without running a real Python.
func writeStubPython(t *testing.T, dir, logPath string, exitStatus int) string {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >> %q\nexit %d\n", logPath, exitStatus)
	p := filepath.Join(dir, "python")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeStubLingtaiAgent(t *testing.T, dir, logPath string, exitStatus int) string {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >> %q\nexit %d\n", logPath, exitStatus)
	p := filepath.Join(dir, "lingtai-agent")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func readInvocationLog(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	return string(data)
}

// TestLaunchAgentUnsafeRejectsInvalidAddonKeyBeforeInterpreter proves that a
// legacy addon key containing a source-boundary character (semicolon) is
// rejected at the validation boundary during launch and that NO interpreter
// process is started for it: the stub python's invocation log must never be
// created.
func TestLaunchAgentUnsafeRejectsInvalidAddonKeyBeforeInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX stub interpreter not available on Windows")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "python-calls.log")
	stub := writeStubPython(t, dir, logPath, 0)

	agentDir := filepath.Join(dir, "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initJSON := `{"addons": {"imap;__import__('os').system('id')": {}}}`
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), []byte(initJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd, err := launchAgentUnsafe(stub, agentDir)
	if err == nil {
		t.Fatal("expected validation error for invalid legacy addon key")
	}
	if cmd != nil {
		t.Errorf("expected no command for invalid addon key, got %v", cmd)
	}
	if !strings.Contains(err.Error(), "invalid legacy addon key") {
		t.Fatalf("error = %v, want validation-boundary error", err)
	}
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Fatalf("interpreter was started despite invalid addon key; invocation log exists: %v", logPath)
	}
}

// TestLaunchAgentUnsafeValidAddonKeyKeepsLaunchBehavior proves that valid
// legacy addon keys retain the existing importability check AND the existing
// launch behavior: the stub interpreter is first invoked for the import
// probe, then the agent command (-m lingtai run) is started.
func TestLaunchAgentUnsafeValidAddonKeyKeepsLaunchBehavior(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX stub interpreter not available on Windows")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "python-calls.log")
	stub := writeStubPython(t, dir, logPath, 0)
	writeStubLingtaiAgent(t, dir, logPath, 0)

	agentDir := filepath.Join(dir, "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initJSON := `{"addons": {"imap": {}}}`
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), []byte(initJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd, err := launchAgentUnsafe(stub, agentDir)
	if err != nil {
		t.Fatalf("launchAgentUnsafe = %v, want nil for valid addon key", err)
	}
	if cmd == nil {
		t.Fatal("expected a started agent command")
	}

	// The stub exits immediately, so wait for it to reap the child.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}

	calls := readInvocationLog(t, logPath)
	for _, want := range []string{"-c", "import lingtai.addons.imap", "run"} {
		if !strings.Contains(calls, want) {
			t.Errorf("interpreter log missing %q; got:\n%s", want, calls)
		}
	}
}
