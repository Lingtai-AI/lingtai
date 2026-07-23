package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

type v1BoundaryOutboxMessage struct {
	To      []string `json:"to"`
	CC      []string `json:"cc"`
	Message string   `json:"message"`
}

func v1BoundaryWriteManifest(t *testing.T, projectDir, dirName, body string) string {
	t.Helper()
	agentDir := filepath.Join(projectDir, dirName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("create agent directory %q: %v", agentDir, err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write agent manifest %q: %v", dirName, err)
	}
	return agentDir
}

func v1BoundaryIncoming(agentID, address, id, at, body string, to interface{}, cc []string) fs.MailMessage {
	return fs.MailMessage{
		MailboxID:  id,
		From:       address,
		To:         to,
		CC:         cc,
		Message:    body,
		ReceivedAt: at,
		Identity:   map[string]interface{}{"agent_id": agentID},
		Delivered:  true,
	}
}

func v1BoundaryApply(app App, msg tea.Msg) (App, tea.Cmd) {
	model, cmd := app.Update(msg)
	return model.(App), cmd
}

func v1BoundaryTypeAgents(t *testing.T, app App) App {
	t.Helper()
	if got := app.mail.input.Value(); got != "" {
		t.Fatalf("precondition: compose input before typing /agents = %q, want empty", got)
	}
	for _, r := range "/agents" {
		app, _ = v1BoundaryApply(app, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	var selectCmd tea.Cmd
	app, selectCmd = v1BoundaryApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if selectCmd == nil {
		t.Fatalf("typing /agents through the real InputModel/palette produced no selection command; want the /agents selector command")
	}
	selected, ok := runCmd(selectCmd).(PaletteSelectMsg)
	if !ok {
		t.Fatalf("typing /agents through the real InputModel/palette produced %T; want PaletteSelectMsg", runCmd(selectCmd))
	}
	if selected.Command != "agents" {
		t.Fatalf("typing /agents through the real InputModel/palette selected /%s; want /agents", selected.Command)
	}
	var routeCmd tea.Cmd
	app, routeCmd = v1BoundaryApply(app, selected)
	if routeCmd != nil {
		t.Fatalf("opening /agents scheduled an activation command before Enter")
	}
	return app
}

func v1BoundaryOutbox(t *testing.T, humanDir string) []v1BoundaryOutboxMessage {
	t.Helper()
	outboxDir := filepath.Join(humanDir, "mailbox", "outbox")
	entries, err := os.ReadDir(outboxDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read human outbox: %v", err)
	}
	messages := make([]v1BoundaryOutboxMessage, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(outboxDir, entry.Name(), "message.json"))
		if err != nil {
			t.Fatalf("read outbox message %q: %v", entry.Name(), err)
		}
		var message v1BoundaryOutboxMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			t.Fatalf("decode outbox message %q: %v", entry.Name(), err)
		}
		messages = append(messages, message)
	}
	return messages
}

func v1BoundaryFixture(t *testing.T, width int) (App, string, string) {
	t.Helper()
	const (
		humanAddress = "project/human"
		alphaAddress = "project/alpha"
		bravoAddress = "project/bravo"
	)

	projectRoot := t.TempDir()
	projectDir := filepath.Join(projectRoot, ".lingtai")
	humanDir := filepath.Join(projectDir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	v1BoundaryWriteManifest(t, projectDir, "human", `{
		"agent_id":"human", "agent_name":"Human", "nickname":"Human",
		"address":"project/human", "state":"ACTIVE", "admin":null
	}`)
	orchDir := v1BoundaryWriteManifest(t, projectDir, "orchestrator", `{
		"agent_id":"orchestrator", "agent_name":"Main Orchestrator", "nickname":"Main Orchestrator",
		"address":"project/orchestrator", "state":"ACTIVE", "admin":{}
	}`)
	v1BoundaryWriteManifest(t, projectDir, "agent-a", `{
		"agent_id":"agent-a", "agent_name":"Alpha", "nickname":"Alpha",
		"address":"project/alpha", "state":"STOPPED", "admin":{}
	}`)
	v1BoundaryWriteManifest(t, projectDir, "agent-b", `{
		"agent_id":"agent-b", "agent_name":"Bravo", "nickname":"Bravo",
		"address":"project/bravo", "state":"SUSPENDED", "admin":{}
	}`)
	v1BoundaryWriteManifest(t, projectDir, "unsafe-blank-route", `{
		"agent_id":"unsafe-blank-route", "agent_name":"Unsafe Blank Route", "nickname":"Unsafe Blank Route",
		"address":"", "state":"STOPPED", "admin":{}
	}`)
	v1BoundaryWriteManifest(t, projectDir, "duplicate-one", `{
		"agent_id":"duplicate-id", "agent_name":"Duplicate One", "nickname":"Duplicate One",
		"address":"project/duplicate-one", "state":"STOPPED", "admin":{}
	}`)
	v1BoundaryWriteManifest(t, projectDir, "duplicate-two", `{
		"agent_id":"duplicate-id", "agent_name":"Duplicate Two", "nickname":"Duplicate Two",
		"address":"project/duplicate-two", "state":"STOPPED", "admin":{}
	}`)

	accepted := []fs.MailMessage{
		v1BoundaryIncoming("agent-a", alphaAddress, "alpha-in", "2026-07-22T10:00:00Z", "ALPHA-DIRECT-IN", humanAddress, nil),
		{
			MailboxID:  "alpha-out",
			From:       humanAddress,
			To:         alphaAddress,
			Message:    "ALPHA-DIRECT-OUT",
			ReceivedAt: "2026-07-22T10:01:00Z",
			Delivered:  true,
		},
		v1BoundaryIncoming("agent-b", bravoAddress, "bravo-in", "2026-07-22T10:02:00Z", "BRAVO-DIRECT-IN", humanAddress, nil),
		v1BoundaryIncoming("agent-a", alphaAddress, "alpha-group", "2026-07-22T10:03:00Z", "ALPHA-GROUP-MUST-NOT-LEAK", []interface{}{humanAddress, bravoAddress}, nil),
		v1BoundaryIncoming("agent-a", alphaAddress, "alpha-cc", "2026-07-22T10:04:00Z", "ALPHA-CC-MUST-NOT-LEAK", humanAddress, []string{bravoAddress}),
		v1BoundaryIncoming("agent-b", alphaAddress, "alpha-contradictory", "2026-07-22T10:05:00Z", "ALPHA-CONTRADICTORY-ID-MUST-NOT-LEAK", humanAddress, nil),
	}

	mail := NewMailModel(humanDir, humanAddress, projectDir, orchDir, "Main Orchestrator", 20, "", "en", false, 0)
	mail.generation = 1
	app := App{
		currentView: appViewMail,
		projectDir:  projectDir,
		orchDir:     orchDir,
		orchName:    "Main Orchestrator",
		mail:        mail,
	}
	app, _ = v1BoundaryApply(app, tea.WindowSizeMsg{Width: width, Height: 24})
	cache := fs.NewMailCache(humanDir)
	cache.Messages = accepted
	app, _ = v1BoundaryApply(app, mailRefreshMsg{generation: app.mail.generation, cache: cache})

	mainMessages := make([]ChatMessage, 0, 18)
	for i := 0; i < 17; i++ {
		mainMessages = append(mainMessages, ChatMessage{
			Type:      "mail",
			From:      "Main",
			Body:      fmt.Sprintf("MAIN-HISTORY-%02d", i),
			Timestamp: fmt.Sprintf("2026-07-22T09:%02d:00Z", i),
		})
	}
	mainMessages = append(mainMessages, ChatMessage{
		Type:      "mail",
		From:      "Main",
		Body:      "MAIN-ONLY-TRANSCRIPT",
		Timestamp: "2026-07-22T09:59:00Z",
	})
	app.mail.messages = mainMessages
	app.mail.initialLoading = false
	app.mail.viewport.SetContent(app.mail.renderMessages(app.mail.visibleMessages()))
	app.mail.viewport.GotoBottom()
	return app, humanDir, alphaAddress
}

// TestAgentsDirectConversationBoundaryThroughRealPalette defines the first
// independently shippable V1 product boundary without naming future selector
// or direct-chat implementation types. The only driver is the existing App,
// MailModel, InputModel/palette, accepted MailCache, and filesystem outbox.
func TestAgentsDirectConversationBoundaryThroughRealPalette(t *testing.T) {
	i18n.SetLang("en")

	for _, width := range []int{40, 84, 85, 120} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			app, humanDir, alphaAddress := v1BoundaryFixture(t, width)
			budget := app.layoutBudget()
			if budget.RailVisible || budget.RailWidth != 0 || budget.ContentWidth != width {
				t.Fatalf("V1 width %d layout = %#v; want no rail and full-width Mail content", width, budget)
			}

			mainMessagesBeforeCursor := append([]ChatMessage(nil), app.mail.messages...)
			mainOffsetBeforeCursor := app.mail.viewport.YOffset()
			app = v1BoundaryTypeAgents(t, app)
			selector := ansi.Strip(app.View().Content)
			for _, safe := range []string{"Main", "Main Orchestrator", "Alpha", "Bravo"} {
				if !strings.Contains(selector, safe) {
					t.Errorf("/agents selector at width %d omitted safe row %q:\n%s", width, safe, selector)
				}
			}
			for _, unsafe := range []string{"Human", "Unsafe Blank Route", "Duplicate One", "Duplicate Two"} {
				if strings.Contains(selector, unsafe) {
					t.Errorf("/agents selector at width %d exposed unsafe row %q:\n%s", width, unsafe, selector)
				}
			}

			var cursorCmd tea.Cmd
			app, cursorCmd = v1BoundaryApply(app, tea.KeyPressMsg{Code: tea.KeyDown})
			if cursorCmd != nil {
				t.Error("moving the /agents cursor scheduled activation before Enter")
			}
			if len(v1BoundaryOutbox(t, humanDir)) != 0 {
				t.Error("moving the /agents cursor wrote mail before activation")
			}
			if !reflect.DeepEqual(app.mail.messages, mainMessagesBeforeCursor) ||
				app.mail.viewport.YOffset() != mainOffsetBeforeCursor {
				t.Error("moving the /agents cursor changed the current Main transcript or viewport")
			}
			app, _ = v1BoundaryApply(app, tea.KeyPressMsg{Code: tea.KeyEsc})
			afterCursor := ansi.Strip(app.View().Content)
			if !strings.Contains(afterCursor, "MAIN-ONLY-TRANSCRIPT") ||
				strings.Contains(afterCursor, "ALPHA-DIRECT-IN") {
				t.Errorf("cursor-only movement changed current Main selection at width %d:\n%s", width, afterCursor)
			}

			const mainDraft = "MAIN-DRAFT-MUST-RETURN"
			app.mail.input.SetValue(mainDraft)
			app.mail.syncViewportHeight()
			app.mail.viewport.GotoBottom()
			mainMessages := append([]ChatMessage(nil), app.mail.messages...)
			mainViewport := app.mail.viewport.View()
			mainOffset := app.mail.viewport.YOffset()

			var openCmd tea.Cmd
			app, openCmd = v1BoundaryApply(app, PaletteSelectMsg{Command: "agents"})
			if openCmd != nil {
				t.Error("reopening /agents scheduled activation before Enter")
			}
			app, _ = v1BoundaryApply(app, tea.KeyPressMsg{Code: tea.KeyHome})
			app, _ = v1BoundaryApply(app, tea.KeyPressMsg{Code: tea.KeyDown})
			var activateCmd tea.Cmd
			app, activateCmd = v1BoundaryApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
			if activateCmd == nil {
				t.Error("activating Alpha returned no deferred current-visibility command")
			}

			direct := ansi.Strip(app.View().Content)
			for _, strict := range []string{"ALPHA-DIRECT-IN", "ALPHA-DIRECT-OUT"} {
				if !strings.Contains(direct, strict) {
					t.Errorf("Alpha direct view at width %d omitted strict mail %q:\n%s", width, strict, direct)
				}
			}
			for _, leak := range []string{
				"BRAVO-DIRECT-IN",
				"ALPHA-GROUP-MUST-NOT-LEAK",
				"ALPHA-CC-MUST-NOT-LEAK",
				"ALPHA-CONTRADICTORY-ID-MUST-NOT-LEAK",
				"MAIN-ONLY-TRANSCRIPT",
				mainDraft,
			} {
				if strings.Contains(direct, leak) {
					t.Errorf("Alpha direct view at width %d leaked %q:\n%s", width, leak, direct)
				}
			}

			const directCompose = "ONE-DIRECT-MESSAGE"
			for _, r := range directCompose {
				app, _ = v1BoundaryApply(app, tea.KeyPressMsg{Code: r, Text: string(r)})
			}
			var sendSignalCmd tea.Cmd
			app, sendSignalCmd = v1BoundaryApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
			if sendSignalCmd == nil {
				t.Fatal("pressing Enter in the direct composer produced no SendMsg")
			}
			sendSignal, ok := runCmd(sendSignalCmd).(SendMsg)
			if !ok {
				t.Fatalf("pressing Enter in the direct composer produced %T; want SendMsg", runCmd(sendSignalCmd))
			}
			app, _ = v1BoundaryApply(app, sendSignal)

			outbox := v1BoundaryOutbox(t, humanDir)
			if len(outbox) != 1 {
				t.Fatalf("direct send wrote %d outbox messages; want exactly one", len(outbox))
			}
			if got, want := outbox[0].To, []string{alphaAddress}; !reflect.DeepEqual(got, want) {
				t.Errorf("direct outbox recipients = %#v; want singleton %#v", got, want)
			}
			if len(outbox[0].CC) != 0 {
				t.Errorf("direct outbox cc = %#v; want empty", outbox[0].CC)
			}
			if outbox[0].Message != directCompose {
				t.Errorf("direct outbox body = %q; want %q", outbox[0].Message, directCompose)
			}

			app = v1BoundaryTypeAgents(t, app)
			app, _ = v1BoundaryApply(app, tea.KeyPressMsg{Code: tea.KeyHome})
			app, _ = v1BoundaryApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})

			if app.mail.input.Value() != mainDraft {
				t.Errorf("Main draft after direct return = %q; want %q", app.mail.input.Value(), mainDraft)
			}
			if !reflect.DeepEqual(app.mail.messages, mainMessages) {
				t.Error("Main transcript changed across direct conversation")
			}
			if app.mail.viewport.View() != mainViewport || app.mail.viewport.YOffset() != mainOffset {
				t.Errorf("Main viewport changed across direct conversation: offset=%d want=%d", app.mail.viewport.YOffset(), mainOffset)
			}
			main := ansi.Strip(app.View().Content)
			if !strings.Contains(main, "MAIN-ONLY-TRANSCRIPT") ||
				strings.Contains(main, "ALPHA-DIRECT-IN") ||
				strings.Contains(main, "ALPHA-DIRECT-OUT") {
				t.Errorf("return to Main at width %d did not restore only Main content:\n%s", width, main)
			}
			if len(v1BoundaryOutbox(t, humanDir)) != 1 {
				t.Error("returning to Main wrote an additional outbox message")
			}
		})
	}
}
