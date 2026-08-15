package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// writeDaemonLedger writes a daemon's per-call token_ledger.jsonl under
// agentDir/daemons/<runID>/logs/token_ledger.jsonl. Each line is one entry.
func writeDaemonLedger(t *testing.T, agentDir, runID string, lines []string) {
	t.Helper()
	dir := filepath.Join(agentDir, "daemons", runID, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir daemon logs: %v", err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "token_ledger.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write daemon ledger: %v", err)
	}
}

// --- Backward-compat tests (wrappers still work) ---

func TestDaemonLedgerSummaryEmptyStates(t *testing.T) {
	for _, tc := range []struct {
		name             string
		createDaemonsDir bool
	}{
		// No daemons/ directory at all must be empty, not an error.
		{name: "missing_daemons_directory"},
		{name: "empty_daemons_directory", createDaemonsDir: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agentDir := t.TempDir()
			if tc.createDaemonsDir {
				if err := os.MkdirAll(filepath.Join(agentDir, "daemons"), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			}

			// Preserve the summary callers' recentN=0 input and assert both return values.
			totalsZero, recentZero := DaemonLedgerSummary(agentDir, 0)
			if len(totalsZero) != 0 {
				t.Fatalf("recentN=0: expected empty totals, got %d entries", len(totalsZero))
			}
			if len(recentZero) != 0 {
				t.Fatalf("recentN=0: expected empty recent rows, got %d entries", len(recentZero))
			}

			// Preserve the wrapper callers' recentN=100 input while asserting both halves.
			totals, recent := DaemonLedgerSummary(agentDir, 100)
			if len(totals) != 0 {
				t.Fatalf("recentN=100: expected empty totals, got %d entries", len(totals))
			}
			if len(recent) != 0 {
				t.Fatalf("recentN=100: expected empty recent rows, got %d entries", len(recent))
			}
			if wrapped := DaemonRecentLedger(agentDir, 100); !reflect.DeepEqual(wrapped, recent) {
				t.Fatalf("DaemonRecentLedger = %+v, want summary recent rows %+v", wrapped, recent)
			}
		})
	}
}

func TestDaemonRecentLedgerTagsIdentity(t *testing.T) {
	agentDir := t.TempDir()
	// Daemon run with a daemon.json identity card and one ledger entry.
	writeDaemonState(t, agentDir, "em-1-20260101-000000-abc123", map[string]interface{}{
		"handle": "em-1",
		"run_id": "em-1-20260101-000000-abc123",
		"state":  "running",
		"task":   "do a thing",
		"model":  "GLM-5.2",
	})
	writeDaemonLedger(t, agentDir, "em-1-20260101-000000-abc123", []string{
		`{"ts":"2026-01-01T00:00:01","input":10,"output":5,"thinking":1,"cached":2,"model":"GLM-5.2","endpoint":"https://z.ai/api"}`,
	})

	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.RunID != "em-1-20260101-000000-abc123" {
		t.Errorf("RunID = %q", e.RunID)
	}
	if e.Handle != "em-1" {
		t.Errorf("Handle = %q", e.Handle)
	}
	if e.State != "running" {
		t.Errorf("State = %q", e.State)
	}
	if e.Input != 10 || e.Output != 5 {
		t.Errorf("tokens wrong: in=%d out=%d", e.Input, e.Output)
	}
	if e.Model != "GLM-5.2" {
		t.Errorf("Model = %q", e.Model)
	}
}

func TestDaemonRecentLedgerPreservesBackendIndependently(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-1-backend", map[string]interface{}{
		"handle":  "em-1",
		"state":   "done",
		"backend": "claude-p",
	})
	writeDaemonLedger(t, agentDir, "em-1-backend", []string{
		`{"ts":"2026-01-01T00:00:01","input":10,"output":5,"model":"GLM-5.2","endpoint":"https://z.ai/api"}`,
	})

	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Backend != "claude-p" {
		t.Fatalf("Backend = %q, want claude-p", entry.Backend)
	}
	if got := DeriveLedgerProvider(entry.Endpoint, entry.Model); got != "zhipu" {
		t.Fatalf("provider = %q, want zhipu", got)
	}
	if entry.Model != "GLM-5.2" {
		t.Fatalf("Model = %q, want GLM-5.2", entry.Model)
	}
}

