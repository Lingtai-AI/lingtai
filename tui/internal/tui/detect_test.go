package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, dir string, manifest map[string]interface{}) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	data, _ := json.MarshalIndent(manifest, "", "  ")
	os.WriteFile(filepath.Join(dir, ".agent.json"), data, 0o644)
}

func TestDetectOrchestrators_NoAgents(t *testing.T) {
	baseDir := t.TempDir()
	result := DetectOrchestrators(baseDir)
	if len(result) != 0 {
		t.Errorf("expected 0 orchestrators, got %d", len(result))
	}
}

func TestIsOrchestrator_HumanAdminNull(t *testing.T) {
	manifest := map[string]interface{}{
		"agent_name": "human",
		"admin":      nil,
	}
	if IsOrchestrator(manifest) {
		t.Error("human (admin=nil) should not be orchestrator")
	}
}

func TestIsOrchestrator_AvatarEmptyAdmin(t *testing.T) {
	manifest := map[string]interface{}{
		"agent_name": "scout",
		"admin":      map[string]interface{}{},
	}
	if IsOrchestrator(manifest) {
		t.Error("avatar (admin={}) should not be orchestrator")
	}
}

func TestIsOrchestrator_OrchestratorKarmaTrue(t *testing.T) {
	manifest := map[string]interface{}{
		"agent_name": "orchestrator",
		"admin":      map[string]interface{}{"karma": true},
	}
	if !IsOrchestrator(manifest) {
		t.Error("admin={karma:true} should be orchestrator")
	}
}

func TestDetectOrchestrators_Mixed(t *testing.T) {
	baseDir := t.TempDir()

	// Human — admin: null
	writeManifest(t, filepath.Join(baseDir, "human"), map[string]interface{}{
		"agent_name": "human",
		"admin":      nil,
	})

	// Avatar — admin: {}
	writeManifest(t, filepath.Join(baseDir, "scout"), map[string]interface{}{
		"agent_name": "scout",
		"admin":      map[string]interface{}{},
	})

	// Orchestrator — admin: {karma: true}
	writeManifest(t, filepath.Join(baseDir, "orchestrator"), map[string]interface{}{
		"agent_name": "orchestrator",
		"admin":      map[string]interface{}{"karma": true},
	})

	result := DetectOrchestrators(baseDir)
	if len(result) != 1 {
		t.Fatalf("expected 1 orchestrator, got %d: %v", len(result), result)
	}
	if result[0] != "orchestrator" {
		t.Errorf("expected orchestrator, got %q", result[0])
	}
}

func TestDetectOrchestrators_Multiple(t *testing.T) {
	baseDir := t.TempDir()

	writeManifest(t, filepath.Join(baseDir, "orch1"), map[string]interface{}{
		"agent_name": "orch1",
		"admin":      map[string]interface{}{"karma": true},
	})
	writeManifest(t, filepath.Join(baseDir, "orch2"), map[string]interface{}{
		"agent_name": "orch2",
		"admin":      map[string]interface{}{"karma": true, "nirvana": true},
	})

	result := DetectOrchestrators(baseDir)
	if len(result) != 2 {
		t.Fatalf("expected 2 orchestrators, got %d: %v", len(result), result)
	}
}
