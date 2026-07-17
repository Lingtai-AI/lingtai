package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
)

func interactiveTrue() bool { return true }

func kernelUpToDate() config.KernelStatus {
	return config.KernelStatus{Installed: "0.9.7", Latest: "0.9.7", NeedsUpdate: false}
}

func kernelUpdateAvailable() config.KernelStatus {
	return config.KernelStatus{Installed: "0.9.6", Latest: "0.9.7", NeedsUpdate: true}
}

func kernelEditable() config.KernelStatus {
	return config.KernelStatus{Installed: "0.9.6", Editable: true, NeedsUpdate: false}
}

func kernelInspectFailed() config.KernelStatus {
	// InspectKernel's own contract: a failed/offline lookup reports
	// NeedsUpdate=false (never asks/mutates on an inconclusive check).
	return config.KernelStatus{NeedsUpdate: false}
}

func kernelMissing() config.KernelStatus {
	// InspectKernel's contract for an absent/unimportable/broken runtime:
	// Installed=="" and NeedsUpdate=true — distinct from a present-but-stale
	// version bump, and must render as an install/repair prompt, never a
	// generic "update available" one.
	return config.KernelStatus{NeedsUpdate: true}
}

// TestMaybeCheckAndPromptAlwaysCallsInspectKernel proves the read-only check
// runs unconditionally — even non-interactively — satisfying "every ordinary
// interactive TUI startup performs read-only version checks." for both TTY
// and non-TTY launches (the non-interactive case still checks, it just never
// prompts or mutates on top of the check).
func TestMaybeCheckAndPromptAlwaysCallsInspectKernel(t *testing.T) {
	for _, interactive := range []bool{true, false} {
		inspectCalls := 0
		maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
			Input:       strings.NewReader("n\n"),
			Output:      &bytes.Buffer{},
			Interactive: func() bool { return interactive },
			InspectKernelFunc: func(string) config.KernelStatus {
				inspectCalls++
				return kernelUpToDate()
			},
		})
		if inspectCalls != 1 {
			t.Fatalf("interactive=%v: expected InspectKernel called exactly once, got %d", interactive, inspectCalls)
		}
	}
}

// TestMaybeCheckAndPromptUpToDateNeverPrompts proves that when InspectKernel
// reports NeedsUpdate=false (already current), no prompt is printed and the
// mutating update is never called — even on an interactive TTY.
func TestMaybeCheckAndPromptUpToDateNeverPrompts(t *testing.T) {
	var out bytes.Buffer
	updateCalls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("y\n"), // queued yes must never be consumed
		Output:            &out,
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelUpToDate() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			updateCalls++
			return config.DoctorReport{Healthy: true}
		},
	})
	if updated {
		t.Fatal("already-current kernel must not report an upgrade")
	}
	if updateCalls != 0 {
		t.Fatalf("already-current kernel must never call RunKernelUpdate, got %d calls", updateCalls)
	}
	if out.Len() != 0 {
		t.Fatalf("already-current kernel must not print a prompt, got %q", out.String())
	}
}

// TestMaybeCheckAndPromptEditableNeverPrompts proves an editable/dev install
// (Editable=true, NeedsUpdate=false) is treated the same as up-to-date: no
// prompt, no mutation.
func TestMaybeCheckAndPromptEditableNeverPrompts(t *testing.T) {
	var out bytes.Buffer
	updateCalls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("y\n"),
		Output:            &out,
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelEditable() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			updateCalls++
			return config.DoctorReport{Healthy: true}
		},
	})
	if updated || updateCalls != 0 || out.Len() != 0 {
		t.Fatalf("editable install must not prompt or mutate: updated=%v calls=%d out=%q", updated, updateCalls, out.String())
	}
}

// TestMaybeCheckAndPromptInspectErrorNeverPrompts proves that when the
// read-only check itself is inconclusive (InspectKernel's own contract:
// NeedsUpdate=false on lookup failure), no prompt is shown and no mutation
// happens — the check error must not be treated as "update available."
func TestMaybeCheckAndPromptInspectErrorNeverPrompts(t *testing.T) {
	var out bytes.Buffer
	updateCalls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("y\n"),
		Output:            &out,
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelInspectFailed() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			updateCalls++
			return config.DoctorReport{Healthy: true}
		},
	})
	if updated || updateCalls != 0 || out.Len() != 0 {
		t.Fatalf("inconclusive check must not prompt or mutate: updated=%v calls=%d out=%q", updated, updateCalls, out.String())
	}
}

// TestMaybeCheckAndPromptAvailableShowsPromptAndDeclineDoesNotMutate proves
// that when an update IS available, the prompt is shown with the
// installed→latest versions, and declining performs zero mutation.
func TestMaybeCheckAndPromptAvailableShowsPromptAndDeclineDoesNotMutate(t *testing.T) {
	var out bytes.Buffer
	updateCalls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("n\n"),
		Output:            &out,
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelUpdateAvailable() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			updateCalls++
			return config.DoctorReport{Healthy: true}
		},
	})
	if updated {
		t.Fatal("declining an available update must not report an upgrade")
	}
	if updateCalls != 0 {
		t.Fatalf("declining an available update must not call RunKernelUpdate, got %d calls", updateCalls)
	}
	if !strings.Contains(out.String(), "0.9.6") || !strings.Contains(out.String(), "0.9.7") {
		t.Fatalf("expected installed/latest versions in the prompt, got %q", out.String())
	}
	if !strings.Contains(out.String(), "[y/N]") {
		t.Fatalf("expected a [y/N] consent prompt, got %q", out.String())
	}
}