func TestDaemonRecentLedgerAggregatesAndSortsNewestFirst(t *testing.T) {
	agentDir := t.TempDir()
	// Two daemons, interleaved timestamps. The result must be globally sorted
	// newest-first by ts, regardless of which daemon dir they came from.
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{"handle": "em-1", "state": "done"})
	writeDaemonState(t, agentDir, "em-2-y", map[string]interface{}{"handle": "em-2", "state": "running"})
	writeDaemonLedger(t, agentDir, "em-1-x", []string{
		`{"ts":"2026-01-01T00:00:01","input":1}`,
		`{"ts":"2026-01-01T00:00:05","input":5}`,
	})
	writeDaemonLedger(t, agentDir, "em-2-y", []string{
		`{"ts":"2026-01-01T00:00:03","input":3}`,
		`{"ts":"2026-01-01T00:00:09","input":9}`,
	})

	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	wantOrder := []int64{9, 5, 3, 1}
	for i, w := range wantOrder {
		if entries[i].Input != w {
			t.Errorf("entry %d input = %d, want %d", i, entries[i].Input, w)
		}
	}
	// Identity tags travel with each entry.
	if entries[0].Handle != "em-2" {
		t.Errorf("newest entry handle = %q, want em-2", entries[0].Handle)
	}
}

func TestDaemonRecentLedgerTrimsToRecentN(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{"handle": "em-1", "state": "done"})
	var lines []string
	for i := 0; i < 250; i++ {
		// ts strictly ascending so newest is the last written. Encode the
		// loop index directly into the seconds-fractional slot so ordering is
		// unambiguous past 60.
		lines = append(lines, fmt.Sprintf(`{"ts":"2026-01-01T00:00:00.%06d","input":%d}`, i, i))
	}
	writeDaemonLedger(t, agentDir, "em-1-x", lines)

	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 100 {
		t.Fatalf("expected 100 entries, got %d", len(entries))
	}
	// Newest first: input 249 should lead.
	if entries[0].Input != 249 {
		t.Errorf("newest input = %d, want 249", entries[0].Input)
	}
}

func TestDaemonRecentLedgerSkipsMalformed(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{"handle": "em-1", "state": "done"})
	writeDaemonLedger(t, agentDir, "em-1-x", []string{
		`{not json`,
		`{"ts":"2026-01-01T00:00:01","input":7}`,
		``,
	})
	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 1 {
		t.Fatalf("expected 1 valid entry, got %d", len(entries))
	}
	if entries[0].Input != 7 {
		t.Errorf("input = %d, want 7", entries[0].Input)
	}
}

func TestDaemonRecentLedgerSkipsNonObjectRowsPreservesSparseObjects(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{
		"handle":  "em-1",
		"state":   "done",
		"backend": "claude-p",
	})
	writeDaemonLedger(t, agentDir, "em-1-x", []string{
		`null`,
		`[]`,
		`{}`,
		`{"ts":"2026-01-01T00:00:01","input":7}`,
	})

	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 2 {
		t.Fatalf("expected sparse object and valid row only, got %d entries: %+v", len(entries), entries)
	}
	var sawSparse, sawValid bool
	for _, entry := range entries {
		if entry.Backend != "claude-p" {
			t.Errorf("object row backend = %q, want claude-p", entry.Backend)
		}
		switch entry.Input {
		case 0:
			if entry.TS != "" {
				t.Errorf("sparse object timestamp = %q, want empty", entry.TS)
			}
			sawSparse = true
		case 7:
			sawValid = true
		}
	}
	if !sawSparse || !sawValid {
		t.Fatalf("expected both sparse and valid object rows, got %+v", entries)
	}
}

