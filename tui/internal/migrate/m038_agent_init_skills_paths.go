package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// migrateAgentInitSkillsPaths is the per-project companion to m037. m037
// repaired global saved presets hit by the preset editor model-switch bug
// (PR #312), but agents materialized while the bug was live carry the same
// damage in their own init.json: a manifest.capabilities map whose skills
// entry lost its paths kwarg (or the skills entry — or the whole
// capabilities map — outright). Those agents won't pick up the repaired
// preset until their next explicit preset refresh, so the already-written
// init.json is the target that actually matters.
//
// Walks every agent directory under the project's .lingtai/ dir and adds the
// default skills paths where missing, using the same rules as m037:
//
//   - existing skills.paths values are preserved exactly
//   - other skills config and other capability entries are untouched
//   - a missing manifest.capabilities map is created
//   - a non-map manifest.capabilities (or manifest) means skip that file
//
// Best-effort: malformed init.json is logged to stderr and skipped; a single
// broken agent must not stall the migration.
//
// Shared on-disk state: the portal carries an identical copy
// (portal/internal/migrate/m038_agent_init_skills_paths.go) because whichever
// binary migrates the project first must perform the repair — keep the two
// in sync.
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
