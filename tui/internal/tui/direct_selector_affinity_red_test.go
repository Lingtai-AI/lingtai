package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

const (
	directAffinityHumanAddress = "affinity/human"
	directAffinityMainName     = "Affinity Main Orchestrator"
)

type directAffinityFixture struct {
	app      App
	root     string
	lingtai  string
	humanDir string
	targetA  fs.DirectTarget
	targetB  fs.DirectTarget
}

type directAffinityOutboxMessage struct {
	To      []string `json:"to"`
	Message string   `json:"message"`
}

func newDirectAffinityFixture(t *testing.T, withOrchestrator bool) directAffinityFixture {
	t.Helper()
	i18n.SetLang("en")

	root := t.TempDir()
	lingtai := filepath.Join(root, ".lingtai")
	humanDir := filepath.Join(lingtai, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatalf("create human directory: %v", err)
	}
	directAffinityWriteManifest(t, humanDir, "human", "Human", directAffinityHumanAddress, true)

	targetA := fs.DirectTarget{
		ProjectDirectory: root,
		Directory:        filepath.Join(lingtai, "agent-a"),
		AgentID:          "agent-a",
		Address:          "affinity/alpha",
	}
	targetB := fs.DirectTarget{
		ProjectDirectory: root,
		Directory:        filepath.Join(lingtai, "agent-b"),
		AgentID:          "agent-b",
		Address:          "affinity/bravo",
	}
	directAffinityWriteManifest(t, targetA.Directory, targetA.AgentID, "Alpha", targetA.Address, false)
	directAffinityWriteManifest(t, targetB.Directory, targetB.AgentID, "Bravo", targetB.Address, false)

	orchDir := ""
	orchName := "Main"
	if withOrchestrator {
		orchDir = filepath.Join(lingtai, "orchestrator")
		orchName = directAffinityMainName
		directAffinityWriteManifest(t, orchDir, "orchestrator", orchName, "affinity/orchestrator", false)
	}

	mail := NewMailModel(
		humanDir,
		directAffinityHumanAddress,
		lingtai,
		orchDir,
		orchName,
		200,
		"",
		"en",
		false,
		0,
	)
	mail.generation = 73
	app := App{
		currentView: appViewMail,
		projectDir:  lingtai,
		orchDir:     orchDir,
		orchName:    orchName,
		mail:        mail,
	}
	app, _ = directAffinityApply(app, tea.WindowSizeMsg{Width: 180, Height: 30})

	initial := []fs.MailMessage{
		directAffinityIncoming(targetA, "alpha-baseline", "2026-07-23T10:00:00Z", "alpha baseline"),
		directAffinityIncoming(targetB, "bravo-baseline", "2026-07-23T10:01:00Z", "bravo baseline"),
	}
	app, _ = directAffinityPublish(t, app, initial)
	app.mail.initialLoading = false

	return directAffinityFixture{
		app:      app,
		root:     root,
		lingtai:  lingtai,
		humanDir: humanDir,
		targetA:  targetA,
		targetB:  targetB,
	}
}

func directAffinityWriteManifest(t *testing.T, directory, agentID, nickname, address string, human bool) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create agent directory %q: %v", directory, err)
	}
	admin := "{}"
	location := ""
	if human {
		admin = "null"
		location = fmt.Sprintf(",\n\t\t\"location\":{\"resolved_at\":%q}", time.Now().UTC().Format(time.RFC3339))
	}
	body := fmt.Sprintf(`{
		"agent_id":%q,
		"agent_name":%q,
		"nickname":%q,
		"address":%q,
		"state":"STOPPED",
		"admin":%s%s
	}`, agentID, nickname, nickname, address, admin, location)
	if err := os.WriteFile(filepath.Join(directory, ".agent.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write agent manifest %q: %v", agentID, err)
	}
}

func directAffinityIncoming(target fs.DirectTarget, mailboxID, receivedAt, body string) fs.MailMessage {
	return fs.MailMessage{
		MailboxID:  mailboxID,
		From:       target.Address,
		To:         directAffinityHumanAddress,
		Message:    body,
		ReceivedAt: receivedAt,
		Identity:   map[string]interface{}{"agent_id": target.AgentID},
		Delivered:  true,
	}
}

func directAffinityCache(humanDir string, messages []fs.MailMessage) fs.MailCache {
	cache := fs.NewMailCache(humanDir)
	cache.Messages = append([]fs.MailMessage(nil), messages...)
	return cache
}

