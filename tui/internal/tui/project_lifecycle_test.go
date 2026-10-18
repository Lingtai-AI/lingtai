package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/process"
)

func TestExternalLifecycleExistingRelaunchCallersBindOriginalOptions(t *testing.T) {
	previous := externalProjectLifecycle
	t.Cleanup(func() { externalProjectLifecycle = previous })
	cause := errors.New("original native launch failure")
	var got process.ProjectLifecycleOptions
	externalProjectLifecycle = func(command string, options process.ProjectLifecycleOptions, native process.ProjectLifecycleNative) (process.ProjectLifecycleResult, error) {
		if command != "original fallback" || native.Puffo == nil || native.ProbeLock == nil || native.Heartbeat == nil {
			t.Fatal("native binding lost")
		}
		got = options
		return process.ProjectLifecycleResult{}, cause
	}
	for _, test := range []struct {
		action, preset string
		run            func() error
	}{
		{"refresh", "", func() error { return hardRefreshDir("original fallback", "original target") }},
		{"refresh", "native preset", func() error { return hardRefreshDirWithPreset("original fallback", "original target", "native preset") }},
		{"revive", "", func() error { return reviveDir("original fallback", "original target") }},
	} {
		if err := test.run(); err != cause {
			t.Fatalf("error identity changed: %v", err)
		}
		if got.Action != test.action || got.Selected != "original target" || got.PresetPath != test.preset || got.All {
			t.Fatalf("options=%+v", got)
		}
	}
}

func TestExternalLifecyclePaletteKeepsActualSelectedDirectTarget(t *testing.T) {
	root := t.TempDir()
	target := fs.DirectTarget{ProjectDirectory: root, Directory: filepath.Join(root, ".lingtai", "worker"), AgentID: "worker", Address: "worker"}
	threadKey := fs.DirectThreadKey(target)
	app := App{currentView: appViewMail, projectDir: filepath.Join(root, ".lingtai"), orchDir: "not-selected", mail: MailModel{
		baseDir: filepath.Join(root, ".lingtai"), acceptedSnapshotSerial: 1,
		directChat:    directChatState{target: target, threadKey: threadKey, generation: 1, acceptedSnapshotSerial: 1},
		agentSelector: agentSelectorState{selectedThreadKey: threadKey},
	}}
	previous := externalProjectLifecycle
	t.Cleanup(func() { externalProjectLifecycle = previous })
	var got process.ProjectLifecycleOptions
	externalProjectLifecycle = func(command string, options process.ProjectLifecycleOptions, _ process.ProjectLifecycleNative) (process.ProjectLifecycleResult, error) {
		if command != "" {
			t.Fatal("empty original launch fallback was replaced")
		}
		got = options
		return process.ProjectLifecycleResult{Count: 1}, nil
	}
	for _, command := range []string{"sleep", "suspend", "cpr"} {
		_, _ = app.handlePaletteCommand(command, "")
		if got.Action != command || got.Selected != target.Directory || got.All {
			t.Fatalf("direct binding=%+v", got)
		}
	}
}

func TestNirvanaStillDrainsMailBeforeSingleCoreCleanup(t *testing.T) {
	previous := externalProjectLifecycle
	t.Cleanup(func() { externalProjectLifecycle = previous })
	calls := 0
	externalProjectLifecycle = func(_ string, options process.ProjectLifecycleOptions, _ process.ProjectLifecycleNative) (process.ProjectLifecycleResult, error) {
		calls++
		if options.Action != "nirvana" || options.Root != "original tree" || options.Force {
			t.Fatalf("nirvana became CLI clean: %+v", options)
		}
		return process.ProjectLifecycleResult{}, nil
	}
	app := App{currentView: appViewNirvana, nirvana: NirvanaModel{lingtaiDir: "original tree", cleaning: true}, mail: MailModel{directUnreadOpInFlight: true, directUnreadSyncPending: true}}
	app, cmd := app.beginNirvanaCleanup()
	if cmd != nil || calls != 0 || app.mail.directUnreadSyncPending {
		t.Fatal("cleanup crossed in-flight writer")
	}
	app.mail.directUnreadOpInFlight = false
	app, cmd = app.maybeStartNirvanaCleanup()
	if cmd == nil {
		t.Fatal("drained cleanup did not start")
	}
	if _, ok := cmd().(nirvanaCleanDoneMsg); !ok || calls != 1 {
		t.Fatal("known terminal missing")
	}
	_, cmd = app.maybeStartNirvanaCleanup()
	if cmd != nil {
		t.Fatal("cleanup issued twice")
	}
}

func TestNirvanaUnknownNeverBecomesDoneOrFirstRun(t *testing.T) {
	previous := externalProjectLifecycle
	t.Cleanup(func() { externalProjectLifecycle = previous })
	externalProjectLifecycle = func(string, process.ProjectLifecycleOptions, process.ProjectLifecycleNative) (process.ProjectLifecycleResult, error) {
		return process.ProjectLifecycleResult{}, &process.ProjectLifecycleUnknown{}
	}
	model := NewNirvanaModel("original tree")
	model.cleaning = true
	message := model.doClean()()
	if _, ok := message.(nirvanaCleanFailedMsg); !ok {
		t.Fatal("unknown became cleanup success")
	}
	model, cmd := model.Update(message)
	if cmd != nil || model.done || model.failure == nil || !strings.Contains(model.View(), "outcome unknown") {
		t.Fatal("unknown cleanup was hidden or enabled first-run")
	}
}
