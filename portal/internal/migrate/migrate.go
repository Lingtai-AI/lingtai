package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CurrentVersion is the retained historical registry version. Production Portal
// startup no longer consults or stamps this registry.
// Version history:
//
//	37 — preset-skills-paths (TUI-only no-op)
//	38 — agent-init-skills-paths (shared): from PR #340
//	39 — agent-init-context-preset-repair (shared): from PR #357
//	     (both PRs independently claimed v38; resolved in fix/migration-version-collision-20260620)
const CurrentVersion = 39

type metaFile struct {
	Version int `json:"version"`
}

// Migration represents a single versioned migration step.
type Migration struct {
	Version int
	Name    string
	Fn      func(lingtaiDir string) error
}

// migrations is the ordered list of all migrations. Append-only.
var migrations = []Migration{
	{Version: 1, Name: "topology-to-portal", Fn: migrateTopologyToPortal},
	{Version: 2, Name: "tape-normalize", Fn: migrateTapeNormalize},
	{Version: 3, Name: "character-to-lingtai", Fn: migrateCharacterToLingtai},
	{Version: 4, Name: "relative-addressing", Fn: migrateRelativeAddressing},
	{Version: 5, Name: "soul-inquiry-source", Fn: func(_ string) error { return nil }},
	{Version: 6, Name: "relative-addressing-fix", Fn: migrateRelativeAddressing},
	{Version: 7, Name: "normalize-ledger", Fn: func(_ string) error { return nil }},
	{Version: 8, Name: "recipe-state", Fn: func(_ string) error { return nil }},
	{Version: 9, Name: "procedures", Fn: func(_ string) error { return nil }},
	{Version: 10, Name: "legacy-addons-warn", Fn: func(_ string) error { return nil }},
	{Version: 11, Name: "session-backfill", Fn: func(_ string) error { return nil }},
	{Version: 12, Name: "session-resort", Fn: func(_ string) error { return nil }},
	{Version: 13, Name: "agora-rename", Fn: func(_ string) error { return nil }},
	{Version: 14, Name: "skills-groups", Fn: func(_ string) error { return nil }},
	// Version 15 ("timemachine-gitignore") is a retained no-op. It once wrote
	// .lingtai/.gitignore for the Time Machine daemon, which was removed
	// entirely (issue #526). The entry MUST stay: parity_test.go requires the
	// registry to be contiguous (Version == index+1), to have exactly
	// CurrentVersion entries, and to match the TUI registry version-for-version.
	// Removing it would create a version gap and fail that invariant. This was
	// the one portal migration that actually wrote to disk; it is now inert like
	// every other portal-side no-op (portal shares the version space but not the
	// data migrations).
	{Version: 15, Name: "timemachine-gitignore", Fn: func(_ string) error { return nil }},
	{Version: 16, Name: "rename-pad-codex-library", Fn: func(_ string) error { return nil }},
	{Version: 17, Name: "rename-preset-caps", Fn: func(_ string) error { return nil }},
	{Version: 18, Name: "library-split", Fn: func(_ string) error { return nil }},
	{Version: 19, Name: "procedures-english-only", Fn: func(_ string) error { return nil }},
	{Version: 20, Name: "pseudo-agent-subscriptions", Fn: func(_ string) error { return nil }},
	{Version: 21, Name: "library-paths", Fn: func(_ string) error { return nil }},
	{Version: 22, Name: "recipe-lang-suffix", Fn: func(_ string) error { return nil }},                    // TUI-only: touches .tui-asset/.recipe
	{Version: 23, Name: "recipe-state-rename", Fn: func(_ string) error { return nil }},                   // TUI-only: renames .tui-asset/.recipe → recipe-state.json
	{Version: 24, Name: "add-active-preset", Fn: func(_ string) error { return nil }},                     // TUI-only: infers manifest.active_preset from existing init.json
	{Version: 25, Name: "preset-description-object", Fn: func(_ string) error { return nil }},             // TUI-only: promotes description to {summary, tier?} object on global preset library files
	{Version: 26, Name: "preset-path-form", Fn: migratePresetPathForm},                                    // shared: rewrites stem-form preset refs in init.json
	{Version: 27, Name: "strip-media-capabilities", Fn: migrateStripMediaCapabilities},                    // shared: drops compose/video/draw/talk/listen from init.json
	{Version: 28, Name: "addons-to-mcp", Fn: migrateAddonsToMCP},                                          // shared: rewrites legacy addons:{name:cfg} dict into addons:[name] + mcp.{name} activation entries
	{Version: 29, Name: "preset-allowed-list", Fn: migratePresetAllowedList},                              // shared: rewrites manifest.preset to {default, active, allowed:[paths]} schema
	{Version: 30, Name: "preset-dir-split", Fn: migratePresetDirSplit},                                    // shared: rewrites flat presets/ paths to templates/ or saved/ subdirs
	{Version: 31, Name: "drop-legacy-intrinsic-capabilities", Fn: migrateDropLegacyIntrinsicCapabilities}, // shared: drops psyche/email from init.json (now intrinsics)
	{Version: 32, Name: "cleanup-codex-oauth", Fn: func(_ string) error { return nil }},                   // TUI-only: renames saved/codex_oauth.json to codex.json
	{Version: 33, Name: "strip-codex-api-key-env", Fn: func(_ string) error { return nil }},               // TUI-only: strips auto-stamped CODEX_N_API_KEY from saved codex presets
	{Version: 34, Name: "library-skills-caps", Fn: func(_ string) error { return nil }},                   // TUI-only: rewrites codex/library capability keys to library/skills
	{Version: 35, Name: "remove-brief", Fn: migrateRemoveBrief},                                           // shared: strips brief.md + brief_file/brief keys after secretary removal
	{Version: 36, Name: "sqlite-log-backfill", Fn: func(_ string) error { return nil }},                   // TUI-only: optional command-line SQLite log backfill prompt/progress
	{Version: 37, Name: "preset-skills-paths", Fn: func(_ string) error { return nil }},                   // TUI-only: patches saved preset skill path overrides
	{Version: 38, Name: "agent-init-skills-paths", Fn: migrateAgentInitSkillsPaths},                       // shared: restores missing skills.paths in agent init.json (PR #340)
	{Version: 39, Name: "agent-init-context-preset-repair", Fn: migrateAgentInitContextPresetRepair},      // shared: copies legacy root context_limit into llm + rewrites stale codex preset pointers (PR #357)
}

