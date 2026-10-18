//go:build !windows

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestSettingsUpdateRefusesStaticFIFOWithoutOpening(t *testing.T) {
	m, path, original := repairSettingsModel(t)
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	// No writer/goroutine: the owner type gate must refuse before opening.
	assertRepairSettingsFailure(t, m, original, false)
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("FIFO replaced")
	}
}

func TestSettingsUpdateReportsPrecedingPsycheWriteOnLateInitFailure(t *testing.T) {
	m, path, original := repairSettingsModel(t)
	// Only the agent root loses write permission; its existing settings
	// subdirectory remains writable. This forces the second independent write
	// to fail after the first without a process-global fault injection seam.
	if err := os.Chmod(m.orchDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(m.orchDir, 0700) })
	probe := filepath.Join(m.orchDir, "permission-probe")
	if f, err := os.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600); err == nil {
		_ = f.Close()
		_ = os.Remove(probe)
		t.Skip("host bypasses directory permission fault (e.g. root)")
	}
	assertRepairSettingsFailure(t, m, original, true)
	owner, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(owner), `"covenant_file"`) {
		t.Fatalf("preceding Psyche write missing: %s, %v", owner, err)
	}
}
