package config

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// kernelReleaseManifestFixture renders a manifest with the same shape the
// kernel release workflow publishes: one wheel per (CPython, platform) pair,
// the sdist fallback entry, plus an artifact kind the installer must ignore.
func kernelReleaseManifestFixture(version string) string {
	platforms := []string{
		"macosx_10_13_x86_64",
		"macosx_11_0_arm64",
		"manylinux_2_17_aarch64.manylinux2014_aarch64",
		"manylinux_2_17_x86_64.manylinux2014_x86_64",
		"win_amd64",
	}
	var entries []string
	n := 0
	for _, python := range []string{"cp311", "cp312", "cp313"} {
		for _, platform := range platforms {
			n++
			entries = append(entries, fmt.Sprintf(
				`{"filename":"lingtai-%s-%s-%s-%s.whl","sha256":"%064x","kind":"wheel","python_tag":%q,"abi_tag":%q,"platform_tag":%q}`,
				version, python, python, platform, n, python, python, platform))
		}
	}
	entries = append(entries, fmt.Sprintf(
		`{"filename":"lingtai-%s.tar.gz","sha256":"%064x","kind":"sdist","python_tag":null,"abi_tag":null,"platform_tag":null}`,
		version, 999))
	entries = append(entries, fmt.Sprintf(
		`{"filename":"lingtai-%s-debug-symbols.zip","sha256":"%064x","kind":"debug","python_tag":null,"abi_tag":null,"platform_tag":null}`,
		version, 1000))
	return fmt.Sprintf(`{
  "schema": "lingtai.kernel.release/v1",
  "kernel_version": %q,
  "kernel_tag": "v%s",
  "commit": "18fb812d2b575ecc82213d39491a53ac82f9e62b",
  "generated_at": "2026-08-10T15:22:26Z",
  "artifacts": [%s],
  "sdist_fallback": "lingtai-%s.tar.gz"
}`, version, version, strings.Join(entries, ","), version)
}

// kernelManifestRoundTripper serves exactly one canned response for the
// manifest URL and records every request, so tests can prove the installer
// never reaches for an index instead.
type kernelManifestRoundTripper struct {
	status   int
	body     string
	err      error
	requests []string
}

func (rt *kernelManifestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.requests = append(rt.requests, req.URL.String())
	if rt.err != nil {
		return nil, rt.err
	}
	status := rt.status
	if status == 0 {
		status = 200
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(rt.body)),
	}, nil
}

func manifestClient(body string) (*http.Client, *kernelManifestRoundTripper) {
	rt := &kernelManifestRoundTripper{body: body}
	return &http.Client{Transport: rt}, rt
}

func mustFetchFixtureManifest(t *testing.T, version string) *kernelReleaseManifest {
	t.Helper()
	client, _ := manifestClient(kernelReleaseManifestFixture(version))
	manifest, err := fetchKernelReleaseManifest(client)
	if err != nil {
		t.Fatalf("fetch fixture manifest: %v", err)
	}
	return manifest
}

// assertReleaseSourceInstall is the core contract of this change: the install
// command names a pinned, checksum-verified release source URL and never asks
// an index for the package called "lingtai".
func assertReleaseSourceInstall(t *testing.T, name string, args []string) {
	t.Helper()
	call := name + " " + strings.Join(args, " ")
	if !strings.Contains(call, "releases/download/") || !strings.Contains(call, ".tar.gz#sha256=") {
		t.Fatalf("install command must install the pinned release source, got %q", call)
	}
	for _, arg := range args {
		if arg == "lingtai" {
			t.Fatalf("install command must never pass the bare package name to an index, got %q", call)
		}
	}
}

