// internal/fs/agent_test.go
package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAgent_ValidManifest(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "alice")
	os.MkdirAll(agentDir, 0o755)

	manifest := map[string]interface{}{
		"agent_name":   "alice",
		"address":      "alice",
		"state":        "ACTIVE",
		"admin":        map[string]interface{}{"karma": true},
		"capabilities": []string{"file", "vision"},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(agentDir, ".agent.json"), data, 0o644)

	node, err := ReadAgent(agentDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.AgentName != "alice" {
		t.Errorf("agent_name = %q, want %q", node.AgentName, "alice")
	}
	if node.State != "ACTIVE" {
		t.Errorf("state = %q, want %q", node.State, "ACTIVE")
	}
	if node.IsHuman {
		t.Error("is_human = true, want false")
	}
	if len(node.Capabilities) != 2 {
		t.Errorf("capabilities len = %d, want 2", len(node.Capabilities))
	}
}

func TestReadAgent_HumanAgent(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "human")
	os.MkdirAll(agentDir, 0o755)

	// admin: null → is_human = true
	manifest := map[string]interface{}{
		"agent_name": "human",
		"address":    "human",
		"admin":      nil,
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(agentDir, ".agent.json"), data, 0o644)

	node, err := ReadAgent(agentDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !node.IsHuman {
		t.Error("is_human = false, want true (admin: null)")
	}
}

func TestReadAgent_MissingAdminKey(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "human2")
	os.MkdirAll(agentDir, 0o755)

	// admin key absent → is_human = true
	manifest := map[string]interface{}{
		"agent_name": "human2",
		"address":    "human2",
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(agentDir, ".agent.json"), data, 0o644)

	node, err := ReadAgent(agentDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !node.IsHuman {
		t.Error("is_human = false, want true (admin key absent)")
	}
}

func TestReadAgent_NoManifest(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadAgent(dir)
	if err == nil {
		t.Error("expected error for missing .agent.json")
	}
}

func TestCapabilitiesForDisplay_AugmentsIntrinsics(t *testing.T) {
	// .agent.json manifest capabilities, as the kanban/props view sees them.
	manifest := []string{
		"knowledge", "skills", "bash", "avatar", "daemon", "mcp",
		"read", "write", "edit", "glob", "grep", "vision", "web_search",
	}

	got := CapabilitiesForDisplay(manifest)

	// The four intrinsic agent capabilities must be present.
	for _, want := range []string{"system", "soul", "email", "psyche"} {
		if !contains(got, want) {
			t.Errorf("CapabilitiesForDisplay() missing intrinsic %q; got %v", want, got)
		}
	}

	// Intrinsics lead, manifest capabilities follow in their original order.
	want := []string{
		"system", "soul", "email", "psyche",
		"knowledge", "skills", "bash", "avatar", "daemon", "mcp",
		"read", "write", "edit", "glob", "grep", "vision", "web_search",
	}
	if !equalSlices(got, want) {
		t.Errorf("CapabilitiesForDisplay() = %v, want %v", got, want)
	}
}

func TestCapabilitiesForDisplay_NoDuplicates(t *testing.T) {
	// A manifest that already lists some intrinsics must not get them twice.
	manifest := []string{"email", "bash", "soul", "read"}

	got := CapabilitiesForDisplay(manifest)

	seen := map[string]int{}
	for _, c := range got {
		seen[c]++
	}
	for c, n := range seen {
		if n > 1 {
			t.Errorf("capability %q appears %d times, want 1; got %v", c, n, got)
		}
	}

	// Intrinsics still lead (deduped against the manifest), then the
	// remaining manifest entries keep their original order.
	want := []string{"system", "soul", "email", "psyche", "bash", "read"}
	if !equalSlices(got, want) {
		t.Errorf("CapabilitiesForDisplay() = %v, want %v", got, want)
	}
}

func TestCapabilitiesForDisplay_EmptyManifest(t *testing.T) {
	got := CapabilitiesForDisplay(nil)
	want := []string{"system", "soul", "email", "psyche"}
	if !equalSlices(got, want) {
		t.Errorf("CapabilitiesForDisplay(nil) = %v, want %v", got, want)
	}
}

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
