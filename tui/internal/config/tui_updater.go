package config

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// TUIUpdater is the install-method-specific backend for TUI binary updates.
// It is deliberately narrow: version checks and prompting stay with callers,
// while the backend owns the mutation or guidance for one install method.
type TUIUpdater interface {
	InstallMethod() TUIInstallMethod
	Upgrade(TUIUpdateOptions) TUIUpdateResult
}

// TUIUpdateOptions injects side effects for TUI updater backends.
type TUIUpdateOptions struct {
	LatestVersion string
	GlobalDir     string

	Runner   CommandRunner
	LookPath func(string) (string, error)
	Stat     func(string) (os.FileInfo, error)

	Install TUIInstallInfo

	// SourceInstallScript overrides the installer source for tests. Production
	// uses the versioned raw GitHub install.sh URL.
	SourceInstallScript string
	// Executable overrides os.Executable for the Homebrew adoption backend so
	// it can infer the install prefix from the running binary in tests.
	Executable func() (string, error)
}

// TUIUpdateResult is the backend result consumed by doctor and startup.
type TUIUpdateResult struct {
	Lines   []DoctorLine
	Healthy bool
	Updated bool
	Err     error
}

func (r *TUIUpdateResult) add(sev DoctorSeverity, format string, args ...interface{}) {
	r.Lines = append(r.Lines, DoctorLine{Severity: sev, Text: fmt.Sprintf(format, args...)})
	if sev == DoctorFail {
		r.Healthy = false
	}
}

// DetectCurrentTUIInstall reports the install method for the running binary
// with production side effects. Tests can use detectTUIInstallMethod directly.
func DetectCurrentTUIInstall(globalDir string) TUIInstallInfo {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return detectTUIInstallMethod(globalDir, exe, DoctorOptions{})
}

// SelectTUIUpdater returns the backend for the detected install method.
func SelectTUIUpdater(install TUIInstallInfo) TUIUpdater {
	switch install.Method {
	case TUIInstallMethodHomebrew:
		return homebrewTUIUpdater{}
	case TUIInstallMethodSource:
		return sourceTUIUpdater{}
	default:
		return unknownTUIUpdater{}
	}
}

// RunTUIUpdate selects and runs the backend for the detected install method.
func RunTUIUpdate(install TUIInstallInfo, opts TUIUpdateOptions) TUIUpdateResult {
	opts.Install = install
	return SelectTUIUpdater(install).Upgrade(opts)
}

// ManualTUIUpdateOptions injects side effects for `lingtai-tui self-update`.
type ManualTUIUpdateOptions struct {
	CurrentTUIVersion string

	HTTPClient *http.Client
	Runner     CommandRunner
	LookPath   func(string) (string, error)
	Stat       func(string) (os.FileInfo, error)
	Executable func() (string, error)
	LookupEnv  func(string) (string, bool)

	SourceInstallScript string
}

// RunManualTUIUpdate detects the current install method and runs the matching
// TUI updater backend. Source/user-local and Homebrew installs update through
// the GitHub Release installer (Homebrew installs are adopted onto
// GitHub-Release management); unknown installs are a command failure that
// reports non-mutating manual guidance. A source/user-local install already at
// the latest release short-circuits as a no-op, but a Homebrew install still
// runs so it can convert to GitHub-Release-managed, and an unknown install
// still runs so the caller sees the unsupported guidance.
func RunManualTUIUpdate(globalDir string, opts ManualTUIUpdateOptions) TUIUpdateResult {
	result := TUIUpdateResult{Healthy: true}
	if opts.Executable == nil {
		opts.Executable = os.Executable
	}

	exe, err := opts.Executable()
	if err != nil || exe == "" {
		result.add(DoctorWarn, "TUI executable: unknown (%v)", err)
	} else {
		result.add(DoctorInfo, "TUI executable: %s", exe)
	}

	install := detectTUIInstallMethod(globalDir, exe, DoctorOptions{LookupEnv: opts.LookupEnv})
	for _, line := range install.Diagnostics {
		result.add(line.Severity, "%s", line.Text)
	}
	result.add(DoctorInfo, "TUI install method: %s", install.Summary())

	latestVersion := ""
	release, releaseErr := fetchLatestGitHubRelease(opts.HTTPClient)
	if releaseErr != nil {
		result.add(DoctorWarn, "Could not check latest TUI release on GitHub: %v", releaseErr)
	} else {
		latestVersion = release.TagName
		result.add(DoctorInfo, "Latest TUI release: %s", release.TagName)
		if current := opts.CurrentTUIVersion; current != "" {
			switch {
			case current == "dev" || strings.Contains(current, "-"):
				result.add(DoctorWarn, "Current TUI build is %q; running updater without version comparison", current)
			case releaseNewer(current, release.TagName):
				result.add(DoctorWarn, "TUI update available: %s -> %s", current, release.TagName)
			default:
				// Already at (or ahead of) the latest release. Source/user-local
				// installs short-circuit as a no-op. Homebrew installs still run
				// so `self-update` can convert them to GitHub-Release-managed even
				// when the version already matches; unknown installs still run so
				// the caller sees the unsupported guidance.
				result.add(DoctorOK, "TUI is already at the latest version (%s)", release.TagName)
				if install.Method == TUIInstallMethodSource {
					return result
				}
			}
		}
	}

	update := RunTUIUpdate(install, TUIUpdateOptions{
		LatestVersion:       latestVersion,
		GlobalDir:           globalDir,
		Runner:              opts.Runner,
		LookPath:            opts.LookPath,
		Stat:                opts.Stat,
		Executable:          opts.Executable,
		SourceInstallScript: opts.SourceInstallScript,
	})
	result.Lines = append(result.Lines, update.Lines...)
	result.Updated = update.Updated
	result.Err = update.Err
	if !update.Healthy {
		result.Healthy = false
		return result
	}
	if install.Method == TUIInstallMethodUnknown {
		result.Err = fmt.Errorf("manual self-update unsupported for %s installs", install.Summary())
		result.add(DoctorFail, "Manual self-update for %s installs is not supported yet.", install.Summary())
		return result
	}
	return result
}