func directAffinityApply(app App, msg tea.Msg) (App, tea.Cmd) {
	model, cmd := app.Update(msg)
	return model.(App), cmd
}

func directAffinityPublish(t *testing.T, app App, accepted []fs.MailMessage) (App, tea.Cmd) {
	t.Helper()
	return directPerformancePreparedRefresh(t, app, accepted)
}

// directAffinityRequirePreparedRefresh keeps the pre-Green RED compile-valid
// while proving that the same ctrl+r command becomes the real detached prepared
// producer at Green: one nonzero request serial, prepared=true, and one
// immutable direct publication. Reflection is test-only because those fields
// are intentionally introduced by the Green whose chronology this RED guards.
func directAffinityRequirePreparedRefresh(t *testing.T, refresh mailRefreshMsg) {
	t.Helper()
	value := reflect.ValueOf(refresh)
	serial := value.FieldByName("refreshRequestSerial")
	if !serial.IsValid() {
		t.Error("real ctrl+r refresh completion has no refreshRequestSerial field")
	} else if serial.Kind() != reflect.Uint64 || serial.Uint() == 0 {
		t.Errorf("real ctrl+r refresh serial = %v, want a nonzero uint64", serial)
	}
	prepared := value.FieldByName("prepared")
	if !prepared.IsValid() {
		t.Error("real ctrl+r refresh completion has no prepared field")
	} else if prepared.Kind() != reflect.Bool || !prepared.Bool() {
		t.Errorf("real ctrl+r refresh prepared = %v, want true", prepared)
	}
	publication := value.FieldByName("directPublication")
	if !publication.IsValid() {
		t.Error("real ctrl+r refresh completion has no directPublication field")
	} else if publication.Kind() != reflect.Ptr || publication.IsNil() {
		t.Errorf("real ctrl+r refresh publication = %v, want non-nil pointer", publication)
	}
}

