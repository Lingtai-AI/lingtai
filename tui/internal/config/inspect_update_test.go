package config

import (
	"errors"
	"os"
	"testing"
)

// inspectUpdateOptions builds DoctorOptions wired to the shared read-only test
// fixtures: a recording fakeRunner, a canned PyPI/GitHub version client, and a
// non-symlink executable outside every Homebrew prefix (install method
// unknown) unless exe overrides it.
func inspectUpdateOptions(t *testing.T, runner *fakeRunner, currentTUI, latestPyPI, latestTUI, exe string) DoctorOptions {
	t.Helper()
	home, env := noDevHome(t)
	return DoctorOptions{
		CurrentTUIVersion: currentTUI,
		HTTPClient:        testVersionClient(t, latestPyPI, latestTUI),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "", errors.New("not found") },
		Executable:        func() (string, error) { return exe, nil },
		Readlink:          func(string) (string, error) { return "", errors.New("not a symlink") },
		Stat:              statAllExist,
		Home:              home,
		LookupEnv:         env,
	}
}

func TestInspectUpdateIssuesNoMutatingCommands(t *testing.T) {
	// Kernel out of date AND a TUI update available: even with everything
	// healable, InspectUpdate must only probe — no brew/pip/uv install.
	runner := &fakeRunner{versions: []string{"0.9.6"}}
	opts := inspectUpdateOptions(t, runner, "v0.8.0", "0.9.7", "v0.8.1", "/opt/homebrew/bin/lingtai-tui-under-test")
	plan := InspectUpdate(t.TempDir(), opts)
	assertNoMutatingCalls(t, runner.calls)
	if !plan.Kernel.NeedsUpdate {
		t.Fatalf("expected kernel NeedsUpdate=true for 0.9.6 -> 0.9.7: %+v", plan.Kernel)
	}
	if !plan.NeedsHeal() {
		t.Fatalf("out-of-date kernel must report NeedsHeal=true: %+v", plan)
	}
	if len(plan.Lines) == 0 {
		t.Fatal("InspectUpdate should carry the diagnostic lines it produced")
	}
}

func TestInspectUpdateEverythingCurrentNeedsNoHeal(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.7"}}
	opts := inspectUpdateOptions(t, runner, "v0.8.1", "0.9.7", "v0.8.1", "/usr/local/bin/lingtai-tui-under-test")
	plan := InspectUpdate(t.TempDir(), opts)
	assertNoMutatingCalls(t, runner.calls)
	if plan.NeedsHeal() {
		t.Fatalf("everything current must report NeedsHeal=false: %+v", plan)
	}
	if plan.TUIHealable() {
		t.Fatalf("up-to-date TUI must not be healable: %+v", plan.TUI)
	}
}

func TestInspectUpdateTUIUpdateUnknownMethodNotHealable(t *testing.T) {
	// TUI update available but the install method is unknown: only the Homebrew
	// backend actually mutates, so unknown/source installs must NOT trigger the
	// heal prompt — they get manual guidance in the diagnostic lines instead.
	runner := &fakeRunner{versions: []string{"0.9.7"}}
	opts := inspectUpdateOptions(t, runner, "v0.8.0", "0.9.7", "v0.8.1", "/usr/local/bin/lingtai-tui-under-test")
	plan := InspectUpdate(t.TempDir(), opts)
	assertNoMutatingCalls(t, runner.calls)
	if !plan.TUI.UpdateAvailable {
		t.Fatalf("expected TUI UpdateAvailable=true for v0.8.0 -> v0.8.1: %+v", plan.TUI)
	}
	if plan.TUI.Install.Method == TUIInstallMethodHomebrew {
		t.Fatalf("test setup expected a non-Homebrew install, got %+v", plan.TUI.Install)
	}
	if plan.TUIHealable() {
		t.Fatalf("non-Homebrew TUI install must not be healable: %+v", plan.TUI)
	}
	if plan.NeedsHeal() {
		t.Fatalf("TUI-only finding on a non-Homebrew install must not trigger heal: %+v", plan)
	}
}

