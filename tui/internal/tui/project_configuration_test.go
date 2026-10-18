package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
)

// Use the repository's existing POSIX child-stub style at the real resolver
// boundary, not a global function replacement or domain-merging fake.
func configurationUIChild(t *testing.T, globalDir string, fail bool) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX UI child stub; portable wire fixture covers Windows framing")
	}
	python := config.VenvPython(filepath.Join(globalDir, "runtime", "venv"))
	if err := os.MkdirAll(filepath.Dir(python), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("fixture interpreter selection only"), 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "argv")
	input := filepath.Join(t.TempDir(), "input.json")
	output := `{"status":"configured","outcome":"complete","updated":1,"skipped":0,"failed":0,"code":"","error":"","source":{"init_written":true,"manifest_written":true,"psyche_written":false,"reader_status":"READ_OK_WITH_IGNORED_FIELDS"}}`
	ending := "exit 0"
	stream := ""
	if fail {
		output = `{"status":"error","outcome":"partial","updated":1,"skipped":0,"failed":1,"code":"manifest_write_failed","error":"Project configuration could not complete","source":{"init_written":true,"manifest_written":false,"psyche_written":false,"reader_status":"not_read"}}`
		ending, stream = "exit 1", " >&2"
	}
	t.Setenv("PROJECT_CONFIGURATION_ARGV", log)
	t.Setenv("PROJECT_CONFIGURATION_STDIN", input)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$PROJECT_CONFIGURATION_ARGV\"\ncat > \"$PROJECT_CONFIGURATION_STDIN\"\nprintf '%s\\n' '" + output + "'" + stream + "\n" + ending + "\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(python), "lingtai-agent"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return log, input
}

func TestSetupCallsKernelAndOnlySendsExplicitPromptEdits(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(map[bool]string{false: "omission", true: "explicit-edit"}[edit], func(t *testing.T) {
			root, global := t.TempDir(), t.TempDir()
			lingtaiDir := filepath.Join(root, ".lingtai")
			selected := filepath.Join(lingtaiDir, "orch-dir")
			if err := os.MkdirAll(selected, 0755); err != nil {
				t.Fatal(err)
			}
			owner := filepath.Join(selected, "settings", "psyche.json")
			if err := os.MkdirAll(filepath.Dir(owner), 0755); err != nil {
				t.Fatal(err)
			}
			before := []byte("opaque pre-existing owner bytes")
			if err := os.WriteFile(owner, before, 0600); err != nil {
				t.Fatal(err)
			}
			log, input := configurationUIChild(t, global, false)
			opts := preset.DefaultAgentOpts()
			opts.CovenantFile = "/displayed/current-covenant.md"
			if edit {
				opts.CovenantFile = "/explicit/edited-covenant.md"
			}
			m := FirstRunModel{baseDir: lingtaiDir, globalDir: global, setupMode: true, setupOrchDir: selected, agentName: "Display", cursor: -1,
				setupKeepPreset: preset.Preset{Name: "keep_current", Source: preset.SourceUnknown}, pendingAgentOpts: opts,
				setupPromptCovenantBaseline: "/displayed/current-covenant.md"}
			updated, cmd := m.performSetupSaveOnly()
			if cmd == nil {
				t.Fatalf("save refused: %s", updated.message)
			}
			if _, ok := cmd().(SetupSavedMsg); !ok {
				t.Fatal("missing actual setup completion")
			}
			data, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			var request process.ProjectConfigureRequest
			if json.Unmarshal(data, &request) != nil || request.PresetRef != "" || !request.KeepCurrent || !request.Options.PreserveActive {
				t.Fatalf("not explicit Keep-current: %s", data)
			}
			if edit != (request.Prompts != nil) || request.Options.Pad != nil {
				t.Fatalf("unexpected owner edit: %s", data)
			}
			after, _ := os.ReadFile(owner)
			if string(after) != string(before) {
				t.Fatal("Go rewrote Psyche bytes")
			}
			argv, _ := os.ReadFile(log)
			if string(argv) != "project\nconfigure\n" {
				t.Fatalf("unexpected argv %q", argv)
			}
		})
	}
}

