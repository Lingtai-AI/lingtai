package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

func projectRecord(project, agent, name string, enterable bool) inventory.Record {
	r := inventory.Record{
		PID:          100,
		Uptime:       "1m 0s",
		Role:         inventory.RoleAgent,
		Agent:        agent,
		Project:      project,
		AgentDir:     filepath.Join(project, ".lingtai", agent),
		Address:      agent,
		AgentName:    name,
		State:        "IDLE",
		AdminSummary: "admin={}",
		Enterable:    enterable,
	}
	if !enterable {
		r.EnterReason = inventory.EnterReasonManifestUnreadable
		r.EnterDetail = "manifest unreadable"
	}
	return r
}

func projectsInventoryForModel(m ProjectsModel, snap inventory.Snapshot) projectsInventoryMsg {
	return projectsInventoryMsg{activationID: m.activationID, requestSeq: m.requestSeq, snapshot: snap}
}

func withProjectsScan(t *testing.T, scan func(inventory.Options) (inventory.Snapshot, error)) {
	t.Helper()
	old := projectsScanInventory
	projectsScanInventory = scan
	t.Cleanup(func() { projectsScanInventory = old })
}

func TestProjectsModelGroupedRowsMarkersAndEnter(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current")
	other := filepath.Join(root, "other")
	origDir := filepath.Join(current, ".lingtai", "orig")
	target := projectRecord(other, "agent-b", "Agent B", true)
	snap := inventory.Snapshot{
		Records: []inventory.Record{target},
		Groups:  []inventory.Group{{Project: other, Records: []inventory.Record{target}}},
	}
	m := NewProjectsModel("", filepath.Join(other, ".lingtai"), ProjectsContext{
		FocusedAgentDir:    target.AgentDir,
		OriginalProjectDir: filepath.Join(current, ".lingtai"),
		OriginalAgentDir:   origDir,
		Visiting:           true,
	})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	if len(m.rows) != 2 || m.rows[0].kind != projectRowGroup || m.rows[1].kind != projectRowAgent {
		t.Fatalf("unexpected rows: %+v", m.rows)
	}
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want first agent row", m.cursor)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"other", "Agent B", "[visiting]", "pid 100", "1m 0s"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	withProjectsScan(t, func(inventory.Options) (inventory.Snapshot, error) {
		return snap, nil
	})
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	validation, ok := runCmd(cmd).(projectsValidationMsg)
	if !ok {
		t.Fatalf("enter produced %T, want projectsValidationMsg", runCmd(cmd))
	}
	m, cmd = m.Update(validation)
	msg, ok := runCmd(cmd).(ProjectsAgentSelectedMsg)
	if !ok {
		t.Fatalf("validation produced %T, want ProjectsAgentSelectedMsg", runCmd(cmd))
	}
	if msg.Record.AgentDir != target.AgentDir {
		t.Fatalf("selected %q, want %q", msg.Record.AgentDir, target.AgentDir)
	}
	if msg.ActivationID != m.activationID {
		t.Fatalf("selection activation = %d, want %d", msg.ActivationID, m.activationID)
	}
	if msg.RequestSeq != m.requestSeq {
		t.Fatalf("selection request = %d, want %d", msg.RequestSeq, m.requestSeq)
	}
}

func TestProjectsModelDisabledRowFailsLoud(t *testing.T) {
	project := t.TempDir()
	rec := projectRecord(project, "bad", "Bad", false)
	snap := inventory.Snapshot{
		Records: []inventory.Record{rec},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{rec}}},
	}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("disabled enter returned cmd")
	}
	if !strings.Contains(m.status, "Manifest unreadable") {
		t.Fatalf("status = %q, want localized enter reason", m.status)
	}
}

func TestProjectsDisabledReasonRendersInEachLocale(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	project := t.TempDir()
	rec := projectRecord(project, "human", "Human", false)
	rec.EnterReason = inventory.EnterReasonHuman
	rec.EnterDetail = ""
	snap := inventory.Snapshot{
		Records: []inventory.Record{rec},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{rec}}},
	}
	for _, lang := range []string{"en", "zh", "wen"} {
		t.Run(lang, func(t *testing.T) {
			if err := i18n.SetLang(lang); err != nil {
				t.Fatal(err)
			}
			m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
			m, _ = m.Update(tea.WindowSizeMsg{Width: 220, Height: 24})
			m, _ = m.Update(projectsInventoryForModel(m, snap))
			view := ansi.Strip(m.View())
			want := i18n.TIn(lang, "projects.enter_reason_human")
			if !strings.Contains(view, want) {
				t.Fatalf("view missing localized reason %q:\n%s", want, view)
			}
			if lang != "en" && strings.Contains(view, "human mailbox is not a running agent target") {
				t.Fatalf("view leaked English shared reason in %s:\n%s", lang, view)
			}
		})
	}
}

