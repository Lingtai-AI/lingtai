package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var defaultPresetSkillsPaths = []interface{}{
	"../.library_shared",
	"~/.lingtai-tui/utilities",
}

// migrateAgentInitSkillsPaths is the portal copy of TUI m038 (see
// tui/internal/migrate/m038_agent_init_skills_paths.go — keep the logic
// identical). Agents materialized while the preset editor model-switch bug
// (PR #312) was live carry damaged init.json: a manifest.capabilities map
// whose skills entry lost its paths kwarg (or the skills entry — or the
// whole capabilities map — outright).
//
// This repair touches shared on-disk state, so whichever binary migrates
// the project first must perform it: a no-op stub here would let a
// portal-first launch stamp meta.json at 38 and silently skip the TUI's
// repair forever.
//
// Walks every agent directory under the project's .lingtai/ dir and adds
// the default skills paths where missing:
//
//   - existing skills.paths values are preserved exactly
//   - other skills config and other capability entries are untouched
//   - a missing manifest.capabilities map is created
//   - a non-map manifest.capabilities (or manifest) means skip that file
//
// Best-effort: malformed init.json is logged to stderr and skipped; a single
// broken agent must not stall the migration.
func migrateAgentInitSkillsPaths(lingtaiDir string) error {
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
		patchAgentInitSkillsPathsFile(filepath.Join(lingtaiDir, name, "init.json"))
	}
	return nil
}

func patchAgentInitSkillsPathsFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // dirs without init.json are not agents
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "m038: skipping %s — unparseable init.json: %v\n", path, err)
		return
	}
	manifest, ok := doc["manifest"].(map[string]interface{})
	if !ok {
		return
	}
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		if _, exists := manifest["capabilities"]; exists {
			return // non-map capabilities: not a shape we understand, leave alone
		}
		caps = map[string]interface{}{}
		manifest["capabilities"] = caps
	}
	if !patchPresetSkillsPathsMap(caps) {
		return
	}
	updated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "m038: marshal failed for %s: %v\n", path, err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, updated, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "m038: write tmp failed for %s: %v\n", path, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		fmt.Fprintf(os.Stderr, "m038: rename failed for %s: %v\n", path, err)
		_ = os.Remove(tmp)
	}
}

// patchPresetSkillsPathsMap mirrors the TUI m037 helper of the same name:
// ensure caps has a skills entry whose paths kwarg exists, without touching
// any existing paths value or sibling config. Reports whether caps changed.
func patchPresetSkillsPathsMap(caps map[string]interface{}) bool {
	skillsRaw, hasSkills := caps["skills"]
	if !hasSkills {
		caps["skills"] = map[string]interface{}{"paths": append([]interface{}{}, defaultPresetSkillsPaths...)}
		return true
	}
	skillsCfg, ok := skillsRaw.(map[string]interface{})
	if !ok {
		return false
	}
	if _, hasPaths := skillsCfg["paths"]; hasPaths {
		return false
	}
	skillsCfg["paths"] = append([]interface{}{}, defaultPresetSkillsPaths...)
	return true
}
