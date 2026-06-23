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

func TestInspectKernelDevCheckoutNeedsNoUpdate(t *testing.T) {
	// A PyPI-wheel runtime on a machine with a local dev checkout: the apply
	// step would reinstall editable rather than upgrade from PyPI, so inspect
	// must classify it as a dev/editable skip — not show a misleading PyPI
	// "X → Y" diff. This guards against inspect/apply drift.
	devRoot := t.TempDir()
	makeKernelCheckout(t, devRoot, "")
	runner := &fakeRunner{versions: []string{"0.9.6"}} // wheel install (not editable)
	status := inspectKernel(t.TempDir(), inspectKernelOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		Stat:       statAllExist,
		Home:       t.TempDir(),
		LookupEnv: func(key string) (string, bool) {
			if key == "LINGTAI_DEV_ROOT" {
				return devRoot, true
			}
			return "", false
		},
	})
	assertNoMutatingCalls(t, runner.calls)
	if !status.Editable {
		t.Fatalf("dev-checkout machine should classify as editable/dev skip: %+v", status)
	}
	if status.NeedsUpdate {
		t.Fatalf("dev checkout must report NeedsUpdate=false (no misleading PyPI diff): %+v", status)
	}
}

func TestRunKernelUpdateRunsKernelUpgradeOnce(t *testing.T) {
	// Non-editable, out-of-date install: RunKernelUpdate runs exactly one
	// uv/pip install --upgrade lingtai (the kernel path) and no brew.
	// Version probes consumed in order: pre-check import (repair gate),
	// UpgradePythonRuntime's installed read, then the post-upgrade verify.
	runner := &fakeRunner{versions: []string{"0.9.6", "0.9.6", "0.9.7"}}
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
	// Two version probes: the pre-check repair gate, then UpgradePythonRuntime's
	// installed read (which then hits the editable gate and stops).
	runner := &fakeRunner{
		versions:       []string{"0.9.6", "0.9.6"},
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

func TestRunKernelUpdateMissingVenvRebuildsThenUpgrades(t *testing.T) {
	// Mirror checkPythonRuntime: a missing venv is rebuilt before the upgrade.
	// The user already confirmed in /update, so this repair is authorized. The
	// venv is missing on the first Stat and present afterwards.
	globalDir := t.TempDir()
	python := VenvPython(RuntimeVenvDir(globalDir))
	runner := &fakeRunner{versions: []string{"0.9.6", "0.9.7"}}
	home, env := noDevHome(t)
	ensureCalled := false
	report := runKernelUpdate(globalDir, true, runKernelUpdateOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat: func(path string) (os.FileInfo, error) {
			if path == python && !ensureCalled {
				return nil, os.ErrNotExist
			}
			return fakeFileInfo{}, nil
		},
		Home:           home,
		LookupEnv:      env,
		EnsureVenvFunc: func(string) error { ensureCalled = true; return nil },
	})
	if !ensureCalled {
		t.Fatalf("missing venv must trigger EnsureVenvFunc: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "Python runtime venv created") {
		t.Fatalf("expected venv-created line: %+v", report.Lines)
	}
	if !report.Healthy {
		t.Fatalf("expected healthy report after rebuild + upgrade: %+v", report.Lines)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "brew") {
			t.Fatalf("RunKernelUpdate must never run brew: %#v", runner.calls)
		}
	}
}

func TestRunKernelUpdateRebuildFailureIsUnhealthy(t *testing.T) {
	// When the venv cannot import lingtai and the rebuild fails, the report is
	// unhealthy and no upgrade is attempted.
	runner := &fakeRunner{} // no versions queued => import lingtai fails
	home, env := noDevHome(t)
	report := runKernelUpdate(t.TempDir(), true, runKernelUpdateOptions{
		HTTPClient:     testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:         runner,
		LookPath:       func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:           statAllExist,
		Home:           home,
		LookupEnv:      env,
		EnsureVenvFunc: func(string) error { return errInjectedRebuild },
	})
	if report.Healthy {
		t.Fatalf("expected unhealthy report when rebuild fails: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "Failed to create Python runtime venv") {
		t.Fatalf("expected rebuild-failure line: %+v", report.Lines)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "brew") {
			t.Fatalf("RunKernelUpdate must never run brew: %#v", runner.calls)
		}
	}
}

var errInjectedRebuild = errVenvRebuild("injected rebuild failure")

type errVenvRebuild string

func (e errVenvRebuild) Error() string { return string(e) }

// guard against an accidental coupling to the file-search / TUI surfaces.
func TestRunKernelUpdateTouchesOnlyKernel(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.6", "0.9.6", "0.9.7"}}
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
