package config

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const homebrewTUIFormula = "lingtai-ai/lingtai/lingtai-tui"

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

	// IncludeHomebrewUpdate preserves doctor's existing `brew update` before
	// `brew upgrade`. Startup leaves this false to keep its current command.
	IncludeHomebrewUpdate bool
	// ResolveHomebrewPath preserves doctor's full-path command reporting.
	// Startup leaves this false so execution still goes through "brew".
	ResolveHomebrewPath bool
	// SourceInstallScript overrides the installer source for tests. Production
	// uses the versioned raw GitHub install.sh URL.
	SourceInstallScript string
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
// TUI updater backend. Unlike doctor, source/user-local and unknown installs
// are command failures because the requested mutation is not implemented yet.
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
				result.add(DoctorOK, "Latest release is not newer than current TUI version; running updater anyway")
			}
		}
	}

	update := RunTUIUpdate(install, TUIUpdateOptions{
		LatestVersion:         latestVersion,
		GlobalDir:             globalDir,
		Runner:                opts.Runner,
		LookPath:              opts.LookPath,
		Stat:                  opts.Stat,
		IncludeHomebrewUpdate: true,
		ResolveHomebrewPath:   true,
		SourceInstallScript:   opts.SourceInstallScript,
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

func (homebrewTUIUpdater) Upgrade(opts TUIUpdateOptions) TUIUpdateResult {
	result := TUIUpdateResult{Healthy: true}
	if opts.Runner == nil {
		opts.Runner = execCommandRunner{}
	}
	brew := "brew"
	if opts.ResolveHomebrewPath {
		lookPath := opts.LookPath
		if lookPath == nil {
			lookPath = exec.LookPath
		}
		resolved, err := lookPath("brew")
		if err != nil || resolved == "" {
			result.Err = errors.New("homebrew not found")
			result.add(DoctorFail, "Homebrew not found; install/update manually from %s", tuiReleaseURL(opts.LatestVersion))
			return result
		}
		brew = resolved
	}

	commands := [][]string{}
	if opts.IncludeHomebrewUpdate {
		commands = append(commands, []string{"update"})
	}
	commands = append(commands, []string{"upgrade", homebrewTUIFormula})
	for _, args := range commands {
		result.add(DoctorInfo, "Running: %s %s", brew, strings.Join(args, " "))
		res := opts.Runner.Run(brew, args...)
		appendCommandOutputToTUIUpdate(&result, res)
		if res.Err != nil {
			result.Err = res.Err
			result.add(DoctorFail, "Command failed: %v", res.Err)
			return result
		}
	}
	result.Updated = true
	result.add(DoctorWarn, "Brew upgrade finished. Restart lingtai-tui and run `lingtai-tui version` to verify the active binary changed.")
	return result
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
	binDir := meta.BinDir
	if binDir == "" {
		binDir = filepath.Join(meta.Prefix, "bin")
	}
	target := filepath.Join(binDir, "lingtai-tui")

	name, args := sourceInstallCommand(opts.SourceInstallScript, meta.Prefix, opts.LatestVersion)
	result.add(DoctorInfo, "Running: %s %s", name, strings.Join(args, " "))
	res := opts.Runner.Run(name, args...)
	appendCommandOutputToTUIUpdate(&result, res)
	if res.Err != nil {
		result.Err = res.Err
		result.add(DoctorFail, "Source update command failed: %v", res.Err)
		return result
	}

	versionRes := opts.Runner.Run(target, "version")
	appendCommandOutputToTUIUpdate(&result, versionRes)
	if versionRes.Err != nil {
		result.Err = versionRes.Err
		result.add(DoctorFail, "Updated lingtai-tui did not run from %s: %v", target, versionRes.Err)
		return result
	}
	versionOut := strings.TrimSpace(versionRes.Stdout)
	if versionOut == "" {
		versionOut = strings.TrimSpace(versionRes.Stderr)
	}
	if !strings.Contains(versionOut, opts.LatestVersion) {
		err := fmt.Errorf("updated binary reported %q, expected %s", versionOut, opts.LatestVersion)
		result.Err = err
		result.add(DoctorFail, "Updated lingtai-tui version verification failed: %v", err)
		return result
	}
	result.add(DoctorInfo, "Updated TUI binary: %s", versionOut)

	postMeta, err := readTUIInstallMetadata(metadataPath)
	if err != nil {
		result.Err = err
		result.add(DoctorFail, "Could not read source install metadata after update at %s: %v", metadataPath, err)
		return result
	}
	if !isSourceInstallMetadata(postMeta) || postMeta.Prefix != meta.Prefix || !sourceMetadataMatchesExecutable(postMeta, target) {
		err := errors.New("updated source install metadata does not match the target binary")
		result.Err = err
		result.add(DoctorFail, "Source install metadata verification failed after update.")
		return result
	}
	if postMeta.StampedVersion != opts.LatestVersion {
		err := fmt.Errorf("metadata stamped_version is %s, expected %s", postMeta.StampedVersion, opts.LatestVersion)
		result.Err = err
		result.add(DoctorFail, "Source install metadata version verification failed: %v", err)
		return result
	}
	result.add(DoctorOK, "Source install metadata verified at %s", metadataPath)

	if opts.GlobalDir != "" {
		python := VenvPython(RuntimeVenvDir(opts.GlobalDir))
		if _, err := opts.Stat(python); err != nil {
			result.add(DoctorWarn, "Python runtime venv not present at %s; startup or doctor will create it when needed.", python)
		} else if runtimeVersion, err := pythonLingtaiVersion(opts.Runner, python); err != nil {
			result.Err = err
			result.add(DoctorFail, "Python runtime import failed after source update: %v", err)
			return result
		} else {
			result.add(DoctorOK, "Python runtime verified after source update: lingtai %s", runtimeVersion)
		}
	}

	result.Updated = true
	result.add(DoctorOK, "Source/user-local TUI update verified. Restart lingtai-tui to use %s.", opts.LatestVersion)
	return result
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
