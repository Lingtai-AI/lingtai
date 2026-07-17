package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/internal/tui"
)

func TestLauncherHandoffProjectIncludesSuccessfulCreate(t *testing.T) {
	const projectRoot = "/created/project"
	for _, kind := range []tui.LauncherDecisionKind{tui.DecisionOpenExisting, tui.DecisionCreate} {
		got, ok := launcherHandoffProject(tui.LauncherResult{Kind: kind, ProjectRoot: projectRoot})
		if !ok || got != projectRoot {
			t.Fatalf("kind %v handoff = (%q, %v), want (%q, true)", kind, got, ok, projectRoot)
		}
	}
	if got, ok := launcherHandoffProject(tui.LauncherResult{Kind: tui.DecisionCancel}); ok || got != "" {
		t.Fatalf("cancel handoff = (%q, %v), want (\"\", false)", got, ok)
	}
}

func TestNoProjectProgramRetainsWindowSizeBeforeHandoff(t *testing.T) {
	m := noProjectProgramModel{
		launcher: tui.NewLauncherRootModel(t.TempDir(), t.TempDir(), ""),
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 123, Height: 45})
	got := updated.(noProjectProgramModel)
	if got.width != 123 || got.height != 45 {
		t.Fatalf("root size = %dx%d, want 123x45", got.width, got.height)
	}
	got.loading = true
	content := got.View().Content
	if lines := strings.Count(content, "\n") + 1; lines != 45 {
		t.Fatalf("loading page height = %d, want retained 45", lines)
	}
}

func TestNoProjectProgramLoadingUsesActiveThemePagePalette(t *testing.T) {
	original := tui.ActiveTheme()
	t.Cleanup(func() { tui.SetTheme(original) })
	tui.SetThemeByName("xuan-paper")

	v := (noProjectProgramModel{loading: true, width: 80, height: 24}).View()
	active := tui.ActiveTheme()
	if !reflect.DeepEqual(v.BackgroundColor, active.BG) {
		t.Fatalf("loading page background = %v, want active theme %v", v.BackgroundColor, active.BG)
	}
	if !reflect.DeepEqual(v.ForegroundColor, active.Text) {
		t.Fatalf("loading page foreground = %v, want active theme %v", v.ForegroundColor, active.Text)
	}
}

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
	if cmd != nil {
		t.Fatal("Ctrl+C during handoff loading quit before preparation returned")
	}
	loading := updated.(noProjectProgramModel)
	if !loading.cancelRequested || !loading.loading {
		t.Fatalf("loading Ctrl+C state = cancelRequested:%v loading:%v", loading.cancelRequested, loading.loading)
	}
}

func TestNoProjectProgramCancelDiscardsLateStartup(t *testing.T) {
	m := noProjectProgramModel{loading: true, width: 80, height: 24}
	updated, cmd := m.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	if cmd != nil {
		t.Fatal("cancel request returned a quit command")
	}
	m = updated.(noProjectProgramModel)
	updated, cmd = m.Update(startupReadyMsg{result: startupResult{
		kind:       startupReady,
		projectDir: "/selected/project",
	}})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("late startup command = %T, want tea.QuitMsg", cmd())
	}
	got := updated.(noProjectProgramModel)
	if got.startup.kind != startupCanceled || got.appReady || got.loading {
		t.Fatalf("late cancel state = kind:%v appReady:%v loading:%v", got.startup.kind, got.appReady, got.loading)
	}
}

func TestAgentCountPromptPredictionHonorsFreshMarker(t *testing.T) {
	now := time.Now()
	testCase := func(t *testing.T, fresh bool, count int, wantFallback, wantWrite bool, write func(string, []byte, os.FileMode) error) {
		t.Helper()
		dir := t.TempDir()
		marker := filepath.Join(dir, ".last_agent_check")
		if fresh {
			if err := os.WriteFile(marker, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(marker, nil, 0o644); err == nil {
			old := now.Add(-agentCheckInterval - time.Second)
			if err := os.Chtimes(marker, old, old); err != nil {
				t.Fatal(err)
			}
		}
		writes := 0
		if write == nil {
			write = func(path string, data []byte, mode os.FileMode) error {
				writes++
				return os.WriteFile(path, data, mode)
			}
		}
		got := agentCountPromptNeeded(dir, now, os.Stat, func() int { return count }, os.MkdirAll, write, os.Chtimes)
		if got != wantFallback {
			t.Fatalf("fallback = %v, want %v", got, wantFallback)
		}
		if write != nil && !wantWrite && writes != 0 {
			t.Fatalf("writes = %d, want zero", writes)
		}
	}
	testCase(t, true, 9, false, false, nil)
	testCase(t, false, 0, false, true, nil)
	testCase(t, false, 9, true, false, nil)
	testCase(t, false, 0, false, true, nil)
	testCase(t, false, 0, true, false, func(string, []byte, os.FileMode) error {
		return errors.New("write denied")
	})
	if got := agentCountPromptNeeded(t.TempDir(), now, func(string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}, func() int { return 0 }, os.MkdirAll, os.WriteFile, os.Chtimes); got {
		t.Fatal("missing marker with zero agents unexpectedly fell back")
	}
	missingN := t.TempDir()
	writes := 0
	if !agentCountPromptNeeded(missingN, now, os.Stat, func() int { return 9 }, os.MkdirAll,
		func(string, []byte, os.FileMode) error {
			writes++
			return os.WriteFile(filepath.Join(missingN, ".last_agent_check"), nil, 0o644)
		}, os.Chtimes) {
		t.Fatal("missing marker with agents did not request outside prompt")
	}
	if writes != 0 {
		t.Fatalf("missing marker positive count wrote marker %d times", writes)
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
