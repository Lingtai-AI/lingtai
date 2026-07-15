package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// RuntimeVenvDir returns ~/.lingtai-tui/runtime/venv/.
func RuntimeVenvDir(globalDir string) string {
	return filepath.Join(globalDir, "runtime", "venv")
}

// VenvPython returns the Python executable path inside a venv directory.
func VenvPython(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python")
}

type runtimeEnvMarkerState string

const (
	runtimeEnvMarkerMissing  runtimeEnvMarkerState = "missing"
	runtimeEnvMarkerMatch    runtimeEnvMarkerState = "match"
	runtimeEnvMarkerMismatch runtimeEnvMarkerState = "mismatch"
	runtimeEnvMarkerUnknown  runtimeEnvMarkerState = "unknown"
)

type runtimeEnvMarkerCheck struct {
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	StampStatus string `json:"stamp_status"`
}

type runtimeEnvMarkerFile struct {
	Schema            string `json:"schema"`
	SchemaVersion     int    `json:"schema_version"`
	LingtaiEnvVersion int    `json:"lingtai_env_version"`
	OS                string `json:"os"`
	Arch              string `json:"arch"`
}

const (
	runtimeEnvMarkerFileName = ".lingtai-env.json"
	runtimeEnvMarkerSchema   = "lingtai.runtime-env"
)

func runtimeEnvMarkerPlatformMismatch(venvPath string) bool {
	raw, err := os.ReadFile(filepath.Join(venvPath, runtimeEnvMarkerFileName))
	if err != nil {
		return false
	}
	var marker runtimeEnvMarkerFile
	if err := json.Unmarshal(raw, &marker); err != nil {
		return false
	}
	if marker.Schema != runtimeEnvMarkerSchema || marker.SchemaVersion != 1 || marker.LingtaiEnvVersion != 1 {
		return false
	}
	return marker.OS != runtime.GOOS || marker.Arch != runtime.GOARCH
}

func runtimeEnvMarkerStateForVenv(venvPath string, runner CommandRunner) (runtimeEnvMarkerState, error) {
	if runtimeEnvMarkerPlatformMismatch(venvPath) {
		return runtimeEnvMarkerMismatch, nil
	}
	check, err := runRuntimeEnvMarkerCommand(venvPath, runner, "check")
	if err != nil {
		return runtimeEnvMarkerUnknown, err
	}
	switch runtimeEnvMarkerState(check.Status) {
	case runtimeEnvMarkerMissing, runtimeEnvMarkerMatch, runtimeEnvMarkerMismatch:
		return runtimeEnvMarkerState(check.Status), nil
	case "error", runtimeEnvMarkerUnknown:
		if check.Detail != "" {
			return runtimeEnvMarkerUnknown, fmt.Errorf("%s", check.Detail)
		}
		return runtimeEnvMarkerUnknown, fmt.Errorf("kernel marker check returned %q", check.Status)
	default:
		return runtimeEnvMarkerUnknown, fmt.Errorf("kernel marker check returned unsupported status %q", check.Status)
	}
}

func runRuntimeEnvMarkerCommand(venvPath string, runner CommandRunner, action string) (runtimeEnvMarkerCheck, error) {
	if runner == nil {
		runner = timeoutCommandRunner{timeout: 3 * time.Second}
	}
	res := runner.Run(VenvPython(venvPath), "-m", "lingtai.venv_resolve", "env-marker", action, "--venv", venvPath)
	if res.Err != nil {
		detail := strings.TrimSpace(res.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(res.Stdout)
		}
		if detail == "" {
			detail = res.Err.Error()
		}
		return runtimeEnvMarkerCheck{}, fmt.Errorf("kernel marker %s failed: %s", action, lastNonEmptyLine(detail))
	}
	var check runtimeEnvMarkerCheck
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &check); err != nil {
		return runtimeEnvMarkerCheck{}, fmt.Errorf("kernel marker %s returned invalid JSON: %w", action, err)
	}
	if check.Status == "" {
		return runtimeEnvMarkerCheck{}, fmt.Errorf("kernel marker %s returned no status", action)
	}
	return check, nil
}

func writeRuntimeEnvMarker(venvPath string, runner CommandRunner) error {
	check, err := runRuntimeEnvMarkerCommand(venvPath, runner, "stamp")
	if err != nil {
		return err
	}
	if runtimeEnvMarkerState(check.Status) == runtimeEnvMarkerMatch {
		return nil
	}
	if check.Detail != "" {
		return fmt.Errorf("%s", check.Detail)
	}
	return fmt.Errorf("kernel marker stamp returned %q", check.Status)
}

func writeRuntimeEnvMarkerIfVenvDirExists(venvPath string, runner CommandRunner) {
	if _, err := os.Stat(venvPath); err != nil {
		return
	}
	_ = writeRuntimeEnvMarker(venvPath, runner)
}

func removeRuntimeVenvIfEnvMismatch(venvPath string, runner CommandRunner) error {
	state, _ := runtimeEnvMarkerStateForVenv(venvPath, runner)
	if state != runtimeEnvMarkerMismatch {
		return nil
	}
	return os.RemoveAll(venvPath)
}

// LingtaiCmd returns the Python interpreter path for running lingtai.
// Callers should invoke as: LingtaiCmd(dir), "-m", "lingtai", "run", agentDir
func LingtaiCmd(globalDir string) string {
	python := VenvPython(RuntimeVenvDir(globalDir))
	if _, err := os.Stat(python); err == nil {
		return python
	}
	// Fallback: python on PATH (dev mode)
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return python
}

// NeedsVenv returns true if no working runtime venv exists
// or if lingtai is not importable inside it. Existing legacy venvs without
// an environment marker are accepted after the import probe succeeds.
func NeedsVenv(globalDir string) bool {
	venvPath := RuntimeVenvDir(globalDir)
	python := VenvPython(venvPath)
	if _, err := os.Stat(python); err != nil {
		return true
	}
	markerState, _ := runtimeEnvMarkerStateForVenv(venvPath, nil)
	if markerState == runtimeEnvMarkerMismatch {
		return true
	}
	// Venv exists — verify lingtai is importable. A working PyPI install may
	// still need conversion to local dev/editable mode; that is handled by the
	// always-run CheckUpgrade/UpgradePythonRuntime path after this check, not by
	// recreating the whole venv here.
	if err := exec.Command(python, "-c", "import lingtai").Run(); err != nil {
		return true
	}
	if markerState == runtimeEnvMarkerMissing {
		_ = writeRuntimeEnvMarker(venvPath, nil)
	}
	return false
}

func EnsureVenv(globalDir string) error {
	return ensureVenv(globalDir, false, nil)
}

// ProgressFunc is called with an i18n key to report setup progress.
type ProgressFunc func(key string)

// EnsureVenvQuiet creates the venv without writing to stdout/stderr.
// Used when running inside the TUI (alt-screen).
func EnsureVenvQuiet(globalDir string, progress ProgressFunc) error {
	return ensureVenv(globalDir, true, progress)
}

