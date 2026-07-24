package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

type runningTUIProcess struct {
	PID     int
	CWD     string
	Command string
}

func handleTUIUpgrade(install config.TUIInstallInfo, version, latestVersion, globalDir string) bool {
	return handleTUIUpgradeWithOptions(install, version, latestVersion, startupTUIUpgradeOptions{
		GlobalDir: globalDir,
	})
}

type startupTUIUpgradeOptions struct {
	Input     io.Reader
	Output    io.Writer
	ErrOutput io.Writer

	Runner   config.CommandRunner
	Stat     func(string) (os.FileInfo, error)
	LookPath func(string) (string, error)

	// UninstallHomebrew overrides the injected Homebrew formula uninstall used
	// when the human confirms the cleanup prompt below. Nil in production
	// (config.homebrewTUIUpdater.Upgrade defaults to the real `brew uninstall`
	// call); tests inject a fake so no real brew ever runs.
	UninstallHomebrew func() error

	GlobalDir           string
	SourceInstallScript string

	CheckTUIUpgrade                 func(string) string
	FindOtherTUIProcesses           func() []runningTUIProcess
	PrepareOtherTUIProcessesUpgrade func([]runningTUIProcess) error
}

func (o *startupTUIUpgradeOptions) setDefaults() {
	if o.Input == nil {
		o.Input = os.Stdin
	}
	if o.Output == nil {
		o.Output = os.Stdout
	}
	if o.ErrOutput == nil {
		o.ErrOutput = os.Stderr
	}
	if o.Runner == nil {
		o.Runner = streamingCommandRunner{stdout: os.Stdout, stderr: os.Stderr}
	}
	if o.CheckTUIUpgrade == nil {
		o.CheckTUIUpgrade = config.CheckTUIUpgrade
	}
	if o.FindOtherTUIProcesses == nil {
		o.FindOtherTUIProcesses = findOtherTUIProcesses
	}
	if o.PrepareOtherTUIProcessesUpgrade == nil {
		o.PrepareOtherTUIProcessesUpgrade = prepareOtherTUIProcessesForUpgrade
	}
}

func handleTUIUpgradeWithOptions(install config.TUIInstallInfo, version, latestVersion string, opts startupTUIUpgradeOptions) bool {
	opts.setDefaults()

	switch install.Method {
	case config.TUIInstallMethodHomebrew:
		return handleHomebrewTUIUpgrade(install, version, latestVersion, opts)
	case config.TUIInstallMethodSource:
		return handleSourceTUIUpgrade(install, version, latestVersion, opts)
	default:
		fmt.Fprintf(opts.Output, "lingtai-tui %s\n", version)
		return false
	}
}

