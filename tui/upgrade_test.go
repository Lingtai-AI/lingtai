package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
)

func TestStartupSourceUpdateDeclineDoesNotMutate(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	metadataPath := writeStartupSourceInstallMetadata(t, globalDir, prefix, binDir, "v0.8.0")
	runner := &startupFakeRunner{}

	var out, errOut bytes.Buffer
	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method:       config.TUIInstallMethodSource,
		Detail:       "metadata at " + metadataPath,
		MetadataPath: metadataPath,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("\n"),
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
		GlobalDir: globalDir,
	})

	if updated {
		t.Fatal("declined source startup update should not stop startup")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("declined source startup update should not run commands, got %#v", runner.calls)
	}
	if !strings.Contains(out.String(), "Update this source install now? [y/N]") {
		t.Fatalf("missing source confirmation prompt:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "lingtai-tui self-update") {
		t.Fatalf("missing manual self-update guidance:\n%s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("decline should not write stderr, got %q", errOut.String())
	}
}

func TestStartupSourceUpdateConfirmRoutesThroughSourceUpdater(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	metadataPath := writeStartupSourceInstallMetadata(t, globalDir, prefix, binDir, "v0.8.0")
	runner := &startupSourceUpdateRunner{
		t:         t,
		globalDir: globalDir,
		prefix:    prefix,
		binDir:    binDir,
		latest:    "v0.8.1",
	}

	var out, errOut bytes.Buffer
	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method:       config.TUIInstallMethodSource,
		Detail:       "metadata at " + metadataPath,
		MetadataPath: metadataPath,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:               strings.NewReader("yes\n"),
		Output:              &out,
		ErrOutput:           &errOut,
		Runner:              runner,
		GlobalDir:           globalDir,
		Stat:                statMissingForStartupTest,
		SourceInstallScript: "/tmp/install.sh",
	})

	if !updated {
		t.Fatalf("confirmed source startup update should stop startup after update; stderr=%q output=\n%s", errOut.String(), out.String())
	}
	if !startupContainsCall(runner.calls, "bash /tmp/install.sh --update --prefix "+prefix+" --version v0.8.1 --non-interactive") {
		t.Fatalf("expected source installer update call, got %#v", runner.calls)
	}
	if startupHasProgram(runner.calls, "brew") {
		t.Fatalf("source startup update must not run brew, got %#v", runner.calls)
	}
	if !strings.Contains(out.String(), "Source/user-local TUI update verified") {
		t.Fatalf("missing source update verification output:\n%s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("successful source startup update should not write stderr, got %q", errOut.String())
	}
}