func ensureVenv(globalDir string, quiet bool, progress ProgressFunc) error {
	if progress == nil {
		progress = func(string) {}
	}
	if !NeedsVenv(globalDir) {
		return nil
	}
	venvPath := RuntimeVenvDir(globalDir)
	if err := removeRuntimeVenvIfEnvMismatch(venvPath, nil); err != nil {
		return fmt.Errorf("failed to remove stale runtime venv: %w", err)
	}
	uvCmd := findUV()

	// Step 1: create venv
	progress("welcome.step_venv")
	os.MkdirAll(filepath.Dir(venvPath), 0o755)
	var cmd *exec.Cmd
	if uvCmd != "" {
		// uv can download Python automatically — request 3.13 to avoid conda/system conflicts
		cmd = exec.Command(uvCmd, "venv", "--python", "3.13", venvPath)
	} else {
		pythonCmd := findPython()
		if pythonCmd == "" {
			return fmt.Errorf("Python 3.11+ is required. Install it from python.org and try again")
		}
		cmd = exec.Command(pythonCmd, "-m", "venv", venvPath)
	}
	if !quiet {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create venv: %w", err)
	}

	// Verify Python version is 3.11+
	venvPython := VenvPython(venvPath)
	verOut, err := exec.Command(venvPython, "-c",
		"import sys; print(sys.version_info >= (3, 11))").Output()
	if err != nil || strings.TrimSpace(string(verOut)) != "True" {
		os.RemoveAll(venvPath)
		return fmt.Errorf("Python 3.11+ is required. Found older version in venv. Install python@3.13 and try again")
	}

	// Step 2: install lingtai
	progress("welcome.step_install")
	home, _ := os.UserHomeDir()
	dev, devMode := findDevCheckouts(home, nil)

	var install *exec.Cmd
	if devMode {
		name, args := devEditableInstallCommand(globalDir, venvPython, dev, exec.LookPath)
		install = exec.Command(name, args...)
	} else if uvCmd != "" {
		install = exec.Command(uvCmd, "pip", "install", "lingtai", "-p", venvPath)
	} else {
		var pipCmd string
		if runtime.GOOS == "windows" {
			pipCmd = filepath.Join(venvPath, "Scripts", "pip.exe")
		} else {
			pipCmd = filepath.Join(venvPath, "bin", "pip")
		}
		install = exec.Command(pipCmd, "install", "lingtai")
	}
	if !quiet {
		install.Stdout = os.Stdout
		install.Stderr = os.Stderr
	}
	if err := install.Run(); err != nil {
		return fmt.Errorf("failed to install lingtai. Check your internet connection and try again: %w", err)
	}

	// Step 3: verify installation
	progress("welcome.step_verify")
	python := VenvPython(venvPath)
	verify := exec.Command(python, "-c", "import lingtai; print(lingtai.__version__)")
	if !quiet {
		verify.Stdout = os.Stdout
		verify.Stderr = os.Stderr
	}
	if err := verify.Run(); err != nil {
		return fmt.Errorf("lingtai installed but import failed — check for missing dependencies: %w", err)
	}
	_ = writeRuntimeEnvMarker(venvPath, nil)

	// Step 4: symlink lingtai-agent CLI into ~/.local/bin so it's on PATH
	linkLingtaiCLI(venvPath)

	return nil
}

// linkLingtaiCLI creates a symlink to the venv's lingtai-agent entry point
// in a directory that's already on PATH. Tries brew prefix first (macOS),
// falls back to ~/.local/bin. Silently does nothing on error (best-effort).
func linkLingtaiCLI(venvPath string) {
	src := filepath.Join(venvPath, "bin", "lingtai-agent")
	if runtime.GOOS == "windows" {
		src = filepath.Join(venvPath, "Scripts", "lingtai-agent.exe")
	}
	if _, err := os.Stat(src); err != nil {
		return
	}

	binDir := findLinkDir()
	if binDir == "" {
		return
	}

	dst := filepath.Join(binDir, "lingtai-agent")
	if runtime.GOOS == "windows" {
		dst += ".exe"
	}

	// Remove stale symlink if it exists
	os.Remove(dst)
	os.Symlink(src, dst)
}

