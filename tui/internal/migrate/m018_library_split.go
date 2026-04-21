package migrate

import (
	"fmt"
	"os"
	"path/filepath"
)

// migrateLibrarySplit renames the pre-existing network-level
// <project>/.lingtai/.library/ to <project>/.lingtai/.library_shared/ and
// strips TUI-managed symlinks inside it.
//
// Prior to this migration, the TUI populated .lingtai/.library/ (see m016
// for the .skills -> .library rename at this same level) with a mix of
// real skill folders and symlinks to recipe/intrinsic skills. The new
// per-agent library capability (kernel-side) owns <agent>/.library/
// independently, and this network-level directory is renamed to
// .library_shared/ to serve as the collective knowledge base.
//
// Real skill folders survive the rename. Symlinks inside are removed (they
// were TUI-managed pointers and are no longer needed).
//
// The lingtaiDir parameter IS the network root (e.g., <project>/.lingtai).
// Agents' init.json defaults library.paths to "../.library_shared", which
// from <project>/.lingtai/<agent>/ resolves to <project>/.lingtai/.library_shared/.
func migrateLibrarySplit(lingtaiDir string) error {
	oldPath := filepath.Join(lingtaiDir, ".library")
	newPath := filepath.Join(lingtaiDir, ".library_shared")

	oldInfo, err := os.Lstat(oldPath)
	if os.IsNotExist(err) {
		// Fresh network — just ensure the new path exists so kernel-side
		// scanning of ../.library_shared doesn't complain.
		return os.MkdirAll(newPath, 0o755)
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", oldPath, err)
	}
	if !oldInfo.IsDir() {
		fmt.Printf("warning: %s exists but is not a directory; skipping\n", oldPath)
		return nil
	}

	// If .library_shared already exists (e.g., partial prior run), don't
	// clobber it — just strip symlinks from the old path and leave both in
	// place. The admin can manually reconcile.
	if _, err := os.Stat(newPath); err == nil {
		fmt.Printf("warning: %s already exists; stripping symlinks from old .library but not renaming\n", oldPath)
		return stripSymlinks(oldPath)
	}

	if err := stripSymlinks(oldPath); err != nil {
		return fmt.Errorf("strip symlinks: %w", err)
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", oldPath, newPath, err)
	}

	fmt.Printf("migrated: %s -> %s\n", oldPath, newPath)
	return nil
}

// stripSymlinks removes every symlink under dir recursively, leaving real
// files and directories intact. Best-effort: errors on individual entries
// are swallowed so one stale symlink can't block the whole migration.
func stripSymlinks(dir string) error {
	return filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			os.Remove(p)
		}
		return nil
	})
}