func TestFetchKernelReleaseManifestDecodesReleaseFields(t *testing.T) {
	client, rt := manifestClient(kernelReleaseManifestFixture("1.0.1"))
	manifest, err := fetchKernelReleaseManifest(client)
	if err != nil {
		t.Fatalf("fetch manifest: %v", err)
	}
	if len(rt.requests) != 1 || rt.requests[0] != kernelReleaseManifestURL {
		t.Fatalf("expected exactly one GET of %q, got %#v", kernelReleaseManifestURL, rt.requests)
	}
	if manifest.KernelVersion != "1.0.1" || manifest.KernelTag != "v1.0.1" {
		t.Fatalf("kernel version/tag = %q/%q, want 1.0.1/v1.0.1", manifest.KernelVersion, manifest.KernelTag)
	}
	if manifest.SdistFallback != "lingtai-1.0.1.tar.gz" {
		t.Fatalf("sdist_fallback = %q", manifest.SdistFallback)
	}
	// 15 wheels + the sdist fallback; the "debug" artifact is dropped.
	if len(manifest.Artifacts) != 16 {
		t.Fatalf("artifacts = %d, want 16 (15 wheels + sdist fallback): %#v", len(manifest.Artifacts), manifest.Artifacts)
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == "debug" {
			t.Fatalf("non-wheel, non-sdist artifacts must be dropped: %#v", artifact)
		}
	}
}

func TestFetchKernelReleaseManifestReportsHTTPError(t *testing.T) {
	client := &http.Client{Transport: &kernelManifestRoundTripper{status: 500, body: "boom"}}
	if _, err := fetchKernelReleaseManifest(client); err == nil {
		t.Fatal("expected an error for HTTP 500")
	} else if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error should carry the status and body, got %v", err)
	}
}

func TestFetchKernelReleaseManifestRejectsUnknownSchema(t *testing.T) {
	client, _ := manifestClient(`{"schema":"lingtai.kernel.release/v2","kernel_tag":"v9.9.9"}`)
	_, err := fetchKernelReleaseManifest(client)
	if err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("expected a schema error, got %v", err)
	}
}

func TestKernelSourceArtifactRequiresMatchingNameAndChecksum(t *testing.T) {
	manifest := mustFetchFixtureManifest(t, "1.0.1")
	artifact, err := kernelSourceArtifact(manifest)
	if err != nil || artifact.Filename != "lingtai-1.0.1.tar.gz" {
		t.Fatalf("source artifact = %+v, %v", artifact, err)
	}
	manifest.Artifacts[len(manifest.Artifacts)-1].SHA256 = "not-a-digest"
	if _, err := kernelSourceArtifact(manifest); err == nil {
		t.Fatal("malformed source checksum must fail")
	}
}

func TestKernelReleaseArtifactURLPinsTagAndChecksum(t *testing.T) {
	url := kernelReleaseArtifactURL("v1.0.1", kernelReleaseArtifact{Filename: "lingtai-1.0.1.tar.gz", SHA256: "abc123"})
	want := "https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v1.0.1/lingtai-1.0.1.tar.gz#sha256=abc123"
	if url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}
}

func TestKernelInstallCommandUsesReleaseSourceWithUV(t *testing.T) {
	globalDir := t.TempDir()
	python := VenvPython(RuntimeVenvDir(globalDir))
	client, _ := manifestClient(kernelReleaseManifestFixture("1.0.1"))
	name, args, err := kernelInstallCommand(globalDir, python,
		func(string) (string, error) { return "/usr/bin/uv", nil },
		&fakeRunner{}, client, false)
	if err != nil {
		t.Fatalf("kernelInstallCommand: %v", err)
	}
	if name != "/usr/bin/uv" {
		t.Fatalf("name = %q, want the uv binary", name)
	}
	assertReleaseSourceInstall(t, name, args)
	call := name + " " + strings.Join(args, " ")
	if !strings.Contains(call, "pip install https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v1.0.1/lingtai-1.0.1.tar.gz#sha256=") {
		t.Fatalf("uv install must pin the v1.0.1 source archive URL, got %q", call)
	}
	if !strings.Contains(call, "-p "+RuntimeVenvDir(globalDir)) {
		t.Fatalf("uv install must target the managed venv, got %q", call)
	}
	if strings.Contains(call, "--upgrade") {
		t.Fatalf("fresh install must not pass --upgrade, got %q", call)
	}
}

