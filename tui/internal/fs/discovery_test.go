// internal/fs/discovery_test.go
package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeDiscoveryManifest writes a minimal .agent.json into agentDir.
func writeDiscoveryManifest(t *testing.T, agentDir, name string) {
	t.Helper()
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := map[string]interface{}{
		"agent_name": name,
		"address":    name,
		"state":      "ACTIVE",
		"admin":      agentDir, // non-nil admin -> is_human=false
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverAgents_MissingManifestExcluded(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "not-an-agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryManifest(t, filepath.Join(base, "real-agent"), "real-agent")

	nodes, err := DiscoverAgents(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1 (absent manifest must be excluded)", len(nodes))
	}
	if nodes[0].AgentName != "real-agent" {
		t.Errorf("agent_name = %q, want real-agent", nodes[0].AgentName)
	}
}

func TestDiscoverAgents_MalformedManifestStillDiscoverable(t *testing.T) {
	base := t.TempDir()
	writeDiscoveryManifest(t, filepath.Join(base, "ok"), "ok")

	// Malformed JSON (truncated object): present but unparseable.
	badJSON := filepath.Join(base, "bad-json")
	if err := os.MkdirAll(badJSON, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badJSON, ".agent.json"), []byte(`{"agent_name": "bad",`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Syntactically valid JSON that is not an object: still malformed as a manifest.
	nonObject := filepath.Join(base, "non-object")
	if err := os.MkdirAll(nonObject, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonObject, ".agent.json"), []byte(`"just a string"`), 0o644); err != nil {
		t.Fatal(err)
	}

	nodes, err := DiscoverAgents(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d, want 3 (malformed manifests must stay discoverable)", len(nodes))
	}

	byDir := map[string]AgentNode{}
	for _, n := range nodes {
		byDir[filepath.Base(n.WorkingDir)] = n
	}
	for _, name := range []string{"bad-json", "non-object"} {
		n, ok := byDir[name]
		if !ok {
			t.Fatalf("malformed agent %q missing from discovery: %+v", name, nodes)
		}
		if n.State != "MALFORMED" {
			t.Errorf("%s: state = %q, want MALFORMED", name, n.State)
		}
		if n.Error == "" {
			t.Errorf("%s: error = %q, want explicit error", name, n.Error)
		}
	}
}

func TestDiscoverAgents_UnreadableManifestStillDiscoverable(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "unreadable")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A directory at the .agent.json path is present but cannot be read as a
	// file — a portable stand-in for an unreadable manifest.
	if err := os.MkdirAll(filepath.Join(dir, ".agent.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	nodes, err := DiscoverAgents(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1 (unreadable manifest must stay discoverable)", len(nodes))
	}
	if nodes[0].State != "MALFORMED" || nodes[0].Error == "" {
		t.Errorf("unreadable node = %+v, want MALFORMED state with explicit error", nodes[0])
	}
}

// TestDiscoverAgents_MalformedNodeSerializesError pins the wire format of the
// repair target: the error is visible in the serialized node, not hidden.
func TestDiscoverAgents_MalformedNodeSerializesError(t *testing.T) {
	base := t.TempDir()
	bad := filepath.Join(base, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, ".agent.json"), []byte(`{oops`), 0o644); err != nil {
		t.Fatal(err)
	}

	nodes, err := DiscoverAgents(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	payload, err := json.Marshal(nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded["error"]; got == nil || got == "" {
		t.Errorf("serialized error = %#v, want explicit error string", got)
	}
	if got := decoded["state"]; got != "MALFORMED" {
		t.Errorf("serialized state = %#v, want MALFORMED", got)
	}
}

// TestBuildNetwork_KeepsMalformedState ensures the explicit repair state of a
// present-but-malformed manifest survives BuildNetwork's liveness
// normalization (it must not be relabeled SUSPENDED).
func TestBuildNetwork_KeepsMalformedState(t *testing.T) {
	base := t.TempDir()
	bad := filepath.Join(base, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, ".agent.json"), []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}

	net, err := BuildNetwork(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(net.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(net.Nodes))
	}
	if net.Nodes[0].State != "MALFORMED" {
		t.Errorf("state = %q, want MALFORMED (must not be relabeled SUSPENDED)", net.Nodes[0].State)
	}
	if net.Nodes[0].Error == "" {
		t.Error("error = empty, want explicit error for malformed manifest")
	}
}
