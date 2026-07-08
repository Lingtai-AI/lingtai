package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeMailTargetAgent(t *testing.T, dir, name, nickname, address, state string, human bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	admin := "{}"
	if human {
		admin = "null"
	}
	body := fmt.Sprintf(`{"agent_name":%q,"nickname":%q,"address":%q,"state":%q,"admin":%s}`, name, nickname, address, state, admin)
	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write .agent.json: %v", err)
	}
	if !human {
		ts := fmt.Sprintf("%f", float64(time.Now().Unix()))
		if err := os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(ts), 0o644); err != nil {
			t.Fatalf("write heartbeat: %v", err)
		}
	}
}

func readOnlyOutboxMessage(t *testing.T, humanDir string) map[string]interface{} {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(humanDir, "mailbox", "outbox", "*", "message.json"))
	if err != nil {
		t.Fatalf("glob outbox: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one outbox message, got %d: %v", len(matches), matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read outbox message: %v", err)
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("decode outbox message: %v", err)
	}
	return msg
}

func TestMailModelSetMailTargetSelectsCurrentProjectAgent(t *testing.T) {
	projectDir := t.TempDir()
	humanDir := filepath.Join(projectDir, "human")
	orchDir := filepath.Join(projectDir, "manager")
	helperDir := filepath.Join(projectDir, "helper")
	writeMailTargetAgent(t, humanDir, "human", "Human", "human", "IDLE", true)
	writeMailTargetAgent(t, orchDir, "manager", "Boss", "manager", "IDLE", false)
	writeMailTargetAgent(t, helperDir, "helper", "Helper", "helper", "IDLE", false)

	m := NewMailModel(humanDir, "human", projectDir, orchDir, "manager", 20, "", "en", false, 0)
	list := m.SetMailTarget(projectDir, "")
	if !strings.Contains(list, "Boss") || !strings.Contains(list, "Helper") {
		t.Fatalf("target list missing current-project agents: %q", list)
	}
	if strings.Contains(list, "Human") {
		t.Fatalf("target list should not include human pseudo-agent: %q", list)
	}

	msg := m.SetMailTarget(projectDir, "helper")
	if !strings.Contains(msg, "Helper") {
		t.Fatalf("select message missing helper: %q", msg)
	}
	if got := m.currentMailTargetDir(); got != helperDir {
		t.Fatalf("target dir = %q, want %q", got, helperDir)
	}
	if got := m.currentMailTargetAddr(); got != "helper" {
		t.Fatalf("target addr = %q, want helper", got)
	}
	if got := m.currentMailTargetDisplayName(); got != "Helper" {
		t.Fatalf("target display = %q, want Helper", got)
	}
}

func TestMailModelFooterUsesSelectedMailTarget(t *testing.T) {
	m := NewMailModel("", "human", "", "", "manager", 20, "", "en", false, 0)
	m.width = 100
	m.height = 20
	m.ready = true
	m.mailTargetName = "helper"
	m.mailTargetNickname = "Helper"
	m.mailTargetState = "IDLE"
	m.mailTargetAlive = true

	line := emailToLine(m.View())
	if !strings.Contains(line, "Email To: Helper") {
		t.Fatalf("footer = %q, want selected target", line)
	}
}

func TestMailModelSendUsesSelectedMailTarget(t *testing.T) {
	projectDir := t.TempDir()
	humanDir := filepath.Join(projectDir, "human")
	orchDir := filepath.Join(projectDir, "manager")
	helperDir := filepath.Join(projectDir, "helper")
	writeMailTargetAgent(t, humanDir, "human", "Human", "human", "IDLE", true)
	writeMailTargetAgent(t, orchDir, "manager", "Boss", "manager", "IDLE", false)
	writeMailTargetAgent(t, helperDir, "helper", "Helper", "helper", "IDLE", false)

	m := NewMailModel(humanDir, "human", projectDir, orchDir, "manager", 20, "", "en", false, 0)
	m.SetMailTarget(projectDir, "helper")
	m.input.SetValue("hello selected target")
	updated, _ := m.Update(SendMsg{})
	if updated.input.Value() != "" {
		t.Fatalf("input not reset after send: %q", updated.input.Value())
	}

	msg := readOnlyOutboxMessage(t, humanDir)
	to, ok := msg["to"].([]interface{})
	if !ok || len(to) != 1 || fmt.Sprint(to[0]) != "helper" {
		t.Fatalf("outbox to = %#v, want [helper]; full=%v", msg["to"], msg)
	}
	if got := fmt.Sprint(msg["message"]); got != "hello selected target" {
		t.Fatalf("message body = %q, want hello selected target", got)
	}
}

func outboxMessageCount(t *testing.T, humanDir string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(humanDir, "mailbox", "outbox", "*", "message.json"))
	if err != nil {
		t.Fatalf("glob outbox: %v", err)
	}
	return len(matches)
}

func makeMailTargetTestNetwork(t *testing.T) (projectDir, humanDir, orchDir, helperDir string) {
	t.Helper()
	projectDir = t.TempDir()
	humanDir = filepath.Join(projectDir, "human")
	orchDir = filepath.Join(projectDir, "manager")
	helperDir = filepath.Join(projectDir, "helper")
	writeMailTargetAgent(t, humanDir, "human", "Human", "human", "IDLE", true)
	writeMailTargetAgent(t, orchDir, "manager", "Boss", "manager", "IDLE", false)
	writeMailTargetAgent(t, helperDir, "helper", "Helper", "helper", "IDLE", false)
	return projectDir, humanDir, orchDir, helperDir
}

func staleMailTargetHeartbeat(t *testing.T, dir string) {
	t.Helper()
	old := fmt.Sprintf("%f", float64(time.Now().Add(-10*time.Second).Unix()))
	if err := os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(old), 0o644); err != nil {
		t.Fatalf("write stale heartbeat: %v", err)
	}
}

