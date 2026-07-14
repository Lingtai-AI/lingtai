package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// installationEnvelope reads the future unexported envelope field without a
// compile-time reference to that field. The unsafe operation is deliberately
// confined to this reflection bridge: reflect does not permit Interface on an
// unexported field, even from a same-package test. Missing/unexpected fields are
// ordinary testing failures, so the unwired tree remains a valid assertion RED.
func installationEnvelope[T any](t *testing.T, msg *T) asyncEnvelope {
	t.Helper()
	field := installationEnvelopeField(t, msg)
	return *(*asyncEnvelope)(unsafe.Pointer(field.UnsafeAddr()))
}

func installationEnvelopeField[T any](t *testing.T, msg *T) reflect.Value {
	t.Helper()
	value := reflect.ValueOf(msg)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		t.Fatalf("message type %T is not an addressable value", msg)
	}
	field := value.Elem().FieldByName("envelope")
	if !field.IsValid() {
		var zero T
		t.Fatalf("message type %T has no asyncEnvelope field named envelope", zero)
	}
	if field.Type() != reflect.TypeOf(asyncEnvelope{}) {
		var zero T
		t.Fatalf("message type %T envelope field has type %v, want asyncEnvelope", zero, field.Type())
	}
	if !field.CanAddr() {
		var zero T
		t.Fatalf("message type %T envelope field is not addressable", zero)
	}
	return field
}

func installationWithEnvelope[T any](t *testing.T, msg T, envelope asyncEnvelope) T {
	t.Helper()
	field := installationEnvelopeField(t, &msg)
	*(*asyncEnvelope)(unsafe.Pointer(field.UnsafeAddr())) = envelope
	return msg
}

func installationProducedEnvelope[T any](t *testing.T, msg *T, wantKind asyncKind) asyncEnvelope {
	t.Helper()
	envelope := installationEnvelope(t, msg)
	wantFields, ok := asyncRequiredMask(wantKind)
	if !ok {
		t.Fatalf("test requested unknown async kind %d", wantKind)
	}
	if envelope.kind != wantKind || envelope.fields != wantFields {
		t.Fatalf("real producer returned envelope kind=%d fields=%06b, want kind=%d fields=%06b", envelope.kind, envelope.fields, wantKind, wantFields)
	}
	return envelope
}

func installationTestStart(t *testing.T) {
	t.Helper()
	t.Logf("INSTALLATION-TEST-START %s", t.Name())
}

type installationScriptedScanner struct {
	scans    atomic.Int64
	messages []fs.MailMessage
}

func (s *installationScriptedScanner) Refresh(cache fs.MailCache) fs.MailCache {
	s.scans.Add(1)
	cache.Messages = append([]fs.MailMessage(nil), s.messages...)
	return cache
}

type installationLocationRecorder struct{ calls atomic.Int64 }

func (r *installationLocationRecorder) update(string) { r.calls.Add(1) }

