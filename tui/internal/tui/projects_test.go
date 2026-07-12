package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
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
	for _, want := range []string{"other", "Agent B", "[visiting]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	// The overview row carries live state/heartbeat but not operational
	// process details; PID and process uptime belong to Details only.
	leftRow := agentRowLine(view, "Agent B")
	if leftRow == "" {
		t.Fatalf("could not isolate Agent B overview row:\n%s", view)
	}
	for _, present := range []string{"AGENT", "IDLE"} {
		if !strings.Contains(leftRow, present) {
			t.Fatalf("overview row missing %q: %q", present, leftRow)
		}
	}
	for _, absent := range []string{"pid ", "100", "1m 0s"} {
		if strings.Contains(leftRow, absent) {
			t.Fatalf("overview row leaked operational detail %q: %q", absent, leftRow)
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

func adminSnapshotRecord(project string) inventory.Record {
	admin := projectRecord(project, "admin", "Admin One", true)
	admin.Address = "alpha-admin"
	admin.Role = inventory.RoleMain
	admin.IsOrchestrator = true
	admin.State = "ACTIVE"
	admin.Heartbeat = fs.HeartbeatStatus{Exists: true, Fresh: true, AgeSeconds: 2}
	admin.CreatedAt = "2026-07-01T10:30:00Z"
	admin.MoltCount = 7
	admin.MoltCountAvailable = true
	admin.ContextTotalTokens = 12345
	admin.ContextWindowSize = 250000
	admin.ContextUsagePct = 4.938
	admin.ContextAvailable = true
	admin.Uptime = "1h 2m"
	admin.PID = 4242
	admin.IMHandles = "telegram:@bot"
	admin.LockExists = true
	return admin
}

// TestProjectsOverviewRowCarriesLiveStateNotOperationalDetail asserts the left
// Kanban row answers who/role/state/heartbeat and, when authoritative, context
// pressure — but never operational details (PID, process uptime) which belong
// to Details.
func TestProjectsOverviewRowCarriesLiveStateNotOperationalDetail(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "network-alpha")
	admin := adminSnapshotRecord(project)
	snap := inventory.Snapshot{
		Records: []inventory.Record{admin},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{admin}}},
	}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	view := ansi.Strip(m.View())
	row := agentRowLine(view, "Admin One")
	if row == "" {
		t.Fatalf("could not isolate Admin One overview row:\n%s", view)
	}
	for _, want := range []string{"MAIN", "ACTIVE", "fresh", "5%"} {
		if !strings.Contains(row, want) {
			t.Fatalf("overview row missing %q: %q", want, row)
		}
	}
	for _, absent := range []string{"pid ", "4242", "1h 2m"} {
		if strings.Contains(row, absent) {
			t.Fatalf("overview row leaked operational detail %q: %q", absent, row)
		}
	}
}

