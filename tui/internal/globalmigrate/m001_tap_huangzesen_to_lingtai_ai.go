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

// migrateTapHuangzesenToLingtaiAI moves users off the legacy huangzesen/lingtai
// Homebrew tap onto lingtai-ai/lingtai after the GitHub org transfer. The
// GitHub redirect keeps the legacy tap working, but the canonical tap shows
// the right org in `brew info` / `brew tap` and protects against the day
// GitHub eventually drops the redirect.
//
// No-op when:
//   - brew is not installed (Linux source builds, Windows)
//   - the legacy tap is not installed (fresh installs)
//
// On a real migration, this taps the new repo first (so the user is never
// left without a tap providing lingtai-tui), then untaps the old one.
func migrateTapHuangzesenToLingtaiAI(globalDir string) error {
	if _, err := exec.LookPath("brew"); err != nil {
		return nil
	}

	if !brewTapInstalled(oldTap) {
		return nil
	}

	if !brewTapInstalled(newTap) {
		if err := exec.Command("brew", "tap", newTap).Run(); err != nil {
			return fmt.Errorf("brew tap %s: %w", newTap, err)
		}
	}

	if err := exec.Command("brew", "untap", oldTap).Run(); err != nil {
		return fmt.Errorf("brew untap %s: %w", oldTap, err)
	}

	fmt.Printf("✓ migrated Homebrew tap: %s → %s\n", oldTap, newTap)
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
