package main

import (
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
)

type launchUpgradeSpy struct {
	inspectCalls int
	applyCalls   int
	status       config.KernelStatus
	report       config.DoctorReport
}

func (s *launchUpgradeSpy) options(tty bool, input string, out *strings.Builder) launchKernelUpgradeOptions {
	return launchKernelUpgradeOptions{
		Inspect: func() config.KernelStatus {
			s.inspectCalls++
			return s.status
		},
		Apply: func() config.DoctorReport {
			s.applyCalls++
			return s.report
		},
		IsTTY:  func() bool { return tty },
		Input:  strings.NewReader(input),
		Output: out,
	}
}

func updateAvailableStatus() config.KernelStatus {
	return config.KernelStatus{
		Installed:   "0.9.3",
		Latest:      "0.9.4",
		NeedsUpdate: true,
	}
}

func TestLaunchKernelUpgradeFirstInstallNeverInspects(t *testing.T) {
	spy := &launchUpgradeSpy{status: updateAvailableStatus()}
	var out strings.Builder

	maybePromptKernelUpgrade(true, spy.options(true, "y\n", &out))

	if spy.inspectCalls != 0 {
		t.Errorf("inspect called %d times on first install, want 0", spy.inspectCalls)
	}
	if spy.applyCalls != 0 {
		t.Errorf("apply called %d times on first install, want 0", spy.applyCalls)
	}
	if out.Len() != 0 {
		t.Errorf("unexpected output on first install: %q", out.String())
	}
}

func TestLaunchKernelUpgradeYesApplies(t *testing.T) {
	spy := &launchUpgradeSpy{status: updateAvailableStatus()}
	spy.report = config.DoctorReport{
		Healthy: true,
		Lines:   []config.DoctorLine{{Severity: config.DoctorOK, Text: "Upgraded Python lingtai: 0.9.3 → 0.9.4"}},
	}
	var out strings.Builder

	maybePromptKernelUpgrade(false, spy.options(true, "y\n", &out))

	if spy.applyCalls != 1 {
		t.Fatalf("apply called %d times, want 1", spy.applyCalls)
	}
	got := out.String()
	if !strings.Contains(got, "0.9.3") || !strings.Contains(got, "0.9.4") {
		t.Errorf("prompt missing versions: %q", got)
	}
	if !strings.Contains(got, "[y/N]") {
		t.Errorf("prompt missing [y/N]: %q", got)
	}
	if !strings.Contains(got, "Upgraded Python lingtai: 0.9.3 → 0.9.4") {
		t.Errorf("report lines not printed: %q", got)
	}
}

func TestLaunchKernelUpgradeApplyFailureStillReturns(t *testing.T) {
	spy := &launchUpgradeSpy{status: updateAvailableStatus()}
	spy.report = config.DoctorReport{
		Healthy: false,
		Lines:   []config.DoctorLine{{Severity: config.DoctorFail, Text: "pip upgrade failed"}},
	}
	var out strings.Builder

	// Must not panic/abort — the FAIL lines are printed and launch continues.
	maybePromptKernelUpgrade(false, spy.options(true, "y\n", &out))

	if spy.applyCalls != 1 {
		t.Fatalf("apply called %d times, want 1", spy.applyCalls)
	}
	if !strings.Contains(out.String(), "pip upgrade failed") {
		t.Errorf("FAIL line not printed: %q", out.String())
	}
}

func TestLaunchKernelUpgradeDeclineDoesNotApply(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"n", "n\n"},
		{"empty line", "\n"},
		{"eof", ""},
		{"garbage", "maybe\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy := &launchUpgradeSpy{status: updateAvailableStatus()}
			var out strings.Builder

			maybePromptKernelUpgrade(false, spy.options(true, tc.input, &out))

			if spy.inspectCalls != 1 {
				t.Errorf("inspect called %d times, want 1", spy.inspectCalls)
			}
			if spy.applyCalls != 0 {
				t.Errorf("apply called %d times after decline, want 0", spy.applyCalls)
			}
			if !strings.Contains(out.String(), "[y/N]") {
				t.Errorf("prompt not shown at TTY: %q", out.String())
			}
		})
	}
}

func TestLaunchKernelUpgradeNonTTYSkipsSilently(t *testing.T) {
	spy := &launchUpgradeSpy{status: updateAvailableStatus()}
	var out strings.Builder

	maybePromptKernelUpgrade(false, spy.options(false, "y\n", &out))

	if spy.applyCalls != 0 {
		t.Errorf("apply called %d times on non-TTY, want 0", spy.applyCalls)
	}
	if out.Len() != 0 {
		t.Errorf("non-TTY launch must print nothing, got %q", out.String())
	}
}

func TestLaunchKernelUpgradeNoUpdateNoPrompt(t *testing.T) {
	for _, status := range []config.KernelStatus{
		{Installed: "0.9.4", Latest: "0.9.4", NeedsUpdate: false},
		{Installed: "0.9.3", Editable: true, NeedsUpdate: false},
		{Installed: "0.9.3", NeedsUpdate: false}, // PyPI lookup failed
	} {
		spy := &launchUpgradeSpy{status: status}
		var out strings.Builder

		maybePromptKernelUpgrade(false, spy.options(true, "y\n", &out))

		if spy.applyCalls != 0 {
			t.Errorf("apply called %d times with NeedsUpdate=false, want 0", spy.applyCalls)
		}
		if out.Len() != 0 {
			t.Errorf("unexpected output with NeedsUpdate=false: %q", out.String())
		}
	}
}
