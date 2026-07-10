// internal/fs/agent.go
package fs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/lingtai-tui/internal/sqlitelog"
)

// agentManifest is the raw JSON shape of .agent.json.
type agentManifest struct {
	AgentName string           `json:"agent_name"`
	Nickname  string           `json:"nickname"`
	Address   string           `json:"address"`
	State     string           `json:"state"`
	Admin     *json.RawMessage `json:"admin,omitempty"`
	// Capabilities can be []string (from TUI-generated) or [][]interface{} (from live agent).
	// We don't need to parse it — just ignore unknown shapes.
	Capabilities json.RawMessage `json:"capabilities"`
	Location     *Location       `json:"location,omitempty"`
}

// ReadAgent reads .agent.json from dir and returns an AgentNode.
func ReadAgent(dir string) (AgentNode, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".agent.json"))
	if err != nil {
		return AgentNode{}, fmt.Errorf("read manifest: %w", err)
	}

	var m agentManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return AgentNode{}, fmt.Errorf("parse manifest: %w", err)
	}

	// is_human: true when admin is JSON null or key is absent entirely
	isHuman := m.Admin == nil || string(*m.Admin) == "null"

	// Parse capabilities from either []string or [["name", {}], ...] format
	caps := ParseCapabilities(m.Capabilities)

	return AgentNode{
		Address:      m.Address,
		AgentName:    m.AgentName,
		Nickname:     m.Nickname,
		State:        m.State,
		IsHuman:      isHuman,
		Capabilities: caps,
		Location:     m.Location, // nil if absent from JSON
		WorkingDir:   dir,
	}, nil
}

// ParseCapabilities handles both []string and [][]interface{} formats.
func ParseCapabilities(raw json.RawMessage) []string {
	if raw == nil {
		return nil
	}
	// Try []string first
	var simple []string
	if err := json.Unmarshal(raw, &simple); err == nil {
		return simple
	}
	// Try [["name", {}], ...] (tuple format from live agent)
	var tuples []json.RawMessage
	if err := json.Unmarshal(raw, &tuples); err == nil {
		var names []string
		for _, t := range tuples {
			var pair []json.RawMessage
			if err := json.Unmarshal(t, &pair); err == nil && len(pair) > 0 {
				var name string
				if err := json.Unmarshal(pair[0], &name); err == nil {
					names = append(names, name)
				}
			}
		}
		return names
	}
	return nil
}

// intrinsicCapabilities are the agent capabilities that always exist on a
// live agent (the kernel wires them in unconditionally) but are not listed
// in .agent.json's `capabilities` field. The kanban/props view should still
// present them so the operator sees the complete capability surface.
var intrinsicCapabilities = []string{"system", "soul", "email", "psyche"}

