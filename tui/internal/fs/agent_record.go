// internal/fs/agent_record.go
package fs

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// AgentRecordSchema is the schema string the kernel's session_stats module
// stamps on every Agent record it writes (lingtai-kernel
// src/lingtai/kernel/session_stats/__init__.py AGENT_RECORD_SCHEMA). Per that
// module's contract, a reader MUST treat an unrecognized schema value as
// absent rather than parsing it, so ReadAgentRecord checks this exact string.
const AgentRecordSchema = "lingtai.agent_record/v1"

const (
	agentRecordSchemaVersion int64 = 1
	asyncWorkSchema                = "lingtai.async_work/v1"
	asyncWorkSchemaVersion   int64 = 1
	asyncWorkWindowSeconds   int64 = 600
)

// agentRecordRelativePath is the agent-directory-relative path the kernel
// atomically publishes the Agent record to (session_stats.AGENT_RECORD_RELATIVE_PATH).
const agentRecordRelativePath = "system/agent_record.json"

// AgentRecordModel is the redacted, safelisted `model` block of the Agent
// record — the same shape base_agent/identity.py's _safe_llm_from_service
// produces. Only the fields Home telemetry needs are decoded; unknown keys
// (base_url, api_compat, service_tier) are ignored.
type AgentRecordModel struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	ContextLimit int64  `json:"context_limit"`
}