// TestProjectsDetailsOwnOperationalDataWithoutStatusDuplication pins the Details
// pane contract: an identity header (no redundant Address: row), a Lifecycle
// section with created/lifetime/uptime/molt, a Network section, and a Runtime
// section carrying PID and exact context — with no generic Status block and no
// Role:/State:/Address: rows repeating what the overview already shows.
func TestProjectsDetailsOwnOperationalDataWithoutStatusDuplication(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "network-alpha")
	admin := adminSnapshotRecord(project)
	worker := projectRecord(project, "worker", "Worker", false)
	worker.EnterReason = inventory.EnterReasonNonAdmin
	outsider := projectRecord(filepath.Join(t.TempDir(), "other-network"), "other-admin", "Other Admin", true)
	outsider.IsOrchestrator = true
	snap := inventory.Snapshot{
		Records: []inventory.Record{admin, worker, outsider},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{admin, worker}}},
	}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	right := detailPane(ansi.Strip(m.View()))
	for _, want := range []string{
		"Admin One",   // identity header (display name)
		"alpha-admin", // identity line (address), not a labeled Address: row
		"Lifecycle",
		"Network",
		"Runtime",
		"Created at: " + formatKanbanTimestamp(admin.CreatedAt),
		"Process uptime: 1h 2m",
		"Molt count: 7",
		"Project: network-alpha",
		"Orchestrator: Admin One",
		"Live members: 2",
		"Live admins: 1",
		"PID: 4242",
		"12,345 / 250,000 (4.9%)",
		"telegram:@bot", // IM in Runtime
	} {
		if !strings.Contains(right, want) {
			t.Fatalf("details missing %q:\n%s", want, right)
		}
	}
	// No generic Status block, and no rows restating overview state.
	for _, unwanted := range []string{
		i18n.T("projects.section_status"),
		"Role:",
		"State:",
		"Address:",
	} {
		if strings.Contains(right, unwanted) {
			t.Fatalf("details duplicated overview data %q:\n%s", unwanted, right)
		}
	}
	// No raw project or agent-dir paths anywhere in the view.
	full := ansi.Strip(m.View())
	for _, leaked := range []string{project, admin.AgentDir, "Agent dir:"} {
		if strings.Contains(full, leaked) {
			t.Fatalf("view leaked raw path/detail %q:\n%s", leaked, full)
		}
	}
}

// TestProjectsContextMeterSuppressedWhenBarWouldBeAllEmpty pins the parent-review
// fix: a low nonzero context (4.9%) that a 12-cell bar quantizes to zero filled
// cells must NOT render an all-empty meter contradicting the exact numeric line;
// the exact "total / window (pct)" text still renders.
func TestProjectsContextMeterSuppressedWhenBarWouldBeAllEmpty(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "network-alpha")
	admin := adminSnapshotRecord(project) // ContextUsagePct = 4.938
	snap := inventory.Snapshot{Records: []inventory.Record{admin}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{admin}}}}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	right := detailPane(ansi.Strip(m.View()))
	if !strings.Contains(right, "12,345 / 250,000 (4.9%)") {
		t.Fatalf("exact context line must still render:\n%s", right)
	}
	if line := meterLine(right); line != "" {
		t.Fatalf("low-pct meter should be suppressed, got meter line %q:\n%s", line, right)
	}
}

// TestProjectsContextMeterRendersMixedForMeaningfulPct pins the other half: a
// meaningful percentage (50%) still renders a meter with both filled and empty
// cells, so suppression never hides real pressure.
func TestProjectsContextMeterRendersMixedForMeaningfulPct(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "network-alpha")
	rec := projectRecord(project, "agent", "Agent", true)
	rec.ContextTotalTokens = 125000
	rec.ContextWindowSize = 250000
	rec.ContextUsagePct = 50.0
	rec.ContextAvailable = true
	snap := inventory.Snapshot{Records: []inventory.Record{rec}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{rec}}}}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	right := detailPane(ansi.Strip(m.View()))
	if !strings.Contains(right, "125,000 / 250,000 (50.0%)") {
		t.Fatalf("exact context line must render:\n%s", right)
	}
	line := meterLine(right)
	if line == "" {
		t.Fatalf("meaningful pct should render a meter:\n%s", right)
	}
	if !strings.Contains(line, "█") || !strings.Contains(line, "░") {
		t.Fatalf("50%% meter must show filled and empty cells, got %q", line)
	}
}

