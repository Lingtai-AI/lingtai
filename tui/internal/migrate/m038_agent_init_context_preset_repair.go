package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// staleCodexRef is the legacy saved-preset ref that no longer resolves on
// machines whose codex preset was renamed. legacyCodexReplacement is the file
// that supersedes it when present.
const (
	staleCodexRef          = "~/.lingtai-tui/presets/saved/codex.json"
	legacyCodexReplacement = "~/.lingtai-tui/presets/saved/codex-gpt5.5.json"
)

// migrateAgentInitContextPresetRepair repairs already-created agent init.json
// files so legacy agents stop dying in refresh/relaunch loops.
//
// Two independent defects are healed, both observed in the same incident:
//
//  1. Legacy root context limit. Older init.json carried the context window
//     as manifest.context_limit at the manifest root, before the canonical
//     home moved to manifest.llm.context_limit. An agent with the root value
//     but no llm value refreshes into a relaunch-dead loop. When the root
//     value is present and llm.context_limit is absent, copy it down. The
//     root key is preserved for backward compatibility — this migration does
//     not introduce a hard failure and does not strip it. When both exist and
//     conflict, the canonical manifest.llm.context_limit wins and is never
//     overwritten.
//
//  2. Stale saved-preset pointer. The incident agent's preset active/default/
//     allowed pointed at ~/.lingtai-tui/presets/saved/codex.json, which had
//     been renamed to codex-gpt5.5.json. When the stale ref is present, or
//     active/default point at another missing preset, and the replacement file
//     exists on disk, active/default are rewritten and the allowed list gains
//     the replacement while dropping the stale entry.
//     Unrelated allowed entries that resolve to existing files are preserved.
//
// Best-effort and idempotent: a single broken init.json logs to stderr and the
// migration continues. Files are only rewritten when something actually
// changed. Atomic temp+rename writes match the surrounding migration style.
func migrateAgentInitContextPresetRepair(lingtaiDir string) error {
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
		agentDir := filepath.Join(lingtaiDir, name)
		initPath := filepath.Join(agentDir, "init.json")
		data, err := os.ReadFile(initPath)
		if err != nil {
			continue // non-agent dir (library/asset) or unreadable — skip
		}
		var init map[string]interface{}
		if err := json.Unmarshal(data, &init); err != nil {
			fmt.Fprintf(os.Stderr, "m038: skipping %s — unparseable init.json: %v\n",
				agentDir, err)
			continue
		}
		manifest, ok := init["manifest"].(map[string]interface{})
		if !ok {
			continue
		}

		changed := repairManifestContextLimit(manifest)
		if repairManifestPreset(manifest) {
			changed = true
		}
		if !changed {
			continue
		}

		if err := writePresetInit(initPath, init); err != nil {
			fmt.Fprintf(os.Stderr, "m038: write %s: %v\n", agentDir, err)
		}
	}
	return nil
}

// repairManifestContextLimit copies a legacy root manifest.context_limit into
// manifest.llm.context_limit when the llm value is absent. The canonical llm
// value is never overwritten. Returns true if the manifest was modified.
func repairManifestContextLimit(manifest map[string]interface{}) bool {
	root, hasRoot := manifest["context_limit"]
	if !hasRoot {
		return false
	}
	llm, ok := manifest["llm"].(map[string]interface{})
	if !ok {
		llm = map[string]interface{}{}
		manifest["llm"] = llm
	}
	if _, hasLLM := llm["context_limit"]; hasLLM {
		// Both present: canonical llm value wins, leave it untouched. Equal or
		// conflicting, the result is the same — never overwrite llm.
		return false
	}
	llm["context_limit"] = root
	return true
}

// repairManifestPreset rewrites stale codex.json preset pointers to the
// codex-gpt5.5.json replacement when that file exists. Returns true if the
// manifest was modified.
func repairManifestPreset(manifest map[string]interface{}) bool {
	preset, ok := manifest["preset"].(map[string]interface{})
	if !ok {
		return false
	}
	// Only act when the replacement actually exists on disk; otherwise leave
	// the pointers alone rather than fabricating a dead ref.
	if !presetRefExists(legacyCodexReplacement) {
		return false
	}

	changed := false
	needsReplacementAllowed := false
	for _, key := range []string{"active", "default"} {
		if s, ok := preset[key].(string); ok && presetRefNeedsRepair(s) {
			preset[key] = legacyCodexReplacement
			changed = true
			needsReplacementAllowed = true
		}
	}

	if rawAllowed, ok := preset["allowed"].([]interface{}); ok {
		var rebuilt []interface{}
		seen := map[string]struct{}{}
		appendUnique := func(s string) {
			if _, dup := seen[s]; dup {
				return
			}
			seen[s] = struct{}{}
			rebuilt = append(rebuilt, s)
		}
		droppedStale := false
		for _, e := range rawAllowed {
			s, ok := e.(string)
			if !ok {
				rebuilt = append(rebuilt, e) // preserve non-string entries verbatim
				continue
			}
			if refIsStaleCodex(s) {
				droppedStale = true
				continue // drop the stale codex.json pointer
			}
			appendUnique(s)
		}
		if droppedStale || needsReplacementAllowed {
			// Ensure the existing replacement is authorized. Prepend it so it
			// sits where the stale entry used to be (entry ordering is not
			// semantically significant, but keeping the replacement visible is
			// friendlier).
			if _, present := seen[legacyCodexReplacement]; !present {
				rebuilt = append([]interface{}{legacyCodexReplacement}, rebuilt...)
			}
			preset["allowed"] = rebuilt
			changed = true
		}
	} else if needsReplacementAllowed {
		preset["allowed"] = []interface{}{legacyCodexReplacement}
		changed = true
	}

	return changed
}

// presetRefNeedsRepair reports whether a required active/default preset ref
// should be rewritten to the known-good codex-gpt5.5 replacement. The explicit
// stale codex ref is always repaired; other refs are repaired only when they do
// not resolve to a file.
func presetRefNeedsRepair(ref string) bool {
	if refIsStaleCodex(ref) {
		return true
	}
	return !presetRefExists(ref)
}

// refIsStaleCodex reports whether a preset ref points at the renamed
// codex.json, comparing tilde and absolute forms equal.
func refIsStaleCodex(ref string) bool {
	return presetRefsEqual(ref, staleCodexRef)
}

// presetRefsEqual compares two preset refs for equality after tilde expansion,
// so ~/-prefixed and absolute forms of the same path compare equal.
func presetRefsEqual(a, b string) bool {
	return expandTilde(a) == expandTilde(b)
}

// presetRefExists reports whether a preset ref resolves to an existing file.
func presetRefExists(ref string) bool {
	if ref == "" {
		return false
	}
	info, err := os.Stat(expandTilde(ref))
	return err == nil && !info.IsDir()
}
