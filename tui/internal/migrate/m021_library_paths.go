package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// migrateLibraryPaths ensures the canonical Tier-1 library paths are merged
// into manifest.capabilities.library.paths everywhere they should be — both
// in per-agent init.json files under the project and in cached preset
// templates under ~/.lingtai-tui/presets/.
//
// Background: the library capability reads its scan paths from
// manifest.capabilities.library.paths. The TUI's libraryDefault() ships
// ["../.library_shared", "~/.lingtai-tui/utilities"], but cached presets
// written by older TUI builds contain a bare `"library": {}` with no
// paths, and agents created from those presets inherit the empty config.
// Result: the skill catalog misses the shared network library and the
// TUI-shipped utility skills (export, recipe, tutorial guides, etc.).
//
// Two scopes:
//
//  1. Per-project agent init.json (walk `<lingtaiDir>/<agent>/init.json`).
//  2. Global cached presets (walk `~/.lingtai-tui/presets/*.json`). This
//     is what m017 also does — per-project invocation, global effect,
//     idempotent so repeated runs across multiple projects are safe.
//
// In both scopes: if the `library` capability is declared in any form
// (null, {}, or a populated object), we union the declared paths with
// the canonical defaults so user-added entries survive while the
// defaults are guaranteed present. If the `library` key is absent
// entirely, we leave it alone — that's a deliberate config choice.
func migrateLibraryPaths(lingtaiDir string) error {
	// Scope 1: per-agent init.json under this project.
	if entries, err := os.ReadDir(lingtaiDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if name == "" || name[0] == '.' || name == "human" {
				continue
			}
			initPath := filepath.Join(lingtaiDir, name, "init.json")
			mergeLibraryPaths(initPath, "agent "+name)
		}
	}

	// Scope 2: global cached presets.
	if globalDir := globalTUIDir(); globalDir != "" {
		presetsDir := filepath.Join(globalDir, "presets")
		if entries, err := os.ReadDir(presetsDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				presetPath := filepath.Join(presetsDir, e.Name())
				mergeLibraryPaths(presetPath, "preset "+e.Name())
			}
		}
	}

	return nil
}

// mergeLibraryPaths reads a JSON file (agent init.json or cached preset),
// merges the canonical default library paths into
// manifest.capabilities.library.paths (preserving any user-added
// entries), and writes the file back. No-op if the file is missing,
// corrupt, or lacks the library capability. `label` is a short
// human-readable tag for the log line.
func mergeLibraryPaths(path, label string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var init map[string]interface{}
	if err := json.Unmarshal(data, &init); err != nil {
		return
	}
	manifest, ok := init["manifest"].(map[string]interface{})
	if !ok {
		return
	}
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		return
	}
	libRaw, present := caps["library"]
	if !present {
		return
	}

	// Normalize library value to a map. null and non-map values get replaced
	// with a fresh map — the capability exists, we just have to store paths
	// on it.
	lib, _ := libRaw.(map[string]interface{})
	if lib == nil {
		lib = map[string]interface{}{}
	}

	defaults := []string{"../.library_shared", "~/.lingtai-tui/utilities"}

	// Pull existing paths (if any) as []string, tolerating non-string
	// elements by dropping them silently — they couldn't have been valid
	// library paths anyway.
	var existing []string
	if rawPaths, ok := lib["paths"].([]interface{}); ok {
		for _, p := range rawPaths {
			if s, ok := p.(string); ok && s != "" {
				existing = append(existing, s)
			}
		}
	}

	merged := append([]string{}, existing...)
	seen := make(map[string]bool, len(existing)+len(defaults))
	for _, p := range existing {
		seen[p] = true
	}
	for _, d := range defaults {
		if !seen[d] {
			merged = append(merged, d)
			seen[d] = true
		}
	}

	// Nothing to do if every default was already present.
	if len(merged) == len(existing) {
		return
	}

	pathsOut := make([]interface{}, len(merged))
	for i, p := range merged {
		pathsOut[i] = p
	}
	lib["paths"] = pathsOut
	caps["library"] = lib

	out, err := json.MarshalIndent(init, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Printf("  warning: failed to update %s (%s): %v\n", path, label, err)
		return
	}
	fmt.Printf("  merged library.paths defaults into %s (%s)\n", label, path)
}
