package globalmigrate

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	oldTap = "huangzesen/lingtai"
	newTap = "lingtai-ai/lingtai"
)

// migrateTapHuangzesenToLingtaiAI introduces the canonical lingtai-ai/lingtai
// Homebrew tap for users who originally installed via huangzesen/lingtai.
//
// Why not auto-untap: brew refuses to untap the source of an installed
// formula, and the only way to satisfy that constraint is to uninstall and
// reinstall the running binary mid-startup — which is unsafe to do for the
// very binary that's executing the migration. The GitHub redirect on
// huangzesen/homebrew-lingtai → Lingtai-AI/homebrew-lingtai means the old
// tap continues to receive formula updates indefinitely, so leaving it
// installed costs nothing.
//
// Behavior:
//   - No-op when brew is missing.
//   - No-op when neither tap is involved (fresh installs that haven't
//     tapped anything yet).
//   - When huangzesen/lingtai is installed and lingtai-ai/lingtai is not,
//     tap the canonical name and print a one-line manual-cleanup suggestion.
//   - When huangzesen/lingtai is installed and lingtai-ai/lingtai is already
//     tapped, just print the suggestion (no-op tap).
func migrateTapHuangzesenToLingtaiAI(globalDir string) error {
	if _, err := exec.LookPath("brew"); err != nil {
		return nil
	}

	hasOld := brewTapInstalled(oldTap)
	if !hasOld {
		// Fresh install, or user already migrated manually. Nothing to do.
		return nil
	}

	if !brewTapInstalled(newTap) {
		if err := exec.Command("brew", "tap", newTap).Run(); err != nil {
			return fmt.Errorf("brew tap %s: %w", newTap, err)
		}
		fmt.Printf("✓ tapped canonical Homebrew source: %s\n", newTap)
	}

	fmt.Printf("\nThe Lingtai project moved to a GitHub org. The old tap (%s)\n", oldTap)
	fmt.Printf("still works via redirect, so no action is required. To switch fully\n")
	fmt.Printf("to the canonical tap (%s), run:\n", newTap)
	fmt.Printf("\n    brew uninstall lingtai-tui && \\\n")
	fmt.Printf("    brew install %s/lingtai-tui && \\\n", newTap)
	fmt.Printf("    brew untap %s\n\n", oldTap)

	return nil
}

// brewTapInstalled reports whether the named tap is currently installed.
// Tap names are case-insensitive in Homebrew (always lowercased internally).
func brewTapInstalled(tap string) bool {
	out, err := exec.Command("brew", "tap").Output()
	if err != nil {
		return false
	}
	want := strings.ToLower(tap)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.ToLower(strings.TrimSpace(line)) == want {
			return true
		}
	}
	return false
}
