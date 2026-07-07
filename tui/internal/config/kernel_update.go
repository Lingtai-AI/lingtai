package config

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// KernelStatus is the read-only result of InspectKernel: the installed and
// latest Python `lingtai` kernel versions, whether the install is an editable
// dev checkout, whether an update is warranted, and the human-readable lines
// describing what was inspected. It issues no install/brew commands.
type KernelStatus struct {
	Installed   string // installed lingtai version, or "" if unimportable
	Latest      string // latest PyPI version, or "" if lookup failed
	Editable    bool   // editable/dev install detected
	NeedsUpdate bool   // false when editable, missing-latest, or installed==latest
	Lines       []DoctorLine
}

// inspectKernelOptions injects side effects for tests. Production callers use
// InspectKernel, which leaves all fields at their defaults (real exec/http).
type inspectKernelOptions struct {
	HTTPClient *http.Client
	Runner     CommandRunner
	Stat       func(string) (os.FileInfo, error)
	Home       string
	LookupEnv  func(string) (string, bool)
}

// InspectKernel reads the managed venv's installed `lingtai` version and the
// latest PyPI release WITHOUT mutating anything: no brew, no pip/uv install.
// It reuses the same helpers the upgrade path uses (VenvPython,
// pythonLingtaiVersion, isEditableLingtaiInstall, fetchLatestPyPIVersion) so
// the read-only classification cannot drift from the apply step
// (RunKernelUpdate). Editable dev installs report Editable=true and
// NeedsUpdate=false — they are never reinstalled.
func InspectKernel(globalDir string) KernelStatus {
	return inspectKernel(globalDir, inspectKernelOptions{})
}

func inspectKernel(globalDir string, opts inspectKernelOptions) KernelStatus {
	if opts.Runner == nil {
		opts.Runner = execCommandRunner{}
	}
	if opts.Stat == nil {
		opts.Stat = os.Stat
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}

	status := KernelStatus{}
	python := VenvPython(RuntimeVenvDir(globalDir))

	if _, err := opts.Stat(python); err != nil {
		status.add(DoctorWarn, "Python runtime venv not found at %s", python)
		// No installed version and nothing to compare; an update (which rebuilds
		// the venv via RunKernelUpdate) is warranted.
		status.NeedsUpdate = true
		return status
	}

	installed, err := pythonLingtaiVersion(opts.Runner, python)
	if err != nil {
		status.add(DoctorWarn, "Could not import lingtai from %s: %v", python, err)
		status.NeedsUpdate = true
		return status
	}
	status.Installed = installed
	status.add(DoctorInfo, "Installed Python lingtai: %s", installed)

	if editable, source := isEditableLingtaiInstall(opts.Runner, python); editable {
		status.Editable = true
		status.NeedsUpdate = false
		if source != "" {
			status.add(DoctorOK, "Python lingtai is an editable install (%s); skipping upgrade", source)
		} else {
			status.add(DoctorOK, "Python lingtai is an editable install; skipping upgrade")
		}
		return status
	}

	// Dev-checkout machines: even when the runtime is a PyPI wheel, the apply
	// step (UpgradePythonRuntime) converts it to an editable install against the
	// discovered local source rather than running a PyPI upgrade. Mirror that
	// classification here so the confirm prompt never shows a misleading
	// "X → Y" PyPI diff that the apply step would not actually perform. This is
	// the one place inspect/apply could otherwise drift.
	home := opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" {
		if dev, ok := findDevCheckouts(home, opts.LookupEnv); ok {
			status.Editable = true
			status.NeedsUpdate = false
			status.add(DoctorOK, "Local dev checkout detected at %s; the kernel update would reinstall editable, not upgrade from PyPI — skipping", dev.KernelSrc)
			return status
		}
	}

	latest, err := fetchLatestPyPIVersion(opts.HTTPClient)
	if err != nil {
		status.add(DoctorWarn, "Could not check latest Python lingtai on PyPI: %v", err)
		// Without a latest version we cannot say an update is available.
		status.NeedsUpdate = false
		return status
	}
	status.Latest = latest
	status.add(DoctorInfo, "Latest Python lingtai on PyPI: %s", latest)

	if installed == latest {
		status.NeedsUpdate = false
		status.add(DoctorOK, "Python lingtai runtime is up to date")
		return status
	}
	status.NeedsUpdate = true
	status.add(DoctorWarn, "Python lingtai update available: %s → %s", installed, latest)
	return status
}

func (s *KernelStatus) add(sev DoctorSeverity, format string, args ...interface{}) {
	s.Lines = append(s.Lines, DoctorLine{Severity: sev, Text: fmt.Sprintf(format, args...)})
}

// UpdatePlan is the read-only aggregate InspectUpdate produces for the
// interactive /doctor: the classification of every update surface it can
// heal, plus the diagnostic lines in check order (TUI, kernel, file-search).
type UpdatePlan struct {
	TUI    TUIUpdateCheck
	Kernel KernelStatus
	Lines  []DoctorLine
}

// TUIHealable reports whether the heal cascade can actually update the TUI
// binary. Only the Homebrew backend mutates; source/unknown installs get
// manual guidance in the diagnostic lines, so they must not trigger a heal
// prompt that would then do nothing.
func (p UpdatePlan) TUIHealable() bool {
	return p.TUI.UpdateAvailable && p.TUI.Install.Method == TUIInstallMethodHomebrew
}

