package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/charmbracelet/x/ansi"
)

func TestKanbanTimestampRendersLocalOffset(t *testing.T) {
	origLocal := time.Local
	t.Cleanup(func() { time.Local = origLocal })
	time.Local = time.FixedZone("test", -7*60*60)

	got := formatKanbanTimestamp("2026-06-13T03:00:00Z")
	want := "2026-06-12 20:00 U-7:00"
	if got != want {
		t.Fatalf("formatKanbanTimestamp() = %q, want %q", got, want)
	}
}

func TestKanbanTimestampKeepsInvalidLegacyCompact(t *testing.T) {
	got := formatKanbanTimestamp("2026-06-13T03:00:00-without-zone")
	want := "2026-06-13T03:00"
	if got != want {
		t.Fatalf("formatKanbanTimestamp() = %q, want legacy compact %q", got, want)
	}
}

func TestPropsRenderLeftShowsStartedAtLocalOffset(t *testing.T) {
	origLocal := time.Local
	t.Cleanup(func() { time.Local = origLocal })
	time.Local = time.FixedZone("test", -7*60*60)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), []byte(`{"agent_name":"mimo","started_at":"2026-06-13T03:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	m := PropsModel{selectedDir: dir}
	left := ansi.Strip(m.renderLeft(80))
	if !strings.Contains(left, "2026-06-12 20:00 U-7:00") {
		t.Fatalf("renderLeft should render started_at in local time with offset:\n%s", left)
	}
	if strings.Contains(left, "2026-06-13T03:00:00Z") {
		t.Fatalf("renderLeft should not show raw UTC started_at:\n%s", left)
	}
}

func TestPropsRenderRightShowsNetworkCreatedLocalOffset(t *testing.T) {
	origLocal := time.Local
	t.Cleanup(func() { time.Local = origLocal })
	time.Local = time.FixedZone("test", -7*60*60)

	m := PropsModel{adminStart: "2026-06-13T03:00:00Z"}
	right := ansi.Strip(m.renderRight(80))
	if !strings.Contains(right, "2026-06-12 20:00 U-7:00") {
		t.Fatalf("renderRight should render network_created in local time with offset:\n%s", right)
	}
	if strings.Contains(right, "2026-06-13T03:00:00Z") {
		t.Fatalf("renderRight should not show raw UTC timestamp:\n%s", right)
	}
}

func TestPropsRecentLanesShowLocalOffsetTimestamps(t *testing.T) {
	origLocal := time.Local
	t.Cleanup(func() { time.Local = origLocal })
	time.Local = time.FixedZone("test", -7*60*60)

	m := PropsModel{
		width: 140,
		detailRecent: []fs.LedgerEntry{
			{TS: "2026-06-13T03:00:00Z", Input: 5, Output: 1, Cached: 2, Model: "glm-4.6", Endpoint: "https://z.ai"},
		},
		detailDaemonRecent: []fs.DaemonLedgerEntry{
			{LedgerEntry: fs.LedgerEntry{TS: "2026-06-13T03:00:05Z", Input: 9, Cached: 3}, Handle: "em-1", State: "running"},
		},
	}
	out := ansi.Strip(strings.Join(m.renderRecentCallLanes(), "\n"))
	if strings.Count(out, "2026-06-12 20:00 U-7:00") != 2 {
		t.Fatalf("recent lanes should render both main and daemon timestamps with local offset:\n%s", out)
	}
	if strings.Contains(out, "2026-06-13T03:00") {
		t.Fatalf("recent lanes should not show raw/trimmed UTC timestamps:\n%s", out)
	}
}

func TestPropsRenderRightShowsRunningDaemons(t *testing.T) {
	m := PropsModel{
		network: fs.Network{
			Activity: fs.NetworkActivity{
				Status:         fs.NetworkStatusDaemonActive,
				RunningDaemons: 2,
			},
		},
	}

	right := ansi.Strip(m.renderRight(80))
	if !strings.Contains(right, "Daemons: 2 running") {
		t.Fatalf("renderRight missing running daemon count:\n%s", right)
	}
}

func TestPropsRenderDetailShowsDaemonCounts(t *testing.T) {
	m := PropsModel{
		detailDaemonCounts: fs.DaemonCounts{
			Running: 1,
			Total:   3,
		},
	}

	detail := ansi.Strip(m.renderDetail())
	if !strings.Contains(detail, "running: 1") {
		t.Fatalf("renderDetail missing running daemon count:\n%s", detail)
	}
	if !strings.Contains(detail, "total: 3") {
		t.Fatalf("renderDetail missing total daemon count:\n%s", detail)
	}
}

func TestPropsRenderDetailShowsContextStats(t *testing.T) {
	m := PropsModel{
		detailContextStats: fs.ContextStats{
			Entries:           5,
			SystemMessages:    1,
			AssistantMessages: 2,
			UserMessages:      2,
			TextInputs:        1,
			TextOutputs:       1,
			ToolCalls:         2,
			ToolResults:       2,
			ToolCounts: []fs.ContextToolCount{
				{Name: "bash", Calls: 2, Results: 1},
				{Name: "read", Calls: 1, Results: 1},
			},
		},
	}

	detail := ansi.Strip(m.renderDetail())
	for _, want := range []string{
		"Current context statistics",
		"entries:                  5",
		"messages:                 system:1  assistant:2  user:2",
		"text input / output:      1 / 1",
		"tool calls / results:     2 / 2",
		"tools in context:",
		"bash",
		"calls:2",
		"results:1",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("renderDetail missing %q:\n%s", want, detail)
		}
	}
}

func TestPropsLoadDetailKeepsLastHundredLedgerEntries(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 0; i < 120; i++ {
		lines = append(lines, fmt.Sprintf(`{"ts":"2026-06-13T03:00:00.%06dZ","input":%d,"output":1,"model":"m%d"}`, i, i+1, i))
	}
	if err := os.WriteFile(filepath.Join(logsDir, "token_ledger.jsonl"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	m := PropsModel{selectedDir: dir}
	m.loadDetail()
	if len(m.detailRecent) != detailRecentCalls {
		t.Fatalf("detailRecent len = %d, want %d", len(m.detailRecent), detailRecentCalls)
	}
	// Newest-first: m119 leads, and only the last 100 are retained (m20 oldest).
	if got := m.detailRecent[0].Model; got != "m119" {
		t.Fatalf("newest recent model = %q, want m119", got)
	}
	if got := m.detailRecent[len(m.detailRecent)-1].Model; got != "m20" {
		t.Fatalf("oldest retained recent model = %q, want m20", got)
	}
}

func TestPropsLoadDetailPopulatesDaemonRecent(t *testing.T) {
	dir := t.TempDir()
	// Main agent ledger.
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "token_ledger.jsonl"),
		[]byte(`{"ts":"2026-06-13T03:00:00Z","input":5,"output":1,"model":"glm-4.6"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// One daemon run with its own ledger + identity card.
	runDir := filepath.Join(dir, "daemons", "em-1-x", "logs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemons", "em-1-x", "daemon.json"),
		[]byte(`{"handle":"em-1","state":"running"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "token_ledger.jsonl"),
		[]byte(`{"ts":"2026-06-13T03:00:05Z","input":9,"output":2}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := PropsModel{selectedDir: dir}
	m.loadDetail()
	if len(m.detailRecent) != 1 {
		t.Fatalf("detailRecent len = %d, want 1", len(m.detailRecent))
	}
	if len(m.detailDaemonRecent) != 1 {
		t.Fatalf("detailDaemonRecent len = %d, want 1", len(m.detailDaemonRecent))
	}
	if got := m.detailDaemonRecent[0].Handle; got != "em-1" {
		t.Fatalf("daemon handle = %q, want em-1", got)
	}
	if got := m.detailDaemonRecent[0].State; got != "running" {
		t.Fatalf("daemon state = %q, want running", got)
	}
}

func TestPropsRecentLanesSingleColumnShowsMainThenDaemons(t *testing.T) {
	m := PropsModel{
		width: 140,
		detailRecent: []fs.LedgerEntry{
			{TS: "2026-06-13T03:00:00Z", Input: 5, Output: 1, Cached: 2, Model: "glm-4.6", Endpoint: "https://z.ai"},
		},
		detailDaemonRecent: []fs.DaemonLedgerEntry{
			{LedgerEntry: fs.LedgerEntry{TS: "2026-06-13T03:00:05Z", Input: 9, Cached: 3}, Handle: "em-1", State: "running"},
		},
	}
	outRaw := strings.Join(m.renderRecentCallLanes(), "\n")
	out := ansi.Strip(outRaw)
	mainIdx := strings.Index(out, i18n.T("props.detail_recent_main"))
	daemonIdx := strings.Index(out, i18n.T("props.detail_recent_daemons"))
	if mainIdx < 0 || daemonIdx < 0 {
		t.Fatalf("single-column ledger missing a title:\n%s", out)
	}
	if mainIdx > daemonIdx {
		t.Errorf("single-column ledger should show main before daemons:\n%s", out)
	}
	for _, want := range []string{"time", "provider", "model", "input", "output", "thinking", "cached", "miss", "cache%", "endpoint", "zhipu", "glm-4.6", "https://z.ai", "em-1", "running", "40.0%", "33.3%"} {
		if !strings.Contains(out, want) {
			t.Errorf("single-column ledger missing %q:\n%s", want, out)
		}
	}
	for _, notWant := range []string{"provider:", "model:", "endpoint:", "daemon:", "state:"} {
		if strings.Contains(out, notWant) {
			t.Errorf("single-column ledger should use table headers, found repeated label %q:\n%s", notWant, out)
		}
	}
	if strings.Contains(outRaw, "│") {
		t.Errorf("single-column ledger should not include a separator column:\n%s", out)
	}
}

func TestPropsRecentLanesDoNotTruncateDiagnosticFields(t *testing.T) {
	longModel := "very-long-model-name-that-should-remain-visible"
	longEndpoint := "https://example.com/a/very/long/endpoint/path/that/should/remain/visible"
	m := PropsModel{
		width: 60,
		detailRecent: []fs.LedgerEntry{
			{TS: "2026-06-13T03:00:00Z", Input: 5, Model: longModel, Endpoint: longEndpoint},
		},
		detailDaemonRecent: []fs.DaemonLedgerEntry{
			{LedgerEntry: fs.LedgerEntry{TS: "2026-06-13T03:00:05Z", Input: 9, Model: longModel, Endpoint: longEndpoint}, Handle: "em-1", RunID: "em-1-very-long-run-id", State: "done"},
		},
	}
	out := ansi.Strip(strings.Join(m.renderRecentCallLanes(), "\n"))
	for _, want := range []string{longModel, longEndpoint, "provider", "daemon", "em-1", "run", "em-1-very-long-run-id", "state", "done", "miss", "cache%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("single-column ledger missing untruncated field %q:\n%s", want, out)
		}
	}
	for _, notWant := range []string{"provider:", "daemon:", "run:", "state:"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("single-column ledger should use table headers, found repeated label %q:\n%s", notWant, out)
		}
	}
}

