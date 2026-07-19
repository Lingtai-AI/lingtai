//go:build windows

package tui

import (
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/duplaunch"
)

// tryLock on Windows reuses the kernel byte-0 lock authority.
func tryLock(path string) bool {
	decision := duplaunch.Check(filepath.Dir(path))
	return decision.Verdict == duplaunch.Allow
}
