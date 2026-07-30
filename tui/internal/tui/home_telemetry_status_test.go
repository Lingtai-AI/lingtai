package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func writeHomeStatus(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".status.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGatherHomeTelemetryUsesOneStatusSnapshotForEconomyAndContext(t *testing.T) {
	dir := t.TempDir()
	writeHomeStatus(t, dir, `{
  "tokens": {
    "input_tokens": 1000,
    "output_tokens": 200,
    "thinking_tokens": 50,
    "cached_tokens": 700,
    "api_calls": 4,
    "context": {"total_tokens": 1234, "window_size": 8000, "usage_pct": 15.425}
  }
}`)

	got := (MailModel{orchestrator: dir}).gatherHomeTelemetry()
	if got.apiCalls != 4 || got.sessionTokens != 1250 || got.inputTokens != 1000 || got.cached != 700 {
		t.Fatalf("economy = %+v, want api=4 total=1250 input=1000 cached=700", got)
	}
	if got.contextUsed != 1234 || got.contextLimit != 8000 || got.contextUsage != 0.15425 {
		t.Fatalf("context = %+v, want used=1234 limit=8000 usage=0.15425", got)
	}
}

func TestGatherHomeTelemetryDoesNotUseSQLiteOrLedgerFallback(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A sidecar and a ledger make the former Home path eligible to launch sqlite3
	// and reconstruct economy from history. Home must not touch either source now.
	if err := os.WriteFile(filepath.Join(logsDir, "log.sqlite"), []byte("not a real database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "token_ledger.jsonl"), []byte(`{"input":999,"output":1,"api_calls":99}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "sqlite-launched")
	fakeSQLite := filepath.Join(binDir, "sqlite3")
	if err := os.WriteFile(fakeSQLite, []byte("#!/bin/sh\nprintf x > \"$HOME_TELEMETRY_SQLITE_MARKER\"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("HOME_TELEMETRY_SQLITE_MARKER", marker)

	writeHomeStatus(t, dir, `{"tokens":{"input_tokens":10,"output_tokens":2,"thinking_tokens":1,"cached_tokens":3,"api_calls":2,"context":{"total_tokens":100,"window_size":1000,"usage_pct":10}}}`)
	got := (MailModel{orchestrator: dir}).gatherHomeTelemetry()
	if got.apiCalls != 2 || got.sessionTokens != 13 {
		t.Fatalf("economy = %+v, want status values rather than ledger values", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Home gather launched sqlite3 or left an unexpected marker: stat err=%v", err)
	}
}

func TestGatherHomeTelemetryMalformedStatusOmitsEconomyWithoutHistoryFallback(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "token_ledger.jsonl"), []byte(`{"input":999,"output":1,"api_calls":99}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeHomeStatus(t, dir, `{"tokens":{"input_tokens":"bad","context":{"window_size":1000,"usage_pct":20}}}`)
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(`{"type":"notification","ts":1782000000,"summary":"sync","meta":{"context":{"usage":0.42}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTelemetryModel(t, dir, dir)
	got := m.gatherHomeTelemetry()
	if got.apiCalls != 0 || got.sessionTokens != 0 || got.inputTokens != 0 || got.cached != 0 {
		t.Fatalf("malformed status economy = %+v, want omitted/zero with no ledger fallback", got)
	}
	if got.contextUsage != 0.20 {
		t.Fatalf("bad economy status contextUsage = %v, want the safely decoded direct status value 0.20", got.contextUsage)
	}
}

func TestGatherHomeTelemetryBadEconomyKeepsDirectStatusContext(t *testing.T) {
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	logsDir := filepath.Join(orchDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(`{"type":"notification","ts":1782000000,"summary":"stale","meta":{"context":{"usage":0.42}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeHomeStatus(t, orchDir, `{"runtime":{"pid":4242,"running":true},"tokens":{"input_tokens":"bad","output_tokens":200,"thinking_tokens":50,"cached_tokens":100,"api_calls":4,"context":{"total_tokens":1234,"window_size":8000,"usage_pct":20}}}`)

	got := newTelemetryModel(t, dir, orchDir).gatherHomeTelemetry()
	if got.apiCalls != 0 || got.sessionTokens != 0 || got.inputTokens != 0 || got.cached != 0 {
		t.Fatalf("economy = %+v, want the whole economy tuple omitted after one bad field", got)
	}
	if got.contextUsage != 0.20 || got.contextUsed != 1234 || got.contextLimit != 8000 {
		t.Fatalf("context = %+v, want direct status context 20%% / 1234 / 8000, not notification fallback 42%%", got)
	}
}

func TestPropsStatusConsumerKeepsContextAfterEconomyTypeError(t *testing.T) {
	dir := t.TempDir()
	writeHomeStatus(t, dir, `{"runtime":{"pid":4242,"running":true},"tokens":{"input_tokens":"bad","output_tokens":200,"thinking_tokens":50,"cached_tokens":100,"api_calls":4,"context":{"total_tokens":1234,"window_size":8000,"usage_pct":20}}}`)
	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), []byte(`{"agent_name":"orchestrator"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	status := fs.ReadStatus(dir)
	rendered := (PropsModel{selectedDir: dir, selectedStatus: status, globalDir: t.TempDir()}).renderLeft(80)
	if !strings.Contains(rendered, "1,234 / 8,000") {
		t.Fatalf("kanban-equivalent rendering = %q, want retained status context", rendered)
	}
}
