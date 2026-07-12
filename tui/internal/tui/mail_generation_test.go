package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestMailModelIgnoresOldGenerationAsyncMessages(t *testing.T) {
	m := NewMailModel("", "", "", "", "agent", 10, "", "en", false, 0)
	m.generation = 2
	m.initialLoading = true
	m.homeTelemetryInFlight = true

	cases := []tea.Msg{
		mailRefreshMsg{generation: 1, initial: true, state: "active"},
		tickMsg{generation: 1},
		pulseTickMsg{generation: 1},
		homeTelemetryMsg{generation: 1, t: homeTelemetry{apiCalls: 9}},
		EditorDoneMsg{Generation: 1, Text: "old editor text"},
	}
	for _, msg := range cases {
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		if cmd != nil {
			t.Fatalf("stale %T returned a command; old generations must not reschedule timers", msg)
		}
	}
	if !m.initialLoading {
		t.Fatal("stale initial refresh should not clear loading")
	}
	if !m.homeTelemetryInFlight {
		t.Fatal("stale telemetry should not clear in-flight state")
	}
	if m.pendingMessage != "" || m.input.Value() != "" {
		t.Fatalf("stale editor completion contaminated input: pending=%q input=%q", m.pendingMessage, m.input.Value())
	}
}

func TestReturnFromVisitResumesInitialLoadingWithNewGenerationRebuild(t *testing.T) {
	a := visitTestApp(t)
	origGen := a.mail.generation
	a.mail.initialLoading = true

	visited, _ := a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target"), "worker", "Worker")})
	targetGen := visited.mail.generation
	model, cmd := visited.Update(mailRefreshMsg{generation: origGen, initial: true, state: "ACTIVE"})
	if cmd != nil {
		t.Fatalf("stale original initial completion returned cmd %T", runCmd(cmd))
	}
	visited = model.(App)
	if visited.mail.generation != targetGen {
		t.Fatalf("stale original completion changed target generation: got %d want %d", visited.mail.generation, targetGen)
	}

	restored, resumeCmd := visited.returnFromVisit()
	if !restored.mail.initialLoading {
		t.Fatal("restored mail should still be loading before resumed initial rebuild lands")
	}
	if restored.mail.generation == origGen || restored.mail.generation == targetGen {
		t.Fatalf("restore generation = %d, want new generation beyond orig %d and target %d", restored.mail.generation, origGen, targetGen)
	}
	cmds := resumeBatchCommands(t, resumeCmd)
	if len(cmds) != 4 {
		t.Fatalf("resume should arm one rebuild/refresh, one poll, one pulse, and size; got %d commands", len(cmds))
	}
	msg, ok := cmds[0]().(mailRefreshMsg)
	if !ok {
		t.Fatalf("first resume command produced %T, want mailRefreshMsg", cmds[0]())
	}
	if !msg.initial || msg.generation != restored.mail.generation {
		t.Fatalf("resume command = initial %v generation %d, want initial true generation %d", msg.initial, msg.generation, restored.mail.generation)
	}
	updated, _ := restored.mail.Update(msg)
	if updated.initialLoading {
		t.Fatal("new-generation initial rebuild should clear loading")
	}

	stale := restored.mail
	stale, cmd = stale.Update(mailRefreshMsg{generation: targetGen, initial: true, state: "ACTIVE"})
	if cmd != nil {
		t.Fatalf("stale target refresh returned cmd %T", runCmd(cmd))
	}
	if !stale.initialLoading || stale.orchState == "ACTIVE" {
		t.Fatalf("stale target refresh mutated restored mail: loading=%v state=%q", stale.initialLoading, stale.orchState)
	}
}