// CapabilitiesForDisplay returns the operator-visible capability list:
// the intrinsic capabilities first, followed by the manifest capabilities in
// their original order, with duplicates removed. Manifest entries that
// duplicate an intrinsic are dropped (the intrinsic keeps its leading slot).
func CapabilitiesForDisplay(manifest []string) []string {
	out := make([]string, 0, len(intrinsicCapabilities)+len(manifest))
	seen := make(map[string]bool, len(intrinsicCapabilities)+len(manifest))
	for _, c := range intrinsicCapabilities {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, c := range manifest {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// ReadInitManifest returns the agent's manifest fields with the llm
// sub-object (model, provider, base_url) and soul.delay flattened to top
// level. It prefers the kernel-published resolved-manifest artifact
// (system/manifest.resolved.json — preset materialized, validated,
// secret-redacted; kernel issue #259) and falls back to raw init.json when
// the artifact is absent or malformed (stopped / never-booted agents).
func ReadInitManifest(dir string) (map[string]interface{}, error) {
	manifest, err := readResolvedInitManifest(dir)
	if err != nil {
		manifest, err = readRawInitManifest(dir)
		if err != nil {
			return nil, err
		}
	}
	flattenInitManifest(manifest)
	return manifest, nil
}

func readResolvedInitManifest(dir string) (map[string]interface{}, error) {
	artifactPath := filepath.Join(dir, "system", "manifest.resolved.json")
	if isResolvedManifestStale(filepath.Join(dir, "init.json"), artifactPath) {
		return nil, fmt.Errorf("manifest.resolved.json is older than init.json")
	}

	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest.resolved.json: %w", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse manifest.resolved.json: %w", err)
	}
	if raw["schema"] != "lingtai.manifest.resolved/v1" {
		return nil, fmt.Errorf("unsupported manifest.resolved.json schema")
	}
	if version, ok := raw["schema_version"].(float64); !ok || version != 1 {
		return nil, fmt.Errorf("unsupported manifest.resolved.json schema_version")
	}
	if raw["source"] != "kernel" {
		return nil, fmt.Errorf("unsupported manifest.resolved.json source")
	}
	manifest, ok := raw["manifest"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no manifest in manifest.resolved.json")
	}
	return manifest, nil
}

func isResolvedManifestStale(initPath, artifactPath string) bool {
	initInfo, err := os.Stat(initPath)
	if err != nil {
		return false
	}
	artifactInfo, err := os.Stat(artifactPath)
	if err != nil {
		return false
	}
	return initInfo.ModTime().After(artifactInfo.ModTime())
}

func readRawInitManifest(dir string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filepath.Join(dir, "init.json"))
	if err != nil {
		return nil, fmt.Errorf("read init.json: %w", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse init.json: %w", err)
	}
	manifest, ok := raw["manifest"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no manifest in init.json")
	}
	return manifest, nil
}

func flattenInitManifest(manifest map[string]interface{}) {
	// Flatten llm sub-object into top level
	if llm, ok := manifest["llm"].(map[string]interface{}); ok {
		for _, key := range []string{"model", "provider", "base_url", "api_compat", "api_key_env"} {
			if v, ok := llm[key]; ok && v != nil {
				manifest[key] = v
			}
		}
	}
	// Flatten soul.delay into soul_delay
	if soul, ok := manifest["soul"].(map[string]interface{}); ok {
		if v, ok := soul["delay"]; ok {
			manifest["soul_delay"] = v
		}
	}
}

// WritePrompt writes a .prompt signal file to inject a [system] text input message.
// The agent's heartbeat loop picks this up and calls agent.send(content, sender="system").
func WritePrompt(agentDir, content string) error {
	return os.WriteFile(filepath.Join(agentDir, ".prompt"), []byte(content), 0o644)
}

// WriteInquiry writes a .inquiry signal file to trigger soul.inquiry.
// No-op if .inquiry or .inquiry.taken already exists (one at a time).
// Format: first line is source ("human", "insight"), rest is question.
func WriteInquiry(agentDir, source, question string) error {
	inquiryPath := filepath.Join(agentDir, ".inquiry")
	takenPath := filepath.Join(agentDir, ".inquiry.taken")
	if _, err := os.Stat(inquiryPath); err == nil {
		return nil // already pending
	}
	if _, err := os.Stat(takenPath); err == nil {
		return nil // already being processed
	}
	content := source + "\n" + question
	return os.WriteFile(inquiryPath, []byte(content), 0o644)
}

// ReadAgentRaw reads .agent.json from dir and returns the full JSON as an ordered map.
func ReadAgentRaw(dir string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".agent.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return raw, nil
}

// DiscoverAgents scans baseDir for subdirectories with .agent.json manifests.
func DiscoverAgents(baseDir string) ([]AgentNode, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("read base dir: %w", err)
	}

	var nodes []AgentNode
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentDir := filepath.Join(baseDir, entry.Name())
		node, err := ReadAgent(agentDir)
		if err != nil {
			continue // skip non-agent dirs
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// AgentStatus holds live runtime status from .status.json (same as system("show")).
type AgentStatus struct {
	Tokens struct {
		Estimated bool `json:"estimated"`
		Context   struct {
			SystemTokens  int     `json:"system_tokens"`
			ToolsTokens   int     `json:"tools_tokens"`
			HistoryTokens int     `json:"history_tokens"`
			TotalTokens   int     `json:"total_tokens"`
			WindowSize    int     `json:"window_size"`
			UsagePct      float64 `json:"usage_pct"`
		} `json:"context"`
	} `json:"tokens"`
	Runtime struct {
		PID           int     `json:"pid"`
		Running       bool    `json:"running"`
		UptimeSeconds float64 `json:"uptime_seconds"`
	} `json:"runtime"`
}

// ContextToolCount is a stable per-tool summary from the current chat history.
type ContextToolCount struct {
	Name    string
	Calls   int
	Results int
}

// ContextStats summarizes the currently retained chat context from
// history/chat_history.jsonl. It counts structural message/block types rather
// than tokens; token budget remains in AgentStatus.Tokens.Context.
type ContextStats struct {
	Entries           int
	SystemMessages    int
	AssistantMessages int
	UserMessages      int
	TextInputs        int
	TextOutputs       int
	ToolCalls         int
	ToolResults       int
	ToolCounts        []ContextToolCount
}

// ReadStatus reads .status.json from an agent directory.
// Returns zero struct if missing or unreadable.
func ReadStatus(dir string) AgentStatus {
	var s AgentStatus
	data, err := os.ReadFile(filepath.Join(dir, ".status.json"))
	if err != nil {
		return s
	}
	json.Unmarshal(data, &s)
	return s
}

// ReadContextStats reads the agent's retained chat history and returns a
// structural summary for diagnostics. Missing/unreadable/malformed rows are
// treated as empty/partial data so the kanban detail view remains best-effort.
func ReadContextStats(dir string) ContextStats {
	var stats ContextStats
	callCounts := map[string]int{}
	resultCounts := map[string]int{}
	_ = forEachJSONLLine(filepath.Join(dir, "history", "chat_history.jsonl"), func(line []byte) {
		var entry struct {
			Role    string          `json:"role"`
			System  string          `json:"system"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			return
		}

		stats.Entries++
		switch entry.Role {
		case "system":
			stats.SystemMessages++
		case "assistant":
			stats.AssistantMessages++
		case "user":
			stats.UserMessages++
		}

		// Current kernel history stores text/tool blocks in content[]. Older or
		// ad-hoc rows may store plain content strings; count those as text by
		// role rather than dropping the whole row. The system prompt lives in the
		// top-level `system` field and is counted as a system message, not as
		// text input/output.
		var blocks []json.RawMessage
		if len(entry.Content) > 0 {
			if err := json.Unmarshal(entry.Content, &blocks); err != nil {
				var plain string
				if err := json.Unmarshal(entry.Content, &plain); err == nil && plain != "" {
					if entry.Role == "assistant" {
						stats.TextOutputs++
					} else if entry.Role != "system" {
						stats.TextInputs++
					}
				}
			}
		}
		for _, raw := range blocks {
			var block struct {
				Type string `json:"type"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			switch block.Type {
			case "tool_call":
				stats.ToolCalls++
				name := block.Name
				if name == "" {
					name = "unknown"
				}
				callCounts[name]++
			case "tool_result":
				stats.ToolResults++
				name := block.Name
				if name == "" {
					name = "unknown"
				}
				resultCounts[name]++
			case "text":
				if entry.Role == "assistant" {
					stats.TextOutputs++
				} else if entry.Role != "system" {
					stats.TextInputs++
				}
			}
		}
	})

	names := make([]string, 0, len(callCounts)+len(resultCounts))
	seen := map[string]bool{}
	for name := range callCounts {
		names = append(names, name)
		seen[name] = true
	}
	for name := range resultCounts {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		ci, cj := callCounts[names[i]], callCounts[names[j]]
		if ci != cj {
			return ci > cj
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		stats.ToolCounts = append(stats.ToolCounts, ContextToolCount{
			Name:    name,
			Calls:   callCounts[name],
			Results: resultCounts[name],
		})
	}
	return stats
}

// TokenTotals holds summed token usage across multiple agents.
type TokenTotals struct {
	Input    int64
	Output   int64
	Thinking int64
	Cached   int64
	APICalls int64
}

// SessionTokenStats holds token/cache statistics for one agent session window.
type SessionTokenStats struct {
	TokenTotals
	HasCodexTransferMode bool
	CodexFull            int64
	CodexIncremental     int64
}

// MoltSessionTokenStats groups API/cache statistics for the current and
// immediately previous molt windows.
type MoltSessionTokenStats struct {
	Current SessionTokenStats
	Last    SessionTokenStats
}

// AggregateTokens sums token usage from logs/token_ledger.jsonl across all given agent directories.
// Skips agents whose ledger is missing or unreadable.
func AggregateTokens(dirs []string) TokenTotals {
	var t TokenTotals
	for _, dir := range dirs {
		ledger := SumTokenLedger(filepath.Join(dir, "logs", "token_ledger.jsonl"))
		t.Input += ledger.Input
		t.Output += ledger.Output
		t.Thinking += ledger.Thinking
		t.Cached += ledger.Cached
		t.APICalls += ledger.APICalls
	}
	return t
}

// SumTokenLedger reads a token_ledger.jsonl file and sums main-agent entries.
// Daemon-sourced rows are skipped because they are reported separately from
// daemons/<run_id>/logs/token_ledger.jsonl. Returns zero totals if the file is
// missing or unreadable.
func SumTokenLedger(path string) TokenTotals {
	var t TokenTotals
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return t
	}
	if cached, ok := cachedTokenLedgerTotals(path, info); ok {
		return cached
	}

	t, err = sumTokenLedgerFile(path)
	if err != nil {
		return TokenTotals{}
	}
	storeTokenLedgerTotals(path, info, t)
	return t
}

func sumTokenLedgerFile(path string) (TokenTotals, error) {
	var t TokenTotals
	err := forEachJSONLLine(path, func(line []byte) {
		var entry LedgerEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return
		}
		if isDaemonLedgerEntry(entry) {
			return
		}
		t.Input += entry.Input
		t.Output += entry.Output
		t.Thinking += entry.Thinking
		t.Cached += entry.Cached
		t.APICalls++
	})
	return t, err
}

type tokenLedgerCacheEntry struct {
	size    int64
	modTime time.Time
	totals  TokenTotals
}

var tokenLedgerTotalsCache = struct {
	sync.Mutex
	byPath map[string]tokenLedgerCacheEntry
}{byPath: map[string]tokenLedgerCacheEntry{}}

type moltSessionTokenLedgerCacheEntry struct {
	ledgerSize    int64
	ledgerModTime time.Time
	currentSince  time.Time
	lastSince     time.Time
	lastBefore    time.Time
	stats         MoltSessionTokenStats
}

var moltSessionTokenLedgerCache = struct {
	sync.Mutex
	byPath map[string]moltSessionTokenLedgerCacheEntry
}{byPath: map[string]moltSessionTokenLedgerCacheEntry{}}

func cachedTokenLedgerTotals(path string, info os.FileInfo) (TokenTotals, bool) {
	key := filepath.Clean(path)
	tokenLedgerTotalsCache.Lock()
	defer tokenLedgerTotalsCache.Unlock()
	entry, ok := tokenLedgerTotalsCache.byPath[key]
	if !ok || entry.size != info.Size() || !entry.modTime.Equal(info.ModTime()) {
		return TokenTotals{}, false
	}
	return entry.totals, true
}

func storeTokenLedgerTotals(path string, info os.FileInfo, totals TokenTotals) {
	key := filepath.Clean(path)
	tokenLedgerTotalsCache.Lock()
	defer tokenLedgerTotalsCache.Unlock()
	tokenLedgerTotalsCache.byPath[key] = tokenLedgerCacheEntry{
		size:    info.Size(),
		modTime: info.ModTime(),
		totals:  totals,
	}
}

// LedgerEntry is a single per-call line from logs/token_ledger.jsonl
// surfaced to UI consumers (the kanban detail view, primarily). Older
// entries written before kernel v0.7.x have no Model/Endpoint/Source — those
// fields are simply empty. Source/EmID/RunID let readers distinguish parent
// agent calls from historical daemon rows that were mirrored into parent
// ledgers.
type LedgerEntry struct {
	TS                 string `json:"ts"`
	Input              int64  `json:"input"`
	Output             int64  `json:"output"`
	Thinking           int64  `json:"thinking"`
	Cached             int64  `json:"cached"`
	Model              string `json:"model,omitempty"`
	Endpoint           string `json:"endpoint,omitempty"`
	Source             string `json:"source,omitempty"`
	EmID               string `json:"em_id,omitempty"`
	RunID              string `json:"run_id,omitempty"`
	CodexTransferMode  string `json:"codex_transfer_mode,omitempty"`
	CodexWSDeltaReason string `json:"codex_ws_delta_reason,omitempty"`
}

// SumTokenLedgerByProvider reads a token_ledger.jsonl, groups main-agent
// entries by derived provider name, and returns the totals plus the most-recent
// `recentN` raw entries (newest first). Provider attribution comes from
// the entry's `endpoint` host when present; falls back to a `model`
// prefix match; otherwise "unknown".
//
// Daemon-sourced rows are skipped here and rendered from daemon run ledgers.
// Missing/unreadable file returns empty maps and nil entries — caller
// renders an empty state rather than erroring.
func SumTokenLedgerByProvider(path string, recentN int) (
	byProvider map[string]TokenTotals, recent []LedgerEntry,
) {
	byProvider = map[string]TokenTotals{}
	_ = forEachJSONLLine(path, func(line []byte) {
		var entry LedgerEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return
		}
		if isDaemonLedgerEntry(entry) {
			return
		}
		provider := DeriveLedgerProvider(entry.Endpoint, entry.Model)
		t := byProvider[provider]
		t.Input += entry.Input
		t.Output += entry.Output
		t.Thinking += entry.Thinking
		t.Cached += entry.Cached
		t.APICalls++
		byProvider[provider] = t
		recent = append(recent, entry)
		if recentN > 0 && len(recent) > recentN {
			copy(recent, recent[len(recent)-recentN:])
			recent = recent[:recentN]
		}
	})
	// Trim to the last recentN entries, newest last in file → newest at
	// the end of `recent`. Reverse so callers can iterate "newest first".
	if recentN > 0 && len(recent) > recentN {
		recent = recent[len(recent)-recentN:]
	}
	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}
	return byProvider, recent
}

