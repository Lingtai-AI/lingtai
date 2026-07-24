package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectTUIUpdaterRoutesInstallMethods(t *testing.T) {
	tests := []struct {
		name   string
		method TUIInstallMethod
		want   TUIInstallMethod
	}{
		{name: "homebrew", method: TUIInstallMethodHomebrew, want: TUIInstallMethodHomebrew},
		{name: "source", method: TUIInstallMethodSource, want: TUIInstallMethodSource},
		{name: "unknown", method: TUIInstallMethodUnknown, want: TUIInstallMethodUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			updater := SelectTUIUpdater(TUIInstallInfo{Method: tc.method})
			if got := updater.InstallMethod(); got != tc.want {
				t.Fatalf("InstallMethod() = %s, want %s", got, tc.want)
			}
		})
	}
}

// runsBrewCommand reports whether any call invokes brew as the command
// itself (leading token "brew" or a path ending in "/brew"), as opposed to
// merely mentioning "brew" as a substring — which false-positives on this
// test's own t.TempDir() paths, since they embed the test name and
// "Homebrew" contains "brew".
func runsBrewCommand(calls []string) bool {
	for _, call := range calls {
		cmd := strings.SplitN(call, " ", 2)[0]
		if cmd == "brew" || strings.HasSuffix(cmd, "/brew") {
			return true
		}
	}
	return false
}

func TestHomebrewTUIUpdaterMigratesToNativeInstallAndVerifies(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	target := filepath.Join(binDir, "lingtai-tui")
	runner := &homebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"}

	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		LatestVersion:       "v0.8.1",
		GlobalDir:           globalDir,
		Runner:              runner,
		Stat:                statAllExist,
		SourceInstallScript: "/tmp/install.sh",
		LookPath:            func(string) (string, error) { return target, nil },
	})

	if !result.Healthy || !result.Updated {
		t.Fatalf("expected healthy migrated result: %+v", result)
	}
	// No ConfirmHomebrewCleanup was supplied (nil, like every non-interactive
	// caller), so cleanup must not run and the truthful pending state stands
	// even though the TUI binary itself migrated successfully.
	if !result.NeedsManualCleanup {
		t.Fatalf("no cleanup consent given; Homebrew removal must stay pending: %+v", result)
	}
	if !containsCall(runner.calls, "bash /tmp/install.sh --version v0.8.1 --non-interactive") {
		t.Fatalf("expected native fresh-install call, got %#v", runner.calls)
	}
	if runsBrewCommand(runner.calls) {
		t.Fatalf("homebrew migration must never run brew, got %#v", runner.calls)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "--update") {
			t.Fatalf("homebrew migration must not use --update (no prior source install exists), got %#v", runner.calls)
		}
	}
	if !containsLine(result.Lines, "Native install metadata verified") {
		t.Fatalf("expected metadata verification line: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "Python runtime verified after migration") {
		t.Fatalf("expected runtime verification line: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "Migrated from Homebrew to the native installer") {
		t.Fatalf("expected migration completion line: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "brew uninstall") {
		t.Fatalf("expected manual brew uninstall guidance since cleanup was not confirmed: %+v", result.Lines)
	}
}

// TestHomebrewTUIUpdaterCleanupConfirmedRemovesHomebrewAfterVerifiedPATHTakeover
// proves the full "y" path (IMPLEMENTATION ACCEPTANCE #2/#3): once install,
// verification, and PATH takeover all succeed, an explicit
// ConfirmHomebrewCleanup=true triggers exactly one injected UninstallHomebrew
// call, and success requires the post-uninstall LookPath to resolve to the
// verified native target before NeedsManualCleanup clears.
func TestHomebrewTUIUpdaterCleanupConfirmedRemovesHomebrewAfterVerifiedPATHTakeover(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	target := filepath.Join(binDir, "lingtai-tui")
	runner := &homebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"}
	uninstallCalls := 0

	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		LatestVersion:          "v0.8.1",
		GlobalDir:              globalDir,
		Runner:                 runner,
		Stat:                   statAllExist,
		SourceInstallScript:    "/tmp/install.sh",
		LookPath:               func(string) (string, error) { return target, nil },
		ConfirmHomebrewCleanup: func() bool { return true },
		UninstallHomebrew: func() error {
			uninstallCalls++
			return nil
		},
	})

	if !result.Healthy || !result.Updated {
		t.Fatalf("expected healthy migrated result: %+v", result)
	}
	if result.NeedsManualCleanup {
		t.Fatalf("confirmed cleanup with successful uninstall and matching post-uninstall PATH should not need manual cleanup: %+v", result)
	}
	if uninstallCalls != 1 {
		t.Fatalf("expected exactly one injected uninstall call, got %d", uninstallCalls)
	}
	if runsBrewCommand(runner.calls) {
		t.Fatalf("the injected CommandRunner must never see a brew call; uninstall goes through UninstallHomebrew only, got %#v", runner.calls)
	}
	if !containsLine(result.Lines, "Migration complete") {
		t.Fatalf("expected migration-complete confirmation line: %+v", result.Lines)
	}
}

