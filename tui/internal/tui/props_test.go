package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/charmbracelet/x/ansi"
)

func TestResolveKanbanLLMConfig(t *testing.T) {
	llm := func(fields map[string]any) map[string]any { return map[string]any{"llm": fields} }
	tests := []struct {
		name              string
		agentRaw, initRaw map[string]any
		want              kanbanLLMConfig
	}{
		{
			name: "materialized values win field by field",
			agentRaw: llm(map[string]any{"model": "gpt-5.6-sol", "provider": "codex", "base_url": "https://chatgpt.com/backend-api/codex",
				"service_tier": "default", "thinking": "xhigh", "wire_api": "responses", "context_limit": float64(250000)}),
			initRaw: llm(map[string]any{"model": "stale-model", "provider": "stale-provider", "base_url": "https://stale.example/v1",
				"service_tier": "flex", "thinking": "low", "wire_api": "openai", "streaming": true, "context_limit": float64(500000)}),
			want: kanbanLLMConfig{Model: "gpt-5.6-sol", Provider: "codex", BaseURL: "https://chatgpt.com/backend-api/codex", Endpoint: "@chatgpt.com",
				ServiceTier: "default", Thinking: "xhigh", WireAPI: "responses", Streaming: "true", ContextLimit: "250000"},
		},
		{
			name:    "explicit init fallback",
			initRaw: llm(map[string]any{"model": "legacy-model", "provider": "legacy-provider", "base_url": "https://legacy.example/v1", "service_tier": "priority", "thinking": "medium", "wire_api": "openai", "context_limit": float64(500000)}),
			want:    kanbanLLMConfig{Model: "legacy-model", Provider: "legacy-provider", BaseURL: "https://legacy.example/v1", Endpoint: "@legacy.example", ServiceTier: "priority", Thinking: "medium", WireAPI: "openai", ContextLimit: "500000"},
		},
		{name: "missing fields stay absent", initRaw: llm(map[string]any{"model": "only-model"}), want: kanbanLLMConfig{Model: "only-model"}},
		{
			name:     "malformed published fields omit instead of reviving stale values",
			agentRaw: llm(map[string]any{"service_tier": 42, "thinking": "bad\nvalue", "base_url": "not a URL", "wire_api": 42, "context_limit": "bad"}),
			initRaw:  llm(map[string]any{"model": "fallback-model", "service_tier": "priority", "thinking": "low", "base_url": "https://fallback.example/v1", "wire_api": "openai", "context_limit": float64(500000)}),
			want:     kanbanLLMConfig{Model: "fallback-model"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveKanbanLLMConfig(tc.agentRaw, tc.initRaw); got != tc.want {
				t.Fatalf("resolveKanbanLLMConfig() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestKanbanEndpointIsHostOnlyAndBounded(t *testing.T) {
	longHost := strings.Repeat("a", 100) + ".example.com"
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"host only", "https://ChatGPT.com:443/backend-api/codex?token=hidden", "@chatgpt.com"},
		{"malformed", "not a URL", ""},
		{"userinfo rejected", "https://user:secret@example.com/v1", ""},
		{"overlong input", "https://" + strings.Repeat("a", kanbanBaseURLLimit) + ".example", ""},
		{"long host bounded", "https://" + longHost + "/v1", "@" + strings.Repeat("a", kanbanCompactEndpointLimit-1) + "…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseURL, got := parseKanbanBaseURL(tc.raw)
			if got != tc.want {
				t.Fatalf("compact endpoint = %q, want %q", got, tc.want)
			}
			if tc.want == "" && baseURL != "" {
				t.Fatalf("invalid URL retained detail value %q", baseURL)
			}
			if len([]rune(got)) > kanbanCompactEndpointLimit+1 {
				t.Fatalf("compact endpoint leaked an unbounded host (%d runes): %q", len([]rune(got)), got)
			}
			if strings.Contains(got, "/") || strings.Contains(got, "?") || strings.Contains(got, "secret") {
				t.Fatalf("compact endpoint exposed non-host material: %q", got)
			}
		})
	}
}

func requireContains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func requireNotContains(t *testing.T, got string, unwanted ...string) {
	t.Helper()
	for _, value := range unwanted {
		if strings.Contains(got, value) {
			t.Fatalf("output unexpectedly contains %q:\n%s", value, got)
		}
	}
}

func writeKanbanDispatchLedger(t *testing.T, agentDir string, runIDs ...string) {
	t.Helper()
	daemonsDir := filepath.Join(agentDir, "daemons")
	if err := os.MkdirAll(daemonsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 0, len(runIDs))
	for i, runID := range runIDs {
		lines = append(lines, fmt.Sprintf(`{"schema":"lingtai.daemon_dispatch/v1","sequence":%d,"run_id":%q,"created_at":"2026-08-26T05:32:52Z"}`, i+1, runID))
	}
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(daemonsDir, ".dispatch-ledger.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveKanbanPresetInfoUsesConfiguredRefsWithoutHealthClaims(t *testing.T) {
	configured := map[string]any{"preset": map[string]any{
		"active":  "saved/configured.json",
		"default": "templates/default.json",
		"allowed": []any{"saved/configured.json", "missing/on-purpose.json"},
	}}
	info := resolveKanbanPresetInfo(configured)
	if info.ActiveRef != "saved/configured.json" || info.DefaultRef != "templates/default.json" || len(info.Allowed) != 2 {
		t.Fatalf("configured preset refs were not presented verbatim: %+v", info)
	}
	m := PropsModel{selectedPresetInfo: info}
	view := ansi.Strip(m.renderDetail())
	requireContains(t, view, "configured", "default", i18n.T("props.preset_refs_not_checked"))
	requireNotContains(t, view, "✓", "✗", "healthy", "broken")
}

func TestPropsInitAndAgentSelectionLoadSnapshotAsynchronously(t *testing.T) {
	baseDir := t.TempDir()
	oldDir := filepath.Join(baseDir, "old")
	newDir := filepath.Join(baseDir, "new")
	for _, dir := range []string{oldDir, newDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	requireSnapshot := func(cmd tea.Cmd, selectedDir string) {
		t.Helper()
		if cmd == nil {
			t.Fatal("expected a Bubble Tea snapshot command")
		}
		msg, ok := cmd().(propsLoadMsg)
		if !ok {
			t.Fatal("command did not return propsLoadMsg")
		}
		if msg.selectedDir != selectedDir {
			t.Fatalf("snapshot selectedDir = %q, want %q", msg.selectedDir, selectedDir)
		}
	}
	m := NewPropsModel(baseDir, oldDir, t.TempDir())
	requireSnapshot(m.Init(), oldDir)

	m.selectedRaw = map[string]any{"agent_name": "old cached agent"}
	m.agentNodes = []fs.AgentNode{
		{AgentName: "old", WorkingDir: oldDir},
		{AgentName: "new", WorkingDir: newDir},
	}
	m.pickerOpen = true
	m.pickerIdx = 1
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if updated.selectedDir != newDir {
		t.Fatalf("selection stayed on %q, want %q", updated.selectedDir, newDir)
	}
	if len(updated.selectedRaw) != 0 {
		t.Fatalf("selection synchronously loaded or retained mislabeled heavy facts: %#v", updated.selectedRaw)
	}
	requireSnapshot(cmd, newDir)
}

func TestPropsIgnoresStaleSnapshotAfterSelection(t *testing.T) {
	newDir := filepath.Join(t.TempDir(), "new")
	m := PropsModel{
		selectedDir:    newDir,
		selectedRaw:    map[string]any{"agent_name": "new cached agent"},
		selectedTokens: fs.TokenTotals{Input: 44},
	}
	staleStatus := fs.AgentStatus{}
	staleStatus.Tokens.APICalls = 99
	after, _ := m.Update(propsLoadMsg{
		selectedDir:    filepath.Join(t.TempDir(), "old"),
		selectedStatus: staleStatus,
		agentNodes:     []fs.AgentNode{{AgentName: "stale"}},
		selectedRaw:    map[string]any{"agent_name": "stale agent"},
		selectedTokens: fs.TokenTotals{Input: 999},
	})
	if after.selectedStatus.Tokens.APICalls != 0 || len(after.agentNodes) != 0 ||
		after.selectedRaw["agent_name"] != "new cached agent" || after.selectedTokens.Input != 44 {
		t.Fatalf("accepted stale snapshot: %+v", after)
	}
}

func TestPropsRejectsOutOfOrderSameAgentCtrlRCompletion(t *testing.T) {
	dir := t.TempDir()
	m := NewPropsModel(dir, dir, t.TempDir())
	olderCmd := m.Init()
	older := olderCmd().(propsLoadMsg)

	updated, newerCmd := m.Update(ctrlR())
	newer := newerCmd().(propsLoadMsg)
	if newer.generation <= older.generation {
		t.Fatalf("request generations did not advance: older=%d newer=%d", older.generation, newer.generation)
	}
	newer.selectedRaw = map[string]any{"agent_name": "newer"}
	updated, _ = updated.Update(newer)
	older.selectedRaw = map[string]any{"agent_name": "older"}
	after, _ := updated.Update(older)
	if got := after.selectedRaw["agent_name"]; got != "newer" {
		t.Fatalf("older same-agent completion overwrote Ctrl+R result: %v", got)
	}
}

func TestPropsRejectsStaleACompletionAfterAtoBtoA(t *testing.T) {
	baseDir := t.TempDir()
	aDir := filepath.Join(baseDir, "a")
	bDir := filepath.Join(baseDir, "b")
	for _, dir := range []string{aDir, bDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	nodes := []fs.AgentNode{{AgentName: "a", WorkingDir: aDir}, {AgentName: "b", WorkingDir: bDir}}
	m := NewPropsModel(baseDir, aDir, t.TempDir())
	staleACmd := m.Init()
	staleA := staleACmd().(propsLoadMsg)

	m.agentNodes = nodes
	m.pickerOpen = true
	m.pickerIdx = 1
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = m.Update(propsLoadMsg{selectedDir: bDir, generation: m.requestGeneration, agentNodes: nodes})
	m.pickerOpen = true
	m.pickerIdx = 0
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	latestGeneration := m.requestGeneration
	m, _ = m.Update(propsLoadMsg{
		selectedDir: aDir,
		generation:  latestGeneration,
		selectedRaw: map[string]any{"agent_name": "fresh-a"},
		agentNodes:  nodes,
	})
	staleA.selectedRaw = map[string]any{"agent_name": "stale-a"}
	after, _ := m.Update(staleA)
	if got := after.selectedRaw["agent_name"]; got != "fresh-a" {
		t.Fatalf("stale first A completion overwrote A→B→A result: %v", got)
	}
}

func TestPropsNetworkTruncationRendersSubsetNotWholeNetworkClaims(t *testing.T) {
	m := PropsModel{network: fs.Network{
		AgentDirectoryTruncated: true,
		Nodes: []fs.AgentNode{
			{IsHuman: true, State: "ACTIVE"},
			{State: "IDLE"},
		},
		Stats:    fs.NetworkStats{Active: 1, Idle: 1},
		Activity: fs.NetworkActivity{Status: fs.NetworkStatusActive},
	}}
	out := ansi.Strip(m.renderRight(100))
	requireContains(t, out, i18n.T("props.network_bounded_subset")+": 2", i18n.T("props.network_subset_notice"), i18n.T("props.network_subset_states"))
	requireNotContains(t, out, i18n.T("props.network_total")+":", networkActivityLabel()+":")
}

func TestPropsLoadDataSkipsMailEdges(t *testing.T) {
	base := t.TempDir()
	agentDir := filepath.Join(base, "alice")
	messageDir := filepath.Join(agentDir, "mailbox", "inbox", "msg-1")
	if err := os.MkdirAll(messageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"agent_id":"id-alice","agent_name":"alice","address":"alice","state":"IDLE","admin":{"karma":false}}`)
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	message, err := json.Marshal(fs.MailMessage{
		ID:         "msg-1",
		From:       "alice",
		To:         "alice",
		ReceivedAt: "2026-07-30T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(messageDir, "message.json"), message, 0o644); err != nil {
		t.Fatal(err)
	}

	msg, ok := (PropsModel{baseDir: base, selectedDir: agentDir, orchDir: agentDir}).loadSnapshot().(propsLoadMsg)
	if !ok {
		t.Fatal("loadSnapshot returned unexpected message type")
	}
	if len(msg.network.MailEdges) != 0 || msg.network.Stats.TotalMails != 0 {
		t.Fatal("Kanban bounded network snapshot must skip mailbox bodies")
	}
	view := (PropsModel{network: msg.network}).renderRight(80)
	if strings.Contains(view, "Total: 0") {
		t.Fatalf("Props rendered omitted mail history as factual zero:\n%s", view)
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
	if err := os.WriteFile(filepath.Join(logsDir, "token_ledger.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := PropsModel{selectedDir: dir}
	m.applySnapshot(m.loadSnapshot().(propsLoadMsg))
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
		[]byte(`{"handle":"em-1","state":"running","backend":"claude-p"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "token_ledger.jsonl"),
		[]byte(`{"ts":"2026-06-13T03:00:05Z","input":9,"output":2}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeKanbanDispatchLedger(t, dir, "em-1-x")

	m := PropsModel{selectedDir: dir}
	m.applySnapshot(m.loadSnapshot().(propsLoadMsg))
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
	if got := m.detailDaemonRecent[0].Backend; got != "claude-p" {
		t.Fatalf("daemon backend = %q, want claude-p", got)
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

var benchmarkPropsRenderedBody string

// TestKanbanGuardedTimingProbe is a read-only acceptance probe for an actual
// agent directory. It is skipped unless both task-specific environment
// variables are supplied, so ordinary tests never depend on developer state.
func TestKanbanGuardedTimingProbe(t *testing.T) {
	baseDir := os.Getenv("LINGTAI_KANBAN_PROBE_BASE_DIR")
	agentDir := os.Getenv("LINGTAI_KANBAN_PROBE_AGENT_DIR")
	if baseDir == "" || agentDir == "" {
		t.Skip("set LINGTAI_KANBAN_PROBE_BASE_DIR and LINGTAI_KANBAN_PROBE_AGENT_DIR")
	}
	m := NewPropsModel(baseDir, agentDir, "")
	m.width = 160
	m.height = 50

	// Prime executable/page-cache state; the reported load is explicitly warm,
	// never represented as an OS-cold timing.
	_ = m.loadSnapshot()
	loadStarted := time.Now()
	snapshot, ok := m.loadSnapshot().(propsLoadMsg)
	if !ok {
		t.Fatal("loadSnapshot returned an unexpected message")
	}
	loadElapsed := time.Since(loadStarted)
	m.applySnapshot(snapshot)
	for i := 0; i < 10; i++ {
		benchmarkPropsRenderedBody = m.renderBody()
	}
	samples := make([]time.Duration, 101)
	for i := range samples {
		started := time.Now()
		benchmarkPropsRenderedBody = m.renderBody()
		samples[i] = time.Since(started)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("KANBAN_PROBE warm_render_median=%s warm_manual_snapshot_load=%s sources=%d", samples[len(samples)/2], loadElapsed, len(snapshot.readStats))
	for i, read := range snapshot.readStats {
		t.Logf("KANBAN_READ index=%d path=%q file_bytes=%d bytes_read=%d complete_lines=%d valid_records=%d dir_entries=%d line_limit=%d byte_limit=%d byte_limited=%t partial_ignored=%t status=%s",
			i, read.Path, read.FileBytes, read.BytesRead, read.CompleteLines, read.ValidRecords, read.DirectoryEntriesRead,
			read.LineLimit, read.ByteLimit, read.ByteLimited, read.TrailingPartialIgnored, read.Status)
	}
	t.Logf("KANBAN_RUN_IDS count=%d ids=%q", len(snapshot.detailDaemonRunIDs), snapshot.detailDaemonRunIDs)
}
