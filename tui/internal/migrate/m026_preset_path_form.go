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
// Background: prior to this migration, the kernel resolved a bare stem like
// "deepseek" against `manifest.preset.path` (or the default ~/.lingtai-tui/presets/).
// Multi-path preset libraries made stem-as-name unworkable — two libraries
// could each hold a `cheap.json` and the listing would silently shadow one.
// The redesign treats the path as the identity. The kernel no longer accepts
// bare stems; init.json files written by older TUI builds need their
// `manifest.preset.active` and `manifest.preset.default` rewritten.
//
// Idempotent: any value that already contains a path separator or ends in
// `.json`/`.jsonc` is left alone. Only bare-stem strings are rewritten.
//
// Skips files that don't have a `manifest.preset` block at all (the kernel
// default-presets path is unused in that case — nothing to migrate).
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

		// Determine library prefix to use for stem→path conversion.
		// Prefer manifest.preset.path (single string form) if it exists; that
		// matches how the kernel previously resolved stems. Otherwise fall
		// back to the per-machine default. Multi-path lists are not handled
		// here — m024 only ever wrote single strings, so this is safe.
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

// isPathForm reports whether s already looks like a path (absolute, ~/-prefixed,
// or extension-suffixed) and so should be left untouched by the migration.
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
