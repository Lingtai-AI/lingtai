// Package globalmigrate is the per-machine analogue of tui/internal/migrate.
// Its scope is global state under ~/.lingtai-tui/, not per-project state under
// each .lingtai/ directory. Versioning is tracked in ~/.lingtai-tui/meta.json.
//
// Conventions mirror tui/internal/migrate: append-only ordered slice,
// forward-only, runs once per machine on TUI launch, prints status with
// fmt.Println (no i18n — runs before the TUI renders).
package globalmigrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CurrentVersion is the latest global-migration version compiled into this binary.
const CurrentVersion = 2

type metaFile struct {
	Version int `json:"version"`
}

// Migration is a single versioned global-migration step.
type Migration struct {
	Version int
	Name    string
	Fn      func(globalDir string) error
}

// migrations is the ordered list of all global migrations. Append-only.
var migrations = []Migration{
	{Version: 1, Name: "tap-huangzesen-to-lingtai-ai", Fn: migrateTapHuangzesenToLingtaiAI},
	{Version: 2, Name: "split-presets-dir", Fn: migrateSplitPresetsDir},
}

// Run executes all pending global migrations against the given ~/.lingtai-tui/
// directory. Reads the current version from meta.json (or assumes 0), runs
// pending migrations in order, then atomically writes the new version.
//
// Failures in individual migrations are reported to stderr but do not abort
// startup — global migrations are best-effort housekeeping, not critical
// data fixes (per-project migrate.Run is the strict one).
func Run(globalDir string) {
	meta, err := loadMeta(globalDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "globalmigrate: read meta.json: %v\n", err)
		return
	}
	current := meta.Version

	if current >= CurrentVersion {
		return
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if err := m.Fn(globalDir); err != nil {
			fmt.Fprintf(os.Stderr, "globalmigrate %d (%s): %v\n", m.Version, m.Name, err)
			return
		}
		current = m.Version
	}

	meta.Version = current
	if err := persistMeta(globalDir, meta); err != nil {
		fmt.Fprintf(os.Stderr, "globalmigrate: write meta.json: %v\n", err)
	}
}

func loadMeta(globalDir string) (*metaFile, error) {
	var meta metaFile
	data, err := os.ReadFile(filepath.Join(globalDir, "meta.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &meta, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &meta, nil
}

func persistMeta(globalDir string, meta *metaFile) error {
	metaPath := filepath.Join(globalDir, "meta.json")
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmpPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmpPath, metaPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