func TestDaemonRecentLedgerRejectsTypeInvalidDaemonCard(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{
		"handle":  "em-1",
		"state":   1,
		"backend": "claude-p",
	})
	writeDaemonLedger(t, agentDir, "em-1-x", []string{
		`{"ts":"2026-01-01T00:00:01","input":7,"model":"GLM-5.2","endpoint":"https://z.ai/api"}`,
	})

	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 1 {
		t.Fatalf("expected valid ledger row, got %d entries", len(entries))
	}
	if entries[0].Backend != "" || entries[0].Handle != "" || entries[0].State != "" {
		t.Fatalf("type-invalid daemon card leaked fields: %+v", entries[0])
	}
}

func TestDaemonRecentLedgerMissingDaemonJSON(t *testing.T) {
	agentDir := t.TempDir()
	// Ledger present but no daemon.json — identity tags fall back to run dir name.
	writeDaemonLedger(t, agentDir, "em-9-z", []string{
		`{"ts":"2026-01-01T00:00:01","input":4}`,
	})
	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].RunID != "em-9-z" {
		t.Errorf("RunID = %q, want em-9-z", entries[0].RunID)
	}
}

// --- Existing aggregated-totals tests (through unified API) ---

func TestDaemonLedgerSummaryGroupsByProvider(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{"handle": "em-1", "state": "done"})
	writeDaemonState(t, agentDir, "em-2-y", map[string]interface{}{"handle": "em-2", "state": "done"})
	writeDaemonLedger(t, agentDir, "em-1-x", []string{
		`{"ts":"2026-01-01T00:00:01","input":10,"output":5,"thinking":1,"cached":2,"model":"GLM-5.2","endpoint":"https://z.ai/api"}`,
	})
	writeDaemonLedger(t, agentDir, "em-2-y", []string{
		`{"ts":"2026-01-01T00:00:01","input":8,"output":3,"model":"deepseek-v4-pro","endpoint":"https://api.deepseek.com"}`,
		`{"ts":"2026-01-01T00:00:02","input":3,"output":1,"model":"deepseek-v4-pro","endpoint":"https://api.deepseek.com"}`,
	})
	got, _ := DaemonLedgerSummary(agentDir, 0)
	if len(got) != 2 {
		t.Fatalf("expected 2 providers, got %d: %v", len(got), got)
	}
	zhipu := got["zhipu"]
	if zhipu.Input != 10 || zhipu.Output != 5 || zhipu.Thinking != 1 || zhipu.Cached != 2 || zhipu.APICalls != 1 {
		t.Errorf("zhipu totals wrong: %+v", zhipu)
	}
	deepseek := got["deepseek"]
	if deepseek.Input != 11 || deepseek.Output != 4 || deepseek.APICalls != 2 {
		t.Errorf("deepseek totals wrong: %+v", deepseek)
	}
}

func TestDaemonLedgerSummaryLedgerOverFallbackPrecedence(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-5", map[string]interface{}{
		"handle":     "em-5",
		"state":      "done",
		"backend":    "claude-p",
		"cli_tokens": map[string]interface{}{"input": 9999, "output": 99, "calls": 1},
	})
	writeDaemonLedger(t, agentDir, "em-5", []string{
		`{"ts":"2026-01-01T00:00:01","input":42,"output":7,"model":"GLM-5.2","endpoint":"https://z.ai/api"}`,
	})
	got, _ := DaemonLedgerSummary(agentDir, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(got))
	}
	zhipu := got["zhipu"]
	if zhipu.Input != 42 || zhipu.APICalls != 1 {
		t.Errorf("ledger should take precedence over cli_tokens: %+v", zhipu)
	}
	// No em-5 bucket — ledger attribution uses DeriveLedgerProvider, not handle.
	if _, ok := got["em-5"]; ok {
		t.Errorf("should not have em-5 bucket when ledger is present")
	}
}

// --- New unified API tests ---

