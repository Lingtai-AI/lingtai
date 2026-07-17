package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
)

func defaultPreflightOptions(out *bytes.Buffer) preflightOptions {
	return preflightOptions{
		GlobalDirPath:        func() (string, error) { return "/tmp/lingtai-test", nil },
		CheckTUIUpgradeFunc:  func(string) string { return "" },
		CheckAndPromptKernel: func(string) bool { return false },
		PrintOutput:          out,
	}
}

// TestRunVersionPreflightChecksBothTUIAndKernel proves the shared preflight
// calls BOTH the read-only TUI check (config.CheckTUIUpgrade) and the
// read-only kernel check (via CheckAndPromptKernel, which itself always
// calls config.InspectKernel first) exactly once per invocation — this is
// the literal "every default interactive launch checks both TUI and kernel"
// requirement, proven at the shared call site rather than per-branch.
func TestRunVersionPreflightChecksBothTUIAndKernel(t *testing.T) {
	var out bytes.Buffer
	tuiCalls := 0
	kernelCalls := 0
	opts := defaultPreflightOptions(&out)
	opts.CheckTUIUpgradeFunc = func(string) string {
		tuiCalls++
		return ""
	}
	opts.CheckAndPromptKernel = func(globalDir string) bool {
		kernelCalls++
		if globalDir != "/tmp/lingtai-test" {
			t.Fatalf("CheckAndPromptKernel globalDir = %q, want /tmp/lingtai-test", globalDir)
		}
		return false
	}
	exited := runVersionPreflightWithOptions(opts)
	if exited {
		t.Fatal("up-to-date TUI and kernel must not exit main()")
	}
	if tuiCalls != 1 {
		t.Fatalf("expected exactly one TUI check, got %d", tuiCalls)
	}
	if kernelCalls != 1 {
		t.Fatalf("expected exactly one kernel check, got %d", kernelCalls)
	}
}

// TestRunVersionPreflightGlobalDirFailureExits proves a failed GlobalDirPath
// resolution reports the error and signals main() to exit, without ever
// reaching the TUI or kernel checks.
func TestRunVersionPreflightGlobalDirFailureExits(t *testing.T) {
	var out bytes.Buffer
	tuiCalls := 0
	kernelCalls := 0
	opts := defaultPreflightOptions(&out)
	opts.GlobalDirPath = func() (string, error) { return "", errors.New("no home dir") }
	opts.CheckTUIUpgradeFunc = func(string) string { tuiCalls++; return "" }
	opts.CheckAndPromptKernel = func(string) bool { kernelCalls++; return false }
	exited := runVersionPreflightWithOptions(opts)
	if !exited {
		t.Fatal("a failed GlobalDirPath must signal main() to exit")
	}
	if tuiCalls != 0 || kernelCalls != 0 {
		t.Fatalf("neither check should run after a GlobalDirPath failure: tui=%d kernel=%d", tuiCalls, kernelCalls)
	}
}

// TestRunVersionPreflightTUISelfUpgradeExits proves that a successful TUI
// self-upgrade (Homebrew/source install method, HandleTUIUpgrade returns
// true) signals main() to exit immediately — the user was already told to
// restart — and never reaches the kernel check on this same launch.
func TestRunVersionPreflightTUISelfUpgradeExits(t *testing.T) {
	var out bytes.Buffer
	kernelCalls := 0
	opts := defaultPreflightOptions(&out)
	opts.CheckTUIUpgradeFunc = func(string) string { return "v0.9.0" }
	opts.DetectCurrentTUIInstall = func(string) config.TUIInstallInfo {
		return config.TUIInstallInfo{Method: config.TUIInstallMethodHomebrew}
	}
	opts.HandleTUIUpgrade = func(config.TUIInstallInfo, string, string, string) bool { return true }
	opts.CheckAndPromptKernel = func(string) bool { kernelCalls++; return false }
	exited := runVersionPreflightWithOptions(opts)
	if !exited {
		t.Fatal("a completed TUI self-upgrade must signal main() to exit")
	}
	if kernelCalls != 0 {
		t.Fatalf("kernel check must not run on the same launch as a completed TUI self-upgrade, got %d calls", kernelCalls)
	}
}