// directAffinityOpenAgents takes the real App /agents route. Tests with an
// empty compose type through InputModel and the command palette; tests that
// deliberately preserve a Main draft feed the same PaletteSelectMsg that the
// real palette emits, so opening the selector does not overwrite that draft.
func directAffinityOpenAgents(t *testing.T, app App) App {
	t.Helper()
	if app.mail.agentSelector.selectorOpen {
		t.Fatal("/agents selector already open")
	}
	if app.mail.input.Value() == "" {
		for _, r := range "/agents" {
			app, _ = directAffinityApply(app, tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		var selectCmd tea.Cmd
		app, selectCmd = directAffinityApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
		if selectCmd == nil {
			t.Fatal("typing /agents produced no palette selection command")
		}
		selected, ok := runCmd(selectCmd).(PaletteSelectMsg)
		if !ok || selected.Command != "agents" {
			t.Fatalf("typing /agents produced %#v, want PaletteSelectMsg for agents", selected)
		}
		app, _ = directAffinityApply(app, selected)
	} else {
		app, _ = directAffinityApply(app, PaletteSelectMsg{Command: "agents"})
	}
	if !app.mail.agentSelector.selectorOpen {
		t.Fatal("real /agents route did not open the Mail-owned selector")
	}
	return app
}

func directAffinityRowIndex(t *testing.T, mail MailModel, agentID string) int {
	t.Helper()
	for index, row := range mail.agentSelector.rows {
		if agentID == "" && row.Main || !row.Main && row.Target.AgentID == agentID {
			return index
		}
	}
	t.Fatalf("/agents has no row for agent %q", agentID)
	return -1
}

func directAffinityCursorAgent(t *testing.T, mail MailModel) string {
	t.Helper()
	if mail.agentSelector.cursor < 0 || mail.agentSelector.cursor >= len(mail.agentSelector.rows) {
		t.Fatalf("/agents cursor %d outside %d accepted rows", mail.agentSelector.cursor, len(mail.agentSelector.rows))
	}
	row := mail.agentSelector.rows[mail.agentSelector.cursor]
	if row.Main {
		return ""
	}
	return row.Target.AgentID
}

func directAffinityMoveCursor(t *testing.T, app App, agentID string) App {
	t.Helper()
	index := directAffinityRowIndex(t, app.mail, agentID)
	app, _ = directAffinityApply(app, tea.KeyPressMsg{Code: tea.KeyHome})
	for range index {
		app, _ = directAffinityApply(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if got := directAffinityCursorAgent(t, app.mail); got != agentID {
		t.Fatalf("/agents cursor = %q, want %q", got, agentID)
	}
	return app
}

func directAffinityActivate(t *testing.T, app App, agentID string) (App, tea.Cmd) {
	t.Helper()
	app = directAffinityOpenAgents(t, app)
	app = directAffinityMoveCursor(t, app, agentID)
	app, cmd := directAffinityApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.mail.agentSelector.selectorOpen {
		t.Fatal("/agents Enter left the selector open")
	}
	return app, cmd
}

func directAffinityVisibilityFromCmd(t *testing.T, cmd tea.Cmd, context string) (directVisibilityMsg, bool) {
	t.Helper()
	if cmd == nil {
		t.Errorf("%s: no immediate direct visibility retry command", context)
		return directVisibilityMsg{}, false
	}
	msg := runCmd(cmd)
	if visibility, ok := msg.(directVisibilityMsg); ok {
		return visibility, true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			if child == nil {
				continue
			}
			if visibility, ok := runCmd(child).(directVisibilityMsg); ok {
				return visibility, true
			}
		}
	}
	t.Errorf("%s: command produced %T without directVisibilityMsg", context, msg)
	return directVisibilityMsg{}, false
}

func directAffinityApplyPreparedResult(t *testing.T, app App, cmd tea.Cmd, context string) App {
	t.Helper()
	if cmd == nil {
		t.Fatalf("%s: no prepared direct-unread command", context)
	}
	msg := runCmd(cmd)
	if result, ok := msg.(directUnreadResultMsg); ok {
		app, _ = directAffinityApply(app, result)
		return app
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		count := 0
		for _, child := range batch {
			if child == nil {
				continue
			}
			result, ok := runCmd(child).(directUnreadResultMsg)
			if !ok {
				continue
			}
			count++
			app, _ = directAffinityApply(app, result)
		}
		if count == 1 {
			return app
		}
		t.Fatalf("%s: prepared command produced %d directUnreadResultMsg values, want exactly 1", context, count)
	}
	t.Fatalf("%s: prepared command produced %T, want directUnreadResultMsg", context, msg)
	return app
}

func directAffinityUnread(t *testing.T, mail MailModel, target fs.DirectTarget, accepted []fs.MailMessage) int {
	t.Helper()
	if mail.directUnread == nil {
		t.Fatal("accepted refresh did not retain the Mail-owned DirectUnreadStore")
	}
	count, err := mail.directUnread.UnreadCount(target, accepted)
	if err != nil {
		t.Fatalf("UnreadCount(%q): %v", target.AgentID, err)
	}
	return count
}

func directAffinityOutbox(t *testing.T, humanDir string) []directAffinityOutboxMessage {
	t.Helper()
	outboxDir := filepath.Join(humanDir, "mailbox", "outbox")
	entries, err := os.ReadDir(outboxDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read pseudo-human outbox: %v", err)
	}
	messages := make([]directAffinityOutboxMessage, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(outboxDir, entry.Name(), "message.json"))
		if err != nil {
			t.Fatalf("read outbox message %q: %v", entry.Name(), err)
		}
		var message directAffinityOutboxMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			t.Fatalf("decode outbox message %q: %v", entry.Name(), err)
		}
		messages = append(messages, message)
	}
	return messages
}

// directAffinityEditorResult keeps the RED compile-clean before EditorDoneMsg
// gains current-direct affinity. It fills the future fields only when they
// exist; a zero-field legacy result remains Main-owned.
func directAffinityEditorResult(mail MailModel, projectRoot, threadKey string, directGeneration uint64, text string) EditorDoneMsg {
	msg := EditorDoneMsg{Text: text, Generation: mail.generation}
	value := reflect.ValueOf(&msg).Elem()
	for name, fieldValue := range map[string]string{
		"ProjectRoot":     projectRoot,
		"DirectThreadKey": threadKey,
	} {
		field := value.FieldByName(name)
		if field.IsValid() && field.CanSet() && field.Kind() == reflect.String {
			field.SetString(fieldValue)
		}
	}
	field := value.FieldByName("DirectGeneration")
	if field.IsValid() && field.CanSet() && field.Kind() == reflect.Uint64 {
		field.SetUint(directGeneration)
	}
	return msg
}

func directAffinityChromeLines(t *testing.T, view string) (string, string) {
	t.Helper()
	var title, email string
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		if title == "" && strings.Contains(line, i18n.T("app.brand")) {
			title = line
		}
		if email == "" && strings.Contains(line, "Email To:") {
			email = line
		}
	}
	if title == "" || email == "" {
		t.Fatalf("could not isolate title and Email To lines:\ntitle=%q\nemail=%q", title, email)
	}
	return title, email
}