func TestDaemonLedgerSummaryReturnsBothTotalsAndRecent(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{"handle": "em-1", "state": "done"})
	writeDaemonLedger(t, agentDir, "em-1-x", []string{
		`{"ts":"2026-01-01T00:00:01","input":10,"output":5,"model":"GLM-5.2","endpoint":"https://z.ai/api"}`,
	})
	totals, recent := DaemonLedgerSummary(agentDir, 100)
	if len(totals) != 1 {
		t.Fatalf("expected 1 provider in totals, got %d", len(totals))
	}
	if len(recent) != 1 {
		t.Fatalf("expected 1 recent entry, got %d", len(recent))
	}
	if recent[0].Handle != "em-1" {
		t.Errorf("recent Handle = %q, want em-1", recent[0].Handle)
	}
	zhipu := totals["zhipu"]
	if zhipu.Input != 10 || zhipu.APICalls != 1 {
		t.Errorf("zhipu totals wrong: %+v", zhipu)
	}
}

func TestDaemonLedgerSummaryTwoCLIRunsSameBackendAggregateToOneBucket(t *testing.T) {
	agentDir := t.TempDir()
	// Two CLI daemon runs (no ledger), both with backend=claude-p.
	// They must aggregate to ONE "claude-p" bucket, not separate em-handle buckets.
	writeDaemonState(t, agentDir, "em-1-cli", map[string]interface{}{
		"handle":     "em-1",
		"state":      "done",
		"backend":    "claude-p",
		"cli_tokens": map[string]interface{}{"input": 100, "output": 50, "calls": 2},
	})
	writeDaemonState(t, agentDir, "em-2-cli", map[string]interface{}{
		"handle":     "em-2",
		"state":      "done",
		"backend":    "claude-p",
		"cli_tokens": map[string]interface{}{"input": 200, "output": 80, "calls": 3},
	})
	totals, recent := DaemonLedgerSummary(agentDir, 100)
	if len(totals) != 1 {
		t.Fatalf("expected 1 bucket, got %d: %v", len(totals), totals)
	}
	claude := totals["claude-p"]
	if claude.Input != 300 || claude.Output != 130 || claude.APICalls != 5 {
		t.Errorf("claude-p totals wrong: %+v", claude)
	}
	// No em-handle buckets.
	for _, bad := range []string{"em-1", "em-2"} {
		if _, ok := totals[bad]; ok {
			t.Errorf("should not have %q bucket", bad)
		}
	}
	if len(recent) != 0 {
		t.Errorf("recent should be empty (no ledger entries), got %d", len(recent))
	}
}

func TestDaemonLedgerSummaryCLITokensNormalizeDisjointCachedInput(t *testing.T) {
	agentDir := t.TempDir()
	const (
		rawInput = int64(406_915)
		cached   = int64(136_061_317)
		total    = int64(136_468_232)
	)
	writeDaemonState(t, agentDir, "em-claude-cache", map[string]interface{}{
		"backend": "claude-p",
		"cli_tokens": map[string]interface{}{
			"input": rawInput, "output": 1_063_675, "cached": cached, "calls": 53,
		},
	})

	totals, recent := DaemonLedgerSummary(agentDir, 100)
	claude, ok := totals["claude-p"]
	if !ok {
		t.Fatalf("expected claude-p bucket, got: %v", totals)
	}
	if claude.Input != total || claude.Cached != cached {
		t.Fatalf("canonical input/cached = %d/%d, want %d/%d", claude.Input, claude.Cached, total, cached)
	}
	if claude.Cached > claude.Input {
		t.Fatalf("cached input %d exceeds canonical input %d", claude.Cached, claude.Input)
	}
	if miss := claude.Input - claude.Cached; miss != rawInput {
		t.Fatalf("cache miss = %d, want raw input %d", miss, rawInput)
	}
	if rate := 100 * float64(claude.Cached) / float64(claude.Input); rate < 99.6 || rate > 99.8 {
		t.Fatalf("cache hit rate = %.3f%%, want about 99.7%%", rate)
	}
	if claude.Output != 1_063_675 || claude.APICalls != 53 {
		t.Fatalf("output/calls changed during normalization: %+v", claude)
	}
	if len(recent) != 0 {
		t.Fatalf("CLI snapshot should not create recent ledger rows, got %d", len(recent))
	}
}