func TestInspectUpdateTUIUpdateSourceMethodNotHealable(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := prefix + "/bin"
	exe := binDir + "/lingtai-tui"
	writeSourceInstallMetadata(t, globalDir, prefix, binDir, []string{exe})

	runner := &fakeRunner{versions: []string{"0.9.7"}}
	opts := inspectUpdateOptions(t, runner, "v0.8.0", "0.9.7", "v0.8.1", exe)
	plan := InspectUpdate(globalDir, opts)
	assertNoMutatingCalls(t, runner.calls)
	if plan.TUI.Install.Method != TUIInstallMethodSource {
		t.Fatalf("expected source install method, got %+v", plan.TUI.Install)
	}
	if !plan.TUI.UpdateAvailable {
		t.Fatalf("expected TUI UpdateAvailable=true: %+v", plan.TUI)
	}
	if plan.TUIHealable() || plan.NeedsHeal() {
		t.Fatalf("source install must not trigger heal: %+v", plan)
	}
}

func TestInspectUpdateTUIUpdateAutoBackendIsHealable(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.7"}}
	opts := inspectUpdateOptions(t, runner, "v0.8.0", "0.9.7", "v0.8.1", "/opt/homebrew/bin/lingtai-tui-under-test")
	plan := InspectUpdate(t.TempDir(), opts)
	assertNoMutatingCalls(t, runner.calls)
	if plan.TUI.Install.Method != TUIInstallMethodHomebrew {
		t.Fatalf("expected Homebrew install method, got %+v", plan.TUI.Install)
	}
	if !plan.TUIHealable() {
		t.Fatalf("Homebrew install with update available must be healable: %+v", plan.TUI)
	}
	if !plan.NeedsHeal() {
		t.Fatalf("healable TUI must report NeedsHeal=true: %+v", plan)
	}
	if plan.TUI.Latest != "v0.8.1" {
		t.Fatalf("plan should carry the latest TUI tag, got %q", plan.TUI.Latest)
	}
}

func TestInspectUpdateEditableKernelDoesNotTriggerHeal(t *testing.T) {
	runner := &fakeRunner{
		versions:       []string{"0.9.6"},
		editableSource: "file:///Users/dev/lingtai-kernel",
	}
	opts := inspectUpdateOptions(t, runner, "v0.8.1", "0.9.7", "v0.8.1", "/usr/local/bin/lingtai-tui-under-test")
	plan := InspectUpdate(t.TempDir(), opts)
	assertNoMutatingCalls(t, runner.calls)
	if !plan.Kernel.Editable {
		t.Fatalf("expected editable kernel: %+v", plan.Kernel)
	}
	if plan.Kernel.NeedsUpdate || plan.NeedsHeal() {
		t.Fatalf("editable kernel must not trigger heal: %+v", plan)
	}
}

func TestInspectUpdateMissingVenvTriggersHeal(t *testing.T) {
	// A missing runtime venv is a healable finding: the heal path rebuilds it
	// via RunKernelUpdate. Installed stays "" so the prompt shows the rebuild
	// action, not a version diff.
	runner := &fakeRunner{}
	opts := inspectUpdateOptions(t, runner, "v0.8.1", "0.9.7", "v0.8.1", "/usr/local/bin/lingtai-tui-under-test")
	opts.Stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	plan := InspectUpdate(t.TempDir(), opts)
	assertNoMutatingCalls(t, runner.calls)
	if !plan.Kernel.NeedsUpdate {
		t.Fatalf("missing venv must report kernel NeedsUpdate=true: %+v", plan.Kernel)
	}
	if plan.Kernel.Installed != "" {
		t.Fatalf("missing venv must leave Installed empty: %+v", plan.Kernel)
	}
	if !plan.NeedsHeal() {
		t.Fatalf("missing venv must trigger heal: %+v", plan)
	}
}