// SumMoltSessionTokenLedger reads an agent's logs and sums non-daemon token
// usage for the current molt session (since the latest psyche_molt event) and
// the immediately previous session (the window before that latest molt).
func SumMoltSessionTokenLedger(agentDir string) MoltSessionTokenStats {
	ledgerPath := filepath.Join(agentDir, "logs", "token_ledger.jsonl")
	currentSince, lastSince, lastBefore, ok, err := sqlitelog.QueryMoltSessionWindows(agentDir)
	if err != nil || !ok {
		currentSince, lastSince, lastBefore = moltSessionWindows(filepath.Join(agentDir, "logs", "events.jsonl"))
	}
	return sumMoltSessionTokenLedgerBetween(ledgerPath, currentSince, lastSince, lastBefore)
}

func sumMoltSessionTokenLedgerBetween(path string, currentSince, lastSince, lastBefore time.Time) MoltSessionTokenStats {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return MoltSessionTokenStats{}
	}
	if cached, ok := cachedMoltSessionTokenLedger(path, info, currentSince, lastSince, lastBefore); ok {
		return cached
	}

	var stats MoltSessionTokenStats
	_ = forEachJSONLLine(path, func(line []byte) {
		var entry LedgerEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return
		}
		if isDaemonLedgerEntry(entry) {
			return
		}
		if ledgerEntryWithinBounds(entry, currentSince, time.Time{}) {
			addLedgerEntryToSessionStats(&stats.Current, entry)
		}
		if !lastBefore.IsZero() && ledgerEntryWithinBounds(entry, lastSince, lastBefore) {
			addLedgerEntryToSessionStats(&stats.Last, entry)
		}
	})
	storeMoltSessionTokenLedger(path, info, currentSince, lastSince, lastBefore, stats)
	return stats
}

