package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateAddonKey(t *testing.T) {
	valid := []string{
		"imap", "telegram", "wechat", "whatsapp", "feishu",
		"_private", "a1", "x_y", "imap.telegram", "a.b.c", "x2.y_3",
	}
	for _, name := range valid {
		if err := ValidateAddonKey(name); err != nil {
			t.Errorf("ValidateAddonKey(%q) = %v, want nil", name, err)
		}
	}

	// Every one of these must be rejected BEFORE it could be interpolated
	// into generated Python import source. Semicolons, newlines, quotes,
	// comment markers, whitespace, path separators, leading digits, empty
	// names, and stray punctuation are all source-boundary/non-identifier
	// characters that could alter the constructed program.
	invalid := []string{
		"",
		"imap;rm",
		";",
		"imap\nprint(1)",
		"imap\r\nimport os",
		"imap#comment",
		"imap x",
		" imap",
		"imap ",
		"imap\t",
		"imap\"x",
		"imap'x",
		"imap\\x",
		"imap/x",
		"imap(x)",
		"imap$x",
		"imap~x",
		"1imap",
		"-imap",
		".imap",
		"imap.",
		"imap..x",
		"imap\u00e9",
		"__import__('os')",
		"os.system('ls')",
		"import os; os.system('id')",
	}
	for _, name := range invalid {
		if err := ValidateAddonKey(name); err == nil {
			t.Errorf("ValidateAddonKey(%q) = nil, want validation error", name)
		}
	}
}

// writeStubPython writes a POSIX shell stub that records every invocation
// (one argument per line) to logPath and then exits with the given status.
// It lets tests prove whether an interpreter process is (or is not) started
// during addon validation without running a real Python.
func writeStubPython(t *testing.T, dir, logPath string, exitStatus int) string {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >> %q\nexit %d\n", logPath, exitStatus)
	p := filepath.Join(dir, "python")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func readInvocationLog(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	var args []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			args = append(args, line)
		}
	}
	return args
}

func writeAgentInitJSON(t *testing.T, agentDir, initJSON string) {
	t.Helper()
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), []byte(initJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestEnsureAddonsRejectsInvalidLegacyKeyBeforeInterpreter proves that a
// legacy addon key containing a source-boundary character (semicolon) fails
// at the validation boundary and that no interpreter process is started for
// it: the stub python's invocation log must never be created.
func TestEnsureAddonsRejectsInvalidLegacyKeyBeforeInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX stub interpreter not available on Windows")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "python-calls.log")
	stub := writeStubPython(t, dir, logPath, 0)

	agentDir := filepath.Join(dir, "alice")
	writeAgentInitJSON(t, agentDir, `{"addons": {"imap;__import__('os').system('id')": {}}}`)

	err := EnsureAddons(stub, agentDir)
	if err == nil {
		t.Fatal("expected validation error for invalid legacy addon key")
	}
	if !strings.Contains(err.Error(), "invalid legacy addon key") {
		t.Fatalf("error = %v, want validation-boundary error", err)
	}
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Fatalf("interpreter was started despite invalid addon key; invocation log exists: %v", logPath)
	}
}

// TestEnsureAddonsValidKeyRunsImportabilityCheck proves valid legacy addon
// keys retain the existing importability check: the stub interpreter is
// invoked exactly once with the constructed import statement, and its exit
// status drives the result.
func TestEnsureAddonsValidKeyRunsImportabilityCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX stub interpreter not available on Windows")
	}

	t.Run("importable", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "python-calls.log")
		stub := writeStubPython(t, dir, logPath, 0)
		agentDir := filepath.Join(dir, "alice")
		writeAgentInitJSON(t, agentDir, `{"addons": {"imap": {}}}`)

		if err := EnsureAddons(stub, agentDir); err != nil {
			t.Fatalf("EnsureAddons = %v, want nil for importable addon", err)
		}
		calls := readInvocationLog(t, logPath)
		if len(calls) != 2 || calls[0] != "-c" || calls[1] != "import lingtai.addons.imap" {
			t.Fatalf("interpreter calls = %v, want [-c import lingtai.addons.imap]", calls)
		}
	})

	t.Run("not importable", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "python-calls.log")
		stub := writeStubPython(t, dir, logPath, 1)
		agentDir := filepath.Join(dir, "alice")
		writeAgentInitJSON(t, agentDir, `{"addons": {"imap": {}}}`)

		err := EnsureAddons(stub, agentDir)
		if err == nil {
			t.Fatal("expected importability error for failing stub")
		}
		if !strings.Contains(err.Error(), "not importable") {
			t.Fatalf("error = %v, want importability error", err)
		}
		calls := readInvocationLog(t, logPath)
		if len(calls) != 2 || calls[0] != "-c" || calls[1] != "import lingtai.addons.imap" {
			t.Fatalf("interpreter calls = %v, want [-c import lingtai.addons.imap]", calls)
		}
	})
}
