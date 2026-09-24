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
  "schema":"lingtai.tui.install/v1", "schema_version":1,
  "install_method":"source", "install_kind":"source-build",
  "prefix":"/usr/local", "bin_dir":"/usr/local/bin",
  "repo_url":"https://github.com/Lingtai-AI/lingtai",
  "requested_ref":"v0.11.0", "resolved_ref":"v0.11.0",
  "resolved_commit":"", "stamped_version":"v0.11.0",
  "managed_binaries":["/usr/local/bin/lingtai-tui"]`
	if kernelSource != "" {
		body += `,
  "kernel_source":"` + kernelSource + `",
  "kernel_version":"0.16.4",
  "kernel_provider":"mirror"`
	}
	body += "\n}\n"
	if err := os.WriteFile(filepath.Join(globalDir, "install.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write install.json: %v", err)
	}
}

func writeMainInstallJSON(t *testing.T, globalDir string) {
	t.Helper()
	body := `{
  "schema":"lingtai.tui.install/v1", "schema_version":1,
  "install_method":"powershell", "install_kind":"powershell-latest-main",
  "prefix":"C:\\LingTai", "bin_dir":"C:\\LingTai\\bin",
  "repo_url":"https://github.com/Lingtai-AI/lingtai",
  "requested_ref":"main", "resolved_ref":"main",
  "resolved_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "stamped_version":"main-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "managed_binaries":["C:\\LingTai\\bin\\lingtai-tui.exe"],
  "source_mode":"latest-main", "tui_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "kernel_source":"main", "kernel_commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "kernel_version":"0.0.0.dev0", "kernel_provider":"github"
}`
	if err := os.WriteFile(filepath.Join(globalDir, "install.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write main install.json: %v", err)
	}
}

type panicOnUseRoundTripper struct{ t *testing.T }

func (rt panicOnUseRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.t.Fatalf("unexpected HTTP request: %s %s", req.Method, req.URL)
	return nil, fmt.Errorf("unreachable")
}

func TestKernelMainProvenanceRecognizesPinnedCommits(t *testing.T) {
	globalDir := t.TempDir()
	writeMainInstallJSON(t, globalDir)
	isMain, meta := kernelMainProvenance(globalDir)
	if !isMain || meta.TuiCommit == "" || meta.KernelCommit == "" {
		t.Fatalf("expected current-main provenance, got isMain=%v meta=%+v", isMain, meta)
	}
}

func TestKernelMainProvenanceRejectsMalformedCommitMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, tui, kernel string
	}{
		{name: "empty", tui: "", kernel: ""},
		{name: "tui-short", tui: strings.Repeat("a", 39), kernel: strings.Repeat("b", 40)},
		{name: "tui-long", tui: strings.Repeat("a", 41), kernel: strings.Repeat("b", 40)},
		{name: "tui-nonhex", tui: strings.Repeat("a", 39) + "g", kernel: strings.Repeat("b", 40)},
		{name: "kernel-short", tui: strings.Repeat("a", 40), kernel: strings.Repeat("b", 39)},
		{name: "kernel-long", tui: strings.Repeat("a", 40), kernel: strings.Repeat("b", 41)},
		{name: "kernel-nonhex", tui: strings.Repeat("a", 40), kernel: strings.Repeat("b", 39) + "g"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			globalDir := t.TempDir()
			writeMainInstallJSON(t, globalDir)
			path := filepath.Join(globalDir, "install.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Replace(string(raw), `"tui_commit":"`+strings.Repeat("a", 40)+`"`, `"tui_commit":"`+tc.tui+`"`, 1)
			text = strings.Replace(text, `"kernel_commit":"`+strings.Repeat("b", 40)+`"`, `"kernel_commit":"`+tc.kernel+`"`, 1)
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			if isMain, _ := kernelMainProvenance(globalDir); isMain {
				t.Fatalf("malformed commit metadata accepted: tui=%q kernel=%q", tc.tui, tc.kernel)
			}
		})
	}
}

func TestUpgradePythonRuntimeRoutineSkipsReleaseCheckForMainProvenance(t *testing.T) {
	globalDir := t.TempDir()
	writeMainInstallJSON(t, globalDir)
	venvPath := RuntimeVenvDir(globalDir)
	mkdirTestVenv(t, venvPath)
	runner := &fakeRunner{versions: []string{"0.0.0.dev0"}}
	result := UpgradePythonRuntime(globalDir, false, &UpgradeRuntimeOptions{
		HTTPClient: &http.Client{Transport: panicOnUseRoundTripper{t: t}},
		Runner:     runner, LookPath: func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat: statAllExist, Home: t.TempDir(), LookupEnv: func(string) (string, bool) { return "", false },
	})
	if !result.Healthy || result.Updated {
		t.Fatalf("main provenance must skip release mutation: healthy=%v updated=%v calls=%v lines=%+v", result.Healthy, result.Updated, runner.calls, result.Lines)
	}
	assertNoMutatingCalls(t, runner.calls)
}

func TestUpgradePythonRuntimeForcedSkipsReleaseCheckForMainProvenance(t *testing.T) {
	globalDir := t.TempDir()
	writeMainInstallJSON(t, globalDir)
	venvPath := RuntimeVenvDir(globalDir)
	mkdirTestVenv(t, venvPath)
	runner := &fakeRunner{versions: []string{"0.0.0.dev0"}}
	result := UpgradePythonRuntime(globalDir, true, &UpgradeRuntimeOptions{
		HTTPClient: &http.Client{Transport: panicOnUseRoundTripper{t: t}},
		Runner:     runner, LookPath: func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat: statAllExist, Home: t.TempDir(), LookupEnv: func(string) (string, bool) { return "", false },
	})
	if !result.Healthy || result.Updated {
		t.Fatalf("forced main provenance must skip release mutation: healthy=%v updated=%v calls=%v lines=%+v", result.Healthy, result.Updated, runner.calls, result.Lines)
	}
	assertNoMutatingCalls(t, runner.calls)
}

func TestReleaseProvenanceRemainsIndependentlyUpgradeable(t *testing.T) {
	globalDir := t.TempDir()
	writeInstallJSONWithKernelSource(t, globalDir, "release")
	venvPath := RuntimeVenvDir(globalDir)
	mkdirTestVenv(t, venvPath)
	runner := &fakeRunner{versions: []string{"0.16.4", "0.16.9"}}
	result := UpgradePythonRuntime(globalDir, false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.16.9", "v0.11.0"),
		Runner:     runner, LookPath: func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat: statAllExist, Home: t.TempDir(), LookupEnv: func(string) (string, bool) { return "", false },
	})
	if !result.Healthy || !result.Updated {
		t.Fatalf("release kernel should update independently: %+v calls=%v", result.Lines, runner.calls)
	}
	var upgrades int
	for _, call := range runner.calls {
		if strings.Contains(call, "releases/download/") && strings.Contains(call, ".tar.gz#sha256=") {
			upgrades++
		}
	}
	if upgrades != 1 {
		t.Fatalf("expected one verified kernel release upgrade, got %d (%#v)", upgrades, runner.calls)
	}
}

func TestEditableGateTakesPriorityOverReleaseProvenance(t *testing.T) {
	globalDir := t.TempDir()
	writeInstallJSONWithKernelSource(t, globalDir, "release")
	venvPath := RuntimeVenvDir(globalDir)
	mkdirTestVenv(t, venvPath)
	runner := &fakeRunner{versions: []string{"0.16.4"}, editableSource: "file:///Users/dev/lingtai-kernel"}
	result := UpgradePythonRuntime(globalDir, false, &UpgradeRuntimeOptions{
		HTTPClient: testVersionClient(t, "0.16.9", "v0.11.0"),
		Runner:     runner, LookPath: func(string) (string, error) { return "/usr/bin/uv", nil },
		Stat: statAllExist, Home: t.TempDir(), LookupEnv: func(string) (string, bool) { return "", false },
	})
	if !result.Healthy {
		t.Fatalf("expected Healthy: %+v", result.Lines)
	}
	assertNoMutatingCalls(t, runner.calls)
	for _, line := range result.Lines {
		if strings.Contains(line.Text, "editable install") {
			return
		}
	}
	t.Fatalf("expected editable-install skip line, got: %+v", result.Lines)
}