// NeedsHeal reports whether the plan contains at least one finding the heal
// cascade can fix.
func (p UpdatePlan) NeedsHeal() bool {
	return p.Kernel.NeedsUpdate || p.TUIHealable()
}

// InspectUpdate runs the strictly read-only classification of the doctor's
// update surfaces: the TUI binary release check, the kernel version check,
// and the file-search sidecar probe. It never runs brew/pip/uv install and
// never rebuilds the venv — it is the diagnostic half whose apply step is the
// gated heal (RunKernelUpdate + RunTUIUpdate + asset bootstrap).
func InspectUpdate(globalDir string, opts DoctorOptions) UpdatePlan {
	opts = fillDoctorOptionDefaults(opts)
	report := DoctorReport{Healthy: true}
	plan := UpdatePlan{}
	plan.TUI = report.inspectTUI(globalDir, opts)
	plan.Kernel = inspectKernel(globalDir, inspectKernelOptions{
		HTTPClient: opts.HTTPClient,
		Runner:     opts.Runner,
		Stat:       opts.Stat,
		Home:       opts.Home,
		LookupEnv:  opts.LookupEnv,
	})
	report.Lines = append(report.Lines, plan.Kernel.Lines...)
	report.checkFileSearchNative(globalDir, opts)
	plan.Lines = report.Lines
	return plan
}

// runKernelUpdateOptions injects side effects for tests. Production callers use
// RunKernelUpdate.
type runKernelUpdateOptions struct {
	HTTPClient *http.Client
	Runner     CommandRunner
	LookPath   func(string) (string, error)
	Stat       func(string) (os.FileInfo, error)
	Home       string
	LookupEnv  func(string) (string, bool)
	// EnsureVenvFunc rebuilds a missing/broken managed venv before the upgrade,
	// mirroring the doctor's checkPythonRuntime repair. Production leaves it nil
	// (a quiet EnsureVenv is used so the TUI alt-screen stays intact).
	EnsureVenvFunc func(string) error
}

// RunKernelUpdate runs ONLY the kernel update path — the equivalent of the
// doctor's checkPythonRuntime → UpgradePythonRuntime step, including the
// venv-repair that step performs when the managed runtime is missing or cannot
// import lingtai. It never touches the TUI binary (no brew) or the file-search
// native sidecar. The existing dev-editable safety in UpgradePythonRuntime is
// preserved unchanged: an editable local checkout is never clobbered, even with
// force=true. This is the single mutating entry point shared by the /update
// command (PR 1) and, later, /doctor's gated heal (PR 2).
func RunKernelUpdate(globalDir string, force bool) DoctorReport {
	return runKernelUpdate(globalDir, force, runKernelUpdateOptions{})
}

func runKernelUpdate(globalDir string, force bool, opts runKernelUpdateOptions) DoctorReport {
	if opts.Runner == nil {
		opts.Runner = execCommandRunner{}
	}
	if opts.Stat == nil {
		opts.Stat = os.Stat
	}
	if opts.EnsureVenvFunc == nil {
		opts.EnsureVenvFunc = func(dir string) error { return EnsureVenvQuiet(dir, nil) }
	}

	report := DoctorReport{Healthy: true}

	// Venv-repair, mirroring checkPythonRuntime: a missing python binary or a
	// venv that cannot import lingtai is rebuilt before the upgrade. Without
	// this, /update would show a confirm prompt, the user would confirm, and
	// UpgradePythonRuntime would only warn "venv not found" — leaving the broken
	// runtime unfixed. The confirmation the user already gave authorizes this.
	python := VenvPython(RuntimeVenvDir(globalDir))
	needsEnsure := false
	if _, err := opts.Stat(python); err != nil {
		report.add(DoctorWarn, "Python runtime venv missing or incomplete at %s", RuntimeVenvDir(globalDir))
		needsEnsure = true
	} else if _, err := pythonLingtaiVersion(opts.Runner, python); err != nil {
		report.add(DoctorWarn, "Python runtime venv exists but cannot import lingtai: %v", err)
		needsEnsure = true
	}
	if needsEnsure {
		report.add(DoctorInfo, "Creating Python runtime venv...")
		if err := opts.EnsureVenvFunc(globalDir); err != nil {
			report.add(DoctorFail, "Failed to create Python runtime venv: %v", err)
			return report
		}
		if _, err := opts.Stat(python); err != nil {
			report.add(DoctorFail, "Python runtime venv was created, but %s is still missing: %v", python, err)
			return report
		}
		report.add(DoctorOK, "Python runtime venv created")
	}

	upgrade := UpgradePythonRuntime(globalDir, force, &UpgradeRuntimeOptions{
		HTTPClient: opts.HTTPClient,
		Runner:     opts.Runner,
		LookPath:   opts.LookPath,
		Stat:       opts.Stat,
		Home:       opts.Home,
		LookupEnv:  opts.LookupEnv,
	})
	for _, line := range upgrade.Lines {
		report.add(line.Severity, "%s", line.Text)
	}
	if !upgrade.Healthy {
		report.Healthy = false
	}
	return report
}
