package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeKernelCheckout writes a minimal lingtai-kernel checkout (pyproject.toml +
// sibling lingtai/ TUI repo) under base/<rel> and returns the kernel dir.
func makeKernelCheckout(t *testing.T, base, rel string) string {
	t.Helper()
	root := filepath.Join(base, rel)
	kernel := filepath.Join(root, "lingtai-kernel")
	tui := filepath.Join(root, "lingtai")
	if err := os.MkdirAll(kernel, 0o755); err != nil {
		t.Fatalf("mkdir kernel: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tui, "tui"), 0o755); err != nil {
		t.Fatalf("mkdir tui: %v", err)
	}
	if err := os.WriteFile(filepath.Join(kernel, "pyproject.toml"), []byte("[project]\nname = \"lingtai\"\n"), 0o644); err != nil {
		t.Fatalf("write pyproject: %v", err)
	}
	return kernel
}

func TestFindDevCheckoutsRequiresExplicitEnv(t *testing.T) {
	home := t.TempDir()
	makeKernelCheckout(t, home, filepath.Join("work", "GitHub"))
	lookupEnv := func(string) (string, bool) { return "", false }

	if _, ok := findDevCheckouts(home, lookupEnv); ok {
		t.Fatalf("expected no dev checkouts without LINGTAI_DEV_ROOT")
	}
}

func TestFindDevCheckoutsReturnsFalseWhenAbsent(t *testing.T) {
	home := t.TempDir()
	lookupEnv := func(string) (string, bool) { return "", false }
	if _, ok := findDevCheckouts(home, lookupEnv); ok {
		t.Fatalf("expected no dev checkouts in an empty home")
	}
}

func TestFindDevCheckoutsRequiresKernelPyproject(t *testing.T) {
	home := t.TempDir()
	// Kernel dir + sibling TUI repo exist, but no pyproject.toml in the kernel.
	root := filepath.Join(home, "devroot")
	if err := os.MkdirAll(filepath.Join(root, "lingtai-kernel"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "lingtai", "tui"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lookupEnv := func(key string) (string, bool) {
		if key == "LINGTAI_DEV_ROOT" {
			return root, true
		}
		return "", false
	}
	if _, ok := findDevCheckouts(home, lookupEnv); ok {
		t.Fatalf("expected dev mode to be invalid without kernel pyproject.toml")
	}
}

func TestFindDevCheckoutsRequiresSiblingTUIRepo(t *testing.T) {
	home := t.TempDir()
	// Kernel with pyproject exists, but no sibling lingtai/ TUI repo.
	devRoot := filepath.Join(home, "devroot")
	root := filepath.Join(devRoot, "lingtai-kernel")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("name=lingtai"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	lookupEnv := func(key string) (string, bool) {
		if key == "LINGTAI_DEV_ROOT" {
			return devRoot, true
		}
		return "", false
	}
	if _, ok := findDevCheckouts(home, lookupEnv); ok {
		t.Fatalf("expected dev mode to be invalid without a sibling lingtai/ repo")
	}
}

func TestFindDevCheckoutsHonorsEnvOverride(t *testing.T) {
	home := t.TempDir()
	// Put the checkouts somewhere the default search would never look.
	other := t.TempDir()
	wantKernel := makeKernelCheckout(t, other, "custom-root")

	lookupEnv := func(key string) (string, bool) {
		if key == "LINGTAI_DEV_ROOT" {
			return filepath.Join(other, "custom-root"), true
		}
		return "", false
	}

	dev, ok := findDevCheckouts(home, lookupEnv)
	if !ok {
		t.Fatalf("expected dev checkouts via LINGTAI_DEV_ROOT override")
	}
	if dev.KernelSrc != wantKernel {
		t.Fatalf("KernelSrc = %q, want %q", dev.KernelSrc, wantKernel)
	}
}

func TestFindDevCheckoutsEnvOverrideInvalidDoesNotAutoScan(t *testing.T) {
	home := t.TempDir()
	// Valid checkout exists under a common path, but an invalid explicit setting
	// must not silently fall back to guessing directories.
	makeKernelCheckout(t, home, filepath.Join("work", "GitHub"))

	lookupEnv := func(key string) (string, bool) {
		if key == "LINGTAI_DEV_ROOT" {
			return filepath.Join(home, "nonexistent"), true
		}
		return "", false
	}

	if _, ok := findDevCheckouts(home, lookupEnv); ok {
		t.Fatalf("expected invalid LINGTAI_DEV_ROOT to disable dev-mode detection")
	}
}

func TestDevInstallTargetsAreKernelOnly(t *testing.T) {
	home := t.TempDir()
	kernel := makeKernelCheckout(t, home, "devroot")
	lookupEnv := func(key string) (string, bool) {
		if key == "LINGTAI_DEV_ROOT" {
			return filepath.Join(home, "devroot"), true
		}
		return "", false
	}

	dev, ok := findDevCheckouts(home, lookupEnv)
	if !ok {
		t.Fatalf("expected dev checkouts")
	}
	targets := dev.installTargets()
	if len(targets) != 1 || targets[0] != kernel {
		t.Fatalf("install targets = %v, want exactly [%q] (kernel is the lingtai package; the TUI repo has no pyproject)", targets, kernel)
	}
}

func TestIsEditableForKernelMatchesSource(t *testing.T) {
	kernel := "/Users/dev/work/GitHub/lingtai-kernel"
	dev := devCheckout{KernelSrc: kernel}

	cases := []struct {
		name   string
		source string
		want   bool
	}{
		{"file url to kernel", "file://" + kernel, true},
		{"file url with trailing slash", "file://" + kernel + "/", true},
		{"plain path", kernel, true},
		{"unrelated wheel cache", "file:///tmp/pip-build/lingtai", false},
		{"empty source", "", false},
		{"different checkout", "file:///Users/dev/other/lingtai-kernel", false},
	}
	for _, tc := range cases {
		if got := dev.isEditableForKernel(tc.source); got != tc.want {
			t.Fatalf("%s: isEditableForKernel(%q) = %v, want %v", tc.name, tc.source, got, tc.want)
		}
	}
}

// Guard: the discovered kernel path must be cleaned/absolute-ish so downstream
// path comparison and pip -e <path> behave predictably.
func TestFindDevCheckoutsReturnsCleanPath(t *testing.T) {
	home := t.TempDir()
	makeKernelCheckout(t, home, "devroot")
	lookupEnv := func(key string) (string, bool) {
		if key == "LINGTAI_DEV_ROOT" {
			return filepath.Join(home, "devroot", "..", "devroot"), true
		}
		return "", false
	}
	dev, ok := findDevCheckouts(home, lookupEnv)
	if !ok {
		t.Fatalf("expected dev checkouts")
	}
	if strings.Contains(dev.KernelSrc, "..") || filepath.Clean(dev.KernelSrc) != dev.KernelSrc {
		t.Fatalf("KernelSrc not clean: %q", dev.KernelSrc)
	}
}
