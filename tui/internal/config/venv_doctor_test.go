package config

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	versions []string
	failPip  bool
	calls    []string
}

func (r *fakeRunner) Run(name string, args ...string) CommandResult {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if strings.Contains(call, "import lingtai") {
		if len(r.versions) == 0 {
			return CommandResult{Err: errors.New("no version queued"), Stderr: "ModuleNotFoundError: lingtai"}
		}
		v := r.versions[0]
		r.versions = r.versions[1:]
		return CommandResult{Stdout: v + "\n"}
	}
	if r.failPip && strings.Contains(call, "pip install") {
		return CommandResult{Err: errors.New("exit status 1"), Stderr: "network down"}
	}
	return CommandResult{Stdout: "ok\n"}
}

func testVersionClient(t *testing.T, latestPyPI, latestTUI string) *http.Client {
	t.Helper()
	return &http.Client{Transport: versionRoundTripper{latestPyPI: latestPyPI, latestTUI: latestTUI}}
}

type versionRoundTripper struct {
	latestPyPI string
	latestTUI  string
}

func (rt versionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	switch {
	case req.URL.Host == "pypi.org" && req.URL.Path == "/pypi/lingtai/json":
		body = fmt.Sprintf(`{"info":{"version":%q}}`, rt.latestPyPI)
	case req.URL.Host == "api.github.com" && req.URL.Path == "/repos/Lingtai-AI/lingtai/releases/latest":
		body = fmt.Sprintf(`{"tag_name":%q}`, rt.latestTUI)
	default:
		body = `{}`
	}
	return &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func statAllExist(string) (os.FileInfo, error) {
	return fakeFileInfo{}, nil
}

type fakeFileInfo struct{ os.FileInfo }

func TestEnsureRuntimeChecksUpgradeAfterCreatingVenv(t *testing.T) {
	var ensured bool
	var checked bool
	updated, err := ensureRuntimeWithOptions("/tmp/lingtai-test", RuntimeEnsureOptions{
		NeedsVenvFunc: func(string) bool { return true },
		EnsureVenvFunc: func(string) error {
			ensured = true
			return nil
		},
		CheckUpgradeFunc: func(string) bool {
			if !ensured {
				t.Fatalf("CheckUpgradeFunc ran before EnsureVenvFunc")
			}
			checked = true
			return true
		},
	})
	if err != nil {
		t.Fatalf("EnsureRuntime err = %v", err)
	}
	if !ensured || !checked || !updated {
		t.Fatalf("expected ensure, check, and updated=true; ensured=%v checked=%v updated=%v", ensured, checked, updated)
	}
}

func TestEnsureRuntimeChecksUpgradeWhenVenvAlreadyExists(t *testing.T) {
	var ensured bool
	var checked bool
	updated, err := ensureRuntimeWithOptions("/tmp/lingtai-test", RuntimeEnsureOptions{
		NeedsVenvFunc: func(string) bool { return false },
		EnsureVenvFunc: func(string) error {
			ensured = true
			return nil
		},
		CheckUpgradeFunc: func(string) bool {
			checked = true
			return false
		},
	})
	if err != nil {
		t.Fatalf("EnsureRuntime err = %v", err)
	}
	if ensured {
		t.Fatalf("did not expect EnsureVenvFunc when NeedsVenvFunc=false")
	}
	if !checked {
		t.Fatalf("expected CheckUpgradeFunc even when venv already exists")
	}
	if updated {
		t.Fatalf("expected updated=false")
	}
}

func TestEnsureRuntimeSkipsUpgradeWhenEnsureFails(t *testing.T) {
	var checked bool
	_, err := ensureRuntimeWithOptions("/tmp/lingtai-test", RuntimeEnsureOptions{
		NeedsVenvFunc:  func(string) bool { return true },
		EnsureVenvFunc: func(string) error { return errors.New("venv boom") },
		CheckUpgradeFunc: func(string) bool {
			checked = true
			return true
		},
	})
	if err == nil || !strings.Contains(err.Error(), "venv boom") {
		t.Fatalf("expected venv boom error, got %v", err)
	}
	if checked {
		t.Fatalf("CheckUpgradeFunc should not run after EnsureVenvFunc failure")
	}
}

func TestUpgradePythonRuntimeReportsCommandFailure(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.6"}, failPip: true}
	result := UpgradePythonRuntime(t.TempDir(), true, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
	})
	if result.Healthy {
		t.Fatalf("expected unhealthy result on pip failure: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "upgrade command failed") || !containsLine(result.Lines, "network down") {
		t.Fatalf("expected command failure and stderr in lines: %+v", result.Lines)
	}
	if result.Updated {
		t.Fatalf("failed upgrade must not report Updated")
	}
}

func TestUpgradePythonRuntimeVerifiesPostInstallVersion(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.6", "0.9.7"}}
	result := UpgradePythonRuntime(t.TempDir(), false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "", errors.New("no uv") },
		Stat:       statAllExist,
	})
	if !result.Healthy || !result.Updated {
		t.Fatalf("expected healthy updated result: %+v", result)
	}
	if !containsCall(runner.calls, "pip install --upgrade lingtai") {
		t.Fatalf("expected pip upgrade call, got %#v", runner.calls)
	}
	if !containsLine(result.Lines, "after upgrade: 0.9.7") {
		t.Fatalf("expected post-upgrade version line: %+v", result.Lines)
	}
}

