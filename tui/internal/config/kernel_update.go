package config

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
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
	LookPath   func(string) (string, error)
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
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
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

// runKernelUpdateOptions injects side effects for tests. Production callers use
// RunKernelUpdate.
type runKernelUpdateOptions struct {
	HTTPClient *http.Client
	Runner     CommandRunner
	LookPath   func(string) (string, error)
	Stat       func(string) (os.FileInfo, error)
	Home       string
	LookupEnv  func(string) (string, bool)
}

// RunKernelUpdate runs ONLY the kernel update path — the equivalent of the
// doctor's checkPythonRuntime → UpgradePythonRuntime step. It never touches the
// TUI binary (no brew) or the file-search native sidecar. The existing
// dev-editable safety in UpgradePythonRuntime is preserved unchanged: an
// editable local checkout is never clobbered, even with force=true. This is
// the single mutating entry point shared by the /update command (PR 1) and,
// later, /doctor's gated heal (PR 2).
func RunKernelUpdate(globalDir string, force bool) DoctorReport {
	return runKernelUpdate(globalDir, force, runKernelUpdateOptions{})
}

func runKernelUpdate(globalDir string, force bool, opts runKernelUpdateOptions) DoctorReport {
	report := DoctorReport{Healthy: true}
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
