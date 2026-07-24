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

// execHomebrewUninstall is the production UninstallHomebrew default: the
// exact one-shot formula removal, invoked as an explicit executable+argv
// (never a shell string). This is the ONLY brew command this package ever
// runs, and only when homebrewTUIUpdater.Upgrade has already confirmed a
// verified native install and the caller's ConfirmHomebrewCleanup returned
// true.
func execHomebrewUninstall() error {
	cmd := exec.Command("brew", "uninstall", homebrewTUIFormula)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return err
	}
	return nil
}

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

	// ConfirmHomebrewCleanup, when non-nil, is called by homebrewTUIUpdater.Upgrade
	// exactly once a verified native install is confirmed (fresh migration or a
	// pre-existing DuplicateNativeInstall), and only then — never before native
	// install/metadata/version/runtime verification. It must return true only on
	// an explicit human "y" answer to a concrete removal prompt; false (including
	// "N"/default) leaves everything untouched and reports truthful
	// cleanup-pending state. Nil in every non-interactive caller (RunDoctorUpdate,
	// RunManualTUIUpdate leave it nil) — those paths must never prompt or
	// uninstall. Only the interactive startup path (tui/upgrade.go) supplies it.
	ConfirmHomebrewCleanup func() bool

	// UninstallHomebrew performs the exact one-shot Homebrew formula uninstall
	// when ConfirmHomebrewCleanup returns true. Defaults to execHomebrewUninstall
	// (`brew uninstall lingtai-ai/lingtai/lingtai-tui`, explicit executable+argv,
	// no shell string). Tests inject a fake so no real brew ever runs.
	UninstallHomebrew func() error
}

// TUIUpdateResult is the backend result consumed by doctor and startup.
type TUIUpdateResult struct {
	Lines   []DoctorLine
	Healthy bool
	Updated bool
	Err     error

	// NeedsManualCleanup is set whenever the old Homebrew formula/keg is still
	// present after this call returns. Two distinct cases both set it:
	//   1. PATH still shadowed: Homebrew resolves ahead of the newly installed
	//      native binary (fresh migration) or still did on a prior migration
	//      (DuplicateNativeInstall). Updated stays false here — the resolved
	//      binary did not change, so this is not a "Migrated!"/exit-early case.
	//   2. PATH takeover confirmed but Homebrew not removed: either no cleanup
	//      consent was given (ConfirmHomebrewCleanup nil/false — the default
	//      for every non-interactive caller and for a declined interactive
	//      prompt) or UninstallHomebrew/the post-uninstall PATH check failed.
	//      Updated is true here: the TUI itself did migrate; only Homebrew
	//      removal is outstanding.
	// Callers must keep re-surfacing this state — including case 2 — rather
	// than declaring the migration/cleanup fully complete or re-running the
	// installer.
	NeedsManualCleanup bool
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
				if install.DuplicateNativeInstall {
					// The resolved binary's version looks current, but that
					// resolved binary is still the shadowed Homebrew one — a
					// verified native install is waiting on manual Homebrew
					// removal. Fall through to the updater so it reports that
					// state instead of a misleading "already latest".
					break
				}
				// Already at (or ahead of) the latest release: skip the updater
				// entirely so we don't run Homebrew for a no-op upgrade.
				result.add(DoctorOK, "TUI is already at the latest version (%s)", release.TagName)
				return result
			}
		}
	}

	update := RunTUIUpdate(install, TUIUpdateOptions{
		LatestVersion:       latestVersion,
		GlobalDir:           globalDir,
		Runner:              opts.Runner,
		LookPath:            opts.LookPath,
		Stat:                opts.Stat,
		SourceInstallScript: opts.SourceInstallScript,
	})
	result.Lines = append(result.Lines, update.Lines...)
	result.Updated = update.Updated
	result.Err = update.Err
	result.NeedsManualCleanup = update.NeedsManualCleanup
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