func TestDaemonLedgerSummaryPresetProviderPrecedence(t *testing.T) {
	agentDir := t.TempDir()
	// preset_provider is set; backend is also set but preset_provider wins.
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{
		"handle":          "em-1",
		"state":           "done",
		"backend":         "claude-p",
		"preset_provider": "deepseek",
		"cli_tokens":      map[string]interface{}{"input": 100, "output": 50, "calls": 1},
	})
	totals, _ := DaemonLedgerSummary(agentDir, 100)
	if len(totals) != 1 {
		t.Fatalf("expected 1 bucket, got %d: %v", len(totals), totals)
	}
	ds, ok := totals["deepseek"]
	if !ok {
		t.Fatalf("expected deepseek bucket, got: %v", totals)
	}
	if ds.Input != 100 {
		t.Errorf("deepseek input = %d, want 100", ds.Input)
	}
	// No claude-p bucket.
	if _, ok := totals["claude-p"]; ok {
		t.Errorf("should not have claude-p bucket when preset_provider is set")
	}
}

func TestDaemonLedgerSummaryHonestUnknownFallback(t *testing.T) {
	agentDir := t.TempDir()
	// No preset_provider, no backend, no recognizable model — must say "daemon".
	writeDaemonState(t, agentDir, "anon-run", map[string]interface{}{
		"state":      "done",
		"cli_tokens": map[string]interface{}{"input": 50, "output": 10, "calls": 1},
	})
	totals, _ := DaemonLedgerSummary(agentDir, 100)
	if len(totals) != 1 {
		t.Fatalf("expected 1 bucket, got %d: %v", len(totals), totals)
	}
	anon, ok := totals["daemon"]
	if !ok {
		t.Fatalf("expected 'daemon' bucket, got: %v", totals)
	}
	if anon.Input != 50 || anon.APICalls != 1 {
		t.Errorf("anon fallback wrong: %+v", anon)
	}
}

func TestDaemonLedgerSummaryModelDerivationFallback(t *testing.T) {
	agentDir := t.TempDir()
	// No preset_provider, backend=lingtai (ignored), but model=deepseek-v4-pro
	// → DeriveLedgerProvider("", "deepseek-v4-pro") = "deepseek".
	writeDaemonState(t, agentDir, "em-ds", map[string]interface{}{
		"handle":     "em-ds",
		"state":      "done",
		"backend":    "lingtai",
		"model":      "deepseek-v4-pro",
		"cli_tokens": map[string]interface{}{"input": 75, "output": 25, "calls": 2},
	})
	totals, _ := DaemonLedgerSummary(agentDir, 100)
	if len(totals) != 1 {
		t.Fatalf("expected 1 bucket, got %d: %v", len(totals), totals)
	}
	ds, ok := totals["deepseek"]
	if !ok {
		t.Fatalf("expected deepseek bucket (model derivation), got: %v", totals)
	}
	if ds.Input != 75 || ds.APICalls != 2 {
		t.Errorf("deepseek totals wrong: %+v", ds)
	}
}

func TestDaemonLedgerSummaryPresetModelOverModel(t *testing.T) {
	agentDir := t.TempDir()
	// preset_model should be preferred over model for derivation.
	writeDaemonState(t, agentDir, "em-pm", map[string]interface{}{
		"handle":       "em-pm",
		"state":        "done",
		"preset_model": "GLM-5.2",
		"model":        "unknown-model",
		"cli_tokens":   map[string]interface{}{"input": 60, "output": 20, "calls": 1},
	})
	totals, _ := DaemonLedgerSummary(agentDir, 100)
	zhipu, ok := totals["zhipu"]
	if !ok {
		t.Fatalf("expected zhipu bucket from preset_model GLM-5.2, got: %v", totals)
	}
	if zhipu.Input != 60 {
		t.Errorf("zhipu input = %d, want 60", zhipu.Input)
	}
	if _, ok := totals["unknown"]; ok {
		t.Errorf("should not have 'unknown' bucket from fallback model")
	}
}

