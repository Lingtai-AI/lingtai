package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/process"
)

func TestPuffoManagedAgentRejectsTUIRelaunchBeforeSignals(t *testing.T) {
	dir := t.TempDir()
	previous := hasPuffoManagedAgent
	hasPuffoManagedAgent = func(string) bool { return true }
	t.Cleanup(func() { hasPuffoManagedAgent = previous })

	for name, run := range map[string]func() error{
		"refresh":        func() error { return hardRefreshDir("python", dir) },
		"refresh_preset": func() error { return hardRefreshDirWithPreset("python", dir, "preset") },
		"revive":         func() error { return reviveDir("python", dir) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, process.ErrPuffoManagedAgent) {
				t.Fatalf("got %v, want ErrPuffoManagedAgent", err)
			}
			for _, signal := range []string{".suspend", ".agent.lock", ".refresh"} {
				if _, err := os.Stat(filepath.Join(dir, signal)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("%s was touched before rejecting ACP owner: %v", signal, err)
				}
			}
		})
	}
}

func TestPuffoManagedAgentErrorExplainsHandoff(t *testing.T) {
	got := process.ErrPuffoManagedAgent.Error()
	want := "this agent is managed by Puffo; to run it in LingTai, pause it in Puffo, start it in LingTai, then resume it in Puffo"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
