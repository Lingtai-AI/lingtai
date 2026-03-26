package preset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Preset is a reusable agent template stored at ~/.lingtai/presets/.
type Preset struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Manifest    map[string]interface{} `json:"manifest"`
}

// PresetsDir returns ~/.lingtai/presets/.
func PresetsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".lingtai", "presets")
}

// List returns all presets from the presets directory.
func List() ([]Preset, error) {
	dir := PresetsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read presets dir: %w", err)
	}
	var presets []Preset
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		p, err := Load(e.Name()[:len(e.Name())-5]) // strip .json
		if err != nil {
			continue
		}
		presets = append(presets, p)
	}
	return presets, nil
}

// HasAny returns true if at least one preset exists.
func HasAny() bool {
	presets, _ := List()
	return len(presets) > 0
}

// Load reads a single preset by name.
func Load(name string) (Preset, error) {
	path := filepath.Join(PresetsDir(), name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Preset{}, fmt.Errorf("read preset %s: %w", name, err)
	}
	var p Preset
	if err := json.Unmarshal(data, &p); err != nil {
		return Preset{}, fmt.Errorf("parse preset %s: %w", name, err)
	}
	return p, nil
}

// Save writes a preset to the presets directory.
func Save(p Preset) error {
	dir := PresetsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create presets dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal preset: %w", err)
	}
	path := filepath.Join(dir, p.Name+".json")
	return os.WriteFile(path, data, 0o644)
}

// Delete removes a preset file.
func Delete(name string) error {
	path := filepath.Join(PresetsDir(), name+".json")
	return os.Remove(path)
}

// EnsureDefault creates the minimax-all preset if no presets exist.
func EnsureDefault() error {
	presets, _ := List()
	if len(presets) > 0 {
		return nil
	}
	return Save(DefaultPreset())
}

// DefaultPreset returns the built-in minimax-all preset.
func DefaultPreset() Preset {
	return Preset{
		Name:        "minimax-all",
		Description: "MiniMax M2.7 with all capabilities",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "minimax",
				"model":       "MiniMax-M2.7-highspeed",
				"api_key":     nil,
				"api_key_env": "MINIMAX_API_KEY",
				"base_url":    nil,
			},
			"capabilities": map[string]interface{}{
				"file":       map[string]interface{}{},
				"email":      map[string]interface{}{},
				"bash":       map[string]interface{}{"yolo": true},
				"web_search": map[string]interface{}{"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"},
				"psyche":     map[string]interface{}{},
				"library":    map[string]interface{}{},
				"vision":     map[string]interface{}{"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"},
				"talk":       map[string]interface{}{"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"},
				"draw":       map[string]interface{}{"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"},
				"compose":    map[string]interface{}{"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"},
				"listen":     map[string]interface{}{},
				"web_read":   map[string]interface{}{},
				"avatar":     map[string]interface{}{},
				"daemon":     map[string]interface{}{},
			},
			"admin": map[string]interface{}{"karma": true},
		},
	}
}

// GenerateInitJSON creates a full init.json from a preset at .lingtai/<agentName>/init.json.
func GenerateInitJSON(p Preset, agentName, lingtaiDir string) error {
	agentDir := filepath.Join(lingtaiDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return fmt.Errorf("create agent dir: %w", err)
	}

	// Build manifest with defaults
	manifest := make(map[string]interface{})
	manifest["agent_name"] = agentName
	manifest["language"] = "en"
	if llm, ok := p.Manifest["llm"]; ok {
		manifest["llm"] = llm
	}
	if caps, ok := p.Manifest["capabilities"]; ok {
		manifest["capabilities"] = caps
	}
	if admin, ok := p.Manifest["admin"]; ok {
		manifest["admin"] = admin
	}
	manifest["soul"] = map[string]interface{}{"delay": 30}
	manifest["stamina"] = 3600
	manifest["context_limit"] = nil
	manifest["molt_pressure"] = 0.8
	manifest["molt_prompt"] = ""
	manifest["max_turns"] = 100
	manifest["streaming"] = true

	initJSON := map[string]interface{}{
		"manifest":  manifest,
		"principle": "You are an orchestrator agent. Coordinate work, communicate via email, and manage sub-agents as needed.",
		"covenant":  "You are a helpful agent. Respond via email to all requests. Be concise and actionable.",
		"memory":    "",
		"prompt":    "Hello. I am ready to receive instructions.",
	}

	data, err := json.MarshalIndent(initJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal init.json: %w", err)
	}

	initPath := filepath.Join(agentDir, "init.json")
	if err := os.WriteFile(initPath, data, 0o644); err != nil {
		return fmt.Errorf("write init.json: %w", err)
	}

	// Also create .agent.json manifest for the agent
	absDir, _ := filepath.Abs(agentDir)
	agentManifest := map[string]interface{}{
		"agent_name": agentName,
		"address":    absDir,
		"state":      "",
		"admin":      p.Manifest["admin"],
	}

	// Create mailbox structure
	for _, sub := range []string{
		"mailbox/inbox",
		"mailbox/sent",
		"mailbox/archive",
	} {
		os.MkdirAll(filepath.Join(agentDir, sub), 0o755)
	}

	mdata, _ := json.MarshalIndent(agentManifest, "", "  ")
	os.WriteFile(filepath.Join(agentDir, ".agent.json"), mdata, 0o644)

	return nil
}