func TestDaemonLedgerSummaryBackendOverLingtai(t *testing.T) {
	agentDir := t.TempDir()
	// backend=lingtai is skipped, backend=mimocode is used.
	writeDaemonState(t, agentDir, "em-mimo", map[string]interface{}{
		"handle":     "em-mimo",
		"state":      "done",
		"backend":    "mimocode",
		"cli_tokens": map[string]interface{}{"input": 30, "output": 10, "calls": 1},
	})
	totals, _ := DaemonLedgerSummary(agentDir, 100)
	mm, ok := totals["mimocode"]
	if !ok {
		t.Fatalf("expected mimocode bucket, got: %v", totals)
	}
	if mm.Input != 30 {
		t.Errorf("mimocode input = %d, want 30", mm.Input)
	}
}

// --- Legacy token fallback (no cli_tokens) ---

func TestDaemonLedgerSummaryLegacyTokensFallback(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-4-legacy", map[string]interface{}{
		"handle": "em-4",
		"state":  "done",
		"tokens": map[string]interface{}{"input": 500, "output": 200, "thinking": 50, "cached": 100},
	})
	got, _ := DaemonLedgerSummary(agentDir, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(got))
	}
	// No backend or model → "daemon".
	legacy := got["daemon"]
	if legacy.Input != 500 || legacy.Output != 200 || legacy.Thinking != 50 || legacy.Cached != 100 {
		t.Errorf("legacy tokens fallback wrong: %+v", legacy)
	}
	if legacy.APICalls != 0 {
		t.Errorf("legacy fallback should have 0 api_calls, got %d", legacy.APICalls)
	}
}

func TestDaemonLedgerSummaryZeroCLITokensFallsBackToLegacyTokens(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-4-transitional", map[string]interface{}{
		"backend":    "claude-p",
		"cli_tokens": map[string]interface{}{"input": 0, "output": 0, "thinking": 0, "cached": 0, "calls": 0},
		"tokens":     map[string]interface{}{"input": 500, "output": 200, "thinking": 50, "cached": 100},
	})
	got, _ := DaemonLedgerSummary(agentDir, 0)
	legacy := got["claude-p"]
	if legacy.Input != 500 || legacy.Output != 200 || legacy.Thinking != 50 || legacy.Cached != 100 {
		t.Errorf("zero cli_tokens should fall back to legacy tokens: %+v", legacy)
	}
	if legacy.APICalls != 0 {
		t.Errorf("legacy fallback should have 0 api_calls, got %d", legacy.APICalls)
	}
}

