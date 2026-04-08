package migrate

import (
	"os"
	"path/filepath"
	"strings"
)

// addonBlockSignature is the substring used to detect a legacy addon-instruction
// block in an agent's system/comment.md. Every historical version of the now-deleted
// preset.WriteAddonComment function started its block with this exact heading,
// so this substring catches all variants. A user who writes their own custom
// comment.md is unlikely to use this exact heading.
const addonBlockSignature = "## Add-ons"

// CheckAddonComment scans .lingtai/<agent>/system/comment.md files for the
// legacy addon-instruction block. Returns paths (relative to lingtaiDir) of
// every comment.md that contains the signature substring.
//
// This function only detects; it does NOT modify any files. The caller is
// responsible for telling the user and waiting for acknowledgment.
func CheckAddonComment(lingtaiDir string) ([]string, error) {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Skip dot-prefixed entries (.portal/, .skills/, .addons/, .tui-asset/)
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		commentPath := filepath.Join(lingtaiDir, entry.Name(), "system", "comment.md")
		data, err := os.ReadFile(commentPath)
		if err != nil {
			continue // missing or unreadable — not a match
		}
		if strings.Contains(string(data), addonBlockSignature) {
			matches = append(matches, commentPath)
		}
	}
	return matches, nil
}

// IsAddonCommentNotified returns true if the user has already been notified
// about the legacy addon comment cleanup. Missing meta.json counts as
// not-yet-notified (so brand-new projects still get marked after the first
// silent check, suppressing future scans).
func IsAddonCommentNotified(lingtaiDir string) (bool, error) {
	meta, err := loadMeta(lingtaiDir)
	if err != nil {
		return false, err
	}
	return meta.AddonCommentCleanupNotified, nil
}

// MarkAddonCommentNotified persists the notification flag to meta.json so
// the check is not run again. Preserves all other meta.json fields. If
// meta.json does not exist yet, creates it with version 0 + the flag set.
func MarkAddonCommentNotified(lingtaiDir string) error {
	meta, err := loadMeta(lingtaiDir)
	if err != nil {
		return err
	}
	if meta.AddonCommentCleanupNotified {
		return nil // already marked, nothing to do
	}
	meta.AddonCommentCleanupNotified = true
	return persistMeta(lingtaiDir, meta)
}
