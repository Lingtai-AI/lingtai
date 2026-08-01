package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// This file pins the two frozen trimmed-V1 contracts that gate the direct-side
// off-loop Green:
//
//  1. one monotonic refresh request serial with newest-accepted-completion
//     rejection for the whole mailbox/direct payload (cache + deep accepted
//     snapshot + canonical selector rows + installed direct publication), and
//  2. the architectural Main boundary: a prepared direct refresh carries no
//     Main SessionCache/messages/history/count/persist payload or writer
//     authority, and acceptance preserves the live Main object identity and
//     state exactly.
//
// Both tests drive only real production seams: ctrl+r request issuance on the
// serialized Update path, the real refreshMail command, and generation-gated
// delivery through MailModel.Update. Neither test invents or requires a
// Main-session coordinate.

type directOrderingFixture struct {
	mail     MailModel
	root     string
	lingtai  string
	humanDir string
	targetA  fs.DirectTarget
	targetB  fs.DirectTarget
}

func newDirectOrderingFixture(t *testing.T) directOrderingFixture {
	t.Helper()
	root := t.TempDir()
	lingtaiDir := filepath.Join(root, ".lingtai")
	humanDir := filepath.Join(lingtaiDir, "human")
	targetA := fs.DirectTarget{
		ProjectDirectory: root,
		Directory:        filepath.Join(lingtaiDir, "agent-a"),
		AgentID:          "agent-a",
		Address:          directPerformanceHuman + "/agent-a",
	}
	targetB := fs.DirectTarget{
		ProjectDirectory: root,
		Directory:        filepath.Join(lingtaiDir, "agent-b"),
		AgentID:          "agent-b",
		Address:          directPerformanceHuman + "/agent-b",
	}
	directPerformanceWriteManifest(t, humanDir, "human", "Human", directPerformanceHuman, true)
	directPerformanceWriteManifest(t, targetA.Directory, targetA.AgentID, "Alpha", targetA.Address, false)

	mail := NewMailModel(
		humanDir,
		directPerformanceHuman,
		lingtaiDir,
		"",
		"Main",
		10,
		"",
		"en",
		false,
		0,
	)
	mail.generation = 83
	mail, _ = mail.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return directOrderingFixture{
		mail:     mail,
		root:     root,
		lingtai:  lingtaiDir,
		humanDir: humanDir,
		targetA:  targetA,
		targetB:  targetB,
	}
}

// directOrderingIssueRefresh issues one real refresh request on the serialized
// Update path (the production ctrl+r seam) and returns the detached command.
func directOrderingIssueRefresh(t *testing.T, mail MailModel) (MailModel, tea.Cmd) {
	t.Helper()
	next, cmd := mail.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+r on the Update path issued no refresh command")
	}
	return next, cmd
}

func directOrderingRunRefresh(t *testing.T, cmd tea.Cmd, context string) tea.Msg {
	t.Helper()
	msg := cmd()
	if _, ok := msg.(mailRefreshMsg); !ok {
		t.Fatalf("%s: refresh command produced %T, want mailRefreshMsg", context, msg)
	}
	return msg
}

func directOrderingMailHas(messages []fs.MailMessage, marker string) bool {
	for _, message := range messages {
		if strings.Contains(message.Message, marker) {
			return true
		}
	}
	return false
}

func directOrderingProjectionHas(projection []ChatMessage, marker string) bool {
	for _, message := range projection {
		if strings.Contains(message.Body, marker) {
			return true
		}
	}
	return false
}

func directOrderingRowsHave(rows []agentSelectorRow, agentID string) bool {
	for _, row := range rows {
		if !row.Main && row.Target.AgentID == agentID {
			return true
		}
	}
	return false
}