func TestReturnFromVisitClearsTelemetryInFlightAndAllowsNewFetch(t *testing.T) {
	a := visitTestApp(t)
	origGen := a.mail.generation
	a.mail.initialLoading = false
	a.mail.homeTelemetryInFlight = true
	a.mail.homeTelemetryLoaded = false

	visited, _ := a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target"), "worker", "Worker")})
	model, cmd := visited.Update(homeTelemetryMsg{generation: origGen, t: homeTelemetry{apiCalls: 9}})
	if cmd != nil {
		t.Fatalf("stale original telemetry returned cmd %T", runCmd(cmd))
	}
	visited = model.(App)

	restored, resumeCmd := visited.returnFromVisit()
	if restored.mail.homeTelemetryInFlight {
		t.Fatal("resume should clear activation-local telemetry in-flight flag")
	}
	cmds := resumeBatchCommands(t, resumeCmd)
	if len(cmds) != 4 {
		t.Fatalf("resume should arm one refresh, one poll, one pulse, and size; got %d commands", len(cmds))
	}
	msg, ok := cmds[0]().(mailRefreshMsg)
	if !ok {
		t.Fatalf("first resume command produced %T, want mailRefreshMsg", cmds[0]())
	}
	if msg.initial {
		t.Fatal("non-loading resume should start ordinary refresh, not initial rebuild")
	}
	if msg.generation != restored.mail.generation {
		t.Fatalf("refresh generation = %d, want %d", msg.generation, restored.mail.generation)
	}

	if telemetryCmd := restored.mail.maybeScheduleHomeTelemetry(time.Now()); telemetryCmd == nil {
		t.Fatal("cleared telemetry in-flight flag should allow a new fetch command")
	}
	if !restored.mail.homeTelemetryInFlight {
		t.Fatal("new telemetry fetch should mark in-flight")
	}
	updated, _ := restored.mail.Update(homeTelemetryMsg{generation: restored.mail.generation, t: homeTelemetry{apiCalls: 1}})
	if updated.homeTelemetryInFlight || !updated.homeTelemetryLoaded {
		t.Fatalf("current telemetry completion did not land: inFlight=%v loaded=%v", updated.homeTelemetryInFlight, updated.homeTelemetryLoaded)
	}
}

func TestBlockedInitialRebuildDoesNotBlockRootInteraction(t *testing.T) {
	a := visitTestApp(t)
	started := make(chan struct{})
	release := make(chan struct{}, 1)
	completed := make(chan tea.Msg, 1)
	released := false
	defer func() {
		if !released {
			release <- struct{}{}
		}
	}()
	a.mail.beforeRebuild = func() {
		close(started)
		<-release
	}
	load := a.mail.initialRebuild
	go func() { completed <- load() }()
	<-started

	model, _ := a.Update(ViewChangeMsg{View: "help"})
	got := model.(App)
	if got.currentView != appViewHelp {
		t.Fatalf("view switch while loader blocked = %v, want help", got.currentView)
	}
	model, _ = got.Update(tea.WindowSizeMsg{Width: 91, Height: 27})
	got = model.(App)
	if got.width != 91 || got.height != 27 {
		t.Fatalf("resize while loader blocked = %dx%d, want 91x27", got.width, got.height)
	}
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	keyMsg := runCmd(cmd)
	if _, ok := keyMsg.(MarkdownViewerCloseMsg); !ok {
		t.Fatalf("key handling while loader blocked produced %T, want MarkdownViewerCloseMsg", keyMsg)
	}
	_, quitCmd := got.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if _, ok := runCmd(quitCmd).(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c while loader blocked produced %T, want tea.QuitMsg", runCmd(quitCmd))
	}

	beforeHelp := struct {
		cursor, focus, width, height, leftY, rightY int
		ready                                       bool
	}{
		cursor: got.help.inner.cursor,
		focus:  got.help.inner.focus,
		width:  got.help.inner.width,
		height: got.help.inner.height,
		leftY:  got.help.inner.leftVP.YOffset(),
		rightY: got.help.inner.rightVP.YOffset(),
		ready:  got.help.inner.ready,
	}
	release <- struct{}{}
	released = true
	completion := <-completed
	model, telemetryCmd := got.Update(completion)
	got = model.(App)
	if got.currentView != appViewHelp || got.mail.initialLoading {
		t.Fatalf("completion after blocked interaction: view=%v loading=%v", got.currentView, got.mail.initialLoading)
	}
	if telemetryCmd == nil || !got.mail.homeTelemetryInFlight {
		t.Fatal("accepted hidden-mail completion did not schedule telemetry")
	}
	telemetryMsg := runCmd(telemetryCmd)
	if _, ok := telemetryMsg.(homeTelemetryMsg); !ok {
		t.Fatalf("hidden-mail command produced %T, want homeTelemetryMsg", telemetryMsg)
	}
	model, followup := got.Update(telemetryMsg)
	if followup != nil {
		t.Fatalf("routed telemetry completion returned unexpected follow-up %T", runCmd(followup))
	}
	got = model.(App)
	afterHelp := struct {
		cursor, focus, width, height, leftY, rightY int
		ready                                       bool
	}{
		cursor: got.help.inner.cursor,
		focus:  got.help.inner.focus,
		width:  got.help.inner.width,
		height: got.help.inner.height,
		leftY:  got.help.inner.leftVP.YOffset(),
		rightY: got.help.inner.rightVP.YOffset(),
		ready:  got.help.inner.ready,
	}
	if got.currentView != appViewHelp || afterHelp != beforeHelp {
		t.Fatalf("mail/telemetry completions changed Help state: before=%+v after=%+v", beforeHelp, afterHelp)
	}
	if got.mail.homeTelemetryInFlight || !got.mail.homeTelemetryLoaded {
		t.Fatalf("telemetry completion did not apply exactly once: inFlight=%v loaded=%v", got.mail.homeTelemetryInFlight, got.mail.homeTelemetryLoaded)
	}
}