// findLinkDir returns a writable directory already on PATH.
func findLinkDir() string {
	// Prefer Homebrew bin (always on PATH for brew users)
	if out, err := exec.Command("brew", "--prefix").Output(); err == nil {
		brewBin := filepath.Join(strings.TrimSpace(string(out)), "bin")
		if writable(brewBin) {
			return brewBin
		}
	}
	// Fallback: ~/.local/bin
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	localBin := filepath.Join(home, ".local", "bin")
	os.MkdirAll(localBin, 0o755)
	return localBin
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".lingtai-probe-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

func findUV() string {
	if path, err := exec.LookPath("uv"); err == nil {
		return path
	}
	return ""
}

func findPython() string {
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// CheckTUIUpgrade compares the running TUI version against the latest GitHub release.
// Returns the latest version string if an upgrade is available, or "" if up-to-date.
// Non-blocking: silently returns "" on any error (offline, timeout, etc.).
func CheckTUIUpgrade(currentVersion string) string {
	currentClass := CompareReleaseVersions(currentVersion, "")
	if currentClass.Kind == ReleaseComparisonCurrentMissing || currentClass.Kind == ReleaseComparisonDevBuild || currentClass.Kind == ReleaseComparisonCurrentNonSemver {
		return ""
	}
	client := &http.Client{Timeout: 3 * time.Second}
	release, err := fetchLatestGitHubRelease(client)
	if err != nil {
		return ""
	}
	if CompareReleaseVersions(currentVersion, release.TagName).Kind == ReleaseComparisonUpdateAvailable {
		return release.TagName
	}
	return ""
}

// EnsureAddons verifies that every addon declared in an agent's init.json is
// importable by the Python interpreter that will run the agent.
//
// Addons ship as submodules of the lingtai package (lingtai.addons.<name>),
// so installing the lingtai wheel — or having it as an editable install — is
// sufficient to make every bundled addon available. There is nothing to
// pip-install per addon. This function only checks importability and returns
// a clear error if an addon is missing, so callers can surface the failure
// instead of silently launching an agent that will crash on first use.
func EnsureAddons(python, agentDir string) error {
	initPath := filepath.Join(agentDir, "init.json")
	data, err := os.ReadFile(initPath)
	if err != nil {
		return nil // no init.json → no addons to verify
	}
	var init map[string]interface{}
	if err := json.Unmarshal(data, &init); err != nil {
		return fmt.Errorf("parse init.json at %s: %w", initPath, err)
	}
	addonsRaw, ok := init["addons"].(map[string]interface{})
	if !ok || len(addonsRaw) == 0 {
		return nil // no addons declared
	}

	for addonName := range addonsRaw {
		modulePath := "lingtai.addons." + addonName
		cmd := exec.Command(python, "-c", "import "+modulePath)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			errMsg := strings.TrimSpace(stderr.String())
			if errMsg != "" {
				return fmt.Errorf("addon %q not importable as %s: %s (try: pip install --upgrade lingtai)", addonName, modulePath, errMsg)
			}
			return fmt.Errorf("addon %q not importable as %s: %w (try: pip install --upgrade lingtai)", addonName, modulePath, err)
		}
	}

	return nil
}

// CheckUpgrade compares installed lingtai version to PyPI latest.
// Runs pip install --upgrade if a newer version is available.
// Returns true if an upgrade was performed.
// Non-blocking: silently returns false on any error (offline, timeout, etc.).
func CheckUpgrade(globalDir string) bool {
	result := UpgradePythonRuntime(globalDir, false, &UpgradeRuntimeOptions{
		HTTPClient: &http.Client{Timeout: 3 * time.Second},
	})
	return result.Updated
}

// RuntimeEnsureOptions injects side effects for startup runtime tests.
type RuntimeEnsureOptions struct {
	NeedsVenvFunc    func(string) bool
	EnsureVenvFunc   func(string) error
	CheckUpgradeFunc func(string) bool
}

// EnsureRuntime ensures the managed Python runtime is usable, then always runs
// the non-blocking lingtai upgrade check. This is intentionally not an
// if/else: a venv that was just created or repaired may still have been
// installed from a stale wheel/cache, so startup should check PyPI in the same
// launch instead of waiting for the next launch.
func EnsureRuntime(globalDir string) (bool, error) {
	return ensureRuntimeWithOptions(globalDir, RuntimeEnsureOptions{})
}

// EnsureRuntimeQuiet is the alt-screen-safe variant used by first-run setup.
func EnsureRuntimeQuiet(globalDir string, progress ProgressFunc) (bool, error) {
	return ensureRuntimeWithOptions(globalDir, RuntimeEnsureOptions{
		EnsureVenvFunc: func(dir string) error { return EnsureVenvQuiet(dir, progress) },
	})
}

func ensureRuntimeWithOptions(globalDir string, opts RuntimeEnsureOptions) (bool, error) {
	if opts.NeedsVenvFunc == nil {
		opts.NeedsVenvFunc = NeedsVenv
	}
	if opts.EnsureVenvFunc == nil {
		opts.EnsureVenvFunc = EnsureVenv
	}
	if opts.CheckUpgradeFunc == nil {
		opts.CheckUpgradeFunc = CheckUpgrade
	}
	if opts.NeedsVenvFunc(globalDir) {
		if err := opts.EnsureVenvFunc(globalDir); err != nil {
			return false, err
		}
	}
	return opts.CheckUpgradeFunc(globalDir), nil
}

// DoctorSeverity classifies lines emitted by the forced doctor/update routine.
type DoctorSeverity string

const (
	DoctorInfo DoctorSeverity = "info"
	DoctorOK   DoctorSeverity = "ok"
	DoctorWarn DoctorSeverity = "warn"
	DoctorFail DoctorSeverity = "fail"
)

// DoctorLine is one human-readable diagnostic/update line.
type DoctorLine struct {
	Severity DoctorSeverity
	Text     string
}

// DoctorReport is returned by RunDoctorUpdate. Healthy is false only when a
// forced repair that should have succeeded failed (for example brew/pip failed,
// venv creation failed, or post-upgrade verification still reports the old
// version). Non-critical conditions such as "GitHub unreachable" are warnings
// because /doctor should still continue with local diagnostics.
type DoctorReport struct {
	Lines   []DoctorLine
	Healthy bool
}

func (r *DoctorReport) add(sev DoctorSeverity, format string, args ...interface{}) {
	r.Lines = append(r.Lines, DoctorLine{Severity: sev, Text: fmt.Sprintf(format, args...)})
	if sev == DoctorFail {
		r.Healthy = false
	}
}

// DoctorOptions controls RunDoctorUpdate.
type DoctorOptions struct {
	CurrentTUIVersion string
	ForceTUI          bool
	ForcePython       bool
	QuietEnsureVenv   bool

	// Test hooks. Production callers leave these nil.
	HTTPClient     *http.Client
	Runner         CommandRunner
	LookPath       func(string) (string, error)
	Executable     func() (string, error)
	Readlink       func(string) (string, error)
	Stat           func(string) (os.FileInfo, error)
	EnsureVenvFunc func(string) error
	// Home / LookupEnv override dev-checkout discovery. Production callers
	// leave them empty/nil (os.UserHomeDir / os.LookupEnv are used).
	Home      string
	LookupEnv func(string) (string, bool)
}

// CommandRunner is the minimal exec abstraction used by forced update code.
type CommandRunner interface {
	Run(name string, args ...string) CommandResult
}

type CommandResult struct {
	Stdout string
	Stderr string
	Err    error
}

// TUIInstallMethod identifies how the running lingtai-tui binary appears to
// have been installed. The updater layer uses it to choose the backend.
type TUIInstallMethod string

const (
	TUIInstallMethodSource   TUIInstallMethod = "source"
	TUIInstallMethodHomebrew TUIInstallMethod = "homebrew"
	TUIInstallMethodUnknown  TUIInstallMethod = "unknown"

	sourceInstallMetadataSchema = "lingtai.tui.install/v1"
)

// TUIInstallInfo is the install-method detection result used by doctor and
// future updater routing.
type TUIInstallInfo struct {
	Method       TUIInstallMethod
	Detail       string
	MetadataPath string
	Diagnostics  []DoctorLine
}

// Summary returns a human-readable label for the install method.
func (i TUIInstallInfo) Summary() string {
	label := string(i.Method)
	if i.Method == TUIInstallMethodSource {
		label = "source/user-local"
	} else if i.Method == TUIInstallMethodUnknown {
		label = "unknown/other"
	}
	if i.Detail == "" {
		return label
	}
	return fmt.Sprintf("%s (%s)", label, i.Detail)
}

type tuiInstallMetadata struct {
	Schema          string   `json:"schema"`
	SchemaVersion   int      `json:"schema_version"`
	InstallMethod   string   `json:"install_method"`
	Prefix          string   `json:"prefix"`
	BinDir          string   `json:"bin_dir"`
	RepoURL         string   `json:"repo_url"`
	RequestedRef    string   `json:"requested_ref"`
	ResolvedRef     string   `json:"resolved_ref"`
	ResolvedCommit  string   `json:"resolved_commit"`
	StampedVersion  string   `json:"stamped_version"`
	ManagedBinaries []string `json:"managed_binaries"`

	// Bundle provenance (additive; absent on install.json written before this
	// field existed, which read.go treats identically to KernelSource=="pypi").
	// Written by install.sh when it installs the Python `lingtai` runtime from
	// a pinned release-bundle artifact by explicit local file path, rather
	// than from PyPI. See kernelSourceFromMetadata / the provenance gate in
	// UpgradePythonRuntime below.
	KernelSource   string `json:"kernel_source,omitempty"`   // "" | "pypi" | "bundle"
	KernelBundleID string `json:"kernel_bundle_id,omitempty"` // e.g. "tui-v0.11.0" — the TUI bundle manifest's bundle_id
	KernelVersion  string `json:"kernel_version,omitempty"`   // the pinned kernel version installed from the bundle
	KernelProvider string `json:"kernel_provider,omitempty"`  // "github" | "gitee" — which provider served the bundle
}

type execCommandRunner struct{}

func (execCommandRunner) Run(name string, args ...string) CommandResult {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
}

// RunDoctorUpdate force-checks and repairs the two shipped update surfaces:
// the TUI binary and the managed Python `lingtai` runtime. It detects the TUI
// install method first; today's automated TUI upgrade path is only Homebrew,
// while source/user-local and unknown installs get explicit manual guidance.
// Python runtime upgrades are delegated to uv/pip, then verified afterwards.
func RunDoctorUpdate(globalDir string, opts DoctorOptions) DoctorReport {
	report := DoctorReport{Healthy: true}
	if opts.Runner == nil {
		opts.Runner = execCommandRunner{}
	}
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	if opts.Executable == nil {
		opts.Executable = os.Executable
	}
	if opts.Readlink == nil {
		opts.Readlink = os.Readlink
	}
	if opts.Stat == nil {
		opts.Stat = os.Stat
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if opts.EnsureVenvFunc == nil {
		opts.EnsureVenvFunc = EnsureVenv
		if opts.QuietEnsureVenv {
			opts.EnsureVenvFunc = func(dir string) error { return EnsureVenvQuiet(dir, nil) }
		}
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.LookupEnv
	}

	report.checkTUI(globalDir, opts)
	report.checkPythonRuntime(globalDir, opts)
	report.checkFileSearchNative(globalDir, opts)
	return report
}

func (r *DoctorReport) checkTUI(globalDir string, opts DoctorOptions) {
	current := strings.TrimSpace(opts.CurrentTUIVersion)
	exe, err := opts.Executable()
	if err != nil || exe == "" {
		r.add(DoctorWarn, "TUI executable: unknown (%v)", err)
	} else {
		r.add(DoctorInfo, "TUI executable: %s", exe)
	}
	install := detectTUIInstallMethod(globalDir, exe, opts)
	for _, line := range install.Diagnostics {
		r.add(line.Severity, "%s", line.Text)
	}
	r.add(DoctorInfo, "TUI install method: %s", install.Summary())
	if exe != "" {
		if target, linkErr := opts.Readlink(exe); linkErr == nil && target != "" {
			if install.Method != TUIInstallMethodHomebrew {
				r.add(DoctorWarn, "TUI executable is a symlink to %s; install method is %s, so automatic TUI updates are not routed through brew", target, install.Summary())
			} else {
				r.add(DoctorWarn, "TUI executable is a symlink to %s; brew may update the Cellar copy without changing this dev/manual link", target)
			}
		}
	}
	if current == "" {
		r.add(DoctorInfo, "TUI version: unknown")
	} else {
		r.add(DoctorInfo, "TUI version: %s", current)
	}

	currentClass := CompareReleaseVersions(current, "")
	switch currentClass.Kind {
	case ReleaseComparisonCurrentMissing:
		r.add(DoctorWarn, "Skipping TUI release upgrade because the current TUI version is unknown")
		return
	case ReleaseComparisonDevBuild:
		r.add(DoctorWarn, "Skipping TUI release upgrade for dev build %q", current)
		return
	}
	release, err := fetchLatestGitHubRelease(opts.HTTPClient)
	if err != nil {
		r.add(DoctorWarn, "Could not check latest TUI release on GitHub: %v", err)
		return
	}
	r.add(DoctorInfo, "Latest TUI release: %s", release.TagName)

	// Startup collapses non-comparable release checks to "no update" so it
	// will not prompt for brew; /doctor keeps them distinct warnings instead
	// of reporting a clean up-to-date result.
	comparison := CompareReleaseVersions(current, release.TagName)
	switch comparison.Kind {
	case ReleaseComparisonUpdateAvailable:
		r.add(DoctorWarn, "TUI update available: %s → %s", current, release.TagName)
	case ReleaseComparisonUpToDate:
		r.add(DoctorOK, "TUI is up to date")
		return
	case ReleaseComparisonCurrentNonSemver:
		r.add(DoctorWarn, "Cannot compare TUI release versions: current version %q is not a strict vX.Y.Z release; latest is %q", current, release.TagName)
		return
	case ReleaseComparisonLatestNonSemver:
		r.add(DoctorWarn, "Cannot compare TUI release versions: latest release tag %q is not a strict vX.Y.Z release; current is %q", release.TagName, current)
		return
	case ReleaseComparisonCurrentMissing:
		r.add(DoctorWarn, "Skipping TUI release upgrade because the current TUI version is unknown")
		return
	case ReleaseComparisonDevBuild:
		r.add(DoctorWarn, "Skipping TUI release upgrade for dev build %q", current)
		return
	}
	if !opts.ForceTUI {
		return
	}
	update := RunTUIUpdate(install, TUIUpdateOptions{
		LatestVersion:         release.TagName,
		GlobalDir:             globalDir,
		Runner:                opts.Runner,
		LookPath:              opts.LookPath,
		Stat:                  opts.Stat,
		IncludeHomebrewUpdate: true,
		ResolveHomebrewPath:   true,
	})
	for _, line := range update.Lines {
		r.add(line.Severity, "%s", line.Text)
	}
	if !update.Healthy {
		r.Healthy = false
	}
}

func detectTUIInstallMethod(globalDir, exe string, opts DoctorOptions) TUIInstallInfo {
	info := TUIInstallInfo{
		Method:       TUIInstallMethodUnknown,
		Detail:       "no matching source metadata or Homebrew path",
		MetadataPath: filepath.Join(globalDir, "install.json"),
	}
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.LookupEnv
	}

	meta, err := readTUIInstallMetadata(info.MetadataPath)
	if err == nil {
		if isSourceInstallMetadata(meta) {
			if exe == "" || sourceMetadataMatchesExecutable(meta, exe) {
				detail := fmt.Sprintf("metadata at %s", info.MetadataPath)
				if meta.Prefix != "" {
					detail = fmt.Sprintf("%s, prefix %s", detail, meta.Prefix)
				}
				return TUIInstallInfo{Method: TUIInstallMethodSource, Detail: detail, MetadataPath: info.MetadataPath}
			}
			info.Diagnostics = append(info.Diagnostics, DoctorLine{
				Severity: DoctorWarn,
				Text:     fmt.Sprintf("TUI install metadata at %s does not match executable %s; ignoring it", info.MetadataPath, exe),
			})
		} else {
			info.Diagnostics = append(info.Diagnostics, DoctorLine{
				Severity: DoctorWarn,
				Text:     fmt.Sprintf("TUI install metadata at %s is not recognized as source install metadata; ignoring it", info.MetadataPath),
			})
		}
	} else if !os.IsNotExist(err) {
		info.Diagnostics = append(info.Diagnostics, DoctorLine{
			Severity: DoctorWarn,
			Text:     fmt.Sprintf("TUI install metadata at %s could not be read: %v", info.MetadataPath, err),
		})
	}

	homebrewExe := exe
	if exe != "" {
		// Intel macOS Homebrew launches can report /usr/local/bin/lingtai-tui
		// here; resolve it before matching Cellar paths, but ignore failures.
		if resolvedExe, err := filepath.EvalSymlinks(exe); err == nil && resolvedExe != "" {
			homebrewExe = resolvedExe
		}
	}
	if ok, detail := detectHomebrewTUIInstall(homebrewExe, opts.LookupEnv); ok {
		info.Method = TUIInstallMethodHomebrew
		info.Detail = detail
	}
	return info
}

func readTUIInstallMetadata(path string) (tuiInstallMetadata, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return tuiInstallMetadata{}, err
	}
	var meta tuiInstallMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return tuiInstallMetadata{}, err
	}
	return meta, nil
}

