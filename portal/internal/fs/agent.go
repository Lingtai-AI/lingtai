// internal/fs/agent.go
package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// ReadInitManifest returns the agent's manifest fields with the llm
// sub-object (model, provider, base_url) and soul.delay flattened to top
// level. It prefers the kernel-published resolved-manifest artifact
// (system/manifest.resolved.json — preset materialized, validated,
// secret-redacted; kernel issue #259) and falls back to the raw init.json
// snapshot when the artifact is absent or malformed (stopped / never-booted
// agents).
func ReadInitManifest(dir string) (map[string]interface{}, error) {
	manifest, err := readResolvedManifest(dir)
	if err != nil {
		manifest, err = readRawInitManifest(dir)
		if err != nil {
			return nil, err
		}
	}
	flattenManifest(manifest)
	return manifest, nil
}

// readResolvedManifest reads the manifest from the kernel-published artifact
// at system/manifest.resolved.json.
func readResolvedManifest(dir string) (map[string]interface{}, error) {
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

// readRawInitManifest reads the manifest from the raw init.json snapshot.
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

// flattenManifest hoists llm sub-fields and soul.delay to top level, in place.
func flattenManifest(manifest map[string]interface{}) {
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
		Context struct {
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

// SumTokenLedger reads a token_ledger.jsonl file and sums all entries.
// Returns zero totals if the file is missing or unreadable.
func SumTokenLedger(path string) TokenTotals {
	var t TokenTotals
	data, err := os.ReadFile(path)
	if err != nil {
		return t
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			Input    int64 `json:"input"`
			Output   int64 `json:"output"`
			Thinking int64 `json:"thinking"`
			Cached   int64 `json:"cached"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		t.Input += entry.Input
		t.Output += entry.Output
		t.Thinking += entry.Thinking
		t.Cached += entry.Cached
		t.APICalls++
	}
	return t
}
