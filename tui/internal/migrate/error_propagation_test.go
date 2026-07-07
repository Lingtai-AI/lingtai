package migrate

// Regression tests for issue #502: schema-critical migrations (m028, m030,
// m039) must propagate per-file transformation failures instead of silently
// swallowing them, so a partial failure never advances the version.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeErrTestInit writes <lingtaiDir>/<agent>/init.json and returns its path.
func writeErrTestInit(t *testing.T, lingtaiDir, agent string, doc map[string]interface{}) string {
	t.Helper()
	agentDir := filepath.Join(lingtaiDir, agent)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(agentDir, "init.json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// makeDirReadOnly makes dir unwritable (so sibling .tmp files cannot be
// created) and restores it on cleanup so TempDir removal succeeds.
func makeDirReadOnly(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root — read-only dirs are still writable")
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func legacyAddonsInit() map[string]interface{} {
	return map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "x"},
		"addons": map[string]interface{}{
			"imap": map[string]interface{}{"config": ".secrets/imap.json"},
		},
	}
}

func TestM028WriteFailurePropagates(t *testing.T) {
	lingtaiDir := filepath.Join(t.TempDir(), ".lingtai")
	initPath := writeErrTestInit(t, lingtaiDir, "alice", legacyAddonsInit())
	before, _ := os.ReadFile(initPath)
	makeDirReadOnly(t, filepath.Dir(initPath))

	err := migrateAddonsToMCP(lingtaiDir)
	if err == nil {
		t.Fatal("expected error when init.json cannot be rewritten, got nil")
	}
	if !strings.Contains(err.Error(), "alice") {
		t.Errorf("error should name the failing agent, got: %v", err)
	}
	after, _ := os.ReadFile(initPath)
	if string(before) != string(after) {
		t.Error("init.json must be left untouched on failure")
	}
}

func TestM028PartialFailureStillMigratesHealthyAgents(t *testing.T) {
	lingtaiDir := filepath.Join(t.TempDir(), ".lingtai")
	brokenPath := writeErrTestInit(t, lingtaiDir, "broken", legacyAddonsInit())
	healthyPath := writeErrTestInit(t, lingtaiDir, "healthy", legacyAddonsInit())
	makeDirReadOnly(t, filepath.Dir(brokenPath))

	err := migrateAddonsToMCP(lingtaiDir)
	if err == nil {
		t.Fatal("expected aggregate error, got nil")
	}
	if strings.Contains(err.Error(), "healthy") {
		t.Errorf("healthy agent must not appear in the error, got: %v", err)
	}

	data, readErr := os.ReadFile(healthyPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, isList := doc["addons"].([]interface{}); !isList {
		t.Error("healthy agent should still have been converted to list form")
	}
}

func TestM030WriteFailurePropagates(t *testing.T) {
	lingtaiDir := filepath.Join(t.TempDir(), ".lingtai")
	initPath := writeErrTestInit(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{
			"preset": map[string]interface{}{
				"active": "~/.lingtai-tui/presets/minimax.json",
			},
		},
	})
	makeDirReadOnly(t, filepath.Dir(initPath))

	err := migratePresetDirSplit(lingtaiDir)
	if err == nil {
		t.Fatal("expected error when init.json cannot be rewritten, got nil")
	}
	if !strings.Contains(err.Error(), "alice") {
		t.Errorf("error should name the failing agent, got: %v", err)
	}
}

func TestM039WriteFailurePropagates(t *testing.T) {
	lingtaiDir := filepath.Join(t.TempDir(), ".lingtai")
	initPath := writeErrTestInit(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{
			// Root context_limit with no llm.context_limit forces a repair write.
			"context_limit": float64(200000),
		},
	})
	makeDirReadOnly(t, filepath.Dir(initPath))

	err := migrateAgentInitContextPresetRepairOnly(lingtaiDir)
	if err == nil {
		t.Fatal("expected error when init.json cannot be rewritten, got nil")
	}
	if !strings.Contains(err.Error(), "alice") {
		t.Errorf("error should name the failing agent, got: %v", err)
	}
}

// TestRunDoesNotStampVersionOnMigrationFailure pins the issue #502 invariant
// end to end: a failing schema-critical migration must leave meta.json at the
// old version so the migration re-runs on the next launch.
func TestRunDoesNotStampVersionOnMigrationFailure(t *testing.T) {
	lingtaiDir := filepath.Join(t.TempDir(), ".lingtai")
	initPath := writeErrTestInit(t, lingtaiDir, "alice", legacyAddonsInit())
	metaPath := filepath.Join(lingtaiDir, "meta.json")
	if err := os.WriteFile(metaPath, []byte(`{"version": 27}`), 0o644); err != nil {
		t.Fatal(err)
	}
	makeDirReadOnly(t, filepath.Dir(initPath))

	if err := Run(lingtaiDir); err == nil {
		t.Fatal("expected Run to fail when m028 cannot rewrite init.json")
	}

	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var meta metaFile
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Version != 27 {
		t.Errorf("version advanced to %d despite failed migration; want 27", meta.Version)
	}
}
