package config

import (
	"encoding/json"
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
	versions          []string
	failPip           bool
	editableSource    string // when non-empty, the editable-detect probe reports EDITABLE <source>
	envMarkerStdout   string // when non-empty, returned for delegated runtime env marker checks
	envMarkerErr      bool
	fileSearchStdout  string // when non-empty, returned for the file_io_sidecar probe
	fileSearchErr     bool
	fileSearchMissing bool // when true, the probe fails with ModuleNotFoundError for file_io_sidecar
	calls             []string
}

func (r *fakeRunner) Run(name string, args ...string) CommandResult {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if strings.Contains(call, "lingtai.venv_resolve") && strings.Contains(call, "env-marker") {
		if r.envMarkerErr {
			return CommandResult{Err: errors.New("exit status 1"), Stderr: "marker probe failed"}
		}
		if r.envMarkerStdout != "" {
			return CommandResult{Stdout: r.envMarkerStdout}
		}
		return CommandResult{Stdout: `{"status":"match"}` + "\n"}
	}
	if strings.Contains(call, "file_io_sidecar") {
		if r.fileSearchMissing {
			return CommandResult{
				Err:    errors.New("exit status 1"),
				Stderr: "Traceback (most recent call last):\n  File \"<string>\", line 2, in <module>\nModuleNotFoundError: No module named 'lingtai.services.file_io_sidecar'",
			}
		}
		if r.fileSearchErr {
			return CommandResult{Err: errors.New("exit status 1"), Stderr: "probe failed"}
		}
		if r.fileSearchStdout != "" {
			return CommandResult{Stdout: r.fileSearchStdout}
		}
		return CommandResult{Stdout: "BACKEND RustFileIOBackend\nSIDECAR /tmp/lingtai-search-sidecar\n"}
	}
	if strings.Contains(call, "direct_url.json") {
		// Editable-install probe (isEditableLingtaiInstall). Default response
		// is "WHEEL" — matching the conservative no-skip default for existing
		// tests that did not exercise this path.
		if r.editableSource != "" {
			return CommandResult{Stdout: "EDITABLE " + r.editableSource + "\n"}
		}
		return CommandResult{Stdout: "WHEEL\n"}
	}
	if strings.Contains(call, "sysconfig.get_platform") && strings.Contains(call, "version_major") {
		return CommandResult{Stdout: `{"implementation":"CPython","machine":"arm64","sys_platform":"darwin","sysconfig_platform":"macosx-14.0-arm64","version":"3.13.5","version_major":3,"version_minor":13,"version_micro":5}` + "\n"}
	}
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

// noDevHome returns an empty home dir and a no-op env lookup so tests that
// exercise the non-dev (PyPI/editable-skip) paths do not pick up the running
// developer's real ~/work/GitHub checkouts. Threaded into UpgradeRuntimeOptions
// / DoctorOptions via Home + LookupEnv.
func noDevHome(t *testing.T) (string, func(string) (string, bool)) {
	t.Helper()
	return t.TempDir(), func(string) (string, bool) { return "", false }
}

type fakeFileInfo struct{ os.FileInfo }

func writeSourceInstallMetadata(t *testing.T, globalDir, prefix, binDir string, managed []string) {
	writeSourceInstallMetadataVersion(t, globalDir, prefix, binDir, "v0.8.1", managed)
}

func writeSourceInstallMetadataVersion(t *testing.T, globalDir, prefix, binDir, version string, managed []string) {
	t.Helper()
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir global dir: %v", err)
	}
	raw, err := json.Marshal(map[string]interface{}{
		"schema":           sourceInstallMetadataSchema,
		"schema_version":   1,
		"install_method":   "source",
		"prefix":           prefix,
		"bin_dir":          binDir,
		"repo_url":         "https://github.com/Lingtai-AI/lingtai.git",
		"requested_ref":    version,
		"resolved_ref":     version,
		"resolved_commit":  "0123456789abcdef",
		"stamped_version":  version,
		"installed_at":     "2026-06-23T00:00:00Z",
		"managed_binaries": managed,
	})
	if err != nil {
		t.Fatalf("marshal install metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "install.json"), raw, 0o644); err != nil {
		t.Fatalf("write install metadata: %v", err)
	}
}

func TestDetectTUIInstallMethodSourceMetadataMatchesExecutable(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	exe := filepath.Join(binDir, "lingtai-tui")
	writeSourceInstallMetadata(t, globalDir, prefix, binDir, []string{exe})

	info := detectTUIInstallMethod(globalDir, exe, DoctorOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if info.Method != TUIInstallMethodSource {
		t.Fatalf("method = %s, want source; diagnostics=%+v", info.Method, info.Diagnostics)
	}
	if !strings.Contains(info.Summary(), "source/user-local") || !strings.Contains(info.Summary(), prefix) {
		t.Fatalf("source summary should mention method and prefix, got %q", info.Summary())
	}
}

func TestDetectTUIInstallMethodSourceMetadataBeatsHomebrewPath(t *testing.T) {
	globalDir := t.TempDir()
	exe := "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__"
	writeSourceInstallMetadata(t, globalDir, "/opt/homebrew", "/opt/homebrew/bin", []string{exe})

	info := detectTUIInstallMethod(globalDir, exe, DoctorOptions{
		LookupEnv: func(key string) (string, bool) {
			if key == "HOMEBREW_PREFIX" {
				return "/opt/homebrew", true
			}
			return "", false
		},
	})
	if info.Method != TUIInstallMethodSource {
		t.Fatalf("source metadata should win over Homebrew path, got %s", info.Method)
	}
}

func TestDetectTUIInstallMethodSourceMetadataMatchesLingtaiAlias(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	// install.sh records lingtai-tui in managed_binaries but invokes through the
	// "lingtai" alias symlink, whose unresolved path matches no managed binary.
	exe := filepath.Join(binDir, "lingtai")
	writeSourceInstallMetadata(t, globalDir, prefix, binDir, []string{filepath.Join(binDir, "lingtai-tui")})

	info := detectTUIInstallMethod(globalDir, exe, DoctorOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if info.Method != TUIInstallMethodSource {
		t.Fatalf("method = %s, want source; diagnostics=%+v", info.Method, info.Diagnostics)
	}
	if len(info.Diagnostics) != 0 {
		t.Fatalf("alias source install should not emit diagnostics, got %+v", info.Diagnostics)
	}
}

func TestDetectTUIInstallMethodSourceAliasInBrewBinBeatsHomebrew(t *testing.T) {
	globalDir := t.TempDir()
	// A source install with brew present lands in $(brew --prefix)/bin. Invoked
	// through the "lingtai" alias, the Homebrew path heuristic would otherwise
	// misroute this source install to `brew upgrade`.
	binDir := "/opt/homebrew/bin"
	exe := filepath.Join(binDir, "lingtai")
	writeSourceInstallMetadata(t, globalDir, "/opt/homebrew", binDir, []string{filepath.Join(binDir, "lingtai-tui")})

	info := detectTUIInstallMethod(globalDir, exe, DoctorOptions{
		LookupEnv: func(key string) (string, bool) {
			if key == "HOMEBREW_PREFIX" {
				return "/opt/homebrew", true
			}
			return "", false
		},
	})
	if info.Method != TUIInstallMethodSource {
		t.Fatalf("source alias in brew bin should report source, got %s; detail=%q", info.Method, info.Detail)
	}
}

func intelHomebrewSymlink(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cellarExe := filepath.Join(root, "usr", "local", "Cellar", "lingtai-tui", "0.8.1", "bin", "lingtai-tui")
	linkExe := filepath.Join(root, "usr", "local", "bin", "lingtai-tui")
	if err := os.MkdirAll(filepath.Dir(cellarExe), 0o755); err != nil {
		t.Fatalf("mkdir Cellar dir: %v", err)
	}
	if err := os.WriteFile(cellarExe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write Cellar executable: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(linkExe), 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	if err := os.Symlink(cellarExe, linkExe); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	return linkExe
}

func TestDetectTUIInstallMethodHomebrewFromPathAndEnv(t *testing.T) {
	tests := []struct {
		name string
		exe  string
		env  map[string]string
	}{
		{name: "apple silicon path", exe: "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__"},
		{name: "linuxbrew path", exe: "/home/linuxbrew/.linuxbrew/bin/lingtai-tui"},
		{name: "intel homebrew env", exe: "/usr/local/bin/__lingtai_doctor_test_lingtai_tui__", env: map[string]string{"HOMEBREW_PREFIX": "/usr/local"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := detectTUIInstallMethod(t.TempDir(), tc.exe, DoctorOptions{
				LookupEnv: func(key string) (string, bool) {
					value, ok := tc.env[key]
					return value, ok
				},
			})
			if info.Method != TUIInstallMethodHomebrew {
				t.Fatalf("method = %s, want homebrew; detail=%q diagnostics=%+v", info.Method, info.Detail, info.Diagnostics)
			}
		})
	}
}

func TestDetectTUIInstallMethodHomebrewFromIntelBinSymlink(t *testing.T) {
	exe := intelHomebrewSymlink(t)

	info := detectTUIInstallMethod(t.TempDir(), exe, DoctorOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if info.Method != TUIInstallMethodHomebrew {
		t.Fatalf("method = %s, want homebrew; detail=%q diagnostics=%+v", info.Method, info.Detail, info.Diagnostics)
	}
}

func TestDetectTUIInstallMethodSourceMetadataBeatsResolvedHomebrewSymlink(t *testing.T) {
	globalDir := t.TempDir()
	exe := intelHomebrewSymlink(t)
	binDir := filepath.Dir(exe)
	prefix := filepath.Dir(binDir)
	writeSourceInstallMetadata(t, globalDir, prefix, binDir, []string{exe})

	info := detectTUIInstallMethod(globalDir, exe, DoctorOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if info.Method != TUIInstallMethodSource {
		t.Fatalf("source metadata should win over resolved Homebrew symlink, got %s; detail=%q diagnostics=%+v", info.Method, info.Detail, info.Diagnostics)
	}
}

func TestDetectTUIInstallMethodUnknownWhenMetadataDoesNotMatch(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	writeSourceInstallMetadata(t, globalDir, prefix, binDir, []string{filepath.Join(binDir, "lingtai-tui")})

	info := detectTUIInstallMethod(globalDir, filepath.Join(t.TempDir(), "bin", "lingtai-tui"), DoctorOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
	})
	if info.Method != TUIInstallMethodUnknown {
		t.Fatalf("method = %s, want unknown", info.Method)
	}
	if len(info.Diagnostics) == 0 || !strings.Contains(info.Diagnostics[0].Text, "does not match executable") {
		t.Fatalf("expected stale metadata diagnostic, got %+v", info.Diagnostics)
	}
}

// TestRuntimeReadyReportsActionableErrorWhenVenvMissing proves RuntimeReady
// is a pure readiness check: given a directory with no managed venv, it
// returns an actionable *RuntimeReadyError rather than creating or repairing
// anything. install.sh installs the kernel by default; the ONLY automatic
// install/repair opportunity is main()'s shared startup preflight, gated on
// explicit current-launch human consent — RuntimeReady must never install on
// its own, even when called from returning-user startup, first-run, headless
// spawn, or post-Create project launch.
func TestRuntimeReadyReportsActionableErrorWhenVenvMissing(t *testing.T) {
	dir := t.TempDir()
	err := RuntimeReady(dir)
	if err == nil {
		t.Fatal("expected an error for a directory with no managed venv")
	}
	var readyErr *RuntimeReadyError
	if !errors.As(err, &readyErr) {
		t.Fatalf("expected *RuntimeReadyError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("expected an actionable 'not ready' message, got %q", err.Error())
	}
	// Confirm no venv materialized as a side effect of merely checking.
	if _, statErr := os.Stat(RuntimeVenvDir(dir)); statErr == nil {
		t.Fatal("RuntimeReady must not create the venv directory as a side effect of checking")
	}
}

// TestRuntimeReadyIssuesNoMutatingCommands is a structural proof that
// RuntimeReady delegates to NeedsVenv (a read-only stat/import probe) and
// nothing else — it cannot reach EnsureVenv/pip/uv because there is no code
// path from RuntimeReady to any of them any more.
func TestRuntimeReadyIssuesNoMutatingCommands(t *testing.T) {
	dir := t.TempDir()
	before, _ := os.ReadDir(dir)
	_ = RuntimeReady(dir)
	after, _ := os.ReadDir(dir)
	if len(after) != len(before) {
		t.Fatalf("RuntimeReady must not write into globalDir merely by checking: before=%d entries, after=%d entries", len(before), len(after))
	}
}

func TestExecCommandRunnerCapturesOutputAfterRun(t *testing.T) {
	result := (execCommandRunner{}).Run("sh", "-c", "printf stdout-text; printf stderr-text >&2")
	if result.Err != nil {
		t.Fatalf("command failed: %v", result.Err)
	}
	if result.Stdout != "stdout-text" || result.Stderr != "stderr-text" {
		t.Fatalf("runner did not capture command output after Run: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestUpgradePythonRuntimeReportsCommandFailure(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.6"}, failPip: true}
	home, env := noDevHome(t)
	result := UpgradePythonRuntime(t.TempDir(), true, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
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
	home, env := noDevHome(t)
	result := UpgradePythonRuntime(t.TempDir(), false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "", errors.New("no uv") },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
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

func TestUpgradePythonRuntimeSkipsEditableInstall(t *testing.T) {
	// Editable installs (pip/uv -e) must be left alone — running pip install
	// --upgrade would silently clobber the dev source checkout. The check
	// must skip BOTH the PyPI fetch and the install command.
	runner := &fakeRunner{
		versions:       []string{"0.10.6"},
		editableSource: "file:///Users/dev/lingtai-kernel",
	}
	home, env := noDevHome(t)
	result := UpgradePythonRuntime(t.TempDir(), false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.10.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	if !result.Healthy {
		t.Fatalf("editable install must remain Healthy: %+v", result.Lines)
	}
	if result.Updated {
		t.Fatalf("editable install must not report Updated")
	}
	if !containsLine(result.Lines, "editable install") {
		t.Fatalf("expected editable-install info line: %+v", result.Lines)
	}
	if containsCall(runner.calls, "pip install --upgrade lingtai") {
		t.Fatalf("editable install must not trigger pip upgrade: %#v", runner.calls)
	}
}

func TestUpgradePythonRuntimeForceRespectsEditableInstall(t *testing.T) {
	// Even force=true (doctor's repair path) must not clobber an editable
	// install — a forced wheel reinstall is more destructive than the broken
	// state it was trying to fix, and the user can always re-create dev mode
	// with `uv pip install -e`.
	runner := &fakeRunner{
		versions:       []string{"0.10.6"},
		editableSource: "file:///Users/dev/lingtai-kernel",
	}
	home, env := noDevHome(t)
	result := UpgradePythonRuntime(t.TempDir(), true, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.10.7", "v0.8.1"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	if result.Updated {
		t.Fatalf("forced editable upgrade must not report Updated")
	}
	if containsCall(runner.calls, "pip install --upgrade lingtai") {
		t.Fatalf("forced editable must not trigger pip upgrade: %#v", runner.calls)
	}
}

func TestCompareReleaseVersionsClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    ReleaseComparisonKind
	}{
		{current: "v0.8.1", latest: "v0.8.2", want: ReleaseComparisonUpdateAvailable},
		{current: "v0.8.2", latest: "v0.8.2", want: ReleaseComparisonUpToDate},
		{current: "v0.8.3", latest: "v0.8.2", want: ReleaseComparisonUpToDate},
		{current: "0.8.9", latest: "v0.8.10", want: ReleaseComparisonUpdateAvailable},
		{current: "2fb2766", latest: "v0.8.2", want: ReleaseComparisonCurrentNonSemver},
		{current: "", latest: "v0.8.2", want: ReleaseComparisonCurrentMissing},
		{current: "dev", latest: "v0.8.2", want: ReleaseComparisonDevBuild},
		{current: "v0.8.2-4-gabcdef", latest: "v0.8.3", want: ReleaseComparisonDevBuild},
		{current: "banana", latest: "v0.8.2", want: ReleaseComparisonCurrentNonSemver},
		{current: "v0.8.2", latest: "", want: ReleaseComparisonLatestNonSemver},
		{current: "v0.8.2", latest: "release-2026", want: ReleaseComparisonLatestNonSemver},
	}
	for _, tc := range cases {
		if got := CompareReleaseVersions(tc.current, tc.latest); got.Kind != tc.want {
			t.Fatalf("CompareReleaseVersions(%q, %q) kind = %s, want %s", tc.current, tc.latest, got.Kind, tc.want)
		}
	}
}

func TestReleaseNewerKeepsBooleanCompatibility(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    bool
	}{
		{current: "v0.8.1", latest: "v0.8.2", want: true},
		{current: "v0.8.2", latest: "v0.8.2", want: false},
		{current: "v0.8.3", latest: "v0.8.2", want: false},
		{current: "0.8.9", latest: "v0.8.10", want: true},
		{current: "2fb2766", latest: "v0.8.2", want: false},
		{current: "dev", latest: "v0.8.2", want: false},
		{current: "v0.8.3", latest: "", want: false},
	}
	for _, tc := range cases {
		if got := releaseNewer(tc.current, tc.latest); got != tc.want {
			t.Fatalf("releaseNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
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
		Home:       t.TempDir(),
		LookupEnv:  func(string) (string, bool) { return "", false },
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
		Executable:        func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		Home:              t.TempDir(),
		LookupEnv:         func(string) (string, bool) { return "", false },
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
		Executable:        func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		Home:              t.TempDir(),
		LookupEnv:         func(string) (string, bool) { return "", false },
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

func TestRunDoctorUpdateTUICurrentNonSemverWarns(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.7", "0.9.7"}}
	report := RunDoctorUpdate(t.TempDir(), DoctorOptions{
		CurrentTUIVersion: "2fb2766",
		ForceTUI:          true,
		ForcePython:       false,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "/opt/homebrew/bin/brew", nil },
		Executable:        func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		Home:              t.TempDir(),
		LookupEnv:         func(string) (string, bool) { return "", false },
		Readlink:          func(string) (string, error) { return "", os.ErrInvalid },
		Stat:              statAllExist,
	})
	if !report.Healthy {
		t.Fatalf("non-semver TUI check should remain a warning, not a failure: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "current version \"2fb2766\" is not a strict vX.Y.Z release") {
		t.Fatalf("expected current non-semver warning: %+v", report.Lines)
	}
	if containsLine(report.Lines, "TUI is up to date") {
		t.Fatalf("current non-semver version must not be reported as up to date: %+v", report.Lines)
	}
	if containsCall(runner.calls, "brew") {
		t.Fatalf("non-comparable TUI version must not run brew, got %#v", runner.calls)
	}
}

func TestRunDoctorUpdateTUILatestNonSemverWarns(t *testing.T) {
	runner := &fakeRunner{versions: []string{"0.9.7", "0.9.7"}}
	report := RunDoctorUpdate(t.TempDir(), DoctorOptions{
		CurrentTUIVersion: "v0.8.1",
		ForceTUI:          true,
		ForcePython:       false,
		HTTPClient:        testVersionClient(t, "0.9.7", "latest"),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "/opt/homebrew/bin/brew", nil },
		Executable:        func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		Home:              t.TempDir(),
		LookupEnv:         func(string) (string, bool) { return "", false },
		Readlink:          func(string) (string, error) { return "", os.ErrInvalid },
		Stat:              statAllExist,
	})
	if !report.Healthy {
		t.Fatalf("malformed latest TUI check should remain a warning, not a failure: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "latest release tag \"latest\" is not a strict vX.Y.Z release") {
		t.Fatalf("expected latest non-semver warning: %+v", report.Lines)
	}
	if containsLine(report.Lines, "TUI is up to date") {
		t.Fatalf("latest non-semver tag must not be reported as up to date: %+v", report.Lines)
	}
	if containsCall(runner.calls, "brew") {
		t.Fatalf("non-comparable latest tag must not run brew, got %#v", runner.calls)
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
		Home:              t.TempDir(),
		LookupEnv:         func(string) (string, bool) { return "", false },
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
		Executable: func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		Home:       t.TempDir(),
		LookupEnv:  func(string) (string, bool) { return "", false },
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

func TestRunDoctorUpdateReportsSourceInstallAndDoesNotRunBrew(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	exe := filepath.Join(binDir, "lingtai-tui")
	writeSourceInstallMetadataVersion(t, globalDir, prefix, binDir, "v0.8.0", []string{exe})
	runner := &sourceUpdateRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", runtimeVersion: "0.9.7"}

	report := RunDoctorUpdate(globalDir, DoctorOptions{
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
		Executable: func() (string, error) { return exe, nil },
		Home:       t.TempDir(),
		LookupEnv:  func(string) (string, bool) { return "", false },
		Readlink:   func(string) (string, error) { return "", os.ErrInvalid },
		Stat:       statAllExist,
	})
	if !report.Healthy {
		t.Fatalf("source install guidance should not fail doctor: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "TUI install method: source/user-local") {
		t.Fatalf("expected source install method report: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "Source/user-local TUI update verified") {
		t.Fatalf("expected source updater success: %+v", report.Lines)
	}
	if containsCall(runner.calls, "brew") {
		t.Fatalf("source install must not run brew, got %#v", runner.calls)
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
		Home:              t.TempDir(),
		LookupEnv:         func(string) (string, bool) { return "", false },
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
		Executable:        func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		Home:              t.TempDir(),
		LookupEnv:         func(string) (string, bool) { return "", false },
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

func TestFileSearchNativeStatusParsesProbe(t *testing.T) {
	runner := &fakeRunner{fileSearchStdout: "BACKEND LocalFileIOBackend\nSIDECAR \n"}
	status, err := FileSearchNativeStatus(t.TempDir(), runner)
	if err != nil {
		t.Fatalf("FileSearchNativeStatus err = %v", err)
	}
	if status.Backend != "LocalFileIOBackend" || status.SidecarPath != "" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if !containsCall(runner.calls, "file_io_sidecar") {
		t.Fatalf("expected Python file_io_sidecar probe, got %#v", runner.calls)
	}
}

func TestRunDoctorUpdateReportsFileSearchFallbackAndMissingCargo(t *testing.T) {
	runner := &fakeRunner{
		versions:         []string{"0.9.7", "0.9.7"},
		fileSearchStdout: "BACKEND LocalFileIOBackend\nSIDECAR \n",
	}
	report := RunDoctorUpdate(t.TempDir(), DoctorOptions{
		CurrentTUIVersion: "v0.8.1",
		ForcePython:       false,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "", errors.New("not found") },
		Executable:        func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		Home:              t.TempDir(),
		LookupEnv:         func(string) (string, bool) { return "", false },
		Readlink:          func(string) (string, error) { return "", os.ErrInvalid },
		Stat:              statAllExist,
	})
	if !report.Healthy {
		t.Fatalf("file-search fallback is a warning, not a failed doctor report: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "Python fallback active") {
		t.Fatalf("expected Python fallback warning: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "cargo not found") {
		t.Fatalf("expected missing cargo warning: %+v", report.Lines)
	}
}

func TestRunDoctorUpdateReportsBundledSidecarWithoutCargo(t *testing.T) {
	runner := &fakeRunner{
		versions:         []string{"0.9.7", "0.9.7"},
		fileSearchStdout: "BACKEND RustFileIOBackend\nSIDECAR /opt/lingtai/bin/lingtai-search-sidecar\n",
	}
	report := RunDoctorUpdate(t.TempDir(), DoctorOptions{
		CurrentTUIVersion: "v0.8.1",
		ForcePython:       false,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "", errors.New("not found") },
		Executable:        func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		Home:              t.TempDir(),
		LookupEnv:         func(string) (string, bool) { return "", false },
		Readlink:          func(string) (string, error) { return "", os.ErrInvalid },
		Stat:              statAllExist,
	})
	if !report.Healthy {
		t.Fatalf("expected healthy report: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "Rust sidecar available") {
		t.Fatalf("expected sidecar OK line: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "bundled sidecar is already available") {
		t.Fatalf("expected no-cargo-but-sidecar info line: %+v", report.Lines)
	}
}

func TestFileSearchNativeStatusReportsUnsupportedOnModuleMissing(t *testing.T) {
	runner := &fakeRunner{fileSearchMissing: true}
	status, err := FileSearchNativeStatus(t.TempDir(), runner)
	if err != nil {
		t.Fatalf("ModuleNotFoundError should be treated as Unsupported, not an error: %v", err)
	}
	if !status.Unsupported {
		t.Fatalf("expected Unsupported=true on missing file_io_sidecar module: %+v", status)
	}
	if !containsCall(runner.calls, "file_io_sidecar") {
		t.Fatalf("expected Python file_io_sidecar probe, got %#v", runner.calls)
	}
}

func TestRunDoctorUpdateReportsUnsupportedRuntimeInfo(t *testing.T) {
	runner := &fakeRunner{
		versions:          []string{"0.9.7", "0.9.7"},
		fileSearchMissing: true,
	}
	report := RunDoctorUpdate(t.TempDir(), DoctorOptions{
		CurrentTUIVersion: "v0.8.1",
		ForcePython:       false,
		HTTPClient:        testVersionClient(t, "0.9.7", "v0.8.1"),
		Runner:            runner,
		LookPath:          func(string) (string, error) { return "", errors.New("not found") },
		Executable:        func() (string, error) { return "/opt/homebrew/bin/__lingtai_doctor_test_lingtai_tui__", nil },
		Home:              t.TempDir(),
		LookupEnv:         func(string) (string, bool) { return "", false },
		Readlink:          func(string) (string, error) { return "", os.ErrInvalid },
		Stat:              statAllExist,
	})
	if !report.Healthy {
		t.Fatalf("an unsupported runtime is informational, not a failed doctor report: %+v", report.Lines)
	}
	if !containsLine(report.Lines, "does not expose Rust sidecar diagnostics yet") {
		t.Fatalf("expected unsupported-runtime info line: %+v", report.Lines)
	}
	// The unsupported path must short-circuit: no generic "could not inspect"
	// warning and no "Python fallback" / cargo guidance.
	if containsLine(report.Lines, "could not inspect runtime sidecar status") {
		t.Fatalf("unsupported runtime should not emit the generic probe-error warning: %+v", report.Lines)
	}
	if containsLine(report.Lines, "Python fallback active") {
		t.Fatalf("unsupported runtime should short-circuit before the fallback/cargo lines: %+v", report.Lines)
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