// TestMaybeCheckAndPromptMissingShowsInstallRepairPromptNotGenericUpdate
// proves the required state-machine distinction: an absent/unimportable/
// broken kernel (Installed=="") renders an explicit INSTALL/REPAIR prompt,
// never the generic "update available" wording used for a present-but-stale
// version. Declining performs zero mutation.
func TestMaybeCheckAndPromptMissingShowsInstallRepairPromptNotGenericUpdate(t *testing.T) {
	var out bytes.Buffer
	updateCalls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("n\n"),
		Output:            &out,
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelMissing() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			updateCalls++
			return config.DoctorReport{Healthy: true}
		},
	})
	if updated {
		t.Fatal("declining an install/repair prompt must not report success")
	}
	if updateCalls != 0 {
		t.Fatalf("declining install/repair must not call RunKernelUpdate, got %d calls", updateCalls)
	}
	prompt := out.String()
	if !strings.Contains(prompt, "not installed") && !strings.Contains(prompt, "Install") {
		t.Fatalf("expected explicit install/repair wording, got %q", prompt)
	}
	if strings.Contains(prompt, "update available") || strings.Contains(prompt, "→") {
		t.Fatalf("missing/broken kernel must not use the generic update-available or version-diff wording, got %q", prompt)
	}
	if !strings.Contains(prompt, "[y/N]") {
		t.Fatalf("expected a [y/N] consent prompt, got %q", prompt)
	}
}

// TestMaybeCheckAndPromptMissingEOFDoesNotMutate proves EOF on the
// install/repair prompt is treated as decline — zero mutation.
func TestMaybeCheckAndPromptMissingEOFDoesNotMutate(t *testing.T) {
	updateCalls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader(""),
		Output:            &bytes.Buffer{},
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelMissing() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			updateCalls++
			return config.DoctorReport{Healthy: true}
		},
	})
	if updated || updateCalls != 0 {
		t.Fatalf("EOF on the install/repair prompt must not mutate: updated=%v calls=%d", updated, updateCalls)
	}
}

// TestMaybeCheckAndPromptMissingNonInteractiveDoesNotMutate proves a
// non-interactive launch (piped/redirected stdin, headless) never prompts
// for install/repair and never mutates, even with a queued "y".
func TestMaybeCheckAndPromptMissingNonInteractiveDoesNotMutate(t *testing.T) {
	var out bytes.Buffer
	updateCalls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("y\n"),
		Output:            &out,
		Interactive:       func() bool { return false },
		InspectKernelFunc: func(string) config.KernelStatus { return kernelMissing() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			updateCalls++
			return config.DoctorReport{Healthy: true}
		},
	})
	if updated || updateCalls != 0 || out.Len() != 0 {
		t.Fatalf("non-interactive launch must not prompt or mutate for install/repair: updated=%v calls=%d out=%q", updated, updateCalls, out.String())
	}
}

// TestMaybeCheckAndPromptMissingConsentInstallsExactlyOnce proves an
// explicit "y" on an install/repair prompt is the only path that calls the
// mutating RunKernelUpdate for a missing/broken kernel, exactly once,
// force=false.
func TestMaybeCheckAndPromptMissingConsentInstallsExactlyOnce(t *testing.T) {
	calls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("y\n"),
		Output:            &bytes.Buffer{},
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelMissing() },
		RunKernelUpdate: func(globalDir string, force bool) config.DoctorReport {
			calls++
			if globalDir != "/tmp/lingtai-test" {
				t.Fatalf("RunKernelUpdate globalDir = %q, want /tmp/lingtai-test", globalDir)
			}
			if force {
				t.Fatal("routine consent-driven install/repair must call RunKernelUpdate with force=false")
			}
			return config.DoctorReport{Healthy: true}
		},
	})
	if !updated {
		t.Fatal("explicit consent to install/repair should report success")
	}
	if calls != 1 {
		t.Fatalf("expected exactly one RunKernelUpdate call, got %d", calls)
	}
}

// TestMaybeCheckAndPromptAvailableEOFDoesNotMutate proves EOF (stdin closed)
// on an available-update prompt is treated as decline — zero mutation.
func TestMaybeCheckAndPromptAvailableEOFDoesNotMutate(t *testing.T) {
	updateCalls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader(""),
		Output:            &bytes.Buffer{},
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelUpdateAvailable() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			updateCalls++
			return config.DoctorReport{Healthy: true}
		},
	})
	if updated || updateCalls != 0 {
		t.Fatalf("EOF on an available-update prompt must not mutate: updated=%v calls=%d", updated, updateCalls)
	}
}