type homebrewTUIUpdater struct{}

func (homebrewTUIUpdater) InstallMethod() TUIInstallMethod {
	return TUIInstallMethodHomebrew
}

// Upgrade adopts a Homebrew install onto the GitHub-Release-managed source
// updater instead of running `brew update`/`brew upgrade`. It derives the
// Homebrew prefix, then runs the versioned install.sh --update contract into
// <prefix>/bin. After adoption, install.json records source metadata for the
// active <prefix>/bin/lingtai-tui, so future detection reports source, not
// Homebrew. Rollback is `brew reinstall lingtai-tui`.
func (homebrewTUIUpdater) Upgrade(opts TUIUpdateOptions) TUIUpdateResult {
	result := TUIUpdateResult{Healthy: true}
	if opts.Runner == nil {
		opts.Runner = execCommandRunner{}
	}
	if opts.Stat == nil {
		opts.Stat = os.Stat
	}
	if opts.LatestVersion == "" {
		result.Err = errors.New("latest TUI release is unknown")
		result.add(DoctorFail, "Homebrew adoption needs a known release tag; try again when GitHub release lookup succeeds.")
		return result
	}

	prefix, detail, err := homebrewAdoptionPrefix(opts)
	if err != nil {
		result.Err = err
		result.add(DoctorFail, "Could not determine a Homebrew prefix to adopt; install/update manually from %s", tuiReleaseURL(opts.LatestVersion))
		return result
	}
	result.add(DoctorInfo, "Adopting Homebrew install onto GitHub-Release-managed updater (prefix %s, %s).", prefix, detail)
	result.add(DoctorInfo, "Roll back to Homebrew management with: brew reinstall lingtai-tui")

	runInstallShAdoption(&result, opts, prefix)
	return result
}

// homebrewAdoptionPrefix resolves the install prefix to convert a Homebrew
// install into a GitHub-Release-managed install. It prefers `brew --prefix`
// (the active Homebrew root) so <prefix>/bin/lingtai-tui replaces the command
// the user actually runs; it falls back to a prefix inferred from the running
// executable's common Homebrew path when brew is not on PATH.
func homebrewAdoptionPrefix(opts TUIUpdateOptions) (string, string, error) {
	if prefix := strings.TrimSpace(brewPrefixFromRunner(opts.Runner)); prefix != "" {
		return prefix, "brew --prefix", nil
	}
	executable := opts.Executable
	if executable == nil {
		executable = os.Executable
	}
	exe, err := executable()
	if err == nil && exe != "" {
		if resolved, rErr := filepath.EvalSymlinks(exe); rErr == nil && resolved != "" {
			exe = resolved
		}
		if prefix := homebrewPrefixFromExecutable(exe); prefix != "" {
			return prefix, "inferred from executable path", nil
		}
	}
	return "", "", errors.New("no Homebrew prefix available")
}