// TestProjectsDetailsHeaderFallbackMatchesOverview pins the identity-consistency
// fix: an address-only record (no nickname/agent-name) uses the address as the
// display header — the same fallback the left overview row uses — and does not
// then repeat it as a separate identity line.
func TestProjectsDetailsHeaderFallbackMatchesOverview(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "network-alpha")
	rec := projectRecord(project, "agent-dir-name", "", true)
	rec.Nickname = ""
	rec.AgentName = ""
	rec.Address = "addr-only-1"
	snap := inventory.Snapshot{Records: []inventory.Record{rec}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{rec}}}}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	right := detailPane(ansi.Strip(m.View()))
	if !strings.Contains(right, "addr-only-1") {
		t.Fatalf("address-only record should surface the address as header:\n%s", right)
	}
	// The address must appear exactly once (as the header) — not echoed as a
	// separate identity line.
	if n := strings.Count(right, "addr-only-1"); n != 1 {
		t.Fatalf("address should render once (header only), got %d occurrences:\n%s", n, right)
	}
	// The agent-dir basename must never be the visible display fallback.
	if strings.Contains(right, "agent-dir-name") {
		t.Fatalf("agent-dir basename leaked as display fallback:\n%s", right)
	}
}

// TestProjectsDetailsShowWarningsImmediatelyAfterHeader keeps validation state
// (disabled/phantom/status) visible right below the identity header rather than
// buried under later sections.
func TestProjectsDetailsShowWarningsImmediatelyAfterHeader(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	rec := projectRecord(project, "worker", "Worker", false)
	rec.EnterReason = inventory.EnterReasonNonAdmin
	snap := inventory.Snapshot{Records: []inventory.Record{rec}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{rec}}}}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	right := detailPane(ansi.Strip(m.View()))
	warnIdx := strings.Index(right, i18n.T("projects.enter_reason_non_admin"))
	lifecycleIdx := strings.Index(right, i18n.T("projects.section_lifecycle"))
	if warnIdx < 0 {
		t.Fatalf("details missing disabled reason:\n%s", right)
	}
	if lifecycleIdx >= 0 && warnIdx > lifecycleIdx {
		t.Fatalf("warning must appear before Lifecycle section:\n%s", right)
	}
}

func TestProjectsRegistryDetailsRenderMissingValuesAsDash(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	rec := projectRecord(project, "agent", "Agent", true)
	rec.Uptime = ""
	snap := inventory.Snapshot{Records: []inventory.Record{rec}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{rec}}}}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
	m, _ = m.Update(projectsInventoryForModel(m, snap))
	right := detailPane(ansi.Strip(m.View()))
	for _, want := range []string{"Created at: —", "Process uptime: —", "Molt count: —", "Orchestrator: —"} {
		if !strings.Contains(right, want) {
			t.Fatalf("details missing unavailable value %q:\n%s", want, right)
		}
	}
	// Context is authoritative-only: absent status means no Context/usage line
	// and no meter — but the Runtime section (PID) still renders.
	if strings.Contains(right, "/ ") && strings.Contains(right, "%)") {
		t.Fatalf("context usage should be omitted without authoritative status data:\n%s", right)
	}
	if !strings.Contains(right, "PID:") {
		t.Fatalf("Runtime PID line should render even without context data:\n%s", right)
	}
}

func TestProjectsRegistryDetailSectionsRenderInEachLocale(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	project := t.TempDir()
	rec := projectRecord(project, "agent", "Agent", true)
	snap := inventory.Snapshot{Records: []inventory.Record{rec}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{rec}}}}
	for _, lang := range []string{"en", "zh", "wen"} {
		t.Run(lang, func(t *testing.T) {
			if err := i18n.SetLang(lang); err != nil {
				t.Fatal(err)
			}
			m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
			m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
			m, _ = m.Update(projectsInventoryForModel(m, snap))
			view := ansi.Strip(m.View())
			for _, key := range []string{"projects.section_lifecycle", "projects.section_network", "projects.section_runtime", "projects.created_at", "projects.live_members"} {
				want := i18n.TIn(lang, key)
				if want == key || !strings.Contains(view, want) {
					t.Fatalf("%s view missing localized %s=%q:\n%s", lang, key, want, view)
				}
			}
		})
	}
}