// handleHomebrewTUIUpgrade migrates a Homebrew-managed install to LingTai's
// native installer instead of running `brew upgrade`. The consent prompt is
// explicit about the migration so a Homebrew user is never surprised by a
// binary appearing at a new path. The migration step itself never touches the
// Homebrew formula/keg. Once a native install is verified — either right
// after this call's own migration, or immediately for a pre-existing
// DuplicateNativeInstall — a second, separate consent question
// (homebrewCleanupPrompt) decides whether to actually remove Homebrew; see
// config.attemptHomebrewCleanup.
func handleHomebrewTUIUpgrade(install config.TUIInstallInfo, version, latestVersion string, opts startupTUIUpgradeOptions) bool {
	fmt.Fprintf(opts.Output, "lingtai-tui %s (latest: %s)\n", version, latestVersion)

	// This function can ask up to two sequential questions in one flow (the
	// migrate-or-others prompt below, then the cleanup prompt inside
	// RunTUIUpdate). Both must read from the SAME *bufio.Reader: constructing
	// a fresh bufio.Reader per prompt (as the old single-prompt readLineLower
	// helper does) silently discards any input already buffered ahead past
	// the first '\n', dropping the second answer even when both lines were
	// present. See readLineLowerFromReader's doc comment.
	stdin := bufio.NewReader(opts.Input)

	if install.DuplicateNativeInstall {
		fmt.Fprintln(opts.Output, "  Homebrew install detected, and a native lingtai-tui install already exists.")
		fmt.Fprintf(opts.Output, "  %s\n", install.DuplicateNativeDetail)
		fmt.Fprintln(opts.Output, "  The native install is ready and verified.")
		update := config.RunTUIUpdate(install, config.TUIUpdateOptions{
			LatestVersion:          latestVersion,
			GlobalDir:              opts.GlobalDir,
			Runner:                 opts.Runner,
			LookPath:               opts.LookPath,
			ConfirmHomebrewCleanup: homebrewCleanupPrompt(opts, stdin),
			UninstallHomebrew:      opts.UninstallHomebrew,
		})
		printTUIUpdateLines(opts.Output, update.Lines)
		return false
	}

	fmt.Fprintln(opts.Output, "  Homebrew install detected. LingTai now installs and updates itself with a native installer instead of Homebrew.")

	others := opts.FindOtherTUIProcesses()
	if len(others) > 0 {
		fmt.Fprintln(opts.Output, "  Other lingtai-tui processes are running:")
		for _, p := range others {
			if p.CWD != "" {
				fmt.Fprintf(opts.Output, "    PID %d  cwd=%s\n", p.PID, p.CWD)
			} else {
				fmt.Fprintf(opts.Output, "    PID %d  %s\n", p.PID, p.Command)
			}
		}
		fmt.Fprintln(opts.Output, "  Migrating while they keep running can leave old/new binaries mixed.")
		fmt.Fprint(opts.Output, "  Put agents in their projects to sleep, stop those TUI processes, and migrate from Homebrew to the native installer now? [y/N] ")
		if !answerYes(readLineLowerFromReader(stdin)) {
			fmt.Fprintln(opts.Output, "  Migration skipped. Quit the other TUI windows first, then run:")
			fmt.Fprintln(opts.Output, "    lingtai-tui self-update")
			return false
		}
		if err := opts.PrepareOtherTUIProcessesUpgrade(others); err != nil {
			fmt.Fprintf(opts.ErrOutput, "  Could not prepare other TUI processes for migration: %v\n", err)
			fmt.Fprintln(opts.Output, "  Migration skipped. Please close them manually and try again.")
			return false
		}
	} else {
		fmt.Fprint(opts.Output, "  Migrate from Homebrew to the native installer now? [y/N] ")
		if !answerYes(readLineLowerFromReader(stdin)) {
			fmt.Fprintln(opts.Output, "  Migration skipped. Run manually later:")
			fmt.Fprintln(opts.Output, "    lingtai-tui self-update")
			return false
		}
	}

	fmt.Fprintln(opts.Output, "  Migrating from Homebrew to the native installer...")
	update := config.RunTUIUpdate(install, config.TUIUpdateOptions{
		LatestVersion:          latestVersion,
		GlobalDir:              opts.GlobalDir,
		Runner:                 opts.Runner,
		LookPath:               opts.LookPath,
		ConfirmHomebrewCleanup: homebrewCleanupPrompt(opts, stdin),
		UninstallHomebrew:      opts.UninstallHomebrew,
	})
	printTUIUpdateLines(opts.Output, update.Lines)
	if !update.Healthy {
		err := update.Err
		if err == nil {
			err = fmt.Errorf("homebrew-to-native migration failed")
		}
		fmt.Fprintf(opts.ErrOutput, "  Migration failed: %v\n", err)
		return false
	}
	if !update.Updated {
		fmt.Fprintln(opts.Output, "  Native install ready, but the migration is not complete yet — see the manual-cleanup guidance above.")
		return false
	}

	fmt.Fprintln(opts.Output, "  Migrated! Please restart lingtai-tui to use the native binary:")
	fmt.Fprintln(opts.Output, "    lingtai-tui")
	return true
}

// homebrewCleanupPrompt returns the ConfirmHomebrewCleanup callback for the
// interactive startup path: the exact, concrete removal question, asked only
// once config.homebrewTUIUpdater.Upgrade has already verified a native
// install exists (fresh migration or a pre-existing duplicate). Defaults to
// No on anything but an explicit "y"/"yes" answer. Takes the same
// *bufio.Reader the caller used for its own prompt(s) so a second sequential
// question in one flow reads the next line instead of losing it to a
// freshly-buffered reader.
func homebrewCleanupPrompt(opts startupTUIUpgradeOptions, stdin *bufio.Reader) func() bool {
	return func() bool {
		fmt.Fprint(opts.Output, "  Remove the old Homebrew installation now? [y/N] ")
		return answerYes(readLineLowerFromReader(stdin))
	}
}