func isSourceInstallMetadata(meta tuiInstallMetadata) bool {
	return meta.Schema == sourceInstallMetadataSchema &&
		meta.SchemaVersion == 1 &&
		meta.InstallMethod == "source"
}

func valueOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// kernelBundleProvenance reads install.json (if present) and reports whether
// the Python `lingtai` runtime was provisioned by install.sh from a pinned
// release-bundle artifact (kernel_source=="bundle") rather than a bare `pip
// install lingtai` against PyPI. Missing install.json, an unparsable file, or
// any kernel_source value other than "bundle" (including the empty string
// legacy installs leave it at) all report false — the same fail-open-to-PyPI
// default as before this field existed.
func kernelBundleProvenance(globalDir string) (isBundle bool, meta tuiInstallMetadata) {
	metaPath := filepath.Join(globalDir, "install.json")
	meta, err := readTUIInstallMetadata(metaPath)
	if err != nil {
		return false, tuiInstallMetadata{}
	}
	return meta.KernelSource == "bundle", meta
}

func sourceMetadataMatchesExecutable(meta tuiInstallMetadata, exe string) bool {
	if exe == "" {
		return false
	}
	for _, managed := range meta.ManagedBinaries {
		if samePath(managed, exe) {
			return true
		}
	}
	if meta.BinDir != "" && samePath(filepath.Join(meta.BinDir, "lingtai-tui"), exe) {
		return true
	}
	// install.sh also creates a "lingtai" alias symlink in bin_dir pointing at
	// lingtai-tui. Invoking through it (e.g. `lingtai doctor`) makes
	// os.Executable report the unresolved alias path on macOS, which matches no
	// managed binary; treat the alias as owned by this source install.
	if meta.BinDir != "" && samePath(filepath.Join(meta.BinDir, "lingtai"), exe) {
		return true
	}
	return false
}