// AgentRecordUsage is the `usage` block of the Agent record: current-session
// (since the latest molt) token economy plus live context-window pressure.
// A field the kernel could not compute (e.g. context_limit_tokens/
// context_usage_pct when no context window is known) is published as JSON
// null, which decodes to the Go zero value here — the same "0/unknown"
// sentinel convention the former .status.json reader used.
type AgentRecordUsage struct {
	APICalls           int64   `json:"api_calls"`
	InputTokens        int64   `json:"input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	ThinkingTokens     int64   `json:"thinking_tokens"`
	CachedTokens       int64   `json:"cached_tokens"`
	ContextUsedTokens  int64   `json:"context_used_tokens"`
	ContextLimitTokens int64   `json:"context_limit_tokens"`
	ContextUsagePct    float64 `json:"context_usage_pct"`
}

// AsyncWorkCounts is one exact four-state count set in a published async-work
// snapshot. Values are validated as nonnegative signed 64-bit integers before
// this type is returned to a caller.
type AsyncWorkCounts struct {
	Running int64
	Queued  int64
	Done    int64
	Failed  int64
}

// AsyncWorkUsage is daemon-scoped usage from the snapshot's daemon lane.
type AsyncWorkUsage struct {
	InputTokens    int64
	OutputTokens   int64
	ThinkingTokens int64
	CachedTokens   int64
	APICalls       int64
}

// AsyncWorkLane is one validated provider lane. BackendCounts, ModelCounts, and
// Usage are populated only for the daemon lane; Shell carries counts only.
type AsyncWorkLane struct {
	Counts        AsyncWorkCounts
	BackendCounts map[string]int64
	ModelCounts   map[string]int64
	Usage         AsyncWorkUsage
}

// AsyncWorkSnapshot is the strict, fresh `lingtai.async_work/v1` child view.
type AsyncWorkSnapshot struct {
	GeneratedAt time.Time
	Counts      AsyncWorkCounts
	Daemon      AsyncWorkLane
	Shell       AsyncWorkLane
}

// AgentRecord is the subset of the kernel-published `system/agent_record.json`
// (schema lingtai.agent_record/v1) that Home consumes. AsyncWork is retained as
// raw JSON so a malformed optional child cannot poison the already-valid model
// and usage telemetry. FreshAsyncWork is the sole strict child decoder.
type AgentRecord struct {
	Schema        string           `json:"schema"`
	SchemaVersion json.RawMessage  `json:"schema_version"`
	Model         AgentRecordModel `json:"model"`
	Usage         AgentRecordUsage `json:"usage"`
	AsyncWork     json.RawMessage  `json:"async_work"`
}

// ReadAgentRecord reads the agent's `system/agent_record.json`. It returns
// ok=false when the file is missing, unreadable, syntactically invalid, or
// carries an unrecognized top-level `schema`. The optional async_work child is
// retained raw and validated separately, so its absence or malformed shape does
// not hide otherwise-valid Home session telemetry.
func ReadAgentRecord(dir string) (AgentRecord, bool) {
	data, err := os.ReadFile(filepath.Join(dir, agentRecordRelativePath))
	if err != nil {
		return AgentRecord{}, false
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return AgentRecord{}, false
	}
	schema, ok := decodeJSONStringField(object, "schema")
	if !ok || schema != AgentRecordSchema {
		return AgentRecord{}, false
	}

	var rec AgentRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return AgentRecord{}, false
	}
	// These fields gate the optional async-work child, so source them through
	// exact map keys rather than encoding/json's case-insensitive struct match.
	rec.Schema = schema
	rec.SchemaVersion = append(json.RawMessage(nil), object["schema_version"]...)
	rec.AsyncWork = append(json.RawMessage(nil), object["async_work"]...)
	return rec, true
}

var strictRFC3339Pattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)

// FreshAsyncWork validates and decodes the optional Agent Record child using
// the kernel-published contract. It fails closed on every shape/value/version/
// freshness mismatch. Exact age 600 seconds is valid; future timestamps and
// older snapshots are unavailable.
func (r AgentRecord) FreshAsyncWork(now time.Time) (AsyncWorkSnapshot, bool) {
	if r.Schema != AgentRecordSchema || !rawExactInt(r.SchemaVersion, agentRecordSchemaVersion) || len(r.AsyncWork) == 0 {
		return AsyncWorkSnapshot{}, false
	}

	object, ok := decodeJSONObject(r.AsyncWork)
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	schema, ok := decodeJSONStringField(object, "schema")
	if !ok || schema != asyncWorkSchema {
		return AsyncWorkSnapshot{}, false
	}
	schemaVersion, ok := decodeInt64Field(object, "schema_version")
	if !ok || schemaVersion != asyncWorkSchemaVersion {
		return AsyncWorkSnapshot{}, false
	}
	windowSeconds, ok := decodeInt64Field(object, "window_seconds")
	if !ok || windowSeconds != asyncWorkWindowSeconds {
		return AsyncWorkSnapshot{}, false
	}
	generatedAtText, ok := decodeJSONStringField(object, "generated_at")
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	generatedAt, ok := parseStrictRFC3339(generatedAtText)
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	age := now.Sub(generatedAt)
	if age < 0 || age > time.Duration(asyncWorkWindowSeconds)*time.Second {
		return AsyncWorkSnapshot{}, false
	}

	daemonObject, ok := decodeJSONObjectField(object, "daemon")
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	shellObject, ok := decodeJSONObjectField(object, "shell")
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	aggregate, ok := strictAsyncWorkCounts(object)
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	daemonCounts, ok := strictAsyncWorkCounts(daemonObject)
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	shellCounts, ok := strictAsyncWorkCounts(shellObject)
	if !ok || !countsSumTo(aggregate, daemonCounts, shellCounts) {
		return AsyncWorkSnapshot{}, false
	}

	usageObject, ok := decodeJSONObjectField(daemonObject, "usage")
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	usage, ok := strictAsyncWorkUsage(usageObject)
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	backendRaw, ok := decodeInt64MapField(daemonObject, "backend_counts")
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	backendCounts, ok := strictAsyncWorkDetails(backendRaw, 48)
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	modelRaw, ok := decodeInt64MapField(daemonObject, "model_counts")
	if !ok {
		return AsyncWorkSnapshot{}, false
	}
	modelCounts, ok := strictAsyncWorkDetails(modelRaw, 128)
	if !ok {
		return AsyncWorkSnapshot{}, false
	}

	return AsyncWorkSnapshot{
		GeneratedAt: generatedAt,
		Counts:      aggregate,
		Daemon: AsyncWorkLane{
			Counts:        daemonCounts,
			BackendCounts: backendCounts,
			ModelCounts:   modelCounts,
			Usage:         usage,
		},
		Shell: AsyncWorkLane{Counts: shellCounts},
	}, true
}

func decodeJSONObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, false
	}
	return object, true
}

func decodeJSONObjectField(object map[string]json.RawMessage, key string) (map[string]json.RawMessage, bool) {
	raw, ok := object[key]
	if !ok {
		return nil, false
	}
	return decodeJSONObject(raw)
}

func decodeJSONStringField(object map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := object[key]
	if !ok {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func decodeInt64Field(object map[string]json.RawMessage, key string) (int64, bool) {
	raw, ok := object[key]
	if !ok {
		return 0, false
	}
	var value *int64
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return 0, false
	}
	return *value, true
}

func decodeInt64MapField(object map[string]json.RawMessage, key string) (map[string]int64, bool) {
	raw, ok := object[key]
	if !ok {
		return nil, true
	}
	var value map[string]int64
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false
	}
	return value, true
}

func parseStrictRFC3339(value string) (time.Time, bool) {
	if !strictRFC3339Pattern.MatchString(value) {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func rawExactInt(raw json.RawMessage, want int64) bool {
	var value int64
	return len(raw) > 0 && json.Unmarshal(raw, &value) == nil && value == want
}

func strictAsyncWorkCounts(object map[string]json.RawMessage) (AsyncWorkCounts, bool) {
	running, runningOK := decodeInt64Field(object, "running")
	queued, queuedOK := decodeInt64Field(object, "queued")
	done, doneOK := decodeInt64Field(object, "done")
	failed, failedOK := decodeInt64Field(object, "failed")
	if !runningOK || !queuedOK || !doneOK || !failedOK || running < 0 || queued < 0 || done < 0 || failed < 0 {
		return AsyncWorkCounts{}, false
	}
	return AsyncWorkCounts{Running: running, Queued: queued, Done: done, Failed: failed}, true
}

func strictAsyncWorkUsage(object map[string]json.RawMessage) (AsyncWorkUsage, bool) {
	inputTokens, inputOK := decodeInt64Field(object, "input_tokens")
	outputTokens, outputOK := decodeInt64Field(object, "output_tokens")
	thinkingTokens, thinkingOK := decodeInt64Field(object, "thinking_tokens")
	cachedTokens, cachedOK := decodeInt64Field(object, "cached_tokens")
	apiCalls, callsOK := decodeInt64Field(object, "api_calls")
	if !inputOK || !outputOK || !thinkingOK || !cachedOK || !callsOK ||
		inputTokens < 0 || outputTokens < 0 || thinkingTokens < 0 || cachedTokens < 0 || apiCalls < 0 {
		return AsyncWorkUsage{}, false
	}
	return AsyncWorkUsage{
		InputTokens:    inputTokens,
		OutputTokens:   outputTokens,
		ThinkingTokens: thinkingTokens,
		CachedTokens:   cachedTokens,
		APICalls:       apiCalls,
	}, true
}

func strictAsyncWorkDetails(raw map[string]int64, limit int) (map[string]int64, bool) {
	if raw == nil {
		return nil, true
	}
	clean := make(map[string]int64, len(raw))
	for name, count := range raw {
		normalized := strings.TrimSpace(name)
		if count <= 0 || !safeMachineIdentifier(normalized, limit) {
			return nil, false
		}
		clean[normalized] = count
	}
	return clean, true
}

func safeMachineIdentifier(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for _, r := range value {
		if r > 127 || !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:/\\-", r)) {
			return false
		}
	}
	return true
}

func countsSumTo(total, daemon, shell AsyncWorkCounts) bool {
	return sumEquals(total.Running, daemon.Running, shell.Running) &&
		sumEquals(total.Queued, daemon.Queued, shell.Queued) &&
		sumEquals(total.Done, daemon.Done, shell.Done) &&
		sumEquals(total.Failed, daemon.Failed, shell.Failed)
}

func sumEquals(total, left, right int64) bool {
	return left <= math.MaxInt64-right && total == left+right
}
