package fs

import "path/filepath"

// ResolveAddress resolves an agent address to an absolute path.
// Relative names are joined with baseDir. Absolute paths returned as-is.
func ResolveAddress(addr, baseDir string) string {
	if filepath.IsAbs(addr) {
		return addr
	}
	return filepath.Join(baseDir, addr)
}