// TestDirectV1AcceptedSelectorPreservesCursorIdentity requires accepted
// catalog replacement to preserve the cursor's stable identity independently
// of the current conversation. The old numeric index is intentionally inherited
// by B after A's mutable label reorders.
func TestDirectV1AcceptedSelectorPreservesCursorIdentity(t *testing.T) {
	t.Run("current B and cursor A stay independent", func(t *testing.T) {
		fixture := newDirectAffinityFixture(t, false)
		app, _ := directAffinityActivate(t, fixture.app, fixture.targetB.AgentID)
		keyB := fs.DirectThreadKey(fixture.targetB)

		app = directAffinityOpenAgents(t, app)
		app = directAffinityMoveCursor(t, app, fixture.targetA.AgentID)
		app, _ = directAffinityApply(app, tea.KeyPressMsg{Code: tea.KeyEsc})
		if got := app.mail.agentSelector.selectedThreadKey; got != keyB {
			t.Fatalf("fixture current key = %q, want B %q", got, keyB)
		}

		directAffinityWriteManifest(t, fixture.targetA.Directory, fixture.targetA.AgentID, "Zulu", fixture.targetA.Address, false)
		accepted := app.mail.acceptedSnapshot.messagesForUnread(app.mail.humanDir)
		app, _ = directAffinityPublish(t, app, accepted)

		if got := app.mail.agentSelector.selectedThreadKey; got != keyB {
			t.Errorf("accepted reorder changed current B key to %q", got)
		}
		if got := directAffinityCursorAgent(t, app.mail); got != fixture.targetA.AgentID {
			t.Errorf("accepted reorder moved independent cursor to %q, want stable A", got)
		}

		app = directAffinityOpenAgents(t, app)
		app, _ = directAffinityApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
		target, ok := app.mail.currentDirectTarget()
		if !ok || target.AgentID != fixture.targetA.AgentID {
			t.Errorf("/agents Enter after reorder activated %#v, want visually selected stable A", target)
		}
	})

	t.Run("Main current and cursor A survive label reorder", func(t *testing.T) {
		fixture := newDirectAffinityFixture(t, false)
		app := directAffinityOpenAgents(t, fixture.app)
		app = directAffinityMoveCursor(t, app, fixture.targetA.AgentID)
		app, _ = directAffinityApply(app, tea.KeyPressMsg{Code: tea.KeyEsc})
		if app.mail.agentSelector.selectedThreadKey != "" {
			t.Fatalf("fixture current = %q, want Main", app.mail.agentSelector.selectedThreadKey)
		}

		directAffinityWriteManifest(t, fixture.targetA.Directory, fixture.targetA.AgentID, "Zulu", fixture.targetA.Address, false)
		accepted := app.mail.acceptedSnapshot.messagesForUnread(app.mail.humanDir)
		app, _ = directAffinityPublish(t, app, accepted)

		if app.mail.agentSelector.selectedThreadKey != "" {
			t.Errorf("accepted reorder changed Main current to %q", app.mail.agentSelector.selectedThreadKey)
		}
		if got := directAffinityCursorAgent(t, app.mail); got != fixture.targetA.AgentID {
			t.Errorf("accepted reorder retargeted Main-owned cursor to %q, want stable A", got)
		}

		app = directAffinityOpenAgents(t, app)
		app, _ = directAffinityApply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
		target, ok := app.mail.currentDirectTarget()
		if !ok || target.AgentID != fixture.targetA.AgentID {
			t.Errorf("/agents Enter after Main-current reorder activated %#v, want stable A", target)
		}
	})
}

