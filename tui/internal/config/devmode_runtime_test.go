package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func noDevEnvLookup(string) (string, bool) { return "", false }

func devEnvLookup(root string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if key == "LINGTAI_DEV_ROOT" {
			return root, true
		}
		return "", false
	}
}

// TestUpgradeConvertsPyPIToEditableWhenDevCheckoutsPresent is the core fix:
// a working PyPI install on a machine with local dev checkouts must be
// reinstalled editable on the next launch, replacing the wheel.
func TestUpgradeConvertsPyPIToEditableWhenDevCheckoutsPresent(t *testing.T) {
	home := t.TempDir()
	devRoot := filepath.Join(home, "devroot")
	kernel := makeKernelCheckout(t, home, "devroot")

	// Importable PyPI install (direct_url probe -> WHEEL by default). Two
	// versions queued: the initial import and the post-install verify import.
	runner := &fakeRunner{versions: []string{"0.9.6", "0.9.6"}}
	result := UpgradePythonRuntime(t.TempDir(), false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  devEnvLookup(devRoot),
	})

	if !result.Healthy {
		t.Fatalf("dev-mode conversion must stay healthy: %+v", result.Lines)
	}
	if !containsCall(runner.calls, "pip install -e "+kernel) {
		t.Fatalf("expected editable install of kernel, got %#v", runner.calls)
	}
	// Must NOT run the PyPI upgrade path.
	if containsKernelWheelInstall(runner.calls) {
		t.Fatalf("dev-mode conversion must not run the release-wheel upgrade: %#v", runner.calls)
	}
	if !containsLine(result.Lines, "dev checkout") {
		t.Fatalf("expected a dev-checkout info line: %+v", result.Lines)
	}
}

// TestUpgradeSkipsReinstallWhenAlreadyEditableForCheckout: if the venv is
// already editable for the local kernel, do not reinstall every launch.
func TestUpgradeSkipsReinstallWhenAlreadyEditableForCheckout(t *testing.T) {
	home := t.TempDir()
	devRoot := filepath.Join(home, "devroot")
	kernel := makeKernelCheckout(t, home, "devroot")

	runner := &fakeRunner{
		versions:       []string{"0.9.6"},
		editableSource: "file://" + kernel,
	}
	result := UpgradePythonRuntime(t.TempDir(), false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  devEnvLookup(devRoot),
	})

	if !result.Healthy {
		t.Fatalf("already-editable must stay healthy: %+v", result.Lines)
	}
	if result.Updated {
		t.Fatalf("already-editable must not report Updated")
	}
	if containsCall(runner.calls, "pip install -e") {
		t.Fatalf("already-editable-for-checkout must not reinstall: %#v", runner.calls)
	}
	if containsKernelWheelInstall(runner.calls) {
		t.Fatalf("already-editable must not run the release-wheel upgrade: %#v", runner.calls)
	}
}

// TestUpgradeReinstallsEditableForDifferentCheckout: editable, but pointing at
// a different/stale source than the local checkout -> reinstall against local.
func TestUpgradeReinstallsEditableForDifferentCheckout(t *testing.T) {
	home := t.TempDir()
	devRoot := filepath.Join(home, "devroot")
	kernel := makeKernelCheckout(t, home, "devroot")

	runner := &fakeRunner{
		versions:       []string{"0.9.6", "0.9.6"},
		editableSource: "file:///somewhere/else/lingtai-kernel",
	}
	result := UpgradePythonRuntime(t.TempDir(), false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  devEnvLookup(devRoot),
	})

	if !result.Healthy {
		t.Fatalf("expected healthy: %+v", result.Lines)
	}
	if !containsCall(runner.calls, "pip install -e "+kernel) {
		t.Fatalf("expected reinstall against local checkout, got %#v", runner.calls)
	}
}

// TestUpgradeNoDevCheckoutsKeepsPyPIBehavior: regression guard for normal
// users — no checkouts means the existing PyPI up-to-date path runs unchanged.
func TestUpgradeNoDevCheckoutsKeepsPyPIBehavior(t *testing.T) {
	home := t.TempDir() // empty: no checkouts

	runner := &fakeRunner{versions: []string{"0.9.7"}}
	result := UpgradePythonRuntime(t.TempDir(), false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  noDevEnvLookup,
	})

	if !result.Healthy {
		t.Fatalf("expected healthy: %+v", result.Lines)
	}
	if containsCall(runner.calls, "pip install -e") {
		t.Fatalf("no checkouts must never trigger editable install: %#v", runner.calls)
	}
	if !containsLine(result.Lines, "up to date") {
		t.Fatalf("expected the normal up-to-date path: %+v", result.Lines)
	}
}

