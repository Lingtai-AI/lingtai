package tui

import (
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestCPRDirectTargetAttemptsReviveWithEmptyFallbackCommand(t *testing.T) {
	root := t.TempDir()
	lingtaiDir := filepath.Join(root, ".lingtai")
	targetDir := filepath.Join(lingtaiDir, "worker")
	writeRecipeFile(t, filepath.Join(targetDir, ".agent.json"), `{
		"agent_id":"worker",
		"agent_name":"Worker",
		"address":"worker",
		"state":"STOPPED",
		"admin":{}
	}`)
	writeRecipeFile(t, filepath.Join(targetDir, "init.json"), `{"venv_path":"`+filepath.Join(root, "runtime", "venv")+`"}`)

	target := fs.DirectTarget{
		ProjectDirectory: root,
		Directory:        targetDir,
		AgentID:          "worker",
		Address:          "worker",
	}
	threadKey := fs.DirectThreadKey(target)
	app := App{
		currentView: appViewMail,
		projectDir:  lingtaiDir,
		orchDir:     filepath.Join(lingtaiDir, "orchestrator"),
		orchName:    "orchestrator",
		lingtaiCmd:  "",
		mail: MailModel{
			baseDir:                lingtaiDir,
			acceptedSnapshotSerial: 1,
			directChat: directChatState{
				target:                 target,
				threadKey:              threadKey,
				generation:             1,
				acceptedSnapshotSerial: 1,
			},
			agentSelector: agentSelectorState{
				selectedThreadKey: threadKey,
			},
		},
	}

	var gotCmd, gotDir string
	previous := reviveAgentDir
	reviveAgentDir = func(lingtaiCmd, dir string) error {
		gotCmd = lingtaiCmd
		gotDir = dir
		return nil
	}
	defer func() { reviveAgentDir = previous }()

	_, _ = app.handlePaletteCommand("cpr", "")

	if gotDir != targetDir {
		t.Fatalf("/cpr revived dir %q, want selected direct target %q", gotDir, targetDir)
	}
	if gotCmd != "" {
		t.Fatalf("/cpr fallback command = %q, want empty string passed through for target venv resolution", gotCmd)
	}
}
