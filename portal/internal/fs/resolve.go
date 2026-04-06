package fs

import (
	"path/filepath"
	"strings"
)

// ResolveAddress resolves an agent address to an absolute path.
// Relative names are joined with baseDir. Absolute paths returned as-is.
func ResolveAddress(addr, baseDir string) string {
	if filepath.IsAbs(addr) {
		return addr
	}
	return filepath.Join(baseDir, addr)
}

// RelativizeAddress converts an absolute address to a relative name
// by stripping the baseDir prefix. If the address is already relative
// or doesn't start with baseDir, it's returned as-is.
func RelativizeAddress(addr, baseDir string) string {
	if !filepath.IsAbs(addr) {
		return addr
	}
	prefix := baseDir + "/"
	if strings.HasPrefix(addr, prefix) {
		return addr[len(prefix):]
	}
	return addr
}
