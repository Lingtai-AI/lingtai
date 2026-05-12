package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// migrateLibrarySkillsCaps rewrites the TUI-emitted capability surface to
// match lingtai-kernel's post-rename names:
//   - codex  -> library (durable knowledge)
//   - library -> skills  (skill catalog)
//   - codex_limit -> library_limit inside the durable knowledge config
//
// The old skill catalog still lives on disk under .library/ paths; only the
// manifest capability key changes. Both project agent init.json files and the
// global preset library are idempotently patched so old TUI-created configs do
// not keep reintroducing the compatibility aliases.
func migrateLibrarySkillsCaps(lingtaiDir string) error {
	if err := migrateLibrarySkillsCapsFromAgentInits(lingtaiDir); err != nil {
		return err
	}
	if err := migrateLibrarySkillsCapsFromGlobalPresets(); err != nil {
		fmt.Fprintf(os.Stderr, "m034: global preset library cleanup: %v\n", err)
	}
	return nil
}

func migrateLibrarySkillsCapsFromAgentInits(lingtaiDir string) error {
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
		rewriteLibrarySkillsCapsFile(filepath.Join(lingtaiDir, name, "init.json"), "init.json")
	}
	return nil
}

func migrateLibrarySkillsCapsFromGlobalPresets() error {
	presetsDir := preset.PresetsDir()
	return filepath.WalkDir(presetsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d == nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		ext := filepath.Ext(name)
		if (ext != ".json" && ext != ".jsonc") || strings.HasPrefix(name, "_") {
			return nil
		}
		rewriteLibrarySkillsCapsFile(path, "preset")
		return nil
	})
}

func rewriteLibrarySkillsCapsFile(path, label string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "m034: skipping %s %s — unparseable: %v\n", label, path, err)
		return
	}
	manifest, ok := doc["manifest"].(map[string]interface{})
	if !ok {
		return
	}
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		return
	}
	if !rewriteLibrarySkillsCapsMap(caps) {
		return
	}
	updated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "m034: marshal failed for %s: %v\n", path, err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, updated, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "m034: write tmp failed for %s: %v\n", path, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		fmt.Fprintf(os.Stderr, "m034: rename failed for %s: %v\n", path, err)
		_ = os.Remove(tmp)
	}
}

func rewriteLibrarySkillsCapsMap(caps map[string]interface{}) bool {
	changed := false

	oldKnowledge, hadCodex := caps["codex"]
	if hadCodex {
		if dst, exists := caps["library"]; exists {
			caps["library"] = mergeCapabilityConfig(dst, oldKnowledge)
		} else {
			caps["library"] = oldKnowledge
		}
		delete(caps, "codex")
		changed = true
	}

	// If the old config only had the skill catalog library key and no codex
	// key, rename it wholesale unless it has a durable-library marker.
	if !hadCodex {
		if libRaw, hasLibrary := caps["library"]; hasLibrary && !looksLikeDurableLibrary(libRaw) {
			if _, hasSkills := caps["skills"]; hasSkills {
				if libCfg, ok := libRaw.(map[string]interface{}); ok {
					libCfg["library_limit"] = float64(50)
					changed = true
				}
			} else {
				caps["skills"] = libRaw
				delete(caps, "library")
				changed = true
			}
		}
	}

	if cfg, ok := caps["library"].(map[string]interface{}); ok {
		if lim, ok := cfg["codex_limit"]; ok {
			if _, exists := cfg["library_limit"]; !exists {
				cfg["library_limit"] = lim
			}
			delete(cfg, "codex_limit")
			changed = true
		}
		if hadCodex {
			if _, hasLimit := cfg["library_limit"]; !hasLimit {
				cfg["library_limit"] = float64(50)
				changed = true
			}
		}
	}

	// After the codex -> library move, a remaining paths field on library is
	// the old skill-catalog config. Move only the paths across so mixed configs
	// keep library_limit on durable library while preserving skill paths.
	if libCfg, ok := caps["library"].(map[string]interface{}); ok {
		if paths, hasPaths := libCfg["paths"]; hasPaths {
			if skillsCfg, ok := caps["skills"].(map[string]interface{}); ok {
				if _, exists := skillsCfg["paths"]; !exists {
					skillsCfg["paths"] = paths
				}
			} else if _, exists := caps["skills"]; !exists {
				caps["skills"] = map[string]interface{}{"paths": paths}
			}
			delete(libCfg, "paths")
			changed = true
		}
	}

	return changed
}

func looksLikeDurableLibrary(raw interface{}) bool {
	cfg, ok := raw.(map[string]interface{})
	if !ok {
		return false
	}
	_, hasLibraryLimit := cfg["library_limit"]
	_, hasCodexLimit := cfg["codex_limit"]
	return hasLibraryLimit || hasCodexLimit
}

func mergeCapabilityConfig(dst, src interface{}) interface{} {
	dm, dok := dst.(map[string]interface{})
	sm, sok := src.(map[string]interface{})
	if !dok || !sok {
		return src
	}
	for k, v := range sm {
		if _, exists := dm[k]; !exists {
			dm[k] = v
		}
	}
	return dm
}