// TestDirectV1AcceptedRefreshReconcilesCurrentTarget requires a selected stable
// key to rebind to its latest accepted safe route, then fail closed to Main
// when that row disappears. The durable U cursor remains keyed by stable A.
func TestDirectV1AcceptedRefreshReconcilesCurrentTarget(t *testing.T) {
	fixture := newDirectAffinityFixture(t, false)
	app := fixture.app
	accepted := app.mail.acceptedSnapshot.messagesForUnread(app.mail.humanDir)
	accepted = append(accepted, directAffinityIncoming(
		fixture.targetA,
		"alpha-before-rebind",
		"2026-07-23T10:02:00Z",
		"unread before route rebind",
	))
	app, _ = directAffinityPublish(t, app, accepted)
	if got := directAffinityUnread(t, app.mail, fixture.targetA, accepted); got != 1 {
		t.Fatalf("precondition: A unread = %d, want 1", got)
	}

	const mainCompose = "MAIN COMPOSE SURVIVES DIRECT ROUTE LOSS"
	app.mail.input.SetValue(mainCompose)
	app.mail.pendingMessage = ""
	app, _ = directAffinityActivate(t, app, fixture.targetA.AgentID)
	if got := app.mail.input.Value(); got != "" {
		t.Fatalf("A activation input = %q, want fresh direct compose", got)
	}

	if err := os.RemoveAll(fixture.targetA.Directory); err != nil {
		t.Fatalf("remove old A route: %v", err)
	}
	rebound := fs.DirectTarget{
		ProjectDirectory: fixture.root,
		Directory:        filepath.Join(fixture.lingtai, "agent-a-rebound"),
		AgentID:          fixture.targetA.AgentID,
		Address:          "affinity/alpha-rebound",
	}
	directAffinityWriteManifest(t, rebound.Directory, rebound.AgentID, "Alpha Rebound", rebound.Address, false)
	accepted = append(accepted, directAffinityIncoming(
		rebound,
		"alpha-after-rebind",
		"2026-07-23T10:03:00Z",
		"unread on rebound route",
	))
	app, _ = directAffinityPublish(t, app, accepted)

	catalogTarget := app.mail.agentSelector.rows[directAffinityRowIndex(t, app.mail, rebound.AgentID)].Target
	if !reflect.DeepEqual(catalogTarget, rebound) {
		t.Fatalf("accepted catalog target = %#v, want rebound route %#v", catalogTarget, rebound)
	}
	current, currentOK := app.mail.currentDirectTarget()
	if !currentOK || !reflect.DeepEqual(current, rebound) {
		t.Errorf("accepted refresh kept stale current route %#v (current=%v), want rebound %#v", current, currentOK, rebound)
	}

	app.mail.input.SetValue("SEND THROUGH REBOUND ROUTE")
	app.mail.pendingMessage = ""
	var reboundSendCmd tea.Cmd
	app.mail, reboundSendCmd = app.mail.Update(SendMsg{})
	reboundOutbox := directAffinityOutbox(t, fixture.humanDir)
	if reboundSendCmd == nil {
		t.Error("send through accepted rebound route returned no refresh command")
	}
	if len(reboundOutbox) != 1 {
		t.Fatalf("rebound send wrote %d outbox messages, want 1", len(reboundOutbox))
	}
	if got, want := reboundOutbox[0].To, []string{rebound.Address}; !reflect.DeepEqual(got, want) {
		t.Errorf("rebound send recipients = %#v, want latest accepted route %#v", got, want)
	}

	app.mail.input.SetValue("STALE REMOVED A MUST NOT SEND")
	app.mail.pendingMessage = ""
	if err := os.RemoveAll(rebound.Directory); err != nil {
		t.Fatalf("remove rebound A route: %v", err)
	}
	app, _ = directAffinityPublish(t, app, accepted)

	if app.mail.agentSelector.selectedThreadKey != "" || app.mail.directChat.threadKey != "" {
		t.Errorf("removed A did not fail closed to Main: selected=%q directKey=%q directAgent=%q directAddress=%q",
			app.mail.agentSelector.selectedThreadKey,
			app.mail.directChat.threadKey,
			app.mail.directChat.target.AgentID,
			app.mail.directChat.target.Address)
	}
	if target, ok := app.mail.currentDirectTarget(); ok {
		t.Errorf("removed A retained stale current direct target %#v", target)
	}
	if got := app.mail.input.Value(); got != mainCompose {
		t.Errorf("removed A restored compose %q, want exact Main compose %q", got, mainCompose)
	}
	if app.mail.directUnread == nil {
		t.Error("removed A discarded the one Mail-owned durable unread store")
	} else if got := directAffinityUnread(t, app.mail, rebound, accepted); got != 1 {
		t.Errorf("removed A durable unread = %d, want retained 1 on stable A key", got)
	}
	statePath := filepath.Join(fixture.lingtai, ".tui-asset", "direct-unread.json")
	if raw, err := os.ReadFile(statePath); err != nil {
		t.Errorf("read retained direct unread state: %v", err)
	} else if !strings.Contains(string(raw), fs.DirectThreadKey(rebound)) {
		t.Errorf("removed A pruned stable unread key %q from %s", fs.DirectThreadKey(rebound), statePath)
	}

	beforeStaleSend := len(directAffinityOutbox(t, fixture.humanDir))
	var staleSendCmd tea.Cmd
	app.mail, staleSendCmd = app.mail.Update(SendMsg{})
	if staleSendCmd != nil {
		t.Error("removed A stale compose scheduled direct refresh work")
	}
	if got := len(directAffinityOutbox(t, fixture.humanDir)); got != beforeStaleSend {
		t.Errorf("removed A stale compose created outbox work: before=%d after=%d", beforeStaleSend, got)
	}
}

