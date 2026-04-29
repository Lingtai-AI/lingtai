package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// migratePresetTagsField backfills the `tags` field on every preset file in
// the user's global preset library at ~/.lingtai-tui/presets/.
//
// The new TUI surfaces preset tags (notably tier:1..tier:5 — the cost/quality
// ladder rendered as stars in EN, 拉完了 / NPC / 顶级 / 人上人 / 夯 in
// zh/wen) in the dedicated /presets screen. Older preset files predate this
// schema and have no `tags` key at all. The kernel's load_preset accepts
// missing tags as an empty list, so this migration is a pure cosmetic
// upgrade — but it makes the field explicit on disk so users who edit a
// preset's JSON by hand see "tags": [] and know it's a thing they can fill.
//
// Idempotent: presets that already declare `tags` (even empty) are skipped.
//
// Despite running per-project (because that's how the TUI migration system
// is wired), the work targets the *shared* global library — so the same
// scan happens once per project context, but each individual file is
// touched at most once across all projects, since the migration short-
// circuits when `tags` is already present.
//
// Note: this migration touches the GLOBAL preset library, not anything
// inside lingtaiDir. The lingtaiDir argument is part of the migration
// signature and is ignored here.
func migratePresetTagsField(lingtaiDir string) error {
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
		// The kernel treats both .json and .jsonc as preset files; mirror
		// that here so JSONC presets get backfilled too.
		if ext != ".json" && ext != ".jsonc" {
			continue
		}
		// Skip the kernel's migration meta file — it sits in the same dir
		// but is not a preset.
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

		// Already migrated → no-op.
		if _, exists := raw["tags"]; exists {
			continue
		}

		// Backfill with an empty list. Empty `tags` is semantically identical
		// to a missing `tags` key (both mean "no tier classified yet"), but
		// having it on disk advertises the field's existence.
		raw["tags"] = []interface{}{}

		updated, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "m025: marshal failed for %s: %v\n", e.Name(), err)
			continue
		}

		// Atomic write — match the rename pattern other migrations use.
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
