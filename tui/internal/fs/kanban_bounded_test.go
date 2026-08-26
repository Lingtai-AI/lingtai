package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeKanbanTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadKanbanTokenWindowUsesOnlyLastThousandCompleteLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token_ledger.jsonl")
	lines := make([]string, 0, KanbanReadLimit+2)
	for i := 1; i <= KanbanReadLimit+2; i++ {
		lines = append(lines, fmt.Sprintf(`{"ts":"2026-08-26T00:00:%02dZ","input":%d}`, i%60, i))
	}
	writeKanbanTestFile(t, path, strings.Join(lines, "\n")+"\n")

	window := ReadKanbanTokenWindow(path, 10)
	if window.Read.CompleteLines != KanbanReadLimit || window.Totals.APICalls != KanbanReadLimit {
		t.Fatalf("read lines/calls = %d/%d, want %d/%d", window.Read.CompleteLines, window.Totals.APICalls, KanbanReadLimit, KanbanReadLimit)
	}
	wantInput := int64((3 + (KanbanReadLimit + 2)) * KanbanReadLimit / 2)
	if window.Totals.Input != wantInput {
		t.Fatalf("bounded input total = %d, want %d (oldest two rows must be ignored)", window.Totals.Input, wantInput)
	}
	if len(window.Recent) != 10 || window.Recent[0].Input != KanbanReadLimit+2 {
		t.Fatalf("recent rows = %#v", window.Recent)
	}
}

func TestBoundedJSONLTailByteCeilingDropsGiantPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeKanbanTestFile(t, path, `{"type":"tool_call","ts":1}`+"\n"+strings.Repeat("x", int(KanbanJSONLByteLimit)+4096))

	lines, stat := readBoundedJSONLTail(path, KanbanReadLimit, KanbanJSONLByteLimit)
	if stat.BytesRead != KanbanJSONLByteLimit || !stat.ByteLimited || !stat.TrailingPartialIgnored {
		t.Fatalf("byte-bound stat = %+v", stat)
	}
	if len(lines) != 0 {
		t.Fatalf("giant unterminated suffix exposed older rows: %q", lines)
	}
	if got := KanbanReadState(stat); got != KanbanSourcePartial {
		t.Fatalf("giant EOF line state = %q, want partial (stat=%+v)", got, stat)
	}
}

func TestKanbanContextAndEventWindowsIgnoreOlderLines(t *testing.T) {
	agentDir := t.TempDir()
	history := make([]string, 0, KanbanReadLimit+1)
	history = append(history, `{"role":"system","content":"too old"}`)
	for i := 0; i < KanbanReadLimit; i++ {
		history = append(history, `{"role":"user","content":"recent"}`)
	}
	writeKanbanTestFile(t, filepath.Join(agentDir, "history", "chat_history.jsonl"), strings.Join(history, "\n")+"\n")
	stats, read := ReadKanbanContextStats(agentDir)
	if read.CompleteLines != KanbanReadLimit || stats.Entries != KanbanReadLimit || stats.SystemMessages != 0 || stats.UserMessages != KanbanReadLimit {
		t.Fatalf("bounded context = %+v read=%+v", stats, read)
	}

	events := make([]string, 0, KanbanReadLimit+1)
	events = append(events, `{"type":"psyche_molt","ts":1}`)
	for i := 0; i < KanbanReadLimit; i++ {
		events = append(events, fmt.Sprintf(`{"type":"tool_call","ts":%d}`, i+2))
	}
	writeKanbanTestFile(t, filepath.Join(agentDir, "logs", "events.jsonl"), strings.Join(events, "\n")+"\n")
	eventWindow := ReadKanbanEventWindow(agentDir, 100)
	if eventWindow.Read.CompleteLines != KanbanReadLimit || len(eventWindow.Rebuilds) != 0 || eventWindow.ToolCalls.Current != 0 {
		t.Fatalf("bounded events = %+v", eventWindow)
	}
}

