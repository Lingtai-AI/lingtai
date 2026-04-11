package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DetectOrchestrators scans baseDir for .agent.json files with admin privileges.
// Returns the directory names (not full paths) of orchestrators found.
func DetectOrchestrators(baseDir string) []string {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}
	var orchestrators []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(baseDir, entry.Name(), ".agent.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var manifest map[string]interface{}
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		if IsOrchestrator(manifest) {
			orchestrators = append(orchestrators, entry.Name())
		}
	}
	return orchestrators
}

// PropagateOrchestratorLLM reads the orchestrator's init.json and copies its
// manifest.llm config to every other agent's init.json in the same .lingtai/
// network. All agents share the same LLM provider within a local network.
// Skips directories that are not agents (no init.json).
func PropagateOrchestratorLLM(baseDir, orchDir string) error {
	// Read orchestrator's LLM config
	orchInitPath := filepath.Join(orchDir, "init.json")
	orchData, err := os.ReadFile(orchInitPath)
	if err != nil {
		return err
	}
	var orchInit map[string]interface{}
	if err := json.Unmarshal(orchData, &orchInit); err != nil {
		return err
	}
	orchManifest, _ := orchInit["manifest"].(map[string]interface{})
	if orchManifest == nil {
		return nil
	}
	orchLLM, _ := orchManifest["llm"].(map[string]interface{})
	if orchLLM == nil {
		return nil
	}
	orchEnvFile, _ := orchInit["env_file"].(string)

	// Walk all agent dirs in baseDir
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentDir := filepath.Join(baseDir, entry.Name())
		if agentDir == orchDir {
			continue // skip the orchestrator itself
		}
		initPath := filepath.Join(agentDir, "init.json")
		data, err := os.ReadFile(initPath)
		if err != nil {
			continue // not an agent or no init.json
		}
		var initJSON map[string]interface{}
		if err := json.Unmarshal(data, &initJSON); err != nil {
			continue
		}
		manifest, _ := initJSON["manifest"].(map[string]interface{})
		if manifest == nil {
			continue
		}
		// Replace LLM config
		manifest["llm"] = orchLLM
		// Sync env_file so the agent can resolve api_key_env
		if orchEnvFile != "" {
			initJSON["env_file"] = orchEnvFile
		}
		out, err := json.MarshalIndent(initJSON, "", "  ")
		if err != nil {
			continue
		}
		os.WriteFile(initPath, out, 0o644)
	}
	return nil
}

// IsOrchestrator checks if a manifest has admin with at least one truthy value.
// admin must be a map[string]interface{} (not nil, not absent) with at least one
// value that is true (bool).
func IsOrchestrator(manifest map[string]interface{}) bool {
	adminRaw, ok := manifest["admin"]
	if !ok || adminRaw == nil {
		return false
	}
	adminMap, ok := adminRaw.(map[string]interface{})
	if !ok {
		return false
	}
	for _, v := range adminMap {
		if b, ok := v.(bool); ok && b {
			return true
		}
	}
	return false
}