// Upgrade migrates a Homebrew-managed install to LingTai's native installer
// instead of running `brew upgrade`. It reuses the exact install.sh
// contract and verification steps as the source updater below (fresh
// install, then verify install.json + binary version + Python runtime), but
// installs to a fresh native prefix rather than `--update`-ing an existing
// source install (none exists yet). The Homebrew formula/keg is never
// touched — no `brew` command runs here — so any failure leaves the
// Homebrew-installed binary exactly as usable as before this call.
func (homebrewTUIUpdater) Upgrade(opts TUIUpdateOptions) TUIUpdateResult {
	result := TUIUpdateResult{Healthy: true}
	if opts.Runner == nil {
		opts.Runner = execCommandRunner{}
	}
	if opts.Stat == nil {
		opts.Stat = os.Stat
	}
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	if opts.Install.DuplicateNativeInstall {
		result.add(DoctorOK, "A native lingtai-tui install already exists and was verified: %s", opts.Install.DuplicateNativeDetail)
		attemptHomebrewCleanup(&result, opts, opts.Install.DuplicateNativeTarget)
		return result
	}
	if opts.LatestVersion == "" {
		result.Err = errors.New("latest TUI release is unknown")
		result.add(DoctorFail, "Homebrew-to-native migration needs a known release tag; try again when GitHub release lookup succeeds. The Homebrew install is untouched.")
		return result
	}
	metadataPath := opts.Install.MetadataPath
	if metadataPath == "" && opts.GlobalDir != "" {
		metadataPath = filepath.Join(opts.GlobalDir, "install.json")
	}
	if metadataPath == "" {
		result.Err = errors.New("global directory is unknown")
		result.add(DoctorFail, "Homebrew-to-native migration needs the LingTai global directory, but none was available. The Homebrew install is untouched.")
		return result
	}

	name, args := nativeMigrationInstallCommand(opts.SourceInstallScript, opts.LatestVersion)
	result.add(DoctorInfo, "Migrating from Homebrew to the native installer. Running: %s %s", name, strings.Join(args, " "))
	res := opts.Runner.Run(name, args...)
	appendCommandOutputToTUIUpdate(&result, res)
	if res.Err != nil {
		result.Err = res.Err
		result.add(DoctorFail, "Native installer failed: %v", res.Err)
		result.add(DoctorInfo, "The Homebrew-installed lingtai-tui is untouched and still usable. Fix the reported native-installer error, then retry with `lingtai-tui self-update`; this migration path never runs `brew update` or `brew upgrade`.")
		return result
	}

	meta, err := readTUIInstallMetadata(metadataPath)
	if err != nil {
		result.Err = err
		result.add(DoctorFail, "Could not read native install metadata at %s after migration: %v", metadataPath, err)
		result.add(DoctorInfo, "The Homebrew-installed lingtai-tui is untouched and still usable.")
		return result
	}
	if !isSourceInstallMetadata(meta) || meta.Prefix == "" {
		result.Err = errors.New("native install metadata missing or invalid after migration")
		result.add(DoctorFail, "Native install metadata at %s is missing or not recognized after migration.", metadataPath)
		result.add(DoctorInfo, "The Homebrew-installed lingtai-tui is untouched and still usable.")
		return result
	}
	if meta.StampedVersion != opts.LatestVersion {
		err := fmt.Errorf("metadata stamped_version is %s, expected %s", meta.StampedVersion, opts.LatestVersion)
		result.Err = err
		result.add(DoctorFail, "Native install metadata version verification failed: %v", err)
		result.add(DoctorInfo, "The Homebrew-installed lingtai-tui is untouched and still usable.")
		return result
	}
	binDir := meta.BinDir
	if binDir == "" {
		binDir = filepath.Join(meta.Prefix, "bin")
	}
	target := filepath.Join(binDir, "lingtai-tui")

	versionRes := opts.Runner.Run(target, "version")
	appendCommandOutputToTUIUpdate(&result, versionRes)
	if versionRes.Err != nil {
		result.Err = versionRes.Err
		result.add(DoctorFail, "Newly installed native lingtai-tui did not run from %s: %v", target, versionRes.Err)
		result.add(DoctorInfo, "The Homebrew-installed lingtai-tui is untouched and still usable.")
		return result
	}
	versionOut := strings.TrimSpace(versionRes.Stdout)
	if versionOut == "" {
		versionOut = strings.TrimSpace(versionRes.Stderr)
	}
	if !strings.Contains(versionOut, opts.LatestVersion) {
		err := fmt.Errorf("installed binary reported %q, expected %s", versionOut, opts.LatestVersion)
		result.Err = err
		result.add(DoctorFail, "Native lingtai-tui version verification failed: %v", err)
		result.add(DoctorInfo, "The Homebrew-installed lingtai-tui is untouched and still usable.")
		return result
	}
	result.add(DoctorInfo, "Installed native TUI binary: %s at %s", versionOut, target)
	result.add(DoctorOK, "Native install metadata verified at %s", metadataPath)

	if opts.GlobalDir != "" {
		python := VenvPython(RuntimeVenvDir(opts.GlobalDir))
		if _, err := opts.Stat(python); err != nil {
			result.add(DoctorWarn, "Python runtime venv not present at %s; startup or doctor will create it when needed.", python)
		} else if runtimeVersion, err := pythonLingtaiVersion(opts.Runner, python); err != nil {
			result.Err = err
			result.add(DoctorFail, "Python runtime import failed after migration: %v", err)
			result.add(DoctorInfo, "The Homebrew-installed lingtai-tui is untouched and still usable.")
			return result
		} else {
			result.add(DoctorOK, "Python runtime verified after migration: lingtai %s", runtimeVersion)
		}
	}

	result.add(DoctorOK, "Native install verified at %s.", binDir)

	resolved, lookErr := opts.LookPath("lingtai-tui")
	if lookErr != nil || !samePath(resolved, target) {
		if lookErr != nil {
			result.add(DoctorWarn, "Installed native lingtai-tui at %s, but could not resolve `lingtai-tui` on PATH to confirm it will run: %v", target, lookErr)
		} else {
			result.add(DoctorWarn, "Installed native lingtai-tui at %s, but your shell still resolves `lingtai-tui` to %s. The native install is ready, but the migration is NOT complete: restarting will keep running the old Homebrew binary.", target, resolved)
		}
		result.add(DoctorInfo, "Remove the old Homebrew install yourself with `brew uninstall %s` (never done automatically), or put %s ahead of it on PATH, then restart lingtai-tui to use the native binary.", homebrewTUIFormula, binDir)
		result.NeedsManualCleanup = true
		return result
	}

	result.Updated = true
	result.add(DoctorOK, "Migrated from Homebrew to the native installer at %s — confirmed `lingtai-tui` on PATH now resolves there.", binDir)
	attemptHomebrewCleanup(&result, opts, target)
	result.add(DoctorWarn, "Restart lingtai-tui so your shell picks up the native binary at %s. Future updates will use the native installer, not brew.", target)
	return result
}