// StampCurrent is the retained historical/test API. Production Portal startup
// no longer calls it; project files remain un-stamped.

func StampCurrent(lingtaiDir string) error {
	metaPath := filepath.Join(lingtaiDir, "meta.json")
	if _, err := os.Stat(metaPath); err == nil {
		return nil
	}
	data, err := json.Marshal(metaFile{Version: CurrentVersion})
	if err != nil {
		return err
	}
	tmpPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, metaPath)
}

// Run is the retained historical/test API. Production Portal startup has no
// caller and leaves existing project files untouched.

func Run(lingtaiDir string) error {
	metaPath := filepath.Join(lingtaiDir, "meta.json")

	current := 0
	rawMeta := map[string]json.RawMessage{}
	if data, err := os.ReadFile(metaPath); err == nil {
		if err := json.Unmarshal(data, &rawMeta); err != nil {
			return fmt.Errorf("parse meta.json: %w", err)
		}
		var m metaFile
		if err := json.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("parse meta.json: %w", err)
		}
		current = m.Version
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read meta.json: %w", err)
	}

	if current > CurrentVersion {
		return fmt.Errorf(
			"data version %d is newer than this binary supports (%d); upgrade lingtai-portal",
			current, CurrentVersion,
		)
	}

	if current == CurrentVersion {
		return nil // already up to date
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if err := m.Fn(lingtaiDir); err != nil {
			return fmt.Errorf("migration %d (%s): %w", m.Version, m.Name, err)
		}
	}

	// Write new version atomically (write temp + rename) while preserving
	// sibling fields that may be maintained by the TUI binary in the shared
	// .lingtai/meta.json file.
	if rawMeta == nil {
		rawMeta = map[string]json.RawMessage{}
	}
	versionData, err := json.Marshal(CurrentVersion)
	if err != nil {
		return fmt.Errorf("marshal meta.json version: %w", err)
	}
	rawMeta["version"] = versionData
	newMeta, err := json.Marshal(rawMeta)
	if err != nil {
		return fmt.Errorf("marshal meta.json: %w", err)
	}
	tmpPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpPath, newMeta, 0o644); err != nil {
		return fmt.Errorf("write meta.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, metaPath); err != nil {
		return fmt.Errorf("rename meta.json: %w", err)
	}

	return nil
}