// TestHomebrewTUIUpdaterCleanupDeclinedDoesNotUninstall proves an explicit
// false answer never calls UninstallHomebrew.
func TestHomebrewTUIUpdaterCleanupDeclinedDoesNotUninstall(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	target := filepath.Join(binDir, "lingtai-tui")
	runner := &homebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"}
	uninstallCalls := 0

	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		LatestVersion:          "v0.8.1",
		GlobalDir:              globalDir,
		Runner:                 runner,
		Stat:                   statAllExist,
		SourceInstallScript:    "/tmp/install.sh",
		LookPath:               func(string) (string, error) { return target, nil },
		ConfirmHomebrewCleanup: func() bool { return false },
		UninstallHomebrew: func() error {
			uninstallCalls++
			return nil
		},
	})

	if !result.Updated {
		t.Fatalf("declining cleanup must not undo a successful TUI migration: %+v", result)
	}
	if !result.NeedsManualCleanup {
		t.Fatalf("declined cleanup must stay truthfully pending: %+v", result)
	}
	if uninstallCalls != 0 {
		t.Fatalf("declined cleanup must never call UninstallHomebrew, got %d calls", uninstallCalls)
	}
}

// TestHomebrewTUIUpdaterCleanupUninstallFailureIsNotSuccess proves a failed
// uninstall keeps NeedsManualCleanup true and does not silently continue as
// if cleanup succeeded.
func TestHomebrewTUIUpdaterCleanupUninstallFailureIsNotSuccess(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	target := filepath.Join(binDir, "lingtai-tui")
	runner := &homebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"}

	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		LatestVersion:          "v0.8.1",
		GlobalDir:              globalDir,
		Runner:                 runner,
		Stat:                   statAllExist,
		SourceInstallScript:    "/tmp/install.sh",
		LookPath:               func(string) (string, error) { return target, nil },
		ConfirmHomebrewCleanup: func() bool { return true },
		UninstallHomebrew: func() error {
			return errors.New("brew uninstall exited 1")
		},
	})

	if !result.Updated {
		t.Fatalf("a failed uninstall must not undo the already-successful TUI migration: %+v", result)
	}
	if !result.NeedsManualCleanup {
		t.Fatalf("failed uninstall must not be reported as success: %+v", result)
	}
	if !containsLine(result.Lines, "failed") {
		t.Fatalf("expected failure line for the uninstall attempt: %+v", result.Lines)
	}
}