func TestStartupHomebrewUpdateDeclineDoesNotMutate(t *testing.T) {
	runner := &startupFakeRunner{}
	var out, errOut bytes.Buffer

	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method: config.TUIInstallMethodHomebrew,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("\n"),
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
		FindOtherTUIProcesses: func() []runningTUIProcess {
			return nil
		},
	})

	if updated {
		t.Fatal("declined homebrew migration should not stop startup")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("declined homebrew migration should not run commands, got %#v", runner.calls)
	}
	if !strings.Contains(out.String(), "Migrate from Homebrew to the native installer now? [y/N]") {
		t.Fatalf("missing migration consent prompt:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "lingtai-tui self-update") {
		t.Fatalf("missing manual self-update guidance:\n%s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("decline should not write stderr, got %q", errOut.String())
	}
}

// TestStartupHomebrewDuplicateInstallSkipsMigratePromptAndInstaller proves
// idempotence at the startup entry point: when detection already reports a
// verified-but-shadowed native install (DuplicateNativeInstall), startup must
// not re-prompt to MIGRATE or run install.sh again — but it must still ask
// the concrete cleanup question every launch (the human contract's "the next
// update detects the duplicate and asks again"). A declined cleanup answer
// leaves the truthful manual-cleanup-required state, never install.sh.
func TestStartupHomebrewDuplicateInstallSkipsMigratePromptAndInstaller(t *testing.T) {
	runner := &startupFakeRunner{}
	var out, errOut bytes.Buffer

	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method:                 config.TUIInstallMethodHomebrew,
		DuplicateNativeInstall: true,
		DuplicateNativeDetail:  "native install already verified at /tmp/native/bin/lingtai-tui (version v0.8.1), but executable under /opt/homebrew is still resolved first on PATH",
		DuplicateNativeTarget:  "/tmp/native/bin/lingtai-tui",
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("\n"), // declines the cleanup prompt (default No)
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
		FindOtherTUIProcesses: func() []runningTUIProcess {
			t.Fatal("duplicate-install detection must short-circuit before checking other running processes")
			return nil
		},
	})

	if updated {
		t.Fatal("duplicate-install detection must not report startup as updated/needing restart")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("duplicate-install detection must not run install.sh again, got %#v", runner.calls)
	}
	if strings.Contains(out.String(), "Migrate from Homebrew to the native installer now? [y/N]") {
		t.Fatalf("must not re-prompt to MIGRATE when a native install is already verified:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Remove the old Homebrew installation now? [y/N]") {
		t.Fatalf("expected the concrete cleanup prompt to be asked again:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "already exists") {
		t.Fatalf("expected already-exists guidance:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "brew uninstall") {
		t.Fatalf("expected manual brew uninstall guidance as the user's next step:\n%s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("truthful partial state is not an error, should not write stderr, got %q", errOut.String())
	}
}

func TestStartupHomebrewUpdateConfirmMigratesToNativeInstall(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	target := filepath.Join(binDir, "lingtai-tui")
	runner := &startupHomebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1"}
	var out, errOut bytes.Buffer

	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method: config.TUIInstallMethodHomebrew,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("y\n"),
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
		GlobalDir: globalDir,
		Stat:      statMissingForStartupTest,
		LookPath:  func(string) (string, error) { return target, nil },
		FindOtherTUIProcesses: func() []runningTUIProcess {
			return nil
		},
	})

	if !updated {
		t.Fatalf("confirmed homebrew migration should stop startup after migrating; stderr=%q output=\n%s", errOut.String(), out.String())
	}
	if !startupContainsCall(runner.calls, "install.sh --version v0.8.1 --non-interactive") {
		t.Fatalf("expected native fresh-install call, got %#v", runner.calls)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "--update") {
			t.Fatalf("homebrew migration must not use --update, got %#v", runner.calls)
		}
	}
	if !strings.Contains(out.String(), "Remove the old Homebrew installation now? [y/N]") {
		t.Fatalf("missing cleanup consent prompt after a verified migration:\n%s", out.String())
	}
	// Input is exhausted after the migration "y\n", so the cleanup prompt
	// reads EOF and defaults to No — Homebrew must stay untouched.
	if !strings.Contains(out.String(), "brew uninstall") {
		t.Fatalf("declined-by-default cleanup should still show manual brew uninstall guidance:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Migrated! Please restart lingtai-tui") {
		t.Fatalf("missing migration completion message:\n%s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("successful migration should not write stderr, got %q", errOut.String())
	}
}

// TestStartupHomebrewUpdateConfirmCleanupRemovesHomebrewAfterVerifiedPATHTakeover
// proves the full interactive y/y path end to end: migration consent, then a
// confirmed cleanup consent, triggers exactly one injected Homebrew uninstall
// and reports migration as fully complete once PATH is re-proven.
func TestStartupHomebrewUpdateConfirmCleanupRemovesHomebrewAfterVerifiedPATHTakeover(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	target := filepath.Join(binDir, "lingtai-tui")
	runner := &startupHomebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1"}
	var out, errOut bytes.Buffer
	uninstallCalls := 0

	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method: config.TUIInstallMethodHomebrew,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("y\ny\n"),
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
		GlobalDir: globalDir,
		Stat:      statMissingForStartupTest,
		LookPath:  func(string) (string, error) { return target, nil },
		FindOtherTUIProcesses: func() []runningTUIProcess {
			return nil
		},
		UninstallHomebrew: func() error {
			uninstallCalls++
			return nil
		},
	})

	if !updated {
		t.Fatalf("confirmed homebrew migration + cleanup should stop startup; stderr=%q output=\n%s", errOut.String(), out.String())
	}
	if uninstallCalls != 1 {
		t.Fatalf("expected exactly one injected uninstall call, got %d", uninstallCalls)
	}
	if startupHasProgram(runner.calls, "brew") {
		t.Fatalf("the CommandRunner must never see a brew call; uninstall goes through UninstallHomebrew only, got %#v", runner.calls)
	}
	if !strings.Contains(out.String(), "Removed the old Homebrew install") {
		t.Fatalf("expected cleanup-completed line:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Migration complete") {
		t.Fatalf("expected full migration-complete confirmation:\n%s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("successful migration+cleanup should not write stderr, got %q", errOut.String())
	}
}

// TestStartupHomebrewUpdateNotTakenOverByPATHDoesNotClaimMigrated is the
// startup-level version of the Apple-Silicon PATH-precedence scenario: a
// fresh install.sh run installs and verifies a native binary, but the
// resolved `lingtai-tui` on PATH is still the Homebrew one. Startup must not
// print "Migrated!" or stop as if a restart will pick up the new binary — it
// must fall through to normal startup with the manual-cleanup guidance
// already printed.
func TestStartupHomebrewUpdateNotTakenOverByPATHDoesNotClaimMigrated(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	runner := &startupHomebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1"}
	var out, errOut bytes.Buffer
	homebrewShadow := "/opt/homebrew/bin/lingtai-tui"

	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method: config.TUIInstallMethodHomebrew,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("y\n"),
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
		GlobalDir: globalDir,
		Stat:      statMissingForStartupTest,
		LookPath:  func(string) (string, error) { return homebrewShadow, nil },
		FindOtherTUIProcesses: func() []runningTUIProcess {
			return nil
		},
	})

	if updated {
		t.Fatal("a shadowed-PATH install must not report startup as updated/needing restart")
	}
	if !startupContainsCall(runner.calls, "install.sh --version v0.8.1 --non-interactive") {
		t.Fatalf("expected native fresh-install call to still run once, got %#v", runner.calls)
	}
	if strings.Contains(out.String(), "Migrated! Please restart lingtai-tui") {
		t.Fatalf("must not claim migration completion when PATH still resolves Homebrew:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "still resolves") {
		t.Fatalf("expected honest shadowed-PATH guidance:\n%s", out.String())
	}
	if !strings.Contains(strings.ToLower(out.String()), "not complete") {
		t.Fatalf("expected explicit not-complete guidance:\n%s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("a verified-but-shadowed install is not an error, should not write stderr, got %q", errOut.String())
	}
}

func TestStartupHomebrewUpdateFailureLeavesHomebrewUsable(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	runner := &startupHomebrewMigrationRunner{t: t, globalDir: globalDir, prefix: prefix, binDir: binDir, latest: "v0.8.1", failInstall: true}
	var out, errOut bytes.Buffer

	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method: config.TUIInstallMethodHomebrew,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("y\n"),
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
		GlobalDir: globalDir,
		Stat:      statMissingForStartupTest,
		FindOtherTUIProcesses: func() []runningTUIProcess {
			return nil
		},
	})

	if updated {
		t.Fatal("failed migration should not report startup as updated")
	}
	if errOut.Len() == 0 {
		t.Fatal("failed migration should report an error on stderr")
	}
	if !strings.Contains(out.String(), "untouched and still usable") {
		t.Fatalf("missing rollback guidance on failed migration:\n%s", out.String())
	}
}

