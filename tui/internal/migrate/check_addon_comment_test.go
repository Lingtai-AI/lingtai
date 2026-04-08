package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckAddonComment_DetectsLegacyBlock(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")

	// Two agents with legacy addon blocks, one with custom (no block), one with no comment.md
	writeFile(t, filepath.Join(lingtaiDir, "alpha", "system", "comment.md"),
		"## Add-ons\n\nSome addon instructions go here.\n")
	writeFile(t, filepath.Join(lingtaiDir, "beta", "system", "comment.md"),
		"User custom prelude\n\n## Add-ons\n\n- imap\n")
	writeFile(t, filepath.Join(lingtaiDir, "gamma", "system", "comment.md"),
		"# My custom comment\n\nNothing addon-related here.\n")
	// "delta" has no comment.md at all
	os.MkdirAll(filepath.Join(lingtaiDir, "delta", "system"), 0o755)

	matches, err := CheckAddonComment(lingtaiDir)
	if err != nil {
		t.Fatalf("CheckAddonComment: %v", err)
	}

	// Should match alpha and beta only — not gamma (no signature) and not delta (no file)
	if len(matches) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(matches), matches)
	}

	gotAlpha, gotBeta := false, false
	for _, m := range matches {
		switch filepath.Base(filepath.Dir(filepath.Dir(m))) {
		case "alpha":
			gotAlpha = true
		case "beta":
			gotBeta = true
		case "gamma":
			t.Errorf("gamma should not match (no addon signature): %s", m)
		case "delta":
			t.Errorf("delta should not match (no comment.md): %s", m)
		}
	}
	if !gotAlpha {
		t.Error("alpha not in matches")
	}
	if !gotBeta {
		t.Error("beta not in matches")
	}
}

func TestCheckAddonComment_SkipsDotDirs(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")

	// Hidden helper dirs (.portal/, .skills/, .addons/, .tui-asset/) should not be scanned
	writeFile(t, filepath.Join(lingtaiDir, ".portal", "system", "comment.md"),
		"## Add-ons\nshould be ignored\n")
	writeFile(t, filepath.Join(lingtaiDir, ".addons", "system", "comment.md"),
		"## Add-ons\nshould be ignored\n")
	// Real agent
	writeFile(t, filepath.Join(lingtaiDir, "agent", "system", "comment.md"),
		"## Add-ons\nshould match\n")

	matches, err := CheckAddonComment(lingtaiDir)
	if err != nil {
		t.Fatalf("CheckAddonComment: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("expected 1 match (only the real agent), got %d: %v", len(matches), matches)
	}
}

func TestCheckAddonComment_MissingLingtaiDir(t *testing.T) {
	dir := t.TempDir()
	// Don't create .lingtai/ at all
	matches, err := CheckAddonComment(filepath.Join(dir, ".lingtai"))
	if err != nil {
		t.Fatalf("CheckAddonComment on missing dir should not error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches on missing dir, got %d", len(matches))
	}
}

func TestMarkAddonCommentNotified_PreservesVersion(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	// Pre-existing meta.json at version 5
	writeMeta(t, lingtaiDir, 5)

	// Mark notified
	if err := MarkAddonCommentNotified(lingtaiDir); err != nil {
		t.Fatalf("MarkAddonCommentNotified: %v", err)
	}

	// Verify both fields persisted
	data, err := os.ReadFile(filepath.Join(lingtaiDir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var m metaFile
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse meta.json: %v", err)
	}
	if m.Version != 5 {
		t.Errorf("expected version preserved at 5, got %d", m.Version)
	}
	if !m.AddonCommentCleanupNotified {
		t.Error("expected AddonCommentCleanupNotified=true after marking")
	}
}

func TestIsAddonCommentNotified_DefaultsFalse(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	notified, err := IsAddonCommentNotified(lingtaiDir)
	if err != nil {
		t.Fatalf("IsAddonCommentNotified: %v", err)
	}
	if notified {
		t.Error("expected notified=false for fresh project")
	}
}

func TestIsAddonCommentNotified_AfterMarking(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	if err := MarkAddonCommentNotified(lingtaiDir); err != nil {
		t.Fatalf("MarkAddonCommentNotified: %v", err)
	}
	notified, err := IsAddonCommentNotified(lingtaiDir)
	if err != nil {
		t.Fatalf("IsAddonCommentNotified: %v", err)
	}
	if !notified {
		t.Error("expected notified=true after marking")
	}
}

func TestRunPreservesNotificationFlag(t *testing.T) {
	// Regression test: bumping the migration version should NOT clear the
	// addon_comment_cleanup_notified flag in meta.json.
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")
	os.MkdirAll(lingtaiDir, 0o755)

	// Start at an old version with the notification flag set
	meta := metaFile{Version: 1, AddonCommentCleanupNotified: true}
	data, _ := json.Marshal(meta)
	os.WriteFile(filepath.Join(lingtaiDir, "meta.json"), data, 0o644)

	// Run migrations to bump version
	if err := Run(lingtaiDir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify both fields still set
	final := readMeta(t, lingtaiDir)
	if final.Version != CurrentVersion {
		t.Errorf("expected version %d, got %d", CurrentVersion, final.Version)
	}
	if !final.AddonCommentCleanupNotified {
		t.Error("AddonCommentCleanupNotified flag was lost after Run()")
	}
}
