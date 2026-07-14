package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

func visitTestApp(t *testing.T) App {
	t.Helper()
	root := t.TempDir()
	global := filepath.Join(root, "global")
	originalProject := filepath.Join(root, "original", ".lingtai")
	originalAgent := filepath.Join(originalProject, "orig")
	a := App{
		currentView: appViewMail,
		globalDir:   global,
		projectDir:  originalProject,
		orchDir:     originalAgent,
		orchName:    "orig",
		tuiConfig:   config.DefaultTUIConfig(),
		width:       100,
		height:      30,
	}
	a.installMailModel(NewMailModel(filepath.Join(originalProject, "human"), "human", originalProject, originalAgent, "orig", 20, global, "en", false, 0))
	return a
}

func visitRecord(projectRoot, agent, name string) inventory.Record {
	return inventory.Record{
		PID:       42,
		Role:      inventory.RoleAgent,
		Agent:     agent,
		Project:   projectRoot,
		AgentDir:  filepath.Join(projectRoot, ".lingtai", agent),
		AgentName: name,
		Address:   agent,
		State:     "IDLE",
		Enterable: true,
	}
}

func TestAppEnterVisitedAgentTransition(t *testing.T) {
	a := visitTestApp(t)
	targetProject := filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target")
	rec := visitRecord(targetProject, "worker", "Worker")

	updated, _ := a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: rec})

	if !updated.visiting {
		t.Fatal("expected visiting=true")
	}
	if updated.projectDir != filepath.Join(targetProject, ".lingtai") {
		t.Fatalf("projectDir = %q", updated.projectDir)
	}
	if updated.orchDir != rec.AgentDir || updated.orchName != "Worker" {
		t.Fatalf("focused agent = (%q,%q)", updated.orchDir, updated.orchName)
	}
	if updated.mail.humanDir != filepath.Join(targetProject, ".lingtai", "human") {
		t.Fatalf("human mailbox = %q", updated.mail.humanDir)
	}
	if updated.currentView != appViewMail {
		t.Fatalf("currentView = %v, want mail", updated.currentView)
	}
	if updated.topChromeRows() != 0 {
		t.Fatalf("visiting should not reserve root chrome rows, got %d", updated.topChromeRows())
	}
	if !updated.mail.visitExitHint {
		t.Fatal("visited target mail should carry inline exit hint")
	}
	model, _ := updated.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	view := ansi.Strip(model.(App).View().Content)
	if n := strings.Count(view, "esc-esc to exit"); n != 1 {
		t.Fatalf("visited mail title should include one inline exit hint, got %d:\n%s", n, view)
	}
}

func TestRepeatedVisitedSwitchPreservesFirstOriginal(t *testing.T) {
	a := visitTestApp(t)
	originalProject := a.projectDir
	originalAgent := a.orchDir

	first, _ := a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target1"), "a", "A")})
	second, _ := first.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target2"), "b", "B")})

	if second.visitReturn == nil || second.visitReturn.projectDir != originalProject || second.visitReturn.orchDir != originalAgent {
		t.Fatalf("original changed after repeated switch: return=%+v", second.visitReturn)
	}
	if second.orchName != "B" {
		t.Fatalf("target should update to second selection, got %q", second.orchName)
	}
	if !second.mail.visitExitHint {
		t.Fatal("repeated visited switch should set hint on the new target mail")
	}
}

func TestReturnFromVisitRestoresOriginalContextAndBumpsGeneration(t *testing.T) {
	a := visitTestApp(t)
	origProject := a.projectDir
	origAgent := a.orchDir
	origGen := a.mail.generation
	visited, _ := a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target"), "worker", "Worker")})

	restored, _ := visited.returnFromVisit()

	if restored.visiting {
		t.Fatal("visiting should be false after return")
	}
	if restored.projectDir != origProject || restored.orchDir != origAgent {
		t.Fatalf("restored context = (%q,%q), want (%q,%q)", restored.projectDir, restored.orchDir, origProject, origAgent)
	}
	if restored.mail.generation == origGen || restored.mail.generation == visited.mail.generation {
		t.Fatalf("mail generation should bump on restore, got %d (orig %d visited %d)", restored.mail.generation, origGen, visited.mail.generation)
	}
	if restored.mail.visitExitHint {
		t.Fatal("restored original mail should not keep target visit hint")
	}
}

