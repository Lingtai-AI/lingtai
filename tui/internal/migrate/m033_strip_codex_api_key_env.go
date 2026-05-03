package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// migrateStripCodexAPIKeyEnv clears manifest.llm.api_key_env on any
// saved/codex.json (provider=codex preset) — codex uses ChatGPT OAuth
// tokens at ~/.lingtai-tui/codex-auth.json, not an env-var API key.
//
// Pre-2026-05 versions of the editor's stampAutoEnvVar helper assigned
// CODEX_N_API_KEY slots to codex presets that arrived without api_key_env
// (true for the template). When such a stamped preset went into init.json,
// presetNeedsKey returned true and the wizard routed users to a paste-key
// page that has nothing to do with codex. Strip the bogus stamp so that
// saved codex presets match the template's empty-env-var shape.
//
// Runs once globally on first launch of the new TUI build.
func migrateStripCodexAPIKeyEnv(lingtaiDir string) error {
	presetDir := filepath.Join(filepath.Dir(lingtaiDir), "presets", "saved")
	entries, err := os.ReadDir(presetDir)
	if err != nil {
		return nil // best-effort
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(presetDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		manifest, _ := raw["manifest"].(map[string]interface{})
		if manifest == nil {
			continue
		}
		llm, _ := manifest["llm"].(map[string]interface{})
		if llm == nil {
			continue
		}
		provider, _ := llm["provider"].(string)
		if provider != "codex" {
			continue
		}
		if envName, _ := llm["api_key_env"].(string); envName == "" {
			continue
		}
		llm["api_key_env"] = ""
		newData, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			continue
		}
		_ = os.WriteFile(path, newData, 0o644)
	}
	return nil
}