// TestDirectV1RouteRemovalClearsPendingEditorWarning requires accepted route
// removal to discard a warning-owned direct draft before restoring Main. The
// warning must not survive long enough to launch that draft under Main context.
func TestDirectV1RouteRemovalClearsPendingEditorWarning(t *testing.T) {
	fixture := newDirectAffinityFixture(t, false)
	app := fixture.app

	const mainCompose = "MAIN COMPOSE SURVIVES EDITOR WARNING ROUTE LOSS"
	app.mail.input.SetValue(mainCompose)
	app.mail.pendingMessage = ""
	app, _ = directAffinityActivate(t, app, fixture.targetA.AgentID)
	if _, ok := app.mail.currentDirectTarget(); !ok {
		t.Fatal("precondition: direct A is not current")
	}

	// Issue the real serialized ctrl+r producer before the warning owns all key
	// input. Execute it only after removing A so its detached discovery observes
	// the route loss while the warning is still open.
	var refreshCmd tea.Cmd
	app.mail, refreshCmd = directOrderingIssueRefresh(t, app.mail)

	const directWarningText = "DIRECT A WARNING DRAFT MUST NOT BECOME MAIN"
	app, _ = directAffinityApply(app, OpenEditorMsg{Text: directWarningText})
	if !app.mail.showEditorWarn || app.mail.editorWarnText != directWarningText {
		t.Fatalf("precondition: editor warning = (%v, %q), want (true, %q)",
			app.mail.showEditorWarn, app.mail.editorWarnText, directWarningText)
	}

	if err := os.RemoveAll(fixture.targetA.Directory); err != nil {
		t.Fatalf("remove current A route: %v", err)
	}
	refreshRaw := directOrderingRunRefresh(t, refreshCmd, "route-removal request")
	refresh, ok := refreshRaw.(mailRefreshMsg)
	if !ok {
		t.Fatalf("route-removal request produced %T, want mailRefreshMsg", refreshRaw)
	}
	directAffinityRequirePreparedRefresh(t, refresh)
	app, _ = directAffinityApply(app, refresh)

	if _, ok := app.mail.currentDirectTarget(); ok {
		t.Fatal("removed A retained a current direct target")
	}
	if got := app.mail.input.Value(); got != mainCompose {
		t.Fatalf("removed A restored Main compose %q, want %q", got, mainCompose)
	}
	if app.mail.showEditorWarn {
		t.Error("removed A left the direct editor warning open in Main")
	}
	if got := app.mail.editorWarnText; got != "" {
		t.Errorf("removed A retained warning-owned direct text in Main: %q", got)
	}
}