func detectHomebrewTUIInstall(exe string, lookupEnv func(string) (string, bool)) (bool, string) {
	if exe == "" {
		return false, ""
	}
	for _, key := range []string{"HOMEBREW_PREFIX", "HOMEBREW_CELLAR", "HOMEBREW_REPOSITORY"} {
		if lookupEnv == nil {
			continue
		}
		value, ok := lookupEnv(key)
		if ok && value != "" && pathWithin(exe, value) {
			return true, fmt.Sprintf("executable under $%s (%s)", key, value)
		}
	}
	slash := filepath.ToSlash(filepath.Clean(exe))
	if strings.Contains(slash, "/Cellar/") {
		return true, "executable under a Homebrew Cellar path"
	}
	for _, prefix := range []string{
		"/opt/homebrew",
		"/home/linuxbrew/.linuxbrew",
		"/usr/local/Homebrew",
		"/usr/local/Cellar",
	} {
		if pathWithin(exe, prefix) {
			return true, fmt.Sprintf("executable under %s", prefix)
		}
	}
	return false, ""
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func pathWithin(path, dir string) bool {
	path = filepath.Clean(path)
	dir = filepath.Clean(dir)
	if samePath(path, dir) {
		return true
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func (r *DoctorReport) checkPythonRuntime(globalDir string, opts DoctorOptions) {
	venvPath := RuntimeVenvDir(globalDir)
	python := VenvPython(venvPath)
	needsEnsure := false
	if _, err := opts.Stat(python); err != nil {
		r.add(DoctorWarn, "Python runtime venv missing or incomplete at %s", venvPath)
		needsEnsure = true
	} else {
		markerState, markerErr := runtimeEnvMarkerStateForVenv(venvPath, opts.Runner)
		if markerState == runtimeEnvMarkerMismatch {
			r.add(DoctorWarn, "Python runtime venv environment marker does not match this host; recreating %s", venvPath)
			needsEnsure = true
		} else {
			if markerErr != nil {
				r.add(DoctorWarn, "Python runtime venv environment marker could not be verified; leaving it in place unless the runtime itself is unusable: %v", markerErr)
			}
			if _, err := pythonLingtaiVersion(opts.Runner, python); err != nil {
				r.add(DoctorWarn, "Python runtime venv exists but cannot import lingtai: %v", err)
				needsEnsure = true
			} else {
				writeRuntimeEnvMarkerIfVenvDirExists(venvPath, opts.Runner)
			}
		}
	}
	if needsEnsure {
		r.add(DoctorInfo, "Creating Python runtime venv...")
		if err := opts.EnsureVenvFunc(globalDir); err != nil {
			r.add(DoctorFail, "Failed to create Python runtime venv: %v", err)
			return
		}
		if _, err := opts.Stat(python); err != nil {
			r.add(DoctorFail, "Python runtime venv was created, but %s is still missing: %v", python, err)
			return
		}
		r.add(DoctorOK, "Python runtime venv created")
		writeRuntimeEnvMarkerIfVenvDirExists(venvPath, opts.Runner)
	}
	upgrade := UpgradePythonRuntime(globalDir, opts.ForcePython, &UpgradeRuntimeOptions{
		HTTPClient: opts.HTTPClient,
		Runner:     opts.Runner,
		LookPath:   opts.LookPath,
		Stat:       opts.Stat,
		Home:       opts.Home,
		LookupEnv:  opts.LookupEnv,
	})
	for _, line := range upgrade.Lines {
		r.add(line.Severity, "%s", line.Text)
	}
	if !upgrade.Healthy {
		r.Healthy = false
	} else {
		writeRuntimeEnvMarkerIfVenvDirExists(venvPath, opts.Runner)
	}
}

func (r *DoctorReport) checkFileSearchNative(globalDir string, opts DoctorOptions) {
	python := VenvPython(RuntimeVenvDir(globalDir))
	if _, err := opts.Stat(python); err != nil {
		r.add(DoctorWarn, "File search native backend: skipped because Python runtime is missing: %v", err)
		return
	}
	status, err := FileSearchNativeStatus(globalDir, opts.Runner)
	if err != nil {
		r.add(DoctorWarn, "File search native backend: could not inspect runtime sidecar status: %v", err)
		return
	}
	if status.Unsupported {
		r.add(DoctorInfo, "File search native backend: installed Python runtime does not expose Rust sidecar diagnostics yet; upgrade the lingtai Python package after the Rust sidecar release to enable this check")
		return
	}
	if status.SidecarPath != "" {
		r.add(DoctorOK, "File search native backend: Rust sidecar available (%s)", status.SidecarPath)
	} else if status.Backend == "RustFileIOBackend" {
		r.add(DoctorOK, "File search native backend: Rust backend active")
	} else {
		r.add(DoctorWarn, "File search native backend: Python fallback active; no packaged Rust sidecar was found")
	}
	if cargo, err := opts.LookPath("cargo"); err == nil && cargo != "" {
		r.add(DoctorOK, "Rust toolchain: cargo found at %s", cargo)
	} else if status.SidecarPath != "" || status.Backend == "RustFileIOBackend" {
		r.add(DoctorInfo, "Rust toolchain: cargo not found, but bundled sidecar is already available at runtime")
	} else {
		r.add(DoctorWarn, "Rust toolchain: cargo not found; install Rust from https://rustup.rs or install a platform wheel with the bundled sidecar to enable accelerated glob/grep")
	}
}

type FileSearchStatus struct {
	Backend     string
	SidecarPath string
	Unsupported bool
}

type timeoutCommandRunner struct {
	timeout time.Duration
}

func (r timeoutCommandRunner) Run(name string, args ...string) CommandResult {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), Err: fmt.Errorf("timed out after %s", timeout)}
	}
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
}

