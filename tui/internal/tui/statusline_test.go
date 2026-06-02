package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
)

func TestStatusLineOffDoesNotRenderOrReserveHeight(t *testing.T) {
	app := App{
		width:     80,
		tuiConfig: config.TUIConfig{StatusLine: config.StatusLineOff},
	}

	if app.statusLineEnabled() {
		t.Fatal("status line is enabled for off mode")
	}
	if got := app.renderStatusLine(); got != "" {
		t.Fatalf("renderStatusLine() = %q, want empty", got)
	}
	msg := app.childWindowSize(tea.WindowSizeMsg{Width: 80, Height: 24})
	if msg.Height != 24 {
		t.Fatalf("child height = %d, want 24", msg.Height)
	}
}

func TestStatusLineDefaultRendersProjectAndRuntime(t *testing.T) {
	oldLang := i18n.Lang()
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang(oldLang) })

	projectDir, orchDir := setupStatusLineProject(t)
	app := App{
		currentView: appViewMail,
		projectDir:  projectDir,
		orchDir:     orchDir,
		orchName:    "orch",
		width:       96,
		height:      24,
		tuiConfig:   config.TUIConfig{StatusLine: config.StatusLineDefault},
	}

	msg := app.childWindowSize(tea.WindowSizeMsg{Width: 96, Height: 24})
	if msg.Height != 23 {
		t.Fatalf("child height = %d, want 23", msg.Height)
	}

	bar := app.renderStatusLine()
	if got := lipgloss.Width(bar); got != 96 {
		t.Fatalf("status line width = %d, want 96\n%s", got, bar)
	}
	for _, want := range []string{"LingTai", "/mail", "demo", "agent", "orch", "ACTIVE", "ctx", "42%", "stamina", "1h1m", "net", "active"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("status line missing %q:\n%s", want, bar)
		}
	}
}

func TestStatusLineFullRendersTokenTotals(t *testing.T) {
	oldLang := i18n.Lang()
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang(oldLang) })

	projectDir, orchDir := setupStatusLineProject(t)
	writeStatusLineLedger(t, orchDir, map[string]int{
		"input":    1000,
		"output":   200,
		"thinking": 300,
		"cached":   50,
	})
	app := App{
		currentView: appViewProps,
		projectDir:  projectDir,
		orchDir:     orchDir,
		orchName:    "orch",
		width:       120,
		height:      24,
		tuiConfig:   config.TUIConfig{StatusLine: config.StatusLineFull},
	}

	bar := app.renderStatusLine()
	for _, want := range []string{"tok", "1.5k", "calls", "1"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("status line missing %q:\n%s", want, bar)
		}
	}
}

func TestAppendStatusLineContentPadsToChildHeight(t *testing.T) {
	got := appendStatusLineContent("one", "bar", 3)
	if got != "one\n\n\nbar" {
		t.Fatalf("appendStatusLineContent() = %q, want %q", got, "one\n\n\nbar")
	}
	if height := lipgloss.Height(got); height != 4 {
		t.Fatalf("rendered height = %d, want 4", height)
	}
}

func setupStatusLineProject(t *testing.T) (string, string) {
	t.Helper()

	root := filepath.Join(t.TempDir(), "demo")
	projectDir := filepath.Join(root, ".lingtai")
	orchDir := filepath.Join(projectDir, "orch")
	if err := os.MkdirAll(orchDir, 0o755); err != nil {
		t.Fatalf("mkdir orchestrator: %v", err)
	}
	writeStatusLineJSON(t, filepath.Join(orchDir, ".agent.json"), map[string]interface{}{
		"agent_name": "orch",
		"address":    "orch",
		"state":      "ACTIVE",
		"admin":      map[string]interface{}{"karma": true},
	})
	writeStatusLineJSON(t, filepath.Join(orchDir, ".status.json"), map[string]interface{}{
		"tokens": map[string]interface{}{
			"context": map[string]interface{}{
				"total_tokens": 42000,
				"window_size":  100000,
				"usage_pct":    42,
			},
		},
		"runtime": map[string]interface{}{
			"stamina_left": 3660,
		},
	})
	if err := os.WriteFile(filepath.Join(orchDir, ".agent.heartbeat"), []byte(fmt.Sprintf("%d", time.Now().Unix())), 0o644); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	return projectDir, orchDir
}

func writeStatusLineJSON(t *testing.T, path string, value interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeStatusLineLedger(t *testing.T, agentDir string, entry map[string]int) {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal ledger: %v", err)
	}
	path := filepath.Join(agentDir, "logs", "token_ledger.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir ledger dir: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
}
