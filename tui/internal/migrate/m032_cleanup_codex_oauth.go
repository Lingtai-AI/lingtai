package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// migrateCleanupCodexOAuth renames presets/saved/codex_oauth.json to
// presets/saved/codex.json if the latter doesn't already exist. When both
// exist, keeps the more-recent file and removes the other.
// Also rewrites any init.json preset paths that reference "codex_oauth".
func migrateCleanupCodexOAuth(lingtaiDir string) error {
	// This migration operates globally (the presets directory lives in
	// ~/.lingtai-tui/, not per-project). We walk each agent directory
	// under lingtaiDir to fix init.json references.
	presetDir := filepath.Join(filepath.Dir(lingtaiDir), "presets", "saved")
	legacyPath := filepath.Join(presetDir, "codex_oauth.json")
	targetPath := filepath.Join(presetDir, "codex.json")

	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		// No legacy file — nothing to do for preset files.
		// Still fix init.json references below.
		return fixInitJSONCodexRefs(lingtaiDir, false)
	}

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		// Target doesn't exist — just rename.
		if err := os.Rename(legacyPath, targetPath); err != nil {
			return err
		}
		return fixInitJSONCodexRefs(lingtaiDir, true)
	}

	// Both exist — keep the newer one.
	legacyInfo, _ := os.Stat(legacyPath)
	targetInfo, _ := os.Stat(targetPath)
	if legacyInfo.ModTime().After(targetInfo.ModTime()) {
		os.Remove(targetPath)
		os.Rename(legacyPath, targetPath)
	} else {
		os.Remove(legacyPath)
	}
	return fixInitJSONCodexRefs(lingtaiDir, true)
}

// fixInitJSONCodexRefs rewrites init.json preset path references from
// codex_oauth to codex when applicable.
func fixInitJSONCodexRefs(lingtaiDir string, renamed bool) error {
	parentDir := filepath.Dir(lingtaiDir)
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil // best-effort
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		initPath := filepath.Join(parentDir, e.Name(), "init.json")
		data, err := os.ReadFile(initPath)
		if err != nil {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		manifest, _ := raw["manifest"].(map[string]interface{})
		if manifest == nil {
			continue
		}
		presetBlock, _ := manifest["preset"].(map[string]interface{})
		if presetBlock == nil {
			continue
		}
		dirty := false
		for _, key := range []string{"default", "active"} {
			if val, ok := presetBlock[key].(string); ok && strings.Contains(val, "codex_oauth") {
				presetBlock[key] = strings.ReplaceAll(val, "codex_oauth", "codex")
				dirty = true
			}
		}
		if allowed, ok := presetBlock["allowed"].([]interface{}); ok {
			for i, v := range allowed {
				if s, ok := v.(string); ok && strings.Contains(s, "codex_oauth") {
					allowed[i] = strings.ReplaceAll(s, "codex_oauth", "codex")
					dirty = true
				}
			}
		}
		if dirty {
			newData, _ := json.MarshalIndent(raw, "", "  ")
			os.WriteFile(initPath, newData, 0o644)
		}
	}
	return nil
}
