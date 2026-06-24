package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
)

func TestStartupSourceUpdateDeclineDoesNotMutate(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	metadataPath := writeStartupSourceInstallMetadata(t, globalDir, prefix, binDir, "v0.8.0")
	runner := &startupFakeRunner{}

	var out, errOut bytes.Buffer
	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method:       config.TUIInstallMethodSource,
		Detail:       "metadata at " + metadataPath,
		MetadataPath: metadataPath,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("\n"),
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
		GlobalDir: globalDir,
	})

	if updated {
		t.Fatal("declined source startup update should not stop startup")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("declined source startup update should not run commands, got %#v", runner.calls)
	}
	if !strings.Contains(out.String(), "Update this source install now? [y/N]") {
		t.Fatalf("missing source confirmation prompt:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "lingtai-tui self-update") {
		t.Fatalf("missing manual self-update guidance:\n%s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("decline should not write stderr, got %q", errOut.String())
	}
}

func TestStartupSourceUpdateConfirmRoutesThroughSourceUpdater(t *testing.T) {
	globalDir := t.TempDir()
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	metadataPath := writeStartupSourceInstallMetadata(t, globalDir, prefix, binDir, "v0.8.0")
	runner := &startupSourceUpdateRunner{
		t:         t,
		globalDir: globalDir,
		prefix:    prefix,
		binDir:    binDir,
		latest:    "v0.8.1",
	}

	var out, errOut bytes.Buffer
	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method:       config.TUIInstallMethodSource,
		Detail:       "metadata at " + metadataPath,
		MetadataPath: metadataPath,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:               strings.NewReader("yes\n"),
		Output:              &out,
		ErrOutput:           &errOut,
		Runner:              runner,
		GlobalDir:           globalDir,
		Stat:                statMissingForStartupTest,
		SourceInstallScript: "/tmp/install.sh",
	})

	if !updated {
		t.Fatalf("confirmed source startup update should stop startup after update; stderr=%q output=\n%s", errOut.String(), out.String())
	}
	if !startupContainsCall(runner.calls, "bash /tmp/install.sh --update --prefix "+prefix+" --version v0.8.1 --non-interactive") {
		t.Fatalf("expected source installer update call, got %#v", runner.calls)
	}
	if startupContainsCall(runner.calls, "brew") {
		t.Fatalf("source startup update must not run brew, got %#v", runner.calls)
	}
	if !strings.Contains(out.String(), "Source/user-local TUI update verified") {
		t.Fatalf("missing source update verification output:\n%s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("successful source startup update should not write stderr, got %q", errOut.String())
	}
}

func TestStartupHomebrewUpdateKeepsExistingDefaultYesBehavior(t *testing.T) {
	runner := &startupFakeRunner{}
	var out, errOut bytes.Buffer

	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method: config.TUIInstallMethodHomebrew,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("\n"),
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
		CheckTUIUpgrade: func(string) string {
			return ""
		},
		FindOtherTUIProcesses: func() []runningTUIProcess {
			return nil
		},
	})

	if !updated {
		t.Fatalf("homebrew default-yes startup update should stop startup; stderr=%q output=\n%s", errOut.String(), out.String())
	}
	if len(runner.calls) != 1 || runner.calls[0] != "brew upgrade lingtai-ai/lingtai/lingtai-tui" {
		t.Fatalf("expected one brew upgrade call, got %#v", runner.calls)
	}
	if !strings.Contains(out.String(), "Upgrade now? [Y/n]") {
		t.Fatalf("missing existing Homebrew prompt:\n%s", out.String())
	}
}

func TestStartupUnknownInstallDoesNotMutate(t *testing.T) {
	runner := &startupFakeRunner{}
	var out, errOut bytes.Buffer

	updated := handleTUIUpgradeWithOptions(config.TUIInstallInfo{
		Method: config.TUIInstallMethodUnknown,
	}, "v0.8.0", "v0.8.1", startupTUIUpgradeOptions{
		Input:     strings.NewReader("yes\n"),
		Output:    &out,
		ErrOutput: &errOut,
		Runner:    runner,
	})

	if updated {
		t.Fatal("unknown startup update should never stop startup as updated")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unknown startup update should not run commands, got %#v", runner.calls)
	}
	if strings.TrimSpace(out.String()) != "lingtai-tui v0.8.0" {
		t.Fatalf("unknown install should preserve version-only startup output, got %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unknown install should not write stderr, got %q", errOut.String())
	}
}

type startupFakeRunner struct {
	calls []string
}

func (r *startupFakeRunner) Run(name string, args ...string) config.CommandResult {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return config.CommandResult{}
}

type startupSourceUpdateRunner struct {
	t         *testing.T
	globalDir string
	prefix    string
	binDir    string
	latest    string
	calls     []string
}

func (r *startupSourceUpdateRunner) Run(name string, args ...string) config.CommandResult {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	switch {
	case strings.Contains(call, "--update"):
		writeStartupSourceInstallMetadata(r.t, r.globalDir, r.prefix, r.binDir, r.latest)
		return config.CommandResult{Stdout: "updated\n"}
	case strings.HasSuffix(call, "lingtai-tui version"):
		return config.CommandResult{Stdout: "lingtai-tui " + r.latest + "\n"}
	default:
		return config.CommandResult{}
	}
}

func writeStartupSourceInstallMetadata(t *testing.T, globalDir, prefix, binDir, version string) string {
	t.Helper()
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(globalDir, "install.json")
	exe := filepath.Join(binDir, "lingtai-tui")
	meta := map[string]interface{}{
		"schema":           "lingtai.tui.install/v1",
		"schema_version":   1,
		"install_method":   "source",
		"prefix":           prefix,
		"bin_dir":          binDir,
		"stamped_version":  version,
		"managed_binaries": []string{exe},
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return metadataPath
}

func statMissingForStartupTest(string) (os.FileInfo, error) {
	return nil, os.ErrNotExist
}

func startupContainsCall(calls []string, sub string) bool {
	for _, call := range calls {
		if strings.Contains(call, sub) {
			return true
		}
	}
	return false
}
