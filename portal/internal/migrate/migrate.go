package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CurrentVersion is the latest migration version compiled into this binary.
const CurrentVersion = 29

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
	{Version: 15, Name: "timemachine-gitignore", Fn: migrateTimeMachineGitignore},
	{Version: 16, Name: "rename-pad-codex-library", Fn: func(_ string) error { return nil }},
	{Version: 17, Name: "rename-preset-caps", Fn: func(_ string) error { return nil }},
	{Version: 18, Name: "library-split", Fn: func(_ string) error { return nil }},
	{Version: 19, Name: "procedures-english-only", Fn: func(_ string) error { return nil }},
	{Version: 20, Name: "pseudo-agent-subscriptions", Fn: func(_ string) error { return nil }},
	{Version: 21, Name: "library-paths", Fn: func(_ string) error { return nil }},
	{Version: 22, Name: "recipe-lang-suffix", Fn: func(_ string) error { return nil }}, // TUI-only: touches .tui-asset/.recipe
	{Version: 23, Name: "recipe-state-rename", Fn: func(_ string) error { return nil }}, // TUI-only: renames .tui-asset/.recipe → recipe-state.json
	{Version: 24, Name: "add-active-preset", Fn: func(_ string) error { return nil }}, // TUI-only: infers manifest.active_preset from existing init.json
	{Version: 25, Name: "preset-description-object", Fn: func(_ string) error { return nil }}, // TUI-only: promotes description to {summary, tier?} object on global preset library files
	{Version: 26, Name: "preset-path-form", Fn: migratePresetPathForm}, // shared: rewrites stem-form preset refs in init.json
	{Version: 27, Name: "strip-media-capabilities", Fn: migrateStripMediaCapabilities}, // shared: drops compose/video/draw/talk/listen from init.json
	{Version: 28, Name: "addons-to-mcp", Fn: migrateAddonsToMCP},                       // shared: rewrites legacy addons:{name:cfg} dict into addons:[name] + mcp.{name} activation entries
	{Version: 29, Name: "preset-allowed-list", Fn: migratePresetAllowedList},           // shared: rewrites manifest.preset to {default, active, allowed:[paths]} schema
}

// StampCurrent writes meta.json at CurrentVersion without running any
// migrations. Mirror of the TUI helper — kept for parity so both binaries
// know to skip migrations on fresh projects.
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

// Run executes all pending migrations on the given .lingtai/ directory.
// It reads the current version from meta.json (or assumes 0 if missing),
// runs migrations sequentially, and writes the new version atomically.
func Run(lingtaiDir string) error {
	metaPath := filepath.Join(lingtaiDir, "meta.json")

	current := 0
	if data, err := os.ReadFile(metaPath); err == nil {
		var m metaFile
		if err := json.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("parse meta.json: %w", err)
		}
		current = m.Version
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

	// Write new version atomically (write temp + rename)
	newMeta, _ := json.Marshal(metaFile{Version: CurrentVersion})
	tmpPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpPath, newMeta, 0o644); err != nil {
		return fmt.Errorf("write meta.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, metaPath); err != nil {
		return fmt.Errorf("rename meta.json: %w", err)
	}

	return nil
}
