package preset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RehydrateNetwork propagates the orchestrator's init.json to every other
// agent in the network as part of the agora rehydration flow.
//
// For each non-orchestrator agent directory under lingtaiDir that has a
// .agent.json blueprint but no init.json, this function:
//
//  1. Reads the orchestrator's freshly-written init.json verbatim.
//  2. Overrides manifest.agent_name with that agent's agent_name from
//     its .agent.json blueprint (authoritative — directory name may differ).
//  3. Overrides manifest.admin with {} (empty map) so the worker is not
//     classified as an orchestrator by checkOrchestratorInvariant on the
//     next launch.
//  4. Deletes the top-level "addons" field so workers don't inherit the
//     orchestrator's addon wiring, which points at the orchestrator's
//     own addon config paths.
//  5. Writes the result to <agent>/init.json.
//
// The loop is best-effort with fail-fast semantics: the first error halts
// the loop and returns. Partial rehydration leaves the network in a mixed
// init.json state, which the next launch's invariant check will catch
// and refuse until the user runs `lingtai-tui clean`.
//
// Dot-prefixed directories under lingtaiDir (.skills, .portal, .addons,
// .tui-asset) are skipped — they are helper dirs, not agents.
func RehydrateNetwork(lingtaiDir, orchDirName string) (workersRehydrated int, err error) {
	orchInitPath := filepath.Join(lingtaiDir, orchDirName, "init.json")
	orchData, err := os.ReadFile(orchInitPath)
	if err != nil {
		return 0, fmt.Errorf("read orchestrator init.json: %w", err)
	}

	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		return 0, fmt.Errorf("read .lingtai/: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Name() == orchDirName {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, entry.Name())

		// Must have .agent.json to qualify as an agent dir. Also require
		// that .agent.json's admin field is non-nil: the human/ placeholder
		// has admin: null, which distinguishes it from real agents. This
		// matches the same rule used by main.go's isAgentDir() helper.
		blueprintPath := filepath.Join(agentDir, ".agent.json")
		blueprintData, err := os.ReadFile(blueprintPath)
		if err != nil {
			continue
		}
		var blueprint map[string]interface{}
		if err := json.Unmarshal(blueprintData, &blueprint); err != nil {
			return workersRehydrated, fmt.Errorf("parse %s: %w", blueprintPath, err)
		}
		// admin == nil (missing or explicit null) means human placeholder — skip.
		if adminRaw, ok := blueprint["admin"]; !ok || adminRaw == nil {
			continue
		}

		// Skip agents that already have init.json — rehydration is only
		// for agents in the all-absent state. In a valid rehydration run,
		// the invariant check has already ensured this is the case for
		// every agent, but we re-check defensively.
		initPath := filepath.Join(agentDir, "init.json")
		if _, err := os.Stat(initPath); err == nil {
			continue
		}

		// Deep-copy the orchestrator's init.json by re-parsing.
		var workerInit map[string]interface{}
		if err := json.Unmarshal(orchData, &workerInit); err != nil {
			return workersRehydrated, fmt.Errorf("parse orchestrator init.json: %w", err)
		}

		// Override manifest.agent_name and manifest.admin.
		manifest, ok := workerInit["manifest"].(map[string]interface{})
		if !ok {
			return workersRehydrated, fmt.Errorf("orchestrator init.json has no manifest object")
		}
		if name, ok := blueprint["agent_name"].(string); ok && name != "" {
			manifest["agent_name"] = name
		} else {
			// Fall back to directory name if .agent.json has no agent_name.
			manifest["agent_name"] = entry.Name()
		}
		manifest["admin"] = map[string]interface{}{}

		// Delete top-level addons so workers don't inherit the publisher's
		// (now the new orchestrator's) addon wiring.
		delete(workerInit, "addons")

		// Write the new init.json.
		out, err := json.MarshalIndent(workerInit, "", "  ")
		if err != nil {
			return workersRehydrated, fmt.Errorf("marshal %s/init.json: %w", entry.Name(), err)
		}
		if err := os.WriteFile(initPath, out, 0o644); err != nil {
			return workersRehydrated, fmt.Errorf("write %s/init.json: %w", entry.Name(), err)
		}
		workersRehydrated++
	}

	return workersRehydrated, nil
}
