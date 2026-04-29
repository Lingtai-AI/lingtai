package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// migrateStripMediaCapabilities removes capability entries that were
// promoted out of lingtai-kernel into TUI-side skills:
//   - compose / video / draw / talk → minimax-token-plan skill
//   - listen → listen skill
//   - web_read → web-browsing skill (its trafilatura logic moved into
//     the skill's scripts/extract.py path; the LLM still gets ranked
//     search via the web_search tool, which now points at web-browsing
//     for URL fetching).
//
// Background: the four media-generation modalities were thin wrappers
// around the MiniMax-Media MCP server, and `listen` was a thin wrapper
// around two local-only Python libraries (faster-whisper + librosa).
// All five were folded into TUI-side skills in tui/internal/preset/skills/.
// Existing agent init.json manifests still reference the capability names
// — without this migration, the kernel would error with
// `Unknown capability: compose` on agent spawn.
//
// Strip-only: no MCP server registration is added. Agents that need media
// generation can register the MiniMax-Media MCP server manually via the
// `lingtai-mcp` skill (or an equivalent for another provider).
//
// Two scopes are touched:
//   1. Per-agent init.json under lingtaiDir (each agent dir's manifest)
//   2. The global preset library at ~/.lingtai-tui/presets/ (shipped on
//      firstrun by older TUI builds — left alone, those preset files
//      keep re-introducing the dropped caps when a user activates one)
//
// Both passes are idempotent.
func migrateStripMediaCapabilities(lingtaiDir string) error {
	if err := stripMediaCapsFromAgentInits(lingtaiDir); err != nil {
		return err
	}
	if err := stripMediaCapsFromGlobalPresets(); err != nil {
		// Global library may not exist (e.g. fresh install); a hard failure
		// here would block the per-project bump. Log and continue.
		fmt.Fprintf(os.Stderr, "m027: global preset library cleanup: %v\n", err)
	}
	return nil
}

func stripMediaCapsFromAgentInits(lingtaiDir string) error {
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
		stripCapsFromManifestFile(initPath, removed, "init.json")
	}
	return nil
}

func stripMediaCapsFromGlobalPresets() error {
	presetsDir := preset.PresetsDir()
	entries, err := os.ReadDir(presetsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read presets dir: %w", err)
	}

	removed := []string{"compose", "video", "draw", "talk", "listen", "web_read"}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".json" && ext != ".jsonc" {
			continue
		}
		if e.Name() == "_kernel_meta.json" {
			continue
		}
		path := filepath.Join(presetsDir, e.Name())
		stripCapsFromManifestFile(path, removed, "preset")
	}
	return nil
}

// stripCapsFromManifestFile mutates a JSON file with shape
// {"manifest": {"capabilities": {...}, ...}, ...} to remove the named
// capability keys. Logs and continues on any error so a single bad file
// doesn't block the rest. label is used in error messages.
func stripCapsFromManifestFile(path string, removed []string, label string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "m027: skipping %s %s — unparseable: %v\n", label, path, err)
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
	changed := false
	for _, key := range removed {
		if _, exists := caps[key]; exists {
			delete(caps, key)
			changed = true
		}
	}
	if !changed {
		return
	}
	updated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "m027: marshal failed for %s: %v\n", path, err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, updated, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "m027: write tmp failed for %s: %v\n", path, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		fmt.Fprintf(os.Stderr, "m027: rename failed for %s: %v\n", path, err)
		_ = os.Remove(tmp)
	}
}
