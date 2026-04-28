package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// migrateAddActivePreset infers manifest.active_preset for each agent in the
// project by content-matching its current manifest.llm.{provider, model}
// against entries in the user's global preset library at ~/.lingtai-tui/presets/.
//
// Skips agents that already have active_preset set, and agents whose llm doesn't
// match any preset (custom configs). Writes init.json atomically only when
// a unique match is found.
//
// Multiple matches: pick alphabetically first, warn on stderr.
func migrateAddActivePreset(lingtaiDir string) error {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .lingtai dir: %w", err)
	}

	library, err := preset.List()
	if err != nil {
		return fmt.Errorf("list presets: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Skip system-level dirs; agent dirs never start with '.' and never
		// equal 'human' (which is the human operator's mailbox shell).
		name := entry.Name()
		if name == "" || name[0] == '.' || name == "human" {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, name)
		initPath := filepath.Join(agentDir, "init.json")
		data, err := os.ReadFile(initPath)
		if err != nil {
			continue // skip dirs without init.json
		}
		var init map[string]interface{}
		if err := json.Unmarshal(data, &init); err != nil {
			fmt.Fprintf(os.Stderr, "m024: skipping %s — unparseable init.json: %v\n", agentDir, err)
			continue
		}
		manifest, ok := init["manifest"].(map[string]interface{})
		if !ok {
			continue
		}
		// Skip if already migrated
		if _, exists := manifest["active_preset"]; exists {
			continue
		}
		llm, ok := manifest["llm"].(map[string]interface{})
		if !ok {
			continue
		}
		provider, _ := llm["provider"].(string)
		model, _ := llm["model"].(string)
		if provider == "" || model == "" {
			continue
		}

		// Find matching preset by exact provider + model
		var matched []string
		for _, p := range library {
			pm, ok := p.Manifest["llm"].(map[string]interface{})
			if !ok {
				continue
			}
			pp, _ := pm["provider"].(string)
			ppmodel, _ := pm["model"].(string)
			if pp == provider && ppmodel == model {
				matched = append(matched, p.Name)
			}
		}

		switch len(matched) {
		case 0:
			// Custom config — leave alone
			continue
		case 1:
			manifest["active_preset"] = matched[0]
		default:
			// Multiple matches — pick the alphabetically first, warn
			fmt.Fprintf(os.Stderr,
				"m024: %s matches multiple presets %v — using %s\n",
				agentDir, matched, matched[0])
			manifest["active_preset"] = matched[0]
		}

		// Atomic write
		updated, err := json.MarshalIndent(init, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "m024: marshal failed for %s: %v\n", agentDir, err)
			continue
		}
		tmp := initPath + ".tmp"
		if err := os.WriteFile(tmp, updated, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "m024: write tmp failed for %s: %v\n", agentDir, err)
			continue
		}
		if err := os.Rename(tmp, initPath); err != nil {
			fmt.Fprintf(os.Stderr, "m024: rename failed for %s: %v\n", agentDir, err)
		}
	}

	return nil
}