// TestHomebrewTUIUpdaterCleanupPostUninstallPATHStillShadowedIsNotSuccess
// proves the acceptance invariant: a currently-running Homebrew process is
// never treated as proof after removal — only a fresh ordinary PATH
// resolution is. If LookPath still resolves elsewhere after a successful
// uninstall (for example a stale shell hash), NeedsManualCleanup must stay
// true.
func TestHomebrewTUIUpdaterCleanupPostUninstallPATHStillShadowedIsNotSuccess(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	target := filepath.Join(binDir, "lingtai-tui")
	homebrewShadow := "/opt/homebrew/bin/lingtai-tui"
	runner := &homebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"}
	uninstallCalls := 0
	lookPathCalls := 0

	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		LatestVersion:       "v0.8.1",
		GlobalDir:           globalDir,
		Runner:              runner,
		Stat:                statAllExist,
		SourceInstallScript: "/tmp/install.sh",
		LookPath: func(string) (string, error) {
			lookPathCalls++
			if lookPathCalls == 1 {
				// Pre-cleanup PATH-takeover check: native install confirmed.
				return target, nil
			}
			// Post-uninstall re-resolution: a stale shell hash (or any other
			// reason) still resolves the old path even though brew uninstall
			// itself reported success — this must not be treated as proof.
			return homebrewShadow, nil
		},
		ConfirmHomebrewCleanup: func() bool { return true },
		UninstallHomebrew: func() error {
			uninstallCalls++
			return nil
		},
	})

	if !result.Updated {
		t.Fatalf("the TUI migration itself succeeded before cleanup ran and must stay reported as updated: %+v", result)
	}
	if !result.NeedsManualCleanup {
		t.Fatalf("post-uninstall PATH still resolving the old path must not be reported as cleanup success: %+v", result)
	}
	if uninstallCalls != 1 {
		t.Fatalf("expected exactly one uninstall call even though PATH stayed shadowed after it, got %d", uninstallCalls)
	}
	if lookPathCalls < 2 {
		t.Fatalf("expected LookPath to be re-resolved after uninstall (fresh PATH proof, not the running process), got %d calls", lookPathCalls)
	}
}