func cachedMoltSessionTokenLedger(path string, info os.FileInfo, currentSince, lastSince, lastBefore time.Time) (MoltSessionTokenStats, bool) {
	key := filepath.Clean(path)
	moltSessionTokenLedgerCache.Lock()
	defer moltSessionTokenLedgerCache.Unlock()
	entry, ok := moltSessionTokenLedgerCache.byPath[key]
	if !ok || entry.ledgerSize != info.Size() || !entry.ledgerModTime.Equal(info.ModTime()) ||
		!entry.currentSince.Equal(currentSince) || !entry.lastSince.Equal(lastSince) || !entry.lastBefore.Equal(lastBefore) {
		return MoltSessionTokenStats{}, false
	}
	return entry.stats, true
}

func storeMoltSessionTokenLedger(path string, info os.FileInfo, currentSince, lastSince, lastBefore time.Time, stats MoltSessionTokenStats) {
	key := filepath.Clean(path)
	moltSessionTokenLedgerCache.Lock()
	defer moltSessionTokenLedgerCache.Unlock()
	moltSessionTokenLedgerCache.byPath[key] = moltSessionTokenLedgerCacheEntry{
		ledgerSize:    info.Size(),
		ledgerModTime: info.ModTime(),
		currentSince:  currentSince,
		lastSince:     lastSince,
		lastBefore:    lastBefore,
		stats:         stats,
	}
}

