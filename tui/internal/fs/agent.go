// internal/fs/agent.go
package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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
		UptimeSeconds float64 `json:"uptime_seconds"`
		StaminaLeft   float64 `json:"stamina_left"`
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
	TS       string `json:"ts"`
	Input    int64  `json:"input"`
	Output   int64  `json:"output"`
	Thinking int64  `json:"thinking"`
	Cached   int64  `json:"cached"`
	Model    string `json:"model,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Source   string `json:"source,omitempty"`
	EmID     string `json:"em_id,omitempty"`
	RunID    string `json:"run_id,omitempty"`
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
