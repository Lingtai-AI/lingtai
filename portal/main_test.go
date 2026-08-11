package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStartPortalFailsWhenDiscoveryDirectoryCannotBeCreated verifies the
// startup boundary from issue #844: if the .portal discovery directory
// cannot be created, startup returns a surfaced error and does not report
// successful startup.
func TestStartPortalFailsWhenDiscoveryDirectoryCannotBeCreated(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	if err := os.Mkdir(lingtaiDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", lingtaiDir, err)
	}
	// Occupy the discovery-directory path with a regular file so MkdirAll
	// fails deterministically on every platform.
	portalPath := filepath.Join(lingtaiDir, ".portal")
	if err := os.WriteFile(portalPath, []byte("occupied"), 0o644); err != nil {
		t.Fatalf("write %s: %v", portalPath, err)
	}

	srv, _, err := startPortal(dir, "", 0, "en")
	if err == nil {
		t.Fatal("startPortal succeeded, want error because the discovery directory could not be created")
	}
	if srv != nil {
		t.Fatal("startPortal returned a server despite the discovery-directory failure")
	}
	if !strings.Contains(err.Error(), "discovery directory") {
		t.Fatalf("error %q should identify the discovery-directory creation failure", err)
	}
}

// TestStartPortalFailsWhenPortFileCannotBeWritten verifies the startup
// boundary from issue #844: if the bound port cannot be published to the
// port file, startup returns a surfaced error and does not report
// successful startup.
func TestStartPortalFailsWhenPortFileCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	portalDir := filepath.Join(dir, ".lingtai", ".portal")
	if err := os.MkdirAll(portalDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", portalDir, err)
	}
	// Occupy the port path with a directory so the port-file write fails
	// deterministically on every platform.
	if err := os.Mkdir(filepath.Join(portalDir, "port"), 0o755); err != nil {
		t.Fatalf("mkdir %s/port: %v", portalDir, err)
	}

	srv, _, err := startPortal(dir, "", 0, "en")
	if err == nil {
		t.Fatal("startPortal succeeded, want error because the port file could not be written")
	}
	if srv != nil {
		t.Fatal("startPortal returned a server despite the port-publication failure")
	}
	if !strings.Contains(err.Error(), "publish discovery port") {
		t.Fatalf("error %q should identify the discovery-port publication failure", err)
	}
}