// MoltSessionToolCalls holds lifecycle type='tool_call' event counts for the
// current and immediately previous molt-session windows. Unlike
// SessionTokenStats (sourced from the token ledger), these counts come from
// the events store, so freshness is keyed on authoritative events.jsonl (with
// a derived-sidecar fallback), NOT on the token-ledger file: an events-only
// append must invalidate the count even when the ledger is unchanged. Tool
// results are never counted here.
type MoltSessionToolCalls struct {
	Current int64
	Last    int64
}

// SumMoltSessionToolCalls counts lifecycle type='tool_call' events in the same
// current and previous molt-session windows SumMoltSessionTokenLedger uses for
// the API rows. It prefers bounded SQLite queries. If the sidecar/window query
// is unavailable, one authoritative JSONL pass rolls the current/last counters
// at each valid psyche_molt boundary instead of scanning the file once for
// bounds and again for counts. Unchanged event stores return from cache before
// any query or scan.
func SumMoltSessionToolCalls(agentDir string) MoltSessionToolCalls {
	eventsPath := filepath.Join(agentDir, "logs", "events.jsonl")
	freshnessPath, info := eventsStoreFreshness(agentDir)
	if info == nil {
		return MoltSessionToolCalls{}
	}
	if cached, ok := cachedMoltSessionToolCalls(freshnessPath, info); ok {
		return cached
	}
	stale, hasStale := lastMoltSessionToolCalls(freshnessPath)

	currentSince, lastSince, lastBefore, ok, err := sqlitelog.QueryMoltSessionWindows(agentDir)
	if err != nil || !ok {
		stats := countMoltSessionToolCallsByMoltJSONL(eventsPath)
		storeMoltSessionToolCalls(freshnessPath, info, stats)
		return stats
	}
	stats, ok := queryMoltSessionToolCalls(agentDir, currentSince, lastSince, lastBefore)
	if !ok {
		// A live sidecar count can exceed its UI-thread deadline on a cold page
		// cache. Do not turn that bounded miss into a multi-second full JSONL scan;
		// keep the last value (or zero on first load) and retry on the next tick.
		if hasStale {
			return stale
		}
		return MoltSessionToolCalls{}
	}
	storeMoltSessionToolCalls(freshnessPath, info, stats)
	return stats
}

