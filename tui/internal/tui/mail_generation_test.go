package tui

import (
	"path/filepath"
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

func resumeBatchCommands(t *testing.T, cmd tea.Cmd) tea.BatchMsg {
	t.Helper()
	msg := runCmd(cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("resume command produced %T, want tea.BatchMsg", msg)
	}
	return batch
}