// attemptHomebrewCleanup is the single owning primitive for the interactive
// Homebrew-removal decision, shared by both the pre-existing-duplicate
// short-circuit above and a just-completed fresh migration below — a fresh
// migration that creates the duplicate and a later update that redetects an
// existing duplicate both reach this exact function, never a drifted copy.
//
// It must only be called after the caller has already verified a native
// install exists at target (either freshly installed-and-verified this call,
// or previously verified and recorded as DuplicateNativeInstall). It never
// runs brew itself: opts.ConfirmHomebrewCleanup is nil for every
// non-interactive caller (RunDoctorUpdate, RunManualTUIUpdate), so this is a
// no-op there beyond reporting the truthful pending-cleanup state. Only the
// interactive startup path (tui/upgrade.go) supplies a real
// ConfirmHomebrewCleanup, and only an explicit "y" answer from it reaches the
// UninstallHomebrew call — a declined/absent confirm never runs anything.
//
// On success it re-resolves ordinary PATH via opts.LookPath and requires it
// to equal target before marking cleanup complete (result.NeedsManualCleanup
// stays false): the currently-running Homebrew process is never treated as
// proof, only a fresh PATH lookup is. A failed uninstall or a still-shadowed
// PATH after uninstall both remain truthful NeedsManualCleanup=true states,
// never a false success.
func attemptHomebrewCleanup(result *TUIUpdateResult, opts TUIUpdateOptions, target string) {
	if opts.ConfirmHomebrewCleanup == nil || !opts.ConfirmHomebrewCleanup() {
		result.NeedsManualCleanup = true
		result.add(DoctorInfo, "Remove the old Homebrew install yourself with `brew uninstall %s` (never done automatically), or put the native bin dir ahead of it on PATH, then restart lingtai-tui.", homebrewTUIFormula)
		return
	}

	uninstall := opts.UninstallHomebrew
	if uninstall == nil {
		uninstall = execHomebrewUninstall
	}
	if err := uninstall(); err != nil {
		result.NeedsManualCleanup = true
		result.add(DoctorWarn, "Removing the old Homebrew install failed: %v", err)
		result.add(DoctorInfo, "The native install at %s is unaffected. Retry `brew uninstall %s` yourself, or run the update again.", target, homebrewTUIFormula)
		return
	}
	result.add(DoctorOK, "Removed the old Homebrew install (`brew uninstall %s`).", homebrewTUIFormula)

	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	resolved, lookErr := lookPath("lingtai-tui")
	if lookErr != nil || !samePath(resolved, target) {
		result.NeedsManualCleanup = true
		if lookErr != nil {
			result.add(DoctorWarn, "Removed the old Homebrew install, but could not resolve `lingtai-tui` on PATH afterward: %v", lookErr)
		} else {
			result.add(DoctorWarn, "Removed the old Homebrew install, but `lingtai-tui` on PATH still resolves to %s, not the native binary at %s.", resolved, target)
		}
		result.add(DoctorInfo, "Open a new shell (or re-hash PATH) and restart lingtai-tui once `lingtai-tui` resolves to %s.", target)
		return
	}
	result.add(DoctorOK, "`lingtai-tui` on PATH now resolves to the native binary at %s. Migration complete.", target)
}