func TestPropsRecentLanesEmptyState(t *testing.T) {
	m := PropsModel{width: 140} // no ledger data
	out := ansi.Strip(strings.Join(m.renderRecentCallLanes(), "\n"))
	if !strings.Contains(out, i18n.T("props.detail_recent_empty")) {
		t.Errorf("missing main empty state:\n%s", out)
	}
	if !strings.Contains(out, i18n.T("props.detail_recent_daemons_empty")) {
		t.Errorf("missing daemon empty state:\n%s", out)
	}
}

func TestCacheMissComplementsCached(t *testing.T) {
	// miss = input - cached (the input tokens not served from cache).
	if got := cacheMiss(2, 5); got != 3 {
		t.Errorf("cacheMiss(2,5) = %d, want 3", got)
	}
	// Clamp at zero when cached >= input (or input missing on legacy rows).
	if got := cacheMiss(7, 5); got != 0 {
		t.Errorf("cacheMiss(7,5) = %d, want 0 (clamped)", got)
	}
	if got := cacheMiss(0, 0); got != 0 {
		t.Errorf("cacheMiss(0,0) = %d, want 0", got)
	}
}

func TestPropsRecentLanesShowCacheMissColumn(t *testing.T) {
	m := PropsModel{
		width: 160,
		detailRecent: []fs.LedgerEntry{
			// input 5, cached 2 → miss 3
			{TS: "2026-06-13T03:00:00Z", Input: 5, Output: 1, Cached: 2, Model: "glm-4.6", Endpoint: "https://z.ai"},
		},
		detailDaemonRecent: []fs.DaemonLedgerEntry{
			// input 9, cached 3 → miss 6
			{LedgerEntry: fs.LedgerEntry{TS: "2026-06-13T03:00:05Z", Input: 9, Cached: 3}, Handle: "em-1", State: "running"},
		},
	}
	mainRows := ansi.Strip(strings.Join(m.renderMainCallRows(), "\n"))
	if !strings.Contains(mainRows, "miss") {
		t.Errorf("main call header missing 'miss' column:\n%s", mainRows)
	}
	// Both the cached count (2) and its miss complement (3) must appear.
	if !strings.Contains(mainRows, "3") {
		t.Errorf("main call row missing cache miss value 3:\n%s", mainRows)
	}

	daemonRows := ansi.Strip(strings.Join(m.renderDaemonCallRows(), "\n"))
	if !strings.Contains(daemonRows, "miss") {
		t.Errorf("daemon call header missing 'miss' column:\n%s", daemonRows)
	}
	if !strings.Contains(daemonRows, "6") {
		t.Errorf("daemon call row missing cache miss value 6:\n%s", daemonRows)
	}
}

func TestPropsHeaderShowsCtrlDHint(t *testing.T) {
	// The non-detail header must prominently advertise ctrl+d for context detail.
	m := PropsModel{
		width:  80,
		height: 24,
		ready:  true,
	}
	m.viewport.SetWidth(80)
	m.viewport.SetHeight(20)

	view := ansi.Strip(m.View())

	// The callout line must mention the i18n key text.
	hint := i18n.T("props.ctrl_d_hint")
	if !strings.Contains(view, hint) {
		t.Fatalf("View() header missing ctrl+d callout hint %q:\n%s", hint, view)
	}

	// Also verify the standard footer line keeps ctrl+d with the renamed label.
	label := i18n.T("props.detail_open")
	if !strings.Contains(view, "ctrl+d "+label) {
		t.Fatalf("View() footer missing 'ctrl+d %s':\n%s", label, view)
	}

	// When detailOpen, the callout should NOT appear.
	m.detailOpen = true
	viewDetail := ansi.Strip(m.View())
	if strings.Contains(viewDetail, hint) {
		t.Fatalf("View() detail mode should NOT show callout:\n%s", viewDetail)
	}
}