// TestUpgradeNoDevCheckoutsExistingEditableStillSkips: regression guard for the
// pre-existing editable-skip behavior when no local checkout is discoverable
// (e.g. checkout was moved). We must not clobber an editable install just
// because we couldn't locate its source.
func TestUpgradeNoDevCheckoutsExistingEditableStillSkips(t *testing.T) {
	home := t.TempDir() // no discoverable checkouts

	runner := &fakeRunner{
		versions:       []string{"0.9.6"},
		editableSource: "file:///moved/away/lingtai-kernel",
	}
	result := UpgradePythonRuntime(t.TempDir(), false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  noDevEnvLookup,
	})

	if result.Updated {
		t.Fatalf("editable install must not be clobbered when no checkout found")
	}
	if containsKernelWheelInstall(runner.calls) {
		t.Fatalf("existing editable must not trigger the release-wheel upgrade: %#v", runner.calls)
	}
	if !containsLine(result.Lines, "editable install") {
		t.Fatalf("expected editable-install skip line: %+v", result.Lines)
	}
}

// TestUpgradeDevEditableInstallFailureIsReported: if the editable reinstall
// command fails, the result must be unhealthy and surface the error.
func TestUpgradeDevEditableInstallFailureIsReported(t *testing.T) {
	home := t.TempDir()
	devRoot := filepath.Join(home, "devroot")
	makeKernelCheckout(t, home, "devroot")

	runner := &fakeRunner{versions: []string{"0.9.6"}, failPip: true}
	result := UpgradePythonRuntime(t.TempDir(), false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  devEnvLookup(devRoot),
	})

	if result.Healthy {
		t.Fatalf("expected unhealthy on editable install failure: %+v", result.Lines)
	}
	if result.Updated {
		t.Fatalf("failed editable install must not report Updated")
	}
}

// TestUpgradeDevModeFallsBackToPipWithoutUV: with no uv, the editable install
// uses the venv's pip.
func TestUpgradeDevModeFallsBackToPipWithoutUV(t *testing.T) {
	home := t.TempDir()
	devRoot := filepath.Join(home, "devroot")
	kernel := makeKernelCheckout(t, home, "devroot")

	runner := &fakeRunner{versions: []string{"0.9.6", "0.9.6"}}
	globalDir := t.TempDir()
	result := UpgradePythonRuntime(globalDir, false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "", errors.New("no uv") },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  devEnvLookup(devRoot),
	})
	if !result.Healthy {
		t.Fatalf("expected healthy: %+v", result.Lines)
	}
	wantPip := filepath.Join(filepath.Dir(VenvPython(RuntimeVenvDir(globalDir))), "pip")
	if !containsCall(runner.calls, wantPip+" install -e "+kernel) {
		t.Fatalf("expected pip editable install %q, got %#v", wantPip, runner.calls)
	}
}

// Sanity: the default (nil Home/LookupEnv) path must still compile/run and
// behave like a normal user when os.UserHomeDir has no checkouts. We simulate
// by pointing Home at an empty dir via the seam, which exercises the same code.
func TestUpgradeDefaultHomeSeamUnset(t *testing.T) {
	// Home unset -> falls back to os.UserHomeDir(); on CI that home has no
	// checkouts, so this must not attempt an editable install.
	runner := &fakeRunner{versions: []string{"0.9.7"}}
	result := UpgradePythonRuntime(t.TempDir(), false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		LookupEnv:  func(string) (string, bool) { return "", false },
		// Home intentionally left empty to exercise the default branch.
		Home: unsetHomeForTest(t),
	})
	if !result.Healthy {
		t.Fatalf("expected healthy: %+v", result.Lines)
	}
	if containsCall(runner.calls, "pip install -e") {
		t.Fatalf("unexpected editable install in a home without checkouts: %#v", runner.calls)
	}
}

// unsetHomeForTest returns a guaranteed-empty home dir (no checkouts).
func unsetHomeForTest(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if _, err := os.Stat(filepath.Join(d, "work")); err == nil {
		t.Fatalf("temp home unexpectedly populated")
	}
	return d
}
