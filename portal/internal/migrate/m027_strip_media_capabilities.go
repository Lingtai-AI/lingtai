package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// migrateStripMediaCapabilities removes capability entries promoted out of
// lingtai-kernel into TUI-side skills: `compose`, `video`, `draw`, `talk`,
// `listen`, `web_read` (the last folded into the `web-browsing` skill).
//
// Strip-only: no MCP server registration is added. Agents that need media
// generation can register the MiniMax-Media MCP server manually via the
// `lingtai-mcp` skill.
//
// Portal scope: only per-agent init.json under lingtaiDir. The TUI binary
// additionally cleans up the global preset library at ~/.lingtai-tui/
// presets/ — portal cannot import that package without breaking layering,
// and since either binary may run first, the TUI handles that side.
//
// Idempotent.
func migrateStripMediaCapabilities(lingtaiDir string) error {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .lingtai dir: %w", err)
	}

	removed := []string{"compose", "video", "draw", "talk", "listen", "web_read"}

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
			fmt.Fprintf(os.Stderr, "m027: skipping %s — unparseable init.json: %v\n",
				agentDir, err)
			continue
		}

		manifest, ok := init["manifest"].(map[string]interface{})
		if !ok {
			continue
		}
		caps, ok := manifest["capabilities"].(map[string]interface{})
		if !ok {
			continue
		}

		changed := false
		for _, key := range removed {
			if _, exists := caps[key]; exists {
				delete(caps, key)
				changed = true
			}
		}
		if !changed {
			continue
		}

		updated, err := json.MarshalIndent(init, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "m027: marshal failed for %s: %v\n", initPath, err)
			continue
		}

		tmp := initPath + ".tmp"
		if err := os.WriteFile(tmp, updated, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "m027: write tmp failed for %s: %v\n", initPath, err)
			continue
		}
		if err := os.Rename(tmp, initPath); err != nil {
			fmt.Fprintf(os.Stderr, "m027: rename failed for %s: %v\n", initPath, err)
			_ = os.Remove(tmp)
		}
	}

	return nil
}
