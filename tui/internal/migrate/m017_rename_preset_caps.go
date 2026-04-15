package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// migrateRenamePresetCaps rewrites capability keys in global preset JSON
// files at ~/.lingtai-tui/presets/*.json so saved built-in presets
// (minimax/zhipu/codex/custom + user clones) reflect the post-m016 rename:
//
//   - capabilities["library"]  → capabilities["codex"]   (knowledge archive)
//   - capabilities["skills"]   → capabilities["library"] (skill library)
//   - capability config "library_limit" → "codex_limit"
//
// m016 only rewrote per-project agent init.json files. The global preset
// templates in ~/.lingtai-tui/presets/ were missed, so the first-run
// capability screen showed the new "codex" and "library" slots as
// unselected when picking any built-in preset.
//
// This migration is idempotent: if a preset file has no stale keys, it's
// left untouched. It runs per-project like every other migration, but
// the work it does is on global state — multiple invocations are safe.
func migrateRenamePresetCaps(_ string) error {
	globalDir := globalTUIDir()
	if globalDir == "" {
		return nil // can't resolve home — skip silently
	}
	presetsDir := filepath.Join(globalDir, "presets")

	entries, err := os.ReadDir(presetsDir)
	if err != nil {
		return nil // no presets dir yet — nothing to migrate
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rewritePresetCaps(filepath.Join(presetsDir, e.Name()))
	}
	return nil
}

// rewritePresetCaps applies the m016 capability-key rename rules to a
// single preset file. Silent no-op if the file is unreadable, corrupt,
// or already migrated.
func rewritePresetCaps(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var p map[string]interface{}
	if json.Unmarshal(data, &p) != nil {
		return
	}

	manifest, _ := p["manifest"].(map[string]interface{})
	if manifest == nil {
		return
	}
	caps, _ := manifest["capabilities"].(map[string]interface{})
	if caps == nil {
		return
	}

	changed := false

	// A preset file is considered "post-migration" if it already has a
	// "codex" key (the new-world knowledge archive). In that case we
	// leave it completely alone — even if a "skills" key exists, we
	// can't safely rename skills→library without risking clobbering
	// an intentional post-m016 "library" (skill library) entry.
	if _, alreadyMigrated := caps["codex"]; alreadyMigrated {
		return
	}

	// "library" (old knowledge archive) → "codex" (new knowledge archive).
	// Do this FIRST so the subsequent skills→library step doesn't
	// clobber the old library value.
	if v, ok := caps["library"]; ok {
		caps["codex"] = v
		delete(caps, "library")
		changed = true

		// Rename library_limit → codex_limit inside the config
		if cfg, ok := caps["codex"].(map[string]interface{}); ok {
			if lim, ok := cfg["library_limit"]; ok {
				cfg["codex_limit"] = lim
				delete(cfg, "library_limit")
			}
		}
	}

	// "skills" (old skill library) → "library" (new skill library).
	if v, ok := caps["skills"]; ok {
		caps["library"] = v
		delete(caps, "skills")
		changed = true
	}

	if !changed {
		return
	}

	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Printf("  warning: failed to rewrite %s: %v\n", path, err)
	} else {
		fmt.Printf("  migrated preset %s\n", filepath.Base(path))
	}
}