func TestDoubleEscFastSlowAndDisarm(t *testing.T) {
	oldNow := appNow
	t.Cleanup(func() { appNow = oldNow })
	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	appNow = func() time.Time { return base }

	a := visitTestApp(t)
	a, _ = a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target"), "worker", "Worker")})

	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	first := model.(App)
	if !first.visiting || !first.doubleEscArmed {
		t.Fatalf("first eligible esc should arm but not return: visiting=%v armed=%v", first.visiting, first.doubleEscArmed)
	}

	appNow = func() time.Time { return base.Add(500 * time.Millisecond) }
	model, _ = first.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if model.(App).visiting {
		t.Fatal("second fast esc should return original")
	}

	appNow = func() time.Time { return base }
	a, _ = a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target2"), "worker", "Worker")})
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	slowFirst := model.(App)
	appNow = func() time.Time { return base.Add(700 * time.Millisecond) }
	model, _ = slowFirst.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !model.(App).visiting {
		t.Fatal("slow second esc should not return")
	}

	appNow = func() time.Time { return base }
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	disarm := model.(App)
	model, _ = disarm.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	disarm = model.(App)
	appNow = func() time.Time { return base.Add(100 * time.Millisecond) }
	model, _ = disarm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !model.(App).visiting {
		t.Fatal("non-esc input should disarm double esc")
	}
}

func TestDoubleEscFromVisitedMailReturnsToPreservedProjectsFirst(t *testing.T) {
	oldNow := appNow
	t.Cleanup(func() { appNow = oldNow })
	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	appNow = func() time.Time { return base }

	a := visitTestApp(t)
	origProject := a.projectDir
	origAgent := a.orchDir
	origMailGeneration := a.mail.generation

	opened, _ := a.openProjectsView()
	a = opened
	a.projects.rows = []projectRow{
		{kind: projectRowGroup, project: filepath.Dir(origProject)},
		{kind: projectRowAgent, project: filepath.Dir(origProject), record: visitRecord(filepath.Dir(origProject), "orig", "Orig")},
		{kind: projectRowAgent, project: filepath.Join(filepath.Dir(filepath.Dir(origProject)), "target"), record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(origProject)), "target"), "worker", "Worker")},
	}
	a.projects.cursor = 2
	a.projects.status = "preserved status"

	visited, _ := a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: a.projects.rows[2].record})
	if visited.currentView != appViewMail || !visited.visiting {
		t.Fatalf("precondition: enter should open visited mail, view=%v visiting=%v", visited.currentView, visited.visiting)
	}

	model, _ := visited.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	first := model.(App)
	appNow = func() time.Time { return base.Add(100 * time.Millisecond) }
	model, _ = first.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := model.(App)

	if got.currentView != appViewProjects {
		t.Fatalf("double Esc returned to view %v, want /projects", got.currentView)
	}
	if got.visiting || got.topChromeRows() != 0 {
		t.Fatalf("visit state not cleared: visiting=%v topChromeRows=%d", got.visiting, got.topChromeRows())
	}
	if got.projectDir != origProject || got.orchDir != origAgent {
		t.Fatalf("restored context = (%q,%q), want (%q,%q)", got.projectDir, got.orchDir, origProject, origAgent)
	}
	if got.mail.generation == origMailGeneration || got.mail.baseDir != origProject || got.mail.orchestrator != origAgent {
		t.Fatalf("original mail not resumed with restored context: gen=%d origGen=%d project=%q orch=%q", got.mail.generation, origMailGeneration, got.mail.baseDir, got.mail.orchestrator)
	}
	if got.mail.visitExitHint {
		t.Fatal("preserved original mail should not keep target visit hint")
	}
	if got.projects.cursor != 2 || got.projects.status != "preserved status" || len(got.projects.rows) != 3 {
		t.Fatalf("projects model was not preserved: cursor=%d status=%q rows=%d", got.projects.cursor, got.projects.status, len(got.projects.rows))
	}

	model, cmd := got.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	cmdMsg := runCmd(cmd)
	msg, ok := cmdMsg.(ViewChangeMsg)
	if !ok || msg.View != "mail" {
		t.Fatalf("subsequent Esc from preserved /projects produced %T %#v, want ViewChangeMsg mail", cmdMsg, cmdMsg)
	}
	model, _ = model.(App).Update(msg)
	if model.(App).currentView != appViewMail {
		t.Fatalf("subsequent Esc from preserved /projects routed to view %v, want mail", model.(App).currentView)
	}
}

func TestDoubleEscFirstEscIsSwallowedByRoot(t *testing.T) {
	a := visitTestApp(t)
	a, _ = a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target"), "worker", "Worker")})
	a.mail.messages = []ChatMessage{{Type: "insight", Timestamp: "insight-1"}}

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := model.(App)
	if cmd != nil {
		t.Fatalf("first arming esc returned cmd %T", runCmd(cmd))
	}
	if !got.doubleEscArmed {
		t.Fatal("first neutral mail esc should arm return chord")
	}
	if got.mail.dismissedInsights["insight-1"] {
		t.Fatal("first root-owned esc should not be forwarded to mail insight dismissal")
	}
}