// nativeMigrationInstallCommand builds the install.sh invocation that
// migrates a Homebrew install to a fresh native install. Unlike
// sourceInstallCommand's --update (which requires an existing source bin
// dir), this runs install.sh's normal fresh-install path: no --update, no
// --prefix, so install.sh resolves its own default writable location
// (preferring /usr/local/bin, else ~/.local/bin — never Homebrew's prefix)
// exactly as it would for any new user.
func nativeMigrationInstallCommand(script, version string) (string, []string) {
	if script == "" {
		script = "https://lingtai.ai/install.sh"
	}
	args := []string{"--version", version, "--non-interactive"}
	if strings.HasPrefix(script, "http://") || strings.HasPrefix(script, "https://") {
		shell := `set -euo pipefail; script="$1"; shift; curl -fsSL "$script" | bash -s -- "$@"`
		shellArgs := []string{"-c", shell, "lingtai-homebrew-migration", script}
		shellArgs = append(shellArgs, args...)
		return "bash", shellArgs
	}
	return "bash", append([]string{script}, args...)
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
		script = "https://lingtai.ai/install.sh"
	}
	args := []string{"--update", "--prefix", prefix, "--version", version, "--non-interactive"}
	if strings.HasPrefix(script, "http://") || strings.HasPrefix(script, "https://") {
		shell := `set -euo pipefail; script="$1"; shift; curl -fsSL "$script" | bash -s -- "$@"`
		shellArgs := []string{"-c", shell, "lingtai-source-update", script}
		shellArgs = append(shellArgs, args...)
		return "bash", shellArgs
	}
	return "bash", append([]string{script}, args...)
}
