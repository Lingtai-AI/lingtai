package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// mkAgentDir creates a subdirectory under base and returns its path.
func mkAgentDir(t *testing.T, base, name string) string {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiscoverAgents_MissingManifestExcluded(t *testing.T) {
	base := t.TempDir()
	mkAgentDir(t, base, "not-an-agent") // no .agent.json
	writeAgentManifest(t, filepath.Join(base, "real-agent"), "real-agent", false)

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

	// Valid manifest: parsed normally.
	writeAgentManifest(t, mkAgentDir(t, base, "ok"), "ok", false)

	// Malformed JSON (truncated object): present but unparseable.
	badJSON := mkAgentDir(t, base, "bad-json")
	if err := os.WriteFile(filepath.Join(badJSON, ".agent.json"), []byte(`{"agent_name": "bad",`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Syntactically valid JSON that is not an object: still malformed as a manifest.
	nonObject := mkAgentDir(t, base, "non-object")
	if err := os.WriteFile(filepath.Join(nonObject, ".agent.json"), []byte(`["not", "an", "object"]`), 0o644); err != nil {
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
	dir := mkAgentDir(t, base, "unreadable")
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

func TestReadAgent_PreservesAgentID(t *testing.T) {
	dir := t.TempDir()
	withID := map[string]interface{}{
		"agent_id":   "id-stable-42",
		"agent_name": "alice",
		"address":    "alice",
	}
	data, _ := json.Marshal(withID)
	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	node, err := ReadAgent(dir)
	if err != nil {
		t.Fatal(err)
	}
	if node.AgentID != "id-stable-42" {
		t.Errorf("agent_id = %q, want id-stable-42", node.AgentID)
	}

	// Older manifests that omit agent_id must keep parsing to an empty value.
	legacy := map[string]interface{}{
		"agent_name": "bob",
		"address":    "bob",
	}
	data, _ = json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	node, err = ReadAgent(dir)
	if err != nil {
		t.Fatal(err)
	}
	if node.AgentID != "" {
		t.Errorf("legacy agent_id = %q, want empty (older manifests omit it)", node.AgentID)
	}
}

// TestBuildNetwork_SerializesAgentIDThroughTopology pins the stable identifier
// in the Portal topology response: BuildNetwork is what the API handler feeds
// to json.NewEncoder, so marshaling the Network mirrors the wire format.
func TestBuildNetwork_SerializesAgentIDThroughTopology(t *testing.T) {
	base := t.TempDir()
	aliceDir := filepath.Join(base, "alice")
	writeAgentManifest(t, aliceDir, "alice", false)

	// Stamp the kernel-published stable identifier into the manifest.
	manifestPath := filepath.Join(aliceDir, ".agent.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["agent_id"] = "id-alice-1"
	out, _ := json.Marshal(m)
	if err := os.WriteFile(manifestPath, out, 0o644); err != nil {
		t.Fatal(err)
	}

	net, err := BuildNetwork(base)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(net)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Nodes []map[string]interface{} `json:"nodes"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(decoded.Nodes))
	}
	if got := decoded.Nodes[0]["agent_id"]; got != "id-alice-1" {
		t.Errorf("serialized agent_id = %#v, want id-alice-1", got)
	}
}

// TestBuildNetwork_KeepsMalformedState ensures the explicit repair state of a
// present-but-malformed manifest survives BuildNetwork's liveness
// normalization (it must not be relabeled SUSPENDED).
func TestBuildNetwork_KeepsMalformedState(t *testing.T) {
	base := t.TempDir()
	bad := mkAgentDir(t, base, "bad")
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