// FileSearchNativeStatus asks the managed Python runtime which file-search
// backend it will use. The probe is intentionally Python-side because the
// sidecar resolver lives in the `lingtai` package installed inside that venv.
// Production calls use a short timeout so startup and /doctor cannot hang on a
// slow or broken managed Python runtime; tests may pass a runner.
func FileSearchNativeStatus(globalDir string, runner CommandRunner) (FileSearchStatus, error) {
	if runner == nil {
		runner = timeoutCommandRunner{timeout: 3 * time.Second}
	}
	python := VenvPython(RuntimeVenvDir(globalDir))
	script := `
import tempfile
from lingtai.services.file_io_sidecar import default_file_io_service, resolve_sidecar_binary
binary = resolve_sidecar_binary()
service = default_file_io_service(tempfile.gettempdir(), backend="auto")
backend = type(getattr(service, "_backend", service)).__name__
print("BACKEND " + backend)
print("SIDECAR " + (str(binary) if binary else ""))
`
	res := runner.Run(python, "-c", script)
	if res.Err != nil {
		stderr := strings.TrimSpace(res.Stderr)
		if strings.Contains(stderr, "ModuleNotFoundError") && strings.Contains(stderr, "file_io_sidecar") {
			return FileSearchStatus{Unsupported: true}, nil
		}
		return FileSearchStatus{}, fmt.Errorf("probe failed: %v: %s", res.Err, stderr)
	}
	status := FileSearchStatus{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "BACKEND "):
			status.Backend = strings.TrimSpace(strings.TrimPrefix(line, "BACKEND "))
		case strings.HasPrefix(line, "SIDECAR "):
			status.SidecarPath = strings.TrimSpace(strings.TrimPrefix(line, "SIDECAR "))
		}
	}
	if status.Backend == "" {
		return FileSearchStatus{}, fmt.Errorf("probe returned no backend line: %q", strings.TrimSpace(res.Stdout))
	}
	return status, nil
}

type latestRelease struct {
	TagName string `json:"tag_name"`
}

func fetchLatestGitHubRelease(client *http.Client) (latestRelease, error) {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	resp, err := client.Get("https://api.github.com/repos/Lingtai-AI/lingtai/releases/latest")
	if err != nil {
		return latestRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return latestRelease{}, fmt.Errorf("GitHub returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var release latestRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return latestRelease{}, err
	}
	if release.TagName == "" {
		return latestRelease{}, fmt.Errorf("GitHub latest release had no tag_name")
	}
	return release, nil
}

func releaseNewer(currentVersion, latestTag string) bool {
	return CompareReleaseVersions(currentVersion, latestTag).Kind == ReleaseComparisonUpdateAvailable
}

// ReleaseComparisonKind is the typed outcome of comparing a running TUI version
// with a release tag. It distinguishes malformed and dev versions from ordinary
// "not newer" results.
type ReleaseComparisonKind string

const (
	ReleaseComparisonUpdateAvailable  ReleaseComparisonKind = "update_available"
	ReleaseComparisonUpToDate         ReleaseComparisonKind = "up_to_date"
	ReleaseComparisonCurrentMissing   ReleaseComparisonKind = "current_missing"
	ReleaseComparisonCurrentNonSemver ReleaseComparisonKind = "current_non_semver"
	ReleaseComparisonLatestNonSemver  ReleaseComparisonKind = "latest_non_semver"
	ReleaseComparisonDevBuild         ReleaseComparisonKind = "dev_build"
)

// ReleaseComparison is the structured result of a strict vX.Y.Z release
// comparison.
type ReleaseComparison struct {
	Kind           ReleaseComparisonKind
	CurrentVersion string
	LatestTag      string
}

// CompareReleaseVersions classifies the comparison between the running version
// and the latest release tag. Versions must be strict three-part releases,
// optionally prefixed with "v"; current dev builds are classified before any
// latest-tag validation so callers can skip network work when appropriate.
func CompareReleaseVersions(currentVersion, latestTag string) ReleaseComparison {
	current := strings.TrimSpace(currentVersion)
	latest := strings.TrimSpace(latestTag)
	comparison := ReleaseComparison{
		CurrentVersion: current,
		LatestTag:      latest,
	}
	if current == "" {
		comparison.Kind = ReleaseComparisonCurrentMissing
		return comparison
	}
	if isDevReleaseVersion(current) {
		comparison.Kind = ReleaseComparisonDevBuild
		return comparison
	}

	currentParsed := parseReleaseVersion(current)
	if currentParsed == nil {
		comparison.Kind = ReleaseComparisonCurrentNonSemver
		return comparison
	}
	latestParsed := parseReleaseVersion(latest)
	if latestParsed == nil {
		comparison.Kind = ReleaseComparisonLatestNonSemver
		return comparison
	}
	for i := range latestParsed {
		if latestParsed[i] != currentParsed[i] {
			if latestParsed[i] > currentParsed[i] {
				comparison.Kind = ReleaseComparisonUpdateAvailable
			} else {
				comparison.Kind = ReleaseComparisonUpToDate
			}
			return comparison
		}
	}
	comparison.Kind = ReleaseComparisonUpToDate
	return comparison
}

func isDevReleaseVersion(version string) bool {
	version = strings.TrimSpace(version)
	return strings.EqualFold(version, "dev") || strings.Contains(version, "-")
}

func parseReleaseVersion(version string) []int {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" || strings.Contains(version, "-") {
		return nil
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return nil
	}
	parsed := make([]int, len(parts))
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		parsed[i] = value
	}
	return parsed
}

// UpgradeRuntimeOptions injects side effects for tests.
type UpgradeRuntimeOptions struct {
	HTTPClient *http.Client
	Runner     CommandRunner
	LookPath   func(string) (string, error)
	Stat       func(string) (os.FileInfo, error)

	// Home overrides the home directory used to discover local dev checkouts.
	// Production callers leave it empty (os.UserHomeDir is used).
	Home string
	// LookupEnv overrides environment lookups (LINGTAI_DEV_ROOT). Production
	// callers leave it nil (os.LookupEnv is used).
	LookupEnv func(string) (string, bool)
}

