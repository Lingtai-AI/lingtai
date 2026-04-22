package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// migratePseudoAgentSubscriptions adds a default
// `manifest.pseudo_agent_subscriptions: ["../human"]` to every agent's
// init.json that doesn't already have the field.
//
// Background: the pseudo-agent outbox subscription feature (shipped in
// TUI and lingtai-kernel) introduces a new manifest field that tells the
// kernel which sibling folders' outboxes to poll. The kernel defaults
// the field to `["../human"]` when missing, so runtime behavior is
// correct for pre-existing agents even without this migration. This
// migration exists purely to keep the on-disk init.json files in sync
// with the TUI's shipped template so the user sees a consistent config.
//
// Only agent directories (siblings of `human` under `.lingtai/`) are
// touched. The `human` folder has no init.json. Hidden / dotfile dirs
// and the `.addons/.library/.library_shared` infrastructure dirs are
// skipped.
func migratePseudoAgentSubscriptions(lingtaiDir string) error {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "" || name[0] == '.' || name == "human" {
			continue
		}
		initPath := filepath.Join(lingtaiDir, name, "init.json")
		addPseudoAgentSubscriptions(initPath)
	}
	return nil
}

// addPseudoAgentSubscriptions reads init.json, adds
// manifest.pseudo_agent_subscriptions = ["../human"] if not present, and
// writes the file back. No-op if init.json is missing, corrupt, or the
// field already exists.
func addPseudoAgentSubscriptions(path string) {
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
	if _, present := manifest["pseudo_agent_subscriptions"]; present {
		return
	}
	manifest["pseudo_agent_subscriptions"] = []interface{}{"../human"}

	out, err := json.MarshalIndent(init, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Printf("  warning: failed to update %s: %v\n", path, err)
		return
	}
	fmt.Printf("  added pseudo_agent_subscriptions to %s\n", path)
}
