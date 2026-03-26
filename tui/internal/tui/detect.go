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