func TestKernelInstallCommandUsesReleaseSourceWithPip(t *testing.T) {
	globalDir := t.TempDir()
	python := VenvPython(RuntimeVenvDir(globalDir))
	client, _ := manifestClient(kernelReleaseManifestFixture("1.0.1"))
	name, args, err := kernelInstallCommand(globalDir, python,
		func(string) (string, error) { return "", errors.New("no uv") },
		&fakeRunner{}, client, false)
	if err != nil {
		t.Fatalf("kernelInstallCommand: %v", err)
	}
	wantPip := filepath.Join(filepath.Dir(python), "pip")
	if runtime.GOOS == "windows" {
		wantPip = filepath.Join(filepath.Dir(python), "pip.exe")
	}
	if name != wantPip {
		t.Fatalf("name = %q, want the venv pip %q", name, wantPip)
	}
	if args[0] != "install" {
		t.Fatalf("first pip arg = %q, want install", args[0])
	}
	assertReleaseSourceInstall(t, name, args)
}

func TestKernelInstallCommandFailsInsteadOfFallingBackToPyPI(t *testing.T) {
	globalDir := t.TempDir()
	python := VenvPython(RuntimeVenvDir(globalDir))
	client := &http.Client{Transport: &kernelManifestRoundTripper{status: 404, body: "Not Found"}}
	name, args, err := kernelInstallCommand(globalDir, python,
		func(string) (string, error) { return "/usr/bin/uv", nil },
		&fakeRunner{}, client, false)
	if err == nil {
		t.Fatalf("unreachable manifest must be an error, got command %q %v", name, args)
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("error should name the manifest, got %v", err)
	}
	if name != "" || args != nil {
		t.Fatalf("failed resolution must not return a command, got %q %v", name, args)
	}
}

func TestKernelInstallCommandFailsWhenNoVerifiedSourceExists(t *testing.T) {
	globalDir := t.TempDir()
	python := VenvPython(RuntimeVenvDir(globalDir))
	manifest := strings.Replace(kernelReleaseManifestFixture("1.0.1"), `"sdist_fallback": "lingtai-1.0.1.tar.gz"`, `"sdist_fallback": ""`, 1)
	client, _ := manifestClient(manifest)
	_, _, err := kernelInstallCommand(globalDir, python,
		func(string) (string, error) { return "/usr/bin/uv", nil },
		&fakeRunner{pythonTag: "cp399"}, client, false)
	if err == nil || !strings.Contains(err.Error(), "source archive") {
		t.Fatalf("expected a missing source archive error, got %v", err)
	}
}

func TestKernelInstallCommandAcceptsV108GenericWheelRelease(t *testing.T) {
	// v1.0.8 publishes py3-any and the verified source archive, but no cp313
	// macOS wheel. This is the release that left doctor unable to repair.
	manifest := `{"schema":"lingtai.kernel.release/v1","kernel_version":"1.0.8","kernel_tag":"v1.0.8","sdist_fallback":"lingtai-1.0.8.tar.gz","artifacts":[{"filename":"lingtai-1.0.8-py3-none-any.whl","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","kind":"wheel","python_tag":"py3","abi_tag":"none","platform_tag":"any"},{"filename":"lingtai-1.0.8.tar.gz","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","kind":"sdist","python_tag":null,"abi_tag":null,"platform_tag":null}]}`
	client, _ := manifestClient(manifest)
	globalDir := t.TempDir()
	name, args, err := kernelInstallCommand(globalDir, VenvPython(RuntimeVenvDir(globalDir)),
		func(string) (string, error) { return "/usr/bin/uv", nil },
		&fakeRunner{pythonTag: "cp313"}, client, true)
	if err != nil {
		t.Fatalf("generic-wheel release must be repairable: %v", err)
	}
	assertReleaseSourceInstall(t, name, args)
	if !strings.Contains(strings.Join(args, " "), "lingtai-1.0.8.tar.gz#sha256=bbbb") {
		t.Fatalf("doctor must select the verified source archive: %v", args)
	}
}

func TestEnsureVenvInstallCommandNonDevInstallsReleaseSource(t *testing.T) {
	globalDir := t.TempDir()
	venvPython := VenvPython(RuntimeVenvDir(globalDir))
	home, env := noDevHome(t)
	client, _ := manifestClient(kernelReleaseManifestFixture("1.0.1"))
	name, args, err := ensureVenvInstallCommand(globalDir, venvPython, home,
		func(string) (string, error) { return "/usr/bin/uv", nil }, env, &fakeRunner{}, client)
	if err != nil {
		t.Fatalf("ensureVenvInstallCommand: %v", err)
	}
	assertReleaseSourceInstall(t, name, args)
}

