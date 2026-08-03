package tui

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setTestHome redirects the user's home directory to dir for the duration of
// the test, on every platform.
//
// t.Setenv("HOME", dir) alone is NOT enough. The code under test resolves the
// home directory through os.UserHomeDir() — preset.PresetsDir/SavedDir,
// config.GlobalDirPath, and codexAuthRefForPath all do — and os.UserHomeDir is
// platform-specific:
//
//   - unix/darwin: $HOME
//   - windows:     $USERPROFILE ($HOME is never consulted)
//   - plan9:       $home
//
// So on Windows a HOME-only redirect is a silent no-op: every test in the
// package reads and writes the *real* %USERPROFILE%\.lingtai-tui tree, which
// both pollutes the developer's own presets/credentials and makes tests clobber
// each other's state (leftover saved presets break count and consensus
// assertions in later tests). Setting the platform's actual variables keeps the
// redirect honest everywhere.
//
// t.Setenv is used throughout, so the values are restored automatically and the
// calling test must not be parallel.
func setTestHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", dir)
		// os.UserHomeDir itself only reads USERPROFILE, but os/user and plenty
		// of spawned tooling still consult HOMEDRIVE+HOMEPATH. Keep them
		// consistent so no resolution order escapes the temp tree.
		vol := filepath.VolumeName(dir)
		t.Setenv("HOMEDRIVE", vol)
		t.Setenv("HOMEPATH", strings.TrimPrefix(dir, vol))
	case "plan9":
		t.Setenv("home", dir)
	}
}

// tempTestHome creates a fresh temp dir and points the user home at it via
// setTestHome, returning the directory.
func tempTestHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	setTestHome(t, dir)
	return dir
}