func TestKanbanDaemonSnapshotUsesDispatchLedgerOnly(t *testing.T) {
	agentDir := t.TempDir()
	dispatches := make([]string, 0, KanbanReadLimit+1)
	for i := 0; i <= KanbanReadLimit; i++ {
		dispatches = append(dispatches, fmt.Sprintf(`{"schema":"lingtai.daemon_dispatch/v1","sequence":%d,"run_id":"run-%04d","created_at":"2026-08-26T05:32:52Z"}`, i+1, i))
	}
	writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", ".dispatch-ledger.jsonl"), strings.Join(dispatches, "\n")+"\n")
	writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", "run-0000", "daemon.json"), `{"handle":"too-old","state":"done"}`)
	writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", "run-0000", "logs", "token_ledger.jsonl"), `{"input":9000}`+"\n")
	writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", "run-1000", "daemon.json"), `{"handle":"latest","state":"done","backend":"lingtai"}`)
	writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", "run-1000", "logs", "token_ledger.jsonl"), `{"ts":"2026-08-26T05:32:53Z","input":7,"run_id":"run-1000"}`+"\n")
	writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", "not-dispatched", "daemon.json"), `{"handle":"rogue","state":"running"}`)
	writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", "not-dispatched", "logs", "token_ledger.jsonl"), `{"input":8000}`+"\n")

	var reads []BoundedReadStats
	snapshot := ReadKanbanDaemonDetailSnapshot(agentDir, 100, &reads)
	if snapshot.WindowState != KanbanSourcePartial || len(snapshot.RunIDs) != KanbanReadLimit || snapshot.Counts.Total != KanbanReadLimit {
		t.Fatalf("dispatch window = state %q ids=%d counts=%+v", snapshot.WindowState, len(snapshot.RunIDs), snapshot.Counts)
	}
	if snapshot.RunIDs[0] != "run-1000" || snapshot.RunIDs[len(snapshot.RunIDs)-1] != "run-0001" {
		t.Fatalf("opened run IDs = first %q last %q", snapshot.RunIDs[0], snapshot.RunIDs[len(snapshot.RunIDs)-1])
	}
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Input != 7 || snapshot.Recent[0].Handle != "latest" {
		t.Fatalf("daemon rows included old/unlisted directories: %+v", snapshot.Recent)
	}
}

func TestKanbanDaemonSnapshotMissingOrMalformedLedgerNeverListsDirectory(t *testing.T) {
	for _, tc := range []struct {
		name, ledger, wantState string
	}{
		{name: "missing", wantState: KanbanSourceUnavailable},
		{name: "malformed", ledger: "not-json\n", wantState: KanbanSourceMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agentDir := t.TempDir()
			writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", "rogue", "daemon.json"), `{"state":"running"}`)
			if tc.ledger != "" {
				writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", ".dispatch-ledger.jsonl"), tc.ledger)
			}
			snapshot := ReadKanbanDaemonDetailSnapshot(agentDir, 100, nil)
			if snapshot.WindowState != tc.wantState || snapshot.Counts.Total != 0 || len(snapshot.RunIDs) != 0 {
				t.Fatalf("snapshot = %+v, want state %q and no directory fallback", snapshot, tc.wantState)
			}
		})
	}
}

func TestKanbanNetworkOptionsCarryEveryBound(t *testing.T) {
	opts := KanbanNetworkOptions(nil)
	if !opts.SkipMailEdges || opts.AgentEntryLimit != KanbanReadLimit ||
		opts.TopologyRecordLimit != KanbanReadLimit || opts.ContactRecordLimit != KanbanReadLimit ||
		opts.DaemonDispatchLimit != KanbanReadLimit || opts.FixedFileByteLimit != KanbanFixedFileByteLimit ||
		opts.JSONLByteLimit != KanbanJSONLByteLimit {
		t.Fatalf("incomplete Kanban network profile: %+v", opts)
	}
}

