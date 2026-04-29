package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// migratePresetPathForm rewrites legacy stem-form preset references in
// agent init.json files into path-form (`~/.lingtai-tui/presets/<stem>.json`).
//
// Mirror of the TUI's m026. Both binaries must run this — the kernel no
// longer accepts bare stems, so init.json files written by older builds
// fail to boot until rewritten. Portal-side logic is identical because
// init.json shape is shared.
//
// Idempotent: any value that already contains a path separator or ends in
// `.json`/`.jsonc` is left alone.
func migratePresetPathForm(lingtaiDir string) error {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .lingtai dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "" || name[0] == '.' || name == "human" {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, name)
		initPath := filepath.Join(agentDir, "init.json")
		data, err := os.ReadFile(initPath)
		if err != nil {
			continue
		}
		var init map[string]interface{}
		if err := json.Unmarshal(data, &init); err != nil {
			fmt.Fprintf(os.Stderr, "m026: skipping %s — unparseable init.json: %v\n",
				agentDir, err)
			continue
		}
		manifest, ok := init["manifest"].(map[string]interface{})
		if !ok {
			continue
		}
		preset, ok := manifest["preset"].(map[string]interface{})
		if !ok {
			continue
		}

		var libPrefix string
		switch p := preset["path"].(type) {
		case string:
			libPrefix = strings.TrimRight(p, "/")
		default:
			libPrefix = "~/.lingtai-tui/presets"
		}

		changed := false
		for _, key := range []string{"active", "default"} {
			val, ok := preset[key].(string)
			if !ok || val == "" {
				continue
			}
			if isPathForm(val) {
				continue
			}
			preset[key] = libPrefix + "/" + val + ".json"
			changed = true
		}
		if !changed {
			continue
		}

		updated, err := json.MarshalIndent(init, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "m026: marshal failed for %s: %v\n", agentDir, err)
			continue
		}
		tmp := initPath + ".tmp"
		if err := os.WriteFile(tmp, updated, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "m026: write tmp failed for %s: %v\n", agentDir, err)
			continue
		}
		if err := os.Rename(tmp, initPath); err != nil {
			fmt.Fprintf(os.Stderr, "m026: rename failed for %s: %v\n", agentDir, err)
		}
	}
	return nil
}

func isPathForm(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsRune(s, '/') {
		return true
	}
	if strings.HasSuffix(s, ".json") || strings.HasSuffix(s, ".jsonc") {
		return true
	}
	return false
}