func TestProjectsNonAdminReasonRendersInEachLocale(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	project := t.TempDir()
	rec := projectRecord(project, "worker", "Worker", false)
	rec.EnterReason = inventory.EnterReasonNonAdmin
	snap := inventory.Snapshot{
		Records: []inventory.Record{rec},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{rec}}},
	}
	for _, lang := range []string{"en", "zh", "wen"} {
		t.Run(lang, func(t *testing.T) {
			if err := i18n.SetLang(lang); err != nil {
				t.Fatal(err)
			}
			m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
			m, _ = m.Update(tea.WindowSizeMsg{Width: 220, Height: 24})
			m, _ = m.Update(projectsInventoryForModel(m, snap))
			view := ansi.Strip(m.View())
			want := i18n.TIn(lang, "projects.enter_reason_non_admin")
			if !strings.Contains(view, want) {
				t.Fatalf("view missing localized non-admin reason %q:\n%s", want, view)
			}
			m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if cmd != nil {
				t.Fatal("non-admin row should not emit a selection command")
			}
			if !strings.Contains(m.status, want) {
				t.Fatalf("status = %q, want localized non-admin reason %q", m.status, want)
			}
		})
	}
}

func TestProjectsModelRefreshKeysReturnLoadCommand(t *testing.T) {
	m := NewProjectsModel(t.TempDir(), t.TempDir(), ProjectsContext{})
	if _, cmd := m.Update(ctrlR()); cmd == nil {
		t.Fatal("ctrl+r should rescan")
	}
	if _, cmd := m.Update(bareR()); cmd == nil {
		t.Fatal("bare r should rescan")
	}
}

func TestProjectsModelDropsOutOfOrderInventoryResults(t *testing.T) {
	root := t.TempDir()
	projectA := filepath.Join(root, "a")
	projectB := filepath.Join(root, "b")
	recA := projectRecord(projectA, "agent-a", "Agent A", true)
	recB := projectRecord(projectB, "agent-b", "Agent B", true)
	snapA := inventory.Snapshot{Records: []inventory.Record{recA}, Groups: []inventory.Group{{Project: projectA, Records: []inventory.Record{recA}}}}
	snapB := inventory.Snapshot{Records: []inventory.Record{recB}, Groups: []inventory.Group{{Project: projectB, Records: []inventory.Record{recB}}}}

	m := NewProjectsModel("", filepath.Join(projectA, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = m.Update(ctrlR())
	latestSeq := m.requestSeq

	m, _ = m.Update(projectsInventoryMsg{activationID: m.activationID, requestSeq: latestSeq, snapshot: snapB})
	m, _ = m.Update(projectsInventoryMsg{activationID: m.activationID, requestSeq: latestSeq - 1, snapshot: snapA})

	if row, ok := m.selectedAgentRow(); !ok || row.record.AgentName != "Agent B" {
		t.Fatalf("older completion overwrote newer rows: cursor row=%+v ok=%v", row, ok)
	}
}

func TestProjectsModelDropsOldActivationInventoryResult(t *testing.T) {
	root := t.TempDir()
	projectOld := filepath.Join(root, "old")
	projectNew := filepath.Join(root, "new")
	oldRec := projectRecord(projectOld, "old-agent", "Old", true)
	newRec := projectRecord(projectNew, "new-agent", "New", true)
	oldSnap := inventory.Snapshot{Records: []inventory.Record{oldRec}, Groups: []inventory.Group{{Project: projectOld, Records: []inventory.Record{oldRec}}}}
	newSnap := inventory.Snapshot{Records: []inventory.Record{newRec}, Groups: []inventory.Group{{Project: projectNew, Records: []inventory.Record{newRec}}}}

	m := NewProjectsModelWithActivation("", filepath.Join(projectNew, ".lingtai"), ProjectsContext{}, 2)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = m.Update(projectsInventoryMsg{activationID: 2, requestSeq: m.requestSeq, snapshot: newSnap})
	m, _ = m.Update(projectsInventoryMsg{activationID: 1, requestSeq: m.requestSeq, snapshot: oldSnap})

	if row, ok := m.selectedAgentRow(); !ok || row.record.AgentName != "New" {
		t.Fatalf("old activation overwrote reopened model: cursor row=%+v ok=%v", row, ok)
	}
}

func TestAgoraProjectsLoadUsesActivationScope(t *testing.T) {
	m := NewAgoraProjectsModelWithActivation("", filepath.Join(t.TempDir(), ".lingtai"), 2)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	current := []projectEntry{{Path: "/current", Name: "current"}}
	old := []projectEntry{{Path: "/old", Name: "old"}}

	m, _ = m.Update(projectsLoadMsg{activationID: 2, requestSeq: m.requestSeq, projects: current})
	m, _ = m.Update(projectsLoadMsg{activationID: 1, requestSeq: m.requestSeq, projects: old})

	if len(m.projects) != 1 || m.projects[0].Name != "current" {
		t.Fatalf("old Agora activation overwrote projects: %+v", m.projects)
	}
}

func TestProjectsModelValidationRejectsStaleTargets(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	rec := projectRecord(project, "agent", "Agent", true)
	initial := inventory.Snapshot{Records: []inventory.Record{rec}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{rec}}}}
	cases := []struct {
		name  string
		fresh inventory.Snapshot
		want  string
	}{
		{
			name:  "process stopped",
			fresh: inventory.Snapshot{},
			want:  "Target stopped or changed",
		},
		{
			name: "manifest unreadable",
			fresh: func() inventory.Snapshot {
				bad := rec
				bad.Enterable = false
				bad.EnterReason = inventory.EnterReasonManifestUnreadable
				bad.EnterDetail = "parse manifest: broken"
				return inventory.Snapshot{Records: []inventory.Record{bad}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{bad}}}}
			}(),
			want: "Manifest unreadable",
		},
		{
			name: "phantom project",
			fresh: func() inventory.Snapshot {
				phantom := rec
				phantom.Enterable = false
				phantom.EnterReason = inventory.EnterReasonPhantomProject
				phantom.Phantom = true
				return inventory.Snapshot{Records: []inventory.Record{phantom}, Groups: []inventory.Group{{Project: project, Phantom: true, Records: []inventory.Record{phantom}}}}
			}(),
			want: "Project .lingtai directory is missing",
		},
		{
			name: "pid mismatch",
			fresh: func() inventory.Snapshot {
				otherPID := rec
				otherPID.PID = rec.PID + 1
				return inventory.Snapshot{Records: []inventory.Record{otherPID}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{otherPID}}}}
			}(),
			want: "Target stopped or changed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withProjectsScan(t, func(inventory.Options) (inventory.Snapshot, error) {
				return tc.fresh, nil
			})
			m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
			m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
			m, _ = m.Update(projectsInventoryForModel(m, initial))

			m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m, cmd = m.Update(runCmd(cmd))
			if cmd != nil {
				t.Fatalf("stale target emitted selection command")
			}
			if !strings.Contains(m.status, tc.want) {
				t.Fatalf("status = %q, want %q", m.status, tc.want)
			}
		})
	}
}