func queryMoltSessionToolCalls(agentDir string, currentSince, lastSince, lastBefore time.Time) (MoltSessionToolCalls, bool) {
	cur, last, ok, _ := sqlitelog.QueryMoltSessionToolCallCounts(agentDir, currentSince, lastSince, lastBefore)
	return MoltSessionToolCalls{Current: cur, Last: last}, ok
}

// countMoltSessionToolCallsByMoltJSONL is the single-pass fallback when SQLite
// cannot resolve the window at all. events.jsonl is append-ordered, so each
// valid psyche_molt promotes the just-finished current count to Last and starts
// a new current bucket. With no molt, all tool calls remain Current; with one
// molt, pre-molt calls form Last, matching the existing JSONL window semantics.
func countMoltSessionToolCallsByMoltJSONL(eventsPath string) MoltSessionToolCalls {
	var stats MoltSessionToolCalls
	var currentSince, lastSince, lastBefore time.Time
	_ = forEachJSONLLine(eventsPath, func(line []byte) {
		var evt struct {
			Type string  `json:"type"`
			TS   float64 `json:"ts"`
		}
		if err := json.Unmarshal(line, &evt); err != nil {
			return
		}
		switch evt.Type {
		case "tool_call":
			t := unixFloatTime(evt.TS)
			if timeWithinBounds(t, currentSince, time.Time{}) {
				stats.Current++
			}
			if !lastBefore.IsZero() && timeWithinBounds(t, lastSince, lastBefore) {
				stats.Last++
			}
		case "psyche_molt":
			if evt.TS > 0 {
				// The just-finished current bucket becomes Last. Then advance
				// the explicit bounds so later rows are bucketed by timestamp,
				// matching moltSessionWindows even if an old row appears late.
				stats.Last = stats.Current
				stats.Current = 0
				lastSince = currentSince
				lastBefore = unixFloatTime(evt.TS)
				currentSince = lastBefore
			}
		}
	})
	return stats
}

