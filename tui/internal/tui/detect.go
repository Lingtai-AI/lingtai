package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/process"
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

// PropagateOrchestratorConfig is the concrete CLI adapter; Project Core owns
// selected-member filtering, merge/stripping and partial propagation policy.
func PropagateOrchestratorConfig(baseDir, orchDir string, python ...string) error {
	cmd := ""
	if len(python) > 0 {
		cmd = python[0]
	} else {
		globalDir, err := config.GlobalDirPath()
		if err != nil {
			return fmt.Errorf("Project kernel interpreter unavailable")
		}
		cmd = config.LingtaiCmd(globalDir)
	}
	cmd = process.ProjectPython(orchDir, cmd)
	_, err := process.PropagateProjectConfig(context.Background(), cmd, filepath.Dir(baseDir), filepath.Base(orchDir))
	return err
}

// IsOrchestrator checks if a manifest has admin with at least one truthy value.
// admin must be a map[string]interface{} (not nil, not absent) with at least one
// value that is true (bool).
func IsOrchestrator(manifest map[string]interface{}) bool {
	return fs.IsOrchestratorManifest(manifest)
}