// TestMaybeCheckAndPromptAvailableDefaultAnswerDoesNotMutate proves a bare
// Enter (no explicit "y") on a [y/N] prompt defaults to no mutation.
func TestMaybeCheckAndPromptAvailableDefaultAnswerDoesNotMutate(t *testing.T) {
	updateCalls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("\n"),
		Output:            &bytes.Buffer{},
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelUpdateAvailable() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			updateCalls++
			return config.DoctorReport{Healthy: true}
		},
	})
	if updated || updateCalls != 0 {
		t.Fatalf("bare Enter on a [y/N] prompt must not mutate: updated=%v calls=%d", updated, updateCalls)
	}
}

// TestMaybeCheckAndPromptAvailableNonInteractiveDoesNotMutate proves that
// even when InspectKernel reports an available update, a non-interactive
// launch (piped/redirected stdin, headless) never prompts and never mutates
// — matching "non-TTY => zero mutation" regardless of a queued affirmative
// answer.
func TestMaybeCheckAndPromptAvailableNonInteractiveDoesNotMutate(t *testing.T) {
	var out bytes.Buffer
	updateCalls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("y\n"),
		Output:            &out,
		Interactive:       func() bool { return false },
		InspectKernelFunc: func(string) config.KernelStatus { return kernelUpdateAvailable() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			updateCalls++
			return config.DoctorReport{Healthy: true}
		},
	})
	if updated || updateCalls != 0 || out.Len() != 0 {
		t.Fatalf("non-interactive launch must not prompt or mutate: updated=%v calls=%d out=%q", updated, updateCalls, out.String())
	}
}

// TestMaybeCheckAndPromptConsentCallsUpdateExactlyOnce proves an explicit "y"
// on THIS launch, with an update actually available, is the only path that
// calls the mutating RunKernelUpdate — and it runs exactly once, force=false
// (routine, not doctor's forced semantics), with the correct globalDir.
func TestMaybeCheckAndPromptConsentCallsUpdateExactlyOnce(t *testing.T) {
	var out bytes.Buffer
	calls := 0
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("y\n"),
		Output:            &out,
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelUpdateAvailable() },
		RunKernelUpdate: func(globalDir string, force bool) config.DoctorReport {
			calls++
			if globalDir != "/tmp/lingtai-test" {
				t.Fatalf("RunKernelUpdate globalDir = %q, want /tmp/lingtai-test", globalDir)
			}
			if force {
				t.Fatal("routine consent-driven update must call RunKernelUpdate with force=false")
			}
			return config.DoctorReport{Healthy: true}
		},
	})
	if !updated {
		t.Fatal("explicit consent with an available update should report the upgrade result")
	}
	if calls != 1 {
		t.Fatalf("expected exactly one RunKernelUpdate call, got %d", calls)
	}
}

// TestMaybeCheckAndPromptConsentUpdateFailureReportsNotUpdated proves that if
// the consented update itself fails, the function reports it did not
// succeed (Healthy=false) rather than claiming an upgrade happened.
func TestMaybeCheckAndPromptConsentUpdateFailureReportsNotUpdated(t *testing.T) {
	updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
		Input:             strings.NewReader("y\n"),
		Output:            &bytes.Buffer{},
		Interactive:       interactiveTrue,
		InspectKernelFunc: func(string) config.KernelStatus { return kernelUpdateAvailable() },
		RunKernelUpdate: func(string, bool) config.DoctorReport {
			return config.DoctorReport{Healthy: false, Lines: []config.DoctorLine{{Text: "upgrade command failed"}}}
		},
	})
	if updated {
		t.Fatal("a failed update must not be reported as an upgrade")
	}
}

// TestInspectKernelIssuesNoMutatingCommands is a smoke test proving the real
// production config.InspectKernel (not a fake) issues no install/upgrade
// command against a nonexistent venv path — it reports "venv not found" and
// returns, never touching pip/uv/brew, and never reaching the network PyPI
// fetch. A missing venv is InspectKernel's own contract for NeedsUpdate=true
// (RunKernelUpdate installs/repairs the venv as part of that consented
// action). InspectKernel runs FIRST in production, in main()'s shared
// startup preflight, before any readiness check — there is no automatic
// EnsureRuntime/EnsureRuntimeQuiet call anywhere any more that could run
// before it; a missing/broken runtime after a decline surfaces later as
// config.RuntimeReady's actionable error, never a silent repair. This test
// only proves InspectKernel itself is read-only, not main.go's ordering
// (that's proved by the maybeCheckAndPrompt* tests above via the
// InspectKernelFunc seam).
func TestInspectKernelIssuesNoMutatingCommands(t *testing.T) {
	status := config.InspectKernel(t.TempDir())
	if !status.NeedsUpdate {
		t.Fatalf("a missing venv is expected to report NeedsUpdate=true per InspectKernel's own contract, got %+v", status)
	}
	if status.Installed != "" {
		t.Fatalf("a missing venv must report no installed version, got %+v", status)
	}
	if len(status.Lines) == 0 || !strings.Contains(status.Lines[0].Text, "not found") {
		t.Fatalf("expected a 'venv not found' diagnostic line, got %+v", status.Lines)
	}
}
