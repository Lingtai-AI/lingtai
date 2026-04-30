package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// migratePresetAllowedList rewrites manifest.preset to the new
// {default, active, allowed} schema by enumerating every preset reachable
// from the legacy `path` field (string or list of directory paths) and
// recording each as an entry in `allowed`. The legacy `path` field is
// dropped — runtime authorization is now declared explicitly via the
// `allowed` list.
//
// Background: pre-m029, agents declared which presets they could use
// implicitly through `manifest.preset.path` (a directory or list of
// directories — the kernel scanned for *.json[c] files at runtime). The
// allowed-paths redesign makes registration the *only* place authorization
// is declared. Two agents can now share a library while authorizing
// different subsets of it without runtime-only side effects.
//
// Idempotent: agents whose `preset` block already has an `allowed` list
// (with `default` and `active` present in it) are skipped.
//
// Files that fail to parse, lack a `manifest.preset` block, or whose
// `default`/`active` are missing are left untouched. Best-effort: any
// per-agent error is logged to stderr and the migration continues with the
// next agent — a single broken init.json must not stall the migration.
func migratePresetAllowedList(lingtaiDir string) error {
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
			fmt.Fprintf(os.Stderr, "m029: skipping %s — unparseable init.json: %v\n",
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

		defaultRef, _ := preset["default"].(string)
		activeRef, _ := preset["active"].(string)
		if defaultRef == "" && activeRef == "" {
			continue
		}

		// Collect candidate library directories from the legacy `path` field.
		var libDirs []string
		switch p := preset["path"].(type) {
		case string:
			if p != "" {
				libDirs = append(libDirs, expandTilde(p))
			}
		case []interface{}:
			for _, e := range p {
				if s, ok := e.(string); ok && s != "" {
					libDirs = append(libDirs, expandTilde(s))
				}
			}
		}

		// Skip already-migrated agents: an `allowed` list that already
		// contains both `default` and `active` is the new shape.
		if existing, ok := preset["allowed"].([]interface{}); ok {
			seen := map[string]struct{}{}
			for _, e := range existing {
				if s, ok := e.(string); ok {
					seen[s] = struct{}{}
				}
			}
			haveDefault := defaultRef == "" || haveSeen(seen, defaultRef)
			haveActive := activeRef == "" || haveSeen(seen, activeRef)
			if haveDefault && haveActive {
				// Still drop `path` if it lingered from a partial migration.
				if _, hasPath := preset["path"]; hasPath {
					delete(preset, "path")
					if err := writePresetInit(initPath, init); err != nil {
						fmt.Fprintf(os.Stderr, "m029: write %s: %v\n", agentDir, err)
					}
				}
				continue
			}
		}

		// Build the allowed list. Order: every *.json[c] file found in the
		// legacy library directories (sorted by path for determinism), then
		// `default` and `active` appended if not already present.
		seen := map[string]struct{}{}
		var allowed []string
		appendUnique := func(s string) {
			if s == "" {
				return
			}
			if _, exists := seen[s]; exists {
				return
			}
			seen[s] = struct{}{}
			allowed = append(allowed, s)
		}

		for _, dir := range libDirs {
			files, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			// Stable order so the same library produces the same allowed list
			// across machines (filesystem listing order is not portable).
			var names []string
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				fname := f.Name()
				if fname == "_kernel_meta.json" {
					continue
				}
				if !strings.HasSuffix(fname, ".json") && !strings.HasSuffix(fname, ".jsonc") {
					continue
				}
				names = append(names, fname)
			}
			// Sort by name for stable output.
			sortStrings(names)
			for _, fname := range names {
				// Render with ~/ shorthand when under $HOME so the manifest
				// stays portable across machines (matches how the TUI writes
				// new entries via AutoEnvVarName / preset_ref formatting).
				appendUnique(homeShorten(filepath.Join(dir, fname)))
			}
		}
		appendUnique(defaultRef)
		appendUnique(activeRef)

		preset["allowed"] = allowed
		delete(preset, "path")

		if err := writePresetInit(initPath, init); err != nil {
			fmt.Fprintf(os.Stderr, "m029: write %s: %v\n", agentDir, err)
		}
	}
	return nil
}

func haveSeen(seen map[string]struct{}, key string) bool {
	_, ok := seen[key]
	return ok
}

func sortStrings(s []string) {
	// Avoid pulling in sort just for one call site; insertion sort is fine
	// for the small N of preset files. (Typical libraries hold <20 files.)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// expandTilde returns the path with a leading `~/` expanded to $HOME, or
// `~` alone expanded to $HOME. Other forms pass through unchanged.
func expandTilde(p string) string {
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

// homeShorten renders an absolute path with `~/...` shorthand when under
// $HOME, mirroring the kernel's home_shortened helper. Never returns an
// empty string for a non-empty input.
func homeShorten(p string) string {
	if p == "" {
		return p
	}
	abs := p
	if !filepath.IsAbs(abs) {
		// Best-effort absolute conversion; if it fails, return the input
		// unchanged so the migration doesn't fabricate paths.
		if a, err := filepath.Abs(p); err == nil {
			abs = a
		} else {
			return p
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return abs
	}
	rel, err := filepath.Rel(home, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return "~/" + rel
}

func writePresetInit(path string, data map[string]interface{}) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
