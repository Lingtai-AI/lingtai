package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// migrateDropLegacyIntrinsicCapabilities removes capability entries for
// `psyche` and `email`, both of which were promoted from wrapper
// capabilities to kernel intrinsics in lingtai-kernel 0.7.5.
//
// Background: psyche and email were always-on wrappers that overrode
// the `eigen` and `mail` kernel intrinsics with richer implementations.
// The wrappers were deleted; the rich versions now ARE the intrinsics.
// The wrapper capability registry no longer lists them, so existing
// init.json files referencing them would fail with `Unknown capability:
// psyche` (or email) on agent spawn.
//
// Strip-only: no replacement registration is needed. The intrinsics are
// always wired by base_agent during agent construction.
//
// Portal scope: only per-agent init.json under lingtaiDir. The TUI binary
// additionally cleans up the global preset library at ~/.lingtai-tui/
// presets/ — portal cannot import that package without breaking layering.
//
// Idempotent.
func migrateDropLegacyIntrinsicCapabilities(lingtaiDir string) error {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .lingtai dir: %w", err)
	}

	removed := []string{"psyche", "email"}

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
			fmt.Fprintf(os.Stderr, "m031: skipping %s — unparseable init.json: %v\n",
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
			fmt.Fprintf(os.Stderr, "m031: marshal failed for %s: %v\n", initPath, err)
			continue
		}

		tmp := initPath + ".tmp"
		if err := os.WriteFile(tmp, updated, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "m031: write tmp failed for %s: %v\n", initPath, err)
			continue
		}
		if err := os.Rename(tmp, initPath); err != nil {
			fmt.Fprintf(os.Stderr, "m031: rename failed for %s: %v\n", initPath, err)
			_ = os.Remove(tmp)
		}
	}

	return nil
}