// brewPrefixFromRunner runs `brew --prefix` and returns the trimmed output.
// A non-zero exit or empty output yields "".
func brewPrefixFromRunner(runner CommandRunner) string {
	if runner == nil {
		runner = execCommandRunner{}
	}
	res := runner.Run("brew", "--prefix")
	if res.Err != nil {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

// homebrewPrefixFromExecutable infers the Homebrew prefix from a resolved
// executable path when it lives under a known Homebrew root. It returns "" when
// the path does not look Homebrew-managed.
func homebrewPrefixFromExecutable(exe string) string {
	clean := filepath.ToSlash(filepath.Clean(exe))
	// An executable under <prefix>/Cellar/lingtai-tui/<ver>/bin/lingtai-tui
	// maps back to <prefix>.
	if idx := strings.Index(clean, "/Cellar/"); idx > 0 {
		return filepath.Clean(clean[:idx])
	}
	for _, prefix := range []string{
		"/opt/homebrew",
		"/home/linuxbrew/.linuxbrew",
		"/usr/local",
	} {
		if pathWithin(exe, prefix) {
			return prefix
		}
	}
	return ""
}

type sourceTUIUpdater struct{}

func (sourceTUIUpdater) InstallMethod() TUIInstallMethod {
	return TUIInstallMethodSource
}

func (sourceTUIUpdater) Upgrade(opts TUIUpdateOptions) TUIUpdateResult {
	result := TUIUpdateResult{Healthy: true}
	if opts.Runner == nil {
		opts.Runner = execCommandRunner{}
	}
	if opts.Stat == nil {
		opts.Stat = os.Stat
	}
	if opts.LatestVersion == "" {
		result.Err = errors.New("latest TUI release is unknown")
		result.add(DoctorFail, "Source/user-local TUI update needs a known release tag; try again when GitHub release lookup succeeds.")
		return result
	}

	metadataPath := opts.Install.MetadataPath
	if metadataPath == "" && opts.GlobalDir != "" {
		metadataPath = filepath.Join(opts.GlobalDir, "install.json")
	}
	if metadataPath == "" {
		result.Err = errors.New("source install metadata path is unknown")
		result.add(DoctorFail, "Source/user-local TUI update needs install metadata, but no metadata path was available.")
		return result
	}
	meta, err := readTUIInstallMetadata(metadataPath)
	if err != nil {
		result.Err = err
		result.add(DoctorFail, "Could not read source install metadata at %s: %v", metadataPath, err)
		return result
	}
	if !isSourceInstallMetadata(meta) {
		result.Err = errors.New("install metadata is not source metadata")
		result.add(DoctorFail, "Install metadata at %s is not recognized as source install metadata.", metadataPath)
		return result
	}
	if meta.Prefix == "" {
		result.Err = errors.New("source install metadata missing prefix")
		result.add(DoctorFail, "Source install metadata at %s is missing prefix.", metadataPath)
		return result
	}

	runInstallShAdoption(&result, opts, meta.Prefix)
	return result
}

// runInstallShAdoption drives the versioned install.sh --update contract for a
// given prefix and verifies the outcome: the new binary reports the target tag,
// install.json now records source metadata for <prefix>/bin/lingtai-tui, and
// (when GlobalDir is set) the Python runtime still imports. Both the source and
// Homebrew-adoption backends share this so a Homebrew install converts to the
// same GitHub-Release-managed state a source install already has. On any
// failure it marks result unhealthy and returns without setting Updated.
func runInstallShAdoption(result *TUIUpdateResult, opts TUIUpdateOptions, prefix string) {
	binDir := filepath.Join(prefix, "bin")
	target := filepath.Join(binDir, "lingtai-tui")

	metadataPath := opts.Install.MetadataPath
	if metadataPath == "" && opts.GlobalDir != "" {
		metadataPath = filepath.Join(opts.GlobalDir, "install.json")
	}

	name, args := sourceInstallCommand(opts.SourceInstallScript, prefix, opts.LatestVersion)
	result.add(DoctorInfo, "Running: %s %s", name, strings.Join(args, " "))
	res := opts.Runner.Run(name, args...)
	appendCommandOutputToTUIUpdate(result, res)
	if res.Err != nil {
		result.Err = res.Err
		result.add(DoctorFail, "Install.sh update command failed: %v", res.Err)
		return
	}

	versionRes := opts.Runner.Run(target, "version")
	appendCommandOutputToTUIUpdate(result, versionRes)
	if versionRes.Err != nil {
		result.Err = versionRes.Err
		result.add(DoctorFail, "Updated lingtai-tui did not run from %s: %v", target, versionRes.Err)
		return
	}
	versionOut := strings.TrimSpace(versionRes.Stdout)
	if versionOut == "" {
		versionOut = strings.TrimSpace(versionRes.Stderr)
	}
	// Match the tag as a whole whitespace-delimited field (output is like
	// "lingtai-tui vX.Y.Z"), not a substring, so v1.2.30 does not satisfy an
	// expected v1.2.3.
	if !versionOutputHasTag(versionOut, opts.LatestVersion) {
		err := fmt.Errorf("updated binary reported %q, expected %s", versionOut, opts.LatestVersion)
		result.Err = err
		result.add(DoctorFail, "Updated lingtai-tui version verification failed: %v", err)
		return
	}
	result.add(DoctorInfo, "Updated TUI binary: %s", versionOut)

	if metadataPath == "" {
		result.Err = errors.New("install metadata path is unknown")
		result.add(DoctorFail, "Update ran but no install metadata path was available to verify.")
		return
	}
	postMeta, err := readTUIInstallMetadata(metadataPath)
	if err != nil {
		result.Err = err
		result.add(DoctorFail, "Could not read source install metadata after update at %s: %v", metadataPath, err)
		return
	}
	if !isSourceInstallMetadata(postMeta) || postMeta.Prefix != prefix || !sourceMetadataMatchesExecutable(postMeta, target) {
		err := errors.New("updated source install metadata does not match the target binary")
		result.Err = err
		result.add(DoctorFail, "Source install metadata verification failed after update.")
		return
	}
	if postMeta.StampedVersion != opts.LatestVersion {
		err := fmt.Errorf("metadata stamped_version is %s, expected %s", postMeta.StampedVersion, opts.LatestVersion)
		result.Err = err
		result.add(DoctorFail, "Source install metadata version verification failed: %v", err)
		return
	}
	result.add(DoctorOK, "Source install metadata verified at %s", metadataPath)

	if opts.GlobalDir != "" {
		python := VenvPython(RuntimeVenvDir(opts.GlobalDir))
		if _, err := opts.Stat(python); err != nil {
			result.add(DoctorWarn, "Python runtime venv not present at %s; startup or doctor will create it when needed.", python)
		} else if runtimeVersion, err := pythonLingtaiVersion(opts.Runner, python); err != nil {
			result.Err = err
			result.add(DoctorFail, "Python runtime import failed after update: %v", err)
			return
		} else {
			result.add(DoctorOK, "Python runtime verified after update: lingtai %s", runtimeVersion)
		}
	}

	result.Updated = true
	result.add(DoctorOK, "TUI update verified. Restart lingtai-tui to use %s.", opts.LatestVersion)
}

type unknownTUIUpdater struct{}

func (unknownTUIUpdater) InstallMethod() TUIInstallMethod {
	return TUIInstallMethodUnknown
}

func (unknownTUIUpdater) Upgrade(opts TUIUpdateOptions) TUIUpdateResult {
	result := TUIUpdateResult{Healthy: true}
	result.add(DoctorWarn, "TUI install method is unknown; update manually from %s", tuiReleaseURL(opts.LatestVersion))
	return result
}

func appendCommandOutputToTUIUpdate(r *TUIUpdateResult, res CommandResult) {
	for _, line := range interestingCommandLines(res.Stdout, res.Stderr) {
		r.add(DoctorInfo, "  %s", line)
	}
}

// versionOutputHasTag reports whether want appears as a whole whitespace-
// delimited field in the binary's version output. This is stricter than a
// substring check: it rejects v1.2.30 when want is v1.2.3.
func versionOutputHasTag(output, want string) bool {
	if want == "" {
		return false
	}
	for _, field := range strings.Fields(output) {
		if field == want {
			return true
		}
	}
	return false
}

func tuiReleaseURL(version string) string {
	if version == "" {
		return "https://github.com/Lingtai-AI/lingtai/releases/latest"
	}
	return "https://github.com/Lingtai-AI/lingtai/releases/tag/" + version
}

func sourceInstallCommand(script, prefix, version string) (string, []string) {
	if script == "" {
		script = "https://raw.githubusercontent.com/Lingtai-AI/lingtai/" + version + "/install.sh"
	}
	args := []string{"--update", "--prefix", prefix, "--version", version, "--non-interactive"}
	if strings.HasPrefix(script, "http://") || strings.HasPrefix(script, "https://") {
		shell := `set -euo pipefail; curl -fsSL "$1" | bash -s -- "$2" "$3" "$4" "$5" "$6" "$7"`
		shellArgs := []string{"-c", shell, "lingtai-source-update", script}
		shellArgs = append(shellArgs, args...)
		return "bash", shellArgs
	}
	return "bash", append([]string{script}, args...)
}
