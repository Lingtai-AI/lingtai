package config

import (
	"os"
	"strings"
	"testing"
)

// mutatingCall reports whether a recorded command would install or upgrade
// anything (brew/pip/uv install). Read-only probes (python -c "import lingtai")
// and the editable-detect / version probes are not mutating.
func mutatingCall(call string) bool {
	switch {
	case strings.Contains(call, "brew"):
		return true
	case strings.Contains(call, "pip install"):
		return true
	case strings.Contains(call, "pip") && strings.Contains(call, "install"):
		return true
	default:
		return false
	}
}

func assertNoMutatingCalls(t *testing.T, calls []string) {
	t.Helper()
	for _, call := range calls {
		if mutatingCall(call) {
			t.Fatalf("expected no install/brew/pip/uv install commands, but ran: %q (all: %#v)", call, calls)
		}
	}
}

func TestInspectKernelIssuesNoMutatingCommands(t *testing.T) {
	// installed != latest so an update IS available; InspectKernel must still
	// only probe (read-only) and never run brew/pip/uv install.
	runner := &fakeRunner{versions: []string{"0.9.6"}}
	home, env := noDevHome(t)
	status := inspectKernel(t.TempDir(), inspectKernelOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	assertNoMutatingCalls(t, runner.calls)
	if status.Installed != "0.9.6" || status.Latest != "0.9.7" {
		t.Fatalf("unexpected versions: %+v", status)
	}
	if !status.NeedsUpdate {
		t.Fatalf("expected NeedsUpdate=true for 0.9.6 -> 0.9.7: %+v", status)
	}
	if status.Editable {
		t.Fatalf("non-editable install should report Editable=false: %+v", status)
	}
}

func TestInspectKernelUpToDateNeedsNoUpdate(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.7"}}
	home, env := noDevHome(t)
	status := inspectKernel(t.TempDir(), inspectKernelOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	assertNoMutatingCalls(t, runner.calls)
	if status.NeedsUpdate {
		t.Fatalf("installed==latest must report NeedsUpdate=false: %+v", status)
	}
}

func TestInspectKernelEditableNeedsNoUpdate(t *testing.T) {
	runner := &fakeRunner{
		versions:       []string{"0.9.6"},
		editableSource: "file:///Users/dev/lingtai-kernel",
	}
	home, env := noDevHome(t)
	status := inspectKernel(t.TempDir(), inspectKernelOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	assertNoMutatingCalls(t, runner.calls)
	if !status.Editable {
		t.Fatalf("expected Editable=true: %+v", status)
	}
	if status.NeedsUpdate {
		t.Fatalf("editable install must report NeedsUpdate=false: %+v", status)
	}
}

func TestRunKernelUpdateRunsKernelUpgradeOnce(t *testing.T) {
	// Non-editable, out-of-date install: RunKernelUpdate runs exactly one
	// uv/pip install --upgrade lingtai (the kernel path) and no brew.
	runner := &fakeRunner{versions: []string{"0.9.6", "0.9.7"}}
	home, env := noDevHome(t)
	report := runKernelUpdate(t.TempDir(), true, runKernelUpdateOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	if !report.Healthy {
		t.Fatalf("expected healthy report: %+v", report.Lines)
	}
	upgrades := 0
	for _, call := range runner.calls {
		if strings.Contains(call, "install --upgrade lingtai") {
			upgrades++
		}
		if strings.Contains(call, "brew") {
			t.Fatalf("RunKernelUpdate must not run brew, got %q", call)
		}
	}
	if upgrades != 1 {
		t.Fatalf("expected exactly one kernel upgrade command, got %d (%#v)", upgrades, runner.calls)
	}
}

func TestRunKernelUpdateSkipsEditableInstall(t *testing.T) {
	runner := &fakeRunner{
		versions:       []string{"0.9.6"},
		editableSource: "file:///Users/dev/lingtai-kernel",
	}
	home, env := noDevHome(t)
	report := runKernelUpdate(t.TempDir(), true, runKernelUpdateOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	if !report.Healthy {
		t.Fatalf("editable install must remain Healthy: %+v", report.Lines)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "install --upgrade lingtai") {
			t.Fatalf("editable install must not run kernel upgrade: %#v", runner.calls)
		}
		if strings.Contains(call, "brew") {
			t.Fatalf("RunKernelUpdate must never run brew: %#v", runner.calls)
		}
	}
}

func TestRunKernelUpdateMissingVenvReported(t *testing.T) {
	// Venv python missing: UpgradePythonRuntime reports it as a warning and
	// does not run an upgrade. RunKernelUpdate surfaces that without brew.
	runner := &fakeRunner{}
	home, env := noDevHome(t)
	report := runKernelUpdate(t.TempDir(), true, runKernelUpdateOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		Home:       home,
		LookupEnv:  env,
	})
	if !containsLine(report.Lines, "venv not found") {
		t.Fatalf("expected venv-not-found warning: %+v", report.Lines)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "brew") {
			t.Fatalf("RunKernelUpdate must never run brew: %#v", runner.calls)
		}
	}
}

// guard against an accidental coupling to the file-search / TUI surfaces.
func TestRunKernelUpdateTouchesOnlyKernel(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.6", "0.9.7"}}
	home, env := noDevHome(t)
	_ = runKernelUpdate(t.TempDir(), true, runKernelUpdateOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	for _, call := range runner.calls {
		if strings.Contains(call, "file_io_sidecar") {
			t.Fatalf("RunKernelUpdate must not probe the file-search sidecar: %#v", runner.calls)
		}
	}
}

func TestRunKernelUpdateReportsImportFailure(t *testing.T) {
	// No version queued => import lingtai fails. The kernel path reports it.
	runner := &fakeRunner{}
	home, env := noDevHome(t)
	report := runKernelUpdate(t.TempDir(), true, runKernelUpdateOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	if report.Healthy {
		t.Fatalf("expected unhealthy report on import failure: %+v", report.Lines)
	}
}
