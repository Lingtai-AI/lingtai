package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestPR5Stage4UnreadBaselineWaitsForAcceptedSnapshotAndMainPreservesOrdinary(t *testing.T) {
	installationTestStart(t)

	app, scanner, _ := installationNewApp(t, 0)
	projectRoot := filepath.Dir(app.projectDir)
	targetDir := filepath.Join(app.projectDir, "agent-a")
	installationWriteAgent(t, targetDir, "agent-a", "Agent A", "Agent A")

	setter, ok := any(&app).(pr5AgentRailInventoryScannerSetter)
	if !ok {
		t.Fatal("App has no root-owned agent-rail inventory lifecycle boundary")
	}
	inventoryScript := &pr5RailInventoryScanScript{steps: []pr5RailInventoryScanStep{{
		snapshot: pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 6101),
	}}}
	setter.setAgentRailInventoryScanner(inventoryScript.Scan)
	inventoryResult := pr5RunTrailingRailInventoryScan(t, app.Init(), inventoryScript)
	app, _ = installationDeliverApp(t, app, inventoryResult)
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent A"})
	if app.mailStore.snapshot != nil || app.mail.acceptedSnapshot != nil || scanner.scans.Load() != 0 {
		t.Fatalf(
			"inventory-first setup accepted mail prematurely: store=%v mail=%v scans=%d",
			app.mailStore.snapshot != nil, app.mail.acceptedSnapshot != nil, scanner.scans.Load(),
		)
	}

	statePath := fs.RailUnreadStatePath(projectRoot)
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("unread state before first accepted mail snapshot: err=%v", err)
	}

	history := pr5ProjectionMail(
		"historical-a", "agent-a", "human", nil,
		"historical Agent A mail", "2026-07-15T00:00:00Z",
	)
	scanner.messages = []fs.MailMessage{history}
	app, _ = installationAcceptInitial(t, app)

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("first accepted snapshot did not persist unread baseline: %v", err)
	}
	var persisted struct {
		Version int                        `json:"version"`
		Targets map[string]json.RawMessage `json:"targets"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unread baseline is not valid JSON: %v", err)
	}
	if persisted.Version != fs.RailUnreadStateVersion || len(persisted.Targets) != 2 {
		t.Fatalf(
			"baseline version/targets = %d/%d, want %d/2 (Main + Agent A)",
			persisted.Version, len(persisted.Targets), fs.RailUnreadStateVersion,
		)
	}
	for _, directory := range []string{app.orchDir, targetDir} {
		if _, exists := persisted.Targets[filepath.Clean(directory)]; !exists {
			t.Fatalf("baseline targets = %v, want accepted directory %q", persisted.Targets, directory)
		}
	}

	targets := []fs.DirectTarget{
		{Directory: app.orchDir, Address: app.mail.orchAddr},
		{Directory: targetDir, Address: "agent-a"},
	}
	reopened, err := fs.OpenRailUnreadStore(
		projectRoot,
		targets,
		app.mailStore.snapshot.cache.Messages,
		app.mail.humanAddr,
	)
	if err != nil {
		t.Fatalf("reopen first accepted unread baseline: %v", err)
	}
	if got := reopened.UnreadCount(targets[1], app.mailStore.snapshot.cache.Messages, app.mail.humanAddr); got != 0 {
		t.Fatalf("historical Agent A unread after first baseline = %d, want 0", got)
	}

	later := pr5ProjectionMail(
		"later-a", "agent-a", "human", nil,
		"later Agent A mail", "2026-07-15T00:01:00Z",
	)
	scanner.messages = []fs.MailMessage{history, later}
	steady := installationRefreshResult(t, &app, false)
	app, _ = installationDeliverApp(t, app, steady)
	if app.mailStore.binding.target.policy != asyncTargetHomeMain {
		t.Fatalf("steady accepted snapshot active policy = %d, want home Main", app.mailStore.binding.target.policy)
	}

	reopened, err = fs.OpenRailUnreadStore(
		projectRoot,
		targets,
		app.mailStore.snapshot.cache.Messages,
		app.mail.humanAddr,
	)
	if err != nil {
		t.Fatalf("reopen unread state after steady accepted snapshot: %v", err)
	}
	if got := reopened.UnreadCount(targets[1], app.mailStore.snapshot.cache.Messages, app.mail.humanAddr); got != 1 {
		t.Fatalf("inactive Agent A unread while aggregate Main remains active = %d, want 1", got)
	}
}
