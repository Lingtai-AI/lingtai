package migrate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// migrateDropLegacyIntrinsicCapabilities removes capability entries for
// `psyche` and `email`, both of which were promoted from wrapper
// capabilities to kernel intrinsics in lingtai-kernel 0.7.5.
//
// Background: psyche and email were always-on wrappers that overrode the
// `eigen` and `mail` kernel intrinsics with richer implementations. The
// wrappers were deleted; the rich versions now ARE the intrinsics. The
// wrapper capability registry no longer lists them, so existing
// init.json files (and global preset files) that still reference them
// would fail with `Unknown capability: psyche` (or email) on agent
// spawn.
//
// Strip-only: no replacement registration is needed. The intrinsics are
// always wired by base_agent during construction.
//
// Two scopes are touched (mirrors m027 / strip-media-capabilities):
//   1. Per-agent init.json under lingtaiDir
//   2. The global preset library at ~/.lingtai-tui/presets/
//
// Both passes are idempotent.
func migrateDropLegacyIntrinsicCapabilities(lingtaiDir string) error {
	if err := dropLegacyIntrinsicCapsFromAgentInits(lingtaiDir); err != nil {
		return err
	}
	if err := dropLegacyIntrinsicCapsFromGlobalPresets(); err != nil {
		// Global library may not exist; a hard failure here would block
		// the per-project bump. Log and continue.
		fmt.Fprintf(os.Stderr, "m031: global preset library cleanup: %v\n", err)
	}
	return nil
}

func dropLegacyIntrinsicCapsFromAgentInits(lingtaiDir string) error {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .lingtai dir: %w", err)
	}

	removed := []string{"psyche", "email"}

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

func dropLegacyIntrinsicCapsFromGlobalPresets() error {
	presetsDir := preset.PresetsDir()
	entries, err := os.ReadDir(presetsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read presets dir: %w", err)
	}

	removed := []string{"psyche", "email"}

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
