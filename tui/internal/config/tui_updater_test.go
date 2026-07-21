package config

import (
	"errors"
	"os/exec"
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

func TestHomebrewTUIUpdaterDoctorRunsUpdateThenUpgrade(t *testing.T) {
	runner := &fakeRunner{}
	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		LatestVersion:         "v0.8.1",
		Runner:                runner,
		LookPath:              func(string) (string, error) { return "/opt/homebrew/bin/brew", nil },
		IncludeHomebrewUpdate: true,
		ResolveHomebrewPath:   true,
	})

	if !result.Healthy || !result.Updated {
		t.Fatalf("expected healthy updated result: %+v", result)
	}
	update := "/opt/homebrew/bin/brew update"
	upgrade := "/opt/homebrew/bin/brew upgrade lingtai-ai/lingtai/lingtai-tui"
	if !containsCall(runner.calls, update) || !containsCall(runner.calls, upgrade) {
		t.Fatalf("expected doctor brew calls, got %#v", runner.calls)
	}
	if indexOfCall(runner.calls, update) > indexOfCall(runner.calls, upgrade) {
		t.Fatalf("expected brew update before brew upgrade, got %#v", runner.calls)
	}
	if !containsLine(result.Lines, "Brew upgrade finished") {
		t.Fatalf("expected restart guidance line: %+v", result.Lines)
	}
}

func TestHomebrewTUIUpdaterStartupRunsUpgradeOnlyWithoutLookPath(t *testing.T) {
	runner := &fakeRunner{}
	lookedUp := false
	result := RunTUIUpdate(TUIInstallInfo{Method: TUIInstallMethodHomebrew}, TUIUpdateOptions{
		LatestVersion: "v0.8.1",
		Runner:        runner,
		LookPath: func(string) (string, error) {
			lookedUp = true
			return "", errors.New("should not be called")
		},
	})

	if !result.Healthy || !result.Updated {
		t.Fatalf("expected healthy updated result: %+v", result)
	}
	if lookedUp {
		t.Fatalf("startup-style updater should execute brew by name without resolving PATH")
	}
	if len(runner.calls) != 1 || runner.calls[0] != "brew upgrade lingtai-ai/lingtai/lingtai-tui" {
		t.Fatalf("expected only brew upgrade call, got %#v", runner.calls)
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

func TestManualTUIUpdateHomebrewRoutesThroughUpdater(t *testing.T) {
	runner := &fakeRunner{}
	report := RunManualTUIUpdate(t.TempDir(), ManualTUIUpdateOptions{
		CurrentTUIVersion: "v0.8.0",
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

	if !report.Healthy || !report.Updated {
		t.Fatalf("expected healthy updated result: %+v", report)
	}
	if !containsLine(report.Lines, "TUI install method: homebrew") {
		t.Fatalf("expected homebrew install method line: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "TUI update available: v0.8.0 -> v0.8.1") {
		t.Fatalf("expected update-available line: %+v", report.Lines)
	}
	update := "/opt/homebrew/bin/brew update"
	upgrade := "/opt/homebrew/bin/brew upgrade lingtai-ai/lingtai/lingtai-tui"
	if !containsCall(runner.calls, update) || !containsCall(runner.calls, upgrade) {
		t.Fatalf("expected manual brew calls, got %#v", runner.calls)
	}
	if indexOfCall(runner.calls, update) > indexOfCall(runner.calls, upgrade) {
		t.Fatalf("expected brew update before brew upgrade, got %#v", runner.calls)
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
	case strings.Contains(call, " --update "):
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

func TestSourceInstallCommandMatchesRealInstallerParser(t *testing.T) {
	installerPath, err := filepath.Abs(filepath.Join("..", "..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	name, args := sourceInstallCommand(installerPath, "/tmp/lingtai prefix", "v0.11.0")
	if name != "bash" || len(args) < 2 || args[0] != installerPath {
		t.Fatalf("local sourceInstallCommand = %q %#v, want bash followed by %s and update args", name, args, installerPath)
	}

	parser := `set -euo pipefail; export LINGTAI_INSTALL_SH_SOURCE_ONLY=1; source "$1"; shift; parse_args "$@"`
	parserArgs := []string{"-c", parser, "lingtai-installer-parser", installerPath}
	parserArgs = append(parserArgs, args[1:]...)
	if output, err := exec.Command("bash", parserArgs...).CombinedOutput(); err != nil {
		t.Fatalf("source updater arguments are rejected by install.sh's real parser: %v\n%s", err, output)
	}
}