func TestProjectsRegistryNarrowWidthFallsBackToList(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	rec := projectRecord(project, "agent", "Agent", true)
	snap := inventory.Snapshot{Records: []inventory.Record{rec}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{rec}}}}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	m, _ = m.Update(projectsInventoryForModel(m, snap))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Agent") {
		t.Fatalf("narrow fallback lost selectable list:\n%s", view)
	}
	if strings.Contains(view, i18n.T("projects.section_lifecycle")) || strings.Contains(view, "│") {
		t.Fatalf("narrow fallback should omit details and separator:\n%s", view)
	}
}

// TestProjectsRegistryLayoutUsefulAtSupportedWidths guards that both the 100
// and 140 column widths render a two-pane layout whose Details include the
// curated Lifecycle/Network/Runtime sections in the first viewport.
func TestProjectsRegistryLayoutUsefulAtSupportedWidths(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "network-alpha")
	admin := adminSnapshotRecord(project)
	snap := inventory.Snapshot{Records: []inventory.Record{admin}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{admin}}}}
	for _, width := range []int{100, 140} {
		t.Run(fmt.Sprintf("w%d", width), func(t *testing.T) {
			m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{})
			m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 28})
			m, _ = m.Update(projectsInventoryForModel(m, snap))
			view := ansi.Strip(m.View())
			if !strings.Contains(view, "│") {
				t.Fatalf("width %d should render a two-pane separator:\n%s", width, view)
			}
			for _, key := range []string{"projects.section_lifecycle", "projects.section_network", "projects.section_runtime"} {
				if !strings.Contains(view, i18n.T(key)) {
					t.Fatalf("width %d details missing %s:\n%s", width, key, view)
				}
			}
			if !strings.Contains(view, "12,345 / 250,000") {
				t.Fatalf("width %d details missing exact context:\n%s", width, view)
			}
		})
	}
}

func TestAgentLifetimeUsesCreationTimeNotProcessUptime(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	created := now.Add(-(26*time.Hour + 30*time.Minute)).Format(time.RFC3339)
	if got, want := formatAgentLifetime(created, now), "1d 2h 30m"; got != want {
		t.Fatalf("formatAgentLifetime = %q, want %q", got, want)
	}
	if got := formatAgentLifetime("", now); got != "—" {
		t.Fatalf("missing creation time lifetime = %q, want dash", got)
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

// leftPane returns the left (overview) pane text joined across lines, keeping
// only the portion before the two-pane separator (or the whole line in the
// narrow list-only layout), so assertions never match content that lives in the
// Details column.
func leftPane(view string) string {
	var b strings.Builder
	for _, line := range strings.Split(view, "\n") {
		b.WriteString(leftOf(line))
		b.WriteString("\n")
	}
	return b.String()
}

// TestProjectsCurrentAgentBlockNamesAppContextNotCursor pins the core
// separation: the dedicated current-agent summary is keyed on the App context
// (FocusedAgentDir / CurrentAgentName), so moving the cursor onto a different
// agent never makes that agent read as the current one. The current agent's row
// keeps its own distinct marker; the cursor row keeps the ">" selection marker.
func TestProjectsCurrentAgentBlockNamesAppContextNotCursor(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "network-alpha")
	current := adminSnapshotRecord(project) // Admin One, ACTIVE, fresh heartbeat
	other := projectRecord(project, "worker", "Worker Two", true)
	other.State = "IDLE"
	snap := inventory.Snapshot{
		Records: []inventory.Record{current, other},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{current, other}}},
	}
	// App context: current agent is Admin One (NOT the row the cursor lands on).
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{
		FocusedAgentDir:  current.AgentDir,
		CurrentAgentName: "Admin One",
	})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	// Move the cursor onto the OTHER agent (Worker Two).
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if row, ok := m.selectedAgentRow(); !ok || row.record.AgentName != "Worker Two" {
		t.Fatalf("cursor should be on Worker Two, got %+v ok=%v", row, ok)
	}

	left := leftPane(ansi.Strip(m.View()))
	header := i18n.T("projects.current_agent")
	hi := strings.Index(left, header)
	if hi < 0 {
		t.Fatalf("left pane missing %q current-agent block:\n%s", header, left)
	}
	// The current-agent block identifies Admin One with its live state/heartbeat,
	// and does so above the running-agents list.
	block := left[hi:]
	listIdx := strings.Index(block, i18n.T("projects.running_agents"))
	if listIdx < 0 {
		t.Fatalf("running-agents section should follow the current-agent block:\n%s", left)
	}
	blockText := block[:listIdx]
	for _, want := range []string{"Admin One", "ACTIVE", i18n.T("projects.heartbeat_fresh")} {
		if !strings.Contains(blockText, want) {
			t.Fatalf("current-agent block missing %q:\n%s", want, blockText)
		}
	}
	// The block must NOT name the cursor's agent — cursor selection cannot
	// masquerade as current identity.
	if strings.Contains(blockText, "Worker Two") {
		t.Fatalf("current-agent block leaked cursor agent Worker Two:\n%s", blockText)
	}

	// Distinct markers: the current agent's row carries the current marker, the
	// cursor row carries ">". They must be different rows.
	currentRow := agentRowLine(ansi.Strip(m.View()), "Admin One")
	cursorRow := agentRowLine(ansi.Strip(m.View()), "Worker Two")
	if !strings.Contains(currentRow, projectsCurrentMarker) {
		t.Fatalf("current agent row missing current marker %q: %q", projectsCurrentMarker, currentRow)
	}
	if !strings.Contains(cursorRow, ">") {
		t.Fatalf("cursor row missing selection marker: %q", cursorRow)
	}
	if strings.Contains(cursorRow, projectsCurrentMarker) {
		t.Fatalf("cursor row must not carry the current marker: %q", cursorRow)
	}
}