func TestMailModelSlashCommandForwardsPaletteArgs(t *testing.T) {
	m := NewMailModel("", "human", "", "", "manager", 20, "", "en", false, 0)
	m.input.SetValue("/to helper")
	_, cmd := m.Update(SendMsg{})
	if cmd == nil {
		t.Fatal("slash command did not return a palette command")
	}
	msg, ok := cmd().(PaletteSelectMsg)
	if !ok {
		t.Fatalf("slash command message type = %T, want PaletteSelectMsg", cmd())
	}
	if msg.Command != "to" || msg.Args != "helper" {
		t.Fatalf("slash command = %#v, want command to with helper args", msg)
	}

	_, forward := m.Update(msg)
	if forward == nil {
		t.Fatal("mail view did not forward PaletteSelectMsg to app")
	}
	forwarded, ok := forward().(PaletteSelectMsg)
	if !ok {
		t.Fatalf("forwarded message type = %T, want PaletteSelectMsg", forward())
	}
	if forwarded.Command != "to" || forwarded.Args != "helper" {
		t.Fatalf("forwarded command = %#v, want args preserved", forwarded)
	}
}

func TestAppHandlePaletteCommandToSelectsTarget(t *testing.T) {
	projectDir, humanDir, orchDir, helperDir := makeMailTargetTestNetwork(t)
	a := App{
		currentView: appViewMail,
		projectDir:  projectDir,
		orchDir:     orchDir,
		orchName:    "manager",
		mail:        NewMailModel(humanDir, "human", projectDir, orchDir, "manager", 20, "", "en", false, 0),
	}

	updated, _ := a.handlePaletteCommand("to", "helper")
	got, ok := updated.(App)
	if !ok {
		t.Fatalf("updated model type = %T, want App", updated)
	}
	if got.mail.currentMailTargetDir() != helperDir {
		t.Fatalf("/to helper target dir = %q, want %q", got.mail.currentMailTargetDir(), helperDir)
	}
}

func TestAppHandlePaletteCommandTargetAliasSelectsTarget(t *testing.T) {
	projectDir, humanDir, orchDir, helperDir := makeMailTargetTestNetwork(t)
	a := App{
		currentView: appViewMail,
		projectDir:  projectDir,
		orchDir:     orchDir,
		orchName:    "manager",
		mail:        NewMailModel(humanDir, "human", projectDir, orchDir, "manager", 20, "", "en", false, 0),
	}

	updated, _ := a.handlePaletteCommand("target", "helper")
	got, ok := updated.(App)
	if !ok {
		t.Fatalf("updated model type = %T, want App", updated)
	}
	if got.mail.currentMailTargetDir() != helperDir {
		t.Fatalf("/target helper target dir = %q, want %q", got.mail.currentMailTargetDir(), helperDir)
	}
}

func TestMailModelSendBlocksSuspendedTarget(t *testing.T) {
	projectDir, humanDir, orchDir, helperDir := makeMailTargetTestNetwork(t)
	writeMailTargetAgent(t, helperDir, "helper", "Helper", "helper", "SUSPENDED", false)

	m := NewMailModel(humanDir, "human", projectDir, orchDir, "manager", 20, "", "en", false, 0)
	m.SetMailTarget(projectDir, "helper")
	m.input.SetValue("should not send")
	updated, _ := m.Update(SendMsg{})
	if got := outboxMessageCount(t, humanDir); got != 0 {
		t.Fatalf("outbox message count = %d, want 0 for suspended target", got)
	}
	if !strings.Contains(updated.statusFlash, "not reachable") {
		t.Fatalf("statusFlash = %q, want not reachable", updated.statusFlash)
	}
}

func TestMailModelSendBlocksStaleTarget(t *testing.T) {
	projectDir, humanDir, orchDir, helperDir := makeMailTargetTestNetwork(t)
	staleMailTargetHeartbeat(t, helperDir)

	m := NewMailModel(humanDir, "human", projectDir, orchDir, "manager", 20, "", "en", false, 0)
	m.SetMailTarget(projectDir, "helper")
	m.input.SetValue("should not send")
	updated, _ := m.Update(SendMsg{})
	if got := outboxMessageCount(t, humanDir); got != 0 {
		t.Fatalf("outbox message count = %d, want 0 for stale target", got)
	}
	if !strings.Contains(updated.statusFlash, "not reachable") {
		t.Fatalf("statusFlash = %q, want not reachable", updated.statusFlash)
	}
}

func TestMailModelSendAllowsAsleepTarget(t *testing.T) {
	projectDir, humanDir, orchDir, helperDir := makeMailTargetTestNetwork(t)
	writeMailTargetAgent(t, helperDir, "helper", "Helper", "helper", "ASLEEP", false)
	staleMailTargetHeartbeat(t, helperDir)

	m := NewMailModel(humanDir, "human", projectDir, orchDir, "manager", 20, "", "en", false, 0)
	m.SetMailTarget(projectDir, "helper")
	m.input.SetValue("wake by mail")
	updated, _ := m.Update(SendMsg{})
	if updated.input.Value() != "" {
		t.Fatalf("input not reset after ASLEEP target send: %q", updated.input.Value())
	}

	msg := readOnlyOutboxMessage(t, humanDir)
	to, ok := msg["to"].([]interface{})
	if !ok || len(to) != 1 || fmt.Sprint(to[0]) != "helper" {
		t.Fatalf("outbox to = %#v, want [helper]; full=%v", msg["to"], msg)
	}
	if got := fmt.Sprint(msg["message"]); got != "wake by mail" {
		t.Fatalf("message body = %q, want wake by mail", got)
	}
}