// timeWithinBounds mirrors ledgerEntryWithinBounds on an already-parsed event
// time: a zero since and zero before select everything; otherwise the event
// must be at or after since and (when before is set) strictly before before.
func timeWithinBounds(t, since, before time.Time) bool {
	if since.IsZero() && before.IsZero() {
		return true
	}
	if !since.IsZero() && t.Before(since) {
		return false
	}
	if !before.IsZero() && !t.Before(before) {
		return false
	}
	return true
}

// eventsStoreFreshness returns a freshness sentinel for lifecycle events. Prefer
// events.jsonl because it is the authoritative source and changes even when a
// live SQLite write is still in WAL or a SQLite query has to fall back to JSONL.
// The derived sidecar is only a last-resort sentinel for SQLite-only fixtures or
// incomplete/offline agent directories. A nil FileInfo means no events store
// exists, so there is nothing to count.
func eventsStoreFreshness(agentDir string) (string, os.FileInfo) {
	events := filepath.Join(agentDir, "logs", "events.jsonl")
	if info, err := os.Stat(events); err == nil && !info.IsDir() {
		return events, info
	}
	if info, err := os.Stat(sqlitelog.DBPath(agentDir)); err == nil && !info.IsDir() {
		return sqlitelog.DBPath(agentDir), info
	}
	return "", nil
}

type moltSessionToolCallsCacheEntry struct {
	storeSize    int64
	storeModTime time.Time
	stats        MoltSessionToolCalls
}

var moltSessionToolCallsCache = struct {
	sync.Mutex
	byPath map[string]moltSessionToolCallsCacheEntry
}{byPath: map[string]moltSessionToolCallsCacheEntry{}}

func cachedMoltSessionToolCalls(path string, info os.FileInfo) (MoltSessionToolCalls, bool) {
	key := filepath.Clean(path)
	moltSessionToolCallsCache.Lock()
	defer moltSessionToolCallsCache.Unlock()
	entry, ok := moltSessionToolCallsCache.byPath[key]
	if !ok || entry.storeSize != info.Size() || !entry.storeModTime.Equal(info.ModTime()) {
		return MoltSessionToolCalls{}, false
	}
	return entry.stats, true
}

func lastMoltSessionToolCalls(path string) (MoltSessionToolCalls, bool) {
	key := filepath.Clean(path)
	moltSessionToolCallsCache.Lock()
	defer moltSessionToolCallsCache.Unlock()
	entry, ok := moltSessionToolCallsCache.byPath[key]
	return entry.stats, ok
}

func storeMoltSessionToolCalls(path string, info os.FileInfo, stats MoltSessionToolCalls) {
	key := filepath.Clean(path)
	moltSessionToolCallsCache.Lock()
	defer moltSessionToolCallsCache.Unlock()
	moltSessionToolCallsCache.byPath[key] = moltSessionToolCallsCacheEntry{
		storeSize:    info.Size(),
		storeModTime: info.ModTime(),
		stats:        stats,
	}
}

// SumSessionTokenLedgerSince reads a token_ledger.jsonl file and sums
// non-daemon entries at or after the supplied cutoff. Rows with malformed
// timestamps are skipped when a cutoff is present so stale historical rows
// cannot leak into bounded session views.
func SumSessionTokenLedgerSince(path string, since time.Time) SessionTokenStats {
	return SumSessionTokenLedgerBetween(path, since, time.Time{})
}

// SumSessionTokenLedgerBetween reads a token_ledger.jsonl file and sums
// non-daemon entries in [since, before). A zero bound is open-ended.
func SumSessionTokenLedgerBetween(path string, since, before time.Time) SessionTokenStats {
	var stats SessionTokenStats
	_ = forEachJSONLLine(path, func(line []byte) {
		var entry LedgerEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return
		}
		if isDaemonLedgerEntry(entry) || !ledgerEntryWithinBounds(entry, since, before) {
			return
		}
		addLedgerEntryToSessionStats(&stats, entry)
	})
	return stats
}

func ledgerEntryWithinBounds(entry LedgerEntry, since, before time.Time) bool {
	if since.IsZero() && before.IsZero() {
		return true
	}
	ts, err := time.Parse(time.RFC3339, entry.TS)
	if err != nil {
		return false
	}
	if !since.IsZero() && ts.Before(since) {
		return false
	}
	if !before.IsZero() && !ts.Before(before) {
		return false
	}
	return true
}

