package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionOutputHasTagRejectsSubstringMatches(t *testing.T) {
	tests := []struct {
		output string
		want   string
		match  bool
	}{
		{"lingtai-tui v1.2.3", "v1.2.3", true},
		{"lingtai-tui v1.2.30", "v1.2.3", false},
		{"lingtai-tui v1.2.3-4-gabcdef", "v1.2.3", false},
		{"v1.2.3", "v1.2.3", true},
		{"lingtai-tui   v1.2.3  ", "v1.2.3", true},
		{"lingtai-tui v1.2.3", "", false},
		{"", "v1.2.3", false},
	}
	for _, tc := range tests {
		if got := versionOutputHasTag(tc.output, tc.want); got != tc.match {
			t.Errorf("versionOutputHasTag(%q, %q) = %v, want %v", tc.output, tc.want, got, tc.match)
		}
	}
}

func TestRunInstallShAdoptionRejectsSubstringVersion(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	exe := filepath.Join(binDir, "lingtai-tui")
	// Seed source metadata so the updater reaches the post-install version check.
	writeSourceInstallMetadataVersion(t, globalDir, prefix, binDir, "v0.8.0", []string{exe})
	// Runner reports v0.8.10 from the updated binary while we expect v0.8.1.
	runner := &sourceUpdateRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.10", runtimeVersion: "0.9.7"}
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

	if result.Healthy || result.Updated {
		t.Fatalf("expected version verification to reject v0.8.10 for expected v0.8.1: %+v", result)
	}
	if !containsLine(result.Lines, "version verification failed") {
		t.Fatalf("expected version verification failure line: %+v", result.Lines)
	}
}

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

