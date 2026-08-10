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

// assertReleaseWheelInstall is the core contract of this change: the install
// command names a pinned, checksum-verified release wheel URL and never asks
// an index for the package called "lingtai".
func assertReleaseWheelInstall(t *testing.T, name string, args []string) {
	t.Helper()
	call := name + " " + strings.Join(args, " ")
	if !strings.Contains(call, "releases/download/") || !strings.Contains(call, ".whl#sha256=") {
		t.Fatalf("install command must install the pinned release wheel, got %q", call)
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

func TestSelectKernelWheelMatchesPythonAndPlatform(t *testing.T) {
	manifest := mustFetchFixtureManifest(t, "1.0.1")
	cases := []struct {
		python string
		goos   string
		goarch string
		want   string
	}{
		{"cp311", "darwin", "arm64", "lingtai-1.0.1-cp311-cp311-macosx_11_0_arm64.whl"},
		{"cp312", "darwin", "arm64", "lingtai-1.0.1-cp312-cp312-macosx_11_0_arm64.whl"},
		{"cp313", "darwin", "arm64", "lingtai-1.0.1-cp313-cp313-macosx_11_0_arm64.whl"},
		{"cp311", "darwin", "amd64", "lingtai-1.0.1-cp311-cp311-macosx_10_13_x86_64.whl"},
		{"cp313", "darwin", "amd64", "lingtai-1.0.1-cp313-cp313-macosx_10_13_x86_64.whl"},
		{"cp311", "linux", "amd64", "lingtai-1.0.1-cp311-cp311-manylinux_2_17_x86_64.manylinux2014_x86_64.whl"},
		{"cp313", "linux", "amd64", "lingtai-1.0.1-cp313-cp313-manylinux_2_17_x86_64.manylinux2014_x86_64.whl"},
		{"cp312", "linux", "arm64", "lingtai-1.0.1-cp312-cp312-manylinux_2_17_aarch64.manylinux2014_aarch64.whl"},
		{"cp313", "windows", "amd64", "lingtai-1.0.1-cp313-cp313-win_amd64.whl"},
		// The bare version form resolves to the same wheel as the cp form.
		{"3.13", "linux", "amd64", "lingtai-1.0.1-cp313-cp313-manylinux_2_17_x86_64.manylinux2014_x86_64.whl"},
	}
	for _, tc := range cases {
		artifact, err := selectKernelWheel(manifest, tc.python, tc.goos, tc.goarch)
		if err != nil {
			t.Fatalf("selectKernelWheel(%s, %s/%s): %v", tc.python, tc.goos, tc.goarch, err)
		}
		if artifact.Filename != tc.want {
			t.Fatalf("selectKernelWheel(%s, %s/%s) = %q, want %q", tc.python, tc.goos, tc.goarch, artifact.Filename, tc.want)
		}
		if artifact.SHA256 == "" {
			t.Fatalf("selected wheel %q has no sha256", artifact.Filename)
		}
	}
}

func TestSelectKernelWheelErrorsWithoutAMatch(t *testing.T) {
	manifest := mustFetchFixtureManifest(t, "1.0.1")
	cases := []struct {
		name   string
		python string
		goos   string
		goarch string
	}{
		{"unpublished python", "cp310", "linux", "amd64"},
		{"unpublished platform", "cp313", "windows", "arm64"},
		{"unknown platform", "cp313", "plan9", "mips"},
		{"missing python tag", "", "linux", "amd64"},
	}
	for _, tc := range cases {
		if artifact, err := selectKernelWheel(manifest, tc.python, tc.goos, tc.goarch); err == nil {
			t.Fatalf("%s: expected an error, got %q", tc.name, artifact.Filename)
		}
	}
}

func TestVenvPythonTagReadsTheManagedInterpreter(t *testing.T) {
	var probed string
	runner := commandRunnerFunc(func(name string, args ...string) CommandResult {
		probed = name + " " + strings.Join(args, " ")
		return CommandResult{Stdout: "cp313\n"}
	})
	tag, err := venvPythonTag(runner, "/venv/bin/python")
	if err != nil {
		t.Fatalf("venvPythonTag: %v", err)
	}
	if tag != "cp313" {
		t.Fatalf("tag = %q, want cp313", tag)
	}
	if !strings.Contains(probed, "/venv/bin/python") || !strings.Contains(probed, "sys.version_info") {
		t.Fatalf("probe should ask the managed interpreter, got %q", probed)
	}
}

func TestVenvPythonTagRejectsUnusableOutput(t *testing.T) {
	failing := commandRunnerFunc(func(string, ...string) CommandResult {
		return CommandResult{Err: errors.New("exit status 1"), Stderr: "no such file"}
	})
	if _, err := venvPythonTag(failing, "/venv/bin/python"); err == nil {
		t.Fatal("expected an error when the probe fails")
	}
	garbage := commandRunnerFunc(func(string, ...string) CommandResult {
		return CommandResult{Stdout: "ok\n"}
	})
	if _, err := venvPythonTag(garbage, "/venv/bin/python"); err == nil {
		t.Fatal("expected an error for a non cp<major><minor> tag")
	}
}

func TestKernelWheelInstallURLPinsTagAndChecksum(t *testing.T) {
	url := kernelWheelInstallURL("v1.0.1", kernelReleaseArtifact{Filename: "lingtai-1.0.1-cp313-cp313-win_amd64.whl", SHA256: "abc123"})
	want := "https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v1.0.1/lingtai-1.0.1-cp313-cp313-win_amd64.whl#sha256=abc123"
	if url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}
}

func TestKernelInstallCommandUsesReleaseWheelWithUV(t *testing.T) {
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
	assertReleaseWheelInstall(t, name, args)
	call := name + " " + strings.Join(args, " ")
	if !strings.Contains(call, "pip install https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v1.0.1/lingtai-1.0.1-cp313-") {
		t.Fatalf("uv install must pin the v1.0.1 cp313 wheel URL, got %q", call)
	}
	if !strings.Contains(call, "-p "+RuntimeVenvDir(globalDir)) {
		t.Fatalf("uv install must target the managed venv, got %q", call)
	}
	if strings.Contains(call, "--upgrade") {
		t.Fatalf("fresh install must not pass --upgrade, got %q", call)
	}
}

func TestKernelInstallCommandUsesReleaseWheelWithPip(t *testing.T) {
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
	assertReleaseWheelInstall(t, name, args)
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

func TestKernelInstallCommandFailsWhenNoWheelMatchesThisPython(t *testing.T) {
	globalDir := t.TempDir()
	python := VenvPython(RuntimeVenvDir(globalDir))
	client, _ := manifestClient(kernelReleaseManifestFixture("1.0.1"))
	_, _, err := kernelInstallCommand(globalDir, python,
		func(string) (string, error) { return "/usr/bin/uv", nil },
		&fakeRunner{pythonTag: "cp399"}, client, false)
	if err == nil || !strings.Contains(err.Error(), "cp399") {
		t.Fatalf("expected a no-matching-wheel error naming cp399, got %v", err)
	}
}

func TestEnsureVenvInstallCommandNonDevInstallsReleaseWheel(t *testing.T) {
	globalDir := t.TempDir()
	venvPython := VenvPython(RuntimeVenvDir(globalDir))
	home, env := noDevHome(t)
	client, _ := manifestClient(kernelReleaseManifestFixture("1.0.1"))
	name, args, err := ensureVenvInstallCommand(globalDir, venvPython, home,
		func(string) (string, error) { return "/usr/bin/uv", nil }, env, &fakeRunner{}, client)
	if err != nil {
		t.Fatalf("ensureVenvInstallCommand: %v", err)
	}
	assertReleaseWheelInstall(t, name, args)
}

func TestEnsureVenvInstallCommandKeepsDevCheckoutEditable(t *testing.T) {
	globalDir := t.TempDir()
	venvPython := VenvPython(RuntimeVenvDir(globalDir))
	home := t.TempDir()
	devRoot := filepath.Join(home, "devroot")
	kernel := makeKernelCheckout(t, home, "devroot")
	// A dev checkout must never reach the release-wheel path at all: the
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
		t.Fatalf("dev checkout must not install the release wheel, got %q", call)
	}
}

func TestRuntimeUpgradeCommandPinsReleaseWheelWithUpgrade(t *testing.T) {
	globalDir := t.TempDir()
	python := VenvPython(RuntimeVenvDir(globalDir))

	client, _ := manifestClient(kernelReleaseManifestFixture("1.0.1"))
	name, args, err := runtimeUpgradeCommand(globalDir, python,
		func(string) (string, error) { return "/usr/bin/uv", nil }, &fakeRunner{}, client)
	if err != nil {
		t.Fatalf("runtimeUpgradeCommand (uv): %v", err)
	}
	assertReleaseWheelInstall(t, name, args)
	uvCall := name + " " + strings.Join(args, " ")
	if !strings.Contains(uvCall, "pip install --upgrade https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v1.0.1/") {
		t.Fatalf("uv upgrade must pin the release wheel URL, got %q", uvCall)
	}

	client, _ = manifestClient(kernelReleaseManifestFixture("1.0.1"))
	name, args, err = runtimeUpgradeCommand(globalDir, python,
		func(string) (string, error) { return "", errors.New("no uv") }, &fakeRunner{}, client)
	if err != nil {
		t.Fatalf("runtimeUpgradeCommand (pip): %v", err)
	}
	assertReleaseWheelInstall(t, name, args)
	pipCall := name + " " + strings.Join(args, " ")
	if !strings.Contains(pipCall, "install --upgrade https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v1.0.1/") {
		t.Fatalf("pip upgrade must pin the release wheel URL, got %q", pipCall)
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
		t.Fatalf("an unresolvable release wheel must be unhealthy: %+v", result.Lines)
	}
	if result.Updated {
		t.Fatalf("no install ran, so Updated must be false: %+v", result.Lines)
	}
	if !containsLine(result.Lines, "release wheel") {
		t.Fatalf("expected a release-wheel failure line: %+v", result.Lines)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "install") && strings.Contains(call, "lingtai") && !strings.Contains(call, "import lingtai") {
			t.Fatalf("no install command may run when the wheel cannot be resolved: %#v", runner.calls)
		}
	}
}