// TestHomebrewTUIUpdaterMigrationNotTakenOverByPATHDoesNotClaimSuccess proves
// the Apple-Silicon failure scenario from the adversarial review: install.sh
// puts the native binary in a bin dir that is NOT ahead of Homebrew on PATH.
// The install and every artifact verification step succeed, but Upgrade must
// not report Updated/"Migrated!" for a binary the shell will not actually run
// — it must report NeedsManualCleanup with truthful guidance instead.
func TestHomebrewTUIUpdaterMigrationNotTakenOverByPATHDoesNotClaimSuccess(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	runner := &homebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"}
	homebrewShadow := "/opt/homebrew/bin/lingtai-tui"

	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		LatestVersion:       "v0.8.1",
		GlobalDir:           globalDir,
		Runner:              runner,
		Stat:                statAllExist,
		SourceInstallScript: "/tmp/install.sh",
		LookPath:            func(string) (string, error) { return homebrewShadow, nil },
	})

	if !result.Healthy {
		t.Fatalf("a shadowed-but-verified install is not a failure, should stay healthy: %+v", result)
	}
	if result.Updated {
		t.Fatalf("must not report Updated when the shell still resolves the old Homebrew binary: %+v", result)
	}
	if !result.NeedsManualCleanup {
		t.Fatalf("expected NeedsManualCleanup for an unresolved PATH takeover: %+v", result)
	}
	if containsLine(result.Lines, "Migrated from Homebrew to the native installer") {
		t.Fatalf("must not claim migration completion when PATH still resolves Homebrew: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "still resolves") {
		t.Fatalf("expected honest shadowed-PATH guidance: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "brew uninstall") {
		t.Fatalf("expected manual brew uninstall guidance as user-initiated next step: %+v", result.Lines)
	}
	if runsBrewCommand(runner.calls) {
		t.Fatalf("must never run brew itself, only mention it as guidance: %#v", runner.calls)
	}
}

// TestHomebrewTUIUpdaterDuplicateInstallDoesNotRerunInstaller proves
// idempotence (BLOCKER-2): once a native install already exists and is
// verified-but-shadowed (DuplicateNativeInstall), a second Upgrade call must
// not re-run install.sh at all — it must short-circuit straight to the
// manual-cleanup-required report.
func TestHomebrewTUIUpdaterDuplicateInstallDoesNotRerunInstaller(t *testing.T) {
	runner := &homebrewMigrationRunner{t: t}

	result := RunTUIUpdate(TUIInstallInfo{
		Method:                 TUIInstallMethodHomebrew,
		DuplicateNativeInstall: true,
		DuplicateNativeDetail:  "native install already verified at /tmp/native/bin/lingtai-tui (version v0.8.1), but executable under /opt/homebrew (opt/homebrew) is still resolved first on PATH",
	}, TUIUpdateOptions{
		LatestVersion: "v0.8.1",
		Runner:        runner,
	})

	if len(runner.calls) != 0 {
		t.Fatalf("duplicate-install detection must not run install.sh again, got %#v", runner.calls)
	}
	if !result.Healthy {
		t.Fatalf("duplicate-install state is not a failure: %+v", result)
	}
	if result.Updated {
		t.Fatalf("duplicate-install detection must not report Updated: %+v", result)
	}
	if !result.NeedsManualCleanup {
		t.Fatalf("expected NeedsManualCleanup for a repeat duplicate-install detection: %+v", result)
	}
	if !containsLine(result.Lines, "already exists and was verified") {
		t.Fatalf("expected already-verified guidance, got %+v", result.Lines)
	}
	if runsBrewCommand(runner.calls) {
		t.Fatalf("must never run brew, got %#v", runner.calls)
	}
}

// TestHomebrewTUIUpdaterPreExistingDuplicateCleanupConfirmedRemovesHomebrew
// proves the same owning cleanup primitive (attemptHomebrewCleanup) also
// completes cleanup for a duplicate detected on a LATER update — not just a
// freshly created one — using the structured DuplicateNativeTarget path
// rather than re-running install.sh.
func TestHomebrewTUIUpdaterPreExistingDuplicateCleanupConfirmedRemovesHomebrew(t *testing.T) {
	runner := &homebrewMigrationRunner{t: t}
	target := "/tmp/native/bin/lingtai-tui"
	uninstallCalls := 0

	result := RunTUIUpdate(TUIInstallInfo{
		Method:                 TUIInstallMethodHomebrew,
		DuplicateNativeInstall: true,
		DuplicateNativeDetail:  "native install already verified at " + target + " (version v0.8.1), but executable under /opt/homebrew is still resolved first on PATH",
		DuplicateNativeTarget:  target,
	}, TUIUpdateOptions{
		LatestVersion:          "v0.8.1",
		Runner:                 runner,
		LookPath:               func(string) (string, error) { return target, nil },
		ConfirmHomebrewCleanup: func() bool { return true },
		UninstallHomebrew: func() error {
			uninstallCalls++
			return nil
		},
	})

	if len(runner.calls) != 0 {
		t.Fatalf("duplicate-install cleanup must not run install.sh, got %#v", runner.calls)
	}
	if uninstallCalls != 1 {
		t.Fatalf("expected exactly one injected uninstall call, got %d", uninstallCalls)
	}
	if result.NeedsManualCleanup {
		t.Fatalf("confirmed cleanup with matching post-uninstall PATH should not need manual cleanup: %+v", result)
	}
	if !result.Healthy {
		t.Fatalf("successful cleanup is not a failure: %+v", result)
	}
}

func TestHomebrewTUIUpdaterMigrationFailureLeavesHomebrewUsable(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	runner := &homebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7", failInstall: true}

	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		LatestVersion:       "v0.8.1",
		GlobalDir:           globalDir,
		Runner:              runner,
		Stat:                statAllExist,
		SourceInstallScript: "/tmp/install.sh",
	})

	if result.Healthy || result.Updated {
		t.Fatalf("expected failed migration result: %+v", result)
	}
	if result.Err == nil {
		t.Fatalf("expected an error on failed migration")
	}
	if !containsLine(result.Lines, "Native installer failed") {
		t.Fatalf("expected installer-failed line: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "Homebrew-installed lingtai-tui is untouched and still usable") {
		t.Fatalf("expected rollback guidance on failure: %+v", result.Lines)
	}
	if runsBrewCommand(runner.calls) {
		t.Fatalf("homebrew migration must never run brew even on failure, got %#v", runner.calls)
	}
}

