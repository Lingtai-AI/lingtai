package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

const (
	directPerformanceMessages  = 1552
	directPerformanceBodyBytes = 640
	directPerformancePageSize  = 200
	directPerformanceHuman     = "performance/human"
)

type directPerformanceFixture struct {
	app     App
	targetA fs.DirectTarget
}

func directPerformancePreparedRefresh(t *testing.T, app App, accepted []fs.MailMessage) (App, tea.Cmd) {
	t.Helper()
	cache := fs.NewMailCache(app.mail.humanDir)
	cache.Messages = append([]fs.MailMessage(nil), accepted...)
	app.mail.cache = cache

	var producer tea.Cmd
	app.mail, producer = app.mail.issueRefreshRequest()
	if producer == nil {
		t.Fatal("real prepared refresh producer returned no command")
	}
	refresh, ok := producer().(mailRefreshMsg)
	if !ok || refresh.refreshRequestSerial == 0 || !refresh.prepared || refresh.directPublication == nil {
		t.Fatalf("real prepared refresh completion = %#v, want nonzero serial/prepared/publication", refresh)
	}
	app, follow := directPerformanceApply(app, refresh)
	if follow == nil {
		t.Fatal("accepted prepared refresh did not start the direct-unread lane")
	}

	message := follow()
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		batch = tea.BatchMsg{func() tea.Msg { return message }}
	}
	var resultCmd tea.Cmd
	for _, child := range batch {
		result, ok := child().(directUnreadResultMsg)
		if !ok {
			continue
		}
		app, resultCmd = directPerformanceApply(app, result)
	}
	if app.mail.directUnread == nil || !app.mail.directPrepared {
		t.Fatalf("prepared refresh did not install V1 direct state: unread=%p prepared=%v",
			app.mail.directUnread, app.mail.directPrepared)
	}
	return app, resultCmd
}

// newDirectPerformanceFixture uses only an accepted in-memory MailCache. The
// human manifest's deliberately future resolved_at is non-stale, so the real
// accepted-refresh location goroutine only reads it and cannot race TempDir
// cleanup by performing a network lookup or manifest write.
func newDirectPerformanceFixture(t *testing.T, pageSize, width, height int, accepted []fs.MailMessage) directPerformanceFixture {
	t.Helper()
	i18n.SetLang("en")

	root := t.TempDir()
	lingtaiDir := filepath.Join(root, ".lingtai")
	humanDir := filepath.Join(lingtaiDir, "human")
	targetA := fs.DirectTarget{
		ProjectDirectory: root,
		Directory:        filepath.Join(lingtaiDir, "agent-a"),
		AgentID:          "agent-a",
		Address:          "performance/agent-a",
	}

	directPerformanceWriteManifest(t, humanDir, "human", "Human", directPerformanceHuman, true)
	directPerformanceWriteManifest(t, targetA.Directory, targetA.AgentID, "Alpha", targetA.Address, false)

	mail := NewMailModel(
		humanDir,
		directPerformanceHuman,
		lingtaiDir,
		"",
		"Main",
		pageSize,
		"",
		"en",
		false,
		0,
	)
	mail.generation = 1
	app := App{
		currentView: appViewMail,
		projectDir:  lingtaiDir,
		mail:        mail,
	}
	app, _ = directPerformanceApply(app, tea.WindowSizeMsg{Width: width, Height: height})

	app, _ = directPerformancePreparedRefresh(t, app, accepted)
	return directPerformanceFixture{app: app, targetA: targetA}
}

func directPerformanceWriteManifest(t *testing.T, directory, agentID, nickname, address string, human bool) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create manifest directory %q: %v", directory, err)
	}
	admin := "{}"
	location := ""
	if human {
		admin = "null"
		location = `,
	"location":{"city":"Test","region":"Test","country":"ZZ","timezone":"Etc/UTC","loc":"0,0","resolved_at":"9999-12-31T23:59:59Z"}`
	}
	manifest := fmt.Sprintf(`{
	"agent_id":%q,
	"agent_name":%q,
	"nickname":%q,
	"address":%q,
	"state":"STOPPED",
	"admin":%s%s
}`, agentID, nickname, nickname, address, admin, location)
	if err := os.WriteFile(filepath.Join(directory, ".agent.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest for %q: %v", agentID, err)
	}
}

func directPerformanceApply(app App, msg tea.Msg) (App, tea.Cmd) {
	model, cmd := app.Update(msg)
	return model.(App), cmd
}

