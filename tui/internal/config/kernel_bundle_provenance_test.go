package config

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeInstallJSONWithKernelSource(t *testing.T, globalDir, kernelSource string) {
	t.Helper()
	body := `{
  "schema": "lingtai.tui.install/v1",
  "schema_version": 1,
  "install_method": "source",
  "install_kind": "release-asset",
  "prefix": "/usr/local",
  "bin_dir": "/usr/local/bin",
  "repo_url": "https://github.com/Lingtai-AI/lingtai.git",
  "requested_ref": "v0.11.0",
  "resolved_ref": "v0.11.0",
  "resolved_commit": "",
  "stamped_version": "v0.11.0",
  "managed_binaries": ["/usr/local/bin/lingtai-tui"]`
	if kernelSource != "" {
		body += `,
  "kernel_source": "` + kernelSource + `",
  "kernel_bundle_id": "tui-v0.11.0",
  "kernel_version": "0.16.4",
  "kernel_provider": "github"`
	}
	body += "\n}\n"
	if err := os.WriteFile(filepath.Join(globalDir, "install.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write install.json: %v", err)
	}
}

func TestKernelBundleProvenanceMissingFileReportsFalse(t *testing.T) {
	isBundle, _ := kernelBundleProvenance(t.TempDir())
	if isBundle {
		t.Fatalf("missing install.json must report isBundle=false (fail-open to PyPI)")
	}
}

func TestKernelBundleProvenanceLegacyInstallReportsFalse(t *testing.T) {
	globalDir := t.TempDir()
	writeInstallJSONWithKernelSource(t, globalDir, "") // legacy: field absent
	isBundle, _ := kernelBundleProvenance(globalDir)
	if isBundle {
		t.Fatalf("install.json without kernel_source must report isBundle=false")
	}
}

func TestKernelBundleProvenancePyPISourceReportsFalse(t *testing.T) {
	globalDir := t.TempDir()
	writeInstallJSONWithKernelSource(t, globalDir, "pypi")
	isBundle, _ := kernelBundleProvenance(globalDir)
	if isBundle {
		t.Fatalf("kernel_source=pypi must report isBundle=false")
	}
}

func TestKernelBundleProvenanceBundleSourceReportsTrue(t *testing.T) {
	globalDir := t.TempDir()
	writeInstallJSONWithKernelSource(t, globalDir, "bundle")
	isBundle, meta := kernelBundleProvenance(globalDir)
	if !isBundle {
		t.Fatalf("kernel_source=bundle must report isBundle=true")
	}
	if meta.KernelBundleID != "tui-v0.11.0" || meta.KernelVersion != "0.16.4" || meta.KernelProvider != "github" {
		t.Fatalf("unexpected provenance metadata: %+v", meta)
	}
}

// --- UpgradePythonRuntime gate integration ---

func TestUpgradePythonRuntimeSkipsPyPIForBundleProvenance(t *testing.T) {
	globalDir := t.TempDir()
	writeInstallJSONWithKernelSource(t, globalDir, "bundle")
	venvPath := RuntimeVenvDir(globalDir)
	mkdirTestVenv(t, venvPath)

	// Only one version probe queued (the installed-version read); if the gate
	// fails to trip, fetchLatestPyPIVersion/runtimeUpgradeCommand would run
	// next and the fakeRunner would either be starved of a queued version or
	// (worse) actually attempt a mutating install call. HTTPClient panics on
	// any use, proving the gate trips BEFORE any PyPI network call — not just
	// before any mutating command.
	runner := &fakeRunner{versions: []string{"0.16.4"}}
	result := UpgradePythonRuntime(globalDir, false, &UpgradeRuntimeOptions{
		HTTPClient: &http.Client{Transport: panicOnUseRoundTripper{t: t}},
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       t.TempDir(), // no dev checkout
		LookupEnv:  func(string) (string, bool) { return "", false },
	})
	if !result.Healthy {
		t.Fatalf("bundle-provenance skip must remain Healthy: %+v", result.Lines)
	}
	if result.Updated {
		t.Fatalf("bundle-provenance skip must not report Updated=true: %+v", result)
	}
	assertNoMutatingCalls(t, runner.calls)

	foundSkipLine := false
	for _, line := range result.Lines {
		if strings.Contains(line.Text, "pinned release bundle") {
			foundSkipLine = true
		}
	}
	if !foundSkipLine {
		t.Fatalf("expected a doctor line explaining the bundle-provenance skip: %+v", result.Lines)
	}
}

// panicOnUseRoundTripper fails the test immediately if any HTTP request is
// made through it — used to prove a code path makes ZERO network calls,
// not just zero mutating ones.
type panicOnUseRoundTripper struct{ t *testing.T }

func (rt panicOnUseRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.t.Fatalf("unexpected HTTP request for a bundle-provisioned runtime: %s %s", req.Method, req.URL)
	return nil, fmt.Errorf("unreachable")
}