// TestProjectsCurrentAgentBlockDegradesWhenNotProcessVisible pins honest
// degradation: when the App's current agent has no process-visible record, the
// block still names it (from context) and shows a localized unavailable status
// instead of inventing a lifecycle state.
func TestProjectsCurrentAgentBlockDegradesWhenNotProcessVisible(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "network-alpha")
	// Snapshot contains only some OTHER agent; the current agent is absent.
	other := projectRecord(project, "worker", "Worker Two", true)
	snap := inventory.Snapshot{
		Records: []inventory.Record{other},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{other}}},
	}
	missingDir := filepath.Join(project, ".lingtai", "ghost")
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{
		FocusedAgentDir:  missingDir,
		CurrentAgentName: "Ghost Admin",
	})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	left := leftPane(ansi.Strip(m.View()))
	hi := strings.Index(left, i18n.T("projects.current_agent"))
	if hi < 0 {
		t.Fatalf("left pane missing current-agent block:\n%s", left)
	}
	blockText := left[hi:]
	if listIdx := strings.Index(blockText, i18n.T("projects.running_agents")); listIdx >= 0 {
		blockText = blockText[:listIdx]
	}
	if !strings.Contains(blockText, "Ghost Admin") {
		t.Fatalf("unavailable current-agent block should still name the agent:\n%s", blockText)
	}
	if !strings.Contains(blockText, i18n.T("projects.current_agent_unavailable")) {
		t.Fatalf("missing record should show localized unavailable status:\n%s", blockText)
	}
	// No invented lifecycle state — the honest-status block never claims a live
	// state string for an agent it cannot see.
	for _, invented := range []string{"ACTIVE", "IDLE", "ASLEEP", "STUCK", "SUSPENDED"} {
		if strings.Contains(blockText, invented) {
			t.Fatalf("unavailable block invented lifecycle state %q:\n%s", invented, blockText)
		}
	}
}