func TestDaemonDetailSnapshotBoundsRunContentsButCountsAllDirectories(t *testing.T) {
	agentDir := t.TempDir()
	base := time.Unix(1_700_000_000, 0)
	for i := 1; i <= 5; i++ {
		runID := fmt.Sprintf("em-%d", i)
		state := "done"
		if i == 5 {
			state = "running"
		}
		writeDaemonState(t, agentDir, runID, map[string]interface{}{
			"handle": fmt.Sprintf("em-%d", i),
			"state":  state,
		})
		writeDaemonLedger(t, agentDir, runID, []string{
			fmt.Sprintf(`{"ts":"2026-01-01T00:00:0%dZ","input":%d}`, i, i),
		})
		runDir := filepath.Join(agentDir, "daemons", runID)
		stamp := base.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(runDir, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	snapshot := ReadDaemonDetailSnapshot(agentDir, 2, 100)
	if snapshot.TotalRuns != 5 || snapshot.Counts.Total != 5 || snapshot.ScannedRuns != 2 {
		t.Fatalf("window/counts = scanned:%d total:%d counts:%+v", snapshot.ScannedRuns, snapshot.TotalRuns, snapshot.Counts)
	}
	if snapshot.Counts.Running != 1 || snapshot.TerminalRuns != 4 {
		t.Fatalf("running/terminal = %d/%d, want 1/4", snapshot.Counts.Running, snapshot.TerminalRuns)
	}
	if got := snapshot.ByProvider["unknown"].Input; got != 9 {
		t.Fatalf("recent-window input = %d, want 9 (runs 5+4)", got)
	}
	if len(snapshot.Recent) != 2 || snapshot.Recent[0].Input != 5 || snapshot.Recent[1].Input != 4 {
		t.Fatalf("recent rows = %+v", snapshot.Recent)
	}
}

func TestDaemonDetailSnapshotInvalidatesLiveRunOnNewLedgerRow(t *testing.T) {
	agentDir := t.TempDir()
	runID := "em-live"
	writeDaemonState(t, agentDir, runID, map[string]interface{}{
		"handle": "em-live",
		"state":  "running",
	})
	writeDaemonLedger(t, agentDir, runID, []string{
		`{"ts":"2026-01-01T00:00:01Z","input":1}`,
	})
	heartbeat := filepath.Join(agentDir, "daemons", runID, ".heartbeat")
	if err := os.WriteFile(heartbeat, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	first := ReadDaemonDetailSnapshot(agentDir, 128, 100)
	if len(first.Recent) != 1 {
		t.Fatalf("first rows = %d, want 1", len(first.Recent))
	}
	ledger := filepath.Join(agentDir, "daemons", runID, "logs", "token_ledger.jsonl")
	f, err := os.OpenFile(ledger, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"ts":"2026-01-01T00:00:02Z","input":2}` + "\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(heartbeat, future, future); err != nil {
		t.Fatal(err)
	}

	second := ReadDaemonDetailSnapshot(agentDir, 128, 100)
	if len(second.Recent) != 2 || second.Recent[0].Input != 2 {
		t.Fatalf("live append hidden by cache: %+v", second.Recent)
	}
	if got := second.ByProvider["unknown"].Input; got != 3 {
		t.Fatalf("live total input = %d, want 3", got)
	}
}

// --- 10-minute live-window tests (CountDaemons) ---

// setRunDirMtime backdates a run directory's mtime to stamp. The daemon.json
// inside keeps its own fresh mtime, so any read of the state file would be
// observable through the counts.
func setRunDirMtime(t *testing.T, agentDir, runID string, stamp time.Time) {
	t.Helper()
	runDir := filepath.Join(agentDir, "daemons", runID)
	if err := os.Chtimes(runDir, stamp, stamp); err != nil {
		t.Fatalf("chtimes run dir %s: %v", runID, err)
	}
}

func TestCountDaemonsWindowIncludesRecentRuns(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "r1", map[string]interface{}{"state": "running"})
	writeDaemonState(t, agentDir, "r2", map[string]interface{}{"state": "active"})
	writeDaemonState(t, agentDir, "r3", map[string]interface{}{"state": "done"})
	writeDaemonState(t, agentDir, "r4", map[string]interface{}{"state": "failed"})
	writeDaemonState(t, agentDir, "r5", map[string]interface{}{"state": "queued"})
	// One second inside the window; every state shows while recent.
	inside := time.Now().Add(-(daemonListWindow - time.Second))
	for _, runID := range []string{"r1", "r2", "r3", "r4", "r5"} {
		setRunDirMtime(t, agentDir, runID, inside)
	}

	counts := CountDaemons(agentDir)
	if counts.Running != 2 || counts.Queued != 1 || counts.Done != 1 || counts.Failed != 1 || counts.Total != 5 {
		t.Fatalf("recent-window counts = %+v, want running:2 queued:1 done:1 failed:1 total:5", counts)
	}
}

func TestCountDaemonsWindowSkipsStaleRunsWithoutReadingState(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "fresh", map[string]interface{}{"state": "running"})

	// Stale runs: their daemon.json files are fresh on disk and would each count
	// as running/done/failed if opened, but the run directories are far outside
	// the window. CountDaemons must short-circuit on the directory mtime before
	// any state-file read, so none of these can leak into the summary.
	for _, runID := range []string{"stale-running", "stale-done", "stale-failed"} {
		writeDaemonState(t, agentDir, runID, map[string]interface{}{"state": "running"})
		setRunDirMtime(t, agentDir, runID, time.Now().Add(-(daemonListWindow + time.Hour)))
	}

	counts := CountDaemons(agentDir)
	if counts.Total != 1 || counts.Running != 1 || counts.Done != 0 || counts.Failed != 0 || counts.Queued != 0 {
		t.Fatalf("stale runs leaked into summary: %+v", counts)
	}
}

func TestCountDaemonsWindowBoundary(t *testing.T) {
	now := time.Now()
	if !withinDaemonListWindow(now.Add(-daemonListWindow), now) {
		t.Error("run exactly daemonListWindow old should still be included")
	}
	if !withinDaemonListWindow(now, now) {
		t.Error("run with zero age should be included")
	}
	if !withinDaemonListWindow(now.Add(time.Minute), now) {
		t.Error("future-mtime run (clock skew) should be included")
	}
	if withinDaemonListWindow(now.Add(-(daemonListWindow + time.Nanosecond)), now) {
		t.Error("run just past daemonListWindow should be excluded")
	}
}

func TestCountDaemonsWindowGatesFlatDaemonJSON(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "sub", map[string]interface{}{"state": "done"})

	// Legacy flat layout: a daemon.json directly under daemons/ is gated by its
	// own file mtime, not a run-directory mtime.
	flat := filepath.Join(agentDir, "daemons", "daemon.json")
	if err := os.MkdirAll(filepath.Dir(flat), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flat, []byte(`{"state":"running"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-(daemonListWindow + time.Hour))
	if err := os.Chtimes(flat, stale, stale); err != nil {
		t.Fatal(err)
	}

	counts := CountDaemons(agentDir)
	if counts.Running != 0 || counts.Total != 1 {
		t.Fatalf("stale flat daemon.json counted: %+v", counts)
	}
}

// BenchmarkCountDaemonsManyStaleRuns measures the per-second summary over a
// daemons directory dominated by historical runs: the window gate must skip all
// stale directories on directory metadata alone, never opening their state
// files, so the tick stays cheap regardless of accumulated history.
func BenchmarkCountDaemonsManyStaleRuns(b *testing.B) {
	agentDir := b.TempDir()
	daemonDir := filepath.Join(agentDir, "daemons")
	staleBase := time.Now().Add(-(daemonListWindow + 30*24*time.Hour))
	for i := 0; i < 1500; i++ {
		runID := fmt.Sprintf("em-%04d", i)
		runDir := filepath.Join(daemonDir, runID)
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runDir, "daemon.json"), []byte(`{"state":"done"}`), 0o644); err != nil {
			b.Fatal(err)
		}
		stamp := staleBase.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(runDir, stamp, stamp); err != nil {
			b.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		runID := fmt.Sprintf("live-%d", i)
		runDir := filepath.Join(daemonDir, runID)
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runDir, "daemon.json"), []byte(`{"state":"running"}`), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counts := CountDaemons(agentDir)
		if counts.Total != 5 || counts.Running != 5 {
			b.Fatalf("bad counts: %+v", counts)
		}
	}
}

func BenchmarkDaemonDetailSnapshot1500Runs(b *testing.B) {
	agentDir := b.TempDir()
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < 1500; i++ {
		runID := fmt.Sprintf("em-%04d", i)
		runDir := filepath.Join(agentDir, "daemons", runID)
		logsDir := filepath.Join(runDir, "logs")
		if err := os.MkdirAll(logsDir, 0o755); err != nil {
			b.Fatal(err)
		}
		card := fmt.Sprintf(`{"handle":%q,"state":"done"}`, runID)
		if err := os.WriteFile(filepath.Join(runDir, "daemon.json"), []byte(card), 0o644); err != nil {
			b.Fatal(err)
		}
		ledger := fmt.Sprintf(`{"ts":"2026-01-01T00:00:00.%06dZ","input":1}`+"\n", i)
		if err := os.WriteFile(filepath.Join(logsDir, "token_ledger.jsonl"), []byte(ledger), 0o644); err != nil {
			b.Fatal(err)
		}
		stamp := base.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(runDir, stamp, stamp); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snapshot := ReadDaemonDetailSnapshot(agentDir, 128, 100)
		if snapshot.TotalRuns != 1500 || snapshot.ScannedRuns != 128 || len(snapshot.Recent) != 100 {
			b.Fatalf("bad snapshot: total=%d scanned=%d rows=%d", snapshot.TotalRuns, snapshot.ScannedRuns, len(snapshot.Recent))
		}
	}
}