func TestRunDoctorUpdateMissingVenvRunsEnsureVenv(t *testing.T) {
	globalDir := t.TempDir()
	pythonPath := VenvPython(RuntimeVenvDir(globalDir))
	runner := &fakeRunner{versions: []string{"0.9.7", "0.9.7"}}
	ensureCalled := false
	report := RunDoctorUpdate(globalDir, DoctorOptions{
		CurrentTUIVersion: "v0.8.1",
		ForcePython:       true,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath: func(name string) (string, error) {
			if name == "brew" {
				return "", errors.New("no brew")
			}
			return "", errors.New("no " + name)
		},
		Executable: func() (string, error) { return filepath.Join(globalDir, "lingtai-tui"), nil },
		Readlink:   func(string) (string, error) { return "", os.ErrInvalid },
		Stat: func(path string) (os.FileInfo, error) {
			if path == pythonPath && !ensureCalled {
				return nil, os.ErrNotExist
			}
			return fakeFileInfo{}, nil
		},
		EnsureVenvFunc: func(string) error {
			ensureCalled = true
			return nil
		},
	})
	if !ensureCalled {
		t.Fatalf("expected EnsureVenvFunc to be called")
	}
	if !report.Healthy {
		t.Fatalf("expected healthy report after successful ensure: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "Python runtime venv created") {
		t.Fatalf("expected venv created line: %+v", report.Lines)
	}
}

func TestRunDoctorUpdateTUIUpToDate(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.7", "0.9.7"}}
	report := RunDoctorUpdate(t.TempDir(), DoctorOptions{
		CurrentTUIVersion: "v0.8.1",
		ForcePython:       false,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "", errors.New("not found") },
		Executable:        func() (string, error) { return "/opt/homebrew/bin/lingtai-tui", nil },
		Readlink:          func(string) (string, error) { return "", os.ErrInvalid },
		Stat:              statAllExist,
	})
	if !report.Healthy {
		t.Fatalf("expected healthy report: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "TUI is up to date") {
		t.Fatalf("expected up-to-date TUI line: %+v", report.Lines)
	}
	if containsCall(runner.calls, "brew upgrade") {
		t.Fatalf("up-to-date TUI must not run brew: %#v", runner.calls)
	}
}

func TestRunDoctorUpdateTUIOutdatedWithoutForceDoesNotRunBrew(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.7", "0.9.7"}}
	report := RunDoctorUpdate(t.TempDir(), DoctorOptions{
		CurrentTUIVersion: "v0.8.0",
		ForceTUI:          false,
		ForcePython:       false,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "/opt/homebrew/bin/brew", nil },
		Executable:        func() (string, error) { return "/opt/homebrew/bin/lingtai-tui", nil },
		Readlink:          func(string) (string, error) { return "", os.ErrInvalid },
		Stat:              statAllExist,
	})
	if !report.Healthy {
		t.Fatalf("availability-only check should remain healthy: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "TUI update available") {
		t.Fatalf("expected update availability warning: %+v", report.Lines)
	}
	if containsCall(runner.calls, "brew") {
		t.Fatalf("ForceTUI=false must not run brew, got %#v", runner.calls)
	}
}

func TestRunDoctorUpdateBrokenVenvRunsEnsureVenv(t *testing.T) {
	globalDir := t.TempDir()
	runner := &fakeRunner{versions: []string{"0.9.7", "0.9.7"}}
	ensureCalled := false
	firstImport := true
	originalRun := runner.Run
	runnerWithBrokenFirstImport := commandRunnerFunc(func(name string, args ...string) CommandResult {
		call := name + " " + strings.Join(args, " ")
		if strings.Contains(call, "import lingtai") && firstImport {
			firstImport = false
			runner.calls = append(runner.calls, call)
			return CommandResult{Err: errors.New("exit status 1"), Stderr: "ModuleNotFoundError: lingtai"}
		}
		return originalRun(name, args...)
	})
	report := RunDoctorUpdate(globalDir, DoctorOptions{
		CurrentTUIVersion: "v0.8.1",
		ForcePython:       true,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runnerWithBrokenFirstImport,
		LookPath:          func(string) (string, error) { return "", errors.New("not found") },
		Executable:        func() (string, error) { return filepath.Join(globalDir, "lingtai-tui"), nil },
		Readlink:          func(string) (string, error) { return "", os.ErrInvalid },
		Stat:              statAllExist,
		EnsureVenvFunc: func(string) error {
			ensureCalled = true
			return nil
		},
	})
	if !ensureCalled {
		t.Fatalf("expected EnsureVenvFunc for broken import")
	}
	if !report.Healthy {
		t.Fatalf("expected healthy report after repair: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "cannot import lingtai") {
		t.Fatalf("expected broken import warning: %+v", report.Lines)
	}
}

type commandRunnerFunc func(string, ...string) CommandResult

func (f commandRunnerFunc) Run(name string, args ...string) CommandResult {
	return f(name, args...)
}

func TestRunDoctorUpdateTUIOutdatedForceRunsBrewInOrder(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.7", "0.9.7"}}
	report := RunDoctorUpdate(t.TempDir(), DoctorOptions{
		CurrentTUIVersion: "v0.8.0",
		ForceTUI:          true,
		ForcePython:       false,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath: func(name string) (string, error) {
			if name == "brew" {
				return "/opt/homebrew/bin/brew", nil
			}
			return "", errors.New("not found")
		},
		Executable: func() (string, error) { return "/opt/homebrew/bin/lingtai-tui", nil },
		Readlink:   func(string) (string, error) { return "", os.ErrInvalid },
		Stat:       statAllExist,
	})
	if !report.Healthy {
		t.Fatalf("expected healthy report: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "Brew upgrade finished") {
		t.Fatalf("expected brew completion guidance: %+v", report.Lines)
	}
	want := []string{
		"/opt/homebrew/bin/brew update",
		"/opt/homebrew/bin/brew upgrade lingtai-ai/lingtai/lingtai-tui",
	}
	if !containsCall(runner.calls, want[0]) || !containsCall(runner.calls, want[1]) {
		t.Fatalf("expected brew calls %v, got %#v", want, runner.calls)
	}
	if indexOfCall(runner.calls, want[0]) > indexOfCall(runner.calls, want[1]) {
		t.Fatalf("expected brew update before brew upgrade, got %#v", runner.calls)
	}
}

func TestRunDoctorUpdateEnsureVenvSuccessButPythonStillMissingFails(t *testing.T) {
	globalDir := t.TempDir()
	pythonPath := VenvPython(RuntimeVenvDir(globalDir))
	ensureCalled := false
	report := RunDoctorUpdate(globalDir, DoctorOptions{
		CurrentTUIVersion: "v0.8.1",
		ForcePython:       true,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            &fakeRunner{},
		LookPath:          func(string) (string, error) { return "", errors.New("not found") },
		Executable:        func() (string, error) { return filepath.Join(globalDir, "lingtai-tui"), nil },
		Readlink:          func(string) (string, error) { return "", os.ErrInvalid },
		Stat: func(path string) (os.FileInfo, error) {
			if path == pythonPath {
				return nil, os.ErrNotExist
			}
			return fakeFileInfo{}, nil
		},
		EnsureVenvFunc: func(string) error {
			ensureCalled = true
			return nil
		},
	})
	if !ensureCalled {
		t.Fatalf("expected EnsureVenvFunc to be called")
	}
	if report.Healthy {
		t.Fatalf("expected unhealthy report when python remains missing: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "still missing") {
		t.Fatalf("expected still-missing failure line: %+v", report.Lines)
	}
}

func TestRunDoctorUpdateReportsTUISymlinkCaveat(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.7", "0.9.7"}}
	report := RunDoctorUpdate(t.TempDir(), DoctorOptions{
		CurrentTUIVersion: "v0.8.1",
		ForcePython:       false,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "", errors.New("not found") },
		Executable:        func() (string, error) { return "/opt/homebrew/bin/lingtai-tui", nil },
		Readlink:          func(string) (string, error) { return "/Users/me/dev/lingtai/tui/bin/lingtai-tui", nil },
		Stat:              statAllExist,
	})
	if !report.Healthy {
		t.Fatalf("expected healthy report: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "executable is a symlink") {
		t.Fatalf("expected symlink caveat: %+v", report.Lines)
	}
}

func containsLine(lines []DoctorLine, sub string) bool {
	for _, line := range lines {
		if strings.Contains(line.Text, sub) {
			return true
		}
	}
	return false
}

func containsCall(calls []string, sub string) bool {
	for _, call := range calls {
		if strings.Contains(call, sub) {
			return true
		}
	}
	return false
}

func indexOfCall(calls []string, sub string) int {
	for i, call := range calls {
		if strings.Contains(call, sub) {
			return i
		}
	}
	return len(calls)
}