// TestDirectRefreshNewestAcceptedWinsOverLateOlderCompletion issues request A,
// then a genuinely newer request B, lets B complete and be accepted first, and
// finally delivers A's late completion. B's mailbox cache, deep accepted
// snapshot, canonical selector rows, and installed direct publication must
// install coherently as one payload; the late older completion must be
// rejected before any component installs, leaving every accepted component at
// B with no mixed A/B state and no stale scheduled side effect.
func TestDirectRefreshNewestAcceptedWinsOverLateOlderCompletion(t *testing.T) {
	const markerOld = "DIRECT-ORDER-A-OLD"
	const markerNew = "DIRECT-ORDER-B-NEW"

	fixture := newDirectOrderingFixture(t)
	mail := fixture.mail
	mail.cache = fs.NewMailCache(fixture.humanDir)
	mail.cache.Messages = []fs.MailMessage{directPerformanceIncoming(fixture.targetA, 1, markerOld)}

	// Request A is issued first on the serialized Update path and runs against
	// the older mailbox/catalog state.
	var cmdA, cmdB tea.Cmd
	mail, cmdA = directOrderingIssueRefresh(t, mail)
	msgA := directOrderingRunRefresh(t, cmdA, "older request A")

	// The world then genuinely advances: a new safe agent-b manifest appears and
	// a newer strict direct message for agent-a arrives. Request B is issued
	// after A and observes that newer state.
	directPerformanceWriteManifest(t, fixture.targetB.Directory, fixture.targetB.AgentID, "Bravo", fixture.targetB.Address, false)
	mail.cache.Messages = append(append([]fs.MailMessage(nil), mail.cache.Messages...),
		directPerformanceIncoming(fixture.targetA, 2, markerNew))
	mail, cmdB = directOrderingIssueRefresh(t, mail)
	msgB := directOrderingRunRefresh(t, cmdB, "newer request B")

	// B completes first and is accepted: every component of its payload
	// installs together.
	mail, _ = mail.Update(msgB)
	if !directOrderingMailHas(mail.cache.Messages, markerNew) {
		t.Fatalf("accepted B did not install its mailbox cache: marker %q missing", markerNew)
	}
	if !directOrderingMailHas(mail.acceptedSnapshot.cache.Messages, markerNew) {
		t.Fatalf("accepted B did not install its deep accepted snapshot: marker %q missing", markerNew)
	}
	if !directOrderingRowsHave(mail.agentSelector.rows, fixture.targetB.AgentID) {
		t.Fatalf("accepted B did not install its canonical selector rows: agent-b row missing from %d rows", len(mail.agentSelector.rows))
	}
	var activateCmd tea.Cmd
	mail, activateCmd = mail.activateDirectTarget(fixture.targetA)
	if activateCmd == nil {
		t.Fatal("direct activation after accepted B produced no deferred visibility command")
	}
	if !directOrderingProjectionHas(mail.directChat.projection, markerNew) {
		t.Fatalf("direct projection after accepted B lacks newest marker %q; got %d projected messages", markerNew, len(mail.directChat.projection))
	}

	// The older request completes late. It must be rejected before any
	// component installs: no rollback of cache, snapshot, rows, or the
	// installed direct publication/projection, and no stale side effect.
	var staleCmd tea.Cmd
	mail, staleCmd = mail.Update(msgA)
	if staleCmd != nil {
		t.Errorf("late older completion A scheduled a stale side-effect command; a rejected completion must change nothing and schedule nothing")
	}
	if !directOrderingMailHas(mail.cache.Messages, markerNew) {
		t.Errorf("late older completion A rolled back the mailbox cache: accepted marker %q missing", markerNew)
	}
	if !directOrderingMailHas(mail.acceptedSnapshot.cache.Messages, markerNew) {
		t.Errorf("late older completion A rolled back the deep accepted snapshot: accepted marker %q missing", markerNew)
	}
	if !directOrderingRowsHave(mail.agentSelector.rows, fixture.targetB.AgentID) {
		t.Errorf("late older completion A rolled back the canonical selector rows: agent-b row missing")
	}
	if !directOrderingProjectionHas(mail.directChat.projection, markerNew) {
		t.Errorf("late older completion A rolled back the installed direct publication/projection: accepted marker %q missing from %d projected messages", markerNew, len(mail.directChat.projection))
	}
	if directOrderingProjectionHas(mail.directChat.projection, markerOld) != true {
		// The older strict message is part of B's accepted history as well; its
		// absence would indicate a truncated rather than newest-accepted payload.
		t.Errorf("accepted direct projection lost the older accepted strict message %q", markerOld)
	}
}

var directOrderingSessionCacheType = reflect.TypeOf((*fs.SessionCache)(nil))

// directOrderingForbiddenFragments name the Main prepared/persistence/freshness
// machinery this trimmed node must never reintroduce, in payload or model.
var directOrderingForbiddenFragments = []string{
	"preparedsession",
	"preparedmessages",
	"preparedhistory",
	"preparedmain",
	"sessionpersist",
	"mainsession",
}

func directOrderingAssertNoMainFieldNames(t *testing.T, typ reflect.Type, context string) {
	t.Helper()
	for index := range typ.NumField() {
		name := strings.ToLower(typ.Field(index).Name)
		for _, fragment := range directOrderingForbiddenFragments {
			if strings.Contains(name, fragment) {
				t.Errorf("%s field %q matches forbidden Main prepared/persistence machinery fragment %q", context, typ.Field(index).Name, fragment)
			}
		}
	}
}

func directOrderingAssertNoMainSessionPayload(t *testing.T, refresh mailRefreshMsg, context string) {
	t.Helper()
	typ := reflect.TypeOf(refresh)
	value := reflect.ValueOf(refresh)
	directOrderingAssertNoMainFieldNames(t, typ, context)
	for index := range typ.NumField() {
		field := typ.Field(index)
		if field.Type == directOrderingSessionCacheType && !value.Field(index).IsNil() {
			t.Errorf("%s: prepared periodic completion carries a live *fs.SessionCache in field %q; the prepared direct payload must hold no Main session state or writer authority", context, field.Name)
		}
	}
}

