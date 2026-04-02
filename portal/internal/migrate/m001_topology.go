package migrate

import (
	"os"
	"path/filepath"
)

// migrateTopologyToPortal moves topology.jsonl from .tui-asset/ to .portal/.
func migrateTopologyToPortal(lingtaiDir string) error {
	oldPath := filepath.Join(lingtaiDir, ".tui-asset", "topology.jsonl")
	newDir := filepath.Join(lingtaiDir, ".portal")
	newPath := filepath.Join(newDir, "topology.jsonl")

	// If new already exists, nothing to do
	if _, err := os.Stat(newPath); err == nil {
		return nil
	}

	// If old doesn't exist, nothing to migrate (fresh install)
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return nil
	}

	// Create .portal/ directory
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return err
	}

	// Move the file
	return os.Rename(oldPath, newPath)
}