func TestUpgradePythonRuntimeForcedUpdateRespectsBundleProvenance(t *testing.T) {
	// Blocker 2 repair: force=true (doctor / `/update --force`) must NOT
	// reinterpret "force" as "discard the bundle pin and install latest
	// PyPI." A bundle-provisioned runtime must be left alone on a forced
	// check exactly as it is on a routine one — no PyPI query, no kernel
	// upgrade command, no network call at all.
	globalDir := t.TempDir()
	writeInstallJSONWithKernelSource(t, globalDir, "bundle")
	venvPath := RuntimeVenvDir(globalDir)
	mkdirTestVenv(t, venvPath)

	runner := &fakeRunner{versions: []string{"0.16.4"}}
	result := UpgradePythonRuntime(globalDir, true, &UpgradeRuntimeOptions{
		HTTPClient: &http.Client{Transport: panicOnUseRoundTripper{t: t}},
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       t.TempDir(),
		LookupEnv:  func(string) (string, bool) { return "", false },
	})
	if !result.Healthy {
		t.Fatalf("forced update on a bundle-provisioned runtime must remain Healthy: %+v", result.Lines)
	}
	if result.Updated {
		t.Fatalf("forced update on a bundle-provisioned runtime must not report Updated=true: %+v", result)
	}
	assertNoMutatingCalls(t, runner.calls)
	upgrades := 0
	for _, call := range runner.calls {
		if strings.Contains(call, "install --upgrade lingtai") {
			upgrades++
		}
	}
	if upgrades != 0 {
		t.Fatalf("forced update on a bundle-provisioned runtime must run ZERO kernel upgrade commands, got %d (%#v)", upgrades, runner.calls)
	}
	foundPinLine := false
	for _, line := range result.Lines {
		if strings.Contains(line.Text, "does not override this pin") {
			foundPinLine = true
		}
	}
	if !foundPinLine {
		t.Fatalf("expected a doctor line explaining the forced update does not override the bundle pin: %+v", result.Lines)
	}
}

func TestUpgradePythonRuntimeEditableGateTakesPriorityOverBundleProvenance(t *testing.T) {
	// Editable dev installs must still win even if install.json (stale from
	// an earlier bundle install, later hand-converted to editable by the
	// developer) says kernel_source=bundle. The editable gate runs first in
	// UpgradePythonRuntime and must not be shadowed by the new gate.
	globalDir := t.TempDir()
	writeInstallJSONWithKernelSource(t, globalDir, "bundle")
	venvPath := RuntimeVenvDir(globalDir)
	mkdirTestVenv(t, venvPath)

	runner := &fakeRunner{
		versions:       []string{"0.16.4"},
		editableSource: "file:///Users/dev/lingtai-kernel",
	}
	result := UpgradePythonRuntime(globalDir, false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.16.9", "v0.11.0"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       t.TempDir(),
		LookupEnv:  func(string) (string, bool) { return "", false },
	})
	if !result.Healthy {
		t.Fatalf("expected Healthy: %+v", result.Lines)
	}
	assertNoMutatingCalls(t, runner.calls)
	foundEditableLine := false
	for _, line := range result.Lines {
		if strings.Contains(line.Text, "editable install") {
			foundEditableLine = true
		}
	}
	if !foundEditableLine {
		t.Fatalf("expected the editable-install skip line to win, got: %+v", result.Lines)
	}
}

func TestUpgradePythonRuntimeLegacyNoProvenanceStillComparesToPyPI(t *testing.T) {
	// Explicit documentation-by-test of the legacy migration path (Blocker 2
	// repair, requirement 4): a runtime with NO kernel_source metadata at
	// all — installed before install.sh's bundle path existed, or never
	// updated through it — is NOT bundle-provisioned, so the pre-existing
	// PyPI-compare/upgrade behavior is intentionally left unchanged for it.
	// This is legacy migration behavior, not a claim that PyPI is the
	// product's canonical install source going forward (every NEW install
	// goes through install.sh's mandatory bundle path instead).
	globalDir := t.TempDir()
	writeInstallJSONWithKernelSource(t, globalDir, "") // no kernel_source field at all
	venvPath := RuntimeVenvDir(globalDir)
	mkdirTestVenv(t, venvPath)

	runner := &fakeRunner{versions: []string{"0.16.4", "0.16.9"}}
	result := UpgradePythonRuntime(globalDir, false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.16.9", "v0.11.0"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       t.TempDir(),
		LookupEnv:  func(string) (string, bool) { return "", false },
	})
	if !result.Healthy {
		t.Fatalf("legacy no-provenance upgrade must remain Healthy: %+v", result.Lines)
	}
	if !result.Updated {
		t.Fatalf("legacy no-provenance install with a newer PyPI version must still upgrade: %+v", result)
	}
	upgrades := 0
	for _, call := range runner.calls {
		if strings.Contains(call, "install --upgrade lingtai") {
			upgrades++
		}
	}
	if upgrades != 1 {
		t.Fatalf("legacy no-provenance path must run exactly one kernel upgrade command (unchanged pre-existing behavior), got %d (%#v)", upgrades, runner.calls)
	}
}

func TestUpgradePythonRuntimeLegacyPyPISourceValueStillComparesToPyPI(t *testing.T) {
	// A hypothetical/pre-repair install.json carrying kernel_source=="pypi"
	// (this script no longer writes that value, but an old install.json on
	// disk could still have it from before this repair) must be treated
	// exactly like no-provenance metadata: not bundle-provisioned, so the
	// legacy PyPI-compare path still applies. Only kernel_source=="bundle"
	// trips the gate.
	globalDir := t.TempDir()
	writeInstallJSONWithKernelSource(t, globalDir, "pypi")
	venvPath := RuntimeVenvDir(globalDir)
	mkdirTestVenv(t, venvPath)

	runner := &fakeRunner{versions: []string{"0.16.4", "0.16.9"}}
	result := UpgradePythonRuntime(globalDir, false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.16.9", "v0.11.0"),
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       t.TempDir(),
		LookupEnv:  func(string) (string, bool) { return "", false },
	})
	if !result.Updated {
		t.Fatalf("kernel_source=pypi metadata must not trip the bundle gate: %+v", result)
	}
}