// TestProjectsCurrentAgentBlockRendersInEachLocale keeps the current-agent block
// header and unavailable status localized across all three locales.
func TestProjectsCurrentAgentBlockRendersInEachLocale(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	project := filepath.Join(t.TempDir(), "network-alpha")
	other := projectRecord(project, "worker", "Worker", true)
	snap := inventory.Snapshot{Records: []inventory.Record{other}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{other}}}}
	for _, lang := range []string{"en", "zh", "wen"} {
		t.Run(lang, func(t *testing.T) {
			if err := i18n.SetLang(lang); err != nil {
				t.Fatal(err)
			}
			m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{
				FocusedAgentDir:  filepath.Join(project, ".lingtai", "ghost"),
				CurrentAgentName: "Ghost",
			})
			m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
			m, _ = m.Update(projectsInventoryForModel(m, snap))
			view := ansi.Strip(m.View())
			for _, key := range []string{"projects.current_agent", "projects.current_agent_unavailable"} {
				want := i18n.TIn(lang, key)
				if want == key || !strings.Contains(view, want) {
					t.Fatalf("%s view missing localized %s=%q:\n%s", lang, key, want, view)
				}
			}
		})
	}
}

// TestProjectsCurrentAgentBlockSurvivesNarrowWidth keeps the current-agent
// identity and status visible in the narrow list-only layout, distinguishable
// from the selected row.
func TestProjectsCurrentAgentBlockSurvivesNarrowWidth(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "network-alpha")
	current := adminSnapshotRecord(project)
	other := projectRecord(project, "worker", "Worker Two", true)
	snap := inventory.Snapshot{
		Records: []inventory.Record{current, other},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{current, other}}},
	}
	m := NewProjectsModel("", filepath.Join(project, ".lingtai"), ProjectsContext{
		FocusedAgentDir:  current.AgentDir,
		CurrentAgentName: "Admin One",
	})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	m, _ = m.Update(projectsInventoryForModel(m, snap))
	// Cursor on the other agent.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	view := ansi.Strip(m.View())
	if strings.Contains(view, "│") {
		t.Fatalf("width 70 should be list-only (no separator):\n%s", view)
	}
	if !strings.Contains(view, i18n.T("projects.current_agent")) {
		t.Fatalf("narrow layout dropped the current-agent block:\n%s", view)
	}
	if !strings.Contains(view, "Admin One") || !strings.Contains(view, "ACTIVE") {
		t.Fatalf("narrow current-agent block missing identity/state:\n%s", view)
	}
	// The current marker still distinguishes the current row from the ">" cursor.
	currentRow := agentRowLine(view, "Admin One")
	cursorRow := agentRowLine(view, "Worker Two")
	if !strings.Contains(currentRow, projectsCurrentMarker) {
		t.Fatalf("narrow current row missing current marker: %q", currentRow)
	}
	if strings.Contains(cursorRow, projectsCurrentMarker) || !strings.Contains(cursorRow, ">") {
		t.Fatalf("narrow cursor row markers wrong: %q", cursorRow)
	}
}

// bodyLineIndexOf returns the index of the single viewport-body line whose left
// (overview) column contains needle, or -1. It reads the model's actual rendered
// body — the same content selectedRenderedLine() indexes into — so it is an
// independent oracle for the row's true position, not a re-derivation of the
// registryListTopLines formula. Callers pass a needle that appears in exactly one
// list row (a non-current agent name, which the current-agent block never repeats).
func bodyLineIndexOf(t *testing.T, m ProjectsModel, needle string) int {
	t.Helper()
	found := -1
	for i, line := range strings.Split(ansi.Strip(m.renderBody()), "\n") {
		if strings.Contains(leftOf(line), needle) {
			if found >= 0 {
				t.Fatalf("needle %q appears on multiple body lines (%d and %d); pick a unique row", needle, found, i)
			}
			found = i
		}
	}
	return found
}