func installationWriteAgent(t *testing.T, dir, address, name, nickname string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"address":%q,"agent_name":%q,"nickname":%q,"state":"IDLE"}`, address, name, nickname)
	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func installationWriteEvents(t *testing.T, dir string, count int, prefix string) {
	t.Helper()
	logs := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&body, `{"ts":%d,"type":"text_output","text":%q}`+"\n", i+1, fmt.Sprintf("%s-%03d", prefix, i))
	}
	if err := os.WriteFile(filepath.Join(logs, "events.jsonl"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func installationAppendEvent(t *testing.T, dir, text string) {
	t.Helper()
	path := filepath.Join(dir, "logs", "events.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(file, `{"ts":999999,"type":"text_output","text":%q}`+"\n", text); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func installationNewApp(t *testing.T, eventCount int) (App, *installationScriptedScanner, *installationLocationRecorder) {
	t.Helper()
	root := t.TempDir()
	projectDir := filepath.Join(root, "project", ".lingtai")
	orchDir := filepath.Join(projectDir, "Main")
	humanDir := filepath.Join(projectDir, "human")
	installationWriteAgent(t, orchDir, "main@installation.test", "Main", "Fixture Main")
	installationWriteEvents(t, orchDir, eventCount, "installation")

	scanner := &installationScriptedScanner{messages: []fs.MailMessage{{
		ID:         "installation-mail",
		MailboxID:  "installation-mail",
		From:       "fixture-agent",
		To:         "human",
		Subject:    "runtime installation",
		Message:    "mail projection sentinel",
		Type:       "normal",
		ReceivedAt: "2026-07-14T12:00:00Z",
		Identity:   map[string]interface{}{"agent_name": "Fixture Agent"},
		Delivered:  true,
	}}}
	locations := &installationLocationRecorder{}

	a := App{
		currentView: appViewMail,
		globalDir:   filepath.Join(root, "global"),
		projectDir:  projectDir,
		orchDir:     orchDir,
		orchName:    "Main",
		tuiConfig:   config.DefaultTUIConfig(),
		width:       100,
		height:      30,
	}
	a.mailStore = newProjectMailStoreWithDeps(projectDir, humanDir, scanner, locations.update)
	a.installMailModel(NewMailModel(humanDir, "human", projectDir, orchDir, "Main", 200, a.globalDir, "en", false, 0))
	a.mail.verbose = verboseThinking
	return a, scanner, locations
}

func installationRefreshResult(t *testing.T, app *App, initial bool) projectMailRefreshMsg {
	t.Helper()
	cmd := app.beginProjectMailRefresh(initial)
	if cmd == nil {
		t.Fatal("real ProjectMailStore refresh producer returned nil")
	}
	msg, ok := cmd().(projectMailRefreshMsg)
	if !ok {
		t.Fatalf("real ProjectMailStore refresh producer returned %T, want projectMailRefreshMsg", cmd())
	}
	return msg
}

func installationDeliverApp(t *testing.T, app App, msg tea.Msg) (App, tea.Cmd) {
	t.Helper()
	model, cmd := app.Update(msg)
	updated, ok := model.(App)
	if !ok {
		t.Fatalf("App.Update returned model %T, want App", model)
	}
	return updated, cmd
}

type installationAppState struct {
	projectDir            string
	orchDir               string
	orchName              string
	storeVersion          uint64
	storeSnapshot         *ProjectMailSnapshot
	storeCache            string
	refreshInFlight       bool
	initialRefreshPending bool
	acceptedSnapshot      *ProjectMailSnapshot
	sessionCache          *fs.SessionCache
	mailMessages          string
	initialLoading        bool
	orchState             string
	orchAlive             bool
	historyCountLoading   bool
	historyCountLoaded    bool
	olderLoadInFlight     bool
	ingestWindow          int
	loadedExtra           int
	locationCalls         int64
}

func installationSnapshot(app App, locations *installationLocationRecorder) installationAppState {
	return installationAppState{
		projectDir:            app.projectDir,
		orchDir:               app.orchDir,
		orchName:              app.orchName,
		storeVersion:          app.mailStore.version,
		storeSnapshot:         app.mailStore.snapshot,
		storeCache:            fmt.Sprintf("%#v", app.mailStore.cache.Messages),
		refreshInFlight:       app.mailStore.refreshInFlight,
		initialRefreshPending: app.mailStore.initialRefreshPending,
		acceptedSnapshot:      app.mail.acceptedSnapshot,
		sessionCache:          app.mail.sessionCache,
		mailMessages:          fmt.Sprintf("%#v", app.mail.messages),
		initialLoading:        app.mail.initialLoading,
		orchState:             app.mail.orchState,
		orchAlive:             app.mail.orchAlive,
		historyCountLoading:   app.mail.historyCountLoading,
		historyCountLoaded:    app.mail.historyCountLoaded,
		olderLoadInFlight:     app.mail.olderLoadInFlight,
		ingestWindow:          app.mail.ingestWindow,
		loadedExtra:           app.mail.loadedExtra,
		locationCalls:         locations.calls.Load(),
	}
}

func installationAssertAppState(t *testing.T, scenario string, got App, locations *installationLocationRecorder, want installationAppState) {
	t.Helper()
	after := installationSnapshot(got, locations)
	if after != want {
		t.Errorf("scenario=%s: rejected completion mutated App/Mail/store state\n got: %+v\nwant: %+v", scenario, after, want)
	}
}

func installationMutateProject(envelope *asyncEnvelope) {
	envelope.owner.projectID = canonicalProjectMailIdentity(filepath.Join(filepath.Dir(envelope.owner.projectID), "wrong-project", ".lingtai"))
}

func installationMutateTarget(envelope *asyncEnvelope) {
	envelope.target.directory = filepath.Join(envelope.owner.projectID, "OtherTarget")
}

func installationMutateAddress(envelope *asyncEnvelope) {
	envelope.target.addressFingerprint = fs.AddressFingerprint("replacement@installation.test")
}

func installationAcceptInitial(t *testing.T, app App) (App, tea.Cmd) {
	t.Helper()
	msg := installationRefreshResult(t, &app, true)
	updated, cmd := installationDeliverApp(t, app, msg)
	if updated.mail.initialLoading || updated.mail.acceptedSnapshot == nil || updated.mail.sessionCache == nil {
		t.Fatalf("real initial refresh did not install through App.Update: loading=%v snapshot=%v cache=%v", updated.mail.initialLoading, updated.mail.acceptedSnapshot != nil, updated.mail.sessionCache != nil)
	}
	return updated, cmd
}

func installationPersistFixture(t *testing.T) (App, mailPersistMsg, *installationPersistenceRecorder) {
	t.Helper()
	app, _, _ := installationNewApp(t, 1)
	app, postFrame := installationAcceptInitial(t, app)
	persist, ok := findMailPersistCmd(postFrame)
	if !ok {
		t.Fatalf("accepted real refresh did not produce mailPersistMsg; command produced %T", runCmd(postFrame))
	}
	recorder := &installationPersistenceRecorder{path: filepath.Join(app.mail.humanDir, "logs", "session.jsonl")}
	recorder.seed(t, []byte("sentinel aggregate\n"))
	return app, persist, recorder
}

// installationPersistenceRecorder is a test-local adapter around the existing
// SessionCache.Persist side effect. The temporary humanDir injects an isolated
// destination; byte transitions record writes without changing production or
// allowing any filesystem effect outside t.TempDir.
type installationPersistenceRecorder struct {
	path   string
	writes int
	last   []byte
}

func (r *installationPersistenceRecorder) seed(t *testing.T, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	r.last = append([]byte(nil), body...)
}

func (r *installationPersistenceRecorder) observe(t *testing.T) {
	t.Helper()
	body, err := os.ReadFile(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, r.last) {
		r.writes++
		r.last = append(r.last[:0], body...)
	}
}

func installationOlderFixture(t *testing.T) (App, mailOlderPageMsg, *installationPersistenceRecorder) {
	t.Helper()
	app, _, _ := installationNewApp(t, 405)
	app, _ = installationAcceptInitial(t, app)
	if !app.mail.cacheIsPartial() {
		t.Fatal("precondition: 405-event initial page must be partial")
	}
	launched, cmd := app.mail.requestOlderPage()
	if cmd == nil || !launched.olderLoadInFlight {
		t.Fatal("real older-page producer did not launch")
	}
	app.mail = launched
	msg, ok := cmd().(mailOlderPageMsg)
	if !ok {
		t.Fatalf("real older-page command returned %T, want mailOlderPageMsg", cmd())
	}
	recorder := &installationPersistenceRecorder{path: filepath.Join(app.mail.humanDir, "logs", "session.jsonl")}
	recorder.seed(t, []byte("older-page sentinel\n"))
	return app, msg, recorder
}

func installationHistoryFixture(t *testing.T) (App, mailHistoryCountMsg) {
	t.Helper()
	app, _, _ := installationNewApp(t, 405)
	app, _ = installationAcceptInitial(t, app)
	if app.mail.historyCountCache == nil || !app.mail.historyCountLoading {
		t.Fatal("real initial refresh did not start an exact-count task")
	}
	msg, ok := app.mail.historyCountCmd(app.mail.historyCountCache, app.mail.generation)().(mailHistoryCountMsg)
	if !ok {
		t.Fatal("real count command did not return mailHistoryCountMsg")
	}
	return app, msg
}

func installationTickFixture(t *testing.T) (App, projectMailTickMsg, *installationScriptedScanner) {
	t.Helper()
	app, scanner, _ := installationNewApp(t, 1)
	app.mail.homeTelemetryInFlight = true // keep the tick result to refresh + rearm only
	app.mailStore.pollRate = time.Nanosecond
	cmd := app.mailStore.resumeTick()
	if cmd == nil {
		t.Fatal("real ProjectMailStore tick producer returned nil")
	}
	msg, ok := cmd().(projectMailTickMsg)
	if !ok {
		t.Fatalf("real tick command returned %T, want projectMailTickMsg", cmd())
	}
	return app, msg, scanner
}

func installationRunBatch(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return installationFlattenBatch(t, cmd())
}

func installationFlattenBatch(t *testing.T, msg tea.Msg) []tea.Msg {
	t.Helper()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, child := range batch {
			if child == nil {
				continue
			}
			messages = append(messages, installationFlattenBatch(t, child())...)
		}
		return messages
	}
	return []tea.Msg{msg}
}

func installationVisitedApp(t *testing.T, eventCount int) (App, *installationScriptedScanner, *installationLocationRecorder) {
	t.Helper()
	home, _, _ := installationNewApp(t, 1)
	root := filepath.Dir(filepath.Dir(home.projectDir))
	targetRoot := filepath.Join(root, "visited")
	record := visitRecord(targetRoot, "worker", "Worker")
	record.Address = "worker@installation.test"
	installationWriteAgent(t, record.AgentDir, record.Address, record.AgentName, "Original nickname")
	installationWriteEvents(t, record.AgentDir, eventCount, "visited")
	visited, _ := home.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: record})

	// enterVisitedAgent creates the real visited owner and marks its returned
	// initial command in-flight. Tests below launch their own observable command,
	// so release only that unexecuted test fixture slot without touching accepted
	// cache/version/snapshot state.
	visited.mailStore.refreshInFlight = false
	visited.mailStore.refreshInitial = false
	visited.mailStore.refreshGeneration = 0
	visited.mailStore.initialRefreshPending = false

	scanner := &installationScriptedScanner{messages: []fs.MailMessage{{
		ID:         "visited-mail",
		MailboxID:  "visited-mail",
		From:       record.Address,
		To:         "human",
		Message:    "visited mail sentinel",
		ReceivedAt: "2026-07-14T12:01:00Z",
		Delivered:  true,
	}}}
	locations := &installationLocationRecorder{}
	visited.mailStore.scanner = scanner
	visited.mailStore.updateLocation = locations.update
	return visited, scanner, locations
}

type installationPulseProducer interface {
	asyncPulseCmd() tea.Cmd
}

func installationPulseFixture(t *testing.T) (App, pulseTickMsg) {
	t.Helper()
	app, _, _ := installationVisitedApp(t, 1)
	app.mail.orchState = "ACTIVE"
	app.mail.pulseTick = 11

	producer, ok := any(app.mail).(installationPulseProducer)
	if !ok {
		t.Fatal("runtime installation seam missing: MailModel has no asyncPulseCmd() tea.Cmd; a generation-only pulseTick call cannot capture owner/target/address/epoch")
	}
	cmd := producer.asyncPulseCmd()
	if cmd == nil {
		t.Fatal("real liveness-pulse producer returned nil")
	}
	msg, ok := cmd().(pulseTickMsg)
	if !ok {
		t.Fatalf("real pulse command returned %T, want pulseTickMsg", cmd())
	}
	return app, msg
}

func installationInstallSameGenerationTarget(t *testing.T, app App, targetDir, targetName string) App {
	t.Helper()
	generation := app.mail.generation
	if generation == 0 {
		t.Fatal("same-generation target fixture requires a non-zero generation")
	}
	app.orchDir = targetDir
	app.orchName = targetName
	app.mailGeneration = generation - 1
	app.installMailModel(NewMailModel(app.mail.humanDir, "human", app.projectDir, targetDir, targetName, 200, app.globalDir, "en", false, 0))
	if app.mail.generation != generation {
		t.Fatalf("same-generation target fixture installed generation %d, want %d", app.mail.generation, generation)
	}
	return app
}

// installationFakeEditorDone is the requested no-process editor seam. It uses a
// real steady-refresh request producer to obtain the current owner/target/thread
// binding, then narrows that captured envelope to the editor kind. No external
// editor or subprocess is launched; App/Mail still consume the real EditorDoneMsg.
func installationFakeEditorDone(t *testing.T, mail MailModel, text string) EditorDoneMsg {
	t.Helper()
	request, ok := mail.requestMailRefresh(false)().(projectMailRefreshRequestMsg)
	if !ok {
		t.Fatal("real refresh-request producer did not return projectMailRefreshRequestMsg")
	}
	envelope := installationProducedEnvelope(t, &request, asyncSteadyRefresh)
	envelope.kind = asyncEditorDone
	envelope.fields, _ = asyncRequiredMask(asyncEditorDone)
	envelope.generation.epoch = 0
	envelope.source = asyncSourceCache{}
	envelope.storeVersion = 0
	return installationWithEnvelope(t, EditorDoneMsg{Text: text}, envelope)
}

type installationResolvedTarget struct {
	present            bool
	projectID          string
	directory          string
	addressFingerprint string
	eligible           bool
	nickname           string
}

type installationInventoryResolver struct {
	calls  atomic.Int64
	record installationResolvedTarget
}

func (r *installationInventoryResolver) resolve(owner asyncOwner, target asyncTarget) bool {
	r.calls.Add(1)
	record := r.record
	return record.present && record.eligible && record.projectID == owner.projectID &&
		record.directory == target.directory && record.addressFingerprint == target.addressFingerprint
}

func installationExactResolvedTarget(envelope asyncEnvelope) installationResolvedTarget {
	return installationResolvedTarget{
		present:            true,
		projectID:          envelope.owner.projectID,
		directory:          envelope.target.directory,
		addressFingerprint: envelope.target.addressFingerprint,
		eligible:           true,
		nickname:           "Original nickname",
	}
}

// Wiring needs one deterministic resolver injection at the App/store boundary.
// Interface assertions keep the currently absent seam compile-safe and make its
// absence an ordinary, explicit failure rather than inventing a parallel test
// acceptance implementation.
func installationInjectResolver(t *testing.T, app *App, resolver func(asyncOwner, asyncTarget) bool) {
	t.Helper()
	type appResolverSetter interface {
		setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool)
	}
	type storeResolverSetter interface {
		setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool)
	}
	if setter, ok := any(app).(appResolverSetter); ok {
		setter.setAsyncTargetRevalidator(resolver)
		return
	}
	if setter, ok := any(&app.mailStore).(storeResolverSetter); ok {
		setter.setAsyncTargetRevalidator(resolver)
		return
	}
	t.Fatalf("runtime installation seam missing: App/ProjectMailStore has no setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool); inventory-bound completion tests must not call the live process scanner")
}

func installationApplyResolverScenario(record installationResolvedTarget, scenario string) installationResolvedTarget {
	switch scenario {
	case "disappeared":
		record.present = false
	case "changed_project":
		record.projectID = canonicalProjectMailIdentity(filepath.Join(filepath.Dir(record.projectID), "replacement-project", ".lingtai"))
	case "became_ineligible":
		record.eligible = false
	case "changed_address":
		record.addressFingerprint = fs.AddressFingerprint("changed-address@installation.test")
	case "nickname_only":
		record.nickname = "Display-only rename"
	}
	return record
}

func TestAsyncRefreshCompletionRejectsWrongProjectStoreTargetAddressGenerationAndVersion(t *testing.T) {
	installationTestStart(t)
	gateApp, _, _ := installationNewApp(t, 1)
	gateMsg := installationRefreshResult(t, &gateApp, true)
	_ = installationProducedEnvelope(t, &gateMsg, asyncInitialRebuild)

	variants := []struct {
		name   string
		mutate func(*asyncEnvelope)
	}{
		{name: "wrong_project", mutate: installationMutateProject},
		{name: "wrong_store", mutate: func(e *asyncEnvelope) { e.owner.storeID++ }},
		{name: "wrong_target", mutate: installationMutateTarget},
		{name: "wrong_address", mutate: installationMutateAddress},
		{name: "wrong_generation", mutate: func(e *asyncEnvelope) { e.generation.thread++ }},
		{name: "wrong_store_version", mutate: func(e *asyncEnvelope) { e.storeVersion++ }},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			app, scanner, locations := installationNewApp(t, 1)
			msg := installationRefreshResult(t, &app, true)
			envelope := installationProducedEnvelope(t, &msg, asyncInitialRebuild)
			variant.mutate(&envelope)
			msg = installationWithEnvelope(t, msg, envelope)
			before := installationSnapshot(app, locations)

			got, cmd := installationDeliverApp(t, app, msg)
			if cmd != nil {
				t.Errorf("scenario=%s: rejected refresh returned follow-up command producing %T", variant.name, runCmd(cmd))
			}
			installationAssertAppState(t, variant.name, got, locations, before)
			if scanner.scans.Load() != 1 {
				t.Errorf("scenario=%s: scanner calls=%d, want exactly the producer's one detached scan", variant.name, scanner.scans.Load())
			}
		})
	}
}

func TestAsyncRefreshCompletionA7B8A9CannotMutateA9(t *testing.T) {
	installationTestStart(t)
	app, _, locations := installationNewApp(t, 1)
	app.mailGeneration = 6
	app.installMailModel(app.mail)
	if app.mail.generation != 7 {
		t.Fatalf("A fixture generation=%d, want 7", app.mail.generation)
	}
	a7 := installationRefreshResult(t, &app, true)
	_ = installationProducedEnvelope(t, &a7, asyncInitialRebuild)

	targetRoot := filepath.Join(filepath.Dir(filepath.Dir(app.projectDir)), "B")
	record := visitRecord(targetRoot, "worker", "B")
	installationWriteAgent(t, record.AgentDir, record.Address, record.AgentName, "B nickname")
	b8, _ := app.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: record})
	if b8.mail.generation != 8 {
		t.Fatalf("B fixture generation=%d, want 8", b8.mail.generation)
	}
	a9, _ := b8.returnFromVisit()
	if a9.mail.generation != 9 {
		t.Fatalf("returned A fixture generation=%d, want 9", a9.mail.generation)
	}
	before := installationSnapshot(a9, locations)

	got, cmd := installationDeliverApp(t, a9, a7)
	if cmd != nil {
		t.Errorf("A7 completion delivered to A9 returned command producing %T", runCmd(cmd))
	}
	installationAssertAppState(t, "A7_after_B8_then_A9", got, locations, before)
}

func TestAsyncPersistRejectsStaleTargetSourceAndStoreVersionWithoutWriting(t *testing.T) {
	installationTestStart(t)
	_, gatePersist, _ := installationPersistFixture(t)
	_ = installationProducedEnvelope(t, &gatePersist, asyncSessionPersist)

	variants := []struct {
		name   string
		mutate func(*asyncEnvelope)
	}{
		{name: "wrong_target", mutate: installationMutateTarget},
		{name: "wrong_address", mutate: installationMutateAddress},
		{name: "wrong_source_cache", mutate: func(e *asyncEnvelope) { e.source.cache = new(fs.SessionCache) }},
		{name: "wrong_source_horizon", mutate: func(e *asyncEnvelope) { e.source.identity += ":stale" }},
		{name: "wrong_store_version", mutate: func(e *asyncEnvelope) { e.storeVersion++ }},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			app, persist, recorder := installationPersistFixture(t)
			envelope := installationProducedEnvelope(t, &persist, asyncSessionPersist)
			variant.mutate(&envelope)
			persist = installationWithEnvelope(t, persist, envelope)
			beforeCache := app.mail.sessionCache

			got, cmd := installationDeliverApp(t, app, persist)
			recorder.observe(t)
			if cmd != nil {
				t.Errorf("scenario=%s: rejected persist returned command producing %T", variant.name, runCmd(cmd))
			}
			if recorder.writes != 0 {
				t.Errorf("scenario=%s: stale persist changed aggregate %d time(s), want zero", variant.name, recorder.writes)
			}
			if got.mail.sessionCache != beforeCache {
				t.Errorf("scenario=%s: rejected persist replaced current session cache", variant.name)
			}
		})
	}

	t.Run("matching_main_writer_writes_once", func(t *testing.T) {
		app, persist, recorder := installationPersistFixture(t)
		got, cmd := installationDeliverApp(t, app, persist)
		recorder.observe(t)
		if recorder.writes != 1 {
			t.Fatalf("matching Main persist writes=%d, want exactly one", recorder.writes)
		}
		if got.mail.sessionCache != app.mail.sessionCache {
			t.Fatal("matching persist unexpectedly replaced the installed cache")
		}
		_ = cmd // matching persistence may schedule one telemetry continuation
	})
}

func TestAsyncOlderPageRejectsStaleTargetSourceAndStoreVersion(t *testing.T) {
	installationTestStart(t)
	_, gatePage, _ := installationOlderFixture(t)
	_ = installationProducedEnvelope(t, &gatePage, asyncOlderPage)

	variants := []struct {
		name   string
		mutate func(*asyncEnvelope)
	}{
		{name: "wrong_target", mutate: installationMutateTarget},
		{name: "wrong_address", mutate: installationMutateAddress},
		{name: "wrong_source_cache", mutate: func(e *asyncEnvelope) { e.source.cache = new(fs.SessionCache) }},
		{name: "wrong_source_horizon", mutate: func(e *asyncEnvelope) { e.source.identity += ":stale" }},
		{name: "wrong_store_version", mutate: func(e *asyncEnvelope) { e.storeVersion++ }},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			app, page, recorder := installationOlderFixture(t)
			envelope := installationProducedEnvelope(t, &page, asyncOlderPage)
			variant.mutate(&envelope)
			page = installationWithEnvelope(t, page, envelope)
			before := installationSnapshot(app, &installationLocationRecorder{})

			got, cmd := installationDeliverApp(t, app, page)
			recorder.observe(t)
			if cmd != nil {
				t.Errorf("scenario=%s: rejected older page returned command producing %T", variant.name, runCmd(cmd))
			}
			// Use a zero recorder in both snapshots; persistence is asserted separately.
			installationAssertAppState(t, variant.name, got, &installationLocationRecorder{}, before)
			if recorder.writes != 0 {
				t.Errorf("scenario=%s: rejected older page changed aggregate %d time(s)", variant.name, recorder.writes)
			}
		})
	}

	t.Run("matching_page_installs_one_window_without_persisting_partial_cache", func(t *testing.T) {
		app, page, recorder := installationOlderFixture(t)
		beforeCache := app.mail.sessionCache
		beforeMessages := len(app.mail.messages)
		got, cmd := installationDeliverApp(t, app, page)
		recorder.observe(t)
		if got.mail.sessionCache == beforeCache || got.mail.olderLoadInFlight || got.mail.ingestWindow != 400 || got.mail.loadedExtra != 200 {
			t.Fatalf("matching older page did not install exactly one window: cacheChanged=%v inFlight=%v window=%d extra=%d", got.mail.sessionCache != beforeCache, got.mail.olderLoadInFlight, got.mail.ingestWindow, got.mail.loadedExtra)
		}
		if len(got.mail.messages) <= beforeMessages {
			t.Fatalf("matching older page messages=%d, want more than initial %d", len(got.mail.messages), beforeMessages)
		}
		if recorder.writes != 0 || cmd != nil {
			t.Fatalf("matching still-partial page persisted or scheduled work: writes=%d cmd=%T", recorder.writes, runCmd(cmd))
		}
	})
}

func TestAsyncHistoryCountRejectsTargetAddressGenerationAndOriginCache(t *testing.T) {
	installationTestStart(t)
	_, gateCount := installationHistoryFixture(t)
	_ = installationProducedEnvelope(t, &gateCount, asyncExactHistoryCount)

	variants := []struct {
		name   string
		mutate func(*asyncEnvelope)
	}{
		{name: "wrong_target", mutate: installationMutateTarget},
		{name: "wrong_address", mutate: installationMutateAddress},
		{name: "wrong_thread_generation", mutate: func(e *asyncEnvelope) { e.generation.thread++ }},
		{name: "wrong_origin_cache", mutate: func(e *asyncEnvelope) { e.source.cache = new(fs.SessionCache) }},
		{name: "wrong_origin_horizon", mutate: func(e *asyncEnvelope) { e.source.identity += ":different-horizon" }},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			app, count := installationHistoryFixture(t)
			envelope := installationProducedEnvelope(t, &count, asyncExactHistoryCount)
			variant.mutate(&envelope)
			count = installationWithEnvelope(t, count, envelope)
			beforeLoading := app.mail.historyCountLoading
			beforeLoaded := app.mail.historyCountLoaded
			beforeStats := app.mail.historyStats
			beforeCache := app.mail.historyCountCache

			got, cmd := installationDeliverApp(t, app, count)
			if cmd != nil {
				t.Errorf("scenario=%s: rejected count returned command producing %T", variant.name, runCmd(cmd))
			}
			if got.mail.historyCountLoading != beforeLoading || got.mail.historyCountLoaded != beforeLoaded ||
				got.mail.historyStats != beforeStats || got.mail.historyCountCache != beforeCache {
				t.Errorf("scenario=%s: rejected count changed loading/loaded/stats/origin", variant.name)
			}
		})
	}

	t.Run("same_horizon_replacement_accepts_and_uses_current_tail_delta", func(t *testing.T) {
		app, count := installationHistoryFixture(t)
		origin := app.mail.historyCountCache
		launched, pageCmd := app.mail.requestOlderPage()
		if pageCmd == nil {
			t.Fatal("same-horizon fixture did not launch older page")
		}
		app.mail = launched
		page := pageCmd().(mailOlderPageMsg)
		_ = installationProducedEnvelope(t, &page, asyncOlderPage)
		app, pageFollowup := installationDeliverApp(t, app, page)
		if pageFollowup != nil {
			t.Fatalf("same-horizon partial replacement unexpectedly scheduled %T", runCmd(pageFollowup))
		}
		if app.mail.sessionCache == origin || app.mail.historyCountCache != origin {
			t.Fatal("same-horizon page did not replace installed cache while retaining count origin")
		}

		installationAppendEvent(t, app.orchDir, "tail delta after replacement")
		mail := app.mail
		mail.buildMessages()
		app.mail = mail
		if got := app.mail.sessionCache.HistoryStats().Detailed; got != 1 {
			t.Fatalf("current replacement cache tail delta=%d, want 1", got)
		}
		if got := origin.HistoryStats().Detailed; got != 0 {
			t.Fatalf("detached count origin received tail delta=%d, want 0", got)
		}

		app, cmd := installationDeliverApp(t, app, count)
		if cmd != nil {
			t.Fatalf("accepted exact count returned command producing %T", runCmd(cmd))
		}
		if !app.mail.historyCountLoaded || app.mail.historyCountLoading {
			t.Fatalf("same-horizon count not accepted: loaded=%v loading=%v", app.mail.historyCountLoaded, app.mail.historyCountLoading)
		}
		if want := count.stats.Detailed + 1; app.mail.historyStats.Detailed != want {
			t.Fatalf("accepted count detailed=%d, want origin %d + current tail 1 = %d", app.mail.historyStats.Detailed, count.stats.Detailed, want)
		}
	})
}

func TestAsyncRefreshTickRejectsOldBindingWithoutRefreshOrRearm(t *testing.T) {
	installationTestStart(t)
	_, gateTick, _ := installationTickFixture(t)
	_ = installationProducedEnvelope(t, &gateTick, asyncRefreshTick)

	variants := []struct {
		name   string
		mutate func(*asyncEnvelope)
	}{
		{name: "wrong_project", mutate: installationMutateProject},
		{name: "wrong_store", mutate: func(e *asyncEnvelope) { e.owner.storeID++ }},
		{name: "wrong_activation", mutate: func(e *asyncEnvelope) { e.owner.activation++ }},
		{name: "wrong_target", mutate: installationMutateTarget},
		{name: "wrong_address", mutate: installationMutateAddress},
		{name: "wrong_thread", mutate: func(e *asyncEnvelope) { e.generation.thread++ }},
		{name: "wrong_epoch", mutate: func(e *asyncEnvelope) { e.generation.epoch++ }},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			app, tick, scanner := installationTickFixture(t)
			envelope := installationProducedEnvelope(t, &tick, asyncRefreshTick)
			variant.mutate(&envelope)
			tick = installationWithEnvelope(t, tick, envelope)
			beforeChain := app.mailStore.tickChain

			got, cmd := installationDeliverApp(t, app, tick)
			if cmd != nil {
				t.Errorf("scenario=%s: stale tick rearmed/launched command producing %T", variant.name, runCmd(cmd))
			}
			if got.mailStore.refreshInFlight || got.mailStore.tickChain != beforeChain || scanner.scans.Load() != 0 {
				t.Errorf("scenario=%s: stale tick changed ownership: refresh=%v chain=%d/%d scans=%d", variant.name, got.mailStore.refreshInFlight, got.mailStore.tickChain, beforeChain, scanner.scans.Load())
			}
		})
	}

	t.Run("matching_tick_launches_one_refresh_and_one_rearm", func(t *testing.T) {
		app, tick, scanner := installationTickFixture(t)
		got, cmd := installationDeliverApp(t, app, tick)
		if !got.mailStore.refreshInFlight {
			t.Fatal("matching tick did not claim one refresh slot")
		}
		messages := installationRunBatch(t, cmd)
		refreshes, ticks := 0, 0
		for _, msg := range messages {
			switch msg.(type) {
			case projectMailRefreshMsg:
				refreshes++
			case projectMailTickMsg:
				ticks++
			default:
				t.Errorf("matching tick returned unexpected command message %T", msg)
			}
		}
		if refreshes != 1 || ticks != 1 || scanner.scans.Load() != 1 {
			t.Fatalf("matching tick effects: refresh messages=%d next ticks=%d scanner calls=%d; want 1/1/1", refreshes, ticks, scanner.scans.Load())
		}
	})
}

func TestAsyncLivenessPulseRejectsOldBindingWithoutAnimationOrRearm(t *testing.T) {
	installationTestStart(t)
	// Keep the unwired baseline RED at the shared missing-field boundary. Once the
	// field exists, installationPulseFixture requires the real full-binding seam.
	gatePulse := pulseTickMsg{}
	_ = installationEnvelope(t, &gatePulse)
	_, gatePulse = installationPulseFixture(t)
	_ = installationProducedEnvelope(t, &gatePulse, asyncLivenessPulse)

	variants := []struct {
		name   string
		mutate func(*asyncEnvelope)
	}{
		{name: "wrong_target", mutate: installationMutateTarget},
		{name: "wrong_address", mutate: installationMutateAddress},
		{name: "wrong_thread", mutate: func(e *asyncEnvelope) { e.generation.thread++ }},
		{name: "wrong_epoch", mutate: func(e *asyncEnvelope) { e.generation.epoch++ }},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			app, pulse := installationPulseFixture(t)
			envelope := installationProducedEnvelope(t, &pulse, asyncLivenessPulse)
			variant.mutate(&envelope)
			pulse = installationWithEnvelope(t, pulse, envelope)
			before := app.mail.pulseTick

			got, cmd := installationDeliverApp(t, app, pulse)
			if cmd != nil {
				t.Errorf("scenario=%s: stale pulse returned next-pulse command producing %T", variant.name, runCmd(cmd))
			}
			if got.mail.pulseTick != before {
				t.Errorf("scenario=%s: stale pulse counter=%d, want %d", variant.name, got.mail.pulseTick, before)
			}
		})
	}

	t.Run("matching_inventory_bound_pulse_animates_without_inventory_scan", func(t *testing.T) {
		app, pulse := installationPulseFixture(t)
		before := app.mail.pulseTick
		got, cmd := installationDeliverApp(t, app, pulse)
		if got.mail.pulseTick != before+1 {
			t.Fatalf("matching active pulse counter=%d, want %d", got.mail.pulseTick, before+1)
		}
		if cmd == nil {
			t.Fatal("matching pulse returned no next pulse")
		}
		next, ok := cmd().(pulseTickMsg)
		if !ok {
			t.Fatalf("matching pulse rearm produced %T, want exactly one pulseTickMsg", cmd())
		}
		_ = installationProducedEnvelope(t, &next, asyncLivenessPulse)
	})
}

func TestEditorDoneCarriesAndValidatesTargetIdentityAddress(t *testing.T) {
	installationTestStart(t)
	// Gate the exact completion type before the no-process fake producer adapter.
	gate := EditorDoneMsg{Text: "field gate"}
	_ = installationEnvelope(t, &gate)

	app, _, _ := installationNewApp(t, 1)
	project := app.projectDir
	targetA := filepath.Join(project, "A")
	targetB := filepath.Join(project, "B")
	installationWriteAgent(t, targetA, "a@installation.test", "A", "A nickname")
	installationWriteAgent(t, targetB, "b@installation.test", "B", "B nickname")
	installationWriteEvents(t, targetA, 1, "A")
	installationWriteEvents(t, targetB, 1, "B")
	app.mailGeneration = 6
	app = installationInstallSameGenerationTarget(t, app, targetA, "A")
	text := strings.Repeat("editor line\n", 12)
	done := installationFakeEditorDone(t, app.mail, text)

	t.Run("same_generation_target_switch_rejects", func(t *testing.T) {
		current := installationInstallSameGenerationTarget(t, app, targetB, "B")
		current.mail, _ = current.mail.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		current.mail.pendingMessage = "B draft"
		current.mail.input.SetValue("B input")
		beforeLines := current.mail.input.LineCount()
		beforeViewport := current.mail.viewport.Height()
		beforeStatus := current.mail.statusFlash

		got, cmd := installationDeliverApp(t, current, done)
		if cmd != nil {
			t.Fatalf("stale target editor completion returned refresh/clear command producing %T", runCmd(cmd))
		}
		if got.mail.pendingMessage != "B draft" || got.mail.input.Value() != "B input" ||
			got.mail.input.LineCount() != beforeLines || got.mail.viewport.Height() != beforeViewport || got.mail.statusFlash != beforeStatus {
			t.Fatal("stale target editor completion changed B draft/input/hint/geometry")
		}
	})

	t.Run("same_directory_address_replacement_rejects", func(t *testing.T) {
		installationWriteAgent(t, targetA, "replacement-a@installation.test", "A", "A nickname")
		current := installationInstallSameGenerationTarget(t, app, targetA, "A")
		current.mail.pendingMessage = "replacement draft"
		current.mail.input.SetValue("replacement input")
		got, cmd := installationDeliverApp(t, current, done)
		if cmd != nil || got.mail.pendingMessage != "replacement draft" || got.mail.input.Value() != "replacement input" {
			t.Fatalf("old-address editor completion installed after directory reuse: pending=%q input=%q cmd=%T", got.mail.pendingMessage, got.mail.input.Value(), runCmd(cmd))
		}
	})

	t.Run("matching_completion_preserves_editor_effects", func(t *testing.T) {
		installationWriteAgent(t, targetA, "a@installation.test", "A", "A nickname")
		current := installationInstallSameGenerationTarget(t, app, targetA, "A")
		current.mail, _ = current.mail.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		beforeLines := current.mail.input.LineCount()
		beforeViewport := current.mail.viewport.Height()
		got, cmd := installationDeliverApp(t, current, done)
		if got.mail.pendingMessage != text || got.mail.input.Value() != text {
			t.Fatalf("matching editor text not preserved: pending=%q input=%q", got.mail.pendingMessage, got.mail.input.Value())
		}
		if got.mail.statusFlash == "" {
			t.Fatal("matching multiline editor completion did not preserve max-height editor hint")
		}
		if got.mail.input.LineCount() <= beforeLines || got.mail.viewport.Height() >= beforeViewport {
			t.Fatalf("matching editor geometry did not update: lines %d->%d viewport %d->%d", beforeLines, got.mail.input.LineCount(), beforeViewport, got.mail.viewport.Height())
		}

		messages := installationRunBatch(t, cmd)
		refreshRequests, clearScreens := 0, 0
		clearType := fmt.Sprintf("%T", tea.ClearScreen())
		for _, msg := range messages {
			switch typed := msg.(type) {
			case projectMailRefreshRequestMsg:
				refreshRequests++
				_ = installationProducedEnvelope(t, &typed, asyncSteadyRefresh)
			default:
				if fmt.Sprintf("%T", msg) == clearType {
					clearScreens++
				} else {
					t.Errorf("matching editor completion returned unexpected effect %T", msg)
				}
			}
		}
		if refreshRequests != 1 || clearScreens != 1 {
			t.Fatalf("matching editor effects: steady refresh=%d clear-screen=%d, want 1/1", refreshRequests, clearScreens)
		}
	})
}

func TestAsyncInventoryDisappearanceRejectsInstallForVisitedTarget(t *testing.T) {
	installationTestStart(t)
	gateApp, _, _ := installationVisitedApp(t, 3)
	gateRefresh := installationRefreshResult(t, &gateApp, true)
	gateEnvelope := installationProducedEnvelope(t, &gateRefresh, asyncInitialRebuild)
	if !gateEnvelope.target.inventoryBound {
		t.Fatal("visited target producer did not capture inventoryBound=true")
	}

	// Fail transparently until the real App/store resolver injection exists. This
	// is intentionally not replaced by a test-side acceptAsync wrapper.
	probeResolver := &installationInventoryResolver{record: installationExactResolvedTarget(gateEnvelope)}
	installationInjectResolver(t, &gateApp, probeResolver.resolve)

	for _, scenario := range []string{"disappeared", "changed_project", "became_ineligible", "changed_address", "nickname_only"} {
		wantAccept := scenario == "nickname_only"
		t.Run(scenario, func(t *testing.T) {
			t.Run("refresh_install", func(t *testing.T) {
				app, _, locations := installationVisitedApp(t, 3)
				refresh := installationRefreshResult(t, &app, true)
				envelope := installationProducedEnvelope(t, &refresh, asyncInitialRebuild)
				resolver := &installationInventoryResolver{record: installationExactResolvedTarget(envelope)}
				installationInjectResolver(t, &app, resolver.resolve)
				resolver.record = installationApplyResolverScenario(resolver.record, scenario)
				before := installationSnapshot(app, locations)

				got, cmd := installationDeliverApp(t, app, refresh)
				if wantAccept {
					if got.mailStore.version != before.storeVersion+1 || got.mail.acceptedSnapshot == nil || got.mail.initialLoading {
						t.Fatalf("nickname-only change rejected valid refresh: version=%d snapshot=%v loading=%v", got.mailStore.version, got.mail.acceptedSnapshot != nil, got.mail.initialLoading)
					}
				} else {
					if cmd != nil {
						t.Errorf("%s exact physical refresh rejection returned unexpected queued command producing %T", scenario, runCmd(cmd))
					}
					// The exact detached scan did finish, so it must release its physical
					// in-flight slot even though acceptAsync forbids publication.
					want := before
					want.refreshInFlight = false
					installationAssertAppState(t, scenario+"/refresh", got, locations, want)
				}
				if resolver.calls.Load() != 1 {
					t.Errorf("refresh resolver calls=%d, want 1", resolver.calls.Load())
				}
			})

			t.Run("history_count_install", func(t *testing.T) {
				app, _, _ := installationVisitedApp(t, 405)
				initial := installationRefreshResult(t, &app, true)
				envelope := installationProducedEnvelope(t, &initial, asyncInitialRebuild)
				resolver := &installationInventoryResolver{record: installationExactResolvedTarget(envelope)}
				installationInjectResolver(t, &app, resolver.resolve)
				app, _ = installationDeliverApp(t, app, initial)
				count := app.mail.historyCountCmd(app.mail.historyCountCache, app.mail.generation)().(mailHistoryCountMsg)
				_ = installationProducedEnvelope(t, &count, asyncExactHistoryCount)
				resolver.record = installationApplyResolverScenario(resolver.record, scenario)
				callsBefore := resolver.calls.Load()

				got, _ := installationDeliverApp(t, app, count)
				if got.mail.historyCountLoaded != wantAccept {
					t.Fatalf("history acceptance=%v, want %v for %s", got.mail.historyCountLoaded, wantAccept, scenario)
				}
				if resolver.calls.Load() != callsBefore+1 {
					t.Errorf("history resolver calls advanced %d, want 1", resolver.calls.Load()-callsBefore)
				}
			})

			t.Run("editor_install", func(t *testing.T) {
				app, _, _ := installationVisitedApp(t, 3)
				done := installationFakeEditorDone(t, app.mail, "visited editor text")
				envelope := installationEnvelope(t, &done)
				resolver := &installationInventoryResolver{record: installationExactResolvedTarget(envelope)}
				installationInjectResolver(t, &app, resolver.resolve)
				resolver.record = installationApplyResolverScenario(resolver.record, scenario)
				app.mail.pendingMessage = "visited draft"
				app.mail.input.SetValue("visited input")

				got, cmd := installationDeliverApp(t, app, done)
				if wantAccept {
					if got.mail.pendingMessage != "visited editor text" || cmd == nil {
						t.Fatalf("nickname-only editor completion rejected: pending=%q cmd=%v", got.mail.pendingMessage, cmd != nil)
					}
				} else if got.mail.pendingMessage != "visited draft" || got.mail.input.Value() != "visited input" || cmd != nil {
					t.Fatalf("%s editor completion installed: pending=%q input=%q cmd=%T", scenario, got.mail.pendingMessage, got.mail.input.Value(), runCmd(cmd))
				}
				if resolver.calls.Load() != 1 {
					t.Errorf("editor resolver calls=%d, want 1", resolver.calls.Load())
				}
			})
		})
	}
}