func TestProjectsModelValidationMatchesCleanedAgentPath(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	clean := projectRecord(project, "agent", "Agent", true)
	dirty := clean
	dirty.AgentDir = filepath.Join(project, ".lingtai", "nested", "..", "agent")
	initial := inventory.Snapshot{Records: []inventory.Record{dirty}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{dirty}}}}
	fresh := inventory.Snapshot{Records: []inventory.Record{clean}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{clean}}}}
	withProjectsScan(t, func(inventory.Options) (inventory.Snapshot, error) {
		return fresh, nil
	})

	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = m.Update(projectsInventoryForModel(m, initial))
	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, cmd = m.Update(runCmd(cmd))
	msg, ok := runCmd(cmd).(ProjectsAgentSelectedMsg)
	if !ok {
		t.Fatalf("cleaned identity did not validate; cmd produced %T status=%q", runCmd(cmd), m.status)
	}
	if msg.Record.AgentDir != clean.AgentDir {
		t.Fatalf("selected agent dir = %q, want clean %q", msg.Record.AgentDir, clean.AgentDir)
	}
}

func TestProjectsModelKeepsCursorVisible(t *testing.T) {
	project := t.TempDir()
	var records []inventory.Record
	for i := 0; i < 18; i++ {
		agent := fmt.Sprintf("agent-%02d", i)
		records = append(records, projectRecord(project, agent, agent, true))
	}
	snap := inventory.Snapshot{Records: records, Groups: []inventory.Group{{Project: project, Records: records}}}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	for i := 0; i < 12; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		assertProjectsCursorVisible(t, m)
		if !m.rowSelectable(m.cursor) {
			t.Fatalf("cursor landed on non-selectable row %d", m.cursor)
		}
	}
	if m.viewport.YOffset() == 0 {
		t.Fatal("expected viewport to scroll after moving below visible rows")
	}
	for i := 0; i < 8; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		assertProjectsCursorVisible(t, m)
		if !m.rowSelectable(m.cursor) {
			t.Fatalf("cursor landed on non-selectable row %d", m.cursor)
		}
	}

	shrunk := inventory.Snapshot{Records: records[:1], Groups: []inventory.Group{{Project: project, Records: records[:1]}}}
	m, _ = m.Update(projectsInventoryForModel(m, shrunk))
	assertProjectsCursorVisible(t, m)
	if m.cursor != 1 {
		t.Fatalf("cursor after shrink = %d, want first agent row 1", m.cursor)
	}
}

func assertProjectsCursorVisible(t *testing.T, m ProjectsModel) {
	t.Helper()
	line, ok := m.selectedRenderedLine()
	if !ok {
		t.Fatal("no selected rendered line")
	}
	offset := m.viewport.YOffset()
	height := m.viewport.Height()
	if line < offset || line >= offset+height {
		t.Fatalf("cursor line %d outside viewport [%d,%d)", line, offset, offset+height)
	}
}