func handleSourceTUIUpgrade(install config.TUIInstallInfo, version, latestVersion string, opts startupTUIUpgradeOptions) bool {
	fmt.Fprintf(opts.Output, "lingtai-tui %s (latest: %s)\n", version, latestVersion)
	fmt.Fprintln(opts.Output, "  Source/user-local install detected.")
	if install.Detail != "" {
		fmt.Fprintf(opts.Output, "  Install detail: %s\n", install.Detail)
	}
	fmt.Fprintln(opts.Output, "  Updating will run the source installer for the latest release tag.")
	fmt.Fprint(opts.Output, "  Update this source install now? [y/N] ")
	if !answerYes(readLineLower(opts.Input)) {
		fmt.Fprintln(opts.Output, "  Update skipped. Run manually later:")
		fmt.Fprintln(opts.Output, "    lingtai-tui self-update")
		return false
	}

	fmt.Fprintln(opts.Output, "  Updating source install...")
	update := config.RunTUIUpdate(install, config.TUIUpdateOptions{
		LatestVersion:       latestVersion,
		GlobalDir:           opts.GlobalDir,
		Runner:              opts.Runner,
		Stat:                opts.Stat,
		SourceInstallScript: opts.SourceInstallScript,
	})
	printTUIUpdateLines(opts.Output, update.Lines)
	if !update.Healthy {
		err := update.Err
		if err == nil {
			err = fmt.Errorf("source update failed")
		}
		fmt.Fprintf(opts.ErrOutput, "  Update failed: %v\n", err)
		return false
	}
	return true
}

func printTUIUpdateLines(w io.Writer, lines []config.DoctorLine) {
	for _, line := range lines {
		fmt.Fprintf(w, "  %s\n", line.Text)
	}
}

func readLineLower(input io.Reader) string {
	return readLineLowerFromReader(bufio.NewReader(input))
}

// readLineLowerFromReader reads one line from an existing *bufio.Reader
// rather than wrapping the underlying io.Reader fresh each call. bufio.Reader
// reads ahead into its own internal buffer, so constructing a new one per
// prompt (as readLineLower above does) silently discards any input already
// buffered past the first '\n' — invisible with a single prompt per call, but
// it drops the answer to a SECOND sequential prompt (e.g. the Homebrew
// cleanup question after the migration question) even though both lines were
// present in the input. Callers that ask more than one question in a single
// flow must share one *bufio.Reader across all of them.
func readLineLowerFromReader(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line))
}

func answerYes(answer string) bool {
	return answer == "y" || answer == "yes"
}

type streamingCommandRunner struct {
	stdout *os.File
	stderr *os.File
}

func (r streamingCommandRunner) Run(name string, args ...string) config.CommandResult {
	cmd := exec.Command(name, args...)
	cmd.Stdout = r.stdout
	cmd.Stderr = r.stderr
	err := cmd.Run()
	return config.CommandResult{Err: err}
}

func prepareOtherTUIProcessesForUpgrade(procs []runningTUIProcess) error {
	projects := map[string]bool{}
	for _, p := range procs {
		if projectDir := findProjectDirFromCWD(p.CWD); projectDir != "" {
			projects[projectDir] = true
		}
	}

	for projectDir := range projects {
		fmt.Printf("  Putting agents in %s to sleep...\n", projectDir)
		if err := sleepAgentsInProject(projectDir); err != nil {
			return err
		}
	}

	for _, p := range procs {
		fmt.Printf("  Stopping lingtai-tui PID %d...\n", p.PID)
		if err := stopTUIProcess(p.PID); err != nil {
			return err
		}
	}
	return nil
}

func findProjectDirFromCWD(cwd string) string {
	if cwd == "" {
		return ""
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		dir = cwd
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, ".lingtai")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func sleepAgentsInProject(projectDir string) error {
	lingtaiDir := filepath.Join(projectDir, ".lingtai")
	agents, err := fs.DiscoverAgents(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var alive []string
	for _, agent := range agents {
		if agent.IsHuman {
			continue
		}
		sleepFile := filepath.Join(agent.WorkingDir, ".sleep")
		if err := os.WriteFile(sleepFile, []byte(""), 0o644); err != nil {
			return err
		}
		if !agentIsAsleep(agent.WorkingDir) {
			alive = append(alive, agent.WorkingDir)
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for len(alive) > 0 && time.Now().Before(deadline) {
		remaining := alive[:0]
		for _, dir := range alive {
			if !agentIsAsleep(dir) {
				remaining = append(remaining, dir)
			}
		}
		alive = remaining
		if len(alive) > 0 {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if len(alive) > 0 {
		fmt.Printf("  Warning: %d agent(s) did not report asleep after .sleep signal.\n", len(alive))
	}
	return nil
}

func agentIsAsleep(agentDir string) bool {
	agent, err := fs.ReadAgent(agentDir)
	if err != nil {
		return true
	}
	return strings.EqualFold(agent.State, "asleep")
}