// TestPreparedDirectRefreshCarriesNoMainSessionPayloadAndPreservesLiveMain is
// the Main-untouched architectural boundary pin. A real periodic prepared
// direct refresh must carry no Main SessionCache, prepared Main messages,
// history stats, count, persistence obligation, or writer authority — and its
// acceptance must leave the live Main object identity and state exactly as the
// existing serialized on-loop Main pipeline maintains them. It characterizes
// the reused direct-core baseline and must keep holding at the trimmed Green.
func TestPreparedDirectRefreshCarriesNoMainSessionPayloadAndPreservesLiveMain(t *testing.T) {
	fixture := newDirectOrderingFixture(t)
	mail := fixture.mail
	mail.cache = fs.NewMailCache(fixture.humanDir)
	mail.cache.Messages = []fs.MailMessage{directPerformanceIncoming(fixture.targetA, 1, "main-boundary baseline direct mail")}

	// Establish one accepted publication so Main's projection exists before the
	// probed refresh.
	var cmd tea.Cmd
	mail, cmd = directOrderingIssueRefresh(t, mail)
	mail, _ = mail.Update(directOrderingRunRefresh(t, cmd, "baseline refresh"))

	// Identifiable live Main state: the one live SessionCache object, accepted
	// exact history stats, and the visible Main message projection.
	liveMain := mail.sessionCache
	if liveMain == nil {
		t.Fatal("fixture has no live Main SessionCache")
	}
	wantStats := fs.SessionHistoryStats{Detailed: 7, Insights: 3}
	liveMain.SetHistoryStats(wantStats)
	mail.historyStats = wantStats
	mail.historyCountLoaded = true
	mainMessages := append([]ChatMessage(nil), mail.messages...)
	mainAuxiliary := mail.auxiliaryMessages
	mainLoadedExtra := mail.loadedExtra
	mainIngestWindow := mail.ingestWindow
	// The durable Main aggregate is owned solely by the existing serialized
	// on-loop pipeline. With no new Main content between the baseline and the
	// probed refresh, acceptance must leave its bytes untouched.
	sessionPath := filepath.Join(fixture.humanDir, "logs", "session.jsonl")
	sessionBytesBefore, _ := os.ReadFile(sessionPath)

	// Prepare one real periodic direct refresh through the production seams.
	mail, cmd = directOrderingIssueRefresh(t, mail)
	raw := directOrderingRunRefresh(t, cmd, "prepared periodic refresh")
	refresh := raw.(mailRefreshMsg)

	// 1. The prepared completion carries no Main session payload, and neither
	// the payload nor the model reintroduces prepared-Main machinery by name.
	directOrderingAssertNoMainSessionPayload(t, refresh, "mailRefreshMsg")
	directOrderingAssertNoMainFieldNames(t, reflect.TypeOf(mail), "MailModel")

	// 2. Acceptance preserves live Main object identity and state exactly.
	updated, _ := mail.Update(raw)
	if updated.sessionCache != liveMain {
		t.Errorf("accepting a prepared direct refresh replaced the live Main SessionCache object; Main must continue through its existing serialized on-loop pipeline")
	}
	if updated.historyStats != wantStats {
		t.Errorf("accepting a prepared direct refresh changed Main exact history stats: got %+v, want %+v", updated.historyStats, wantStats)
	}
	if !updated.historyCountLoaded {
		t.Errorf("accepting a prepared direct refresh dropped Main's accepted exact-count state")
	}
	if !reflect.DeepEqual(updated.messages, mainMessages) {
		t.Errorf("accepting a prepared direct refresh changed the visible Main messages: got %d, want %d unchanged entries", len(updated.messages), len(mainMessages))
	}
	if updated.auxiliaryMessages != mainAuxiliary {
		t.Errorf("accepting a prepared direct refresh changed Main auxiliary count: got %d, want %d", updated.auxiliaryMessages, mainAuxiliary)
	}
	if updated.loadedExtra != mainLoadedExtra || updated.ingestWindow != mainIngestWindow {
		t.Errorf("accepting a prepared direct refresh changed Main history window state: loadedExtra %d→%d ingestWindow %d→%d",
			mainLoadedExtra, updated.loadedExtra, mainIngestWindow, updated.ingestWindow)
	}
	sessionBytesAfter, _ := os.ReadFile(sessionPath)
	if !reflect.DeepEqual(sessionBytesBefore, sessionBytesAfter) {
		t.Errorf("accepting a prepared direct refresh changed the durable Main session aggregate %q (%d → %d bytes); the prepared payload must carry no persistence obligation or writer authority", sessionPath, len(sessionBytesBefore), len(sessionBytesAfter))
	}
}
