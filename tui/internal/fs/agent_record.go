// internal/fs/agent_record.go
package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AgentRecordSchema is the schema string the kernel's session_stats module
// stamps on every Agent record it writes (lingtai-kernel
// src/lingtai/kernel/session_stats/__init__.py AGENT_RECORD_SCHEMA). Per that
// module's contract, a reader MUST treat an unrecognized schema value as
// absent rather than parsing it, so ReadAgentRecord checks this exact string.
const AgentRecordSchema = "lingtai.agent_record/v1"

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

// AgentRecord is the subset of the kernel-published `system/agent_record.json`
// (schema lingtai.agent_record/v1) that Home telemetry consumes. See
// lingtai-kernel src/lingtai/kernel/session_stats/CONTRACT.md "Shapes" for the
// complete record; identity/health/handles/integrations/daemons are read by
// other, not-yet-migrated consumers and are intentionally omitted here.
type AgentRecord struct {
	Schema string           `json:"schema"`
	Model  AgentRecordModel `json:"model"`
	Usage  AgentRecordUsage `json:"usage"`
}

// ReadAgentRecord reads the agent's `system/agent_record.json`. It returns
// ok=false when the file is missing, unreadable, syntactically invalid, or
// carries an unrecognized `schema` — the direct-migration contract requires
// treating all three as an explicit absent record rather than falling back to
// `.status.json`, heartbeat files, or a partial parse. There is no other
// writer or fallback source for this data.
func ReadAgentRecord(dir string) (AgentRecord, bool) {
	var rec AgentRecord
	data, err := os.ReadFile(filepath.Join(dir, agentRecordRelativePath))
	if err != nil {
		return AgentRecord{}, false
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return AgentRecord{}, false
	}
	if rec.Schema != AgentRecordSchema {
		return AgentRecord{}, false
	}
	return rec, true
}