func TestDoubleEscSafetyForCopySelectSearchAndPicker(t *testing.T) {
	a := visitTestApp(t)
	a, _ = a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target"), "worker", "Worker")})

	copyApp := a
	copyApp.mail.copyMode = true
	model, _ := copyApp.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := model.(App)
	if got.doubleEscArmed || got.mail.copyMode {
		t.Fatalf("copy-mode esc should be child-owned and only exit copy mode: armed=%v copy=%v", got.doubleEscArmed, got.mail.copyMode)
	}

	editorApp := a
	editorApp.mail.showEditorWarn = true
	model, _ = editorApp.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got = model.(App)
	if got.doubleEscArmed || got.mail.showEditorWarn {
		t.Fatalf("editor-warning esc should be child-owned and close warning only: armed=%v warning=%v", got.doubleEscArmed, got.mail.showEditorWarn)
	}

	selectApp := a
	selectApp.currentView = appViewMailbox
	selectApp.selectMode = true
	model, _ = selectApp.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got = model.(App)
	if got.doubleEscArmed || got.selectMode {
		t.Fatalf("select-mode esc should be child-owned by root select mode: armed=%v select=%v", got.doubleEscArmed, got.selectMode)
	}

	searchApp := a
	searchApp.currentView = appViewMailbox
	searchApp.mailbox = NewMailboxModel(searchApp.projectDir)
	searchApp.mailbox.searchMode = true
	model, _ = searchApp.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got = model.(App)
	if got.doubleEscArmed {
		t.Fatal("mailbox search esc must not arm visit return")
	}

	pickerApp := a
	pickerApp.currentView = appViewDaemons
	pickerApp.daemons = NewDaemonsModel(a.projectDir, a.orchDir)
	pickerApp.daemons.pickerOpen = true
	model, _ = pickerApp.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got = model.(App)
	if got.doubleEscArmed {
		t.Fatal("picker esc must not arm visit return")
	}
}

func TestDoubleEscFailClosedForNonMailAndUnknownViews(t *testing.T) {
	a := visitTestApp(t)
	a, _ = a.enterVisitedAgent(ProjectsAgentSelectedMsg{Record: visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target"), "worker", "Worker")})

	nonMail := a
	nonMail.currentView = appViewHelp
	nonMail.help = NewHelpModel()
	model, _ := nonMail.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got := model.(App)
	if got.doubleEscArmed || !got.visiting {
		t.Fatalf("non-mail esc should be child-owned: armed=%v visiting=%v", got.doubleEscArmed, got.visiting)
	}

	unknown := a
	unknown.currentView = appView(999)
	model, _ = unknown.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	got = model.(App)
	if got.doubleEscArmed || !got.visiting {
		t.Fatalf("unknown view esc should fail closed: armed=%v visiting=%v", got.doubleEscArmed, got.visiting)
	}
}

func TestProjectsSelectionRequiresActiveProjectsViewActivationAndRequest(t *testing.T) {
	a := visitTestApp(t)
	target := visitRecord(filepath.Join(filepath.Dir(filepath.Dir(a.projectDir)), "target"), "worker", "Worker")

	opened, _ := a.openProjectsView()
	currentRequest := opened.projects.requestSeq
	wrongActivation := opened
	model, _ := wrongActivation.Update(ProjectsAgentSelectedMsg{ActivationID: opened.projects.activationID + 1, RequestSeq: currentRequest, Record: target})
	if model.(App).visiting {
		t.Fatal("selection with wrong activation should not enter visit")
	}

	staleRequest := opened
	staleRequest.projects.nextRequestSeq()
	model, _ = staleRequest.Update(ProjectsAgentSelectedMsg{ActivationID: opened.projects.activationID, RequestSeq: currentRequest, Record: target})
	if model.(App).visiting {
		t.Fatal("selection from an older request should not enter visit")
	}

	leftView := opened
	leftView.currentView = appViewMail
	model, _ = leftView.Update(ProjectsAgentSelectedMsg{ActivationID: opened.projects.activationID, RequestSeq: currentRequest, Record: target})
	if model.(App).visiting {
		t.Fatal("delayed selection after leaving /projects should not enter visit")
	}

	model, _ = opened.Update(ProjectsAgentSelectedMsg{ActivationID: opened.projects.activationID, RequestSeq: currentRequest, Record: target})
	if !model.(App).visiting {
		t.Fatal("matching active /projects selection should enter visit")
	}
}
