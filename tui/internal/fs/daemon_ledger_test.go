package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
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

func TestDaemonRecentLedgerMissing(t *testing.T) {
	agentDir := t.TempDir()
	// No daemons/ directory at all → empty, not an error.
	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 0 {
		t.Fatalf("expected empty, got %d entries", len(entries))
	}
}

func TestDaemonRecentLedgerEmptyDaemonsDir(t *testing.T) {
	agentDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(agentDir, "daemons"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	entries := DaemonRecentLedger(agentDir, 100)
	if len(entries) != 0 {
		t.Fatalf("expected empty, got %d entries", len(entries))
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
		"model":  "glm-4.6",
	})
	writeDaemonLedger(t, agentDir, "em-1-20260101-000000-abc123", []string{
		`{"ts":"2026-01-01T00:00:01","input":10,"output":5,"thinking":1,"cached":2,"model":"glm-4.6","endpoint":"https://z.ai/api"}`,
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
	if e.Model != "glm-4.6" {
		t.Errorf("Model = %q", e.Model)
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

func TestDaemonLedgerSummaryNoDaemonsDir(t *testing.T) {
	agentDir := t.TempDir()
	got, _ := DaemonLedgerSummary(agentDir, 0)
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(got))
	}
}

func TestDaemonLedgerSummaryEmptyDaemonsDir(t *testing.T) {
	agentDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(agentDir, "daemons"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, _ := DaemonLedgerSummary(agentDir, 0)
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(got))
	}
}

func TestDaemonLedgerSummaryGroupsByProvider(t *testing.T) {
	agentDir := t.TempDir()
	writeDaemonState(t, agentDir, "em-1-x", map[string]interface{}{"handle": "em-1", "state": "done"})
	writeDaemonState(t, agentDir, "em-2-y", map[string]interface{}{"handle": "em-2", "state": "done"})
	writeDaemonLedger(t, agentDir, "em-1-x", []string{
		`{"ts":"2026-01-01T00:00:01","input":10,"output":5,"thinking":1,"cached":2,"model":"glm-4.6","endpoint":"https://z.ai/api"}`,
	})
	writeDaemonLedger(t, agentDir, "em-2-y", []string{
		`{"ts":"2026-01-01T00:00:01","input":8,"output":3,"model":"deepseek-v3","endpoint":"https://api.deepseek.com"}`,
		`{"ts":"2026-01-01T00:00:02","input":3,"output":1,"model":"deepseek-v3","endpoint":"https://api.deepseek.com"}`,
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
		`{"ts":"2026-01-01T00:00:01","input":42,"output":7,"model":"glm-4.6","endpoint":"https://z.ai/api"}`,
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
		`{"ts":"2026-01-01T00:00:01","input":10,"output":5,"model":"glm-4.6","endpoint":"https://z.ai/api"}`,
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
	// No preset_provider, backend=lingtai (ignored), but model=deepseek-v3
	// → DeriveLedgerProvider("", "deepseek-v3") = "deepseek".
	writeDaemonState(t, agentDir, "em-ds", map[string]interface{}{
		"handle":     "em-ds",
		"state":      "done",
		"backend":    "lingtai",
		"model":      "deepseek-v3",
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
		"preset_model": "glm-4.6",
		"model":        "unknown-model",
		"cli_tokens":   map[string]interface{}{"input": 60, "output": 20, "calls": 1},
	})
	totals, _ := DaemonLedgerSummary(agentDir, 100)
	zhipu, ok := totals["zhipu"]
	if !ok {
		t.Fatalf("expected zhipu bucket from preset_model glm-4.6, got: %v", totals)
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