func TestHomebrewTUIUpdaterMigrationRequiresKnownRelease(t *testing.T) {
	runner := &fakeRunner{}
	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		Runner: runner,
	})

	if result.Healthy || result.Updated {
		t.Fatalf("migration without a release tag should fail: %+v", result)
	}
	if !containsLine(result.Lines, "needs a known release tag") {
		t.Fatalf("expected release-tag failure, got %+v", result.Lines)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("migration should fail before running any command, got %#v", runner.calls)
	}
}

func TestSourceTUIUpdaterRunsInstallScriptAndVerifiesRuntime(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	exe := filepath.Join(binDir, "lingtai-tui")
	writeSourceInstallMetadataVersion(t, globalDir, prefix, binDir, "v0.8.0", []string{exe})
	runner := &sourceUpdateRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"}

	result := RunTUIUpdate(TUIInstallInfo{
		Method:       TUIInstallMethodSource,
		MetadataPath: filepath.Join(globalDir, "install.json"),
	}, TUIUpdateOptions{
		LatestVersion:       "v0.8.1",
		GlobalDir:           globalDir,
		Runner:              runner,
		Stat:                statAllExist,
		SourceInstallScript: "/tmp/install.sh",
	})

	if !result.Healthy || !result.Updated {
		t.Fatalf("expected healthy source update: %+v", result)
	}
	if !containsCall(runner.calls, "bash /tmp/install.sh --update --prefix "+prefix+" --version v0.8.1 --non-interactive") {
		t.Fatalf("expected installer update call, got %#v", runner.calls)
	}
	if containsCall(runner.calls, "brew") {
		t.Fatalf("source updater must not run brew, got %#v", runner.calls)
	}
	if !containsLine(result.Lines, "Source install metadata verified") {
		t.Fatalf("expected metadata verification line: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "Python runtime verified after source update") {
		t.Fatalf("expected runtime verification line: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "Source/user-local TUI update verified") {
		t.Fatalf("expected source update completion line: %+v", result.Lines)
	}
}

func TestSourceTUIUpdaterRequiresKnownRelease(t *testing.T) {
	runner := &fakeRunner{}
	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodSource}, TUIUpdateOptions{
		Runner: runner,
	})

	if result.Healthy || result.Updated {
		t.Fatalf("source update without a release tag should fail: %+v", result)
	}
	if !containsLine(result.Lines, "needs a known release tag") {
		t.Fatalf("expected release-tag failure, got %+v", result.Lines)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("source updater should fail before commands, got %#v", runner.calls)
	}
}

func TestUnknownTUIUpdaterDoesNotRunBrew(t *testing.T) {
	runner := &fakeRunner{}
	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodUnknown}, TUIUpdateOptions{
		LatestVersion: "v0.8.1",
		Runner:        runner,
		LookPath:      func(string) (string, error) { return "/opt/homebrew/bin/brew", nil },
	})

	if !result.Healthy {
		t.Fatalf("unknown updater guidance should not fail doctor-style update: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "TUI install method is unknown") {
		t.Fatalf("expected unknown updater guidance, got %+v", result.Lines)
	}
	if containsCall(runner.calls, "brew") {
		t.Fatalf("unknown updater must not run brew, got %#v", runner.calls)
	}
}