func TestInitialRebuildDoesNotMutateInstalledCacheBeforeAcceptance(t *testing.T) {
	root := t.TempDir()
	humanDir := filepath.Join(root, "human")
	orchDir := filepath.Join(root, "orch")
	writeMailGenerationEvent(t, orchDir, "command-local history")

	m := NewMailModel(humanDir, "human", root, orchDir, "agent", unlimitedPageSize, "", "en", false, 0)
	installed := m.sessionCache
	msg := m.initialRebuild()

	if got := installed.Len(); got != 0 {
		t.Fatalf("initial rebuild mutated the installed cache before acceptance: got %d entries", got)
	}
	if _, err := os.Stat(humanDir); !os.IsNotExist(err) {
		t.Fatalf("detached initial rebuild touched human filesystem before acceptance: %v", err)
	}
	updated, _ := m.Update(msg)
	if updated.sessionCache == installed {
		t.Fatal("accepted initial rebuild did not install its command-local session cache")
	}
	if got := updated.sessionCache.Len(); got == 0 {
		t.Fatal("accepted initial rebuild installed an empty session cache")
	}
	if _, err := os.Stat(filepath.Join(humanDir, "logs", "session.jsonl")); err != nil {
		t.Fatalf("accepted initial rebuild did not persist its derived cache: %v", err)
	}
}