func TestStartupUnknownInstallDoesNotMutate(t *testing.T) {
	runner := &startupFakeRunner{}
	var out, errOut bytes.Buffer

	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method: config.TUIInstallMethodUnknown,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("yes\n"),
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
	})

	if updated {
		t.Fatal("unknown startup update should never stop startup as updated")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unknown startup update should not run commands, got %#v", runner.calls)
	}
	if strings.TrimSpace(out.String()) != "lingtai-tui v0.8.0" {
		t.Fatalf("unknown install should preserve version-only startup output, got %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unknown install should not write stderr, got %q", errOut.String())
	}
}

type startupFakeRunner struct {
	calls []string
}

func (r *startupFakeRunner) Run(name string, args ...string) config.CommandResult {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return config.CommandResult{}
}

type startupSourceUpdateRunner struct {
	t         *testing.T
	globalDir string
	prefix    string
	binDir    string
	latest    string
	calls     []string
}

func (r *startupSourceUpdateRunner) Run(name string, args ...string) config.CommandResult {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	switch {
	case strings.Contains(call, "--update"):
		writeStartupSourceInstallMetadata(r.t, r.globalDir, r.prefix, r.binDir, r.latest)
		return config.CommandResult{Stdout: "updated\n"}
	case strings.HasSuffix(call, "lingtai-tui version"):
		return config.CommandResult{Stdout: "lingtai-tui " + r.latest + "\n"}
	default:
		return config.CommandResult{}
	}
}

type startupHomebrewMigrationRunner struct {
	t           *testing.T
	globalDir   string
	prefix      string
	binDir      string
	latest      string
	failInstall bool
	calls       []string
}

func (r *startupHomebrewMigrationRunner) Run(name string, args ...string) config.CommandResult {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	switch {
	case strings.Contains(call, "install.sh"):
		if r.failInstall {
			return config.CommandResult{Err: os.ErrInvalid, Stderr: "install failed"}
		}
		writeStartupSourceInstallMetadata(r.t, r.globalDir, r.prefix, r.binDir, r.latest)
		return config.CommandResult{Stdout: "installed\n"}
	case strings.HasSuffix(call, "lingtai-tui version"):
		return config.CommandResult{Stdout: "lingtai-tui " + r.latest + "\n"}
	case strings.Contains(call, "import lingtai"):
		return config.CommandResult{Stdout: "0.9.7\n"}
	default:
		return config.CommandResult{}
	}
}

func writeStartupSourceInstallMetadata(t *testing.T, globalDir, prefix, binDir, version string) string {
	t.Helper()
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(globalDir, "install.json")
	exe := filepath.Join(binDir, "lingtai-tui")
	meta := map[string]interface{}{
		"schema":           "lingtai.tui.install/v1",
		"schema_version":   1,
		"install_method":   "source",
		"prefix":           prefix,
		"bin_dir":          binDir,
		"stamped_version":  version,
		"managed_binaries": []string{exe},
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return metadataPath
}

func statMissingForStartupTest(string) (os.FileInfo, error) {
	return nil, os.ErrNotExist
}

func startupHasProgram(calls []string, program string) bool {
	for _, call := range calls {
		if call == program || strings.HasPrefix(call, program+" ") {
			return true
		}
	}
	return false
}

func startupContainsCall(calls []string, sub string) bool {
	for _, call := range calls {
		if strings.Contains(call, sub) {
			return true
		}
	}
	return false
}
