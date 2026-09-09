// internal/fs/agent_record_test.go
package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func validAsyncWorkChild(generatedAt time.Time) string {
	return fmt.Sprintf(`{
  "schema":"lingtai.async_work/v1","schema_version":1,
  "generated_at":%q,"window_seconds":600,
  "running":3,"queued":2,"done":12,"failed":1,
  "daemon":{
    "running":1,"queued":0,"done":4,"failed":0,
    "backend_counts":{"lingtai":4,"claude-code":1},
    "model_counts":{"glm-5.2":3,"gpt-5.6/terra":1},
    "usage":{"input_tokens":1200,"output_tokens":300,"thinking_tokens":40,"cached_tokens":600,"api_calls":7}
  },
  "shell":{"running":2,"queued":2,"done":8,"failed":1}
}`, generatedAt.UTC().Format(time.RFC3339Nano))
}

func agentRecordWithAsyncWork(child string) string {
	return fmt.Sprintf(`{
  "schema":"lingtai.agent_record/v1","schema_version":1,
  "model":{"provider":"zhipu","model":"glm-5.2"},
  "usage":{"api_calls":9,"input_tokens":99},
  "async_work":%s
}`, child)
}

func readAsyncWork(t *testing.T, child string, now time.Time) (AsyncWorkSnapshot, bool) {
	t.Helper()
	dir := t.TempDir()
	writeAgentRecord(t, dir, agentRecordWithAsyncWork(child))
	rec, ok := ReadAgentRecord(dir)
	if !ok {
		t.Fatal("valid top-level Agent Record was rejected")
	}
	return rec.FreshAsyncWork(now)
}

func TestFreshAsyncWorkValidMixedSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	got, ok := readAsyncWork(t, validAsyncWorkChild(now.Add(-time.Minute)), now)
	if !ok {
		t.Fatal("valid fresh mixed async_work child was rejected")
	}
	if got.Counts != (AsyncWorkCounts{Running: 3, Queued: 2, Done: 12, Failed: 1}) {
		t.Fatalf("aggregate = %+v", got.Counts)
	}
	if got.Daemon.Counts != (AsyncWorkCounts{Running: 1, Done: 4}) ||
		got.Shell.Counts != (AsyncWorkCounts{Running: 2, Queued: 2, Done: 8, Failed: 1}) {
		t.Fatalf("lanes = daemon:%+v shell:%+v", got.Daemon.Counts, got.Shell.Counts)
	}
	if got.Daemon.Usage != (AsyncWorkUsage{InputTokens: 1200, OutputTokens: 300, ThinkingTokens: 40, CachedTokens: 600, APICalls: 7}) {
		t.Fatalf("daemon usage = %+v", got.Daemon.Usage)
	}
	if got.Daemon.BackendCounts["lingtai"] != 4 || got.Daemon.ModelCounts["glm-5.2"] != 3 {
		t.Fatalf("daemon details = backends:%v models:%v", got.Daemon.BackendCounts, got.Daemon.ModelCounts)
	}
}

func TestFreshAsyncWorkExactAgeBoundary(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	if _, ok := readAsyncWork(t, validAsyncWorkChild(now.Add(-600*time.Second)), now); !ok {
		t.Fatal("snapshot exactly 600 seconds old must remain valid")
	}
}

func TestFreshAsyncWorkAcceptsStrictRFC3339Forms(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	base := validAsyncWorkChild(now)
	baseTimestamp := now.Format(time.RFC3339Nano)
	for _, timestamp := range []string{
		"2026-09-08T11:59:59.123456789Z",
		"2026-09-08T13:59:00.25+02:00",
	} {
		t.Run(timestamp, func(t *testing.T) {
			child := strings.Replace(base, baseTimestamp, timestamp, 1)
			if _, ok := readAsyncWork(t, child, now); !ok {
				t.Fatalf("valid RFC3339 timestamp %q was rejected", timestamp)
			}
		})
	}
}

