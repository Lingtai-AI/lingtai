package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestPR5Stage4UnreadPersistenceFailureSurfacesStatusWithoutPublishingTargets(t *testing.T) {
	installationTestStart(t)

	t.Run("first accepted snapshot open failure", func(t *testing.T) {
		app, scanner, _ := installationNewApp(t, 0)
		projectRoot := filepath.Dir(app.projectDir)
		targetDir := filepath.Join(app.projectDir, "agent-a")
		installationWriteAgent(t, targetDir, "agent-a", "Agent A", "Agent A")

		script := &pr5RailInventoryScanScript{steps: []pr5RailInventoryScanStep{{
			snapshot: pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 6201),
		}}}
		setter, ok := any(&app).(pr5AgentRailInventoryScannerSetter)
		if !ok {
			t.Fatal("App has no root-owned agent-rail inventory lifecycle boundary")
		}
		setter.setAgentRailInventoryScanner(script.Scan)
		inventoryResult := pr5RunTrailingRailInventoryScan(t, app.Init(), script)
		app, _ = installationDeliverApp(t, app, inventoryResult)
		pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent A"})

		pr5BlockRailUnreadStateParent(t, projectRoot)
		scanner.messages = []fs.MailMessage{pr5ProjectionMail(
			"historical-a", "agent-a", "human", nil,
			"historical Agent A mail", "2026-07-15T02:30:00Z",
		)}
		app.mail.statusFlash = ""
		app.mail.statusExpiry = time.Time{}
		app, _ = installationAcceptInitial(t, app)

		if app.railUnreadStore != nil {
			t.Fatal("failed initial unread open published a live store")
		}
		pr5RequireUnreadPersistenceStatus(t, app)
	})

	t.Run("accepted inventory sync failure", func(t *testing.T) {
		app, scanner, _ := installationNewApp(t, 0)
		projectRoot := filepath.Dir(app.projectDir)
		targetDir := filepath.Join(app.projectDir, "agent-a")
		installationWriteAgent(t, targetDir, "agent-a", "Agent A", "Agent A")

		initial := pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 6301)
		changed := pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 6301)
		agentB := pr5RailLifecycleSnapshot(app, "agent-b", "Agent B", 6302)
		changed.Records = append(changed.Records, agentB.Records...)
		script := &pr5RailInventoryScanScript{steps: []pr5RailInventoryScanStep{
			{snapshot: initial},
			{snapshot: changed},
		}}
		setter, ok := any(&app).(pr5AgentRailInventoryScannerSetter)
		if !ok {
			t.Fatal("App has no root-owned agent-rail inventory lifecycle boundary")
		}
		setter.setAgentRailInventoryScanner(script.Scan)
		inventoryResult := pr5RunTrailingRailInventoryScan(t, app.Init(), script)
		app, _ = installationDeliverApp(t, app, inventoryResult)

		history := pr5ProjectionMail(
			"historical-a", "agent-a", "human", nil,
			"historical Agent A mail", "2026-07-15T02:30:00Z",
		)
		scanner.messages = []fs.MailMessage{history}
		app, _ = installationAcceptInitial(t, app)
		if app.railUnreadStore == nil {
			t.Fatal("precondition unread store was not opened")
		}

		targetA := fs.DirectTarget{Directory: targetDir, Address: "agent-a"}
		later := pr5ProjectionMail(
			"later-a", "agent-a", "human", nil,
			"later Agent A mail", "2026-07-15T02:31:00Z",
		)
		acceptedLater := []fs.MailMessage{history, later}
		if got := app.railUnreadStore.UnreadCount(targetA, acceptedLater, app.mail.humanAddr); got != 1 {
			t.Fatalf("precondition Agent A unread = %d, want 1", got)
		}

		pr5BlockRailUnreadStateParent(t, projectRoot)
		app.mail.statusFlash = ""
		app.mail.statusExpiry = time.Time{}
		nextInventory := pr5RunTrailingRailInventoryScan(t, app.resumeProjectMail(false), script)
		app, _ = installationDeliverApp(t, app, nextInventory)
		pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent A", "Agent B"})

		pr5RequireUnreadPersistenceStatus(t, app)
		if got := app.railUnreadStore.UnreadCount(targetA, acceptedLater, app.mail.humanAddr); got != 1 {
			t.Fatalf("Agent A unread after failed target sync = %d, want preserved 1", got)
		}
	})
}

func pr5BlockRailUnreadStateParent(t *testing.T, projectRoot string) {
	t.Helper()
	statePath := fs.RailUnreadStatePath(projectRoot)
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove unread state: %v", err)
	}
	stateDir := filepath.Dir(statePath)
	if err := os.Remove(stateDir); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove unread state directory: %v", err)
	}
	if err := os.WriteFile(stateDir, []byte("block unread state persistence"), 0o644); err != nil {
		t.Fatalf("install unread state blocker: %v", err)
	}
}

func pr5RequireUnreadPersistenceStatus(t *testing.T, app App) {
	t.Helper()
	if !strings.Contains(strings.ToLower(app.mail.statusFlash), "unread") {
		t.Fatalf("unread persistence status = %q, want surfaced unread error", app.mail.statusFlash)
	}
	if app.mail.statusExpiry.IsZero() || !app.mail.statusExpiry.After(time.Now()) {
		t.Fatalf("unread persistence status expiry = %v, want future transient expiry", app.mail.statusExpiry)
	}
}
