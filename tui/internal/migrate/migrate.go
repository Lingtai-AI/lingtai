package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CurrentVersion is the latest migration version compiled into this binary.
const CurrentVersion = 24

type metaFile struct {
	Version                       int  `json:"version"`
	AddonCommentCleanupNotified  bool `json:"addon_comment_cleanup_notified,omitempty"`
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
	{Version: 2, Name: "tape-normalize", Fn: func(_ string) error { return nil }},
	{Version: 3, Name: "character-to-lingtai", Fn: migrateCharacterToLingtai},
	{Version: 4, Name: "soul-inquiry-source", Fn: migrateSoulInquirySource},
	{Version: 5, Name: "relative-addressing", Fn: migrateRelativeAddressing},
	{Version: 6, Name: "relative-addressing-fix", Fn: migrateRelativeAddressing},
	{Version: 7, Name: "normalize-ledger", Fn: migrateNormalizeLedger},
	{Version: 8, Name: "recipe-state", Fn: migrateRecipeState},
	{Version: 9, Name: "procedures", Fn: migrateProcedures},
	{Version: 10, Name: "legacy-addons-warn", Fn: migrateLegacyAddonsWarn},
	{Version: 11, Name: "session-backfill", Fn: migrateSessionBackfill},
	{Version: 12, Name: "session-resort", Fn: migrateSessionResort},
	{Version: 13, Name: "agora-rename", Fn: migrateAgoraRename},
	{Version: 14, Name: "skills-groups", Fn: migrateSkillsGroups},
	{Version: 15, Name: "timemachine-gitignore", Fn: migrateTimeMachineGitignore},
	{Version: 16, Name: "rename-pad-codex-library", Fn: migrateRenamePadCodexLibrary},
	{Version: 17, Name: "rename-preset-caps", Fn: migrateRenamePresetCaps},
	{Version: 18, Name: "library-split", Fn: migrateLibrarySplit},
	{Version: 19, Name: "procedures-english-only", Fn: migrateProceduresEnglishOnly},
	{Version: 20, Name: "pseudo-agent-subscriptions", Fn: migratePseudoAgentSubscriptions},
	{Version: 21, Name: "library-paths", Fn: migrateLibraryPaths},
	{Version: 22, Name: "recipe-lang-suffix", Fn: migrateRecipeLangSuffix},
	{Version: 23, Name: "recipe-state-rename", Fn: migrateRecipeStateRename},
	{Version: 24, Name: "add-active-preset", Fn: migrateAddActivePreset},
}

// Run executes all pending migrations on the given .lingtai/ directory.
// It reads the current version from meta.json (or assumes 0 if missing),
// runs migrations sequentially, and writes the new version atomically.
// Preserves all sibling fields in meta.json (e.g. addon_comment_cleanup_notified)
// across the version bump.
func Run(lingtaiDir string) error {
	metaPath := filepath.Join(lingtaiDir, "meta.json")

	var meta metaFile
	if data, err := os.ReadFile(metaPath); err == nil {
		if err := json.Unmarshal(data, &meta); err != nil {
			return fmt.Errorf("parse meta.json: %w", err)
		}
	}
	current := meta.Version

	if current > CurrentVersion {
		return fmt.Errorf(
			"data version %d is newer than this binary supports (%d); upgrade lingtai-tui",
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

	// Bump version while preserving sibling fields, then write atomically.
	meta.Version = CurrentVersion
	return persistMeta(lingtaiDir, &meta)
}

// StampCurrent writes meta.json at CurrentVersion without running any
// migrations. Called by InitProject when a fresh .lingtai/ directory is
// created — a freshly-generated project conforms to the current schema
// by construction, so running historical migrations against it would
// corrupt otherwise-valid data (e.g. the pre-m016 "library" key meant
// "knowledge archive" but post-m016 it means "skill library", and m016
// unconditionally renames library→codex).
//
// No-op if meta.json already exists — upgrade paths stay authoritative
// for projects created by older TUI binaries.
func StampCurrent(lingtaiDir string) error {
	metaPath := filepath.Join(lingtaiDir, "meta.json")
	if _, err := os.Stat(metaPath); err == nil {
		return nil
	}
	return persistMeta(lingtaiDir, &metaFile{Version: CurrentVersion})
}

// loadMeta reads meta.json. Returns a zero metaFile if the file is missing.
func loadMeta(lingtaiDir string) (*metaFile, error) {
	var meta metaFile
	data, err := os.ReadFile(filepath.Join(lingtaiDir, "meta.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &meta, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse meta.json: %w", err)
	}
	return &meta, nil
}

// persistMeta serializes meta.json atomically (temp + rename).
func persistMeta(lingtaiDir string, meta *metaFile) error {
	metaPath := filepath.Join(lingtaiDir, "meta.json")
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal meta.json: %w", err)
	}
	tmpPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write meta.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, metaPath); err != nil {
		return fmt.Errorf("rename meta.json: %w", err)
	}
	return nil
}