func TestAppRoutesInitialMailCompletionWhileProjectsActive(t *testing.T) {
	a := visitTestApp(t)
	a.mail.verbose = verboseThinking
	writeMailGenerationEvent(t, a.orchDir, "projects-time completion")
	msg := a.mail.initialRebuild()

	model, _ := a.Update(ViewChangeMsg{View: "projects"})
	got := model.(App)
	if got.currentView != appViewProjects {
		t.Fatalf("real App.Update transition entered %v, want projects", got.currentView)
	}
	model, _ = got.Update(tea.WindowSizeMsg{Width: 93, Height: 31})
	got = model.(App)
	if got.width != 93 || got.height != 31 || got.currentView != appViewProjects {
		t.Fatalf("projects resize through App.Update: view=%v size=%dx%d", got.currentView, got.width, got.height)
	}
	model, _ = got.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	got = model.(App)
	if !got.selectMode {
		t.Fatal("projects key message did not reach root select-mode handling")
	}
	model, _ = got.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	got = model.(App)
	if got.selectMode {
		t.Fatal("second projects key message did not leave root select mode")
	}
	beforeProjects := struct {
		cursor, width, height, viewportY int
		requestSeq                       uint64
		loadErr, status                  string
		ready                            bool
	}{
		cursor:     got.projects.cursor,
		width:      got.projects.width,
		height:     got.projects.height,
		viewportY:  got.projects.viewport.YOffset(),
		requestSeq: got.projects.requestSeq,
		loadErr:    got.projects.loadErr,
		status:     got.projects.status,
		ready:      got.projects.ready,
	}
	model, telemetryCmd := got.Update(msg)
	got = model.(App)
	if got.currentView != appViewProjects {
		t.Fatalf("mail completion changed active view to %v; want projects", got.currentView)
	}
	if got.mail.initialLoading {
		t.Fatal("mail completion was lost while projects was active")
	}
	if telemetryCmd == nil || !got.mail.homeTelemetryInFlight {
		t.Fatal("projects-time mail completion did not schedule telemetry")
	}
	telemetryMsg := runCmd(telemetryCmd)
	if _, ok := telemetryMsg.(homeTelemetryMsg); !ok {
		t.Fatalf("projects-time mail command produced %T, want homeTelemetryMsg", telemetryMsg)
	}
	model, followup := got.Update(telemetryMsg)
	if followup != nil {
		t.Fatalf("routed projects-time telemetry returned unexpected follow-up %T", runCmd(followup))
	}
	got = model.(App)
	afterProjects := struct {
		cursor, width, height, viewportY int
		requestSeq                       uint64
		loadErr, status                  string
		ready                            bool
	}{
		cursor:     got.projects.cursor,
		width:      got.projects.width,
		height:     got.projects.height,
		viewportY:  got.projects.viewport.YOffset(),
		requestSeq: got.projects.requestSeq,
		loadErr:    got.projects.loadErr,
		status:     got.projects.status,
		ready:      got.projects.ready,
	}
	if got.currentView != appViewProjects || afterProjects != beforeProjects {
		t.Fatalf("mail/telemetry completions changed Projects state: before=%+v after=%+v", beforeProjects, afterProjects)
	}
	if got.mail.homeTelemetryInFlight || !got.mail.homeTelemetryLoaded {
		t.Fatalf("projects-time telemetry did not apply exactly once: inFlight=%v loaded=%v", got.mail.homeTelemetryInFlight, got.mail.homeTelemetryLoaded)
	}

	model, _ = got.Update(ViewChangeMsg{View: "mail"})
	got = model.(App)
	if got.mail.initialLoading {
		t.Fatal("mail was still loading after returning from projects")
	}
	matches := 0
	for _, message := range got.mail.messages {
		if message.Body == "projects-time completion" {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("accepted projects-time completion appeared %d times; want exactly once", matches)
	}
}

func TestLateInitialRebuildCannotMutateCurrentGenerationCache(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := t.TempDir()
	writeMailGenerationEvent(t, orchDir, "generation B")

	a := App{currentView: appViewMail}
	a.installMailModel(NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", unlimitedPageSize, "", "en", false, 0))
	a.mail.verbose = verboseThinking
	lateA := a.mail.initialRebuild

	// Install generation B from the same preserved model. This mirrors returning
	// to a preserved mail model: generations differ, but the pre-fix cache pointer
	// was shared by both command closures.
	a.installMailModel(a.mail)
	model, _ := a.Update(a.mail.initialRebuild())
	a = model.(App)
	beforeEntries := a.mail.sessionCache.Entries()
	beforeMessages := append([]ChatMessage(nil), a.mail.messages...)
	sessionPath := filepath.Join(humanDir, "logs", "session.jsonl")
	beforeFile, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}

	appendMailGenerationEvent(t, orchDir, "late generation A")
	staleMsg := lateA()
	if got := a.mail.sessionCache.Entries(); !reflect.DeepEqual(got, beforeEntries) {
		t.Fatalf("late generation A mutated generation B cache before acceptance:\n got %#v\nwant %#v", got, beforeEntries)
	}
	if got, err := os.ReadFile(sessionPath); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(got, beforeFile) {
		t.Fatal("late generation A rewrote generation B's persisted session cache before acceptance")
	}

	model, cmd := a.Update(staleMsg)
	if cmd != nil {
		t.Fatalf("stale initial completion returned command %T", runCmd(cmd))
	}
	got := model.(App)
	if !reflect.DeepEqual(got.mail.sessionCache.Entries(), beforeEntries) {
		t.Fatal("rejected generation A changed generation B cache")
	}
	if !reflect.DeepEqual(got.mail.messages, beforeMessages) {
		t.Fatalf("rejected generation A changed generation B visible projection:\n got %#v\nwant %#v", got.mail.messages, beforeMessages)
	}
	if afterFile, err := os.ReadFile(sessionPath); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(afterFile, beforeFile) {
		t.Fatal("rejected generation A changed generation B's canonical session.jsonl")
	}
}

func writeMailGenerationEvent(t *testing.T, orchDir, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"ts":1781300001,"type":"text_output","text":"` + text + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(orchDir, "logs", "events.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendMailGenerationEvent(t *testing.T, orchDir, text string) {
	t.Helper()
	line := `{"ts":1781300002,"type":"text_output","text":"` + text + `"}` + "\n"
	f, err := os.OpenFile(filepath.Join(orchDir, "logs", "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func resumeBatchCommands(t *testing.T, cmd tea.Cmd) tea.BatchMsg {
	t.Helper()
	msg := runCmd(cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("resume command produced %T, want tea.BatchMsg", msg)
	}
	return batch
}