func TestHomebrewTUIUpdaterAdoptsViaInstallShFromBrewPrefix(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	exe := filepath.Join(binDir, "lingtai-tui")
	runner := &homebrewAdoptionRunner{
		sourceUpdateRunner: sourceUpdateRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"},
		brewPrefix:         prefix,
	}

	result := RunTUIUpdate(TUIInstallInfo{
		Method:       TUIInstallMethodHomebrew,
		MetadataPath: filepath.Join(globalDir, "install.json"),
	}, TUIUpdateOptions{
		LatestVersion:       "v0.8.1",
		GlobalDir:           globalDir,
		Runner:              runner,
		Stat:                statAllExist,
		SourceInstallScript: "/tmp/install.sh",
	})

	if !result.Healthy || !result.Updated {
		t.Fatalf("expected healthy updated adoption result: %+v", result)
	}
	if !containsCall(runner.calls, "brew --prefix") {
		t.Fatalf("expected brew --prefix probe, got %#v", runner.calls)
	}
	if !containsCall(runner.calls, "bash /tmp/install.sh --update --prefix "+prefix+" --version v0.8.1 --non-interactive") {
		t.Fatalf("expected install.sh adoption call, got %#v", runner.calls)
	}
	if containsCall(runner.calls, "brew upgrade") || containsCall(runner.calls, "brew update") {
		t.Fatalf("homebrew adoption must not run brew update/upgrade, got %#v", runner.calls)
	}
	if !containsLine(result.Lines, "brew reinstall lingtai-tui") {
		t.Fatalf("expected rollback guidance line: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "Adopting Homebrew install") {
		t.Fatalf("expected adoption line: %+v", result.Lines)
	}
	_ = exe
}

func TestHomebrewTUIUpdaterInfersPrefixFromExecutableWhenNoBrew(t *testing.T) {
	globalDir := t.TempDir()
	prefix := "/opt/homebrew"
	binDir := filepath.Join(prefix, "bin")
	runner := &homebrewAdoptionRunner{
		sourceUpdateRunner: sourceUpdateRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"},
		brewPrefix:         "", // brew --prefix fails
	}

	result := RunTUIUpdate(TUIInstallInfo{
		Method:       TUIInstallMethodHomebrew,
		MetadataPath: filepath.Join(globalDir, "install.json"),
	}, TUIUpdateOptions{
		LatestVersion:       "v0.8.1",
		GlobalDir:           globalDir,
		Runner:              runner,
		Stat:                statAllExist,
		SourceInstallScript: "/tmp/install.sh",
		Executable:          func() (string, error) { return "/opt/homebrew/Cellar/lingtai-tui/0.8.0/bin/lingtai-tui", nil },
	})

	if !result.Updated {
		t.Fatalf("expected adoption to complete after inferring prefix: %+v", result)
	}
	if !containsCall(runner.calls, "brew --prefix") {
		t.Fatalf("expected brew --prefix probe before falling back, got %#v", runner.calls)
	}
	if !containsCall(runner.calls, "bash /tmp/install.sh --update --prefix /opt/homebrew --version v0.8.1 --non-interactive") {
		t.Fatalf("expected install.sh adoption call with inferred prefix, got %#v", runner.calls)
	}
	if containsCall(runner.calls, "brew upgrade") {
		t.Fatalf("homebrew adoption must not run brew upgrade, got %#v", runner.calls)
	}
}

func TestHomebrewTUIUpdaterFailsWhenNoPrefixResolvable(t *testing.T) {
	runner := &homebrewAdoptionRunner{
		sourceUpdateRunner: sourceUpdateRunner{t: t},
		brewPrefix:         "",
	}
	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		LatestVersion:       "v0.8.1",
		Runner:              runner,
		SourceInstallScript: "/tmp/install.sh",
		Executable:          func() (string, error) { return "/tmp/somewhere/lingtai-tui", nil },
	})

	if result.Healthy || result.Updated {
		t.Fatalf("adoption with no resolvable prefix should fail: %+v", result)
	}
	if containsCall(runner.calls, "--update") {
		t.Fatalf("adoption must not run install.sh without a prefix, got %#v", runner.calls)
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
	if !containsLine(result.Lines, "Python runtime verified after update") {
		t.Fatalf("expected runtime verification line: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "TUI update verified") {
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

func TestManualTUIUpdateHomebrewAdoptsViaInstallSh(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	runner := &homebrewAdoptionRunner{
		sourceUpdateRunner: sourceUpdateRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"},
		brewPrefix:         prefix,
	}
	report := RunManualTUIUpdate(globalDir, ManualTUIUpdateOptions{
		CurrentTUIVersion:   "v0.8.0",
		HTTPClient:          testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:              runner,
		Stat:                statAllExist,
		LookPath:            func(string) (string, error) { return "/opt/homebrew/bin/brew", nil },
		Executable:          func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		LookupEnv:           func(string) (string, bool) { return "", false },
		SourceInstallScript: "/tmp/install.sh",
	})

	if !report.Healthy || !report.Updated {
		t.Fatalf("expected healthy updated result: %+v", report)
	}
	if !containsLine(report.Lines, "TUI install method: homebrew") {
		t.Fatalf("expected homebrew install method line: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "TUI update available: v0.8.0 -> v0.8.1") {
		t.Fatalf("expected update-available line: %+v", report.Lines)
	}
	if !containsCall(runner.calls, "bash /tmp/install.sh --update --prefix "+prefix+" --version v0.8.1 --non-interactive") {
		t.Fatalf("expected manual install.sh adoption call, got %#v", runner.calls)
	}
	if containsCall(runner.calls, "brew upgrade") || containsCall(runner.calls, "brew update") {
		t.Fatalf("manual homebrew adoption must not run brew update/upgrade, got %#v", runner.calls)
	}
}

func TestManualTUIUpdateSourceSkipsWhenAlreadyLatest(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	exe := filepath.Join(binDir, "lingtai-tui")
	writeSourceInstallMetadataVersion(t, globalDir, prefix, binDir, "v0.8.1", []string{exe})
	runner := &fakeRunner{}
	report := RunManualTUIUpdate(globalDir, ManualTUIUpdateOptions{
		CurrentTUIVersion:   "v0.8.1",
		HTTPClient:          testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:              runner,
		LookPath:            func(string) (string, error) { return "", errors.New("not found") },
		Executable:          func() (string, error) { return exe, nil },
		LookupEnv:           func(string) (string, bool) { return "", false },
		SourceInstallScript: "/tmp/install.sh",
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
		t.Fatalf("already-latest source update must not run any commands, got %#v", runner.calls)
	}
}

func TestManualTUIUpdateHomebrewAdoptsEvenWhenAlreadyLatest(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	runner := &homebrewAdoptionRunner{
		sourceUpdateRunner: sourceUpdateRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"},
		brewPrefix:         prefix,
	}
	report := RunManualTUIUpdate(globalDir, ManualTUIUpdateOptions{
		CurrentTUIVersion:   "v0.8.1", // already latest, but Homebrew should still convert
		HTTPClient:          testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:              runner,
		Stat:                statAllExist,
		LookPath:            func(string) (string, error) { return "/opt/homebrew/bin/brew", nil },
		Executable:          func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		LookupEnv:           func(string) (string, bool) { return "", false },
		SourceInstallScript: "/tmp/install.sh",
	})

	if !report.Healthy || !report.Updated {
		t.Fatalf("homebrew already-latest should still adopt: %+v", report)
	}
	if !containsLine(report.Lines, "TUI is already at the latest version (v0.8.1)") {
		t.Fatalf("expected already-latest line before adoption: %+v", report.Lines)
	}
	if !containsCall(runner.calls, "bash /tmp/install.sh --update --prefix "+prefix+" --version v0.8.1 --non-interactive") {
		t.Fatalf("expected install.sh adoption call even when already latest, got %#v", runner.calls)
	}
	if containsCall(runner.calls, "brew upgrade") || containsCall(runner.calls, "brew update") {
		t.Fatalf("homebrew adoption must not run brew update/upgrade, got %#v", runner.calls)
	}
}

func TestManualTUIUpdateUnknownReportsUnsupportedWhenAlreadyLatest(t *testing.T) {
	runner := &fakeRunner{}
	report := RunManualTUIUpdate(t.TempDir(), ManualTUIUpdateOptions{
		CurrentTUIVersion: "v0.8.1", // already latest, but unknown must still report unsupported
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "", errors.New("not found") },
		Executable:        func() (string, error) { return "/tmp/manual/lingtai-tui", nil },
		LookupEnv:         func(string) (string, bool) { return "", false },
	})

	if report.Healthy || report.Updated {
		t.Fatalf("unknown already-latest self-update should be unsupported: %+v", report)
	}
	if !containsLine(report.Lines, "TUI install method is unknown") {
		t.Fatalf("expected unknown updater guidance: %+v", report.Lines)
	}
	if containsCall(runner.calls, "brew") {
		t.Fatalf("unknown self-update must not run brew, got %#v", runner.calls)
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
	if !containsLine(report.Lines, "TUI update verified") {
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

// homebrewAdoptionRunner answers `brew --prefix` with brewPrefix (empty ==>
// non-zero exit so the backend falls back to executable inference) and defers
// everything else to the embedded sourceUpdateRunner, so it drives the shared
// install.sh --update adoption path. calls are recorded on the embedded runner.
type homebrewAdoptionRunner struct {
	sourceUpdateRunner
	brewPrefix string
}

func (r *homebrewAdoptionRunner) Run(name string, args ...string) CommandResult {
	if name == "brew" && len(args) == 1 && args[0] == "--prefix" {
		r.calls = append(r.calls, "brew --prefix")
		if r.brewPrefix == "" {
			return CommandResult{Err: errors.New("exit status 1")}
		}
		return CommandResult{Stdout: r.brewPrefix + "\n"}
	}
	return r.sourceUpdateRunner.Run(name, args...)
}
