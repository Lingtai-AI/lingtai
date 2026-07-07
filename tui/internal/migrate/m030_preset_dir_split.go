package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// migratePresetDirSplit rewrites `manifest.preset.{default,active,allowed}`
// path strings in agent init.json files to use the new templates/saved
// subdirectory layout.
//
// Background: pre-m030, the TUI wrote preset refs as
// "~/.lingtai-tui/presets/<name>.json" with templates and saved
// presets coexisting in one directory. m030 + global m002 split that
// directory into presets/templates/ and presets/saved/. Existing init
// files still point at the flat layout; this migration rewrites them.
//
// Classification: same as global m002. A path whose filename stem
// matches the built-in template name set goes to templates/; everything
// else goes to saved/. The rewrite is purely string-level — we don't
// require the new files to exist on disk yet (global m002 may run on
// the next launch when the presets dir didn't exist at TUI install
// time).
//
// Idempotent: paths that already contain "/templates/" or "/saved/"
// are left alone.
//
// Error handling: schema-critical — an init.json left pointing at the
// flat layout resolves to nothing once the directories are split, so a
// failed rewrite (marshal/write/rename) is collected and returned as an
// aggregate error rather than swallowed. The version then stays put and
// the migration re-runs on the next launch (issue #502). Unparseable
// init.json files are still warned and skipped: they predate the
// migration and a retry can never fix them.
func migratePresetDirSplit(lingtaiDir string) error {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .lingtai dir: %w", err)
	}

	var errs []error
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
			fmt.Fprintf(os.Stderr, "m030: skipping %s — unparseable init.json: %v\n",
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

		changed := false
		for _, key := range []string{"active", "default"} {
			if s, ok := preset[key].(string); ok {
				if rewritten, did := rewritePresetRef(s); did {
					preset[key] = rewritten
					changed = true
				}
			}
		}
		if al, ok := preset["allowed"].([]interface{}); ok {
			for i, e := range al {
				if s, ok := e.(string); ok {
					if rewritten, did := rewritePresetRef(s); did {
						al[i] = rewritten
						changed = true
					}
				}
			}
			preset["allowed"] = al
		}
		if !changed {
			continue
		}

		updated, err := json.MarshalIndent(init, "", "  ")
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: marshal: %w", name, err))
			continue
		}
		tmp := initPath + ".tmp"
		if err := os.WriteFile(tmp, updated, 0o644); err != nil {
			errs = append(errs, fmt.Errorf("%s: write tmp: %w", name, err))
			continue
		}
		if err := os.Rename(tmp, initPath); err != nil {
			_ = os.Remove(tmp)
			errs = append(errs, fmt.Errorf("%s: rename: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("preset-dir-split incomplete on %d agent(s): %w",
			len(errs), errors.Join(errs...))
	}
	return nil
}

// rewritePresetRef rewrites a flat-layout preset path to its new
// templates/ or saved/ home. Returns the new path and true if a
// rewrite happened. Paths that already include "/templates/" or
// "/saved/" pass through unchanged.
//
// Mirrors the IsBuiltin name list — keep in sync with
// preset.builtinNames (which lives in a different package).
func rewritePresetRef(s string) (string, bool) {
	if s == "" {
		return s, false
	}
	if strings.Contains(s, "/templates/") || strings.Contains(s, "/saved/") {
		return s, false
	}
	// Look for the legacy presets/ segment ending in a *.json path.
	const seg = "/presets/"
	idx := strings.LastIndex(s, seg)
	if idx < 0 {
		return s, false
	}
	tail := s[idx+len(seg):]
	if !strings.HasSuffix(tail, ".json") && !strings.HasSuffix(tail, ".jsonc") {
		return s, false
	}
	if strings.Contains(tail, "/") {
		return s, false // tail has its own subdir — leave alone
	}
	stem := tail
	if i := strings.LastIndex(stem, "."); i >= 0 {
		stem = stem[:i]
	}
	subdir := "saved"
	if migrateBuiltinNames[stem] {
		subdir = "templates"
	}
	return s[:idx+len(seg)] + subdir + "/" + tail, true
}

// migrateBuiltinNames mirrors preset.builtinNames. Duplicated here so
// the migration package stays decoupled from preset.
var migrateBuiltinNames = map[string]bool{
	"minimax":     true,
	"zhipu":       true,
	"mimo":        true,
	"deepseek":    true,
	"openrouter":  true,
	"codex":       true,
	"codex_oauth": true,
	"custom":      true,
}
