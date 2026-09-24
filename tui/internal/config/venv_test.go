package config

import (
	"errors"
	"os"
	"path/filepath"
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

func mkdirTestVenv(t *testing.T, venvPath string) {
	t.Helper()
	if err := os.MkdirAll(venvPath, 0o755); err != nil {
		t.Fatalf("mkdir venv: %v", err)
	}
}

func TestManagedPythonCompatibilityRejectsPython314OnDarwin(t *testing.T) {
	if managedPythonCompatible(managedPythonInfo{Major: 3, Minor: 14, Platform: "darwin", Machine: "arm64"}, "darwin", "arm64") {
		t.Fatal("Python 3.14 must not be accepted for the managed macOS runtime")
	}
	if !managedPythonCompatible(managedPythonInfo{Major: 3, Minor: 13, Platform: "darwin", Machine: "arm64"}, "darwin", "arm64") {
		t.Fatal("Python 3.13 should be accepted for the managed macOS runtime")
	}
}

func TestFindCompatiblePythonSkipsPython314OnDarwin(t *testing.T) {
	paths := map[string]string{
		"python3.13": "/opt/homebrew/bin/python3.13",
		"python3.12": "/opt/homebrew/bin/python3.12",
		"python3":    "/opt/homebrew/bin/python3",
		"python":     "/opt/homebrew/bin/python",
	}
	lookPath := func(name string) (string, error) {
		path, ok := paths[name]
		if !ok {
			return "", errors.New("not found")
		}
		return path, nil
	}
	runner := commandRunnerFunc(func(name string, _ ...string) CommandResult {
		if strings.HasSuffix(name, "python3.12") {
			return CommandResult{Stdout: `{"major":3,"minor":13,"platform":"darwin","machine":"arm64"}`}
		}
		return CommandResult{Stdout: `{"major":3,"minor":14,"platform":"darwin","machine":"arm64"}`}
	})
	path, err := findCompatiblePythonWith(lookPath, runner, "darwin", "arm64")
	if err != nil {
		t.Fatalf("findCompatiblePythonWith: %v", err)
	}
	if path != paths["python3.12"] {
		t.Fatalf("path = %q, want compatible fallback %q after rejecting 3.14", path, paths["python3.12"])
	}
}

func TestFindCompatiblePythonReportsDarwinPolicyError(t *testing.T) {
	lookPath := func(string) (string, error) { return "/usr/local/bin/python3", nil }
	runner := commandRunnerFunc(func(string, ...string) CommandResult {
		return CommandResult{Stdout: `{"major":3,"minor":14,"platform":"darwin","machine":"arm64"}`}
	})
	_, err := findCompatiblePythonWith(lookPath, runner, "darwin", "arm64")
	if err == nil || !strings.Contains(err.Error(), "Python 3.11-3.13") {
		t.Fatalf("error = %v, want actionable managed macOS range", err)
	}
}

func TestRuntimeEnvMarkerMissingIsLegacy(t *testing.T) {
	venvPath := filepath.Join(t.TempDir(), "venv")
	runner := commandRunnerFunc(func(string, ...string) CommandResult {
		return CommandResult{Stdout: `{"status":"missing"}` + "\n"}
	})
	state, err := runtimeEnvMarkerStateForVenv(venvPath, runner)
	if err != nil {
		t.Fatalf("missing marker should not error: %v", err)
	}
	if state != runtimeEnvMarkerMissing {
		t.Fatalf("state = %s, want %s", state, runtimeEnvMarkerMissing)
	}
}

func TestRuntimeEnvMarkerDetectsDelegatedMismatch(t *testing.T) {
	venvPath := filepath.Join(t.TempDir(), "venv")
	runner := commandRunnerFunc(func(string, ...string) CommandResult {
		return CommandResult{Stdout: `{"status":"mismatch","detail":"platform mismatch"}` + "\n"}
	})

	state, err := runtimeEnvMarkerStateForVenv(venvPath, runner)
	if err != nil {
		t.Fatalf("mismatched marker should not error: %v", err)
	}
	if state != runtimeEnvMarkerMismatch {
		t.Fatalf("state = %s, want %s", state, runtimeEnvMarkerMismatch)
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

func TestInspectKernelSemanticVersionMatchNeedsNoUpdate(t *testing.T) {
	runner := &fakeRunner{versions: []string{"v1.0.1"}}
	home, env := noDevHome(t)
	status := inspectKernel(t.TempDir(), inspectKernelOptions{
		HTTPClient: testVersionClient(t, "1.0.1", "v1.0.0"),
		Runner:     runner,
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	if status.NeedsUpdate || !containsLine(status.Lines, "runtime is up to date") {
		t.Fatalf("semantically equal release versions must be up to date: %+v", status)
	}
}

func TestInspectKernelNewerThanLatestDoesNotPromptDowngrade(t *testing.T) {
	runner := &fakeRunner{versions: []string{"1.0.1"}}
	home, env := noDevHome(t)
	status := inspectKernel(t.TempDir(), inspectKernelOptions{
		HTTPClient: testVersionClient(t, "0.19.3", "v1.0.0"),
		Runner:     runner,
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	assertNoMutatingCalls(t, runner.calls)
	if status.NeedsUpdate {
		t.Fatalf("installed newer than latest must not prompt a downgrade: %+v", status)
	}
	if !containsLine(status.Lines, "skipping downgrade") {
		t.Fatalf("expected an explicit newer-than-latest diagnostic: %+v", status.Lines)
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
	// uv/pip install --upgrade <release source URL> (the kernel path) and no brew.
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
		if strings.Contains(call, "releases/download/") && strings.Contains(call, ".tar.gz#sha256=") {
			upgrades++
		}
		if strings.Contains(call, "install --upgrade lingtai") {
			t.Fatalf("the kernel upgrade must never request the package name from an index: %q", call)
		}
		if strings.Contains(call, "brew") {
			t.Fatalf("RunKernelUpdate must not run brew, got %q", call)
		}
	}
	if upgrades != 1 {
		t.Fatalf("expected exactly one kernel upgrade command, got %d (%#v)", upgrades, runner.calls)
	}
}

func TestUpgradePythonRuntimeNewerThanLatestNeverDowngrades(t *testing.T) {
	globalDir := t.TempDir()
	mkdirTestVenv(t, RuntimeVenvDir(globalDir))
	runner := &fakeRunner{versions: []string{"1.0.1"}}
	home, env := noDevHome(t)
	result := UpgradePythonRuntime(globalDir, true, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.19.3", "v1.0.0"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	if !result.Healthy || result.Updated {
		t.Fatalf("newer runtime must remain unchanged: %+v", result)
	}
	assertNoMutatingCalls(t, runner.calls)
	if !containsLine(result.Lines, "skipping downgrade") {
		t.Fatalf("expected an explicit newer-than-latest diagnostic: %+v", result.Lines)
	}
}

func TestUpgradePythonRuntimeForceAllowsSemanticVersionMatch(t *testing.T) {
	globalDir := t.TempDir()
	mkdirTestVenv(t, RuntimeVenvDir(globalDir))
	runner := &fakeRunner{versions: []string{"v1.0.1", "1.0.1"}}
	home, env := noDevHome(t)
	result := UpgradePythonRuntime(globalDir, true, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "1.0.1", "v1.0.0"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	if !result.Healthy || !result.Updated {
		t.Fatalf("force repair must proceed for semantically equal releases: %+v", result)
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
		if strings.Contains(call, "releases/download/") || strings.Contains(call, "install --upgrade lingtai") {
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

func TestRuntimeRepairRestoresPreviousVenvOnFailure(t *testing.T) {
	venv := filepath.Join(t.TempDir(), "runtime", "venv")
	if err := os.MkdirAll(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(venv, "previous-data")
	if err := os.WriteFile(marker, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := withRuntimeVenvRollback(venv, func() error {
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old venv should be out of the build path: %v", err)
		}
		if err := os.MkdirAll(venv, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(venv, "partial-install"), []byte("bad"), 0o600); err != nil {
			return err
		}
		return errInjectedRebuild
	})
	if !errors.Is(err, errInjectedRebuild) {
		t.Fatalf("repair error = %v", err)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "kept" {
		t.Fatalf("previous venv was not restored: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(venv, "partial-install")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial install survived: %v", err)
	}
}

func TestRuntimeRepairRemovesIncompleteFreshVenv(t *testing.T) {
	venv := filepath.Join(t.TempDir(), "runtime", "venv")
	err := withRuntimeVenvRollback(venv, func() error {
		if err := os.MkdirAll(venv, 0o755); err != nil {
			return err
		}
		return errInjectedRebuild
	})
	if !errors.Is(err, errInjectedRebuild) {
		t.Fatalf("repair error = %v", err)
	}
	if _, err := os.Stat(venv); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete fresh venv survived: %v", err)
	}
}

func TestRuntimeRepairRemovesBackupAfterSuccess(t *testing.T) {
	venv := filepath.Join(t.TempDir(), "runtime", "venv")
	if err := os.MkdirAll(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(venv, "previous-data"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := withRuntimeVenvRollback(venv, func() error {
		if err := os.MkdirAll(venv, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(venv, "new-runtime"), []byte("ready"), 0o600)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(venv, "new-runtime")); err != nil {
		t.Fatalf("replacement missing: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(venv), "backups", "venv-pre-repair-*"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("repair backup was not removed: %v, %v", backups, err)
	}
}