type UpgradeRuntimeResult struct {
	Lines   []DoctorLine
	Updated bool
	Healthy bool
}

func (r *UpgradeRuntimeResult) add(sev DoctorSeverity, format string, args ...interface{}) {
	r.Lines = append(r.Lines, DoctorLine{Severity: sev, Text: fmt.Sprintf(format, args...)})
	if sev == DoctorFail {
		r.Healthy = false
	}
}

// UpgradePythonRuntime compares installed lingtai to PyPI and runs a forced
// `pip install --upgrade lingtai` when force=true, even if versions already
// match. All command failures and post-install verification failures are
// reported in the returned lines.
func UpgradePythonRuntime(globalDir string, force bool, opts *UpgradeRuntimeOptions) UpgradeRuntimeResult {
	if opts == nil {
		opts = &UpgradeRuntimeOptions{}
	}
	if opts.Runner == nil {
		opts.Runner = execCommandRunner{}
	}
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	if opts.Stat == nil {
		opts.Stat = os.Stat
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	result := UpgradeRuntimeResult{Healthy: true}
	venvPath := RuntimeVenvDir(globalDir)
	python := VenvPython(venvPath)
	if _, err := opts.Stat(python); err != nil {
		result.add(DoctorWarn, "Python runtime venv not found at %s", python)
		return result
	}
	installed, err := pythonLingtaiVersion(opts.Runner, python)
	if err != nil {
		result.add(DoctorFail, "Could not import lingtai from %s: %v", python, err)
		return result
	}
	result.add(DoctorInfo, "Installed Python lingtai: %s", installed)

	// Dev-checkout conversion: on a machine with local lingtai-kernel/lingtai
	// source, the managed runtime should track that source via an editable
	// install, not the PyPI wheel. This runs BEFORE the generic editable gate
	// below so that a working PyPI install (or an editable install pointed at a
	// stale/moved checkout) is reinstalled editable against the local source.
	// When the runtime is already editable for the discovered checkout, it is
	// left untouched so this does not reinstall on every launch.
	home := opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" {
		if dev, ok := findDevCheckouts(home, opts.LookupEnv); ok {
			editable, source := isEditableLingtaiInstall(opts.Runner, python)
			if editable && dev.isEditableForKernel(source) {
				result.add(DoctorOK, "Python lingtai already editable for local dev checkout (%s); skipping reinstall", dev.KernelSrc)
				writeRuntimeEnvMarkerIfVenvDirExists(venvPath, opts.Runner)
				return result
			}
			result.add(DoctorInfo, "Local dev checkout detected at %s; installing lingtai editable to replace the %s runtime",
				dev.KernelSrc, devRuntimeKind(editable))
			argsName, args := devEditableInstallCommand(globalDir, python, dev, opts.LookPath)
			result.add(DoctorInfo, "Running: %s %s", argsName, strings.Join(args, " "))
			cmdResult := opts.Runner.Run(argsName, args...)
			appendCommandOutputToRuntime(&result, cmdResult)
			if cmdResult.Err != nil {
				result.add(DoctorFail, "Editable dev install failed: %v", cmdResult.Err)
				return result
			}
			if _, err := pythonLingtaiVersion(opts.Runner, python); err != nil {
				result.add(DoctorFail, "lingtai import failed after editable dev install: %v", err)
				return result
			}
			result.Updated = true
			result.add(DoctorOK, "Python lingtai runtime switched to editable dev install")
			writeRuntimeEnvMarkerIfVenvDirExists(venvPath, opts.Runner)
			return result
		}
	}

	// Dev-mode gate: when lingtai was installed editable (pip/uv -e), leave it
	// alone. A PyPI wheel reinstall would silently clobber the editable link
	// and undo the user's local source checkout — the symptom CLAUDE.md
	// "Auto-upgrader can clobber the editable install" warns about. The doctor
	// path (force=true) also respects this: a forced repair that nukes a dev
	// setup is more destructive than the broken state it was trying to fix,
	// and the user can always re-create dev mode explicitly with `uv pip
	// install -e`.
	if editable, editableSource := isEditableLingtaiInstall(opts.Runner, python); editable {
		if editableSource != "" {
			result.add(DoctorOK, "Python lingtai is an editable install (%s); skipping upgrade", editableSource)
		} else {
			result.add(DoctorOK, "Python lingtai is an editable install; skipping upgrade")
		}
		writeRuntimeEnvMarkerIfVenvDirExists(venvPath, opts.Runner)
		return result
	}

	// Bundle-provenance gate: install.sh's Gitee/GitHub bundle path installs
	// the Python `lingtai` runtime from a pinned, checksum-verified release
	// artifact by explicit local file path — LingTai is NEVER installed by
	// requesting the package name "lingtai" from any index; there is no PyPI
	// fallback for it (RELEASING.md; see install.sh's
	// install_kernel_from_bundle). This gate applies to BOTH a routine
	// upgrade AND a forced one (doctor/`/update --force`): "force" means
	// "run the upgrade check even if we'd otherwise skip it as up to date,"
	// not "abandon the compatibility pin and install an arbitrary version
	// from a package index." A forced check on a bundle-provisioned runtime
	// reports that the kernel is pinned to the TUI bundle and points at the
	// bundle-update path (re-run the one-command installer / `--update`)
	// instead of running any PyPI query or install — unlike the editable-
	// install gate above, this is not "same behavior, force or not" by
	// accident; it is the explicit fix for a real defect where force used to
	// fall through to PyPI here.
	if isBundle, bundleMeta := kernelBundleProvenance(globalDir); isBundle {
		if force {
			result.add(DoctorOK,
				"Python lingtai is pinned to release bundle %s (kernel %s via %s); a forced update does not override this pin. Re-run the one-command installer (or its --update path) to move to a newer bundle.",
				valueOrUnknown(bundleMeta.KernelBundleID), valueOrUnknown(bundleMeta.KernelVersion), valueOrUnknown(bundleMeta.KernelProvider))
		} else {
			result.add(DoctorOK,
				"Python lingtai was installed from a pinned release bundle (%s, kernel %s via %s); skipping PyPI upgrade.",
				valueOrUnknown(bundleMeta.KernelBundleID), valueOrUnknown(bundleMeta.KernelVersion), valueOrUnknown(bundleMeta.KernelProvider))
		}
		writeRuntimeEnvMarkerIfVenvDirExists(venvPath, opts.Runner)
		return result
	}

	// Legacy PyPI-compare/upgrade path: reached only for a runtime that is
	// neither an editable dev install nor bundle-provisioned (no
	// kernel_source metadata at all — installs that predate install.sh's
	// bundle path, or that were never upgraded through it). This is
	// pre-existing migration behavior, kept minimal and unchanged rather than
	// retracted here: doing so is out of scope for this fix, and PyPI is not
	// being newly introduced as a source for LingTai by keeping it. It is
	// NOT the product's canonical install path going forward — every new
	// install goes through install.sh's mandatory bundle path (RELEASING.md)
	// and is never reached here (see the bundle-provenance gate above). A
	// legacy runtime naturally migrates to kernel_source=="bundle" bookkeeping
	// the next time a human re-runs the one-command installer.
	latest, err := fetchLatestPyPIVersion(opts.HTTPClient)
	if err != nil {
		result.add(DoctorWarn, "Could not check latest Python lingtai on PyPI: %v", err)
		if !force {
			writeRuntimeEnvMarkerIfVenvDirExists(venvPath, opts.Runner)
			return result
		}
	} else {
		result.add(DoctorInfo, "Latest Python lingtai on PyPI: %s", latest)
		if !force && installed == latest {
			result.add(DoctorOK, "Python lingtai runtime is up to date")
			writeRuntimeEnvMarkerIfVenvDirExists(venvPath, opts.Runner)
			return result
		}
	}

	argsName, args := runtimeUpgradeCommand(globalDir, python, opts.LookPath)
	result.add(DoctorInfo, "Running: %s %s", argsName, strings.Join(args, " "))
	cmdResult := opts.Runner.Run(argsName, args...)
	appendCommandOutputToRuntime(&result, cmdResult)
	if cmdResult.Err != nil {
		result.add(DoctorFail, "Python lingtai upgrade command failed: %v", cmdResult.Err)
		return result
	}

	post, err := pythonLingtaiVersion(opts.Runner, python)
	if err != nil {
		result.add(DoctorFail, "Python lingtai import failed after upgrade: %v", err)
		return result
	}
	result.add(DoctorInfo, "Python lingtai after upgrade: %s", post)
	if latest != "" && post != latest {
		result.add(DoctorFail, "Python lingtai is still %s after upgrade; expected %s", post, latest)
		return result
	}
	result.Updated = true
	result.add(DoctorOK, "Python lingtai runtime verified after upgrade")
	writeRuntimeEnvMarkerIfVenvDirExists(venvPath, opts.Runner)
	return result
}

