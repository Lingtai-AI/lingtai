package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func writeHomeAgentRecord(t *testing.T, dir, body string) {
	t.Helper()
	sysDir := filepath.Join(dir, "system")
	if err := os.MkdirAll(sysDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sysDir, "agent_record.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGatherHomeTelemetryUsesOneAgentRecordForEconomyAndContext(t *testing.T) {
	dir := t.TempDir()
	writeHomeAgentRecord(t, dir, `{
  "schema": "lingtai.agent_record/v1",
  "usage": {
    "input_tokens": 1000,
    "output_tokens": 200,
    "thinking_tokens": 50,
    "cached_tokens": 700,
    "api_calls": 4,
    "context_used_tokens": 1234,
    "context_limit_tokens": 8000,
    "context_usage_pct": 15.425
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

func TestGatherHomeTelemetryReadsActiveConfiguredPairFromAgentRecord(t *testing.T) {
	dir := t.TempDir()
	writeHomeAgentRecord(t, dir, `{"schema": "lingtai.agent_record/v1", "model": {"provider": "zhipu", "model": "glm-5.2"}}`)

	got := (MailModel{orchestrator: dir}).gatherHomeTelemetry()
	if got.provider != "zhipu" || got.model != "glm-5.2" {
		t.Fatalf("configured pair = %q/%q, want zhipu/glm-5.2", got.provider, got.model)
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

	writeHomeAgentRecord(t, dir, `{"schema":"lingtai.agent_record/v1","usage":{"input_tokens":10,"output_tokens":2,"thinking_tokens":1,"cached_tokens":3,"api_calls":2,"context_used_tokens":100,"context_limit_tokens":1000,"context_usage_pct":10}}`)
	got := (MailModel{orchestrator: dir}).gatherHomeTelemetry()
	if got.apiCalls != 2 || got.sessionTokens != 13 {
		t.Fatalf("economy = %+v, want agent-record values rather than ledger values", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Home gather launched sqlite3 or left an unexpected marker: stat err=%v", err)
	}
}

// The direct-migration contract (lingtai-kernel session_stats CONTRACT.md)
// requires a corrupt or unreadable Agent record to be surfaced as an explicit
// bounded unavailable state — never partially trusted, never reconstructed
// from another source. A syntactically invalid record therefore omits BOTH the
// economy AND the context segment, unlike the old `.status.json` reader which
// kept safely-decoded sibling fields after a type error on one field.
func TestGatherHomeTelemetryMalformedRecordOmitsEverything(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "token_ledger.jsonl"), []byte(`{"input":999,"output":1,"api_calls":99}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(`{"type":"notification","ts":1782000000,"summary":"sync","meta":{"context":{"usage":0.42}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// input_tokens as a string is a JSON type mismatch against AgentRecordUsage.
	writeHomeAgentRecord(t, dir, `{"schema":"lingtai.agent_record/v1","usage":{"input_tokens":"bad","context_limit_tokens":1000,"context_usage_pct":20}}`)

	m := newTelemetryModel(t, dir, dir)
	got := m.gatherHomeTelemetry()
	if got.apiCalls != 0 || got.sessionTokens != 0 || got.inputTokens != 0 || got.cached != 0 {
		t.Fatalf("malformed record economy = %+v, want all-zero", got)
	}
	if got.contextUsage != -1 || got.contextUsed != 0 || got.contextLimit != 0 {
		t.Fatalf("malformed record context = %+v, want the unavailable sentinel (-1/0/0), no notification/ledger fallback", got)
	}
}

// An unrecognized `schema` value must be treated exactly like a missing
// record — the contract's explicit "treat unrecognized schema as absent"
// rule — never parsed for a best-effort partial read.
func TestGatherHomeTelemetryUnrecognizedSchemaOmitsEverything(t *testing.T) {
	dir := t.TempDir()
	writeHomeAgentRecord(t, dir, `{"schema":"lingtai.agent_record/v2","model":{"provider":"zhipu","model":"glm-5.2"},"usage":{"api_calls":4,"input_tokens":1000,"context_limit_tokens":8000,"context_usage_pct":20}}`)

	got := (MailModel{orchestrator: dir}).gatherHomeTelemetry()
	if got.provider != "" || got.model != "" || got.apiCalls != 0 || got.contextUsage != -1 {
		t.Fatalf("unrecognized-schema record = %+v, want fully omitted", got)
	}
}

// Missing orchestrator dir / no agent_record.json at all is the ordinary
// "never booted" / "record not yet written" case: the row stays hidden rather
// than falling back to the manifest for a configuration-only display.
func TestGatherHomeTelemetryNoRecordOmitsEverything(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "init.json"), []byte(`{"manifest":{"llm":{"provider":"zhipu","model":"glm-5.2"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := (MailModel{orchestrator: dir}).gatherHomeTelemetry()
	if got.hasData() {
		t.Fatalf("telemetry = %+v, want no data when no agent_record.json exists yet (no manifest fallback)", got)
	}
}

// newTelemetryModel builds a MailModel rooted at dir/orch and drives the deferred
// initial rebuild so the session cache is populated — the normal launch path.
func newTelemetryModel(t *testing.T, dir, orchDir string) MailModel {
	t.Helper()
	humanDir := filepath.Join(dir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewMailModel(humanDir, "human@local", "~", orchDir, "TestOrch", 50, dir, "en", false, 0)
	m, _ = m.Update(m.initialRebuild())
	return m
}

// TestPropsStatusConsumerKeepsContextAfterEconomyTypeError pins /kanban's own
// (unmigrated) `.status.json` consumer, which is out of scope for the Home
// telemetry Agent Record migration — PropsModel still reads fs.ReadStatus
// directly. It lives here only for historical proximity to the old
// status-based Home tests; it exercises props.go, not home_telemetry.go.
func TestPropsStatusConsumerKeepsContextAfterEconomyTypeError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".status.json"),
		[]byte(`{"runtime":{"pid":4242,"running":true},"tokens":{"input_tokens":"bad","output_tokens":200,"thinking_tokens":50,"cached_tokens":100,"api_calls":4,"context":{"total_tokens":1234,"window_size":8000,"usage_pct":20}}}`),
		0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), []byte(`{"agent_name":"orchestrator"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	status := fs.ReadStatus(dir)
	rendered := (PropsModel{selectedDir: dir, selectedStatus: status, globalDir: t.TempDir()}).renderLeft(80)
	if !strings.Contains(rendered, "1,234 / 8,000") {
		t.Fatalf("kanban-equivalent rendering = %q, want retained status context", rendered)
	}
}