// TestDirectV1EditorCompletionRequiresCurrentContext fixes editor completion to
// the exact Main/direct context captured at launch, not merely the enclosing
// Mail activation generation.
func TestDirectV1EditorCompletionRequiresCurrentContext(t *testing.T) {
	t.Run("A completion cannot populate or send through B", func(t *testing.T) {
		fixture := newDirectAffinityFixture(t, false)
		app, _ := directAffinityActivate(t, fixture.app, fixture.targetA.AgentID)
		mailGeneration := app.mail.generation
		aProjectRoot := app.mail.directChat.target.ProjectDirectory
		aThreadKey := app.mail.directChat.threadKey
		aDirectGeneration := app.mail.directChat.generation

		app, _ = directAffinityActivate(t, app, fixture.targetB.AgentID)
		if app.mail.generation != mailGeneration {
			t.Fatalf("A -> B changed Mail generation from %d to %d", mailGeneration, app.mail.generation)
		}
		app.mail.input.Reset()
		app.mail.pendingMessage = ""

		staleText := "EDITOR RESULT CAPTURED FOR A"
		staleA := directAffinityEditorResult(app.mail, aProjectRoot, aThreadKey, aDirectGeneration, staleText)
		app, _ = directAffinityApply(app, staleA)
		if got := app.mail.input.Value(); got != "" {
			t.Errorf("stale A editor completion populated B input with %q", got)
		}
		if got := app.mail.pendingMessage; got != "" {
			t.Errorf("stale A editor completion populated B pending message with %q", got)
		}
		beforeSend := len(directAffinityOutbox(t, fixture.humanDir))
		app, sendCmd := directAffinityApply(app, SendMsg{})
		if sendCmd != nil || len(directAffinityOutbox(t, fixture.humanDir)) != beforeSend {
			t.Error("stale A editor completion became sendable through B")
		}

		app.mail.input.Reset()
		app.mail.pendingMessage = ""
		currentText := "EDITOR RESULT CAPTURED FOR CURRENT B"
		currentB := directAffinityEditorResult(
			app.mail,
			app.mail.directChat.target.ProjectDirectory,
			app.mail.directChat.threadKey,
			app.mail.directChat.generation,
			currentText,
		)
		app, _ = directAffinityApply(app, currentB)
		if got := app.mail.input.Value(); got != currentText {
			t.Errorf("exact current B editor completion input = %q, want %q", got, currentText)
		}
		if got := app.mail.pendingMessage; got != currentText {
			t.Errorf("exact current B editor completion pending = %q, want %q", got, currentText)
		}
	})

	t.Run("legacy result is Main-owned and direct tag cannot return to Main", func(t *testing.T) {
		fixture := newDirectAffinityFixture(t, false)
		app := fixture.app

		mainText := "LEGACY MAIN EDITOR RESULT"
		mainResult := directAffinityEditorResult(app.mail, "", "", 0, mainText)
		app, _ = directAffinityApply(app, mainResult)
		if got := app.mail.pendingMessage; got != mainText {
			t.Fatalf("existing Main editor generation behavior lost result: got %q, want %q", got, mainText)
		}
		app.mail.input.Reset()
		app.mail.pendingMessage = ""

		legacyCapturedOnMain := directAffinityEditorResult(app.mail, "", "", 0, "LATE MAIN RESULT")
		app, _ = directAffinityActivate(t, app, fixture.targetA.AgentID)
		app, _ = directAffinityApply(app, legacyCapturedOnMain)
		if got := app.mail.input.Value(); got != "" {
			t.Errorf("untagged Main editor result became direct A input %q", got)
		}
		if got := app.mail.pendingMessage; got != "" {
			t.Errorf("untagged Main editor result became direct A pending %q", got)
		}

		app.mail.input.Reset()
		app.mail.pendingMessage = ""
		directCapturedOnA := directAffinityEditorResult(
			app.mail,
			app.mail.directChat.target.ProjectDirectory,
			app.mail.directChat.threadKey,
			app.mail.directChat.generation,
			"LATE DIRECT A RESULT",
		)
		app, _ = directAffinityActivate(t, app, "")
		if got := app.mail.input.Value(); got != "" {
			t.Fatalf("Main restoration fixture input = %q, want empty", got)
		}
		app, _ = directAffinityApply(app, directCapturedOnA)
		if got := app.mail.input.Value(); got != "" {
			t.Errorf("direct-tagged A editor result became Main input %q", got)
		}
		if got := app.mail.pendingMessage; got != "" {
			t.Errorf("direct-tagged A editor result became Main pending %q", got)
		}
	})
}