func fetchLatestPyPIVersion(client *http.Client) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	resp, err := client.Get("https://pypi.org/pypi/lingtai/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("PyPI returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var pypi struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pypi); err != nil {
		return "", err
	}
	if pypi.Info.Version == "" {
		return "", fmt.Errorf("PyPI response had no info.version")
	}
	return pypi.Info.Version, nil
}

func pythonLingtaiVersion(runner CommandRunner, python string) (string, error) {
	res := runner.Run(python, "-c", "import lingtai; print(lingtai.__version__)")
	if res.Err != nil {
		detail := strings.TrimSpace(res.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(res.Stdout)
		}
		if detail == "" {
			detail = res.Err.Error()
		}
		return "", fmt.Errorf("%s", lastNonEmptyLine(detail))
	}
	return strings.TrimSpace(res.Stdout), nil
}

// isEditableLingtaiInstall reports whether the lingtai distribution in the
// given Python interpreter was installed in editable mode (pip/uv -e ...).
// Detection follows PEP 610: the install records a direct_url.json file
// inside the package's .dist-info/ directory; editable installs set
// dir_info.editable: true. The second return is the editable source path
// (e.g. file:///Users/.../lingtai-kernel) if available, for the log line.
// Returns (false, "") on any error so a missing or malformed direct_url is
// treated as "regular wheel install" — the conservative default that lets the
// upgrade proceed.
func isEditableLingtaiInstall(runner CommandRunner, python string) (bool, string) {
	const detect = `
import sys
try:
    from importlib.metadata import distribution
    d = distribution("lingtai")
    raw = d.read_text("direct_url.json") or ""
    import json
    info = json.loads(raw)
    if info.get("dir_info", {}).get("editable") is True:
        print("EDITABLE", info.get("url", ""))
    else:
        print("WHEEL")
except Exception:
    print("WHEEL")
`
	res := runner.Run(python, "-c", detect)
	if res.Err != nil {
		return false, ""
	}
	out := strings.TrimSpace(res.Stdout)
	if !strings.HasPrefix(out, "EDITABLE") {
		return false, ""
	}
	source := strings.TrimSpace(strings.TrimPrefix(out, "EDITABLE"))
	return true, source
}

func runtimeUpgradeCommand(globalDir, python string, lookPath func(string) (string, error)) (string, []string) {
	if uv, err := lookPath("uv"); err == nil && uv != "" {
		return uv, []string{"pip", "install", "--upgrade", "lingtai", "-p", RuntimeVenvDir(globalDir)}
	}
	pipCmd := filepath.Join(filepath.Dir(python), "pip")
	if runtime.GOOS == "windows" {
		pipCmd = filepath.Join(filepath.Dir(python), "pip.exe")
	}
	return pipCmd, []string{"install", "--upgrade", "lingtai"}
}

// devRuntimeKind labels the runtime being replaced, for the log line.
func devRuntimeKind(editable bool) string {
	if editable {
		return "stale editable"
	}
	return "PyPI wheel"
}

// devEditableInstallCommand builds the `pip install -e <kernel>` invocation
// that replaces the current runtime install with one tracking the local
// checkout. Prefers uv (with -p <venv>) and falls back to the venv's pip.
// Only the kernel is installed editable — it is the source of the `lingtai`
// package; the sibling lingtai/ TUI repo is not a Python package.
func devEditableInstallCommand(globalDir, python string, dev devCheckout, lookPath func(string) (string, error)) (string, []string) {
	targets := dev.installTargets()
	if uv, err := lookPath("uv"); err == nil && uv != "" {
		args := []string{"pip", "install"}
		for _, t := range targets {
			args = append(args, "-e", t)
		}
		args = append(args, "-p", RuntimeVenvDir(globalDir))
		return uv, args
	}
	pipCmd := filepath.Join(filepath.Dir(python), "pip")
	if runtime.GOOS == "windows" {
		pipCmd = filepath.Join(filepath.Dir(python), "pip.exe")
	}
	args := []string{"install"}
	for _, t := range targets {
		args = append(args, "-e", t)
	}
	return pipCmd, args
}

func appendCommandOutputToRuntime(r *UpgradeRuntimeResult, res CommandResult) {
	for _, line := range interestingCommandLines(res.Stdout, res.Stderr) {
		r.add(DoctorInfo, "  %s", line)
	}
}

// interestingCommandLines flattens captured command stdout and stderr into a
// slice of non-empty trimmed lines. Output is never truncated: doctor users
// rely on seeing the full pip/brew failure to know what actually went wrong,
// and silently dropping the middle of a stack trace turns a 30-second debug
// into a re-run.
func interestingCommandLines(outputs ...string) []string {
	var lines []string
	for _, out := range outputs {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

func lastNonEmptyLine(s string) string {
	parts := strings.Split(s, "\n")
	for i := len(parts) - 1; i >= 0; i-- {
		if trimmed := strings.TrimSpace(parts[i]); trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(s)
}
