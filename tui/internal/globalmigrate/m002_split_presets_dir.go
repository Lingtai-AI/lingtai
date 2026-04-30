package globalmigrate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// migrateSplitPresetsDir moves files out of `~/.lingtai-tui/presets/`
// (the flat layout) into `presets/templates/` and `presets/saved/`.
//
// Background: pre-m002, templates and user-saved presets coexisted in
// the same directory and were distinguished by a hardcoded name list
// (preset.IsBuiltin). That made every operation ask "is this a
// template?" and let users accidentally clobber a template by editing
// it in place. The split makes the directory the answer.
//
// Classification:
//   - filename stem matches the built-in name set → templates/
//   - everything else (including renames like minimax_cn) → saved/
//   - _kernel_meta.json stays at the parent presets/ dir (the kernel
//     migration system writes there)
//
// Idempotent: any file already in templates/ or saved/ is left alone.
// Re-running is a no-op once the parent dir contains only the meta
// file and the two subdirs.
func migrateSplitPresetsDir(globalDir string) error {
	presetsDir := filepath.Join(globalDir, "presets")
	if _, err := os.Stat(presetsDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat presets dir: %w", err)
	}

	templatesDir := filepath.Join(presetsDir, "templates")
	savedDir := filepath.Join(presetsDir, "saved")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		return fmt.Errorf("create templates dir: %w", err)
	}
	if err := os.MkdirAll(savedDir, 0o755); err != nil {
		return fmt.Errorf("create saved dir: %w", err)
	}

	entries, err := os.ReadDir(presetsDir)
	if err != nil {
		return fmt.Errorf("read presets dir: %w", err)
	}

	// Hardcoded so this migration stays self-contained — it can't import
	// the preset package without creating a dependency cycle (preset
	// imports config which... fine, but globalmigrate is meant to be
	// dependency-free). Keep this list in sync with preset.builtinNames.
	builtinNames := map[string]bool{
		"minimax":     true,
		"zhipu":       true,
		"mimo":        true,
		"deepseek":    true,
		"openrouter":  true,
		"codex":       true,
		"codex_oauth": true,
		"custom":      true,
	}

	for _, e := range entries {
		if e.IsDir() {
			continue // already-migrated subdirs and anything else stay put
		}
		name := e.Name()
		if name == "_kernel_meta.json" {
			continue // meta file lives at the parent forever
		}
		if filepath.Ext(name) != ".json" {
			continue
		}
		stem := name[:len(name)-len(".json")]
		var dst string
		if builtinNames[stem] {
			dst = filepath.Join(templatesDir, name)
		} else {
			dst = filepath.Join(savedDir, name)
		}
		src := filepath.Join(presetsDir, name)
		if err := moveFile(src, dst); err != nil {
			fmt.Fprintf(os.Stderr, "globalmigrate m002: move %s → %s: %v\n",
				src, dst, err)
			continue
		}
	}
	return nil
}

// moveFile renames src→dst when on the same filesystem; falls back to
// copy+remove for cross-filesystem cases. Refuses to overwrite an
// existing destination so a re-run can't clobber a saved preset that
// happens to share a name with a template (post-migration the editor's
// auto-clone naming guarantees this doesn't happen, but better safe).
func moveFile(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		// Destination exists — likely a re-run. Silently drop the
		// source file; the user's data is already in the right place.
		return os.Remove(src)
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
