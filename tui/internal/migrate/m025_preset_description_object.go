package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// migratePresetDescriptionObject promotes `description` on every preset in
// the global preset library at ~/.lingtai-tui/presets/ from a plain string
// (or missing) to the structured object form `{summary, tier?}`. Any
// existing top-level `tags: ["tier:N"]` array gets folded into
// `description.tier`, then the `tags` key is dropped.
//
// This mirrors the kernel-side m002 migration. Both run idempotently and
// converge on the same on-disk shape; whichever fires first wins, the
// other becomes a no-op. The TUI runs this so version-stamped projects
// don't have to wait for a kernel call to normalize their library.
//
// Keeping the migration registered at version 25 (rather than adding an
// m027) keeps the shared meta.json version space contiguous. The previous
// m025 (tags-field backfill) was unreleased.
//
// Note: this migration touches the GLOBAL preset library, not anything
// inside lingtaiDir. The lingtaiDir argument is part of the migration
// signature and is ignored here.
func migratePresetDescriptionObject(lingtaiDir string) error {
	_ = lingtaiDir // unused — see docstring

	presetsDir := preset.PresetsDir()
	entries, err := os.ReadDir(presetsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no library yet → nothing to migrate
		}
		return fmt.Errorf("read presets dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".json" && ext != ".jsonc" {
			continue
		}
		// Skip the kernel's migration meta file — same dir but not a preset.
		if e.Name() == "_kernel_meta.json" {
			continue
		}

		path := filepath.Join(presetsDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "m025: skipping %s — read failed: %v\n", e.Name(), err)
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			fmt.Fprintf(os.Stderr, "m025: skipping %s — unparseable: %v\n", e.Name(), err)
			continue
		}

		changed := false

		// 1. description: string → {summary: string}; missing → {summary: ""}
		var desc map[string]interface{}
		switch v := raw["description"].(type) {
		case string:
			desc = map[string]interface{}{"summary": v}
			raw["description"] = desc
			changed = true
		case map[string]interface{}:
			desc = v
		case nil:
			desc = map[string]interface{}{"summary": ""}
			raw["description"] = desc
			changed = true
		default:
			fmt.Fprintf(os.Stderr, "m025: skipping %s — description has unexpected type %T\n", e.Name(), v)
			continue
		}

		// 2. fold tags:[tier:N] → description.tier
		if tagsRaw, ok := raw["tags"]; ok {
			if tags, ok := tagsRaw.([]interface{}); ok {
				if tier := extractTier(tags); tier != "" {
					if _, exists := desc["tier"]; !exists {
						desc["tier"] = tier
					}
				}
			}
			delete(raw, "tags")
			changed = true
		}

		if !changed {
			continue
		}

		updated, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "m025: marshal failed for %s: %v\n", e.Name(), err)
			continue
		}

		// Atomic write.
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, updated, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "m025: write tmp failed for %s: %v\n", e.Name(), err)
			continue
		}
		if err := os.Rename(tmp, path); err != nil {
			fmt.Fprintf(os.Stderr, "m025: rename failed for %s: %v\n", e.Name(), err)
			_ = os.Remove(tmp)
		}
	}

	return nil
}

// extractTier returns "1".."5" if any element of tags is the string
// "tier:<v>" with v in that vocabulary, otherwise "". Unknown tier
// values (e.g. legacy "tier:opus") are dropped because they have no
// counterpart in the new vocabulary.
func extractTier(tags []interface{}) string {
	valid := map[string]bool{"1": true, "2": true, "3": true, "4": true, "5": true}
	for _, t := range tags {
		s, ok := t.(string)
		if !ok {
			continue
		}
		if !strings.HasPrefix(s, "tier:") {
			continue
		}
		v := strings.TrimPrefix(s, "tier:")
		if valid[v] {
			return v
		}
	}
	return ""
}