// TestProjectsSelectedRenderedLineMatchesActualRowWithBlock pins that
// selectedRenderedLine() reports the true body-line index of the selected list
// row once the current-agent block is prepended — the scroll-to-cursor invariant.
// It selects a NON-current agent (so its name is not duplicated in the block) and
// compares selectedRenderedLine() to the row's actual rendered position derived
// independently from renderBody(). It covers both a visible current-agent block
// (taller: header + name + state/heartbeat line) and an unavailable one (shorter:
// header + name + status), proving the offset tracks the real block height. This
// fails on a len(block)+1+projectsListTopLines off-by-one.
func TestProjectsSelectedRenderedLineMatchesActualRowWithBlock(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "network-alpha")
	admin := adminSnapshotRecord(project) // Admin One, process-visible
	other := projectRecord(project, "worker", "Worker Two", true)
	snap := inventory.Snapshot{
		Records: []inventory.Record{admin, other},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{admin, other}}},
	}

	cases := []struct {
		name    string
		ctx     ProjectsContext
		visible bool // whether the current-agent block resolves a process record
	}{
		{
			name:    "visible current-agent block",
			ctx:     ProjectsContext{FocusedAgentDir: admin.AgentDir, CurrentAgentName: "Admin One"},
			visible: true,
		},
		{
			name:    "unavailable current-agent block",
			ctx:     ProjectsContext{FocusedAgentDir: filepath.Join(project, ".lingtai", "ghost"), CurrentAgentName: "Ghost"},
			visible: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewProjectsModel("", filepath.Join(project, ".lingtai"), tc.ctx)
			m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 28})
			m, _ = m.Update(projectsInventoryForModel(m, snap))
			// Select "Worker Two": always non-current in both cases, so its name is
			// unique to its list row (never echoed by the current-agent block).
			m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			row, ok := m.selectedAgentRow()
			if !ok || row.record.AgentName != "Worker Two" {
				t.Fatalf("cursor should be on Worker Two, got %+v ok=%v", row, ok)
			}

			got, ok := m.selectedRenderedLine()
			if !ok {
				t.Fatalf("selectedRenderedLine reported no line")
			}
			want := bodyLineIndexOf(t, m, "Worker Two")
			if want < 0 {
				t.Fatalf("could not locate Worker Two row in rendered body:\n%s", ansi.Strip(m.renderBody()))
			}
			if got != want {
				t.Fatalf("selectedRenderedLine() = %d, actual rendered row line = %d (block visible=%v)", got, want, tc.visible)
			}
		})
	}
}

// leftOf returns the portion of a rendered line before the two-pane separator,
// or the whole line when there is no separator (narrow list-only layout).
func leftOf(line string) string {
	if i := strings.Index(line, "│"); i >= 0 {
		return line[:i]
	}
	return line
}

// agentRowLine returns the left-pane text of the first list row whose left side
// contains name — the overview row for that agent, isolated from the Details
// pane sharing the same terminal line. It skips the dedicated current-agent
// block (which also renders the current agent's name) by only searching lines at
// or below the running-agents section header, so per-row assertions target the
// list rows, not the summary block.
func agentRowLine(view, name string) string {
	header := i18n.T("projects.running_agents")
	inList := false
	for _, line := range strings.Split(view, "\n") {
		left := leftOf(line)
		if !inList {
			if strings.Contains(left, header) {
				inList = true
			}
			continue
		}
		if strings.Contains(left, name) {
			return left
		}
	}
	return ""
}

// detailPane returns the Details (right) pane text joined across lines, with the
// left overview column stripped so assertions never accidentally match content
// that lives in the left Kanban column.
func detailPane(view string) string {
	var b strings.Builder
	for _, line := range strings.Split(view, "\n") {
		if i := strings.Index(line, "│"); i >= 0 {
			b.WriteString(line[i+len("│"):])
			b.WriteString("\n")
		}
	}
	return b.String()
}

// meterLine returns the trimmed content of the first line containing a context
// meter cell (filled "█" or empty "░"), or "" when no meter line is present.
// Callers pass ANSI-stripped text so the rune check is not fooled by styling.
func meterLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.ContainsAny(line, "█░") {
			return strings.TrimSpace(line)
		}
	}
	return ""
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
