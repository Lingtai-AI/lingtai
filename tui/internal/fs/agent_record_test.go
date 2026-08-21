// internal/fs/agent_record_test.go
package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAgentRecord(t *testing.T, dir, body string) {
	t.Helper()
	sysDir := filepath.Join(dir, "system")
	if err := os.MkdirAll(sysDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sysDir, "agent_record.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadAgentRecordParsesModelAndUsage(t *testing.T) {
	dir := t.TempDir()
	writeAgentRecord(t, dir, `{
  "schema": "lingtai.agent_record/v1",
  "schema_version": 1,
  "generated_at": "2026-08-20T00:00:00Z",
  "model": {"provider": "zhipu", "model": "glm-5.2", "context_limit": 250000, "base_url": "https://example"},
  "usage": {
    "api_calls": 42,
    "input_tokens": 181585,
    "output_tokens": 5000,
    "thinking_tokens": 100,
    "cached_tokens": 180224,
    "context_used_tokens": 146000,
    "context_limit_tokens": 200000,
    "context_usage_pct": 73.0
  }
}`)

	rec, ok := ReadAgentRecord(dir)
	if !ok {
		t.Fatal("ReadAgentRecord reported not-ok for a valid record")
	}
	if rec.Model.Provider != "zhipu" || rec.Model.Model != "glm-5.2" || rec.Model.ContextLimit != 250000 {
		t.Fatalf("model = %+v, want zhipu/glm-5.2/250000", rec.Model)
	}
	if rec.Usage.APICalls != 42 || rec.Usage.InputTokens != 181585 || rec.Usage.OutputTokens != 5000 ||
		rec.Usage.ThinkingTokens != 100 || rec.Usage.CachedTokens != 180224 {
		t.Fatalf("usage economy = %+v, unexpected", rec.Usage)
	}
	if rec.Usage.ContextUsedTokens != 146000 || rec.Usage.ContextLimitTokens != 200000 || rec.Usage.ContextUsagePct != 73.0 {
		t.Fatalf("usage context = %+v, want used=146000 limit=200000 pct=73.0", rec.Usage)
	}
}

func TestReadAgentRecordNullContextFieldsDecodeAsZero(t *testing.T) {
	dir := t.TempDir()
	// The kernel publishes context_limit_tokens/context_usage_pct as JSON null
	// when no context window is known yet (agent._chat not initialized). A
	// reader must treat that as the same "0/unknown" sentinel used elsewhere.
	writeAgentRecord(t, dir, `{
  "schema": "lingtai.agent_record/v1",
  "model": {"provider": "zhipu", "model": "glm-5.2"},
  "usage": {
    "api_calls": 1, "input_tokens": 10, "output_tokens": 1, "thinking_tokens": 0, "cached_tokens": 0,
    "context_used_tokens": 0, "context_limit_tokens": null, "context_usage_pct": null
  }
}`)

	rec, ok := ReadAgentRecord(dir)
	if !ok {
		t.Fatal("ReadAgentRecord reported not-ok for a valid record with null context fields")
	}
	if rec.Usage.ContextLimitTokens != 0 || rec.Usage.ContextUsagePct != 0 {
		t.Fatalf("null context fields = %+v, want zero", rec.Usage)
	}
}

func TestReadAgentRecordMissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadAgentRecord(dir); ok {
		t.Fatal("ReadAgentRecord reported ok with no system/agent_record.json present")
	}
}

func TestReadAgentRecordMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	writeAgentRecord(t, dir, `{not valid json`)
	if _, ok := ReadAgentRecord(dir); ok {
		t.Fatal("ReadAgentRecord reported ok for malformed JSON")
	}
}

func TestReadAgentRecordUnrecognizedSchemaTreatedAsAbsent(t *testing.T) {
	dir := t.TempDir()
	// The contract requires an unrecognized schema to be treated as absent
	// rather than parsed — future/unknown record versions must not silently
	// mix into today's field expectations.
	writeAgentRecord(t, dir, `{"schema": "lingtai.agent_record/v2", "model": {"provider": "zhipu", "model": "glm-5.2"}}`)
	if _, ok := ReadAgentRecord(dir); ok {
		t.Fatal("ReadAgentRecord reported ok for an unrecognized schema value")
	}
}

func TestReadAgentRecordMissingSchemaTreatedAsAbsent(t *testing.T) {
	dir := t.TempDir()
	writeAgentRecord(t, dir, `{"model": {"provider": "zhipu", "model": "glm-5.2"}}`)
	if _, ok := ReadAgentRecord(dir); ok {
		t.Fatal("ReadAgentRecord reported ok for a record with no schema field")
	}
}
