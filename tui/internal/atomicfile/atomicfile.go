// Package atomicfile provides a crash-safe file write primitive shared by the
// config-writing paths in the TUI. Critical config files (init.json,
// .agent.json) MUST be written through Write: a partial write to one of them
// leaves the agent unlaunchable, so a mid-write crash or power loss must never
// leave the target truncated, empty, or half-serialized.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write writes data to path using the temp-file-plus-rename pattern.
//
// The temp file is created alongside path (rename is only atomic within a
// single filesystem) with a unique name, so concurrent writers to the same
// target never collide on a shared ".tmp" sidecar and clobber each other's
// partial writes. On any failure the temp file is best-effort removed and the
// existing target is left untouched — a failed write never destroys the
// current config.
//
// perm is the mode used when path does not yet exist. When path already
// exists, its current mode is preserved so an atomic rewrite does not silently
// widen or narrow permissions.
func Write(path string, data []byte, perm os.FileMode) error {
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}

	dir := filepath.Dir(path)
	// A unique temp file per write. Using os.CreateTemp guarantees the name is
	// unique in dir, so parallel writers to the same target each get their own
	// sidecar rather than racing on a fixed path+".tmp".
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()

	// From here on any early return must clean up the temp file.
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	// Flush to disk before the rename so the rename cannot expose an
	// unsynced (potentially empty) file after a crash.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		cleanup()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}