func addLedgerEntryToSessionStats(stats *SessionTokenStats, entry LedgerEntry) {
	stats.Input += entry.Input
	stats.Output += entry.Output
	stats.Thinking += entry.Thinking
	stats.Cached += entry.Cached
	stats.APICalls++

	switch strings.ToLower(strings.TrimSpace(entry.CodexTransferMode)) {
	case "full":
		stats.HasCodexTransferMode = true
		stats.CodexFull++
	case "incremental":
		stats.HasCodexTransferMode = true
		stats.CodexIncremental++
	case "":
		// Non-Codex rows.
	}
}

// moltSessionWindows returns the current lower bound, previous lower bound, and
// previous upper bound from logs/events.jsonl psyche_molt rows. Missing bounds
// are returned as zero times, which makes the first current session start at the
// beginning of the ledger while suppressing a nonexistent previous session.
func moltSessionWindows(eventsPath string) (currentSince, lastSince, lastBefore time.Time) {
	f, err := os.Open(eventsPath)
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	for {
		var evt struct {
			Type string  `json:"type"`
			TS   float64 `json:"ts"`
		}
		if err := dec.Decode(&evt); err != nil {
			if err == io.EOF {
				break
			}
			return currentSince, lastSince, lastBefore
		}
		if evt.Type != "psyche_molt" || evt.TS <= 0 {
			continue
		}
		lastSince = currentSince
		lastBefore = unixFloatTime(evt.TS)
		currentSince = lastBefore
	}
	return currentSince, lastSince, lastBefore
}

func unixFloatTime(ts float64) time.Time {
	sec := int64(ts)
	nsec := int64((ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).UTC()
}

func isDaemonLedgerEntry(entry LedgerEntry) bool {
	return strings.EqualFold(strings.TrimSpace(entry.Source), "daemon") ||
		strings.TrimSpace(entry.EmID) != "" ||
		strings.TrimSpace(entry.RunID) != ""
}

// DeriveLedgerProvider maps a ledger entry's endpoint host (or model
// prefix as a fallback) to a canonical provider name. Returns "unknown"
// when neither signal narrows things down — older ledger entries that
// predate the kernel's model/endpoint attribution land here, as do
// custom user-hosted endpoints we don't recognize.
//
// Endpoint matching uses substring on the URL because base_url shapes
// vary ("https://api.minimaxi.com/v1", "api.minimax.chat", etc.).
func DeriveLedgerProvider(endpoint, model string) string {
	ep := strings.ToLower(endpoint)
	switch {
	case ep == "":
		// fall through to model
	case strings.Contains(ep, "minimaxi.com"), strings.Contains(ep, "minimax.chat"):
		return "minimax"
	case strings.Contains(ep, "deepseek.com"):
		return "deepseek"
	case strings.Contains(ep, "z.ai"), strings.Contains(ep, "bigmodel.cn"):
		return "zhipu"
	case strings.Contains(ep, "xiaomimimo.com"):
		return "mimo"
	case strings.Contains(ep, "openai.com"):
		return "openai"
	case strings.Contains(ep, "anthropic.com"):
		return "anthropic"
	case strings.Contains(ep, "googleapis.com"), strings.Contains(ep, "generativelanguage"):
		return "gemini"
	case strings.Contains(ep, "openrouter.ai"):
		return "openrouter"
	case strings.Contains(ep, "api.nvidia.com"):
		return "nvidia"
	case ep != "":
		// Recognized URL but not in our table — surface the host so the
		// user can still see the breakdown without a code change.
		host := ep
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
		if i := strings.Index(host, "/"); i >= 0 {
			host = host[:i]
		}
		host = strings.TrimPrefix(host, "www.")
		if host != "" {
			return host
		}
	}
	// Fallback to model prefix.
	mp := strings.ToLower(model)
	switch {
	case strings.HasPrefix(mp, "minimax-"):
		return "minimax"
	case strings.HasPrefix(mp, "deepseek-"):
		return "deepseek"
	case strings.HasPrefix(mp, "glm-"):
		return "zhipu"
	case strings.HasPrefix(mp, "mimo-"):
		return "mimo"
	case strings.HasPrefix(mp, "gpt-"), strings.HasPrefix(mp, "o1-"), strings.HasPrefix(mp, "o3-"):
		return "openai"
	case strings.HasPrefix(mp, "claude-"):
		return "anthropic"
	case strings.HasPrefix(mp, "gemini-"):
		return "gemini"
	}
	return "unknown"
}