func TestFreshAsyncWorkRejectsInvalidChildren(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	valid := validAsyncWorkChild(now)
	overflowingLane := strings.NewReplacer(
		`"running":3`, `"running":9223372036854775807`,
		`"daemon":{
    "running":1`, `"daemon":{
    "running":9223372036854775807`,
	).Replace(valid)
	var overflowCheck struct {
		Daemon struct {
			Running int64 `json:"running"`
		} `json:"daemon"`
	}
	if err := json.Unmarshal([]byte(overflowingLane), &overflowCheck); err != nil || overflowCheck.Daemon.Running != 9223372036854775807 {
		t.Fatalf("overflow fixture did not set daemon running to MaxInt64: value=%d err=%v", overflowCheck.Daemon.Running, err)
	}
	duplicateEmptyShell := strings.TrimSuffix(valid, "\n}") + ",\n  \"shell\":{}\n}"
	baseTimestamp := now.Format(time.RFC3339Nano)
	commaTimestamp := now.Format("2006-01-02T15:04:05") + ",000Z"
	invalidOffsetTimestamp := now.Add(24*time.Hour).Format("2006-01-02T15:04:05") + "+24:00"
	tests := []struct {
		name  string
		child string
	}{
		{"missing child", "__MISSING__"},
		{"null child", "null"},
		{"wrong shape", `[]`},
		{"malformed child", `{`},
		{"future schema", strings.Replace(valid, `lingtai.async_work/v1`, `lingtai.async_work/v2`, 1)},
		{"future version", strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1)},
		{"wrong window", strings.Replace(valid, `"window_seconds":600`, `"window_seconds":601`, 1)},
		{"capitalized required count", strings.Replace(valid, `"running":3`, `"Running":3`, 1)},
		{"duplicate empty shell lane", duplicateEmptyShell},
		{"negative count", strings.Replace(valid, `"running":3`, `"running":-1`, 1)},
		{"null zero-valued daemon count", strings.Replace(valid, `"queued":0`, `"queued":null`, 1)},
		{"fractional count", strings.Replace(valid, `"running":3`, `"running":3.5`, 1)},
		{"boolean count", strings.Replace(valid, `"running":3`, `"running":true`, 1)},
		{"aggregate mismatch", strings.Replace(valid, `"running":3`, `"running":4`, 1)},
		{"missing daemon usage", strings.Replace(valid, `"usage":`, `"usage_missing":`, 1)},
		{"negative daemon usage", strings.Replace(valid, `"input_tokens":1200`, `"input_tokens":-1`, 1)},
		{"null daemon usage", strings.Replace(valid, `"input_tokens":1200`, `"input_tokens":null`, 1)},
		{"unsafe model name", strings.Replace(valid, `glm-5.2`, "bad\\nmodel", 1)},
		{"zero model count", strings.Replace(valid, `"glm-5.2":3`, `"glm-5.2":0`, 1)},
		{"invalid timestamp", strings.Replace(valid, baseTimestamp, `not-a-time`, 1)},
		{"comma fractional timestamp", strings.Replace(valid, baseTimestamp, commaTimestamp, 1)},
		{"invalid 24-hour offset", strings.Replace(valid, baseTimestamp, invalidOffsetTimestamp, 1)},
		{"future timestamp", validAsyncWorkChild(now.Add(time.Nanosecond))},
		{"stale timestamp", validAsyncWorkChild(now.Add(-600*time.Second - time.Nanosecond))},
		{"overflowing lane sum", overflowingLane},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			body := agentRecordWithAsyncWork(tc.child)
			if tc.child == "__MISSING__" {
				body = `{"schema":"lingtai.agent_record/v1","schema_version":1,"model":{"model":"still-visible"},"usage":{"api_calls":5}}`
			}
			writeAgentRecord(t, dir, body)
			rec, topOK := ReadAgentRecord(dir)
			if tc.name == "malformed child" {
				// An actually truncated child also truncates the enclosing document;
				// this is invalid top-level JSON, not merely an invalid optional value.
				if topOK {
					t.Fatal("truncated document unexpectedly parsed")
				}
				return
			}
			if !topOK {
				t.Fatal("invalid optional child poisoned the valid top-level record")
			}
			if _, ok := rec.FreshAsyncWork(now); ok {
				t.Fatal("invalid async_work child was accepted")
			}
		})
	}
}

func TestMalformedAsyncWorkDoesNotPoisonTopLevelTelemetry(t *testing.T) {
	dir := t.TempDir()
	writeAgentRecord(t, dir, agentRecordWithAsyncWork(`{"schema":17,"daemon":"bad"}`))
	rec, ok := ReadAgentRecord(dir)
	if !ok {
		t.Fatal("malformed optional child poisoned top-level Agent Record")
	}
	if rec.Model.Model != "glm-5.2" || rec.Usage.APICalls != 9 {
		t.Fatalf("top-level telemetry was lost: model=%+v usage=%+v", rec.Model, rec.Usage)
	}
	if _, ok := rec.FreshAsyncWork(time.Now()); ok {
		t.Fatal("malformed optional child was accepted")
	}
}

func TestCapitalizedTopSchemaVersionOnlyDisablesAsyncWork(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	body := strings.Replace(agentRecordWithAsyncWork(validAsyncWorkChild(now)), `"schema_version":1`, `"Schema_Version":1`, 1)
	writeAgentRecord(t, dir, body)
	rec, ok := ReadAgentRecord(dir)
	if !ok || rec.Model.Model != "glm-5.2" || rec.Usage.APICalls != 9 {
		t.Fatal("capitalized top schema_version poisoned valid top-level telemetry")
	}
	if _, ok := rec.FreshAsyncWork(now); ok {
		t.Fatal("async_work accepted without canonical top-level schema_version")
	}
}

func TestMalformedTopSchemaVersionOnlyDisablesAsyncWork(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	body := strings.Replace(agentRecordWithAsyncWork(validAsyncWorkChild(now)), `"schema_version":1`, `"schema_version":"one"`, 1)
	writeAgentRecord(t, dir, body)
	rec, ok := ReadAgentRecord(dir)
	if !ok || rec.Model.Model != "glm-5.2" {
		t.Fatal("top-level schema_version shape poisoned valid top-level telemetry")
	}
	if _, ok := rec.FreshAsyncWork(now); ok {
		t.Fatal("async_work accepted under malformed top-level schema_version")
	}
}
