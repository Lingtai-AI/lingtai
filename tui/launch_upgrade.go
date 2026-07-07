package main

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
)

// launchKernelUpgradeOptions injects the side-effecting seams of the launch
// kernel-upgrade gate so tests never touch the network, the venv, or a real
// terminal. main() wires the real InspectKernel/RunKernelUpdate/stdin/stdout.
type launchKernelUpgradeOptions struct {
	Inspect func() config.KernelStatus
	Apply   func() config.DoctorReport
	IsTTY   func() bool
	Input   io.Reader
	Output  io.Writer
}

func (o *launchKernelUpgradeOptions) setDefaults() {
	if o.IsTTY == nil {
		// Both ends must be terminals: the prompt is written to stdout and the
		// y/N answer is read from stdin. With only a stdout check, a piped
		// stdin (wrapper script, process supervisor, `yes |`) would either
		// hang the launch on the read or auto-accept a venv-mutating upgrade.
		o.IsTTY = func() bool {
			return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
		}
	}
	if o.Input == nil {
		o.Input = os.Stdin
	}
	if o.Output == nil {
		o.Output = os.Stdout
	}
}

// maybePromptKernelUpgrade replaces the old silent launch auto-upgrade with a
// confirm-then-update gate. wasFirstInstall must be NeedsVenv captured BEFORE
// the venv was ensured: a first install just built its kernel from the latest
// wheel, so there is nothing to upgrade and no prompt. For an existing kernel
// with an update available, an interactive launch asks y/N (EOF/empty/anything
// but yes declines, per-launch only); a non-TTY launch skips silently so
// scripts and CI are never blocked and never mutated. A failed apply prints
// its FAIL lines and returns — an optional upgrade never aborts the launch.
func maybePromptKernelUpgrade(wasFirstInstall bool, opts launchKernelUpgradeOptions) {
	if wasFirstInstall {
		return
	}
	opts.setDefaults()

	// TTY gate first: a non-TTY launch must never pay Inspect's network
	// probe (up to 5s against PyPI when offline) for a prompt it can't show.
	if !opts.IsTTY() {
		return
	}

	status := opts.Inspect()
	if !status.NeedsUpdate {
		return
	}

	fmt.Fprint(opts.Output, i18n.TF("launch.kernel_upgrade_prompt", status.Installed, status.Latest))
	if !answerYes(readLineLower(opts.Input)) {
		fmt.Fprintln(opts.Output, i18n.T("launch.kernel_upgrade_skipped"))
		return
	}

	fmt.Fprintln(opts.Output, i18n.T("launch.kernel_upgrading"))
	report := opts.Apply()
	for _, line := range report.Lines {
		fmt.Fprintf(opts.Output, "  %s\n", line.Text)
	}
	if report.Healthy {
		fmt.Fprintln(opts.Output, i18n.T("launch.kernel_upgraded"))
	}
}