func TestImportedFlowMakesOneRehydrateBeforeSourceInitAndStopsOnPartial(t *testing.T) {
	root, global := t.TempDir(), t.TempDir()
	lingtaiDir := filepath.Join(root, ".lingtai")
	selected := filepath.Join(lingtaiDir, "orch-dir")
	if err := os.MkdirAll(selected, 0755); err != nil {
		t.Fatal(err)
	}
	log, input := configurationUIChild(t, global, true)
	// New preceding Recipe operations use the real installed Core; only the
	// original rehydrate result remains the existing partial-failure fixture.
	installed := fixtureUIProjectPython(t)
	python := config.VenvPython(filepath.Join(global, "runtime", "venv"))
	candidateCLI := filepath.Join(filepath.Dir(installed), "lingtai-agent")
	cli := filepath.Join(filepath.Dir(python), "lingtai-agent")
	original, err := os.ReadFile(cli)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROJECT_FIXTURE_REAL_PYTHON", installed)
	t.Setenv("PROJECT_FIXTURE_REAL_CLI", candidateCLI)
	prefix := "#!/bin/sh\ncase \"$1:$2\" in project:recipe-layer) exec \"$PROJECT_FIXTURE_REAL_CLI\" \"$@\";; esac\n"
	if err := os.WriteFile(cli, append([]byte(prefix), original[len("#!/bin/sh\n"):]...), 0755); err != nil {
		t.Fatal(err)
	}
	wrapper := "#!/bin/sh\nif [ \"$1:$2:$3:$4\" = \"-m:lingtai:project:recipe-materialize\" ]; then exec \"$PROJECT_FIXTURE_REAL_PYTHON\" \"$@\"; fi\nexit 91\n"
	if err := os.WriteFile(python, []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}
	// WriteFile preserves the existing nonexecutable fixture handle's mode.
	if err := os.Chmod(python, 0755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	managed := filepath.Join(home, config.GlobalDirName, "runtime", "venv")
	if err := os.MkdirAll(filepath.Dir(managed), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(global, "runtime", "venv"), managed); err != nil {
		t.Fatal(err)
	}
	p := preset.Preset{Name: "real", Source: preset.SourceSaved}
	m := FirstRunModel{baseDir: lingtaiDir, globalDir: global, rehydrateMode: true, rehydrateOrchDir: "orch-dir", agentName: "Display", pendingDirName: "orch-dir", pendingAgentOpts: preset.DefaultAgentOpts(), presets: []preset.Preset{p}, cursor: 0}
	updated, cmd := m.performRecipeSave(preset.DefaultRecipe)
	if cmd == nil {
		t.Fatalf("no combined command: %s", updated.message)
	}
	if _, err := os.Stat(filepath.Join(selected, "init.json")); !os.IsNotExist(err) {
		t.Fatal("source init written before all-absent gate")
	}
	msg, ok := cmd().(rehydrateDoneMsg)
	if !ok || msg.err == "" {
		t.Fatalf("partial became success: %+v", msg)
	}
	updated, _ = updated.Update(msg)
	if !updated.rehydrateDone || updated.rehydrateErr == "" {
		t.Fatal("partial not terminal")
	}
	_, quit := updated.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if quit == nil {
		t.Fatal("partial allowed initialization to continue")
	}
	argv, _ := os.ReadFile(log)
	if string(argv) != "project\nrehydrate\n" {
		t.Fatalf("split configure/retry: %q", argv)
	}
	data, _ := os.ReadFile(input)
	if !strings.Contains(string(data), `"directory_name":"orch-dir"`) {
		t.Fatalf("selected directory lost: %s", data)
	}
	if _, err := os.Stat(filepath.Join(selected, ".prompt")); !os.IsNotExist(err) {
		t.Fatal("Recipe applied after partial kernel result")
	}
}

func TestAppRecoveryDoesNotLaunchAfterUnknownPropagation(t *testing.T) {
	a := visitTestApp(t)
	a.recoveryMode = true
	a.lingtaiCmd = filepath.Join(t.TempDir(), "missing", "python")
	model, _ := a.Update(SetupSavedMsg{})
	after := model.(App)
	if after.currentView != appViewMail {
		t.Fatal("failure not displayed")
	}
	// AddSystemMessage displays statusFlash; it does not append mail records.
	if !strings.Contains(after.mail.statusFlash, "outcome unknown") {
		t.Fatalf("unknown propagation not displayed: %q", after.mail.statusFlash)
	}
	if !after.recoveryMode {
		t.Fatal("failed propagation cleared recovery before a successful launch")
	}
}
