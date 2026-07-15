package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNoProjectProgramLoadingKeepsOneAltScreenLifecycle(t *testing.T) {
	m := noProjectProgramModel{loading: true, width: 80, height: 24}
	v := m.View()
	if !v.AltScreen {
		t.Fatal("handoff loading view must own the alternate screen")
	}
	if !strings.Contains(ansi.Strip(v.Content), "⢀⡴⠖⠚⠃") {
		t.Fatal("handoff loading view did not contain the canonical Bodhi leaf")
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	if cmd == nil {
		t.Fatal("Ctrl+C during handoff loading did not return a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl+C command produced %T, want tea.QuitMsg", cmd())
	}
	if _, ok := updated.(noProjectProgramModel); !ok {
		t.Fatalf("handoff Ctrl+C returned %T", updated)
	}
}

func TestStartupUpgradeOutcomeNeverRetriesOrCreatesApp(t *testing.T) {
	if got := startupKindAfterTUIUpgrade(false, true); got != startupUpgradeExit {
		t.Fatalf("successful outside-program upgrade = %v, want upgrade exit", got)
	}
	if got := startupKindAfterTUIUpgrade(true, true); got != startupFallback {
		t.Fatalf("successful in-program upgrade = %v, want outside-program fallback", got)
	}
	if got := startupKindAfterTUIUpgrade(false, false); got != startupReady {
		t.Fatalf("declined/failed upgrade = %v, want continue", got)
	}
}

func TestAgentCountPromptPredictionHonorsFreshMarker(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, ".last_agent_check")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	countCalls := 0
	count := func() int { countCalls++; return 9 }
	if got := agentCountPromptNeeded(dir, now, os.Stat, count, os.MkdirAll, os.WriteFile, os.Chtimes); got {
		t.Fatal("fresh marker predicted a second startup prompt")
	}
	if countCalls != 0 {
		t.Fatalf("fresh marker scanned processes %d times", countCalls)
	}
	old := now.Add(-agentCheckInterval - time.Second)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatal(err)
	}
	if got := agentCountPromptNeeded(dir, now, os.Stat, count, os.MkdirAll, os.WriteFile, os.Chtimes); !got {
		t.Fatal("stale marker did not predict the gated agent-count check")
	}
	if got := agentCountPromptNeeded(dir, now, os.Stat, func() int { return 0 }, os.MkdirAll, os.WriteFile, os.Chtimes); got {
		t.Fatal("stale marker with zero agents predicted a prompt")
	}
	if got := agentCountPromptNeeded(t.TempDir(), now, os.Stat, func() int { return 0 }, os.MkdirAll, os.WriteFile, os.Chtimes); got {
		t.Fatal("missing marker with zero agents predicted a prompt")
	}
	if got := agentCountPromptNeeded(t.TempDir(), now, os.Stat, func() int { return 4 }, os.MkdirAll, os.WriteFile, os.Chtimes); !got {
		t.Fatal("missing marker with agents did not predict a prompt")
	}
}

func TestNoProjectProgramHandoffFailureQuitsWithoutZeroApp(t *testing.T) {
	m := noProjectProgramModel{loading: true, width: 80, height: 24}
	updated, cmd := m.Update(startupReadyMsg{result: startupResult{
		kind:       startupFallback,
		projectDir: "/selected/project",
	}})
	if cmd == nil {
		t.Fatal("fallback handoff did not quit the renderer")
	}
	got := updated.(noProjectProgramModel)
	if got.appReady {
		t.Fatal("fallback handoff transitioned to a zero App")
	}
	if got.startup.projectDir != "/selected/project" {
		t.Fatalf("fallback lost selected project root: %q", got.startup.projectDir)
	}
}

func TestNoProjectProgramHandoffFatalQuitsAfterRendererState(t *testing.T) {
	m := noProjectProgramModel{loading: true}
	updated, cmd := m.Update(startupReadyMsg{result: startupResult{
		kind: startupFatal,
		err:  errors.New("boom"),
	}})
	if cmd == nil {
		t.Fatal("fatal handoff did not quit the renderer")
	}
	if updated.(noProjectProgramModel).appReady {
		t.Fatal("fatal handoff transitioned to an App")
	}
}
