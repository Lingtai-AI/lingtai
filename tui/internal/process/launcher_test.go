package process

import (
	"context"
	"errors"
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

func writeProjectCreateStub(t *testing.T, dir, logPath, stdout, stderr string, exitStatus int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX stub lingtai-agent not available on Windows")
	}
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\nprintf '%%b' %q\nprintf '%%b' %q >&2\nexit %d\n", logPath, stdout, stderr, exitStatus)
	if err := os.WriteFile(filepath.Join(dir, "lingtai-agent"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestKernelCLIForOS_UsesExpectedExecutableName(t *testing.T) {
	python := filepath.Join("runtime", "venv", "bin", "python")
	for _, tc := range []struct {
		name     string
		goos     string
		filename string
	}{
		{name: "unix", goos: "linux", filename: "lingtai-agent"},
		{name: "windows", goos: "windows", filename: "lingtai-agent.exe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := kernelCLIForOS(python, tc.goos)
			if filepath.Base(got) != tc.filename {
				t.Errorf("kernelCLIForOS filename = %q, want %q", filepath.Base(got), tc.filename)
			}
			if filepath.Dir(got) != filepath.Dir(python) {
				t.Errorf("kernelCLIForOS dir = %q, want %q", filepath.Dir(got), filepath.Dir(python))
			}
		})
	}
}

func TestCreateProject_UsesLiteralArgvAndCapturesSuccess(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "create-argv.log")
	stdout := "{\"agent_name\":\"alice\",\"preset_ref\":\"/presets/chosen.json\",\"status\":\"created\"}\n"
	writeProjectCreateStub(t, dir, logPath, stdout, "", 0)

	request := ProjectCreateRequest{
		Dir:          filepath.Join(dir, "project with spaces"),
		AgentName:    "alice",
		Preset:       filepath.Join(dir, "chosen preset.json"),
		CovenantFile: filepath.Join(dir, "covenant input.md"),
	}
	result, err := CreateProject(context.Background(), filepath.Join(dir, "python"), request)
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if result.Status != "created" || result.AgentName != request.AgentName || result.PresetRef != "/presets/chosen.json" || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Stdout != stdout || result.Stderr != "" {
		t.Fatalf("child output was not retained: %+v", result)
	}
	got := strings.Split(strings.TrimSuffix(readInvocationLog(t, logPath), "\n"), "\n")
	want := []string{
		"project", "create",
		"--dir", request.Dir,
		"--name", request.AgentName,
		"--preset", request.Preset,
		"--covenant-file", request.CovenantFile,
		"--json",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("literal argv = %#v, want %#v", got, want)
	}
}

func TestCreateProject_MapsOnlyExitOneJSONStderrAsHandlerFailure(t *testing.T) {
	dir := t.TempDir()
	writeProjectCreateStub(t, dir, filepath.Join(dir, "create-argv.log"), "untrusted stdout", `{"code":"invalid_preset","error":"bad preset","status":"error"}`, 1)

	_, err := CreateProject(context.Background(), filepath.Join(dir, "python"), ProjectCreateRequest{})
	var failure *ProjectCreateError
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want ProjectCreateError", err)
	}
	if failure.Kind != ProjectCreateHandlerFailure || failure.Code != "invalid_preset" || failure.Message != "bad preset" || failure.Stdout != "untrusted stdout" {
		t.Fatalf("unexpected handler failure: %+v", failure)
	}
}

func TestCreateProject_RejectsMalformedSuccessAndExitTwoAsTransport(t *testing.T) {
	for _, tc := range []struct {
		name      string
		stdout    string
		stderr    string
		exit      int
		agentName string
		reason    string
	}{
		{name: "multiple stdout documents", stdout: "{\"agent_name\":\"alice\",\"preset_ref\":\"/presets/chosen.json\",\"status\":\"created\"}\n{\"extra\":true}", exit: 0, agentName: "alice", reason: "malformed_result"},
		{name: "mismatched agent", stdout: "{\"agent_name\":\"bob\",\"preset_ref\":\"/presets/chosen.json\",\"status\":\"created\"}", exit: 0, agentName: "alice", reason: "malformed_result"},
		{name: "missing preset reference", stdout: "{\"agent_name\":\"alice\",\"status\":\"created\"}", exit: 0, agentName: "alice", reason: "malformed_result"},
		{name: "relative preset reference", stdout: "{\"agent_name\":\"alice\",\"preset_ref\":\"presets/chosen.json\",\"status\":\"created\"}", exit: 0, agentName: "alice", reason: "malformed_result"},
		{name: "argparse exit two", stderr: "usage: lingtai-agent project create", exit: 2, reason: "argument_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProjectCreateStub(t, dir, filepath.Join(dir, "create-argv.log"), tc.stdout, tc.stderr, tc.exit)
			_, err := CreateProject(context.Background(), filepath.Join(dir, "python"), ProjectCreateRequest{AgentName: tc.agentName})
			var failure *ProjectCreateError
			if !errors.As(err, &failure) {
				t.Fatalf("error = %v, want ProjectCreateError", err)
			}
			if failure.Kind != ProjectCreateTransportFailure || failure.Reason != tc.reason {
				t.Fatalf("unexpected transport failure: %+v", failure)
			}
		})
	}
}