// TestRunVersionPreflightTUIUpgradeDeclinedContinuesToKernelCheck proves that
// when a TUI update is available (Homebrew/source) but HandleTUIUpgrade
// returns false (user declined, or an unknown install method took the
// version-only path), the preflight continues on to the kernel check rather
// than exiting.
func TestRunVersionPreflightTUIUpgradeDeclinedContinuesToKernelCheck(t *testing.T) {
	var out bytes.Buffer
	kernelCalls := 0
	opts := defaultPreflightOptions(&out)
	opts.CheckTUIUpgradeFunc = func(string) string { return "v0.9.0" }
	opts.DetectCurrentTUIInstall = func(string) config.TUIInstallInfo {
		return config.TUIInstallInfo{Method: config.TUIInstallMethodHomebrew}
	}
	opts.HandleTUIUpgrade = func(config.TUIInstallInfo, string, string, string) bool { return false }
	opts.CheckAndPromptKernel = func(string) bool { kernelCalls++; return false }
	exited := runVersionPreflightWithOptions(opts)
	if exited {
		t.Fatal("a declined TUI upgrade must not exit main()")
	}
	if kernelCalls != 1 {
		t.Fatalf("expected the kernel check to still run after a declined TUI upgrade, got %d calls", kernelCalls)
	}
}

// TestRunVersionPreflightUnknownInstallMethodPrintsVersionAndContinues
// proves that an unrecognized TUI install method (not Homebrew/source) never
// calls HandleTUIUpgrade at all — it just prints the version-only banner —
// and still continues to the kernel check.
func TestRunVersionPreflightUnknownInstallMethodPrintsVersionAndContinues(t *testing.T) {
	var out bytes.Buffer
	handleCalls := 0
	kernelCalls := 0
	opts := defaultPreflightOptions(&out)
	opts.CheckTUIUpgradeFunc = func(string) string { return "v0.9.0" }
	opts.DetectCurrentTUIInstall = func(string) config.TUIInstallInfo {
		return config.TUIInstallInfo{Method: config.TUIInstallMethodUnknown}
	}
	opts.HandleTUIUpgrade = func(config.TUIInstallInfo, string, string, string) bool {
		handleCalls++
		return false
	}
	opts.CheckAndPromptKernel = func(string) bool { kernelCalls++; return false }
	exited := runVersionPreflightWithOptions(opts)
	if exited {
		t.Fatal("unknown install method must not exit main()")
	}
	if handleCalls != 0 {
		t.Fatalf("unknown install method must never call HandleTUIUpgrade, got %d calls", handleCalls)
	}
	if kernelCalls != 1 {
		t.Fatalf("expected the kernel check to still run, got %d calls", kernelCalls)
	}
	if !strings.Contains(out.String(), "lingtai-tui ") {
		t.Fatalf("expected the version-only banner, got %q", out.String())
	}
}

// TestRunVersionPreflightKernelUpgradedPrintsConfirmation proves that when
// CheckAndPromptKernel reports an upgrade was actually performed, the
// preflight prints the confirmation line — matching the pre-existing
// "Upgraded lingtai to latest version." convention this correction preserved
// from the original returning-user path.
func TestRunVersionPreflightKernelUpgradedPrintsConfirmation(t *testing.T) {
	var out bytes.Buffer
	opts := defaultPreflightOptions(&out)
	opts.CheckAndPromptKernel = func(string) bool { return true }
	exited := runVersionPreflightWithOptions(opts)
	if exited {
		t.Fatal("a successful kernel upgrade must not exit main()")
	}
	if !strings.Contains(out.String(), "Upgraded lingtai to latest version.") {
		t.Fatalf("expected upgrade confirmation line, got %q", out.String())
	}
}