func TestBuildNetworkWithOptionsHonorsEveryKanbanLimit(t *testing.T) {
	baseDir := t.TempDir()
	agentDir := filepath.Join(baseDir, "a")
	writeKanbanTestFile(t, filepath.Join(agentDir, ".agent.json"), `{"agent_id":"a","agent_name":"a","address":"a","state":"IDLE","admin":{}}`)
	writeKanbanTestFile(t, filepath.Join(agentDir, ".agent.heartbeat"), fmt.Sprintf("%f", float64(time.Now().UnixNano())/1e9))
	writeKanbanTestFile(t, filepath.Join(baseDir, "b", ".agent.json"), `{"agent_id":"b","agent_name":"b","address":"b","state":"IDLE","admin":{}}`)

	ledger := []string{
		`{"event":"avatar","name":"one","working_dir":"child-1"}`,
		`{"event":"avatar","name":"two","working_dir":"child-2"}`,
		`{"event":"avatar","name":"three","working_dir":"child-3"}`,
	}
	for _, child := range []string{"child-1", "child-2", "child-3"} {
		if err := os.MkdirAll(filepath.Join(baseDir, child), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeKanbanTestFile(t, filepath.Join(agentDir, "delegates", "ledger.jsonl"), strings.Join(ledger, "\n")+"\n")
	writeKanbanTestFile(t, filepath.Join(agentDir, "mailbox", "contacts.json"), `[{"address":"one"},{"address":"two"},{"address":"three"}]`)
	dispatches := []string{
		`{"schema":"lingtai.daemon_dispatch/v1","sequence":1,"run_id":"run-1"}`,
		`{"schema":"lingtai.daemon_dispatch/v1","sequence":2,"run_id":"run-2"}`,
		`{"schema":"lingtai.daemon_dispatch/v1","sequence":3,"run_id":"run-3"}`,
	}
	writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", ".dispatch-ledger.jsonl"), strings.Join(dispatches, "\n")+"\n")
	for _, runID := range []string{"run-1", "run-2", "run-3"} {
		writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", runID, "daemon.json"), `{"state":"running"}`)
	}

	var reads []BoundedReadStats
	opts := KanbanNetworkOptions(&reads)
	opts.AgentEntryLimit = 5
	opts.TopologyRecordLimit = 2
	opts.ContactRecordLimit = 2
	opts.DaemonDispatchLimit = 2
	network, err := BuildNetworkWithOptions(baseDir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(network.Nodes) != 4 || len(network.AvatarEdges) != 2 || len(network.ContactEdges) != 2 || network.Activity.RunningDaemons != 2 {
		t.Fatalf("bounded network shape: nodes=%d avatars=%d contacts=%d daemons=%d", len(network.Nodes), len(network.AvatarEdges), len(network.ContactEdges), network.Activity.RunningDaemons)
	}
	if len(network.MailEdges) != 0 {
		t.Fatal("Kanban network profile read mailbox bodies")
	}
}

func TestReadDirEntriesBoundedDetectsTruncationWithOneExtraEntry(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i <= KanbanReadLimit; i++ {
		if err := os.Mkdir(filepath.Join(dir, fmt.Sprintf("agent-%04d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	entries, stat, err := readDirEntriesBounded(dir, KanbanReadLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != KanbanReadLimit || stat.DirectoryEntriesRead != KanbanReadLimit+1 || !stat.Truncated || stat.Complete {
		t.Fatalf("bounded directory truth = len:%d stat:%+v", len(entries), stat)
	}
	opts := KanbanNetworkOptions(nil)
	network, err := BuildNetworkWithOptions(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !network.AgentDirectoryTruncated {
		t.Fatal("bounded directory truncation was not propagated to Network")
	}
}

func TestKanbanSessionAttributionRequiresEventCoverageAcrossTokenWindow(t *testing.T) {
	t.Run("covered", func(t *testing.T) {
		agentDir := t.TempDir()
		writeKanbanTestFile(t, filepath.Join(agentDir, "logs", "token_ledger.jsonl"), strings.Join([]string{
			`{"ts":"2026-08-26T00:00:10Z","input":10}`,
			`{"ts":"2026-08-26T00:00:20Z","input":20}`,
		}, "\n")+"\n")
		writeKanbanTestFile(t, filepath.Join(agentDir, "logs", "events.jsonl"), strings.Join([]string{
			`{"type":"tool_call","ts":1787702405}`,
			`{"type":"psyche_molt","ts":1787702415}`,
			`{"type":"tool_call","ts":1787702418}`,
		}, "\n")+"\n")
		tokens := ReadKanbanTokenWindow(filepath.Join(agentDir, "logs", "token_ledger.jsonl"), 10)
		events := ReadKanbanEventWindow(agentDir, 10)
		stats, ok := KanbanSessionTokenStats(tokens, events)
		if !ok || stats.Current.APICalls != 1 || stats.Current.Input != 20 || events.ToolCalls.Current != 1 {
			t.Fatalf("covered attribution = ok:%t stats:%+v events:%+v", ok, stats, events)
		}
	})

	t.Run("old molt fell outside high-volume event tail", func(t *testing.T) {
		agentDir := t.TempDir()
		writeKanbanTestFile(t, filepath.Join(agentDir, "logs", "token_ledger.jsonl"), `{"ts":"2026-08-26T00:00:10Z","input":10}`+"\n")
		events := []string{`{"type":"psyche_molt","ts":1787702400}`}
		for i := 0; i < KanbanReadLimit; i++ {
			events = append(events, fmt.Sprintf(`{"type":"tool_call","ts":%d}`, 1787702500+i))
		}
		writeKanbanTestFile(t, filepath.Join(agentDir, "logs", "events.jsonl"), strings.Join(events, "\n")+"\n")
		tokens := ReadKanbanTokenWindow(filepath.Join(agentDir, "logs", "token_ledger.jsonl"), 10)
		eventWindow := ReadKanbanEventWindow(agentDir, 10)
		stats, ok := KanbanSessionTokenStats(tokens, eventWindow)
		if ok || stats.Current.APICalls != 0 || eventWindow.ToolCalls.Current != 0 {
			t.Fatalf("stale old-molt attribution escaped guard: ok=%t stats=%+v events=%+v", ok, stats, eventWindow)
		}
	})
}

func TestKanbanDispatchDeduplicatesLatestOccurrenceAndSkipsMalformed(t *testing.T) {
	agentDir := t.TempDir()
	writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", ".dispatch-ledger.jsonl"), strings.Join([]string{
		`{"schema":"lingtai.daemon_dispatch/v1","sequence":1,"run_id":"run-a"}`,
		`not-json`,
		`{"schema":"lingtai.daemon_dispatch/v1","sequence":2,"run_id":"run-b"}`,
		`{"schema":"lingtai.daemon_dispatch/v1","sequence":3,"run_id":"run-a"}`,
	}, "\n")+"\n")
	for runID, input := range map[string]int{"run-a": 7, "run-b": 5} {
		writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", runID, "daemon.json"), fmt.Sprintf(`{"run_id":%q,"state":"done"}`, runID))
		writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", runID, "logs", "token_ledger.jsonl"), fmt.Sprintf(`{"ts":"2026-08-26T00:00:00Z","input":%d,"endpoint":"https://api.z.ai/v1"}`+"\n", input))
	}
	snapshot := ReadKanbanDaemonDetailSnapshot(agentDir, 100, nil)
	if got := strings.Join(snapshot.RunIDs, ","); got != "run-a,run-b" {
		t.Fatalf("deduplicated latest order = %q, want run-a,run-b", got)
	}
	if snapshot.Counts.Total != 2 || snapshot.Counts.Done != 2 || snapshot.DispatchRead.ValidRecords != 3 || snapshot.DispatchRead.MalformedRecords != 1 {
		t.Fatalf("deduplicated counts/read truth = counts:%+v read:%+v", snapshot.Counts, snapshot.DispatchRead)
	}
	if got := snapshot.ByProvider["zhipu"]; got.Input != 12 || got.APICalls != 2 || len(snapshot.Recent) != 2 {
		t.Fatalf("deduplicated provider totals = %+v recent=%d", got, len(snapshot.Recent))
	}
}

func TestKanbanRejectsSymlinkedRunCardAndLedger(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics require supported POSIX")
	}
	for _, tc := range []struct {
		name string
		make func(t *testing.T, agentDir, external string)
	}{
		{
			name: "run directory",
			make: func(t *testing.T, agentDir, external string) {
				extRun := filepath.Join(external, "run-a")
				writeKanbanTestFile(t, filepath.Join(extRun, "daemon.json"), `{"state":"done"}`)
				writeKanbanTestFile(t, filepath.Join(extRun, "logs", "token_ledger.jsonl"), `{"input":999}`+"\n")
				if err := os.Symlink(extRun, filepath.Join(agentDir, "daemons", "run-a")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "daemon card",
			make: func(t *testing.T, agentDir, external string) {
				runDir := filepath.Join(agentDir, "daemons", "run-a")
				writeKanbanTestFile(t, filepath.Join(external, "daemon.json"), `{"handle":"escaped","state":"done"}`)
				if err := os.MkdirAll(filepath.Join(runDir, "logs"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(external, "daemon.json"), filepath.Join(runDir, "daemon.json")); err != nil {
					t.Fatal(err)
				}
				writeKanbanTestFile(t, filepath.Join(runDir, "logs", "token_ledger.jsonl"), `{"ts":"2026-08-26T00:00:00Z","input":7}`+"\n")
			},
		},
		{
			name: "token ledger",
			make: func(t *testing.T, agentDir, external string) {
				runDir := filepath.Join(agentDir, "daemons", "run-a")
				writeKanbanTestFile(t, filepath.Join(runDir, "daemon.json"), `{"handle":"safe","state":"done"}`)
				writeKanbanTestFile(t, filepath.Join(external, "token_ledger.jsonl"), `{"input":999}`+"\n")
				if err := os.MkdirAll(filepath.Join(runDir, "logs"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(external, "token_ledger.jsonl"), filepath.Join(runDir, "logs", "token_ledger.jsonl")); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agentDir := t.TempDir()
			external := t.TempDir()
			writeKanbanTestFile(t, filepath.Join(agentDir, "daemons", ".dispatch-ledger.jsonl"), `{"schema":"lingtai.daemon_dispatch/v1","run_id":"run-a"}`+"\n")
			tc.make(t, agentDir, external)
			snapshot := ReadKanbanDaemonDetailSnapshot(agentDir, 100, nil)
			if snapshot.WindowState != KanbanSourcePartial {
				t.Fatalf("unsafe %s state = %q, want partial", tc.name, snapshot.WindowState)
			}
			for _, row := range snapshot.Recent {
				if row.Input == 999 || row.Handle == "escaped" {
					t.Fatalf("unsafe %s content leaked: %+v", tc.name, row)
				}
			}
		})
	}
}

func TestResolveContainedKanbanChildRejectsTopologyEscapes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics require supported POSIX")
	}
	baseDir := t.TempDir()
	external := t.TempDir()
	inside := filepath.Join(baseDir, "inside")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(baseDir, "escape-link")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		wantOK     bool
	}{
		{name: "contained child", path: "inside", wantOK: true},
		{name: "absolute", path: inside},
		{name: "dot dot", path: filepath.Join("..", filepath.Base(external))},
		{name: "symlink escape", path: "escape-link"},
		{name: "missing", path: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveContainedKanbanChild(tc.path, baseDir)
			if ok != tc.wantOK || (ok && got != inside) {
				t.Fatalf("resolveContainedKanbanChild(%q) = %q,%t", tc.path, got, ok)
			}
		})
	}
}