// TestDirectV1PaletteCancelRetriesVisibilityImmediately distinguishes an
// immediate visibility retry on palette Esc from the eventual one-second mail
// refresh. The rejected obscured coordinate is never replayed.
func TestDirectV1PaletteCancelRetriesVisibilityImmediately(t *testing.T) {
	fixture := newDirectAffinityFixture(t, false)
	app := fixture.app
	accepted := app.mail.acceptedSnapshot.messagesForUnread(app.mail.humanDir)
	accepted = append(accepted, directAffinityIncoming(
		fixture.targetA,
		"alpha-palette-unread",
		"2026-07-23T10:02:00Z",
		"unread while palette obscures transcript",
	))
	app, _ = directAffinityPublish(t, app, accepted)
	if got := directAffinityUnread(t, app.mail, fixture.targetA, accepted); got != 1 {
		t.Fatalf("precondition: A unread = %d, want 1", got)
	}

	var activationCmd tea.Cmd
	app, activationCmd = directAffinityActivate(t, app, fixture.targetA.AgentID)
	activationVisibility, ok := directAffinityVisibilityFromCmd(t, activationCmd, "A activation")
	if !ok {
		return
	}

	app, _ = directAffinityApply(app, tea.KeyPressMsg{Code: '/', Text: "/"})
	if !app.mail.input.IsPaletteActive() {
		t.Fatal("slash did not activate the Mail input palette")
	}
	app, _ = directAffinityApply(app, activationVisibility)
	if got := directAffinityUnread(t, app.mail, fixture.targetA, accepted); got != 1 {
		t.Errorf("obscured activation coordinate cleared unread to %d, want retained 1", got)
	}

	var retryCmd tea.Cmd
	app, retryCmd = directAffinityApply(app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.mail.input.IsPaletteActive() {
		t.Fatal("Esc did not close the Mail input palette")
	}
	retry, retryOK := directAffinityVisibilityFromCmd(t, retryCmd, "palette Esc")
	if retryOK {
		var markSeenCmd tea.Cmd
		app, markSeenCmd = directAffinityApply(app, retry)
		app = directAffinityApplyPreparedResult(t, app, markSeenCmd, "palette visibility retry")
	}
	if got := directAffinityUnread(t, app.mail, fixture.targetA, accepted); got != 0 {
		t.Errorf("fresh exact visibility immediately after palette Esc left unread=%d, want 0", got)
	}
}

// TestDirectV1RecipientChromeOmitsMainLifecycle keeps Main's existing lifecycle
// chrome intact while preventing a direct recipient identity from being paired
// with Main's state, thinking quote, elapsed time, or network activity.
func TestDirectV1RecipientChromeOmitsMainLifecycle(t *testing.T) {
	fixture := newDirectAffinityFixture(t, true)
	app := fixture.app
	app, _ = directAffinityApply(app, mailRefreshMsg{
		generation: app.mail.generation,
		state:      "active",
		alive:      true,
		activity: fs.NetworkActivity{
			Status:       fs.NetworkStatusActive,
			ActiveAgents: 3,
		},
	})
	app.mail.activeSince = time.Now().Add(-12 * time.Second)

	mainTitle, mainEmail := directAffinityChromeLines(t, app.mail.View())
	stateLabel := i18n.T("state.active")
	quote := thinkingQuotesMap["en"][app.mail.quoteIdx%len(thinkingQuotesMap["en"])]
	networkLabel := networkActivityShortLabel()
	networkState := networkActivityStatusLabel(fs.NetworkStatusActive)
	elapsed := strings.TrimSpace(app.mail.activeElapsed())
	for _, check := range []struct {
		name     string
		line     string
		required []string
	}{
		{"Main title", mainTitle, []string{directAffinityMainName, "◉", stateLabel, quote, networkLabel, networkState}},
		{"Main Email To", mainEmail, []string{directAffinityMainName, spinnerFrames[app.mail.pulseTick%len(spinnerFrames)], stateLabel, elapsed}},
	} {
		for _, token := range check.required {
			if token == "" || !strings.Contains(check.line, token) {
				t.Errorf("%s omitted existing lifecycle token %q: %q", check.name, token, check.line)
			}
		}
	}

	app, _ = directAffinityActivate(t, app, fixture.targetA.AgentID)
	directTitle, directEmail := directAffinityChromeLines(t, app.mail.View())
	for name, line := range map[string]string{
		"direct title":    directTitle,
		"direct Email To": directEmail,
	} {
		if !strings.Contains(line, fixture.targetA.Address) {
			t.Errorf("%s omitted exact recipient %q: %q", name, fixture.targetA.Address, line)
		}
		for _, mainLifecycle := range []string{
			"◉",
			spinnerFrames[app.mail.pulseTick%len(spinnerFrames)],
			stateLabel,
			elapsed,
			quote,
			networkLabel,
			networkState,
		} {
			if mainLifecycle != "" && strings.Contains(line, mainLifecycle) {
				t.Errorf("%s paired direct recipient with Main lifecycle token %q: %q", name, mainLifecycle, line)
			}
		}
	}
}