// directPerformanceActivateA deliberately follows the real InputModel/palette
// /agents route and selector keys. It never assigns direct state directly.
func directPerformanceActivateA(t *testing.T, app App, target fs.DirectTarget) App {
	t.Helper()
	if got := app.mail.input.Value(); got != "" {
		t.Fatalf("precondition: compose before /agents = %q, want empty", got)
	}
	for _, r := range "/agents" {
		app, _ = directPerformanceApply(app, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	app, paletteCmd := directPerformanceApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if paletteCmd == nil {
		t.Fatal("typing /agents produced no palette selection command")
	}
	selected, ok := paletteCmd().(PaletteSelectMsg)
	if !ok || selected.Command != "agents" {
		t.Fatalf("typing /agents produced %#v, want PaletteSelectMsg for agents", selected)
	}
	app, _ = directPerformanceApply(app, selected)
	if !app.mail.agentSelector.selectorOpen {
		t.Fatal("real /agents route did not open the Mail-owned selector")
	}

	index := -1
	for candidate, row := range app.mail.agentSelector.rows {
		if !row.Main && row.Target.AgentID == target.AgentID {
			index = candidate
			break
		}
	}
	if index < 0 {
		t.Fatalf("/agents selector did not publish target %q", target.AgentID)
	}
	app, _ = directPerformanceApply(app, tea.KeyPressMsg{Code: tea.KeyHome})
	for range index {
		app, _ = directPerformanceApply(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	app, visibilityCmd := directPerformanceApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if visibilityCmd == nil {
		t.Fatal("real /agents activation produced no deferred visibility command")
	}
	if app.mail.agentSelector.selectorOpen {
		t.Fatal("real /agents activation left selector open")
	}
	if current, ok := app.mail.currentDirectTarget(); !ok || current.AgentID != target.AgentID {
		t.Fatalf("real /agents activation current target = %#v (current=%v), want %q", current, ok, target.AgentID)
	}
	return app
}

func directPerformanceMarker(index int) string {
	return fmt.Sprintf("DIRECT-PERF-%04d", index)
}

func directPerformanceBody(index int) string {
	marker := directPerformanceMarker(index) + " "
	return marker + strings.Repeat("x", directPerformanceBodyBytes-len(marker))
}

func directPerformanceIncoming(target fs.DirectTarget, index int, body string) fs.MailMessage {
	return fs.MailMessage{
		MailboxID:  fmt.Sprintf("direct-perf-%04d", index),
		From:       target.Address,
		To:         directPerformanceHuman,
		Message:    body,
		ReceivedAt: time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC).Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano),
		Identity:   map[string]interface{}{"agent_id": target.AgentID},
		Delivered:  true,
	}
}

func directPerformanceProjectionBytes(messages []ChatMessage) int {
	bytes := 0
	for _, message := range messages {
		bytes += len(message.Body)
	}
	return bytes
}

// TestDirectFirstPaintIsBoundedToConfiguredMailPageSize defines the direct
// first-paint presentation horizon without truncating the accepted snapshot.
func TestDirectFirstPaintIsBoundedToConfiguredMailPageSize(t *testing.T) {
	target := fs.DirectTarget{AgentID: "agent-a", Address: "performance/agent-a"}
	accepted := make([]fs.MailMessage, 0, directPerformanceMessages)
	for index := range directPerformanceMessages {
		accepted = append(accepted, directPerformanceIncoming(target, index, directPerformanceBody(index)))
	}

	fixture := newDirectPerformanceFixture(t, directPerformancePageSize, 100, 24, accepted)
	app := directPerformanceActivateA(t, fixture.app, fixture.targetA)
	projection := app.mail.directChat.projection
	projectionBytes := directPerformanceProjectionBytes(projection)
	wantBytes := directPerformancePageSize * directPerformanceBodyBytes
	if len(projection) != directPerformancePageSize || projectionBytes != wantBytes {
		t.Errorf("current direct render projection = %d messages / %d body bytes; want newest mail_page_size = %d messages / %d body bytes",
			len(projection), projectionBytes, directPerformancePageSize, wantBytes)
	}

	firstExpected := directPerformanceMessages - directPerformancePageSize
	for offset, message := range projection {
		wantMarker := directPerformanceMarker(firstExpected + offset)
		if !strings.Contains(message.Body, wantMarker) {
			t.Errorf("direct render projection marker at offset %d = %q; want newest chronological marker %q", offset, message.Body, wantMarker)
			break
		}
	}
	if len(projection) > 0 && !strings.Contains(projection[0].Body, directPerformanceMarker(firstExpected)) {
		t.Errorf("oldest direct render marker = %q; want %q", directPerformanceMarker(0), directPerformanceMarker(firstExpected))
	}

	view := app.mail.activeViewportView()
	if !strings.Contains(view, directPerformanceMarker(directPerformanceMessages-1)) {
		t.Errorf("current direct view omitted newest marker %q", directPerformanceMarker(directPerformanceMessages-1))
	}
	if strings.Contains(view, directPerformanceMarker(0)) {
		t.Errorf("current direct view included oldest full-history marker %q", directPerformanceMarker(0))
	}
	if got := len(app.mail.acceptedSnapshot.messagesForUnread(app.mail.humanDir)); got != directPerformanceMessages {
		t.Errorf("full accepted snapshot = %d messages; want retained full %d", got, directPerformanceMessages)
	}
}

// TestDirectViewDoesNotReRenderHiddenProjectionPrefix proves that a read-only
// direct View consumes already-installed viewport content. The mutation is
// intentionally unpublished: no refresh, activation, resize, paging, or scroll
// path is allowed between the two byte comparisons.
func TestDirectViewDoesNotReRenderHiddenProjectionPrefix(t *testing.T) {
	const messageCount = 64
	target := fs.DirectTarget{AgentID: "agent-a", Address: "performance/agent-a"}
	accepted := make([]fs.MailMessage, 0, messageCount)
	for index := range messageCount {
		accepted = append(accepted, directPerformanceIncoming(target, index, directPerformanceMarker(index)))
	}

	fixture := newDirectPerformanceFixture(t, directPerformancePageSize, 90, 12, accepted)
	app := directPerformanceActivateA(t, fixture.app, fixture.targetA)
	baseline := app.mail.activeViewportView()
	if !strings.Contains(baseline, directPerformanceMarker(messageCount-1)) {
		t.Fatalf("tail-view precondition omitted newest marker %q", directPerformanceMarker(messageCount-1))
	}
	if strings.Contains(baseline, directPerformanceMarker(0)) {
		t.Fatalf("tail-view precondition included hidden first marker %q", directPerformanceMarker(0))
	}

	app.mail.directChat.projection[0].Body = strings.Repeat("UNPUBLISHED-HIDDEN-PREFIX-POISON\n", 64)
	if got := app.mail.activeViewportView(); got != baseline {
		t.Errorf("activeViewportView changed after an unpublished hidden-prefix mutation; View must consume the installed current viewport without render/SetContent work")
	}
}