func TestEnsureVenvInstallCommandKeepsDevCheckoutEditable(t *testing.T) {
	globalDir := t.TempDir()
	venvPython := VenvPython(RuntimeVenvDir(globalDir))
	home := t.TempDir()
	devRoot := filepath.Join(home, "devroot")
	kernel := makeKernelCheckout(t, home, "devroot")
	// A dev checkout must never reach the release-source path at all: the
	// manifest client here would fail the test if it were consulted.
	client := &http.Client{Transport: &kernelManifestRoundTripper{err: errors.New("dev checkout must not fetch the release manifest")}}

	name, args, err := ensureVenvInstallCommand(globalDir, venvPython, home,
		func(string) (string, error) { return "/usr/bin/uv", nil }, devEnvLookup(devRoot), &fakeRunner{}, client)
	if err != nil {
		t.Fatalf("ensureVenvInstallCommand (dev): %v", err)
	}
	call := name + " " + strings.Join(args, " ")
	if !strings.Contains(call, "pip install -e "+kernel) {
		t.Fatalf("dev checkout must install editable, got %q", call)
	}
	if strings.Contains(call, "releases/download/") {
		t.Fatalf("dev checkout must not install the release source, got %q", call)
	}
}

func TestRuntimeUpgradeCommandPinsReleaseSourceWithUpgrade(t *testing.T) {
	globalDir := t.TempDir()
	python := VenvPython(RuntimeVenvDir(globalDir))

	client, _ := manifestClient(kernelReleaseManifestFixture("1.0.1"))
	name, args, err := runtimeUpgradeCommand(globalDir, python,
		func(string) (string, error) { return "/usr/bin/uv", nil }, &fakeRunner{}, client)
	if err != nil {
		t.Fatalf("runtimeUpgradeCommand (uv): %v", err)
	}
	assertReleaseSourceInstall(t, name, args)
	uvCall := name + " " + strings.Join(args, " ")
	if !strings.Contains(uvCall, "pip install --upgrade https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v1.0.1/") {
		t.Fatalf("uv upgrade must pin the release source URL, got %q", uvCall)
	}

	client, _ = manifestClient(kernelReleaseManifestFixture("1.0.1"))
	name, args, err = runtimeUpgradeCommand(globalDir, python,
		func(string) (string, error) { return "", errors.New("no uv") }, &fakeRunner{}, client)
	if err != nil {
		t.Fatalf("runtimeUpgradeCommand (pip): %v", err)
	}
	assertReleaseSourceInstall(t, name, args)
	pipCall := name + " " + strings.Join(args, " ")
	if !strings.Contains(pipCall, "install --upgrade https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v1.0.1/") {
		t.Fatalf("pip upgrade must pin the release source URL, got %q", pipCall)
	}
}

func TestRuntimeUpgradeCommandFailureIsReportedNotSwallowed(t *testing.T) {
	globalDir := t.TempDir()
	runner := &fakeRunner{versions: []string{"0.9.6"}}
	home, env := noDevHome(t)
	result := UpgradePythonRuntime(globalDir, true, &UpgradeRuntimeOptions{
		HTTPClient: &http.Client{Transport: &kernelManifestRoundTripper{status: 503, body: "unavailable"}},
		Runner:     runner,
		LookPath:   func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat:       statAllExist,
		Home:       home,
		LookupEnv:  env,
	})
	if result.Healthy {
		t.Fatalf("an unresolvable release source must be unhealthy: %+v", result.Lines)
	}
	if result.Updated {
		t.Fatalf("no install ran, so Updated must be false: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "release source archive") {
		t.Fatalf("expected a release-source failure line: %+v", result.Lines)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "install") && strings.Contains(call, "lingtai") && !strings.Contains(call, "import lingtai") {
			t.Fatalf("no install command may run when the source cannot be resolved: %#v", runner.calls)
		}
	}
}