func TestTUIUpdaterSourceMetadataFailureDoesNotRunBrew(t *testing.T) {
	tests := []struct {
		name       string
		method     TUIInstallMethod
		wantLine   string
		wantHealth bool
	}{
		{name: "source", method: TUIInstallMethodSource, wantLine: "install metadata", wantHealth: false},
		{name: "unknown", method: TUIInstallMethodUnknown, wantLine: "TUI install method is unknown", wantHealth: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			result := RunTUIUpdate(TUIInstallInfo{Method: tc.method}, TUIUpdateOptions{
				LatestVersion: "v0.8.1",
				Runner:        runner,
				LookPath:      func(string) (string, error) { return "/opt/homebrew/bin/brew", nil },
			})
			if result.Healthy != tc.wantHealth {
				t.Fatalf("Healthy = %v, want %v: %+v", result.Healthy, tc.wantHealth, result.Lines)
			}
			if !containsLine(result.Lines, tc.wantLine) {
				t.Fatalf("expected guidance line %q, got %+v", tc.wantLine, result.Lines)
			}
			if containsCall(runner.calls, "brew") {
				t.Fatalf("%s updater must not run brew, got %#v", tc.name, runner.calls)
			}
		})
	}
}

func TestManualTUIUpdateHomebrewMigratesToNativeInstall(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	target := filepath.Join(binDir, "lingtai-tui")
	runner := &homebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"}
	report := RunManualTUIUpdate(globalDir, ManualTUIUpdateOptions{
		CurrentTUIVersion: "v0.8.0",
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		Stat:              statAllExist,
		Executable:        func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		LookupEnv:         func(string) (string, bool) { return "", false },
		LookPath:          func(string) (string, error) { return target, nil },
	})

	if !report.Healthy || !report.Updated {
		t.Fatalf("expected healthy migrated result: %+v", report)
	}
	// self-update is non-interactive: it never supplies ConfirmHomebrewCleanup,
	// so PATH takeover succeeding does not by itself clear the pending-cleanup
	// state — Homebrew removal requires the interactive startup prompt.
	if !report.NeedsManualCleanup {
		t.Fatalf("self-update must never uninstall Homebrew itself; cleanup should stay pending: %+v", report)
	}
	if !containsLine(report.Lines, "TUI install method: homebrew") {
		t.Fatalf("expected homebrew install method line: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "TUI update available: v0.8.0 -> v0.8.1") {
		t.Fatalf("expected update-available line: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "Migrated from Homebrew to the native installer") {
		t.Fatalf("expected migration completion line: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "brew uninstall") {
		t.Fatalf("expected manual brew uninstall guidance since self-update never prompts/uninstalls: %+v", report.Lines)
	}
	if runsBrewCommand(runner.calls) {
		t.Fatalf("self-update for a homebrew install must never run brew, got %#v", runner.calls)
	}
}

// TestManualTUIUpdateHomebrewDuplicateInstallSkipsInstaller proves
// idempotence at the self-update entry point: when detection reports
// DuplicateNativeInstall (a prior migration already installed and verified a
// native binary that Homebrew still shadows on PATH), `lingtai-tui
// self-update` must report the manual-cleanup state and must not re-run
// install.sh.
func TestManualTUIUpdateHomebrewDuplicateInstallSkipsInstaller(t *testing.T) {
	globalDir := t.TempDir()
	nativePrefix := t.TempDir()
	nativeBinDir := filepath.Join(nativePrefix, "bin")
	writeSourceInstallMetadataVersion(t, globalDir, nativePrefix, nativeBinDir, "v0.8.1", []string{filepath.Join(nativeBinDir, "lingtai-tui")})
	runner := &fakeRunner{}

	report := RunManualTUIUpdate(globalDir, ManualTUIUpdateOptions{
		CurrentTUIVersion: "v0.8.0",
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		Executable:        func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		LookupEnv:         func(string) (string, bool) { return "", false },
	})

	if !report.Healthy {
		t.Fatalf("duplicate-install state is not a failure: %+v", report)
	}
	if report.Updated {
		t.Fatalf("duplicate-install detection must not report Updated: %+v", report)
	}
	if !report.NeedsManualCleanup {
		t.Fatalf("expected NeedsManualCleanup, got %+v", report)
	}
	if !containsLine(report.Lines, "already exists and was verified") {
		t.Fatalf("expected already-verified guidance, got %+v", report.Lines)
	}
	if runsBrewCommand(runner.calls) {
		t.Fatalf("must never run brew, got %#v", runner.calls)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("duplicate-install detection must not run install.sh again, got %#v", runner.calls)
	}
}

func TestManualTUIUpdateSkipsWhenAlreadyLatest(t *testing.T) {
	runner := &fakeRunner{}
	report := RunManualTUIUpdate(t.TempDir(), ManualTUIUpdateOptions{
		CurrentTUIVersion: "v0.8.1",
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath: func(name string) (string, error) {
			if name == "brew" {
				return "/opt/homebrew/bin/brew", nil
			}
			return "", errors.New("not found")
		},
		Executable: func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		LookupEnv:  func(string) (string, bool) { return "", false },
	})

	if !report.Healthy {
		t.Fatalf("already-latest result should be healthy: %+v", report)
	}
	if report.Updated {
		t.Fatalf("already-latest result should not report an update: %+v", report)
	}
	if report.Err != nil {
		t.Fatalf("already-latest result should have no error: %v", report.Err)
	}
	if !containsLine(report.Lines, "TUI is already at the latest version (v0.8.1)") {
		t.Fatalf("expected already-latest line: %+v", report.Lines)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("already-latest update must not run any commands, got %#v", runner.calls)
	}
	if containsCall(runner.calls, "brew") {
		t.Fatalf("already-latest update must not run brew, got %#v", runner.calls)
	}
}

func TestManualTUIUpdateSourceInstallSucceeds(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	exe := filepath.Join(binDir, "lingtai-tui")
	writeSourceInstallMetadataVersion(t, globalDir, prefix, binDir, "v0.8.0", []string{exe})
	runner := &sourceUpdateRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"}

	report := RunManualTUIUpdate(globalDir, ManualTUIUpdateOptions{
		CurrentTUIVersion:   "v0.8.0",
		HTTPClient:          testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:              runner,
		LookPath:            func(string) (string, error) { return "/opt/homebrew/bin/brew", nil },
		Stat:                statAllExist,
		Executable:          func() (string, error) { return exe, nil },
		LookupEnv:           func(string) (string, bool) { return "", false },
		SourceInstallScript: "/tmp/install.sh",
	})

	if !report.Healthy || !report.Updated {
		t.Fatalf("source manual self-update should succeed: %+v", report)
	}
	if !containsLine(report.Lines, "TUI install method: source/user-local") {
		t.Fatalf("expected source install method line: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "Source/user-local TUI update verified") {
		t.Fatalf("expected source updater success: %+v", report.Lines)
	}
	if containsLine(report.Lines, "Manual self-update for source/user-local") {
		t.Fatalf("source manual self-update should no longer report unsupported: %+v", report.Lines)
	}
	if containsCall(runner.calls, "brew") {
		t.Fatalf("source manual self-update must not run brew, got %#v", runner.calls)
	}
}

func TestManualTUIUpdateUnknownInstallIsUnsupported(t *testing.T) {
	runner := &fakeRunner{}
	report := RunManualTUIUpdate(t.TempDir(), ManualTUIUpdateOptions{
		CurrentTUIVersion: "v0.8.0",
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "/opt/homebrew/bin/brew", nil },
		Executable:        func() (string, error) { return "/tmp/manual/lingtai-tui", nil },
		LookupEnv:         func(string) (string, bool) { return "", false },
	})

	if report.Healthy || report.Updated {
		t.Fatalf("unknown manual self-update should be unsupported: %+v", report)
	}
	if !containsLine(report.Lines, "TUI install method: unknown/other") {
		t.Fatalf("expected unknown install method line: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "TUI install method is unknown") {
		t.Fatalf("expected unknown updater guidance: %+v", report.Lines)
	}
	if containsCall(runner.calls, "brew") {
		t.Fatalf("unknown manual self-update must not run brew, got %#v", runner.calls)
	}
}

type sourceUpdateRunner struct {
	t              *testing.T
	globalDir      string
	prefix         string
	binDir         string
	latest         string
	runtimeVersion string
	failInstall    bool
	calls          []string
}

func (r *sourceUpdateRunner) Run(name string, args ...string) CommandResult {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	switch {
	case strings.Contains(call, "--update"):
		if r.failInstall {
			return CommandResult{Err: errors.New("exit status 1"), Stderr: "install failed"}
		}
		writeSourceInstallMetadataVersion(r.t, r.globalDir, r.prefix, r.binDir, r.latest, []string{filepath.Join(r.binDir, "lingtai-tui")})
		return CommandResult{Stdout: "updated\n"}
	case strings.HasSuffix(call, "lingtai-tui version"):
		return CommandResult{Stdout: "lingtai-tui " + r.latest + "\n"}
	case strings.Contains(call, "import lingtai"):
		return CommandResult{Stdout: r.runtimeVersion + "\n"}
	default:
		return CommandResult{Stdout: "ok\n"}
	}
}

type homebrewMigrationRunner struct {
	t              *testing.T
	globalDir      string
	prefix         string
	binDir         string
	latest         string
	runtimeVersion string
	failInstall    bool
	calls          []string
}

func (r *homebrewMigrationRunner) Run(name string, args ...string) CommandResult {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	switch {
	case strings.Contains(call, "install.sh"):
		if r.failInstall {
			return CommandResult{Err: errors.New("exit status 1"), Stderr: "install failed"}
		}
		writeSourceInstallMetadataVersion(r.t, r.globalDir, r.prefix, r.binDir, r.latest, []string{filepath.Join(r.binDir, "lingtai-tui")})
		return CommandResult{Stdout: "installed\n"}
	case strings.HasSuffix(call, "lingtai-tui version"):
		return CommandResult{Stdout: "lingtai-tui " + r.latest + "\n"}
	case strings.Contains(call, "import lingtai"):
		return CommandResult{Stdout: r.runtimeVersion + "\n"}
	default:
		return CommandResult{Stdout: "ok\n"}
	}
}

func TestNativeMigrationInstallCommandUsesCanonicalWebsiteInstallerNoUpdateNoPrefix(t *testing.T) {
	name, args := nativeMigrationInstallCommand("", "v0.11.0")
	if name != "bash" {
		t.Fatalf("name = %q, want bash", name)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"https://lingtai.ai/install.sh",
		"--version v0.11.0 --non-interactive",
		`shift; curl -fsSL "$script" | bash -s -- "$@"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("nativeMigrationInstallCommand args %q do not contain %q", joined, want)
		}
	}
	for _, unwanted := range []string{"--update", "--prefix"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("nativeMigrationInstallCommand args %q must not contain %q (no prior source install to update)", joined, unwanted)
		}
	}
}

func TestSourceInstallCommandUsesCanonicalWebsiteInstallerAndForwardsAllArgs(t *testing.T) {
	name, args := sourceInstallCommand("", "/tmp/lingtai prefix", "v0.11.0")
	if name != "bash" {
		t.Fatalf("name = %q, want bash", name)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"https://lingtai.ai/install.sh",
		"--update --prefix /tmp/lingtai prefix --version v0.11.0 --non-interactive",
		`shift; curl -fsSL "$script" | bash -s -- "$@"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("sourceInstallCommand args %q do not contain %q", joined, want)
		}
	}
}
